// internal/service/ultra_mapper.go
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

// Pre-compiled scale functions
var (
	ScalePowerMW    = func(v int) int { return v / 1000 }  // milliwatts → watts
	ScaleEnergy100  = func(v int) int { return v / 100 }   // 0.01 units → whole
	ScaleVoltage10  = func(v int) int { return v / 10 }    // decivolts → volts
	ScaleCurrentMA  = func(v int) int { return v / 1000 }  // milliamps → amps
	ScaleTemp10     = func(v int) int { return v / 10 }    // 0.1°C → °C
	ScaleFreq100    = func(v int) int { return v / 100 }   // 0.01 Hz → Hz
	ScaleFreq1000   = func(v int) int { return v / 1000 }  // 0.001 Hz → Hz
	ScaleNone       = func(v int) int { return v }         // No scaling
)

// UltraFieldSpec - Optimized field with pre-compiled scale function
type UltraFieldSpec struct {
	SourceKey  string
	TargetName string
	Scale      ScaleFunc
	Required   bool
	DefaultVal int
}

// UltraMapping - Cache-friendly mapping
type UltraMapping struct {
	SourceID     string
	NestedPath   string
	Fields       []UltraFieldSpec
	RequiredKeys map[string]bool // For fast required check
}

// UltraMapper - Maximum performance mapper
type UltraMapper struct {
	mu         sync.RWMutex
	mappings   map[string]*UltraMapping
	collection *mongo.Collection
	stopReload chan struct{}
}

// NewUltraMapper creates the highest-performance mapper
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

// Load mappings and pre-compile everything
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

// compileUltra - Compile to fastest possible representation
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
		}

		ultra.Fields = append(ultra.Fields, spec)
		
		if field.Required {
			ultra.RequiredKeys[field.SourceField] = true
		}
	}

	return ultra
}

// compileScaleFunc - Choose the optimal pre-compiled function
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
	case op == "multiply" && val == 0.001:
		return ScaleCurrentMA
	case op == "multiply" && val == 0.01:
		return ScaleEnergy100
	case op == "multiply" && val == 0.1:
		return ScaleVoltage10
	case op == "divide" && val == 1000:
		return ScaleCurrentMA
	case op == "divide" && val == 100:
		return ScaleEnergy100
	case op == "divide" && val == 10:
		return ScaleVoltage10
	default:
		return compileCustomScale(op, val)
	}
}

// compileCustomScale - For non-standard scales
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

// DetectSourceID - Fast detection
func (m *UltraMapper) DetectSourceID(data map[string]interface{}) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Fast path: explicit hints
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

	// Extract all keys once
	allKeys := make(map[string]bool, len(data)*2)
	for key := range data {
		allKeys[key] = true
	}
	if nested, ok := data["data"].(map[string]interface{}); ok {
		for key := range nested {
			allKeys[key] = true
		}
	}

	// Find best match
	bestMatch := ""
	bestScore := 0

	for sourceID, mapping := range m.mappings {
		// Check all required fields present
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

		// Count matching fields
		matchCount := 0
		for _, field := range mapping.Fields {
			if allKeys[field.SourceKey] {
				matchCount++
			}
		}

		// 40% threshold
		threshold := len(mapping.Fields) * 2 / 5
		if matchCount > bestScore && matchCount >= threshold {
			bestScore = matchCount
			bestMatch = sourceID
		}
	}

	return bestMatch
}

// MapFields - High-performance mapping
func (m *UltraMapper) MapFields(sourceID string, data map[string]interface{}) (map[string]interface{}, error) {
	m.mu.RLock()
	mapping, exists := m.mappings[sourceID]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("mapping not found: %s", sourceID)
	}

	// Get data source
	dataSource := data
	if mapping.NestedPath != "" {
		if nested, ok := data[mapping.NestedPath].(map[string]interface{}); ok {
			dataSource = nested
		}
	}

	// Build result
	result := make(map[string]interface{}, len(mapping.Fields))
	
	for _, spec := range mapping.Fields {
		// Extract value
		rawVal := extractIntFast(dataSource, data, spec.SourceKey)
		
		// Check required
		if rawVal == 0 && spec.Required {
			if _, exists := dataSource[spec.SourceKey]; !exists {
				if _, exists := data[spec.SourceKey]; !exists {
					return nil, fmt.Errorf("required field missing: %s", spec.SourceKey)
				}
			}
		}
		
		// Apply scale (FASTEST PATH - direct function call)
		scaledVal := spec.Scale(rawVal)
		result[spec.TargetName] = scaledVal
	}

	return result, nil
}

// extractIntFast - Fast integer extraction
func extractIntFast(dataSource, fallback map[string]interface{}, key string) int {
	if val, ok := dataSource[key]; ok {
		return fastToInt(val)
	}
	if val, ok := fallback[key]; ok {
		return fastToInt(val)
	}
	return 0
}

// fastToInt - Quick conversion
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

// seedUltraDefaults - FoxESS mapping
func (m *UltraMapper) seedUltraDefaults() error {
	defaults := []domain.DataSourceMapping{
		{
			SourceID:    "current_format",
			Description: "Legacy format",
			NestedPath:  "data",
			Mappings: []domain.FieldMapping{
				{SourceField: "serial_no", StandardField: "serial_no", DataType: "string", DefaultValue: "UNKNOWN"},
				{SourceField: "s1v", StandardField: "voltage", DataType: "int"},
				{SourceField: "total_output_power", StandardField: "power", DataType: "int"},
				{SourceField: "f", StandardField: "frequency", DataType: "int"},
				{SourceField: "today_e", StandardField: "today_energy", DataType: "int"},
				{SourceField: "total_e", StandardField: "total_energy", DataType: "int"},
				{SourceField: "inv_temp", StandardField: "temperature", DataType: "int"},
				{SourceField: "fault_code", StandardField: "fault_code", DataType: "int"},
			},
			Active:    true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		{
			SourceID:    "Inv",
			Description: "FoxESS Inverter (Ultra-Optimized)",
			NestedPath:  "data",
			Mappings: []domain.FieldMapping{
				{SourceField: "sno", StandardField: "serial_no", DataType: "string", Required: true},
				{SourceField: "slid", StandardField: "slave_id", DataType: "int"},
				{SourceField: "p", StandardField: "power", DataType: "int", Transform: "divide:1000"},
				{SourceField: "e", StandardField: "today_energy", DataType: "int", Transform: "divide:1000"},
				{SourceField: "te", StandardField: "total_energy", DataType: "int", Transform: "divide:1000"},
				{SourceField: "pv1v", StandardField: "pv1_voltage", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv2v", StandardField: "pv2_voltage", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv3v", StandardField: "pv3_voltage", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv4v", StandardField: "pv4_voltage", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv1c", StandardField: "pv1_current", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv2c", StandardField: "pv2_current", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv3c", StandardField: "pv3_current", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv4c", StandardField: "pv4_current", DataType: "int", Transform: "divide:100"},
				{SourceField: "gvr", StandardField: "grid_voltage_r", DataType: "int", Transform: "divide:100"},
				{SourceField: "gvs", StandardField: "grid_voltage_s", DataType: "int", Transform: "divide:100"},
				{SourceField: "gvt", StandardField: "grid_voltage_t", DataType: "int", Transform: "divide:100"},
				{SourceField: "gcr", StandardField: "grid_current_r", DataType: "int", Transform: "divide:1000"},
				{SourceField: "gcs", StandardField: "grid_current_s", DataType: "int", Transform: "divide:1000"},
				{SourceField: "gct", StandardField: "grid_current_t", DataType: "int", Transform: "divide:1000"},
				{SourceField: "itmp", StandardField: "temperature", DataType: "int", Transform: "divide:10"},
				{SourceField: "fr", StandardField: "frequency", DataType: "int", Transform: "divide:1000"},
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