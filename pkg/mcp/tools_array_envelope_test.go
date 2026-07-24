// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArrayReturningToolsEmitObjectEnvelope is the regression guard for #680:
// tools that return a list must wrap it in a top-level JSON OBJECT (record) with
// a named key, never a bare top-level array. A bare array sets structuredContent
// to an array, which strict (zod) MCP clients reject with "expected record,
// received array", making the tool unusable over MCP.
func TestArrayReturningToolsEmitObjectEnvelope(t *testing.T) {
	reg, err := builtin.DefaultRegistry(context.Background())
	require.NoError(t, err)
	root := buildTestCommandTree()
	srv, err := NewServer(
		WithServerRegistry(reg),
		WithServerVersion("test"),
		WithRootCommand(root),
	)
	require.NoError(t, err)

	// assertObjectEnvelope checks the result is a top-level JSON object whose
	// `key` holds a non-empty JSON array.
	assertObjectEnvelope := func(t *testing.T, result *mcp.CallToolResult, key string) {
		t.Helper()
		require.False(t, result.IsError, "tool returned an error result")
		text := extractJSONContent(t, result)
		require.NotEmpty(t, text)

		trimmed := text
		require.Equalf(t, byte('{'), trimmed[0],
			"output must be a top-level JSON object (record), not a bare array; got: %s", trimmed[:minInt(80, len(trimmed))])

		var obj map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(text), &obj))

		raw, ok := obj[key]
		require.Truef(t, ok, "output must contain the %q envelope key; keys present: %v", key, keysOf(obj))

		var arr []json.RawMessage
		require.NoErrorf(t, json.Unmarshal(raw, &arr), "key %q must hold a JSON array", key)
		assert.NotEmptyf(t, arr, "key %q should list at least one item", key)
	}

	call := func(t *testing.T, name string, args map[string]any, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) *mcp.CallToolResult {
		t.Helper()
		req := mcp.CallToolRequest{}
		req.Params.Name = name
		req.Params.Arguments = args
		result, err := handler(context.Background(), req)
		require.NoError(t, err)
		return result
	}

	t.Run("list_providers", func(t *testing.T) {
		assertObjectEnvelope(t, call(t, "list_providers", map[string]any{}, srv.handleListProviders), "providers")
	})
	t.Run("list_providers with capability filter", func(t *testing.T) {
		assertObjectEnvelope(t, call(t, "list_providers", map[string]any{"capability": "from"}, srv.handleListProviders), "providers")
	})
	t.Run("list_cel_functions", func(t *testing.T) {
		assertObjectEnvelope(t, call(t, "list_cel_functions", map[string]any{}, srv.handleListCELFunctions), "functions")
	})
	t.Run("list_cel_functions custom_only", func(t *testing.T) {
		assertObjectEnvelope(t, call(t, "list_cel_functions", map[string]any{"custom_only": true}, srv.handleListCELFunctions), "functions")
	})
	t.Run("list_go_template_functions", func(t *testing.T) {
		assertObjectEnvelope(t, call(t, "list_go_template_functions", map[string]any{}, srv.handleListGoTemplateFunctions), "functions")
	})
	t.Run("list_official_providers", func(t *testing.T) {
		// Official providers come from a fixed manifest; expect a non-empty list.
		assertObjectEnvelope(t, call(t, "list_official_providers", map[string]any{}, srv.handleListOfficialProviders), "providers")
	})
	// Note: list_plugins is intentionally not covered here. When the plugin cache
	// is empty (the norm in CI) it returns a plain-text message, not a JSON
	// envelope, so a bare "must be an object" assertion is environment-dependent.
	// Its populated JSON path (the one that could carry the bare-array bug) is
	// covered by TestHandleListPlugins/returns_plugins_when_cache_populated, which
	// seeds a plugin and asserts the {"plugins": [...]} envelope.
	t.Run("get_command_help no-arg", func(t *testing.T) {
		assertObjectEnvelope(t, call(t, "get_command_help", map[string]any{}, srv.handleGetCommandHelp), "commands")
	})
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
