package service

import "strconv"

// getFloat retrieves a float64 value from the map, supporting multiple numeric types and strings.
func getFloat(data map[string]interface{}, key string, defaultValue float64) float64 {
	if data == nil {
		return defaultValue
	}

	val, ok := data[key]
	if !ok {
		return defaultValue
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
	case string:
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed
		}
	}

	return defaultValue
}
