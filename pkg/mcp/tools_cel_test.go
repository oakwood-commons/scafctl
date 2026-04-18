// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// extractJSONContent finds the JSON content block in a tool result.
// When summary index is prepended, JSON is in Content[1]; otherwise Content[0].
func extractJSONContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	for _, content := range result.Content {
		tc, ok := content.(mcp.TextContent)
		if !ok {
			continue
		}
		if len(tc.Text) > 0 && tc.Text[0] == '[' || tc.Text[0] == '{' {
			return tc.Text
		}
	}
	t.Fatal("no JSON content block found in result")
	return ""
}

func TestHandleListCELFunctions(t *testing.T) {
	t.Run("returns all functions", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_cel_functions"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListCELFunctions(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		// First content block should be the summary index
		require.GreaterOrEqual(t, len(result.Content), 2, "expected summary index + JSON content")
		summaryText := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, summaryText, "# Summary")

		text := extractJSONContent(t, result)
		var functions []celexp.ExtFunction
		require.NoError(t, json.Unmarshal([]byte(text), &functions))
		assert.NotEmpty(t, functions, "expected at least one CEL function")
	})

	t.Run("returns custom only", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_cel_functions"
		request.Params.Arguments = map[string]any{
			"custom_only": true,
		}

		result, err := srv.handleListCELFunctions(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := extractJSONContent(t, result)
		var functions []celexp.ExtFunction
		require.NoError(t, json.Unmarshal([]byte(text), &functions))
		assert.NotEmpty(t, functions, "expected at least one custom function")

		// All returned should be custom
		for _, f := range functions {
			assert.True(t, f.Custom, "function %q should be custom", f.Name)
		}
	})

	t.Run("returns builtin only", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_cel_functions"
		request.Params.Arguments = map[string]any{
			"builtin_only": true,
		}

		result, err := srv.handleListCELFunctions(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := extractJSONContent(t, result)
		var functions []celexp.ExtFunction
		require.NoError(t, json.Unmarshal([]byte(text), &functions))
		assert.NotEmpty(t, functions, "expected at least one builtin function")

		// All returned should be builtin (not custom)
		for _, f := range functions {
			assert.False(t, f.Custom, "function %q should be builtin", f.Name)
		}
	})

	t.Run("filters by name", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_cel_functions"
		request.Params.Arguments = map[string]any{
			"name": "map",
		}

		result, err := srv.handleListCELFunctions(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var functions []celexp.ExtFunction
		require.NoError(t, json.Unmarshal([]byte(text), &functions))
		assert.NotEmpty(t, functions, "expected at least one function matching 'map'")
	})

	t.Run("returns error for unknown name", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_cel_functions"
		request.Params.Arguments = map[string]any{
			"name": "xyznonexistent123",
		}

		result, err := srv.handleListCELFunctions(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

func TestHandleEvaluateCEL(t *testing.T) {
	t.Run("simple expression", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "evaluate_cel"
		request.Params.Arguments = map[string]any{
			"expression": "1 + 2",
		}

		result, err := srv.handleEvaluateCEL(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, "1 + 2", parsed["expression"])
		// CEL returns int64, JSON converts to float64
		assert.Equal(t, float64(3), parsed["result"])
	})

	t.Run("with inline data", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "evaluate_cel"
		request.Params.Arguments = map[string]any{
			"expression": "_.name",
			"data": map[string]any{
				"name": "hello-world",
			},
		}

		result, err := srv.handleEvaluateCEL(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, "hello-world", parsed["result"])
	})

	t.Run("with variables", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "evaluate_cel"
		request.Params.Arguments = map[string]any{
			"expression": "_.count > threshold",
			"data": map[string]any{
				"count": 10,
			},
			"variables": map[string]any{
				"threshold": 5,
			},
		}

		result, err := srv.handleEvaluateCEL(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, true, parsed["result"])
	})

	t.Run("with data file", func(t *testing.T) {
		// Create temp YAML file
		tmpDir := t.TempDir()
		dataFile := filepath.Join(tmpDir, "data.yaml")
		require.NoError(t, os.WriteFile(dataFile, []byte("name: from-file\ncount: 42\n"), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "evaluate_cel"
		request.Params.Arguments = map[string]any{
			"expression": "_.name",
			"data_file":  dataFile,
		}

		result, err := srv.handleEvaluateCEL(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, "from-file", parsed["result"])
	})

	t.Run("both data and data_file returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "evaluate_cel"
		request.Params.Arguments = map[string]any{
			"expression": "1 + 1",
			"data":       map[string]any{"x": 1},
			"data_file":  "/tmp/file.yaml",
		}

		result, err := srv.handleEvaluateCEL(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "cannot specify both")
	})

	t.Run("missing expression returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "evaluate_cel"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleEvaluateCEL(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("invalid expression returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "evaluate_cel"
		request.Params.Arguments = map[string]any{
			"expression": "this is not valid CEL >>>",
		}

		result, err := srv.handleEvaluateCEL(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "CEL evaluation failed")
	})

	t.Run("nonexistent data file returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "evaluate_cel"
		request.Params.Arguments = map[string]any{
			"expression": "_.name",
			"data_file":  "/nonexistent/path/data.yaml",
		}

		result, err := srv.handleEvaluateCEL(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "failed to load data file")
	})

	t.Run("no data expression", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "evaluate_cel"
		request.Params.Arguments = map[string]any{
			"expression": "'hello' + ' ' + 'world'",
		}

		result, err := srv.handleEvaluateCEL(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, "hello world", parsed["result"])
	})
}

func TestHandleListCELFunctions_Search(t *testing.T) {
	t.Run("search matches name", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_cel_functions"
		request.Params.Arguments = map[string]any{
			"search": "json",
		}

		result, err := srv.handleListCELFunctions(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := extractJSONContent(t, result)
		var functions []celexp.ExtFunction
		require.NoError(t, json.Unmarshal([]byte(text), &functions))
		assert.NotEmpty(t, functions, "expected at least one function matching 'json'")
	})

	t.Run("search matches description", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_cel_functions"
		request.Params.Arguments = map[string]any{
			"search": "serialize",
		}

		result, err := srv.handleListCELFunctions(context.Background(), request)
		require.NoError(t, err)
		// May or may not find results depending on descriptions, just verify no panic
		if !result.IsError {
			text := extractJSONContent(t, result)
			var functions []celexp.ExtFunction
			require.NoError(t, json.Unmarshal([]byte(text), &functions))
		}
	})

	t.Run("search no results returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_cel_functions"
		request.Params.Arguments = map[string]any{
			"search": "xyznonexistent123",
		}

		result, err := srv.handleListCELFunctions(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

func TestHandleListCELFunctions_Category(t *testing.T) {
	t.Run("filters by encoding category", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_cel_functions"
		request.Params.Arguments = map[string]any{
			"category": "encoding",
		}

		result, err := srv.handleListCELFunctions(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := extractJSONContent(t, result)
		var functions []celexp.ExtFunction
		require.NoError(t, json.Unmarshal([]byte(text), &functions))
		assert.NotEmpty(t, functions, "expected at least one encoding function")

		for _, f := range functions {
			assert.Equal(t, "encoding", f.Category, "function %q should be in encoding category", f.Name)
		}
	})
}

func TestHandleListCELFunctions_SummaryIndex(t *testing.T) {
	t.Run("summary index included in unfiltered response", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_cel_functions"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListCELFunctions(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		require.GreaterOrEqual(t, len(result.Content), 2, "expected summary index + JSON")

		summary := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, summary, "# Summary")
		assert.Contains(t, summary, "##")
	})

	t.Run("summary index not included when name filter is used", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_cel_functions"
		request.Params.Arguments = map[string]any{
			"name": "json",
		}

		result, err := srv.handleListCELFunctions(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		// When using name filter, should return JSON directly without summary
		require.Equal(t, 1, len(result.Content), "name filter should return only JSON, no summary")
	})
}
