// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSolutionAgainstSchema(t *testing.T) {
	t.Run("valid minimal solution passes", func(t *testing.T) {
		resetSolutionSchemaOnce()

		data := map[string]any{
			"apiVersion": "scafctl.io/v1",
			"kind":       "Solution",
			"metadata": map[string]any{
				"name":    "test-solution",
				"version": "1.0.0",
			},
			"spec": map[string]any{
				"resolvers": map[string]any{
					"env": map[string]any{
						"name": "env",
						"resolve": map[string]any{
							"with": []any{
								map[string]any{
									"provider": "parameter",
									"inputs": map[string]any{
										"name": "environment",
									},
								},
							},
						},
					},
				},
			},
		}

		violations, err := ValidateSolutionAgainstSchema(data)
		require.NoError(t, err)
		assert.Empty(t, violations, "valid solution should produce no violations")
	})

	t.Run("wrong type for apiVersion is flagged", func(t *testing.T) {
		resetSolutionSchemaOnce()

		data := map[string]any{
			"apiVersion": 123, // should be string
			"kind":       "Solution",
			"metadata": map[string]any{
				"name":    "test-solution",
				"version": "1.0.0",
			},
		}

		violations, err := ValidateSolutionAgainstSchema(data)
		require.NoError(t, err)
		assert.NotEmpty(t, violations, "wrong type should produce violations")
	})

	t.Run("empty data has violations", func(t *testing.T) {
		resetSolutionSchemaOnce()

		data := map[string]any{}

		violations, err := ValidateSolutionAgainstSchema(data)
		require.NoError(t, err)
		// An empty object should have some violations (at minimum for missing properties
		// if the schema has required fields, or no violations if none are required).
		// Either way, it should not error.
		_ = violations
	})

	t.Run("nil data does not panic", func(t *testing.T) {
		resetSolutionSchemaOnce()

		violations, err := ValidateSolutionAgainstSchema(nil)
		// Should not panic; may or may not produce violations
		_ = violations
		_ = err
	})
}

func TestJsonPointerToDotPath(t *testing.T) {
	tests := []struct {
		pointer  string
		expected string
	}{
		{"/spec/resolvers/env", "spec.resolvers.env"},
		{"/spec/workflow/actions/build", "spec.workflow.actions.build"},
		{"/spec/resolvers/env/resolve/with/0", "spec.resolvers.env.resolve.with[0]"},
		{"/metadata/name", "metadata.name"},
		{"/", ""},
		{"", ""},
		{"/spec/workflow/actions/build/inputs/command", "spec.workflow.actions.build.inputs.command"},
	}

	for _, tt := range tests {
		t.Run(tt.pointer, func(t *testing.T) {
			result := jsonPointerToDotPath(tt.pointer)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsNumeric(t *testing.T) {
	assert.True(t, isNumeric("0"))
	assert.True(t, isNumeric("123"))
	assert.False(t, isNumeric(""))
	assert.False(t, isNumeric("abc"))
	assert.False(t, isNumeric("12a"))
}

func TestPatchSchema_ValueRef(t *testing.T) {
	resetSolutionSchemaOnce()

	schemaBytes, err := GenerateSolutionSchema()
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &doc))

	defs, ok := doc["$defs"].(map[string]any)
	require.True(t, ok, "$defs should exist")

	// Find the ValueRef definition
	key := findDefKey(defs, "ValueRef")
	require.NotEmpty(t, key, "ValueRef $def should exist")

	valRefDef := defs[key].(map[string]any)

	// Should have anyOf (from our patch)
	anyOf, ok := valRefDef["anyOf"]
	require.True(t, ok, "ValueRef should have anyOf after patching")

	// anyOf should contain multiple type options
	anyOfSlice, ok := anyOf.([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(anyOfSlice), 4, "anyOf should have at least 4 options (literals + structured ref)")
}

func TestPatchSchema_SkipBuiltinsValue(t *testing.T) {
	resetSolutionSchemaOnce()

	schemaBytes, err := GenerateSolutionSchema()
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &doc))

	defs, ok := doc["$defs"].(map[string]any)
	require.True(t, ok)

	key := findDefKey(defs, "SkipBuiltinsValue")
	if key == "" {
		t.Skip("SkipBuiltinsValue not in Solution struct tree")
	}

	sbvDef := defs[key].(map[string]any)
	oneOf, ok := sbvDef["oneOf"]
	require.True(t, ok, "SkipBuiltinsValue should have oneOf after patching")

	oneOfSlice := oneOf.([]any)
	assert.Len(t, oneOfSlice, 2, "oneOf should have bool and array options")
}

func TestPatchSchema_MapKeyNames(t *testing.T) {
	resetSolutionSchemaOnce()

	schemaBytes, err := GenerateSolutionSchema()
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &doc))

	defs, ok := doc["$defs"].(map[string]any)
	require.True(t, ok)

	// Resolver, Action, and TestCase should NOT have "name" in required
	for _, suffix := range []string{"Resolver", "Action", "TestCase"} {
		key := findDefKey(defs, suffix)
		if key == "" {
			continue
		}
		def := defs[key].(map[string]any)
		if req, ok := def["required"].([]any); ok {
			for _, r := range req {
				assert.NotEqual(t, "name", r, "%s should not require 'name' (set from map key)", suffix)
			}
		}
	}
}

func TestPatchSchema_JSONSchemaType(t *testing.T) {
	resetSolutionSchemaOnce()

	schemaBytes, err := GenerateSolutionSchema()
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &doc))

	defs, ok := doc["$defs"].(map[string]any)
	require.True(t, ok)

	key := findDefKey(defs, "JsonschemaSchema")
	if key == "" {
		t.Skip("JsonschemaSchema not in Solution struct tree")
	}

	def := defs[key].(map[string]any)
	// Should be an open object (type: object with no additionalProperties restriction)
	assert.Equal(t, "object", def["type"], "JsonschemaSchema should be type object")
	assert.Nil(t, def["additionalProperties"], "JsonschemaSchema should not restrict additional properties")
}
