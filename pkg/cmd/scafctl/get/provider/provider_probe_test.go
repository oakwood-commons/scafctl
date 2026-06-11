// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	echoPluginOnce sync.Once
	echoPluginPath string
)

// buildEchoPlugin compiles the echo example plugin and returns its path.
// The binary is built once and reused across tests.
func buildEchoPlugin(t *testing.T) string {
	t.Helper()
	echoPluginOnce.Do(func() {
		// Project root is 5 levels up from pkg/cmd/scafctl/get/provider/
		projectRoot, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
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

func TestOptions_RunGetProvider_OfficialProviderProbeSuccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping plugin probe test in short mode (requires go build)")
	}

	echoPath := buildEchoPlugin(t)

	t.Run("returns_full_schema_for_official_provider_structured", func(t *testing.T) {
		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		xdg.Reload()

		// Place the echo plugin in the expected cache location.
		platform := runtime.GOOS + "-" + runtime.GOARCH
		binDir := filepath.Join(cacheDir, "scafctl", "plugins", "echo-plugin", "1.0.0", platform)
		require.NoError(t, os.MkdirAll(binDir, 0o755))
		cachedBin := filepath.Join(binDir, pluginBinaryName("echo-plugin"))
		// Copy the built echo plugin binary to the cache location.
		data, err := os.ReadFile(echoPath)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(cachedBin, data, 0o755))

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{BinaryName: "scafctl"}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			BinaryName:     "scafctl",
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		}

		officialReg := official.NewRegistryFrom([]official.Provider{
			{Name: "echo", CatalogRef: "echo-plugin", DefaultVersion: "1.0.0", Description: "Echo provider"},
		})

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err = options.RunGetProvider(ctx, "echo")
		require.NoError(t, err)

		output := outBuf.String()
		// Should return full schema from probed plugin, not a hint.
		assert.NotContains(t, output, "hint", "should not fallback to hint when probe succeeds")
		assert.Contains(t, output, "echo")
		assert.Contains(t, output, "official")

		// Verify it's valid JSON with schema fields.
		var result map[string]any
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		assert.Equal(t, "official", result["source"])
		assert.Equal(t, "1.0.0", result["version"])
	})

	t.Run("returns_full_schema_for_official_provider_default_output", func(t *testing.T) {
		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		xdg.Reload()

		platform := runtime.GOOS + "-" + runtime.GOARCH
		binDir := filepath.Join(cacheDir, "scafctl", "plugins", "echo-plugin", "1.0.0", platform)
		require.NoError(t, os.MkdirAll(binDir, 0o755))
		cachedBin := filepath.Join(binDir, pluginBinaryName("echo-plugin"))
		data, err := os.ReadFile(echoPath)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(cachedBin, data, 0o755))

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{BinaryName: "scafctl"}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			BinaryName:     "scafctl",
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: ""},
		}

		officialReg := official.NewRegistryFrom([]official.Provider{
			{Name: "echo", CatalogRef: "echo-plugin", DefaultVersion: "1.0.0", Description: "Echo provider"},
		})

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)
		ctx = official.WithRegistry(ctx, officialReg)

		err = options.RunGetProvider(ctx, "echo")
		require.NoError(t, err)

		output := outBuf.String()
		// Default output shows the detailed provider view.
		assert.Contains(t, output, "echo")
		assert.NotContains(t, output, "plugins install", "should not show install hint when probe succeeds")
	})
}

func TestOptions_RunGetProvider_CacheProbeSuccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping plugin probe test in short mode (requires go build)")
	}

	echoPath := buildEchoPlugin(t)

	t.Run("returns_full_schema_for_cached_plugin_structured", func(t *testing.T) {
		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		xdg.Reload()

		// Place the echo plugin under its own name in the cache.
		platform := runtime.GOOS + "-" + runtime.GOARCH
		binDir := filepath.Join(cacheDir, "scafctl", "plugins", "echo", "2.0.0", platform)
		require.NoError(t, os.MkdirAll(binDir, 0o755))
		cachedBin := filepath.Join(binDir, pluginBinaryName("echo"))
		data, err := os.ReadFile(echoPath)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(cachedBin, data, 0o755))

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{BinaryName: "scafctl"}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			BinaryName:     "scafctl",
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		}

		// No official registry — goes to cache fallback.
		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		err = options.RunGetProvider(ctx, "echo")
		require.NoError(t, err)

		output := outBuf.String()
		var result map[string]any
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		assert.Equal(t, "local", result["source"])
		assert.Equal(t, "2.0.0", result["version"])
		assert.Contains(t, output, "echo")
	})

	t.Run("returns_full_detail_for_cached_plugin_default_output", func(t *testing.T) {
		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		xdg.Reload()

		platform := runtime.GOOS + "-" + runtime.GOARCH
		binDir := filepath.Join(cacheDir, "scafctl", "plugins", "echo", "2.0.0", platform)
		require.NoError(t, os.MkdirAll(binDir, 0o755))
		cachedBin := filepath.Join(binDir, pluginBinaryName("echo"))
		data, err := os.ReadFile(echoPath)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(cachedBin, data, 0o755))

		var outBuf bytes.Buffer
		ioStreams := &terminal.IOStreams{Out: &outBuf, ErrOut: &outBuf}
		cliParams := &settings.Run{BinaryName: "scafctl"}
		reg := provider.NewRegistry(provider.WithAllowOverwrite(true))

		options := &Options{
			IOStreams:      ioStreams,
			CliParams:      cliParams,
			BinaryName:     "scafctl",
			registry:       reg,
			KvxOutputFlags: flags.KvxOutputFlags{Output: ""},
		}

		w := writer.New(ioStreams, cliParams)
		ctx := writer.WithWriter(context.Background(), w)

		err = options.RunGetProvider(ctx, "echo")
		require.NoError(t, err)

		output := outBuf.String()
		// Default output prints the detailed provider view.
		assert.Contains(t, output, "echo")
		assert.Contains(t, output, "Echo Provider")
	})
}
