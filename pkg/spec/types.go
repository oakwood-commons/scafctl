// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Type represents the type of a resolved value.
type Type string

const (
	TypeString   Type = "string"
	TypeInt      Type = "int"   // int64 internally
	TypeFloat    Type = "float" // float64
	TypeBool     Type = "bool"
	TypeArray    Type = "array"    // []any (heterogeneous)
	TypeObject   Type = "object"   // map[string]any
	TypeTime     Type = "time"     // time.Time
	TypeDuration Type = "duration" // time.Duration
	TypeAny      Type = "any"
)

// CoerceType attempts to coerce a value to the specified type.
// Returns the coerced value or an error if coercion is not possible.
func CoerceType(value any, targetType Type) (any, error) {
	// Nil values pass through for all types
	if value == nil {
		return nil, nil
	}

	// Handle type aliases first
	targetType = normalizeType(targetType)

	// Any type accepts anything
	if targetType == TypeAny || targetType == "" {
		return value, nil
	}

	switch targetType {
	case TypeString:
		return coerceToString(value)
	case TypeInt:
		return coerceToInt(value)
	case TypeFloat:
		return coerceToFloat(value)
	case TypeBool:
		return coerceToBool(value)
	case TypeArray:
		return coerceToArray(value), nil
	case TypeObject:
		return coerceToObject(value)
	case TypeTime:
		return coerceToTime(value)
	case TypeDuration:
		return coerceToDuration(value)
	case TypeAny:
		return value, nil
	default:
		return nil, fmt.Errorf("unknown type: %s", targetType)
	}
}

// normalizeType converts type aliases to canonical types.
func normalizeType(t Type) Type {
	switch strings.ToLower(string(t)) {
	case "timestamp", "datetime":
		return TypeTime
	case "integer":
		return TypeInt
	case "number":
		return TypeFloat
	case "boolean":
		return TypeBool
	case "map":
		return TypeObject
	default:
		return t
	}
}

// coerceToString converts value to string.
func coerceToString(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case int:
		return strconv.FormatInt(int64(v), 10), nil
	case int8:
		return strconv.FormatInt(int64(v), 10), nil
	case int16:
		return strconv.FormatInt(int64(v), 10), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case float32:
		// Format without scientific notation
		return strconv.FormatFloat(float64(v), 'f', -1, 32), nil
	case float64:
		// Format without scientific notation
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	case time.Time:
		return v.Format(time.RFC3339), nil
	case time.Duration:
		return v.String(), nil
	default:
		return "", fmt.Errorf("cannot coerce %T to string", value)
	}
}

// coerceToInt converts value to int64 (returned as int for compatibility).
func coerceToInt(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		// Check if fits in int (architecture dependent)
		if v > int64(^uint(0)>>1) || v < -int64(^uint(0)>>1)-1 {
			return 0, fmt.Errorf("int64 value %d exceeds int range", v)
		}
		return int(v), nil
	case uint:
		if v > uint(^uint(0)>>1) {
			return 0, fmt.Errorf("uint value %d exceeds int range", v)
		}
		return int(v), nil
	case uint8:
		return int(v), nil
	case uint16:
		return int(v), nil
	case uint32:
		return int(v), nil
	case uint64:
		if v > uint64(^uint(0)>>1) {
			return 0, fmt.Errorf("uint64 value %d exceeds int range", v)
		}
		return int(v), nil
	case float32:
		// Check for decimal part
		if float32(int(v)) != v {
			return 0, fmt.Errorf("float32 value %v has decimal part, cannot coerce to int", v)
		}
		return int(v), nil
	case float64:
		// Check for decimal part
		if float64(int(v)) != v {
			return 0, fmt.Errorf("float64 value %v has decimal part, cannot coerce to int", v)
		}
		// Check range
		if v > float64(^uint(0)>>1) || v < -float64(^uint(0)>>1)-1 {
			return 0, fmt.Errorf("float64 value %v exceeds int range", v)
		}
		return int(v), nil
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot coerce string %q to int: %w", v, err)
		}
		if i > int64(^uint(0)>>1) || i < -int64(^uint(0)>>1)-1 {
			return 0, fmt.Errorf("string value %q exceeds int range", v)
		}
		return int(i), nil
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("cannot coerce %T to int", value)
	}
}

// coerceToFloat converts value to float64.
func coerceToFloat(value any) (float64, error) {
	switch v := value.(type) {
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot coerce string %q to float: %w", v, err)
		}
		return f, nil
	case bool:
		if v {
			return 1.0, nil
		}
		return 0.0, nil
	default:
		return 0, fmt.Errorf("cannot coerce %T to float", value)
	}
}

// coerceToBool converts value to bool.
func coerceToBool(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		// Parse standard bool strings
		b, err := strconv.ParseBool(v)
		if err != nil {
			return false, fmt.Errorf("cannot coerce string %q to bool: %w", v, err)
		}
		return b, nil
	case int, int8, int16, int32, int64:
		// Use reflection to handle all int types
		return reflect.ValueOf(v).Int() != 0, nil
	case uint, uint8, uint16, uint32, uint64:
		return reflect.ValueOf(v).Uint() != 0, nil
	case float32, float64:
		return reflect.ValueOf(v).Float() != 0, nil
	default:
		return false, fmt.Errorf("cannot coerce %T to bool", value)
	}
}

// coerceToObject converts value to map[string]any.
// Accepts map[string]any and map[string]interface{} directly.
// Other map types with string keys are converted.
func coerceToObject(value any) (map[string]any, error) {
	v := reflect.ValueOf(value)

	if v.Kind() == reflect.Map {
		if v.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("cannot coerce %T to object: map key must be string", value)
		}
		result := make(map[string]any, v.Len())
		for _, key := range v.MapKeys() {
			result[key.String()] = v.MapIndex(key).Interface()
		}
		return result, nil
	}

	return nil, fmt.Errorf("cannot coerce %T to object", value)
}

// coerceToArray converts value to array.
// Special handling: non-array values are wrapped in single-element array.
func coerceToArray(value any) []any {
	v := reflect.ValueOf(value)

	// Already an array/slice
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		result := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			result[i] = v.Index(i).Interface()
		}
		return result
	}

	// Wrap non-array value in single-element array
	return []any{value}
}

// coerceToTime converts value to time.Time.
// Supports multiple input formats with automatic detection.
func coerceToTime(value any) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v, nil
	case string:
		// Try multiple formats in order of specificity
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}

		for _, format := range formats {
			if t, err := time.Parse(format, v); err == nil {
				return t, nil
			}
		}

		// Try parsing as Unix timestamp string
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.Unix(i, 0), nil
		}

		return time.Time{}, fmt.Errorf("cannot parse string %q as time (tried RFC3339, ISO8601, date, and Unix timestamp)", v)
	case int:
		// Unix timestamp
		return time.Unix(int64(v), 0), nil
	case int32:
		return time.Unix(int64(v), 0), nil
	case int64:
		return time.Unix(v, 0), nil
	case float64:
		// Unix timestamp with fractional seconds
		sec := int64(v)
		nsec := int64((v - float64(sec)) * 1e9)
		return time.Unix(sec, nsec), nil
	default:
		return time.Time{}, fmt.Errorf("cannot coerce %T to time", value)
	}
}

// coerceToDuration converts value to time.Duration.
func coerceToDuration(value any) (time.Duration, error) {
	switch v := value.(type) {
	case time.Duration:
		return v, nil
	case string:
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0, fmt.Errorf("cannot parse string %q as duration: %w", v, err)
		}
		return d, nil
	case int:
		// Assume nanoseconds
		return time.Duration(v), nil
	case int64:
		return time.Duration(v), nil
	case float64:
		// Assume seconds with fractional part
		return time.Duration(v * float64(time.Second)), nil
	default:
		return 0, fmt.Errorf("cannot coerce %T to duration", value)
	}
}
