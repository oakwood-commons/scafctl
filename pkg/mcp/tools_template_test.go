// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"text/template"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleEvaluateGoTemplate(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("simple template", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "evaluate_go_template"
		request.Params.Arguments = map[string]any{
			"template": "Hello {{ .name }}!",
			"data":     map[string]any{"name": "World"},
		}

		result, err := srv.handleEvaluateGoTemplate(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Equal(t, "Hello World!", output["output"])
	})

	t.Run("template with no data", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "evaluate_go_template"
		request.Params.Arguments = map[string]any{
			"template": "Static text only",
		}

		result, err := srv.handleEvaluateGoTemplate(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Equal(t, "Static text only", output["output"])
	})

	t.Run("template missing required param", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "evaluate_go_template"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleEvaluateGoTemplate(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("invalid template syntax", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "evaluate_go_template"
		request.Params.Arguments = map[string]any{
			"template": "{{ .name",
		}

		result, err := srv.handleEvaluateGoTemplate(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

// TestHandleListGoTemplateFunctions_EmbedderOnly verifies the MCP
// list_go_template_functions tool surfaces embedder-registered functions,
// tagged with the embedder source, both in the embedder_only view and the
// default (combined) view.
func TestHandleListGoTemplateFunctions_EmbedderOnly(t *testing.T) {
	// The gotmpl registry is process-global; reset it so this test does not leak
	// the embedder function into other tests in this binary (which could hit
	// register-once collisions or see extra entries in default listings).
	gotmpl.ResetRegistryForTesting()
	t.Cleanup(gotmpl.ResetRegistryForTesting)
	require.NoError(t, gotmpl.RegisterFuncs(template.FuncMap{
		"mcpEmbedderListTestFunc": func(s string) string { return s },
	}))

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	list := func(t *testing.T, args map[string]any) []map[string]any {
		t.Helper()
		request := mcp.CallToolRequest{}
		request.Params.Name = "list_go_template_functions"
		request.Params.Arguments = args

		result, err := srv.handleListGoTemplateFunctions(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output struct {
			Functions []map[string]any `json:"functions"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		return output.Functions
	}

	find := func(fns []map[string]any, name string) map[string]any {
		for _, fn := range fns {
			if fn["name"] == name {
				return fn
			}
		}
		return nil
	}

	t.Run("embedder_only view", func(t *testing.T) {
		fn := find(list(t, map[string]any{"embedder_only": true}), "mcpEmbedderListTestFunc")
		require.NotNil(t, fn, "embedder function should appear in embedder_only listing")
		assert.Equal(t, gotmpl.SourceEmbedder, fn["source"])
	})

	t.Run("default combined view", func(t *testing.T) {
		fn := find(list(t, map[string]any{}), "mcpEmbedderListTestFunc")
		require.NotNil(t, fn, "embedder function should appear in the default listing")
		assert.Equal(t, gotmpl.SourceEmbedder, fn["source"])
	})
}

func TestHandleValidateExpression(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("valid CEL expression", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "validate_expression"
		request.Params.Arguments = map[string]any{
			"expression": "1 + 2 == 3",
			"type":       "cel",
		}

		result, err := srv.handleValidateExpression(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Equal(t, true, output["valid"])
		assert.Equal(t, "cel", output["type"])
	})

	t.Run("invalid CEL expression", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "validate_expression"
		request.Params.Arguments = map[string]any{
			"expression": "1 + + 2",
			"type":       "cel",
		}

		result, err := srv.handleValidateExpression(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError) // tool returns valid=false, not an error

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Equal(t, false, output["valid"])
	})

	t.Run("valid Go template", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "validate_expression"
		request.Params.Arguments = map[string]any{
			"expression": "{{ .name }}",
			"type":       "go-template",
		}

		result, err := srv.handleValidateExpression(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Equal(t, true, output["valid"])
		assert.Equal(t, "go-template", output["type"])
	})

	t.Run("invalid Go template", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "validate_expression"
		request.Params.Arguments = map[string]any{
			"expression": "{{ .name",
			"type":       "go-template",
		}

		result, err := srv.handleValidateExpression(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Equal(t, false, output["valid"])
	})

	t.Run("missing required params", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "validate_expression"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleValidateExpression(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("unsupported type", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "validate_expression"
		request.Params.Arguments = map[string]any{
			"expression": "test",
			"type":       "javascript",
		}

		result, err := srv.handleValidateExpression(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}
