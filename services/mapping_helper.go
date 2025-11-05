package services

import (
	"fmt"
	"os"
	"strings"
	"solar_project/models"
)

// ApplyTransform applies mathematical transformations to values
func ApplyTransform(value interface{}, transform, dataType string) interface{} {
	typed := ConvertType(value, dataType)

	if transform == "" {
		return typed
	}

	parts := strings.Split(transform, ":")
	if len(parts) != 2 {
		return typed
	}

	var factor float64
	fmt.Sscanf(parts[1], "%f", &factor)

	numValue := toFloat64(typed)
	if numValue == nil {
		return typed
	}

	result := calculate(parts[0], *numValue, factor)
	
	if dataType == "int" {
		return int(result)
	}
	return result
}

// calculate performs the arithmetic operation
func calculate(op string, value, factor float64) float64 {
	switch op {
	case "multiply":
		return value * factor
	case "divide":
		if factor != 0 {
			return value / factor
		}
		return value
	case "add":
		return value + factor
	case "subtract":
		return value - factor
	default:
		return value
	}
}

// toFloat64 converts any numeric type to float64
func toFloat64(value interface{}) *float64 {
	var result float64
	
	switch v := value.(type) {
	case int:
		result = float64(v)
	case int64:
		result = float64(v)
	case float64:
		result = v
	case float32:
		result = float64(v)
	default:
		return nil
	}
	
	return &result
}

// ConvertType converts value to specified data type
func ConvertType(value interface{}, dataType string) interface{} {
	switch dataType {
	case "int":
		return toInt(value)
	case "float":
		return toFloat(value)
	case "string":
		return toString(value)
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
	case string:
		var i int
		fmt.Sscanf(v, "%d", &i)
		return i
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
	case string:
		var f float64
		fmt.Sscanf(v, "%f", &f)
		return f
	default:
		return 0.0
	}
}

func toString(value interface{}) string {
	return fmt.Sprintf("%v", value)
}

func toBool(value interface{}) bool {
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
}

// ValidateMapping checks if a mapping is valid
func ValidateMapping(mapping *models.DataSourceMapping) error {
	if mapping.SourceID == "" {
		return fmt.Errorf("source_id is required")
	}

	if len(mapping.Mappings) == 0 {
		return fmt.Errorf("at least one field mapping is required")
	}

	validTypes := map[string]bool{
		"int": true, "float": true, "string": true, "bool": true,
	}

	for i, field := range mapping.Mappings {
		if field.SourceField == "" {
			return fmt.Errorf("mapping[%d]: source_field required", i)
		}
		if field.StandardField == "" {
			return fmt.Errorf("mapping[%d]: standard_field required", i)
		}
		if !validTypes[field.DataType] {
			return fmt.Errorf("mapping[%d]: invalid data_type '%s'", i, field.DataType)
		}
	}

	return nil
}

// getEnv gets environment variable with default
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}