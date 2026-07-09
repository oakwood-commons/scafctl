// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package stateprovider

import (
	"context"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ctxWithState(t *testing.T, entries map[string]*state.PersistedEntry) context.Context {
	t.Helper()
	sd := state.NewData()
	for k, v := range entries {
		sd.Resolvers[k] = v
	}
	return state.WithState(context.Background(), sd)
}

func TestStateProvider_Descriptor(t *testing.T) {
	p := New()
	desc := p.Descriptor()

	assert.Equal(t, ProviderName, desc.Name)
	assert.Equal(t, "v1", desc.APIVersion)
	assert.NotNil(t, desc.Version)
	assert.NotEmpty(t, desc.Description)
	assert.Contains(t, desc.Capabilities, provider.CapabilityFrom)
	assert.NotEmpty(t, desc.Schema.Properties)
	assert.NotEmpty(t, desc.Examples)
}

func TestStateProvider_Execute_ReadsPersistedValue(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, map[string]*state.PersistedEntry{
		"db_password": {Value: "prior-secret", Type: "string", CreatedAt: time.Now().UTC()},
	})

	output, err := p.Execute(ctx, map[string]any{
		"operation": OperationGet,
		"key":       "db_password",
	})
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "prior-secret", output.Data)
	assert.Equal(t, true, output.Metadata["exists"])
}

func TestStateProvider_Execute_OperationDefaultsToGet(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, map[string]*state.PersistedEntry{
		"token": {Value: "abc", Type: "string", CreatedAt: time.Now().UTC()},
	})

	output, err := p.Execute(ctx, map[string]any{"key": "token"})
	require.NoError(t, err)
	assert.Equal(t, "abc", output.Data)
}

func TestStateProvider_Execute_MissingKeyReturnsDefault(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, nil)

	output, err := p.Execute(ctx, map[string]any{
		"key":     "absent",
		"default": "fallback",
	})
	require.NoError(t, err)
	assert.Equal(t, "fallback", output.Data)
	assert.Equal(t, false, output.Metadata["exists"])
}

func TestStateProvider_Execute_MissingKeyNoDefaultReturnsNil(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, nil)

	output, err := p.Execute(ctx, map[string]any{"key": "absent"})
	require.NoError(t, err)
	assert.Nil(t, output.Data)
	assert.Equal(t, false, output.Metadata["exists"])
}

func TestStateProvider_Execute_NoStateInContextReturnsDefault(t *testing.T) {
	p := New()

	output, err := p.Execute(context.Background(), map[string]any{
		"key":     "anything",
		"default": "d",
	})
	require.NoError(t, err)
	assert.Equal(t, "d", output.Data)
	assert.Equal(t, false, output.Metadata["exists"])
}

func TestStateProvider_Execute_MissingKeyInput(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, nil)

	_, err := p.Execute(ctx, map[string]any{"operation": OperationGet})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required input: key")
}

func TestStateProvider_Execute_EmptyKeyInput(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, nil)

	_, err := p.Execute(ctx, map[string]any{"key": ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required input: key")
}

func TestStateProvider_Execute_UnsupportedOperation(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, nil)

	_, err := p.Execute(ctx, map[string]any{"operation": "delete", "key": "k"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported operation")
}

func TestStateProvider_Execute_NonStringOperation(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, nil)

	_, err := p.Execute(ctx, map[string]any{"operation": 123, "key": "k"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "operation must be a string")
}

func TestStateProvider_Execute_InvalidInputType(t *testing.T) {
	p := New()

	_, err := p.Execute(context.Background(), "not-a-map")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected map[string]any")
}

func TestStateProvider_WhatIf(t *testing.T) {
	p := New()
	whatIf := p.Descriptor().WhatIf
	require.NotNil(t, whatIf)

	msg, err := whatIf(context.Background(), map[string]any{"key": "db_password"})
	require.NoError(t, err)
	assert.Contains(t, msg, "db_password")

	msg, err = whatIf(context.Background(), map[string]any{})
	require.NoError(t, err)
	assert.NotEmpty(t, msg)

	msg, err = whatIf(context.Background(), "not-a-map")
	require.NoError(t, err)
	assert.Empty(t, msg)
}
