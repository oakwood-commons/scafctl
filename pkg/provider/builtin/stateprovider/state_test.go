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
	assert.Contains(t, err.Error(), `specify exactly one of "key", "keys", or "all"`)
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

func TestStateProvider_WhatIf_MapModes(t *testing.T) {
	p := New()
	whatIf := p.Descriptor().WhatIf
	require.NotNil(t, whatIf)

	msg, err := whatIf(context.Background(), map[string]any{"all": true})
	require.NoError(t, err)
	assert.Contains(t, msg, "entire persisted state snapshot")

	msg, err = whatIf(context.Background(), map[string]any{"keys": []any{"a", "b"}})
	require.NoError(t, err)
	assert.Contains(t, msg, "requested persisted state keys")

	// all:false is inert and should not select the snapshot message.
	msg, err = whatIf(context.Background(), map[string]any{"all": false, "key": "k"})
	require.NoError(t, err)
	assert.Contains(t, msg, "k")
}

func TestStateProvider_Execute_KeysMode_ReturnsMapOmittingAbsent(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, map[string]*state.PersistedEntry{
		"keyA": {Value: "alpha", Type: "string", CreatedAt: time.Now().UTC()},
		"keyC": {Value: "gamma", Type: "string", CreatedAt: time.Now().UTC()},
	})

	output, err := p.Execute(ctx, map[string]any{
		"operation": OperationGet,
		"keys":      []any{"keyA", "keyB", "keyC"},
	})
	require.NoError(t, err)
	require.NotNil(t, output)

	got, ok := output.Data.(map[string]any)
	require.True(t, ok, "map mode must return a map")
	assert.Equal(t, map[string]any{"keyA": "alpha", "keyC": "gamma"}, got)
	// keyB is absent, so has()/optional chaining stay faithful.
	_, exists := got["keyB"]
	assert.False(t, exists)

	assert.Equal(t, "map", output.Metadata["mode"])
	assert.Equal(t, []string{"keyA", "keyC"}, output.Metadata["keys"])
	assert.Equal(t, []string{"keyB"}, output.Metadata["missing"])
}

func TestStateProvider_Execute_KeysMode_StringSlice(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, map[string]*state.PersistedEntry{
		"keyA": {Value: "alpha", Type: "string", CreatedAt: time.Now().UTC()},
	})

	output, err := p.Execute(ctx, map[string]any{"keys": []string{"keyA", "keyB"}})
	require.NoError(t, err)
	got, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"keyA": "alpha"}, got)
	assert.Equal(t, []string{"keyB"}, output.Metadata["missing"])
}

func TestStateProvider_Execute_KeysMode_EmptyListReturnsEmptyMap(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, map[string]*state.PersistedEntry{
		"keyA": {Value: "alpha", Type: "string", CreatedAt: time.Now().UTC()},
	})

	output, err := p.Execute(ctx, map[string]any{"keys": []any{}})
	require.NoError(t, err)
	got, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Empty(t, got)
}

func TestStateProvider_Execute_AllMode_ReturnsWholeSnapshot(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, map[string]*state.PersistedEntry{
		"keyA": {Value: "alpha", Type: "string", CreatedAt: time.Now().UTC()},
		"keyC": {Value: "gamma", Type: "string", CreatedAt: time.Now().UTC()},
	})

	output, err := p.Execute(ctx, map[string]any{"operation": OperationGet, "all": true})
	require.NoError(t, err)
	got, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"keyA": "alpha", "keyC": "gamma"}, got)
	assert.Equal(t, "map", output.Metadata["mode"])
	assert.Equal(t, []string{"keyA", "keyC"}, output.Metadata["keys"])
	// all mode reports no "missing" (no requested set).
	_, hasMissing := output.Metadata["missing"]
	assert.False(t, hasMissing)
}

func TestStateProvider_Execute_AllMode_NoStateReturnsEmptyMap(t *testing.T) {
	p := New()

	output, err := p.Execute(context.Background(), map[string]any{"all": true})
	require.NoError(t, err)
	got, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Empty(t, got)
}

func TestStateProvider_Execute_AllFalseIsInert(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, map[string]*state.PersistedEntry{
		"keyA": {Value: "alpha", Type: "string", CreatedAt: time.Now().UTC()},
	})

	// all:false does not select map mode; key still drives single-key mode.
	output, err := p.Execute(ctx, map[string]any{"all": false, "key": "keyA"})
	require.NoError(t, err)
	assert.Equal(t, "alpha", output.Data)
}

func TestStateProvider_Execute_MultipleSelectorsError(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, nil)

	tests := []map[string]any{
		{"key": "keyA", "keys": []any{"keyB"}},
		{"key": "keyA", "all": true},
		{"keys": []any{"keyB"}, "all": true},
	}
	for _, inputs := range tests {
		_, err := p.Execute(ctx, inputs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `specify exactly one of "key", "keys", or "all"`)
	}
}

func TestStateProvider_Execute_MapMode_RejectsDefault(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, nil)

	_, err := p.Execute(ctx, map[string]any{"keys": []any{"keyA"}, "default": ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"default" is only valid with a single "key"`)

	_, err = p.Execute(ctx, map[string]any{"all": true, "default": ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"default" is only valid with a single "key"`)
}

func TestStateProvider_Execute_KeysMode_InvalidKeyPattern(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, nil)

	_, err := p.Execute(ctx, map[string]any{"keys": []any{"ok", "../etc/passwd"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid key "../etc/passwd" at keys[1]`)
}

func TestStateProvider_Execute_KeysMode_NonStringEntry(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, nil)

	_, err := p.Execute(ctx, map[string]any{"keys": []any{"ok", 123}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keys[1] must be a string")
}

func TestStateProvider_Execute_KeysMode_WrongType(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, nil)

	_, err := p.Execute(ctx, map[string]any{"keys": "not-a-list"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"keys" must be an array of strings`)
}

func TestStateProvider_Execute_NonBoolAll(t *testing.T) {
	p := New()
	ctx := ctxWithState(t, nil)

	_, err := p.Execute(ctx, map[string]any{"all": "yes"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all must be a boolean")
}
