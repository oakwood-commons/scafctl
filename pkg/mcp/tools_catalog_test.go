// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
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
