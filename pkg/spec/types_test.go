// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCoerceType_NilValues tests that nil values pass through for all types
func TestCoerceType_NilValues(t *testing.T) {
	types := []Type{TypeString, TypeInt, TypeFloat, TypeBool, TypeArray, TypeObject, TypeTime, TypeDuration, TypeAny}

	for _, targetType := range types {
		t.Run(string(targetType), func(t *testing.T) {
			result, err := CoerceType(nil, targetType)
			require.NoError(t, err)
			assert.Nil(t, result)
		})
	}
}

// TestCoerceType_TypeAny tests that any type accepts any value
func TestCoerceType_TypeAny(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{"string", "hello"},
		{"int", 42},
		{"float", 3.14},
		{"bool", true},
		{"array", []any{1, 2, 3}},
		{"struct", struct{ name string }{"test"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceType(tt.value, TypeAny)
			require.NoError(t, err)
			assert.Equal(t, tt.value, result)
		})
	}
}

// TestCoerceType_String tests string coercion
func TestCoerceType_String(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected string
		wantErr  bool
	}{
		{"string_passthrough", "hello", "hello", false},
		{"int", 42, "42", false},
		{"int8", int8(42), "42", false},
		{"int16", int16(42), "42", false},
		{"int32", int32(42), "42", false},
		{"int64", int64(42), "42", false},
		{"uint", uint(42), "42", false},
		{"uint8", uint8(42), "42", false},
		{"uint16", uint16(42), "42", false},
		{"uint32", uint32(42), "42", false},
		{"uint64", uint64(42), "42", false},
		{"float32", float32(3.14), "3.14", false},
		{"float64", 3.14, "3.14", false},
		{"bool_true", true, "true", false},
		{"bool_false", false, "false", false},
		{"time", time.Date(2026, 1, 14, 12, 0, 0, 0, time.UTC), "2026-01-14T12:00:00Z", false},
		{"duration", 5 * time.Minute, "5m0s", false},
		{"struct", struct{}{}, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceType(tt.value, TypeString)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestCoerceType_Int tests int coercion
func TestCoerceType_Int(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected int
		wantErr  bool
	}{
		{"int_passthrough", 42, 42, false},
		{"int8", int8(42), 42, false},
		{"int16", int16(42), 42, false},
		{"int32", int32(42), 42, false},
		{"int64", int64(42), 42, false},
		{"uint", uint(42), 42, false},
		{"uint8", uint8(42), 42, false},
		{"uint16", uint16(42), 42, false},
		{"uint32", uint32(42), 42, false},
		{"uint64", uint64(42), 42, false},
		{"float64_whole", 42.0, 42, false},
		{"float64_decimal", 42.5, 0, true}, // Has decimal part
		{"string_valid", "42", 42, false},
		{"string_invalid", "abc", 0, true},
		{"bool_true", true, 1, false},
		{"bool_false", false, 0, false},
		{"negative", -42, -42, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceType(tt.value, TypeInt)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestCoerceType_Float tests float coercion
func TestCoerceType_Float(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected float64
		wantErr  bool
	}{
		{"float64_passthrough", 3.14, 3.14, false},
		{"float32", float32(3.14), 3.140000104904175, false}, // Float32 precision
		{"int", 42, 42.0, false},
		{"int8", int8(42), 42.0, false},
		{"int16", int16(42), 42.0, false},
		{"int32", int32(42), 42.0, false},
		{"int64", int64(42), 42.0, false},
		{"uint", uint(42), 42.0, false},
		{"string_valid", "3.14", 3.14, false},
		{"string_invalid", "abc", 0, true},
		{"bool_true", true, 1.0, false},
		{"bool_false", false, 0.0, false},
		{"negative", -3.14, -3.14, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceType(tt.value, TypeFloat)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestCoerceType_Bool tests bool coercion
func TestCoerceType_Bool(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected bool
		wantErr  bool
	}{
		{"bool_true", true, true, false},
		{"bool_false", false, false, false},
		{"string_true", "true", true, false},
		{"string_false", "false", false, false},
		{"string_1", "1", true, false},
		{"string_0", "0", false, false},
		{"string_invalid", "yes", false, true},
		{"int_nonzero", 42, true, false},
		{"int_zero", 0, false, false},
		{"int_negative", -1, true, false},
		{"float_nonzero", 3.14, true, false},
		{"float_zero", 0.0, false, false},
		{"uint", uint(1), true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceType(tt.value, TypeBool)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestCoerceType_Array tests array coercion
func TestCoerceType_Array(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected []any
	}{
		{"slice_passthrough", []any{1, 2, 3}, []any{1, 2, 3}},
		{"int_slice", []int{1, 2, 3}, []any{1, 2, 3}},
		{"string_slice", []string{"a", "b"}, []any{"a", "b"}},
		{"empty_slice", []any{}, []any{}},
		{"single_int", 42, []any{42}},              // Wrapped
		{"single_string", "hello", []any{"hello"}}, // Wrapped
		{"single_bool", true, []any{true}},         // Wrapped
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceType(tt.value, TypeArray)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCoerceType_Time tests time coercion
func TestCoerceType_Time(t *testing.T) {
	now := time.Date(2026, 1, 14, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		value    any
		expected time.Time
		wantErr  bool
	}{
		{"time_passthrough", now, now, false},
		{"rfc3339", "2026-01-14T12:00:00Z", now, false},
		{"iso8601", "2026-01-14T12:00:00", time.Date(2026, 1, 14, 12, 0, 0, 0, time.UTC), false},
		{"date_only", "2026-01-14", time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC), false},
		{"unix_timestamp_int", int(1736856000), time.Unix(1736856000, 0), false},
		{"unix_timestamp_int64", int64(1736856000), time.Unix(1736856000, 0), false},
		{"unix_timestamp_float", 1736856000.5, time.Unix(1736856000, 500000000), false},
		{"unix_timestamp_string", "1736856000", time.Unix(1736856000, 0), false},
		{"invalid_string", "not a time", time.Time{}, true},
		{"invalid_type", true, time.Time{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceType(tt.value, TypeTime)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected.Unix(), result.(time.Time).Unix())
			}
		})
	}
}

// TestCoerceType_Duration tests duration coercion
func TestCoerceType_Duration(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected time.Duration
		wantErr  bool
	}{
		{"duration_passthrough", 5 * time.Minute, 5 * time.Minute, false},
		{"string_seconds", "30s", 30 * time.Second, false},
		{"string_minutes", "5m", 5 * time.Minute, false},
		{"string_hours", "2h", 2 * time.Hour, false},
		{"string_complex", "1h30m", 90 * time.Minute, false},
		{"int_nanoseconds", int(1000000), 1000000 * time.Nanosecond, false},
		{"int64_nanoseconds", int64(1000000000), time.Second, false},
		{"float_seconds", 2.5, 2500 * time.Millisecond, false},
		{"invalid_string", "not a duration", 0, true},
		{"invalid_type", true, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceType(tt.value, TypeDuration)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestCoerceType_Object tests object coercion
func TestCoerceType_Object(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected map[string]any
		wantErr  bool
	}{
		{"map_string_any_passthrough", map[string]any{"key": "value"}, map[string]any{"key": "value"}, false},
		{"map_string_interface_passthrough", map[string]interface{}{"a": 1, "b": "two"}, map[string]any{"a": 1, "b": "two"}, false},
		{"nested_map", map[string]any{"outer": map[string]any{"inner": 42}}, map[string]any{"outer": map[string]any{"inner": 42}}, false},
		{"empty_map", map[string]any{}, map[string]any{}, false},
		{"string_value", "not a map", nil, true},
		{"int_value", 42, nil, true},
		{"bool_value", true, nil, true},
		{"array_value", []any{1, 2, 3}, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceType(tt.value, TypeObject)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "cannot coerce")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestCoerceType_Object_MapAlias tests that "map" alias resolves to object type
func TestCoerceType_Object_MapAlias(t *testing.T) {
	value := map[string]any{"key": "value"}
	result, err := CoerceType(value, Type("map"))
	require.NoError(t, err)
	assert.Equal(t, value, result)
}

// TestCoerceType_TypeAliases tests type alias normalization
func TestCoerceType_TypeAliases(t *testing.T) {
	tests := []struct {
		name       string
		alias      Type
		value      any
		expectType string
	}{
		{"timestamp", Type("timestamp"), "2026-01-14T12:00:00Z", "time.Time"},
		{"datetime", Type("datetime"), "2026-01-14T12:00:00Z", "time.Time"},
		{"integer", Type("integer"), "42", "int"},
		{"number", Type("number"), "3.14", "float64"},
		{"boolean", Type("boolean"), "true", "bool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceType(tt.value, tt.alias)
			require.NoError(t, err)
			assert.NotNil(t, result)
		})
	}
}

// TestCoerceType_EmptyType tests that empty type returns value as-is
func TestCoerceType_EmptyType(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{"string", "hello"},
		{"int", 42},
		{"bool", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceType(tt.value, Type(""))
			require.NoError(t, err)
			assert.Equal(t, tt.value, result)
		})
	}
}

// TestCoerceType_UnknownType tests that unknown types return an error
func TestCoerceType_UnknownType(t *testing.T) {
	result, err := CoerceType("hello", Type("unknown"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown type")
	assert.Nil(t, result)
}

// TestType_Constants tests that type constants have expected values
func TestType_Constants(t *testing.T) {
	assert.Equal(t, Type("string"), TypeString)
	assert.Equal(t, Type("int"), TypeInt)
	assert.Equal(t, Type("float"), TypeFloat)
	assert.Equal(t, Type("bool"), TypeBool)
	assert.Equal(t, Type("array"), TypeArray)
	assert.Equal(t, Type("object"), TypeObject)
	assert.Equal(t, Type("time"), TypeTime)
	assert.Equal(t, Type("duration"), TypeDuration)
	assert.Equal(t, Type("any"), TypeAny)
}
