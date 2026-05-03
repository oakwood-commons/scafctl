// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package collections

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeLen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    any
		expected int
		wantErr  bool
	}{
		{name: "nil value", input: nil, expected: 0},
		{name: "empty string", input: "", expected: 0},
		{name: "non-empty string", input: "hello", expected: 5},
		{name: "empty slice", input: []any{}, expected: 0},
		{name: "non-empty slice", input: []any{1, 2, 3}, expected: 3},
		{name: "nil slice", input: ([]string)(nil), expected: 0},
		{name: "empty map", input: map[string]any{}, expected: 0},
		{name: "non-empty map", input: map[string]any{"a": 1, "b": 2}, expected: 2},
		{name: "nil map", input: (map[string]any)(nil), expected: 0},
		{name: "array", input: [3]int{1, 2, 3}, expected: 3},
		{name: "pointer to array", input: &[3]int{1, 2, 3}, expected: 3},
		{name: "pointer to slice", input: func() any { s := []string{"a", "b"}; return &s }(), expected: 2},
		{name: "nil pointer", input: (*[3]int)(nil), expected: 0},
		{name: "integer (unsupported)", input: 42, wantErr: true},
		{name: "bool (unsupported)", input: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := SafeLen(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSafeLenFunc_Metadata(t *testing.T) {
	t.Parallel()
	f := SafeLenFunc()
	assert.Equal(t, "len", f.Name)
	assert.True(t, f.Custom)
	assert.NotEmpty(t, f.Description)
	assert.Contains(t, f.Func, "len")
}

func BenchmarkSafeLen(b *testing.B) {
	inputs := []struct {
		name  string
		value any
	}{
		{"nil", nil},
		{"string", "hello world"},
		{"slice", []int{1, 2, 3, 4, 5}},
		{"map", map[string]int{"a": 1, "b": 2}},
	}

	for _, tt := range inputs {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				SafeLen(tt.value) //nolint:errcheck // benchmark
			}
		})
	}
}
