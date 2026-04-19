// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fileprovider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileProvider_StateCapability(t *testing.T) {
	p := NewFileProvider()
	desc := p.Descriptor()
	assert.Contains(t, desc.Capabilities, provider.CapabilityState)
	assert.Contains(t, desc.OutputSchemas, provider.CapabilityState)
}

func TestFileProvider_StateLoad_NotFound(t *testing.T) {
	p := NewFileProvider()

	result, err := p.executeStateLoad(filepath.Join(t.TempDir(), "nonexistent.json"))
	require.NoError(t, err)

	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.NotNil(t, data["data"])
}

func TestFileProvider_StateRoundTrip(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()
	absPath := filepath.Join(tmpDir, "state.json")

	stateData := state.NewData()
	stateData.Values = map[string]*state.Entry{
		"greeting": {Value: "hello"},
	}

	// Save
	saveResult, err := p.executeStateSave(absPath, map[string]any{"data": stateData})
	require.NoError(t, err)
	assert.True(t, saveResult.Data.(map[string]any)["success"].(bool))

	// Verify file exists on disk
	_, err = os.Stat(absPath)
	require.NoError(t, err)

	// Load
	loadResult, err := p.executeStateLoad(absPath)
	require.NoError(t, err)
	loadMap := loadResult.Data.(map[string]any)
	assert.True(t, loadMap["success"].(bool))

	loaded, ok := loadMap["data"].(*state.Data)
	require.True(t, ok)
	assert.Equal(t, "hello", loaded.Values["greeting"].Value)

	// Delete
	delResult, err := p.executeStateDelete(absPath)
	require.NoError(t, err)
	assert.True(t, delResult.Data.(map[string]any)["success"].(bool))

	// Verify file is gone
	_, err = os.Stat(absPath)
	assert.True(t, os.IsNotExist(err))
}

func TestFileProvider_StateDeleteNotFound(t *testing.T) {
	p := NewFileProvider()

	result, err := p.executeStateDelete(filepath.Join(t.TempDir(), "nonexistent.json"))
	require.NoError(t, err)
	assert.True(t, result.Data.(map[string]any)["success"].(bool))
}

func TestFileProvider_StateDispatch(t *testing.T) {
	p := NewFileProvider()
	ctx := provider.WithExecutionMode(context.Background(), provider.CapabilityState)

	// Dispatch routes state_load through the Execute path
	result, err := p.Execute(ctx, map[string]any{
		"operation": "state_load",
		"path":      "some/path.json",
	})
	require.NoError(t, err)
	// state_load with non-existent file returns empty state
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
}

func TestFileProvider_StateDryRun(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		hasData   bool
	}{
		{"load", "state_load", true},
		{"save", "state_save", false},
		{"delete", "state_delete", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewFileProvider()
			ctx := provider.WithDryRun(context.Background(), true)

			result, err := p.dispatchStateOperation(ctx, tt.operation, map[string]any{
				"path": "test.json",
				"data": state.NewData(),
			})
			require.NoError(t, err)
			data := result.Data.(map[string]any)
			assert.True(t, data["success"].(bool))
			if tt.hasData {
				assert.NotNil(t, data["data"])
			}
		})
	}
}

func TestFileProvider_StateMissingPath(t *testing.T) {
	p := NewFileProvider()
	ctx := provider.WithExecutionMode(context.Background(), provider.CapabilityState)

	_, err := p.Execute(ctx, map[string]any{
		"operation": "state_load",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
}

func TestFileProvider_StateLoadInvalidJSON(t *testing.T) {
	p := NewFileProvider()

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "bad.json")
	require.NoError(t, os.WriteFile(statePath, []byte("{invalid"), 0o600))

	_, err := p.executeStateLoad(statePath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestFileProvider_StateSaveMarshal(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()
	absPath := filepath.Join(tmpDir, "map-test.json")

	result, err := p.executeStateSave(absPath, map[string]any{
		"data": map[string]any{"key": "value"},
	})
	require.NoError(t, err)
	assert.True(t, result.Data.(map[string]any)["success"].(bool))

	raw, err := os.ReadFile(absPath)
	require.NoError(t, err)
	var loaded map[string]any
	require.NoError(t, json.Unmarshal(raw, &loaded))
	assert.Equal(t, "value", loaded["key"])
}

func TestFileProvider_StateSaveMissingData(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()
	absPath := filepath.Join(tmpDir, "no-data.json")

	_, err := p.executeStateSave(absPath, map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "data is required")
}

func BenchmarkFileProvider_StateLoad(b *testing.B) {
	p := NewFileProvider()
	tmpDir := b.TempDir()

	sd := state.NewData()
	sd.Values = map[string]*state.Entry{
		"x": {Value: "y"},
	}
	data, _ := json.MarshalIndent(sd, "", "  ")
	statePath := filepath.Join(tmpDir, "bench.json")
	require.NoError(b, os.WriteFile(statePath, data, 0o600))

	b.ResetTimer()
	for b.Loop() {
		_, _ = p.executeStateLoad(statePath)
	}
}

func BenchmarkFileProvider_StateSave(b *testing.B) {
	p := NewFileProvider()
	tmpDir := b.TempDir()

	sd := state.NewData()
	sd.Values = map[string]*state.Entry{
		"x": {Value: "y"},
	}
	absPath := filepath.Join(tmpDir, "bench.json")

	b.ResetTimer()
	for b.Loop() {
		_, _ = p.executeStateSave(absPath, map[string]any{"data": sd})
	}
}
