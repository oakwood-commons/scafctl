// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kvx

import (
	"context"
	"testing"

	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: Snapshot tests are not parallelized because kvx's DefaultMenuConfig
// uses a package-level map that is not safe for concurrent access.

func TestSnapshot_SimpleMap(t *testing.T) {
	data := map[string]any{
		"name":    "test-solution",
		"version": "1.0.0",
		"enabled": true,
	}

	result, err := Snapshot(data, WithNoColor(true), WithDimensions(80, 24))
	require.NoError(t, err)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "name")
	assert.Contains(t, result, "test-solution")
}

func TestSnapshot_SliceOfMaps(t *testing.T) {
	data := []map[string]any{
		{"name": "alpha", "status": "ok"},
		{"name": "beta", "status": "failed"},
	}

	result, err := Snapshot(data, WithNoColor(true), WithDimensions(80, 24))
	require.NoError(t, err)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "alpha")
	assert.Contains(t, result, "beta")
}

func TestSnapshot_WithExpression(t *testing.T) {
	data := map[string]any{
		"items": []map[string]any{
			{"name": "a"},
			{"name": "b"},
		},
		"count": 2,
	}

	result, err := Snapshot(data,
		WithNoColor(true),
		WithDimensions(80, 24),
		WithExpression("_.items"),
	)
	require.NoError(t, err)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "a")
	assert.Contains(t, result, "b")
}

func TestSnapshot_InvalidExpression(t *testing.T) {
	data := map[string]any{"key": "value"}

	_, err := Snapshot(data,
		WithNoColor(true),
		WithExpression("_.nonexistent.deep.path"),
	)
	assert.Error(t, err)
}

func TestSnapshot_AppName(t *testing.T) {
	data := map[string]any{"key": "value"}

	result, err := Snapshot(data,
		WithNoColor(true),
		WithDimensions(80, 24),
		WithAppName("mycli"),
	)
	require.NoError(t, err)
	assert.Contains(t, result, "mycli")
}

func TestOutputOptions_Snapshot(t *testing.T) {
	data := map[string]any{
		"name":   "test",
		"status": "ok",
	}

	opts := &OutputOptions{
		NoColor: true,
		AppName: "scafctl",
	}

	result, err := opts.Snapshot(data)
	require.NoError(t, err)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "test")
	assert.Contains(t, result, "ok")
}

func TestOutputOptions_Snapshot_AllOptions(t *testing.T) {
	data := []map[string]any{
		{"name": "alpha", "enabled": true},
		{"name": "beta", "enabled": false},
	}

	opts := &OutputOptions{
		Ctx:         context.Background(),
		NoColor:     true,
		AppName:     "mycli inspect",
		HelpTitle:   "Test Help",
		HelpLines:   []string{"Line 1", "Line 2"},
		Theme:       "dark",
		Expression:  "",
		Where:       "_.enabled",
		ColumnOrder: []string{"name", "enabled"},
		ColumnHints: map[string]tui.ColumnHint{
			"name": {MaxWidth: 20, Priority: 10},
		},
	}

	result, err := opts.Snapshot(data)
	require.NoError(t, err)
	assert.Contains(t, result, "mycli inspect")
	assert.Contains(t, result, "alpha")
	// "beta" should be filtered out by Where filter (enabled == false)
	assert.NotContains(t, result, "beta")
}

func TestSnapshot_InvalidDisplaySchema(t *testing.T) {
	data := map[string]any{"key": "value"}

	_, err := Snapshot(data,
		WithNoColor(true),
		WithDisplaySchemaJSON([]byte(`{"x-kvx-detail": {"sections": [{"fields": ["key"], "layout": "invalid"}]}}`)),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "display schema")
}

func TestSnapshot_WithWhereFilter(t *testing.T) {
	data := []map[string]any{
		{"name": "keep", "active": true},
		{"name": "drop", "active": false},
	}

	result, err := Snapshot(data,
		WithNoColor(true),
		WithDimensions(80, 24),
		WithWhere("_.active"),
	)
	require.NoError(t, err)
	assert.Contains(t, result, "keep")
	assert.NotContains(t, result, "drop")
}

func TestSnapshot_WithThemeAndHelp(t *testing.T) {
	data := map[string]any{"key": "value"}

	result, err := Snapshot(data,
		WithNoColor(true),
		WithDimensions(80, 24),
		WithTheme("dark"),
		WithHelp("My Help", []string{"Line 1"}),
	)
	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

func BenchmarkSnapshot(b *testing.B) {
	data := make([]map[string]any, 20)
	for i := range data {
		data[i] = map[string]any{
			"name":    "item",
			"index":   i,
			"enabled": i%2 == 0,
		}
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = Snapshot(data, WithNoColor(true), WithDimensions(120, 40))
	}
}
