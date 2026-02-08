// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package compare

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValues(t *testing.T) {
	tests := []struct {
		name     string
		a        any
		b        any
		expected int
	}{
		// Numeric comparisons
		{"int equal", 5, 5, 0},
		{"int less", 3, 5, -1},
		{"int greater", 7, 5, 1},
		{"float equal", 5.5, 5.5, 0},
		{"float less", 3.5, 5.5, -1},
		{"float greater", 7.5, 5.5, 1},
		{"mixed numeric equal", 5, 5.0, 0},
		{"int vs float less", 3, 5.5, -1},
		{"int vs float greater", 7, 5.5, 1},
		{"int64 comparison", int64(10), int64(20), -1},
		{"uint comparison", uint(15), uint(10), 1},

		// String comparisons
		{"string equal", "apple", "apple", 0},
		{"string less", "apple", "banana", -1},
		{"string greater", "banana", "apple", 1},
		{"empty strings", "", "", 0},
		{"string vs empty", "a", "", 1},

		// Boolean comparisons
		{"bool equal true", true, true, 0},
		{"bool equal false", false, false, 0},
		{"bool false less than true", false, true, -1},
		{"bool true greater than false", true, false, 1},

		// Nil comparisons
		{"nil equal", nil, nil, 0},
		{"nil less than value", nil, "value", -1},
		{"value greater than nil", "value", nil, 1},
		{"nil less than number", nil, 42, -1},

		// Mixed type comparisons (falls back to type name comparison)
		{"string vs int", "text", 123, 1}, // "string" > "int" alphabetically
		{"int vs bool", 123, true, 1},     // "int" > "bool" alphabetically
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Values(tt.a, tt.b)
			assert.Equal(t, tt.expected, result, "Values(%v, %v) = %d, want %d", tt.a, tt.b, result, tt.expected)
		})
	}
}

func TestValues_Symmetry(t *testing.T) {
	// Test that Values(a, b) == -Values(b, a)
	testCases := []struct {
		a any
		b any
	}{
		{5, 10},
		{"apple", "banana"},
		{true, false},
		{3.14, 2.71},
		{nil, "value"},
	}

	for _, tc := range testCases {
		forward := Values(tc.a, tc.b)
		backward := Values(tc.b, tc.a)
		assert.Equal(t, -forward, backward, "Values(%v, %v) should be negative of Values(%v, %v)", tc.a, tc.b, tc.b, tc.a)
	}
}

func TestValues_Transitivity(t *testing.T) {
	// Test that if a < b and b < c, then a < c
	a, b, c := 1, 5, 10

	ab := Values(a, b)
	bc := Values(b, c)
	ac := Values(a, c)

	assert.Less(t, ab, 0, "a < b")
	assert.Less(t, bc, 0, "b < c")
	assert.Less(t, ac, 0, "a < c (transitivity)")
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected float64
		ok       bool
	}{
		// Floating point types
		{"float64", float64(5.5), 5.5, true},
		{"float32", float32(5.5), 5.5, true},

		// Signed integer types
		{"int", 5, 5.0, true},
		{"int8", int8(5), 5.0, true},
		{"int16", int16(5), 5.0, true},
		{"int32", int32(5), 5.0, true},
		{"int64", int64(5), 5.0, true},

		// Unsigned integer types
		{"uint", uint(5), 5.0, true},
		{"uint8", uint8(5), 5.0, true},
		{"uint16", uint16(5), 5.0, true},
		{"uint32", uint32(5), 5.0, true},
		{"uint64", uint64(5), 5.0, true},

		// Negative numbers
		{"negative int", -10, -10.0, true},
		{"negative float", -3.14, -3.14, true},

		// Zero values
		{"zero int", 0, 0.0, true},
		{"zero float", 0.0, 0.0, true},

		// Non-numeric types
		{"string", "not a number", 0, false},
		{"bool", true, 0, false},
		{"nil", nil, 0, false},
		{"slice", []int{1, 2, 3}, 0, false},
		{"map", map[string]int{"a": 1}, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := ToFloat64(tt.input)
			assert.Equal(t, tt.ok, ok, "ToFloat64(%v) ok = %v, want %v", tt.input, ok, tt.ok)
			if ok {
				assert.Equal(t, tt.expected, result, "ToFloat64(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToFloat64_LargeNumbers(t *testing.T) {
	// Test with large numbers to ensure proper conversion
	tests := []struct {
		name  string
		input any
		want  float64
	}{
		{"large int64", int64(9223372036854775807), float64(9223372036854775807)},
		{"large uint64", uint64(18446744073709551615), float64(18446744073709551615)},
		{"large negative", int64(-9223372036854775808), float64(-9223372036854775808)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := ToFloat64(tt.input)
			assert.True(t, ok)
			assert.Equal(t, tt.want, result)
		})
	}
}

// Benchmark tests
func BenchmarkValues_Numeric(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Values(42, 100)
	}
}

func BenchmarkValues_String(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Values("apple", "banana")
	}
}

func BenchmarkValues_Bool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Values(true, false)
	}
}

func BenchmarkToFloat64_Int(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ToFloat64(42)
	}
}

func BenchmarkToFloat64_Float64(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ToFloat64(3.14)
	}
}

func BenchmarkToFloat64_String(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ToFloat64("not a number")
	}
}
