package resolver

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCoerceType_NilValues tests that nil values pass through for all types
func TestCoerceType_NilValues(t *testing.T) {
	types := []Type{TypeString, TypeInt, TypeFloat, TypeBool, TypeArray, TypeTime, TypeDuration, TypeAny}

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

// TestCoerceType_TypeAliases tests type alias normalization
func TestCoerceType_TypeAliases(t *testing.T) {
	tests := []struct {
		name       string
		alias      Type
		value      any
		expectType Type
	}{
		{"timestamp", "timestamp", "2026-01-14T12:00:00Z", TypeTime},
		{"datetime", "datetime", "2026-01-14T12:00:00Z", TypeTime},
		{"integer", "integer", "42", TypeInt},
		{"number", "number", "3.14", TypeFloat},
		{"boolean", "boolean", "true", TypeBool},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceType(tt.value, tt.alias)
			require.NoError(t, err)
			assert.NotNil(t, result)
			// Verify the result matches the expected type
			_, err = CoerceType(result, tt.expectType)
			assert.NoError(t, err)
		})
	}
}

// TestCoerceType_UnknownType tests that unknown types return an error
func TestCoerceType_UnknownType(t *testing.T) {
	result, err := CoerceType("value", Type("unknown"))
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown type")
}

// TestCoerceType_EdgeCases tests edge cases and boundary conditions
func TestCoerceType_EdgeCases(t *testing.T) {
	t.Run("empty_string_to_int", func(t *testing.T) {
		result, err := CoerceType("", TypeInt)
		assert.Error(t, err)
		assert.Equal(t, 0, result)
	})

	t.Run("empty_string_to_float", func(t *testing.T) {
		result, err := CoerceType("", TypeFloat)
		assert.Error(t, err)
		assert.Equal(t, 0.0, result)
	})

	t.Run("large_int64", func(t *testing.T) {
		large := int64(9223372036854775807) // Max int64
		result, err := CoerceType(large, TypeString)
		require.NoError(t, err)
		assert.Equal(t, "9223372036854775807", result)
	})

	t.Run("negative_duration", func(t *testing.T) {
		result, err := CoerceType("-5m", TypeDuration)
		require.NoError(t, err)
		assert.Equal(t, -5*time.Minute, result)
	})

	t.Run("zero_values", func(t *testing.T) {
		tests := []struct {
			targetType Type
			zero       any
		}{
			{TypeInt, 0},
			{TypeFloat, 0.0},
			{TypeBool, false},
			{TypeString, ""},
		}

		for _, tt := range tests {
			result, err := CoerceType(tt.zero, tt.targetType)
			require.NoError(t, err)
			assert.Equal(t, tt.zero, result)
		}
	})
}

// TestCoerceType_Overflow tests integer overflow and boundary conditions
func TestCoerceType_Overflow(t *testing.T) {
	// Test int64 overflow to int (on 64-bit system, max int == max int64)
	// On 32-bit systems these would overflow
	t.Run("int64_max_to_int", func(t *testing.T) {
		maxInt64 := int64(9223372036854775807)
		result, err := CoerceType(maxInt64, TypeInt)
		// On 64-bit systems this should succeed
		if err != nil {
			assert.Contains(t, err.Error(), "exceeds int range")
		} else {
			assert.Equal(t, int(maxInt64), result)
		}
	})

	t.Run("int64_min_to_int", func(t *testing.T) {
		minInt64 := int64(-9223372036854775808)
		result, err := CoerceType(minInt64, TypeInt)
		// On 64-bit systems this should succeed
		if err != nil {
			assert.Contains(t, err.Error(), "exceeds int range")
		} else {
			assert.Equal(t, int(minInt64), result)
		}
	})

	t.Run("uint64_max_to_int_overflow", func(t *testing.T) {
		maxUint64 := uint64(18446744073709551615)
		result, err := CoerceType(maxUint64, TypeInt)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds int range")
		assert.Equal(t, 0, result)
	})

	t.Run("large_uint_to_int_overflow", func(t *testing.T) {
		// Value that exceeds max signed int
		largeUint := uint64(9223372036854775808) // max int64 + 1
		result, err := CoerceType(largeUint, TypeInt)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds int range")
		assert.Equal(t, 0, result)
	})

	t.Run("float64_exceeds_int_range", func(t *testing.T) {
		largeFloat := float64(1e20) // Much larger than max int64
		result, err := CoerceType(largeFloat, TypeInt)
		assert.Error(t, err)
		// Error message may mention decimal part or range depending on how it's checked
		assert.True(t, err != nil, "should error for large float")
		assert.Equal(t, 0, result)
	})

	t.Run("negative_float_exceeds_int_range", func(t *testing.T) {
		largeNegFloat := float64(-1e20)
		result, err := CoerceType(largeNegFloat, TypeInt)
		assert.Error(t, err)
		// Error message may mention decimal part or range depending on how it's checked
		assert.True(t, err != nil, "should error for large negative float")
		assert.Equal(t, 0, result)
	})

	t.Run("string_exceeds_int64_range", func(t *testing.T) {
		hugeNum := "99999999999999999999999999999999"
		result, err := CoerceType(hugeNum, TypeInt)
		assert.Error(t, err)
		assert.Equal(t, 0, result)
	})

	t.Run("string_negative_exceeds_int64_range", func(t *testing.T) {
		hugeNegNum := "-99999999999999999999999999999999"
		result, err := CoerceType(hugeNegNum, TypeInt)
		assert.Error(t, err)
		assert.Equal(t, 0, result)
	})
}

// TestCoerceType_PrecisionLoss tests floating-point precision edge cases
func TestCoerceType_PrecisionLoss(t *testing.T) {
	t.Run("float64_with_decimal_to_int_fails", func(t *testing.T) {
		result, err := CoerceType(42.5, TypeInt)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decimal part")
		assert.Equal(t, 0, result)
	})

	t.Run("float32_with_decimal_to_int_fails", func(t *testing.T) {
		result, err := CoerceType(float32(42.5), TypeInt)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decimal part")
		assert.Equal(t, 0, result)
	})

	t.Run("small_decimal_to_int_fails", func(t *testing.T) {
		// Even very small decimals should fail
		result, err := CoerceType(42.000001, TypeInt)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decimal part")
		assert.Equal(t, 0, result)
	})

	t.Run("float32_precision_loss_to_float64", func(t *testing.T) {
		// float32 has limited precision
		f32 := float32(3.14159265358979323846)
		result, err := CoerceType(f32, TypeFloat)
		require.NoError(t, err)
		// float32 loses precision
		assert.NotEqual(t, 3.14159265358979323846, result)
		assert.InDelta(t, 3.14159, result, 0.0001)
	})

	t.Run("large_int64_to_float64_precision", func(t *testing.T) {
		// Large int64 values may lose precision when converted to float64
		largeInt := int64(9007199254740993) // Larger than 2^53
		result, err := CoerceType(largeInt, TypeFloat)
		require.NoError(t, err)
		// Note: float64 can't represent this exactly
		f := result.(float64)
		assert.NotEqual(t, int64(f), largeInt, "large int64 should lose precision in float64")
	})

	t.Run("string_float_to_float64_precision", func(t *testing.T) {
		// String with many decimal places
		result, err := CoerceType("3.141592653589793238462643383279", TypeFloat)
		require.NoError(t, err)
		// float64 can only represent about 15-17 significant digits
		assert.InDelta(t, 3.141592653589793, result, 1e-15)
	})

	t.Run("infinity_and_nan_strings", func(t *testing.T) {
		// Go's ParseFloat handles these
		posInf, err := CoerceType("+Inf", TypeFloat)
		require.NoError(t, err)
		assert.True(t, math.IsInf(posInf.(float64), 1), "should be positive infinity")

		negInf, err := CoerceType("-Inf", TypeFloat)
		require.NoError(t, err)
		assert.True(t, math.IsInf(negInf.(float64), -1), "should be negative infinity")

		nan, err := CoerceType("NaN", TypeFloat)
		require.NoError(t, err)
		assert.True(t, math.IsNaN(nan.(float64)), "should be NaN")
	})
}
