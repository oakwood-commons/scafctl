// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package messageprovider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisplaySchemaFromMap_ListConfig(t *testing.T) {
	display := map[string]any{
		"collectionTitle": "GCP Projects",
		"list": map[string]any{
			"titleField":      "name",
			"subtitleField":   "type",
			"badgeFields":     []any{"environmentCode"},
			"secondaryFields": []any{"folderID", "number"},
		},
	}

	result, err := displaySchemaFromMap(display)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(result, &parsed))

	assert.Equal(t, "GCP Projects", parsed["x-kvx-collectionTitle"])
	listConfig, ok := parsed["x-kvx-list"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "name", listConfig["titleField"])
	assert.Equal(t, "type", listConfig["subtitleField"])
}

func TestDisplaySchemaFromMap_DetailConfig(t *testing.T) {
	display := map[string]any{
		"detail": map[string]any{
			"titleField": "name",
			"sections": []any{
				map[string]any{
					"title":  "Identity",
					"fields": []any{"clientID", "name"},
				},
				map[string]any{
					"title":  "Access",
					"fields": []any{"adminGroup", "opsGroup"},
					"layout": "inline",
				},
			},
		},
	}

	result, err := displaySchemaFromMap(display)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(result, &parsed))

	detailConfig, ok := parsed["x-kvx-detail"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "name", detailConfig["titleField"])

	sections, ok := detailConfig["sections"].([]any)
	require.True(t, ok)
	assert.Len(t, sections, 2)
}

func TestDisplaySchemaFromMap_FullConfig(t *testing.T) {
	display := map[string]any{
		"icon":            "📦",
		"collectionTitle": "Projects",
		"version":         "v1",
		"list": map[string]any{
			"titleField": "name",
		},
		"detail": map[string]any{
			"titleField": "name",
		},
	}

	result, err := displaySchemaFromMap(display)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(result, &parsed))

	assert.Equal(t, "📦", parsed["x-kvx-icon"])
	assert.Equal(t, "Projects", parsed["x-kvx-collectionTitle"])
	assert.Equal(t, "v1", parsed["x-kvx-version"])
	assert.Contains(t, parsed, "x-kvx-list")
	assert.Contains(t, parsed, "x-kvx-detail")
}

func TestDisplaySchemaFromMap_Empty(t *testing.T) {
	_, err := displaySchemaFromMap(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestDisplaySchemaFromMap_Nil(t *testing.T) {
	_, err := displaySchemaFromMap(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestDisplaySchemaFromMap_UnknownKeysPassthrough(t *testing.T) {
	display := map[string]any{
		"list":       map[string]any{"titleField": "name"},
		"futureFlag": true,
	}

	result, err := displaySchemaFromMap(display)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(result, &parsed))

	assert.Contains(t, parsed, "x-kvx-list")
	assert.Equal(t, true, parsed["x-kvx-futureFlag"])
}

func TestDisplaySchemaFromMap_AlreadyPrefixedKeys(t *testing.T) {
	display := map[string]any{
		"list":       map[string]any{"titleField": "name"},
		"x-kvx-icon": "rocket",
	}

	result, err := displaySchemaFromMap(display)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(result, &parsed))

	// Should NOT double-prefix to x-kvx-x-kvx-icon.
	assert.Equal(t, "rocket", parsed["x-kvx-icon"])
	assert.NotContains(t, parsed, "x-kvx-x-kvx-icon")
}

func TestColumnHintsToJSON_BasicProperties(t *testing.T) {
	hints := map[string]any{
		"properties": map[string]any{
			"name": map[string]any{
				"x-kvx-header":   "Full Name",
				"x-kvx-maxWidth": 30,
			},
			"email": map[string]any{
				"x-kvx-header": "Email Address",
			},
		},
	}

	result, err := columnHintsToJSON(hints)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(result, &parsed))

	props, ok := parsed["properties"].(map[string]any)
	require.True(t, ok)

	nameHints, ok := props["name"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Full Name", nameHints["x-kvx-header"])
}

func TestColumnHintsToJSON_HiddenFields(t *testing.T) {
	hints := map[string]any{
		"properties": map[string]any{
			"metadata": map[string]any{
				"x-kvx-visible": false,
			},
		},
	}

	result, err := columnHintsToJSON(hints)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(result, &parsed))

	props := parsed["properties"].(map[string]any)
	meta := props["metadata"].(map[string]any)
	assert.Equal(t, false, meta["x-kvx-visible"])
}

func TestColumnHintsToJSON_Empty(t *testing.T) {
	_, err := columnHintsToJSON(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestColumnHintsToJSON_Nil(t *testing.T) {
	_, err := columnHintsToJSON(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func BenchmarkDisplaySchemaFromMap(b *testing.B) {
	display := map[string]any{
		"collectionTitle": "GCP Projects",
		"icon":            "📦",
		"list": map[string]any{
			"titleField":    "name",
			"subtitleField": "type",
			"badgeFields":   []any{"environmentCode"},
		},
		"detail": map[string]any{
			"titleField": "name",
			"sections": []any{
				map[string]any{
					"title":  "Identity",
					"fields": []any{"name", "number"},
				},
			},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = displaySchemaFromMap(display)
	}
}

func BenchmarkColumnHintsToJSON(b *testing.B) {
	hints := map[string]any{
		"properties": map[string]any{
			"name":     map[string]any{"x-kvx-header": "Full Name", "x-kvx-maxWidth": 30},
			"email":    map[string]any{"x-kvx-header": "Email Address"},
			"metadata": map[string]any{"x-kvx-visible": false},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = columnHintsToJSON(hints)
	}
}
