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

func TestHandleCatalogInspect(t *testing.T) {
	t.Run("missing reference", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{"kind": "solution"}
		result, err := srv.handleCatalogInspect(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("missing kind", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{"reference": "my-solution"}
		result, err := srv.handleCatalogInspect(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("invalid kind", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{"reference": "my-solution", "kind": "invalid"}
		result, err := srv.handleCatalogInspect(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := extractText(t, result)
		assert.Contains(t, text, "invalid kind")
	})

	t.Run("nonexistent artifact", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{"reference": "nonexistent-solution-12345", "kind": "solution"}
		result, err := srv.handleCatalogInspect(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := extractText(t, result)
		assert.Contains(t, text, "not found")
	})
}

func TestHandleListAuthHandlers(t *testing.T) {
	t.Run("no auth registry", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		request := mcp.CallToolRequest{}
		result, err := srv.handleListAuthHandlers(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))
		handlers := data["handlers"].([]any)
		assert.Empty(t, handlers)
	})
}

func TestHandleGetConfigPaths(t *testing.T) {
	t.Run("returns all paths", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		request := mcp.CallToolRequest{}
		result, err := srv.handleGetConfigPaths(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))
		pathList := data["paths"].([]any)
		assert.GreaterOrEqual(t, len(pathList), 8)
		count := data["count"].(float64)
		assert.Equal(t, float64(len(pathList)), count)
		names := make(map[string]bool)
		for _, p := range pathList {
			entry := p.(map[string]any)
			names[entry["name"].(string)] = true
			assert.NotEmpty(t, entry["path"])
			assert.NotEmpty(t, entry["description"])
			assert.NotEmpty(t, entry["xdgVariable"])
		}
		assert.True(t, names["config"])
		assert.True(t, names["data"])
		assert.True(t, names["cache"])
		assert.True(t, names["catalog"])
		assert.True(t, names["secrets"])
	})
}

func TestHandleValidateExpressions(t *testing.T) {
	t.Run("all valid", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"expressions": []any{
				map[string]any{"expression": "1 + 2", "type": "cel"},
				map[string]any{"expression": "Hello {{ .Name }}", "type": "go-template"},
			},
		}
		result, err := srv.handleValidateExpressions(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))
		results := data["results"].([]any)
		assert.Len(t, results, 2)
		summary := data["summary"].(map[string]any)
		assert.Equal(t, float64(2), summary["total"])
		assert.Equal(t, float64(2), summary["valid"])
		assert.Equal(t, float64(0), summary["invalid"])
	})

	t.Run("mixed valid and invalid", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"expressions": []any{
				map[string]any{"expression": "1 + 2", "type": "cel"},
				map[string]any{"expression": "{{ .Name", "type": "go-template"},
				map[string]any{"expression": "1 +++ 2", "type": "cel"},
			},
		}
		result, err := srv.handleValidateExpressions(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))
		summary := data["summary"].(map[string]any)
		assert.Equal(t, float64(3), summary["total"])
		assert.Equal(t, float64(1), summary["valid"])
		assert.Equal(t, float64(2), summary["invalid"])
	})

	t.Run("empty expressions", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{"expressions": []any{}}
		result, err := srv.handleValidateExpressions(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("missing expressions", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{}
		result, err := srv.handleValidateExpressions(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("unknown expression type", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"expressions": []any{
				map[string]any{"expression": "test", "type": "javascript"},
			},
		}
		result, err := srv.handleValidateExpressions(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))
		results := data["results"].([]any)
		r0 := results[0].(map[string]any)
		assert.Equal(t, false, r0["valid"])
		assert.Contains(t, r0["error"], "unknown expression type")
	})
}
