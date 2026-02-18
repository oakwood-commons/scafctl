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

func TestHandleGetSolutionSchema(t *testing.T) {
	t.Run("returns full schema", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_solution_schema"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleGetSolutionSchema(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		// Verify it's valid JSON with expected top-level fields
		text := extractText(t, result)
		var doc map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &doc))
		assert.Equal(t, "scafctl Solution", doc["title"])
		assert.Equal(t, "object", doc["type"])

		props, ok := doc["properties"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, props, "apiVersion")
		assert.Contains(t, props, "kind")
		assert.Contains(t, props, "metadata")
		assert.Contains(t, props, "spec")
	})

	t.Run("returns field-specific schema", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_solution_schema"
		request.Params.Arguments = map[string]any{
			"field": "metadata",
		}

		result, err := srv.handleGetSolutionSchema(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var doc map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &doc))
		// Metadata should have properties like name, version
		props, ok := doc["properties"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, props, "name")
		assert.Contains(t, props, "version")
	})

	t.Run("returns error for unknown field", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_solution_schema"
		request.Params.Arguments = map[string]any{
			"field": "nonexistent",
		}

		result, err := srv.handleGetSolutionSchema(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.IsError)
	})
}

func TestHandleExplainKind(t *testing.T) {
	t.Run("returns solution kind info", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "explain_kind"
		request.Params.Arguments = map[string]any{
			"kind": "solution",
		}

		result, err := srv.handleExplainKind(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)
	})

	t.Run("returns resolver kind info", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "explain_kind"
		request.Params.Arguments = map[string]any{
			"kind": "resolver",
		}

		result, err := srv.handleExplainKind(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)
	})

	t.Run("returns specific field info", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "explain_kind"
		request.Params.Arguments = map[string]any{
			"kind":  "solution",
			"field": "metadata",
		}

		result, err := srv.handleExplainKind(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)
	})

	t.Run("returns error for unknown kind", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "explain_kind"
		request.Params.Arguments = map[string]any{
			"kind": "nonexistent",
		}

		result, err := srv.handleExplainKind(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("returns error when kind is missing", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "explain_kind"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleExplainKind(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.IsError)
	})
}

// extractText extracts the text content from a tool result.
func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content)
	tc, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "expected TextContent")
	return tc.Text
}
