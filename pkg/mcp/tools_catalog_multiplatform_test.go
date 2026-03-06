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
	"github.com/go-logr/logr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setTestXDG sets XDG_DATA_HOME and XDG_CACHE_HOME to tmpDir and reloads xdg
// so that NewLocalCatalog() picks up the override. Also restores xdg on cleanup.
func setTestXDG(t *testing.T, tmpDir string) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	xdg.Reload()
	t.Cleanup(func() { xdg.Reload() })
}

func TestHandleCatalogListPlatforms(t *testing.T) {
	t.Run("missing reference returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "catalog_list_platforms"
		request.Params.Arguments = map[string]any{
			"kind": "provider",
		}

		result, err := srv.handleCatalogListPlatforms(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("missing kind returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "catalog_list_platforms"
		request.Params.Arguments = map[string]any{
			"reference": "my-provider@1.0.0",
		}

		result, err := srv.handleCatalogListPlatforms(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("invalid kind returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "catalog_list_platforms"
		request.Params.Arguments = map[string]any{
			"reference": "my-provider@1.0.0",
			"kind":      "invalid",
		}

		result, err := srv.handleCatalogListPlatforms(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "invalid kind")
	})

	t.Run("nonexistent artifact returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		setTestXDG(t, tmpDir)

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "catalog_list_platforms"
		request.Params.Arguments = map[string]any{
			"reference": "nonexistent@1.0.0",
			"kind":      "provider",
		}

		result, err := srv.handleCatalogListPlatforms(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

func TestHandleBuildPlugin(t *testing.T) {
	t.Run("missing name returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "build_plugin"
		request.Params.Arguments = map[string]any{
			"kind":    "provider",
			"version": "1.0.0",
		}

		result, err := srv.handleBuildPlugin(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("invalid kind returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "build_plugin"
		request.Params.Arguments = map[string]any{
			"name":    "test-plugin",
			"kind":    "solution",
			"version": "1.0.0",
		}

		result, err := srv.handleBuildPlugin(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "invalid kind")
	})

	t.Run("invalid version returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "build_plugin"
		request.Params.Arguments = map[string]any{
			"name":    "test-plugin",
			"kind":    "provider",
			"version": "not-a-version",
		}

		result, err := srv.handleBuildPlugin(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "invalid semantic version")
	})

	t.Run("missing platforms returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "build_plugin"
		request.Params.Arguments = map[string]any{
			"name":    "test-plugin",
			"kind":    "provider",
			"version": "1.0.0",
		}

		result, err := srv.handleBuildPlugin(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "platforms")
	})

	t.Run("unsupported platform returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		binPath := filepath.Join(tmpDir, "binary")
		require.NoError(t, os.WriteFile(binPath, []byte("data"), 0o755))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "build_plugin"
		request.Params.Arguments = map[string]any{
			"name":    "test-plugin",
			"kind":    "provider",
			"version": "1.0.0",
			"platforms": map[string]any{
				"freebsd/amd64": binPath,
			},
		}

		result, err := srv.handleBuildPlugin(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "unsupported platform")
	})

	t.Run("binary not found returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "build_plugin"
		request.Params.Arguments = map[string]any{
			"name":    "test-plugin",
			"kind":    "provider",
			"version": "1.0.0",
			"platforms": map[string]any{
				"linux/amd64": "/nonexistent/binary",
			},
		}

		result, err := srv.handleBuildPlugin(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "binary not found")
	})

	t.Run("successful single-platform build", func(t *testing.T) {
		tmpDir := t.TempDir()
		setTestXDG(t, tmpDir)

		binPath := filepath.Join(tmpDir, "my-binary")
		require.NoError(t, os.WriteFile(binPath, []byte("fake-plugin-binary"), 0o755))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "build_plugin"
		request.Params.Arguments = map[string]any{
			"name":    "mcp-test-provider",
			"kind":    "provider",
			"version": "1.0.0",
			"platforms": map[string]any{
				"linux/amd64": binPath,
			},
		}

		result, err := srv.handleBuildPlugin(context.Background(), request)
		require.NoError(t, err)
		require.False(t, result.IsError, "expected success, got: %+v", result.Content)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, "mcp-test-provider", parsed["name"])
		assert.Equal(t, "provider", parsed["kind"])
		assert.Equal(t, "1.0.0", parsed["version"])
		assert.Equal(t, float64(1), parsed["platformCount"])
	})

	t.Run("successful multi-platform build", func(t *testing.T) {
		tmpDir := t.TempDir()
		setTestXDG(t, tmpDir)

		linuxBin := filepath.Join(tmpDir, "linux-bin")
		darwinBin := filepath.Join(tmpDir, "darwin-bin")
		require.NoError(t, os.WriteFile(linuxBin, []byte("linux-binary"), 0o755))
		require.NoError(t, os.WriteFile(darwinBin, []byte("darwin-binary"), 0o755))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "build_plugin"
		request.Params.Arguments = map[string]any{
			"name":    "mcp-multi-provider",
			"kind":    "provider",
			"version": "1.0.0",
			"platforms": map[string]any{
				"linux/amd64":  linuxBin,
				"darwin/arm64": darwinBin,
			},
		}

		result, err := srv.handleBuildPlugin(context.Background(), request)
		require.NoError(t, err)
		require.False(t, result.IsError, "expected success, got: %+v", result.Content)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, "mcp-multi-provider", parsed["name"])
		assert.Equal(t, float64(2), parsed["platformCount"])
	})

	t.Run("duplicate version without force returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		setTestXDG(t, tmpDir)

		binPath := filepath.Join(tmpDir, "binary")
		require.NoError(t, os.WriteFile(binPath, []byte("binary-data"), 0o755))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		buildReq := mcp.CallToolRequest{}
		buildReq.Params.Name = "build_plugin"
		buildReq.Params.Arguments = map[string]any{
			"name":    "dup-provider",
			"kind":    "provider",
			"version": "1.0.0",
			"platforms": map[string]any{
				"linux/amd64": binPath,
			},
		}

		// First build
		result, err := srv.handleBuildPlugin(context.Background(), buildReq)
		require.NoError(t, err)
		require.False(t, result.IsError)

		// Second build without force
		result, err = srv.handleBuildPlugin(context.Background(), buildReq)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "already exists")
	})

	t.Run("force overwrite succeeds", func(t *testing.T) {
		tmpDir := t.TempDir()
		setTestXDG(t, tmpDir)

		binPath := filepath.Join(tmpDir, "binary")
		require.NoError(t, os.WriteFile(binPath, []byte("binary-data"), 0o755))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		buildReq := mcp.CallToolRequest{}
		buildReq.Params.Name = "build_plugin"
		buildReq.Params.Arguments = map[string]any{
			"name":    "force-provider",
			"kind":    "provider",
			"version": "1.0.0",
			"platforms": map[string]any{
				"linux/amd64": binPath,
			},
		}

		// First build
		result, err := srv.handleBuildPlugin(context.Background(), buildReq)
		require.NoError(t, err)
		require.False(t, result.IsError)

		// Force overwrite — create new request with force=true
		forceReq := mcp.CallToolRequest{}
		forceReq.Params.Name = "build_plugin"
		forceReq.Params.Arguments = map[string]any{
			"name":    "force-provider",
			"kind":    "provider",
			"version": "1.0.0",
			"platforms": map[string]any{
				"linux/amd64": binPath,
			},
			"force": true,
		}
		result, err = srv.handleBuildPlugin(context.Background(), forceReq)
		require.NoError(t, err)
		assert.False(t, result.IsError)
	})

	t.Run("build then list platforms round-trip", func(t *testing.T) {
		tmpDir := t.TempDir()
		setTestXDG(t, tmpDir)

		linuxBin := filepath.Join(tmpDir, "linux-bin")
		darwinBin := filepath.Join(tmpDir, "darwin-bin")
		require.NoError(t, os.WriteFile(linuxBin, []byte("linux-binary"), 0o755))
		require.NoError(t, os.WriteFile(darwinBin, []byte("darwin-binary"), 0o755))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		// Build multi-platform plugin
		buildReq := mcp.CallToolRequest{}
		buildReq.Params.Name = "build_plugin"
		buildReq.Params.Arguments = map[string]any{
			"name":    "round-trip-provider",
			"kind":    "provider",
			"version": "1.0.0",
			"platforms": map[string]any{
				"linux/amd64":  linuxBin,
				"darwin/arm64": darwinBin,
			},
		}

		result, err := srv.handleBuildPlugin(context.Background(), buildReq)
		require.NoError(t, err)
		require.False(t, result.IsError, "build failed: %+v", result.Content)

		// List platforms
		listReq := mcp.CallToolRequest{}
		listReq.Params.Name = "catalog_list_platforms"
		listReq.Params.Arguments = map[string]any{
			"reference": "round-trip-provider@1.0.0",
			"kind":      "provider",
		}

		result, err = srv.handleCatalogListPlatforms(context.Background(), listReq)
		require.NoError(t, err)
		require.False(t, result.IsError, "list platforms failed: %+v", result.Content)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, true, parsed["isMultiPlatform"])
		assert.Equal(t, float64(2), parsed["platformCount"])

		platforms := parsed["platforms"].([]any)
		platformStrs := make([]string, len(platforms))
		for i, p := range platforms {
			platformStrs[i] = p.(string)
		}
		assert.Contains(t, platformStrs, "linux/amd64")
		assert.Contains(t, platformStrs, "darwin/arm64")
	})

	t.Run("auth-handler kind works", func(t *testing.T) {
		tmpDir := t.TempDir()
		setTestXDG(t, tmpDir)

		binPath := filepath.Join(tmpDir, "auth-handler")
		require.NoError(t, os.WriteFile(binPath, []byte("auth-binary"), 0o755))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "build_plugin"
		request.Params.Arguments = map[string]any{
			"name":    "test-auth",
			"kind":    "auth-handler",
			"version": "1.0.0",
			"platforms": map[string]any{
				"darwin/arm64": binPath,
			},
		}

		result, err := srv.handleBuildPlugin(context.Background(), request)
		require.NoError(t, err)
		require.False(t, result.IsError, "expected success, got: %+v", result.Content)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, "test-auth", parsed["name"])
		assert.Equal(t, "auth-handler", parsed["kind"])
	})
}

func TestCatalogInspect_MultiPlatformInfo(t *testing.T) {
	t.Run("inspect multi-platform artifact shows platforms", func(t *testing.T) {
		tmpDir := t.TempDir()
		setTestXDG(t, tmpDir)

		// Build a multi-platform artifact directly via catalog
		localCatalog, err := catalog.NewLocalCatalog(logr.Discard())
		require.NoError(t, err)

		ref, err := catalog.ParseReference(catalog.ArtifactKindProvider, "inspect-mp-test@1.0.0")
		require.NoError(t, err)

		_, err = localCatalog.StoreMultiPlatform(context.Background(), ref, []catalog.PlatformBinary{
			{Platform: "linux/amd64", Data: []byte("linux-binary")},
			{Platform: "darwin/arm64", Data: []byte("darwin-binary")},
		}, nil, false)
		require.NoError(t, err)

		// Now inspect via MCP
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "catalog_inspect"
		request.Params.Arguments = map[string]any{
			"reference": "inspect-mp-test@1.0.0",
			"kind":      "provider",
		}

		result, err := srv.handleCatalogInspect(context.Background(), request)
		require.NoError(t, err)
		require.False(t, result.IsError, "expected success, got: %+v", result.Content)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))

		assert.Equal(t, true, parsed["isMultiPlatform"])
		assert.Equal(t, float64(2), parsed["platformCount"])

		platforms := parsed["platforms"].([]any)
		platformStrs := make([]string, len(platforms))
		for i, p := range platforms {
			platformStrs[i] = p.(string)
		}
		assert.Contains(t, platformStrs, "linux/amd64")
		assert.Contains(t, platformStrs, "darwin/arm64")
	})
}
