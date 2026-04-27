// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSolution_Defaults(t *testing.T) {
	result := Solution(Options{
		Name:        "my-solution",
		Description: "A test solution",
	})

	require.NotNil(t, result)
	assert.Equal(t, "./my-solution.yaml", result.Filename)
	assert.Contains(t, result.YAML, "name: my-solution")
	assert.NotContains(t, result.YAML, "version:")
	assert.Contains(t, result.YAML, "description: A test solution")
	assert.Contains(t, result.YAML, "provider: parameter")
	assert.Contains(t, result.YAML, "key: inputName")
	assert.Contains(t, result.YAML, "workflow:")
	assert.Contains(t, result.YAML, "transform:")
	assert.Contains(t, result.YAML, "validate:")
	assert.Contains(t, result.YAML, "testing:")
	assert.Contains(t, result.Features, "parameters")
	assert.Contains(t, result.Features, "resolvers")
	assert.Contains(t, result.Features, "actions")
	assert.Contains(t, result.Features, "transforms")
	assert.Contains(t, result.Features, "validation")
	assert.Contains(t, result.Features, "tests")
	assert.NotContains(t, result.Features, "composition")
	assert.NotEmpty(t, result.NextSteps)
}

func TestSolution_AllFeatures(t *testing.T) {
	result := Solution(Options{
		Name:        "full-solution",
		Description: "Everything enabled",
		Version:     "2.0.0",
		Features: map[string]bool{
			"parameters":  true,
			"resolvers":   true,
			"actions":     true,
			"transforms":  true,
			"validation":  true,
			"tests":       true,
			"composition": true,
		},
	})

	require.NotNil(t, result)
	assert.Contains(t, result.YAML, "version: \"2.0.0\"")
	assert.Contains(t, result.YAML, "provider: parameter")
	assert.Contains(t, result.YAML, "key: inputName")
	assert.Contains(t, result.YAML, "transform:")
	assert.Contains(t, result.YAML, "workflow:")
	assert.Contains(t, result.YAML, "testing:")
	assert.Contains(t, result.YAML, "compose:")
	assert.Contains(t, result.YAML, "validate:")
	assert.Contains(t, result.YAML, "-r")
	assert.NotContains(t, result.YAML, "resolvers:\n          inputName:")
}

func TestSolution_WithProviders(t *testing.T) {
	result := Solution(Options{
		Name:        "provider-demo",
		Description: "Provider examples",
		Features: map[string]bool{
			"resolvers": true,
			"actions":   true,
		},
		Providers: []string{"http", "exec", "env"},
	})

	require.NotNil(t, result)
	assert.Contains(t, result.YAML, "provider: http")
	assert.Contains(t, result.YAML, "provider: exec")
	assert.Contains(t, result.YAML, "provider: env")
}

func TestSolution_ResolversOnly(t *testing.T) {
	result := Solution(Options{
		Name:        "resolvers-only",
		Description: "Just resolvers",
		Features: map[string]bool{
			"resolvers": true,
		},
	})

	require.NotNil(t, result)
	assert.Contains(t, result.YAML, "resolvers:")
	assert.Contains(t, result.YAML, "provider: static")
	assert.NotContains(t, result.YAML, "workflow:")
}

func TestBuildYAML_EmptyFeatures(t *testing.T) {
	yaml := BuildYAML("empty", "empty solution", "1.0.0", map[string]bool{}, nil)
	assert.Contains(t, yaml, "name: empty")
	assert.Contains(t, yaml, "spec:")
	assert.NotContains(t, yaml, "resolvers:")
}

func TestFeatureKeys(t *testing.T) {
	keys := FeatureKeys(map[string]bool{
		"zebra": true,
		"apple": true,
		"mango": true,
	})
	assert.Equal(t, []string{"apple", "mango", "zebra"}, keys)
}

func TestFeatureKeys_Empty(t *testing.T) {
	keys := FeatureKeys(map[string]bool{})
	assert.Empty(t, keys)
}

func TestSolution_CustomVersion(t *testing.T) {
	result := Solution(Options{
		Name:        "versioned",
		Description: "Custom version",
		Version:     "3.2.1",
	})

	require.NotNil(t, result)
	assert.Contains(t, result.YAML, "version: \"3.2.1\"")
}

func TestBuildYAML_ValidYAMLStructure(t *testing.T) {
	yaml := BuildYAML("test", "Test solution", "1.0.0", map[string]bool{
		"parameters": true,
		"resolvers":  true,
	}, nil)

	// Verify basic YAML structure
	assert.True(t, strings.HasPrefix(yaml, "apiVersion:"))
	assert.Contains(t, yaml, "kind: Solution")
	assert.Contains(t, yaml, "metadata:")
	assert.Contains(t, yaml, "spec:")
}
