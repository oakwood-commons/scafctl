// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package metadataprovider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescriptor(t *testing.T) {
	p := New()
	d := p.Descriptor()
	assert.Equal(t, ProviderName, d.Name)
	assert.Equal(t, "Metadata Provider", d.DisplayName)
	assert.NotNil(t, d.Schema)
	assert.Len(t, d.Capabilities, 1)
	assert.Len(t, d.Examples, 2)
}

func TestExecute_FullMap(t *testing.T) {
	p := New()
	input := map[string]any{
		"name":    "my-solution",
		"version": "1.0.0",
		"tags":    []string{"gcp", "terraform"},
	}
	out, err := p.Execute(context.Background(), input)
	require.NoError(t, err)

	result, ok := out.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "my-solution", result["name"])
	assert.Equal(t, "1.0.0", result["version"])
	assert.Equal(t, []string{"gcp", "terraform"}, result["tags"])
}

func TestExecute_SingleField(t *testing.T) {
	p := New()
	input := map[string]any{
		"field":   "name",
		"name":    "my-solution",
		"version": "2.0.0",
	}
	out, err := p.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "my-solution", out.Data)
}

func TestExecute_SingleField_Missing(t *testing.T) {
	p := New()
	input := map[string]any{
		"field": "missing-key",
		"name":  "my-solution",
	}
	_, err := p.Execute(context.Background(), input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing-key")
}

func TestExecute_FieldKeyExcluded(t *testing.T) {
	p := New()
	input := map[string]any{
		"field":   "",
		"name":    "test",
		"version": "1.0.0",
	}
	out, err := p.Execute(context.Background(), input)
	require.NoError(t, err)
	result, ok := out.Data.(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, result, "field")
	assert.Equal(t, "test", result["name"])
}

func TestExecute_BadInputType(t *testing.T) {
	p := New()
	_, err := p.Execute(context.Background(), "not-a-map")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected map[string]any")
}

func TestExecute_EmptyMap(t *testing.T) {
	p := New()
	out, err := p.Execute(context.Background(), map[string]any{})
	require.NoError(t, err)
	result, ok := out.Data.(map[string]any)
	require.True(t, ok)
	assert.Empty(t, result)
}
