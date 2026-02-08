// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package debugprovider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDebugProvider(t *testing.T) {
	p := NewDebugProvider()
	require.NotNil(t, p)
	assert.Equal(t, "debug", p.Descriptor().Name)
	assert.Equal(t, "Debug Provider", p.Descriptor().DisplayName)
}

func TestDebugProvider_Execute_AllResolverData(t *testing.T) {
	p := NewDebugProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))

	// Add resolver data to context
	resolverData := map[string]any{
		"user":   map[string]any{"name": "John", "age": 30},
		"config": map[string]any{"debug": true},
	}
	ctx = provider.WithResolverContext(ctx, resolverData)

	inputs := map[string]any{}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.True(t, data["success"].(bool))

	// Verify result contains all resolver data
	result, ok := data["result"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, result, "user")
	assert.Contains(t, result, "config")
}

func TestDebugProvider_Execute_WithExpression(t *testing.T) {
	p := NewDebugProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))

	// Add resolver data to context
	resolverData := map[string]any{
		"user":   map[string]any{"name": "John", "age": 30},
		"config": map[string]any{"debug": true},
	}
	ctx = provider.WithResolverContext(ctx, resolverData)

	inputs := map[string]any{
		"expression": "_.user.name",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.True(t, data["success"].(bool))

	// Verify result is filtered by expression
	assert.Equal(t, "John", data["result"])
}

func TestDebugProvider_Execute_File(t *testing.T) {
	p := NewDebugProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))

	// Add resolver data to context
	resolverData := map[string]any{"test": "data"}
	ctx = provider.WithResolverContext(ctx, resolverData)

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "debug.log")

	inputs := map[string]any{
		"destination": "file",
		"file":        filePath,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify file was created and contains data
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "test")
}

func TestDebugProvider_Execute_DryRun(t *testing.T) {
	p := NewDebugProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = provider.WithDryRun(ctx, true)

	inputs := map[string]any{
		"expression": "_.user.name",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.True(t, data["_dryRun"].(bool))
}
