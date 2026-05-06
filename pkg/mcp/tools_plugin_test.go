// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleListPlugins(t *testing.T) {
	t.Run("returns empty when no plugins cached", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", tmpDir)
		xdg.Reload()

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_plugins"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListPlugins(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		// Should indicate no plugins found
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "No cached plugins")
	})

	t.Run("returns plugins when cache populated", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		xdg.Reload()

		// Create cache structure under an isolated plugin cache dir
		cacheDir := paths.PluginCacheDir()
		pluginDir := filepath.Join(cacheDir, "mcp-test-provider", "1.0.0", "linux-amd64")
		require.NoError(t, os.MkdirAll(pluginDir, 0o755))
		binPath := filepath.Join(pluginDir, "mcp-test-provider")
		require.NoError(t, os.WriteFile(binPath, []byte("binary"), 0o755))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_plugins"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListPlugins(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var plugins []plugin.CachedPlugin
		require.NoError(t, json.Unmarshal([]byte(text), &plugins))

		// Find our test plugin in the results
		var found bool
		for _, p := range plugins {
			if p.Name == "mcp-test-provider" {
				found = true
				assert.Equal(t, "1.0.0", p.Version)
				assert.Equal(t, "linux/amd64", p.Platform)
				break
			}
		}
		assert.True(t, found, "expected mcp-test-provider in plugin list")
	})
}

func TestHandleGetPluginCachePath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "get_plugin_cache_path"
	request.Params.Arguments = map[string]any{}

	result, err := srv.handleGetPluginCachePath(context.Background(), request)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	text := result.Content[0].(mcp.TextContent).Text
	assert.Contains(t, text, "plugins")
}

func TestHandleListOfficialProviders(t *testing.T) {
	t.Run("returns default official providers when no registry in context", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_official_providers"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListOfficialProviders(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var items []officialProviderItem
		require.NoError(t, json.Unmarshal([]byte(text), &items))

		// Should contain the 10 default official providers
		assert.Len(t, items, 10)

		// Verify they are sorted alphabetically
		names := make([]string, len(items))
		for i, item := range items {
			names[i] = item.Name
		}
		assert.Equal(t, "directory", names[0])
		assert.Equal(t, "sleep", names[len(names)-1])

		// Verify structure
		for _, item := range items {
			assert.NotEmpty(t, item.Name)
			assert.NotEmpty(t, item.CatalogRef)
			assert.NotEmpty(t, item.DefaultVersion)
		}
	})

	t.Run("uses registry from context when available", func(t *testing.T) {
		customProviders := []official.Provider{
			{Name: "custom-one", CatalogRef: "custom-one", DefaultVersion: "^2.0.0"},
			{Name: "custom-two", CatalogRef: "my-custom-two", DefaultVersion: "latest"},
		}
		reg := official.NewRegistryFrom(customProviders)
		ctx := official.WithRegistry(context.Background(), reg)

		srv, err := NewServer(WithServerVersion("test"), WithServerContext(ctx))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_official_providers"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListOfficialProviders(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var items []officialProviderItem
		require.NoError(t, json.Unmarshal([]byte(text), &items))

		assert.Len(t, items, 2)
		assert.Equal(t, "custom-one", items[0].Name)
		assert.Equal(t, "custom-one", items[0].CatalogRef)
		assert.Equal(t, "^2.0.0", items[0].DefaultVersion)
		assert.Equal(t, "custom-two", items[1].Name)
		assert.Equal(t, "my-custom-two", items[1].CatalogRef)
	})
}
