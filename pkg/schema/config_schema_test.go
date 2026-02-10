// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateConfigSchema(t *testing.T) {
	t.Parallel()

	schemaBytes, err := GenerateConfigSchema()
	require.NoError(t, err)
	require.NotEmpty(t, schemaBytes)

	// Verify it's valid JSON
	var schema map[string]any
	err = json.Unmarshal(schemaBytes, &schema)
	require.NoError(t, err)

	// Verify key fields
	assert.Equal(t, ConfigSchemaID, schema["$id"])
	assert.Equal(t, "scafctl Configuration", schema["title"])
	assert.Contains(t, schema["description"].(string), "version 1")

	// Verify it has properties
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "schema should have properties")

	// Check expected top-level properties
	assert.Contains(t, props, "version")
	assert.Contains(t, props, "settings")
	assert.Contains(t, props, "catalogs")
	assert.Contains(t, props, "httpClient")
}

func TestGenerateConfigSchemaCompact(t *testing.T) {
	t.Parallel()

	schemaBytes, err := GenerateConfigSchemaCompact()
	require.NoError(t, err)
	require.NotEmpty(t, schemaBytes)

	// Verify it's valid JSON
	var schema map[string]any
	err = json.Unmarshal(schemaBytes, &schema)
	require.NoError(t, err)

	// Compact should not have newlines (it's a single line)
	assert.NotContains(t, string(schemaBytes), "\n")
}

func TestGenerateConfigSchema_HTTPClientProperties(t *testing.T) {
	t.Parallel()

	schemaBytes, err := GenerateConfigSchema()
	require.NoError(t, err)

	var schema map[string]any
	err = json.Unmarshal(schemaBytes, &schema)
	require.NoError(t, err)

	props := schema["properties"].(map[string]any)
	httpClient, ok := props["httpClient"].(map[string]any)
	require.True(t, ok, "schema should have httpClient property")

	httpClientProps, ok := httpClient["properties"].(map[string]any)
	require.True(t, ok, "httpClient should have properties")

	// Verify HTTP client properties exist
	expectedHTTPProps := []string{
		"timeout",
		"retryMax",
		"retryWaitMin",
		"retryWaitMax",
		"enableCache",
		"cacheType",
		"cacheDir",
		"cacheTTL",
		"enableCircuitBreaker",
		"enableCompression",
	}

	for _, prop := range expectedHTTPProps {
		assert.Contains(t, httpClientProps, prop, "httpClient should have %s property", prop)
	}
}
