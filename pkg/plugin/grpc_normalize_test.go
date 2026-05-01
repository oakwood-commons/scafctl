// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeJSONNumbers(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{
			name:     "integer json.Number becomes int64",
			input:    json.Number("42"),
			expected: int64(42),
		},
		{
			name:     "negative integer json.Number becomes int64",
			input:    json.Number("-7"),
			expected: int64(-7),
		},
		{
			name:     "float json.Number becomes float64",
			input:    json.Number("3.14"),
			expected: float64(3.14),
		},
		{
			name:     "string unchanged",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "bool unchanged",
			input:    true,
			expected: true,
		},
		{
			name:     "nil unchanged",
			input:    nil,
			expected: nil,
		},
		{
			name:     "slice of json.Number becomes slice of int64",
			input:    []any{json.Number("1"), json.Number("2"), json.Number("3")},
			expected: []any{int64(1), int64(2), int64(3)},
		},
		{
			name: "nested map with json.Number",
			input: map[string]any{
				"count": json.Number("5"),
				"name":  "test",
				"nested": map[string]any{
					"value": json.Number("99"),
				},
			},
			expected: map[string]any{
				"count": int64(5),
				"name":  "test",
				"nested": map[string]any{
					"value": int64(99),
				},
			},
		},
		{
			name:     "mixed slice",
			input:    []any{json.Number("1"), "two", json.Number("3.5"), true},
			expected: []any{int64(1), "two", float64(3.5), true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeJSONNumbers(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestNormalizeJSONNumbersMap_Nil(t *testing.T) {
	assert.Nil(t, normalizeJSONNumbersMap(nil))
}

func TestNormalizeJSONNumbers_RoundTrip(t *testing.T) {
	// Simulate what happens when plugin returns [1, 2, 3] through JSON
	original := map[string]any{
		"data": []any{1, 2, 3, 4, 5},
	}
	encoded, err := json.Marshal(original)
	require.NoError(t, err)

	// Decode with UseNumber (as our fix does)
	var decoded map[string]any
	dec := json.NewDecoder(bytes.NewReader(encoded))
	dec.UseNumber()
	require.NoError(t, dec.Decode(&decoded))

	// Normalize
	result := normalizeJSONNumbersMap(decoded)

	// Values should be int64 not float64
	data, ok := result["data"].([]any)
	require.True(t, ok)
	for i, v := range data {
		assert.IsType(t, int64(0), v, "element %d should be int64", i)
		assert.Equal(t, int64(i+1), v)
	}
}
