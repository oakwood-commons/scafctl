// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescribeCachedPlugins_SkipsNamesInSkipSet(t *testing.T) {
	t.Parallel()
	cached := []CachedPlugin{
		{Name: "builtin-provider", Version: "1.0.0", Path: "/fake/path"},
		{Name: "local-provider", Version: "2.0.0", Path: "/fake/path2"},
	}
	skip := map[string]bool{"builtin-provider": true}

	result := DescribeCachedPlugins(context.Background(), cached, skip)

	// local-provider is excluded because the probe fails (non-existent binary).
	assert.Empty(t, result)
}

func TestDescribeCachedPlugins_SkipsFailedProbes(t *testing.T) {
	t.Parallel()
	cached := []CachedPlugin{
		{Name: "bad-plugin", Version: "1.0.0", Path: "/nonexistent/binary"},
	}

	result := DescribeCachedPlugins(context.Background(), cached, nil)
	assert.Empty(t, result, "plugins that fail probing should be excluded")
}

func TestDescribeCachedPlugins_EmptyInput(t *testing.T) {
	t.Parallel()
	result := DescribeCachedPlugins(context.Background(), nil, nil)
	assert.Empty(t, result)
}

func TestProbePluginDescription_InvalidBinary(t *testing.T) {
	t.Parallel()
	missingPath := filepath.Join(t.TempDir(), "nonexistent", "binary")
	desc, ok := ProbePluginDescription(context.Background(), missingPath, "test")
	assert.Empty(t, desc)
	assert.False(t, ok)
}

func TestProbePluginDescriptor_InvalidBinary(t *testing.T) {
	t.Parallel()
	missingPath := filepath.Join(t.TempDir(), "nonexistent", "binary")
	desc, err := ProbePluginDescriptor(context.Background(), missingPath, "test")
	assert.Nil(t, desc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "starting plugin binary")
	assert.Contains(t, err.Error(), missingPath, "error should include the binary path for diagnostics")
}

func TestProbePluginDescriptor_NonExecutableFile(t *testing.T) {
	t.Parallel()
	// Create a file without execute permission.
	tmp := filepath.Join(t.TempDir(), "not-executable")
	require.NoError(t, os.WriteFile(tmp, []byte("not a binary"), 0o600))
	desc, err := ProbePluginDescriptor(context.Background(), tmp, "test")
	assert.Nil(t, desc)
	assert.Error(t, err)
}

func TestProbePluginDescriptor_CancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately
	missingPath := filepath.Join(t.TempDir(), "nonexistent", "binary")
	desc, err := ProbePluginDescriptor(ctx, missingPath, "test")
	assert.Nil(t, desc)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled, "should short-circuit on cancelled context before starting binary")
}

var (
	echoPluginOnce sync.Once
	echoPluginPath string
)

// buildEchoPlugin compiles the echo example plugin once for reuse.
func buildEchoPlugin(t *testing.T) string {
	t.Helper()
	echoPluginOnce.Do(func() {
		// pkg/plugin/ is 2 levels from root.
		projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			t.Fatalf("failed to resolve project root: %v", err)
		}
		tmpDir, err := os.MkdirTemp("", "scafctl-echo-plugin-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		binName := "scafctl-plugin-echo"
		if runtime.GOOS == "windows" {
			binName += ".exe"
		}
		binPath := filepath.Join(tmpDir, binName)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./examples/plugins/echo/main.go")
		cmd.Dir = projectRoot
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("failed to build echo plugin: %v\n%s", err, output)
		}
		echoPluginPath = binPath
	})
	require.NotEmpty(t, echoPluginPath, "echo plugin build failed")
	return echoPluginPath
}

func TestProbePluginDescriptor_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping plugin probe test in short mode (requires go build)")
	}
	echoPath := buildEchoPlugin(t)

	desc, err := ProbePluginDescriptor(context.Background(), echoPath, "echo")
	require.NoError(t, err)
	require.NotNil(t, desc)
	assert.Equal(t, "echo", desc.Name)
	assert.Equal(t, "Echo Provider", desc.DisplayName)
	assert.NotEmpty(t, desc.Description)
	assert.NotNil(t, desc.Schema)
}

func TestProbePluginDescriptor_ProviderNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping plugin probe test in short mode (requires go build)")
	}
	echoPath := buildEchoPlugin(t)

	desc, err := ProbePluginDescriptor(context.Background(), echoPath, "nonexistent")
	assert.Nil(t, desc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found in plugin")
}

func TestProbePluginDescription_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping plugin probe test in short mode (requires go build)")
	}
	echoPath := buildEchoPlugin(t)

	desc, ok := ProbePluginDescription(context.Background(), echoPath, "echo")
	assert.True(t, ok)
	assert.NotEmpty(t, desc)
}

func TestDescribeCachedPlugins_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping plugin probe test in short mode (requires go build)")
	}
	echoPath := buildEchoPlugin(t)

	cached := []CachedPlugin{
		{Name: "echo", Version: "1.0.0", Path: echoPath},
	}

	result := DescribeCachedPlugins(context.Background(), cached, nil)
	require.Len(t, result, 1)
	assert.Equal(t, "echo", result[0].Name)
	assert.Equal(t, "1.0.0", result[0].Version)
	assert.NotEmpty(t, result[0].Description)
}
