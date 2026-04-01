// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterItems_NoFilter(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	result, err := FilterItems(context.Background(), items, "")
	assert.NoError(t, err)
	assert.Equal(t, items, result)
}

func TestFilterItems_WithMatchingFilter(t *testing.T) {
	items := []map[string]any{
		{"name": "alpha", "value": 1},
		{"name": "beta", "value": 2},
		{"name": "gamma", "value": 3},
	}
	result, err := FilterItems(context.Background(), items, `item["value"] > 1`)
	assert.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "beta", result[0]["name"])
	assert.Equal(t, "gamma", result[1]["name"])
}

func TestFilterItems_AllFiltered(t *testing.T) {
	items := []int{1, 2, 3}
	result, err := FilterItems(context.Background(), items, "item > 100")
	assert.NoError(t, err)
	assert.Empty(t, result)
}

func TestFilterItems_NoneFiltered(t *testing.T) {
	items := []int{10, 20, 30}
	result, err := FilterItems(context.Background(), items, "item > 5")
	assert.NoError(t, err)
	assert.Equal(t, items, result)
}

func TestFilterItems_NonBoolExpression(t *testing.T) {
	items := []map[string]any{{"x": 1}}
	_, err := FilterItems(context.Background(), items, `item["x"] + 1`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "boolean")
}

func TestFilterItems_InvalidExpression(t *testing.T) {
	items := []int{1, 2, 3}
	_, err := FilterItems(context.Background(), items, "{{invalid}}")
	assert.Error(t, err)
}

func TestFilterItems_EmptySlice(t *testing.T) {
	var items []string
	result, err := FilterItems(context.Background(), items, `item == "foo"`)
	assert.NoError(t, err)
	assert.Empty(t, result)
}

// TestFilterItems_StructFieldAccess verifies that CEL expressions can access
// struct fields by their JSON tag names (e.g., item.name on a struct with
// Name string `json:"name"`). This is the pattern used by the providers and
// catalogs endpoints.
func TestFilterItems_StructFieldAccess(t *testing.T) {
	type testItem struct {
		Name     string `json:"name"`
		Category string `json:"category"`
		Value    int    `json:"value"`
	}

	items := []testItem{
		{Name: "alpha", Category: "a", Value: 1},
		{Name: "beta", Category: "b", Value: 2},
		{Name: "gamma", Category: "a", Value: 3},
	}

	tests := []struct {
		name     string
		filter   string
		expected []string
	}{
		{
			name:     "filter by name equality",
			filter:   `item.name == "beta"`,
			expected: []string{"beta"},
		},
		{
			name:     "filter by category",
			filter:   `item.category == "a"`,
			expected: []string{"alpha", "gamma"},
		},
		{
			name:     "filter by numeric field",
			filter:   `item.value > 1`,
			expected: []string{"beta", "gamma"},
		},
		{
			name:     "combined filter",
			filter:   `item.category == "a" && item.value > 1`,
			expected: []string{"gamma"},
		},
		{
			name:     "no matches",
			filter:   `item.name == "nonexistent"`,
			expected: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := FilterItems(context.Background(), items, tc.filter)
			assert.NoError(t, err)
			assert.Len(t, result, len(tc.expected))
			for i, item := range result {
				assert.Equal(t, tc.expected[i], item.Name)
			}
		})
	}
}

func BenchmarkFilterItems_StructFieldAccess(b *testing.B) {
	type benchItem struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	items := make([]benchItem, 100)
	for i := range items {
		items[i] = benchItem{Name: "item", Value: i}
	}
	ctx := context.Background()
	for b.Loop() {
		_, _ = FilterItems(ctx, items, `item.value > 50`)
	}
}

func BenchmarkFilterItems_NoFilter(b *testing.B) {
	items := make([]string, 100)
	for i := range items {
		items[i] = "test"
	}
	ctx := context.Background()
	for b.Loop() {
		_, _ = FilterItems(ctx, items, "")
	}
}
