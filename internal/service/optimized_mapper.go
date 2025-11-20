// internal/service/optimized_mapper.go
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

// Pre-compiled scale factors for common measurements
const (
	// Power measurements (milliwatts to watts)
	ScalePower = 0.001 // Divide by 1000
	
	// Energy measurements (Wh with decimal precision)
	ScaleEnergy = 0.01 // Divide by 100
	
	// Voltage measurements (decivolts to volts)
	ScaleVoltage = 0.1 // Divide by 10
	
	// Current measurements (milliamps to amps)
	ScaleCurrent = 0.001 // Divide by 1000
	
	// Temperature (0.1°C to °C)
	ScaleTemperature = 0.1 // Divide by 10
	
	// Frequency (0.01 Hz to Hz)
	ScaleFrequency = 0.01 // Divide by 100
)

// FieldSpec defines a field with pre-compiled transformation
type FieldSpec struct {
	Source      string
	Target      string
	ScaleFactor float64 // Pre-calculated multiplier
	Required    bool
	DefaultVal  interface{}
}

// CompiledMapping is a performance-optimized mapping
type CompiledMapping struct {
	SourceID    string
	NestedPath  string
	Fields      []FieldSpec
	FieldIndex  map[string]int // Quick lookup by source field
	RequiredSet map[string]bool
}

// OptimizedMapper replaces the original mapper with performance optimizations
type OptimizedMapper struct {
	mu         sync.RWMutex
	mappings   map[string]*CompiledMapping
	collection *mongo.Collection
	
	// Performance: Pre-allocated buffers
	keyBuffer    []string
	resultBuffer map[string]interface{}
	
	stopReload chan struct{}
}

// NewOptimizedMapper creates a high-performance mapper
func NewOptimizedMapper(db *config.MongoDatabase) (*OptimizedMapper, error) {
	m := &OptimizedMapper{
		mappings:     make(map[string]*CompiledMapping),
		collection:   db.Database.Collection("mappings"),
		keyBuffer:    make([]string, 0, 50), // Pre-allocate for typical payload
		resultBuffer: make(map[string]interface{}, 20),
		stopReload:   make(chan struct{}),
	}

	// Create index
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	indexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "source_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	}
	m.collection.Indexes().CreateOne(ctx, indexModel)

	// Load and compile mappings
	if err := m.Load(); err != nil {
		return nil, err
	}

	// Seed optimized defaults
	if len(m.mappings) == 0 {
		if err := m.seedOptimizedDefaults(); err != nil {
			return nil, err
		}
	}

	// Start auto-reload
	go m.autoReload()

	logger.Info(fmt.Sprintf("OptimizedMapper initialized with %d compiled mappings", len(m.mappings)))
	return m, nil
}

// Load and compile mappings
func (m *OptimizedMapper) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cursor, err := m.collection.Find(ctx, bson.M{"active": true})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	newMappings := make(map[string]*CompiledMapping)
	for cursor.Next(ctx) {
		var mapping domain.DataSourceMapping
		if err := cursor.Decode(&mapping); err != nil {
			continue
		}
		
		// Compile into optimized format
		compiled := m.compile(&mapping)
		newMappings[mapping.SourceID] = compiled
	}

	m.mappings = newMappings
	return nil
}

// compile converts domain mapping to optimized format
func (m *OptimizedMapper) compile(src *domain.DataSourceMapping) *CompiledMapping {
	compiled := &CompiledMapping{
		SourceID:    src.SourceID,
		NestedPath:  src.NestedPath,
		Fields:      make([]FieldSpec, len(src.Mappings)),
		FieldIndex:  make(map[string]int, len(src.Mappings)),
		RequiredSet: make(map[string]bool),
	}

	for i, field := range src.Mappings {
		spec := FieldSpec{
			Source:     field.SourceField,
			Target:     field.StandardField,
			Required:   field.Required,
			DefaultVal: field.DefaultValue,
		}

		// Pre-calculate scale factor from transform string
		spec.ScaleFactor = parseScaleFactor(field.Transform)

		compiled.Fields[i] = spec
		compiled.FieldIndex[field.SourceField] = i
		
		if field.Required {
			compiled.RequiredSet[field.SourceField] = true
		}
	}

	return compiled
}

// // parseScaleFactor converts transform string to float multiplier
// func parseScaleFactor(transform string) float64 {
// 	if transform == "" {
// 		return 1.0
// 	}

// 	var operation string
// 	var value float64
	
// 	// Parse "multiply:1000" or "divide:100"
// 	if n, _ := fmt.Sscanf(transform, "%s:%f", &operation, &value); n != 2 {
// 		return 1.0
// 	}

// 	switch operation {
// 	case "multiply":
// 		return value
// 	case "divide":
// 		if value != 0 {
// 			return 1.0 / value
// 		}
// 	case "add":
// 		// Store as negative multiplier for addition (handled in apply)
// 		return -value
// 	case "subtract":
// 		return -value
// 	}

// 	return 1.0
// }

// DetectSourceID - Optimized with pre-allocated buffers
func (m *OptimizedMapper) DetectSourceID(data map[string]interface{}) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Fast path: Check explicit hints
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

	// Extract keys once (reuse buffer)
	m.keyBuffer = m.keyBuffer[:0] // Reset without reallocation
	m.extractKeysToBuffer(data, &m.keyBuffer)

	// Find best match using pre-computed required sets
	bestMatch := ""
	bestScore := 0

	for sourceID, mapping := range m.mappings {
		// Fast required check
		allRequired := true
		for reqField := range mapping.RequiredSet {
			if !containsString(m.keyBuffer, reqField) {
				allRequired = false
				break
			}
		}
		
		if !allRequired {
			continue
		}

		// Count matching fields
		score := 0
		for _, field := range mapping.Fields {
			if containsString(m.keyBuffer, field.Source) {
				score++
			}
		}

		// 40% threshold
		threshold := len(mapping.Fields) * 2 / 5 // Integer division
		if score > bestScore && score >= threshold {
			bestScore = score
			bestMatch = sourceID
		}
	}

	return bestMatch
}

// MapFields - High-performance mapping with pre-compiled scale factors
func (m *OptimizedMapper) MapFields(sourceID string, data map[string]interface{}) (map[string]interface{}, error) {
	m.mu.RLock()
	mapping, exists := m.mappings[sourceID]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("mapping not found: %s", sourceID)
	}

	// Reuse result buffer
	for k := range m.resultBuffer {
		delete(m.resultBuffer, k)
	}

	// Get data source
	dataSource := data
	if mapping.NestedPath != "" {
		if nested, ok := data[mapping.NestedPath].(map[string]interface{}); ok {
			dataSource = nested
		}
	}

	// Apply all field specs
	for _, spec := range mapping.Fields {
		value, exists := dataSource[spec.Source]
		
		// Fallback to root
		if !exists && mapping.NestedPath != "" {
			value, exists = data[spec.Source]
		}

		// Handle missing
		if !exists {
			if spec.DefaultVal != nil {
				value = spec.DefaultVal
			} else if spec.Required {
				return nil, fmt.Errorf("required field missing: %s", spec.Source)
			} else {
				continue
			}
		}

		// Apply scale factor (fast path for common case)
		if spec.ScaleFactor != 1.0 {
			value = applyScale(value, spec.ScaleFactor)
		}

		m.resultBuffer[spec.Target] = value
	}

	// Copy result (caller owns the map)
	result := make(map[string]interface{}, len(m.resultBuffer))
	for k, v := range m.resultBuffer {
		result[k] = v
	}

	return result, nil
}

// applyScale - Optimized scaling without interface{} allocations
func applyScale(value interface{}, scale float64) interface{} {
	switch v := value.(type) {
	case int:
		return int(float64(v) * scale)
	case int64:
		return int64(float64(v) * scale)
	case float64:
		return v * scale
	case float32:
		return float32(float64(v) * scale)
	default:
		return value
	}
}

// extractKeysToBuffer - Zero-allocation key extraction
func (m *OptimizedMapper) extractKeysToBuffer(data map[string]interface{}, buf *[]string) {
	for key, value := range data {
		*buf = append(*buf, key)
		if nested, ok := value.(map[string]interface{}); ok {
			for nk := range nested {
				*buf = append(*buf, nk)
			}
		}
	}
}

// containsString - Inline check without allocations
func containsString(slice []string, item string) bool {
	for i := range slice {
		if slice[i] == item {
			return true
		}
	}
	return false
}

// seedOptimizedDefaults - FoxESS mapping with proper scale factors
func (m *OptimizedMapper) seedOptimizedDefaults() error {
	defaults := []domain.DataSourceMapping{
		{
			SourceID:    "current_format",
			Description: "Legacy format",
			NestedPath:  "data",
			Mappings: []domain.FieldMapping{
				{SourceField: "serial_no", StandardField: "serial_no", DataType: "string", DefaultValue: "UNKNOWN"},
				{SourceField: "s1v", StandardField: "voltage", DataType: "int", Transform: "multiply:0.1"},
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
			SourceID:    "Inv", // Your new FoxESS format
			Description: "FoxESS Inverter with scaled measurements",
			NestedPath:  "data",
			Mappings: []domain.FieldMapping{
				// Identification
				{SourceField: "sno", StandardField: "serial_no", DataType: "string", Required: true},
				{SourceField: "model", StandardField: "model", DataType: "string"},
				
				// Power (milliwatts to watts)
				{SourceField: "p", StandardField: "power", DataType: "int", Transform: "multiply:0.001"},
				
				// Energy (in 0.01 kWh units)
				{SourceField: "e", StandardField: "today_energy", DataType: "int", Transform: "multiply:0.01"},
				{SourceField: "te", StandardField: "total_energy", DataType: "int", Transform: "multiply:0.01"},
				
				// PV Voltages (decivolts to volts)
				{SourceField: "pv1v", StandardField: "pv1_voltage", DataType: "int", Transform: "multiply:0.1"},
				{SourceField: "pv2v", StandardField: "pv2_voltage", DataType: "int", Transform: "multiply:0.1"},
				{SourceField: "pv3v", StandardField: "pv3_voltage", DataType: "int", Transform: "multiply:0.1"},
				{SourceField: "pv4v", StandardField: "pv4_voltage", DataType: "int", Transform: "multiply:0.1"},
				
				// PV Currents (milliamps to amps)
				{SourceField: "pv1c", StandardField: "pv1_current", DataType: "int", Transform: "multiply:0.001"},
				{SourceField: "pv2c", StandardField: "pv2_current", DataType: "int", Transform: "multiply:0.001"},
				{SourceField: "pv3c", StandardField: "pv3_current", DataType: "int", Transform: "multiply:0.001"},
				{SourceField: "pv4c", StandardField: "pv4_current", DataType: "int", Transform: "multiply:0.001"},
				
				// Grid Voltages (decivolts to volts)
				{SourceField: "gvr", StandardField: "grid_voltage_r", DataType: "int", Transform: "multiply:0.1"},
				{SourceField: "gvs", StandardField: "grid_voltage_s", DataType: "int", Transform: "multiply:0.1"},
				{SourceField: "gvt", StandardField: "grid_voltage_t", DataType: "int", Transform: "multiply:0.1"},
				
				// Grid Currents (milliamps to amps)
				{SourceField: "gcr", StandardField: "grid_current_r", DataType: "int", Transform: "multiply:0.001"},
				{SourceField: "gcs", StandardField: "grid_current_s", DataType: "int", Transform: "multiply:0.001"},
				{SourceField: "gct", StandardField: "grid_current_t", DataType: "int", Transform: "multiply:0.001"},
				
				// Temperature (0.1°C to °C)
				{SourceField: "itmp", StandardField: "temperature", DataType: "int", Transform: "multiply:0.1"},
				
				// Frequency (0.01 Hz to Hz)
				{SourceField: "fr", StandardField: "frequency", DataType: "int", Transform: "multiply:0.01"},
				
				// Alarms/Faults
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
		// Check if exists
		count, _ := m.collection.CountDocuments(ctx, bson.M{"source_id": mapping.SourceID})
		if count == 0 {
			m.collection.InsertOne(ctx, mapping)
		}
	}

	return m.Load()
}

func (m *OptimizedMapper) autoReload() {
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

func (m *OptimizedMapper) Close() {
	close(m.stopReload)
}