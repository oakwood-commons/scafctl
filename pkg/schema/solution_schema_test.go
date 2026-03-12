// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSolutionSchema(t *testing.T) {
	t.Run("produces valid JSON", func(t *testing.T) {
		// Reset the schema state for testing
		resetSolutionSchemaOnce()

		schemaBytes, err := GenerateSolutionSchema()
		require.NoError(t, err)
		require.NotEmpty(t, schemaBytes)

		var doc map[string]any
		err = json.Unmarshal(schemaBytes, &doc)
		require.NoError(t, err)

		assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", doc["$schema"])
		assert.Equal(t, SolutionSchemaID, doc["$id"])
		assert.Equal(t, "scafctl Solution", doc["title"])
		assert.Equal(t, "object", doc["type"])
	})

	t.Run("contains expected top-level properties", func(t *testing.T) {
		resetSolutionSchemaOnce()

		schemaBytes, err := GenerateSolutionSchema()
		require.NoError(t, err)

		var doc map[string]any
		require.NoError(t, json.Unmarshal(schemaBytes, &doc))

		props, ok := doc["properties"].(map[string]any)
		require.True(t, ok, "expected properties to be an object")

		assert.Contains(t, props, "apiVersion")
		assert.Contains(t, props, "kind")
		assert.Contains(t, props, "metadata")
		assert.Contains(t, props, "spec")
		assert.Contains(t, props, "catalog")
		assert.Contains(t, props, "compose")
		assert.Contains(t, props, "bundle")
	})

	t.Run("metadata has doc descriptions", func(t *testing.T) {
		resetSolutionSchemaOnce()

		schemaBytes, err := GenerateSolutionSchema()
		require.NoError(t, err)

		var doc map[string]any
		require.NoError(t, json.Unmarshal(schemaBytes, &doc))

		props := doc["properties"].(map[string]any)
		apiVersion := props["apiVersion"].(map[string]any)
		assert.Contains(t, apiVersion, "description")
		assert.Contains(t, apiVersion, "examples")
	})

	t.Run("has $defs for component types", func(t *testing.T) {
		resetSolutionSchemaOnce()

		schemaBytes, err := GenerateSolutionSchema()
		require.NoError(t, err)

		var doc map[string]any
		require.NoError(t, json.Unmarshal(schemaBytes, &doc))

		defs, ok := doc["$defs"].(map[string]any)
		require.True(t, ok, "expected $defs to be present")
		assert.NotEmpty(t, defs)
	})

	t.Run("no leftover #/components/schemas refs", func(t *testing.T) {
		resetSolutionSchemaOnce()

		schemaBytes, err := GenerateSolutionSchema()
		require.NoError(t, err)

		assert.NotContains(t, string(schemaBytes), "#/components/schemas/")
	})
}

func TestGenerateSolutionSchemaCompact(t *testing.T) {
	t.Run("produces compact JSON", func(t *testing.T) {
		resetSolutionSchemaOnce()

		compact, err := GenerateSolutionSchemaCompact()
		require.NoError(t, err)
		require.NotEmpty(t, compact)

		// Compact JSON should not have indentation
		assert.NotContains(t, string(compact), "  ")
	})
}

func TestRewriteRefs(t *testing.T) {
	t.Run("rewrites component schema refs to defs", func(t *testing.T) {
		doc := map[string]any{
			"$ref": "#/components/schemas/Metadata",
			"nested": map[string]any{
				"$ref": "#/components/schemas/Spec",
			},
			"array": []any{
				map[string]any{"$ref": "#/components/schemas/Action"},
			},
		}

		rewriteRefs(doc)

		assert.Equal(t, "#/$defs/Metadata", doc["$ref"])
		nested := doc["nested"].(map[string]any)
		assert.Equal(t, "#/$defs/Spec", nested["$ref"])
		arr := doc["array"].([]any)
		arrItem := arr[0].(map[string]any)
		assert.Equal(t, "#/$defs/Action", arrItem["$ref"])
	})
}

// resetSolutionSchemaOnce resets the schema state so each test starts clean.
func resetSolutionSchemaOnce() {
	resetSolutionSchemaForTesting()
}
