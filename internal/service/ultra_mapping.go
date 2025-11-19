// internal/service/ultra_mapper.go
// FINAL VERSION - Stores scaled values correctly

package service

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"solar_project/internal/config"
	"solar_project/internal/domain"
	"solar_project/pkg/logger"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ScaleFunc is a pre-compiled scaling function (zero allocation)
type ScaleFunc func(int) int

// Pre-compiled scale functions (THESE DO THE ACTUAL CONVERSION)
var (
	ScalePowerMW    = func(v int) int { return v / 1000 }  // 3432000 → 3432 W
	ScaleEnergy1000 = func(v int) int { return v / 1000 }  // 110130000 → 110130 Wh
	ScaleVoltage100 = func(v int) int { return v / 100 }   // 45640 → 456 V (0.01V units)
	ScaleCurrent100 = func(v int) int { return v / 100 }   // 36000 → 360 A (0.01A units)
	ScaleTemp10     = func(v int) int { return v / 10 }    // 530 → 53°C
	ScaleFreq1000   = func(v int) int { return v / 1000 }  // 499900 → 499 Hz (0.001Hz)
	ScaleNone       = func(v int) int { return v }         // No scaling
)

// UltraFieldSpec - Field with pre-compiled scale function
type UltraFieldSpec struct {
	SourceKey  string
	TargetName string
	Scale      ScaleFunc
	Required   bool
	DefaultVal int
	IsString   bool // NEW: Track if field is string (don't scale)
}

// UltraMapping - Cache-friendly mapping
type UltraMapping struct {
	SourceID     string
	NestedPath   string
	Fields       []UltraFieldSpec
	RequiredKeys map[string]bool
}

// UltraMapper - Maximum performance mapper
type UltraMapper struct {
	mu         sync.RWMutex
	mappings   map[string]*UltraMapping
	collection *mongo.Collection
	stopReload chan struct{}
}

func NewUltraMapper(db *config.MongoDatabase) (*UltraMapper, error) {
	m := &UltraMapper{
		mappings:   make(map[string]*UltraMapping),
		collection: db.Database.Collection("mappings"),
		stopReload: make(chan struct{}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	indexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "source_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	}
	m.collection.Indexes().CreateOne(ctx, indexModel)

	if err := m.Load(); err != nil {
		return nil, err
	}

	if len(m.mappings) == 0 {
		if err := m.seedUltraDefaults(); err != nil {
			return nil, err
		}
	}

	go m.autoReload()

	logger.Info(fmt.Sprintf("UltraMapper initialized with %d zero-cost mappings", len(m.mappings)))
	return m, nil
}

func (m *UltraMapper) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cursor, err := m.collection.Find(ctx, bson.M{"active": true})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	newMappings := make(map[string]*UltraMapping)
	for cursor.Next(ctx) {
		var mapping domain.DataSourceMapping
		if err := cursor.Decode(&mapping); err != nil {
			continue
		}
		
		compiled := m.compileUltra(&mapping)
		newMappings[mapping.SourceID] = compiled
	}

	m.mappings = newMappings
	return nil
}

func (m *UltraMapper) compileUltra(src *domain.DataSourceMapping) *UltraMapping {
	ultra := &UltraMapping{
		SourceID:     src.SourceID,
		NestedPath:   src.NestedPath,
		Fields:       make([]UltraFieldSpec, 0, len(src.Mappings)),
		RequiredKeys: make(map[string]bool),
	}

	for _, field := range src.Mappings {
		spec := UltraFieldSpec{
			SourceKey:  field.SourceField,
			TargetName: field.StandardField,
			Required:   field.Required,
			DefaultVal: getDefaultInt(field.DefaultValue),
			Scale:      compileScaleFunc(field.Transform),
			IsString:   field.DataType == "string", // NEW
		}

		ultra.Fields = append(ultra.Fields, spec)
		
		if field.Required {
			ultra.RequiredKeys[field.SourceField] = true
		}
	}

	return ultra
}

func compileScaleFunc(transform string) ScaleFunc {
	if transform == "" {
		return ScaleNone
	}

	var op string
	var val float64
	if n, _ := fmt.Sscanf(transform, "%s:%f", &op, &val); n != 2 {
		return ScaleNone
	}

	// Return pre-compiled functions for common cases
	switch {
	case op == "divide" && val == 1000:
		return ScalePowerMW
	case op == "divide" && val == 100:
		return ScaleVoltage100
	case op == "divide" && val == 10:
		return ScaleTemp10
	case op == "multiply" && val == 0.001:
		return ScalePowerMW
	case op == "multiply" && val == 0.01:
		return ScaleVoltage100
	case op == "multiply" && val == 0.1:
		return ScaleTemp10
	default:
		return compileCustomScale(op, val)
	}
}

func compileCustomScale(op string, val float64) ScaleFunc {
	switch op {
	case "multiply":
		if val == math.Floor(val) {
			intVal := int(val)
			return func(v int) int { return v * intVal }
		}
		return func(v int) int { return int(float64(v) * val) }
	case "divide":
		if val != 0 {
			if val == math.Floor(val) {
				intVal := int(val)
				return func(v int) int { return v / intVal }
			}
			return func(v int) int { return int(float64(v) / val) }
		}
	}
	return ScaleNone
}

func (m *UltraMapper) DetectSourceID(data map[string]interface{}) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if sid, ok := data["source_id"].(string); ok {
		if _, exists := m.mappings[sid]; exists {
			return sid
		}
	}
	if dtype, ok := data["device_type"].(string); ok {
		if _, exists := m.mappings[dtype]; exists {
			return dtype
		}
	}

	allKeys := make(map[string]bool, len(data)*2)
	for key := range data {
		allKeys[key] = true
	}
	if nested, ok := data["data"].(map[string]interface{}); ok {
		for key := range nested {
			allKeys[key] = true
		}
	}

	bestMatch := ""
	bestScore := 0

	for sourceID, mapping := range m.mappings {
		allRequiredPresent := true
		for reqKey := range mapping.RequiredKeys {
			if !allKeys[reqKey] {
				allRequiredPresent = false
				break
			}
		}
		
		if !allRequiredPresent {
			continue
		}

		matchCount := 0
		for _, field := range mapping.Fields {
			if allKeys[field.SourceKey] {
				matchCount++
			}
		}

		threshold := len(mapping.Fields) * 2 / 5
		if matchCount > bestScore && matchCount >= threshold {
			bestScore = matchCount
			bestMatch = sourceID
		}
	}

	return bestMatch
}

// MapFields - CRITICAL FIX: Handle both strings and integers
func (m *UltraMapper) MapFields(sourceID string, data map[string]interface{}) (map[string]interface{}, error) {
	m.mu.RLock()
	mapping, exists := m.mappings[sourceID]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("mapping not found: %s", sourceID)
	}

	dataSource := data
	if mapping.NestedPath != "" {
		if nested, ok := data[mapping.NestedPath].(map[string]interface{}); ok {
			dataSource = nested
		}
	}

	result := make(map[string]interface{}, len(mapping.Fields))
	
	for _, spec := range mapping.Fields {
		// Get raw value
		rawValue := extractValue(dataSource, data, spec.SourceKey)
		
		// Check required
		if rawValue == nil && spec.Required {
			return nil, fmt.Errorf("required field missing: %s", spec.SourceKey)
		}
		
		if rawValue == nil {
			if spec.DefaultVal != 0 {
				result[spec.TargetName] = spec.DefaultVal
			}
			continue
		}

		// CRITICAL: Handle strings vs integers differently
		if spec.IsString {
			// Store strings as-is
			if str, ok := rawValue.(string); ok {
				result[spec.TargetName] = str
			} else {
				result[spec.TargetName] = fmt.Sprintf("%v", rawValue)
			}
		} else {
			// Scale integers
			intVal := fastToInt(rawValue)
			scaledVal := spec.Scale(intVal)
			result[spec.TargetName] = scaledVal
		}
	}

	return result, nil
}

// extractValue - Get raw value (interface{})
func extractValue(dataSource, fallback map[string]interface{}, key string) interface{} {
	if val, ok := dataSource[key]; ok {
		return val
	}
	if val, ok := fallback[key]; ok {
		return val
	}
	return nil
}

func fastToInt(val interface{}) int {
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

func (m *UltraMapper) seedUltraDefaults() error {
	defaults := []domain.DataSourceMapping{
		{
			SourceID:    "Inv",
			Description: "FoxESS Inverter (Ultra-Optimized with Correct Scaling)",
			NestedPath:  "data",
			Mappings: []domain.FieldMapping{
				// STRING FIELDS (no scaling)
				{SourceField: "sno", StandardField: "serial_no", DataType: "string", Required: true},
				{SourceField: "slid", StandardField: "slave_id", DataType: "string"},
				{SourceField: "model", StandardField: "model", DataType: "string"},
				
				// INTEGER FIELDS (with scaling)
				// Power: 3432000 → 3432 W
				{SourceField: "p", StandardField: "power", DataType: "int", Transform: "divide:1000"},
				
				// Energy: 110130000 → 110130 Wh, 418000 → 418 Wh
				{SourceField: "e", StandardField: "today_energy", DataType: "int", Transform: "divide:1000"},
				{SourceField: "te", StandardField: "total_energy", DataType: "int", Transform: "divide:1000"},
				
				// PV Voltages: 45640 → 456 V (0.01V precision)
				{SourceField: "pv1v", StandardField: "pv1_voltage", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv2v", StandardField: "pv2_voltage", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv3v", StandardField: "pv3_voltage", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv4v", StandardField: "pv4_voltage", DataType: "int", Transform: "divide:100"},
				
				// PV Currents: 36000 → 360 A (0.01A precision)
				{SourceField: "pv1c", StandardField: "pv1_current", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv2c", StandardField: "pv2_current", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv3c", StandardField: "pv3_current", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv4c", StandardField: "pv4_current", DataType: "int", Transform: "divide:100"},
				
				// Grid Voltages: 25130 → 251 V (0.01V precision)
				{SourceField: "gvr", StandardField: "grid_voltage_r", DataType: "int", Transform: "divide:100"},
				{SourceField: "gvs", StandardField: "grid_voltage_s", DataType: "int", Transform: "divide:100"},
				{SourceField: "gvt", StandardField: "grid_voltage_t", DataType: "int", Transform: "divide:100"},
				
				// Grid Currents: 4670000 → 4670 A (0.001A precision)
				{SourceField: "gcr", StandardField: "grid_current_r", DataType: "int", Transform: "divide:1000"},
				{SourceField: "gcs", StandardField: "grid_current_s", DataType: "int", Transform: "divide:1000"},
				{SourceField: "gct", StandardField: "grid_current_t", DataType: "int", Transform: "divide:1000"},
				
				// Temperature: 530 → 53°C
				{SourceField: "itmp", StandardField: "temperature", DataType: "int", Transform: "divide:10"},
				
				// Frequency: 499900 → 499 Hz (0.001Hz precision)
				{SourceField: "fr", StandardField: "frequency", DataType: "int", Transform: "divide:1000"},
				
				// Alarms (no scaling)
				{SourceField: "al1", StandardField: "alarm1", DataType: "int", DefaultValue: 0},
				{SourceField: "al2", StandardField: "alarm2", DataType: "int", DefaultValue: 0},
				{SourceField: "al3", StandardField: "alarm3", DataType: "int", DefaultValue: 0},
			},
			Active:    true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, mapping := range defaults {
		count, _ := m.collection.CountDocuments(ctx, bson.M{"source_id": mapping.SourceID})
		if count == 0 {
			m.collection.InsertOne(ctx, mapping)
			logger.Info(fmt.Sprintf("Created mapping for source_id: %s", mapping.SourceID))
		} else {
			// UPDATE existing mapping
			filter := bson.M{"source_id": mapping.SourceID}
			update := bson.M{"$set": mapping}
			m.collection.UpdateOne(ctx, filter, update)
			logger.Info(fmt.Sprintf("Updated mapping for source_id: %s", mapping.SourceID))
		}
	}

	return m.Load()
}

func (m *UltraMapper) autoReload() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.Load()
		case <-m.stopReload:
			return
		}
	}
}

func (m *UltraMapper) Close() {
	close(m.stopReload)
}

func getDefaultInt(val interface{}) int {
	if val == nil {
		return 0
	}
	return fastToInt(val)
}