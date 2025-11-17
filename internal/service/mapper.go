package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"solar_project/internal/config"
	"solar_project/internal/domain"
	"solar_project/pkg/logger"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Mapper handles field mapping transformations
type Mapper struct {
	mu         sync.RWMutex
	mappings   map[string]*domain.DataSourceMapping
	collection *mongo.Collection
	stopReload chan struct{}
}

// NewMapper creates a new mapper instance
func NewMapper(db *config.MongoDatabase) (*Mapper, error) {
	m := &Mapper{
		mappings:   make(map[string]*domain.DataSourceMapping),
		collection: db.Database.Collection("mappings"),
		stopReload: make(chan struct{}),
	}

	// Create index
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	indexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "source_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	}
	m.collection.Indexes().CreateOne(ctx, indexModel)

	// Load mappings
	if err := m.Load(); err != nil {
		return nil, err
	}

	// Seed defaults if empty
	if len(m.mappings) == 0 {
		if err := m.seedDefaults(); err != nil {
			return nil, err
		}
	}

	// Start auto-reload
	go m.autoReload()

	logger.Info(fmt.Sprintf("Mapper initialized with %d mappings", len(m.mappings)))
	return m, nil
}

// Load reads all active mappings from database
func (m *Mapper) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cursor, err := m.collection.Find(ctx, bson.M{"active": true})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	newMappings := make(map[string]*domain.DataSourceMapping)
	for cursor.Next(ctx) {
		var mapping domain.DataSourceMapping
		if err := cursor.Decode(&mapping); err != nil {
			continue
		}
		newMappings[mapping.SourceID] = &mapping
	}

	m.mappings = newMappings
	return nil
}

// DetectSourceID identifies data source from raw data
func (m *Mapper) DetectSourceID(data map[string]interface{}) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check explicit source_id or device_type
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

	// Extract all keys
	allKeys := extractKeys(data)
	
	// Find best matching source
	bestMatch := ""
	bestScore := 0

	for sourceID, mapping := range m.mappings {
		score := 0
		requiredFields := []string{}
		
		for _, field := range mapping.Mappings {
			if field.Required {
				requiredFields = append(requiredFields, field.SourceField)
			}
			if contains(allKeys, field.SourceField) {
				score++
			}
		}

		// Check all required fields present
		allRequiredPresent := true
		for _, req := range requiredFields {
			if !contains(allKeys, req) {
				allRequiredPresent = false
				break
			}
		}

		if !allRequiredPresent {
			continue
		}

		// Require 40% field match
		threshold := len(mapping.Mappings) * 40 / 100
		if score > bestScore && score >= threshold {
			bestScore = score
			bestMatch = sourceID
		}
	}

	return bestMatch
}

// MapFields transforms raw data to standardized format
func (m *Mapper) MapFields(sourceID string, data map[string]interface{}) (map[string]interface{}, error) {
	m.mu.RLock()
	mapping, exists := m.mappings[sourceID]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("mapping not found: %s", sourceID)
	}

	result := make(map[string]interface{})

	// Get data source (nested or root)
	dataSource := data
	if mapping.NestedPath != "" {
		if nested, ok := data[mapping.NestedPath].(map[string]interface{}); ok {
			dataSource = nested
		}
	}

	// Map each field
	for _, fieldMap := range mapping.Mappings {
		value, exists := dataSource[fieldMap.SourceField]
		
		// Try root if nested path didn't work
		if !exists && mapping.NestedPath != "" {
			value, exists = data[fieldMap.SourceField]
		}

		// Handle missing fields
		if !exists {
			if fieldMap.DefaultValue != nil {
				value = fieldMap.DefaultValue
			} else if fieldMap.Required {
				return nil, fmt.Errorf("required field missing: %s", fieldMap.SourceField)
			} else {
				continue
			}
		}

		// Transform value
		transformed := m.transform(value, fieldMap.Transform, fieldMap.DataType)
		result[fieldMap.StandardField] = transformed
	}

	return result, nil
}

// transform applies transformations to values
func (m *Mapper) transform(value interface{}, transform, dataType string) interface{} {
	// Convert type first
	typed := convertType(value, dataType)

	// Apply transformation if specified
	if transform == "" {
		return typed
	}

	parts := strings.Split(transform, ":")
	if len(parts) != 2 {
		return typed
	}

	operation := parts[0]
	var factor float64
	fmt.Sscanf(parts[1], "%f", &factor)

	// Convert to float for math
	numValue := toFloat(typed)
	if numValue == 0 && typed != 0 {
		return typed // Conversion failed
	}

	// Perform operation
	var result float64
	switch operation {
	case "multiply":
		result = numValue * factor
	case "divide":
		if factor != 0 {
			result = numValue / factor
		} else {
			result = numValue
		}
	case "add":
		result = numValue + factor
	case "subtract":
		result = numValue - factor
	default:
		return typed
	}

	// Return as int if original was int
	if dataType == "int" {
		return int(result)
	}
	return result
}

// CRUD operations
func (m *Mapper) Create(mapping *domain.DataSourceMapping) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mapping.Active = true
	mapping.CreatedAt = time.Now()
	mapping.UpdatedAt = time.Now()

	_, err := m.collection.InsertOne(ctx, mapping)
	if err != nil {
		return err
	}

	m.Load()
	return nil
}

func (m *Mapper) Update(sourceID string, mapping *domain.DataSourceMapping) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mapping.UpdatedAt = time.Now()
	filter := bson.M{"source_id": sourceID}
	update := bson.M{"$set": mapping}

	_, err := m.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	m.Load()
	return nil
}

func (m *Mapper) Delete(sourceID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{"source_id": sourceID}
	update := bson.M{"$set": bson.M{"active": false, "updated_at": time.Now()}}

	_, err := m.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	m.Load()
	return nil
}

func (m *Mapper) GetAll() map[string]*domain.DataSourceMapping {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*domain.DataSourceMapping)
	for k, v := range m.mappings {
		result[k] = v
	}
	return result
}

// autoReload periodically reloads mappings
func (m *Mapper) autoReload() {
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

func (m *Mapper) Close() {
	close(m.stopReload)
}

// seedDefaults creates default mappings
func (m *Mapper) seedDefaults() error {
	defaults := []domain.DataSourceMapping{
		{
			SourceID:    "current_format",
			Description: "Default format",
			NestedPath:  "data",
			Mappings: []domain.FieldMapping{
				{SourceField: "serial_no", StandardField: "serial_no", DataType: "string", DefaultValue: "UNKNOWN"},
				{SourceField: "s1v", StandardField: "voltage", DataType: "int", DefaultValue: 0},
				{SourceField: "total_output_power", StandardField: "power", DataType: "int", DefaultValue: 0},
				{SourceField: "f", StandardField: "frequency", DataType: "int", DefaultValue: 0},
				{SourceField: "today_e", StandardField: "today_energy", DataType: "int", DefaultValue: 0},
				{SourceField: "total_e", StandardField: "total_energy", DataType: "int", DefaultValue: 0},
				{SourceField: "inv_temp", StandardField: "temperature", DataType: "int", DefaultValue: 0},
				{SourceField: "fault_code", StandardField: "fault_code", DataType: "int", DefaultValue: 0},
			},
			Active:    true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	for _, mapping := range defaults {
		m.Create(&mapping)
	}

	return nil
}

// Helper functions
func extractKeys(data map[string]interface{}) []string {
	keys := make([]string, 0, len(data)*2)
	for key, value := range data {
		keys = append(keys, key)
		if nested, ok := value.(map[string]interface{}); ok {
			for nk := range nested {
				keys = append(keys, nk)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func convertType(value interface{}, dataType string) interface{} {
	switch dataType {
	case "int":
		return toInt(value)
	case "float":
		return toFloat(value)
	case "string":
		return fmt.Sprintf("%v", value)
	case "bool":
		return toBool(value)
	default:
		return value
	}
}

func toInt(value interface{}) int {
	switch v := value.(type) {
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

func toFloat(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0.0
	}
}

func toBool(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case string:
		return v == "true" || v == "1"
	default:
		return false
	}
}