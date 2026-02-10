// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package staticprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaticProvider_Descriptor(t *testing.T) {
	p := New()
	desc := p.Descriptor()

	assert.Equal(t, "static", desc.Name)
	assert.Equal(t, "v1", desc.APIVersion)
	assert.NotNil(t, desc.Version)
	assert.NotEmpty(t, desc.Description)
	assert.Contains(t, desc.Capabilities, provider.CapabilityFrom)
	assert.NotEmpty(t, desc.Schema.Properties)
	assert.NotEmpty(t, desc.Examples)
}

func TestStaticProvider_Execute_String(t *testing.T) {
	p := New()
	ctx := context.Background()

	inputs := map[string]any{
		"value": "test-value",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "test-value", output.Data)
}

func TestStaticProvider_Execute_Number(t *testing.T) {
	p := New()
	ctx := context.Background()

	inputs := map[string]any{
		"value": 42,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 42, output.Data)
}

func TestStaticProvider_Execute_Boolean(t *testing.T) {
	p := New()
	ctx := context.Background()

	inputs := map[string]any{
		"value": true,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, true, output.Data)
}

func TestStaticProvider_Execute_Object(t *testing.T) {
	p := New()
	ctx := context.Background()

	obj := map[string]any{
		"key1": "value1",
		"key2": 123,
		"key3": true,
	}

	inputs := map[string]any{
		"value": obj,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	result := output.Data.(map[string]any)
	assert.Equal(t, "value1", result["key1"])
	assert.Equal(t, 123, result["key2"])
	assert.Equal(t, true, result["key3"])
}

func TestStaticProvider_Execute_Array(t *testing.T) {
	p := New()
	ctx := context.Background()

	arr := []any{"item1", "item2", "item3"}

	inputs := map[string]any{
		"value": arr,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	result := output.Data.([]any)
	assert.Len(t, result, 3)
	assert.Equal(t, "item1", result[0])
	assert.Equal(t, "item2", result[1])
	assert.Equal(t, "item3", result[2])
}

func TestStaticProvider_Execute_MissingValue(t *testing.T) {
	p := New()
	ctx := context.Background()

	inputs := map[string]any{}

	output, err := p.Execute(ctx, inputs)
	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "missing required input: value")
}

func TestStaticProvider_Execute_NilValue(t *testing.T) {
	p := New()
	ctx := context.Background()

	inputs := map[string]any{
		"value": nil,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Nil(t, output.Data)
}
