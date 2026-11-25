// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"encoding/json"
	"sync"
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

func TestGenerateComposePartSchema(t *testing.T) {
	t.Run("produces restricted envelope reusing patched defs", func(t *testing.T) {
		resetSolutionSchemaOnce()

		schemaBytes, err := GenerateComposePartSchema()
		require.NoError(t, err)
		require.NotEmpty(t, schemaBytes)

		var doc map[string]any
		require.NoError(t, json.Unmarshal(schemaBytes, &doc))

		assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", doc["$schema"])
		assert.Equal(t, ComposePartSchemaID, doc["$id"])
		assert.Equal(t, "object", doc["type"])
		assert.Equal(t, false, doc["additionalProperties"])

		rootProps, ok := doc["properties"].(map[string]any)
		require.True(t, ok)
		assert.ElementsMatch(t, []string{"spec", "bundle"}, mapKeys(rootProps))

		specSchema, ok := rootProps["spec"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, false, specSchema["additionalProperties"])
		specProps, ok := specSchema["properties"].(map[string]any)
		require.True(t, ok)
		assert.ElementsMatch(t, []string{"resolvers", "workflow", "testing"}, mapKeys(specProps))

		wfSchema, ok := specProps["workflow"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, false, wfSchema["additionalProperties"])
		wfProps, ok := wfSchema["properties"].(map[string]any)
		require.True(t, ok)
		assert.ElementsMatch(t, []string{"actions", "finally"}, mapKeys(wfProps))

		bundleSchema, ok := rootProps["bundle"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, false, bundleSchema["additionalProperties"])
		bundleProps, ok := bundleSchema["properties"].(map[string]any)
		require.True(t, ok)
		assert.ElementsMatch(t, []string{"include"}, mapKeys(bundleProps))

		defs, ok := doc["$defs"].(map[string]any)
		require.True(t, ok)
		assert.NotEmpty(t, defs)
	})

	t.Run("safe under concurrent access", func(t *testing.T) {
		resetSolutionSchemaOnce()

		const workers = 8
		results := make([][]byte, workers)
		var wg sync.WaitGroup
		wg.Add(workers)
		for i := range workers {
			go func(idx int) {
				defer wg.Done()
				b, err := GenerateComposePartSchema()
				assert.NoError(t, err)
				results[idx] = b
			}(i)
		}
		wg.Wait()

		for i := 1; i < workers; i++ {
			assert.Equal(t, results[0], results[i])
		}
	})

	t.Run("returns cloned slice so caller mutation cannot corrupt cache", func(t *testing.T) {
		resetSolutionSchemaOnce()
		first, err := GenerateComposePartSchema()
		require.NoError(t, err)
		origByte := first[0]
		first[0] = 'X'

		second, err := GenerateComposePartSchema()
		require.NoError(t, err)
		assert.Equal(t, origByte, second[0])
	})
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
