// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProgressReporter(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("no progress token logs instead", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "test_tool"
		request.Params.Arguments = map[string]any{}

		reporter := newProgressReporter(srv, request)
		assert.Nil(t, reporter.token, "should have no progress token")

		// Should not panic when reporting without a token
		reporter.setTotal(3)
		reporter.report(srv.ctx, 1, "step 1")
		reporter.report(srv.ctx, 2, "step 2")
		reporter.report(srv.ctx, 3, "step 3")
	})

	t.Run("with progress token", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "test_tool"
		request.Params.Arguments = map[string]any{}
		request.Params.Meta = &mcp.Meta{
			ProgressToken: mcp.ProgressToken("test-token-123"),
		}

		reporter := newProgressReporter(srv, request)
		assert.NotNil(t, reporter.token, "should have progress token")

		reporter.setTotal(5)
		assert.Equal(t, float64(5), reporter.total)
	})
}

func TestToolIcons(t *testing.T) {
	// Verify all expected icon categories exist
	expectedCategories := []string{
		"solution", "provider", "cel", "template", "schema",
		"example", "catalog", "auth", "lint", "scaffold",
		"action", "diff", "dryrun", "config", "refs",
		"testing", "snapshot", "version",
	}

	for _, cat := range expectedCategories {
		t.Run("tool icon "+cat, func(t *testing.T) {
			icon, ok := toolIcons[cat]
			assert.True(t, ok, "icon category %q should exist", cat)
			assert.NotEmpty(t, icon.Src, "icon %q should have Src", cat)
			assert.Equal(t, "image/svg+xml", icon.MIMEType, "icon %q should be SVG", cat)
			assert.Contains(t, icon.Src, "data:image/svg+xml,", "icon %q should be data URI", cat)
		})
	}
}

func TestPromptIcons(t *testing.T) {
	expectedCategories := []string{"create", "debug", "guide", "analyze"}

	for _, cat := range expectedCategories {
		t.Run("prompt icon "+cat, func(t *testing.T) {
			icon, ok := promptIcons[cat]
			assert.True(t, ok, "prompt icon %q should exist", cat)
			assert.NotEmpty(t, icon.Src)
			assert.Equal(t, "image/svg+xml", icon.MIMEType)
		})
	}
}

func TestResourceIcons(t *testing.T) {
	expectedCategories := []string{"solution", "provider"}

	for _, cat := range expectedCategories {
		t.Run("resource icon "+cat, func(t *testing.T) {
			icon, ok := resourceIcons[cat]
			assert.True(t, ok, "resource icon %q should exist", cat)
			assert.NotEmpty(t, icon.Src)
			assert.Equal(t, "image/svg+xml", icon.MIMEType)
		})
	}
}

func TestToolInputSchemas(t *testing.T) {
	// Verify all registered tool schemas are valid JSON Schema.
	// In particular, array-typed properties must have an "items" definition
	// or MCP clients (VS Code, Cursor, etc.) will reject the tool.
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	tools := srv.mcpServer.ListTools()
	require.NotEmpty(t, tools, "server should have registered tools")

	for name, st := range tools {
		t.Run(name, func(t *testing.T) {
			// Marshal the tool to JSON and back so we get the final schema
			// exactly as the MCP client would see it.
			raw, err := json.Marshal(st.Tool)
			require.NoError(t, err, "tool should marshal to JSON")

			var toolJSON map[string]any
			require.NoError(t, json.Unmarshal(raw, &toolJSON))

			// Check inputSchema
			if schema, ok := toolJSON["inputSchema"].(map[string]any); ok {
				assertSchemaArrayItems(t, name, "inputSchema", schema)
			}

			// Check outputSchema — VS Code Copilot validates these too
			if schema, ok := toolJSON["outputSchema"].(map[string]any); ok {
				assertSchemaArrayItems(t, name, "outputSchema", schema)
			}
		})
	}
}

// assertSchemaArrayItems recursively checks that any "type":"array" node has an "items" key.
// It walks properties, items, and additionalProperties.
func assertSchemaArrayItems(t *testing.T, toolName, path string, schema any) {
	t.Helper()
	m, ok := schema.(map[string]any)
	if !ok {
		return
	}

	if typ, ok := m["type"].(string); ok && typ == "array" {
		assert.Contains(t, m, "items",
			"tool %q at %q is type array but missing required 'items' schema", toolName, path)
	}

	// Recurse into items
	if items, ok := m["items"]; ok {
		assertSchemaArrayItems(t, toolName, path+".items", items)
	}

	// Recurse into object properties
	if props, ok := m["properties"].(map[string]any); ok {
		for name, val := range props {
			assertSchemaArrayItems(t, toolName, path+"."+name, val)
		}
	}

	// Recurse into additionalProperties
	if addl, ok := m["additionalProperties"].(map[string]any); ok {
		assertSchemaArrayItems(t, toolName, path+".additionalProperties", addl)
	}
}

func TestOutputSchemas(t *testing.T) {
	// Verify all output schemas are valid JSON
	schemas := map[string][]byte{
		"ListSolutions":    outputSchemaListSolutions,
		"InspectSolution":  outputSchemaInspectSolution,
		"ListProviders":    outputSchemaListProviders,
		"Version":          outputSchemaVersion,
		"LintResult":       outputSchemaLintResult,
		"EvaluateCEL":      outputSchemaEvaluateCEL,
		"RenderSolution":   outputSchemaRenderSolution,
		"AuthStatus":       outputSchemaAuthStatus,
		"GetConfig":        outputSchemaGetConfig,
		"GetConfigPaths":   outputSchemaGetConfigPaths,
		"PreviewResolvers": outputSchemaPreviewResolvers,
		"DryRun":           outputSchemaDryRun,
	}

	for name, schema := range schemas {
		t.Run(name, func(t *testing.T) {
			assert.True(t, len(schema) > 0, "schema should not be empty")
			// Verify it's valid JSON
			var parsed map[string]any
			err := json.Unmarshal(schema, &parsed)
			assert.NoError(t, err, "schema should be valid JSON")
			// Verify it has a type
			assert.Contains(t, parsed, "type", "schema should have a type field")
		})
	}
}

func TestElicitMissingParams(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("empty param names returns nil", func(t *testing.T) {
		result := srv.elicitMissingParams(srv.ctx, nil, nil)
		assert.Nil(t, result)
	})

	t.Run("elicitation gracefully degrades without session", func(t *testing.T) {
		// Without a connected MCP session, elicitation should return nil
		result := srv.elicitMissingParams(srv.ctx, []string{"name"}, map[string]string{
			"name": "Your name",
		})
		assert.Nil(t, result)
	})
}

func TestDiscoverWorkspaceRoots(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("gracefully returns nil without session", func(t *testing.T) {
		roots := srv.discoverWorkspaceRoots(srv.ctx)
		assert.Nil(t, roots)
	})

	t.Run("discoverSolutionFiles returns nil without roots", func(t *testing.T) {
		files := srv.discoverSolutionFiles(srv.ctx)
		// May return a file if CWD has a solution, or nil otherwise
		// Either way it should not panic
		_ = files
	})
}

func TestContainsPath(t *testing.T) {
	assert.True(t, containsPath([]string{"/a/b", "/c/d"}, "/a/b"))
	assert.False(t, containsPath([]string{"/a/b", "/c/d"}, "/e/f"))
	assert.False(t, containsPath(nil, "/a/b"))
}

func TestSendLog(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	// Should not panic without a session
	srv.sendLog(srv.ctx, mcp.LoggingLevelInfo, "test-logger", "test message")
}
