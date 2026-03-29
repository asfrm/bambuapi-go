// Package util provides utility functions for type conversions and common operations.
package util

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ToInt converts an interface{} to int with a default value.
// It handles int, int64, float64, string, and json.Number types.
func ToInt(val interface{}, defaultValue int) int {
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case float64:
		return int(v)
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return int(parsed)
		}
	}
	return defaultValue
}

// ToInt64 converts an interface{} to int64 with a default value.
func ToInt64(val interface{}, defaultValue int64) int64 {
	switch v := val.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			return parsed
		}
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// ToFloat64 converts an interface{} to float64 with a default value.
func ToFloat64(val interface{}, defaultValue float64) float64 {
	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	case string:
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed
		}
	case json.Number:
		if parsed, err := v.Float64(); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// ToString converts an interface{} to string with a default value.
func ToString(val interface{}, defaultValue string) string {
	switch v := val.(type) {
	case string:
		return v
	case nil:
		return defaultValue
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ToBool converts an interface{} to bool with a default value.
func ToBool(val interface{}, defaultValue bool) bool {
	switch v := val.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case string:
		return v == "true" || v == "1" || v == "yes"
	}
	return defaultValue
}

// ToMapStringInterface safely converts an interface{} to map[string]interface{}.
func ToMapStringInterface(val interface{}) (map[string]interface{}, bool) {
	m, ok := val.(map[string]interface{})
	return m, ok
}

// ToSliceInterface safely converts an interface{} to []interface{}.
func ToSliceInterface(val interface{}) ([]interface{}, bool) {
	s, ok := val.([]interface{})
	return s, ok
}

// ClampInt clamps an integer value between min and max.
func ClampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ClampFloat64 clamps a float64 value between min and max.
func ClampFloat64(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
