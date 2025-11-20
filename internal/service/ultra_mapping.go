// internal/service/ultra_mapping.go
// FIXED VERSION - Proper data extraction and scaling

package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"solar_project/internal/config"
	"solar_project/internal/domain"
	"solar_project/pkg/logger"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ScaleSpec defines source field → target field with scaling
type ScaleSpec struct {
	Source      string  // e.g., "totaloutputpower"
	Target      string  // e.g., "total_output_power" (for server storage)
	ScaleFactor float64 // e.g., 0.001 for divide by 1000
	IsString    bool    // true for string fields
	DefaultVal  interface{}
}

// UltraMapping - Pre-compiled optimized mapping
type UltraMapping struct {
	SourceID      string
	NestedPath    string
	Specs         []ScaleSpec
	SpecsBySource map[string]ScaleSpec
}

// UltraMapper - Ultra-fast zero-allocation mapper
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

	logger.Info(fmt.Sprintf("UltraMapper initialized with %d mappings", len(m.mappings)))
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

		compiled := m.compileMapping(&mapping)
		newMappings[mapping.SourceID] = compiled
	}

	m.mappings = newMappings
	logger.Debug(fmt.Sprintf("Loaded %d mappings into memory", len(m.mappings)))
	return nil
}

func (m *UltraMapper) compileMapping(src *domain.DataSourceMapping) *UltraMapping {
	ultra := &UltraMapping{
		SourceID:      src.SourceID,
		NestedPath:    src.NestedPath,
		Specs:         make([]ScaleSpec, len(src.Mappings)),
		SpecsBySource: make(map[string]ScaleSpec),
	}

	for i, field := range src.Mappings {
		spec := ScaleSpec{
			Source:      field.SourceField,
			Target:      field.StandardField,
			IsString:    field.DataType == "string",
			ScaleFactor: parseScaleFactor(field.Transform),
			DefaultVal:  field.DefaultValue,
		}

		ultra.Specs[i] = spec
		ultra.SpecsBySource[field.SourceField] = spec
	}

	return ultra
}

func parseScaleFactor(transform string) float64 {
	if transform == "" {
		return 1.0
	}

	var op string
	var val float64
	if n, _ := fmt.Sscanf(transform, "%s:%f", &op, &val); n != 2 {
		return 1.0
	}

	switch op {
	case "divide":
		if val != 0 {
			return 1.0 / val
		}
	case "multiply":
		return val
	}
	return 1.0
}

func (m *UltraMapper) DetectSourceID(data map[string]interface{}) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Fast path: check explicit hints
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

	// Check all mappings (Inv is default for FoxESS)
	if _, exists := m.mappings["Inv"]; exists {
		return "Inv"
	}

	return ""
}

// MapFields - CRITICAL: Extract and scale properly
func (m *UltraMapper) MapFields(sourceID string, data map[string]interface{}) (map[string]interface{}, error) {
	m.mu.RLock()
	mapping, exists := m.mappings[sourceID]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("mapping not found: %s", sourceID)
	}

	// Extract data source
	dataSource := data
	if mapping.NestedPath != "" {
		if nested, ok := data[mapping.NestedPath].(map[string]interface{}); ok {
			dataSource = nested
		}
	}

	result := make(map[string]interface{}, len(mapping.Specs))

	// Process each field spec
	for _, spec := range mapping.Specs {
		// Get raw value from data
		rawValue, exists := dataSource[spec.Source]

		// Try fallback to root if nested path didn't work
		if !exists && mapping.NestedPath != "" {
			rawValue, exists = data[spec.Source]
		}

		// Handle missing values
		if !exists {
			if spec.DefaultVal != nil {
				result[spec.Target] = spec.DefaultVal
			}
			continue
		}

		// Process value based on type
		if spec.IsString {
			// String fields: no scaling
			if str, ok := rawValue.(string); ok {
				result[spec.Target] = str
			} else {
				result[spec.Target] = fmt.Sprintf("%v", rawValue)
			}
		} else {
			// Numeric fields: scale them
			numVal := toFloat64Strict(rawValue)
			scaledVal := numVal * spec.ScaleFactor

			// Store as float64 (will be converted to appropriate type later)
			result[spec.Target] = scaledVal
		}
	}

	return result, nil
}

// toFloat64Strict - Safely convert various types to float64
func toFloat64Strict(val interface{}) float64 {
	if val == nil {
		return 0
	}

	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	default:
		return 0
	}
}

func (m *UltraMapper) seedUltraDefaults() error {
	defaults := []domain.DataSourceMapping{
		{
			SourceID:    "Inv",
			Description: "FoxESS long-key format with integer scaling",
			NestedPath:  "data",
			Mappings: []domain.FieldMapping{
				// IDENTIFICATION (strings, no scaling)
				{SourceField: "slaveid", StandardField: "slave_id", DataType: "string"},
				{SourceField: "serialno", StandardField: "serial_no", DataType: "string", Required: true},
				{SourceField: "modelname", StandardField: "model_name", DataType: "string"},

				// POWER & ENERGY (integers scaled by divide 1000)
				{SourceField: "totaloutputpower", StandardField: "total_output_power", DataType: "int", Transform: "divide:1000"},
				{SourceField: "todaye", StandardField: "today_e", DataType: "int", Transform: "divide:1000"},
				{SourceField: "totale", StandardField: "total_e", DataType: "int", Transform: "divide:1000"},

				// PV VOLTAGES (integers scaled by divide 100, stored as centivolts)
				{SourceField: "pv1voltage", StandardField: "pv1_voltage", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv1current", StandardField: "pv1_current", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv2voltage", StandardField: "pv2_voltage", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv2current", StandardField: "pv2_current", DataType: "int", Transform: "divide:100"},
				{SourceField: "pv3voltage", StandardField: "pv3_voltage", DataType: "int", Transform: "divide:100", DefaultValue: 0},
				{SourceField: "pv3current", StandardField: "pv3_current", DataType: "int", Transform: "divide:100", DefaultValue: 0},
				{SourceField: "pv4voltage", StandardField: "pv4_voltage", DataType: "int", Transform: "divide:100", DefaultValue: 0},
				{SourceField: "pv4current", StandardField: "pv4_current", DataType: "int", Transform: "divide:100", DefaultValue: 0},

				// GRID VOLTAGES (divide by 100)
				{SourceField: "gridvoltager", StandardField: "grid_voltage_r", DataType: "int", Transform: "divide:100"},
				{SourceField: "gridvoltages", StandardField: "grid_voltage_s", DataType: "int", Transform: "divide:100"},
				{SourceField: "gridvoltaget", StandardField: "grid_voltage_t", DataType: "int", Transform: "divide:100"},

				// GRID CURRENTS (divide by 1000)
				{SourceField: "gridcurrentr", StandardField: "grid_current_r", DataType: "int", Transform: "divide:1000"},
				{SourceField: "gridcurrents", StandardField: "grid_current_s", DataType: "int", Transform: "divide:1000"},
				{SourceField: "gridcurrentt", StandardField: "grid_current_t", DataType: "int", Transform: "divide:1000"},

				// TEMPERATURE & FREQUENCY
				{SourceField: "invertertemp", StandardField: "inverter_temp", DataType: "int", Transform: "divide:10"},
				{SourceField: "frequency", StandardField: "frequency", DataType: "int", Transform: "divide:1000"},

				// ALARMS (no scaling)
				{SourceField: "alarm1", StandardField: "alarm1", DataType: "int", DefaultValue: 0},
				{SourceField: "alarm2", StandardField: "alarm2", DataType: "int", DefaultValue: 0},
				{SourceField: "alarm3", StandardField: "alarm3", DataType: "int", DefaultValue: 0},
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
			logger.Info(fmt.Sprintf("✓ Seeded mapping: %s", mapping.SourceID))
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
