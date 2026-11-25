// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleCatalogList(t *testing.T) {
	t.Run("list all kinds", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "catalog_list"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleCatalogList(context.Background(), request)
		require.NoError(t, err)
		// May or may not be an error depending on catalog existence
		// but it should not be a Go error
		assert.NotNil(t, result)
	})

	t.Run("list by kind solution", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "catalog_list"
		request.Params.Arguments = map[string]any{
			"kind": "solution",
		}

		result, err := srv.handleCatalogList(context.Background(), request)
		require.NoError(t, err)
		assert.NotNil(t, result)

		// If not error, verify structure
		if !result.IsError {
			text := result.Content[0].(mcp.TextContent).Text
			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(text), &parsed))
			assert.Equal(t, "solution", parsed["kind"])
			assert.Contains(t, parsed, "count")
		}
	})

	t.Run("invalid kind returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "catalog_list"
		request.Params.Arguments = map[string]any{
			"kind": "invalid-kind",
		}

		result, err := srv.handleCatalogList(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "invalid kind")
	})

	t.Run("list by name filter", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "catalog_list"
		request.Params.Arguments = map[string]any{
			"kind": "solution",
			"name": "nonexistent-solution-xyz",
		}

		result, err := srv.handleCatalogList(context.Background(), request)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestHandleCatalogInspect_PreRelease(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDir)

	localCat, err := catalog.NewLocalCatalog(logr.Discard())
	require.NoError(t, err)

	for _, ver := range []string{"0.4.0", "0.5.0-dev"} {
		_, storeErr := localCat.StoreMultiPlatform(context.Background(), catalog.Reference{
			Kind:    catalog.ArtifactKindProvider,
			Name:    "exec",
			Version: semver.MustParse(ver),
		}, []catalog.PlatformBinary{
			{Platform: "linux/amd64", Data: []byte("exec-" + ver)},
		}, nil, false)
		require.NoError(t, storeErr)
	}

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	callInspect := func(args map[string]any) *mcp.CallToolResult {
		t.Helper()
		req := mcp.CallToolRequest{}
		req.Params.Name = "catalog_inspect"
		req.Params.Arguments = args
		res, callErr := srv.handleCatalogInspect(context.Background(), req)
		require.NoError(t, callErr)
		require.NotNil(t, res)
		return res
	}

	t.Run("unpinned excludes pre-release by default", func(t *testing.T) {
		res := callInspect(map[string]any{
			"reference": "exec",
			"kind":      "provider",
		})
		require.False(t, res.IsError)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed))
		assert.Equal(t, "provider", parsed["kind"])
		assert.Equal(t, "exec", parsed["name"])
		assert.Equal(t, "0.4.0", parsed["version"])
		assert.Equal(t, true, parsed["isMultiPlatform"])
	})

	t.Run("unpinned includes pre-release when pre_release is true", func(t *testing.T) {
		res := callInspect(map[string]any{
			"reference":   "exec",
			"kind":        "provider",
			"pre_release": true,
		})
		require.False(t, res.IsError)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed))
		assert.Equal(t, "0.5.0-dev", parsed["version"])
		assert.Equal(t, true, parsed["isMultiPlatform"])
	})

	t.Run("exact pre-release version resolves without pre_release flag", func(t *testing.T) {
		res := callInspect(map[string]any{
			"reference": "exec@0.5.0-dev",
			"kind":      "provider",
		})
		require.False(t, res.IsError)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &parsed))
		assert.Equal(t, "0.5.0-dev", parsed["version"])
	})

	t.Run("validation and not-found errors", func(t *testing.T) {
		assert.True(t, callInspect(map[string]any{"kind": "provider"}).IsError)
		assert.True(t, callInspect(map[string]any{"reference": "exec"}).IsError)
		assert.True(t, callInspect(map[string]any{"reference": "exec", "kind": "bad"}).IsError)
		assert.True(t, callInspect(map[string]any{"reference": "@@@", "kind": "provider"}).IsError)
		assert.True(t, callInspect(map[string]any{"reference": "missing", "kind": "provider"}).IsError)
	})
}
