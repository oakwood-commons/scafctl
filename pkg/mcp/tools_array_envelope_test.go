// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
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
	// Sandbox XDG so the plugin cache / catalog handlers read an isolated temp
	// directory instead of the developer's or CI user's real XDG paths.
	setTestXDG(t, t.TempDir())

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
	// `key` holds a JSON array. requireNonEmpty toggles whether the array must
	// contain at least one element -- some lists (plugins, solutions) are
	// legitimately empty in a clean test environment; the point of the guard is
	// that the shape is always a record with an array under `key`, never a bare
	// top-level array.
	assertObjectEnvelope := func(t *testing.T, result *mcp.CallToolResult, key string, requireNonEmpty bool) {
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

		// The value must be a JSON array, not null. json.Unmarshal into a slice
		// also accepts null (yielding a nil slice), so check the raw token first.
		rawTrimmed := bytes.TrimSpace(raw)
		require.Truef(t, len(rawTrimmed) > 0 && rawTrimmed[0] == '[',
			"key %q must hold a JSON array, got: %s", key, string(rawTrimmed))

		var arr []json.RawMessage
		require.NoErrorf(t, json.Unmarshal(raw, &arr), "key %q must hold a JSON array", key)
		if requireNonEmpty {
			assert.NotEmptyf(t, arr, "key %q should list at least one item", key)
		}
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
		assertObjectEnvelope(t, call(t, "list_providers", map[string]any{}, srv.handleListProviders), "providers", true)
	})
	t.Run("list_providers with capability filter", func(t *testing.T) {
		assertObjectEnvelope(t, call(t, "list_providers", map[string]any{"capability": "from"}, srv.handleListProviders), "providers", true)
	})
	t.Run("list_cel_functions", func(t *testing.T) {
		assertObjectEnvelope(t, call(t, "list_cel_functions", map[string]any{}, srv.handleListCELFunctions), "functions", true)
	})
	t.Run("list_cel_functions custom_only", func(t *testing.T) {
		assertObjectEnvelope(t, call(t, "list_cel_functions", map[string]any{"custom_only": true}, srv.handleListCELFunctions), "functions", true)
	})
	t.Run("list_go_template_functions", func(t *testing.T) {
		assertObjectEnvelope(t, call(t, "list_go_template_functions", map[string]any{}, srv.handleListGoTemplateFunctions), "functions", true)
	})
	t.Run("list_official_providers", func(t *testing.T) {
		// Official providers come from a fixed manifest; expect a non-empty list.
		assertObjectEnvelope(t, call(t, "list_official_providers", map[string]any{}, srv.handleListOfficialProviders), "providers", true)
	})
	t.Run("list_plugins", func(t *testing.T) {
		// The plugin cache may be empty in a clean environment; the envelope must
		// still be an object with a (possibly empty) plugins array.
		assertObjectEnvelope(t, call(t, "list_plugins", map[string]any{}, srv.handleListPlugins), "plugins", false)
	})
	t.Run("catalog_list_plugins", func(t *testing.T) {
		// Local catalog is 'local' so no network is required; the local catalog
		// may be empty in a clean environment, so the plugins array may be empty
		// -- the envelope must still be a record with a plugins array.
		assertObjectEnvelope(t, call(t, "catalog_list_plugins", map[string]any{"catalog": "local"}, srv.handleCatalogListPlugins), "plugins", false)
	})
	t.Run("list_solutions", func(t *testing.T) {
		// The local catalog may be empty; the envelope must still be an object
		// with a (possibly empty) solutions array.
		assertObjectEnvelope(t, call(t, "list_solutions", map[string]any{}, srv.handleListSolutions), "solutions", false)
	})
	t.Run("get_command_help no-arg", func(t *testing.T) {
		assertObjectEnvelope(t, call(t, "get_command_help", map[string]any{}, srv.handleGetCommandHelp), "commands", true)
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
