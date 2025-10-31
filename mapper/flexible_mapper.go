package mapper

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// StandardField represents the normalized field name in your system
type StandardField string

const (
	// Device Info
	DeviceType     StandardField = "device_type"
	DeviceName     StandardField = "device_name"
	DeviceID       StandardField = "device_id"
	SerialNo       StandardField = "serial_no"
	SignalStrength StandardField = "signal_strength"
	
	// Electrical Measurements
	Voltage          StandardField = "voltage"
	Current          StandardField = "current"
	Power            StandardField = "power"
	Frequency        StandardField = "frequency"
	Temperature      StandardField = "temperature"
	
	// Energy Measurements
	TodayEnergy StandardField = "today_energy"
	TotalEnergy StandardField = "total_energy"
	
	// Status
	FaultCode StandardField = "fault_code"
	
	// Time
	Timestamp   StandardField = "timestamp"
	Date        StandardField = "date"
	Time        StandardField = "time"
)

// FieldMapping maps source field names to standard field names
type FieldMapping struct {
	SourceField   string        `json:"source_field"`   // Original field name
	StandardField StandardField `json:"standard_field"` // Normalized field name
	DataType      string        `json:"data_type"`      // int, float, string, bool
	DefaultValue  interface{}   `json:"default_value"`  // Used if field missing
	Transform     string        `json:"transform"`      // Optional transformations
	Required      bool          `json:"required"`       // Is this field mandatory?
}

// DataSourceMapping contains all mappings for a specific data source
type DataSourceMapping struct {
	SourceID     string         `json:"source_id"`
	Description  string         `json:"description"`
	NestedPath   string         `json:"nested_path"`    // e.g., "data" or "" for flat
	Mappings     []FieldMapping `json:"mappings"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// FlexibleMapper handles any JSON structure with hot-reload capability
type FlexibleMapper struct {
	mu              sync.RWMutex
	mappings        map[string]*DataSourceMapping
	configPath      string
	autoReloadWatch bool
	lastReload      time.Time
}

// NewFlexibleMapper creates a new mapper instance
func NewFlexibleMapper(configPath string, autoReload bool) *FlexibleMapper {
	fm := &FlexibleMapper{
		mappings:        make(map[string]*DataSourceMapping),
		configPath:      configPath,
		autoReloadWatch: autoReload,
		lastReload:      time.Now(),
	}

	fm.LoadMappings()

	if autoReload {
		go fm.watchConfigFile()
	}

	return fm
}

// LoadMappings loads field mappings from JSON config file
func (fm *FlexibleMapper) LoadMappings() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	data, err := os.ReadFile(fm.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var sources []DataSourceMapping
	if err := json.Unmarshal(data, &sources); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	for i := range sources {
		fm.mappings[sources[i].SourceID] = &sources[i]
	}

	fm.lastReload = time.Now()
	fmt.Printf("✅ Loaded %d data source mappings at %s\n", len(sources), time.Now().Format("15:04:05"))
	
	return nil
}

// watchConfigFile monitors config file for changes and auto-reloads
func (fm *FlexibleMapper) watchConfigFile() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastModTime time.Time
	fileInfo, err := os.Stat(fm.configPath)
	if err == nil {
		lastModTime = fileInfo.ModTime()
	}

	for range ticker.C {
		fileInfo, err := os.Stat(fm.configPath)
		if err != nil {
			continue
		}

		if fileInfo.ModTime().After(lastModTime) {
			fmt.Println("🔄 Config file changed, reloading mappings...")
			if err := fm.LoadMappings(); err != nil {
				fmt.Printf("❌ Failed to reload: %v\n", err)
			}
			lastModTime = fileInfo.ModTime()
		}
	}
}

// MapFields transforms raw JSON data to standardized format
// Handles nested objects, flat structures, missing fields, etc.
func (fm *FlexibleMapper) MapFields(sourceID string, rawData map[string]interface{}) (map[string]interface{}, error) {
	fm.mu.RLock()
	mapping, exists := fm.mappings[sourceID]
	fm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no mapping found for source: %s", sourceID)
	}

	result := make(map[string]interface{})

	// Determine where to look for data
	var dataSource map[string]interface{}
	
	if mapping.NestedPath != "" {
		// Look inside nested object (e.g., "data" field)
		if nested, ok := rawData[mapping.NestedPath].(map[string]interface{}); ok {
			dataSource = nested
		} else {
			dataSource = rawData // Fallback to root if nested path doesn't exist
		}
	} else {
		// Flat structure - use root
		dataSource = rawData
	}

	// Also keep root data accessible for fields outside nested path
	for _, fieldMap := range mapping.Mappings {
		var value interface{}
		var exists bool

		// Try to get from nested path first
		value, exists = dataSource[fieldMap.SourceField]
		
		// If not found and we have nested path, try root level
		if !exists && mapping.NestedPath != "" {
			value, exists = rawData[fieldMap.SourceField]
		}

		// If still not found, use default value
		if !exists {
			if fieldMap.DefaultValue != nil {
				value = fieldMap.DefaultValue
			} else if fieldMap.Required {
				return nil, fmt.Errorf("required field '%s' is missing", fieldMap.SourceField)
			} else {
				// Skip optional missing fields
				continue
			}
		}

		// Apply transformations if specified
		transformedValue := fm.applyTransform(value, fieldMap.Transform, fieldMap.DataType)

		// Store with standard field name
		result[string(fieldMap.StandardField)] = transformedValue
	}

	return result, nil
}

// applyTransform applies mathematical transformations and type conversions
func (fm *FlexibleMapper) applyTransform(value interface{}, transform string, dataType string) interface{} {
	// First, convert to appropriate type
	typedValue := fm.convertType(value, dataType)

	// Then apply transformation if specified
	if transform == "" {
		return typedValue
	}

	parts := strings.Split(transform, ":")
	if len(parts) != 2 {
		return typedValue
	}

	operation := parts[0]
	var factor float64
	fmt.Sscanf(parts[1], "%f", &factor)

	// Convert to float64 for math operations
	var numValue float64
	switch v := typedValue.(type) {
	case int:
		numValue = float64(v)
	case int64:
		numValue = float64(v)
	case float64:
		numValue = v
	case float32:
		numValue = float64(v)
	default:
		return typedValue // Can't transform non-numeric
	}

	// Apply operation
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
		return typedValue
	}

	// Return in original type if it was int
	if dataType == "int" {
		return int(result)
	}
	return result
}

// convertType converts value to specified data type
func (fm *FlexibleMapper) convertType(value interface{}, dataType string) interface{} {
	switch dataType {
	case "int":
		switch v := value.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case float32:
			return int(v)
		case string:
			var i int
			fmt.Sscanf(v, "%d", &i)
			return i
		default:
			return 0
		}
	
	case "float":
		switch v := value.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		case string:
			var f float64
			fmt.Sscanf(v, "%f", &f)
			return f
		default:
			return 0.0
		}
	
	case "string":
		return fmt.Sprintf("%v", value)
	
	case "bool":
		switch v := value.(type) {
		case bool:
			return v
		case int:
			return v != 0
		case string:
			return v == "true" || v == "1" || v == "yes"
		default:
			return false
		}
	
	default:
		return value // Return as-is if type not specified
	}
}

// AddMapping adds a new mapping at runtime
func (fm *FlexibleMapper) AddMapping(mapping *DataSourceMapping) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	mapping.UpdatedAt = time.Now()
	if mapping.CreatedAt.IsZero() {
		mapping.CreatedAt = time.Now()
	}

	fm.mappings[mapping.SourceID] = mapping
	return fm.saveMappingsToFile()
}

// UpdateMapping updates an existing mapping
func (fm *FlexibleMapper) UpdateMapping(sourceID string, mapping *DataSourceMapping) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if _, exists := fm.mappings[sourceID]; !exists {
		return fmt.Errorf("mapping not found: %s", sourceID)
	}

	mapping.UpdatedAt = time.Now()
	fm.mappings[sourceID] = mapping
	return fm.saveMappingsToFile()
}

// DeleteMapping removes a mapping
func (fm *FlexibleMapper) DeleteMapping(sourceID string) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	delete(fm.mappings, sourceID)
	return fm.saveMappingsToFile()
}

// GetAllMappings returns all current mappings
func (fm *FlexibleMapper) GetAllMappings() map[string]*DataSourceMapping {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	result := make(map[string]*DataSourceMapping)
	for k, v := range fm.mappings {
		result[k] = v
	}
	return result
}

// saveMappingsToFile persists current mappings to config file
func (fm *FlexibleMapper) saveMappingsToFile() error {
	sources := make([]DataSourceMapping, 0, len(fm.mappings))
	for _, mapping := range fm.mappings {
		sources = append(sources, *mapping)
	}

	data, err := json.MarshalIndent(sources, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal mappings: %w", err)
	}

	if err := os.WriteFile(fm.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// DetectSourceID attempts to identify data source from raw JSON
func (fm *FlexibleMapper) DetectSourceID(rawData map[string]interface{}) string {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	bestMatch := ""
	bestScore := 0

	for sourceID, mapping := range fm.mappings {
		score := 0
		
		// Check both root and nested data
		dataToCheck := []map[string]interface{}{rawData}
		if mapping.NestedPath != "" {
			if nested, ok := rawData[mapping.NestedPath].(map[string]interface{}); ok {
				dataToCheck = append(dataToCheck, nested)
			}
		}

		for _, fieldMap := range mapping.Mappings {
			for _, data := range dataToCheck {
				if _, exists := data[fieldMap.SourceField]; exists {
					score++
					break
				}
			}
		}

		// Require at least 40% field match (more lenient for missing fields)
		threshold := int(float64(len(mapping.Mappings)) * 0.4)
		if score > bestScore && score >= threshold {
			bestScore = score
			bestMatch = sourceID
		}
	}

	return bestMatch
}