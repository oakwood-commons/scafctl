// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package stateprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescriptor(t *testing.T) {
	p := New()
	desc := p.Descriptor()

	assert.Equal(t, ProviderName, desc.Name)
	assert.Equal(t, "v1", desc.APIVersion)
	assert.NotNil(t, desc.Version)
	assert.Contains(t, desc.Capabilities, provider.CapabilityFrom)
	assert.Contains(t, desc.Capabilities, provider.CapabilityAction)
	assert.NotNil(t, desc.Schema)
}

func TestValidateDescriptor(t *testing.T) {
	p := New()
	err := provider.ValidateDescriptor(p.Descriptor())
	assert.NoError(t, err)
}

func TestExecute_Read_Hit(t *testing.T) {
	stateData := state.NewMockData("test-sol", "1.0.0", map[string]*state.Entry{
		"auth_token": {Value: "tok-123", Type: "string"},
	})
	ctx := state.WithState(context.Background(), stateData)

	p := New()
	output, err := p.Execute(ctx, map[string]any{
		"key": "auth_token",
	})

	require.NoError(t, err)
	assert.Equal(t, "tok-123", output.Data)
}

func TestExecute_Read_Miss_WithFallback(t *testing.T) {
	stateData := state.NewMockData("test-sol", "1.0.0", nil)
	ctx := state.WithState(context.Background(), stateData)

	p := New()
	output, err := p.Execute(ctx, map[string]any{
		"key":      "missing_key",
		"required": false,
		"fallback": "default-value",
	})

	require.NoError(t, err)
	assert.Equal(t, "default-value", output.Data)
}

func TestExecute_Read_Miss_NoFallback(t *testing.T) {
	stateData := state.NewMockData("test-sol", "1.0.0", nil)
	ctx := state.WithState(context.Background(), stateData)

	p := New()
	output, err := p.Execute(ctx, map[string]any{
		"key": "missing_key",
	})

	require.NoError(t, err)
	assert.Nil(t, output.Data)
}

func TestExecute_Read_Miss_Required(t *testing.T) {
	stateData := state.NewMockData("test-sol", "1.0.0", nil)
	ctx := state.WithState(context.Background(), stateData)

	p := New()
	_, err := p.Execute(ctx, map[string]any{
		"key":      "missing_key",
		"required": true,
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, state.ErrKeyNotFound)
}

func TestExecute_Read_NoStateInContext(t *testing.T) {
	p := New()
	output, err := p.Execute(context.Background(), map[string]any{
		"key":      "some_key",
		"fallback": "fallback-val",
	})

	require.NoError(t, err)
	assert.Equal(t, "fallback-val", output.Data)
}

func TestExecute_Read_NoStateInContext_Required(t *testing.T) {
	p := New()
	_, err := p.Execute(context.Background(), map[string]any{
		"key":      "some_key",
		"required": true,
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, state.ErrKeyNotFound)
}

func TestExecute_Write(t *testing.T) {
	stateData := state.NewMockData("test-sol", "1.0.0", nil)
	ctx := state.WithState(context.Background(), stateData)

	p := New()
	output, err := p.Execute(ctx, map[string]any{
		"key":   "deploy_id",
		"value": "dep-456",
		"type":  "string",
	})

	require.NoError(t, err)
	result := output.Data.(map[string]any)
	assert.True(t, result["success"].(bool))

	// Verify state was updated
	entry, exists := stateData.Values["deploy_id"]
	require.True(t, exists)
	assert.Equal(t, "dep-456", entry.Value)
	assert.Equal(t, "string", entry.Type)
	assert.False(t, entry.Immutable)
	assert.False(t, entry.UpdatedAt.IsZero())
}

func TestExecute_Write_Overwrite(t *testing.T) {
	stateData := state.NewMockData("test-sol", "1.0.0", map[string]*state.Entry{
		"key1": {Value: "old", Type: "string"},
	})
	ctx := state.WithState(context.Background(), stateData)

	p := New()
	_, err := p.Execute(ctx, map[string]any{
		"key":   "key1",
		"value": "new",
		"type":  "string",
	})

	require.NoError(t, err)
	assert.Equal(t, "new", stateData.Values["key1"].Value)
}

func TestExecute_Write_ImmutableEntry(t *testing.T) {
	stateData := state.NewMockData("test-sol", "1.0.0", map[string]*state.Entry{
		"locked_key": {Value: "locked", Type: "string", Immutable: true},
	})
	ctx := state.WithState(context.Background(), stateData)

	p := New()
	_, err := p.Execute(ctx, map[string]any{
		"key":   "locked_key",
		"value": "new-value",
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, state.ErrImmutableEntry)
	// Value should not have changed
	assert.Equal(t, "locked", stateData.Values["locked_key"].Value)
}

func TestExecute_Write_NoStateInContext(t *testing.T) {
	p := New()
	_, err := p.Execute(context.Background(), map[string]any{
		"key":   "some_key",
		"value": "some_value",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "state not available")
}

func TestExecute_MissingKey(t *testing.T) {
	p := New()
	_, err := p.Execute(context.Background(), map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key is required")
}

func TestExecute_InvalidInputType(t *testing.T) {
	p := New()
	_, err := p.Execute(context.Background(), "invalid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid input type")
}

func TestWhatIf(t *testing.T) {
	p := New()
	desc := p.Descriptor()
	require.NotNil(t, desc.WhatIf)

	t.Run("read", func(t *testing.T) {
		msg, err := desc.WhatIf(context.Background(), map[string]any{
			"key": "token",
		})
		assert.NoError(t, err)
		assert.Contains(t, msg, "read")
		assert.Contains(t, msg, "token")
	})

	t.Run("write", func(t *testing.T) {
		msg, err := desc.WhatIf(context.Background(), map[string]any{
			"key":   "token",
			"value": "abc",
		})
		assert.NoError(t, err)
		assert.Contains(t, msg, "write")
		assert.Contains(t, msg, "token")
	})
}
