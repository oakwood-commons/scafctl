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

// --- Phase 3B: migrate_solution prompt ---

func TestHandleMigrateSolutionPrompt(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("add-composition migration", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "migrate_solution"
		request.Params.Arguments = map[string]string{
			"path":       "solutions/my-app/solution.yaml",
			"migration":  "add-composition",
			"target_dir": "/tmp/migrated",
		}

		result, err := srv.handleMigrateSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Messages)
		assert.Equal(t, mcp.RoleUser, result.Messages[0].Role)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "solutions/my-app/solution.yaml")
		assert.Contains(t, text, "add-composition")
		assert.Contains(t, text, "/tmp/migrated")
		assert.Contains(t, text, "inspect_solution")
		assert.Contains(t, text, "lint_solution")
		assert.Contains(t, text, "behavior-preserving")
	})

	t.Run("extract-templates migration", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Arguments = map[string]string{
			"path":      "solution.yaml",
			"migration": "extract-templates",
		}

		result, err := srv.handleMigrateSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "EXTRACT TEMPLATES")
		assert.Contains(t, text, "extract_resolver_refs")
	})

	t.Run("upgrade-patterns migration", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Arguments = map[string]string{
			"path":      "solution.yaml",
			"migration": "upgrade-patterns",
		}

		result, err := srv.handleMigrateSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "UPGRADE PATTERNS")
		assert.Contains(t, text, "get_provider_schema")
	})

	t.Run("unknown migration type falls back to generic", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Arguments = map[string]string{
			"path":      "solution.yaml",
			"migration": "custom-refactor",
		}

		result, err := srv.handleMigrateSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "custom-refactor")
	})
}

// --- Phase 3C: optimize_solution prompt ---

func TestHandleOptimizeSolutionPrompt(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("all focus (default)", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Arguments = map[string]string{
			"path": "solutions/my-app/solution.yaml",
		}

		result, err := srv.handleOptimizeSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "solutions/my-app/solution.yaml")
		assert.Contains(t, text, "all")
		assert.Contains(t, text, "PERFORMANCE")
		assert.Contains(t, text, "READABILITY")
		assert.Contains(t, text, "TESTING")
		assert.Contains(t, text, "inspect_solution")
		assert.Contains(t, text, "render_solution")
	})

	t.Run("performance focus", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Arguments = map[string]string{
			"path":  "solution.yaml",
			"focus": "performance",
		}

		result, err := srv.handleOptimizeSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "PERFORMANCE ANALYSIS")
		assert.Contains(t, text, "parallel")
	})

	t.Run("readability focus", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Arguments = map[string]string{
			"path":  "solution.yaml",
			"focus": "readability",
		}

		result, err := srv.handleOptimizeSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "READABILITY ANALYSIS")
		assert.Contains(t, text, "naming")
	})

	t.Run("testing focus", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Arguments = map[string]string{
			"path":  "solution.yaml",
			"focus": "testing",
		}

		result, err := srv.handleOptimizeSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "TESTING ANALYSIS")
		assert.Contains(t, text, "list_tests")
	})
}

// --- Phase 5B: Structured errors ---

func TestStructuredErrors(t *testing.T) {
	t.Run("newStructuredError with all options", func(t *testing.T) {
		result := newStructuredError(ErrCodeInvalidInput, "field X is required",
			WithField("x"),
			WithSuggestion("Provide the x field"),
			WithRelatedTools("tool_a", "tool_b"),
		)
		require.NotNil(t, result)
		assert.True(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var te ToolError
		require.NoError(t, json.Unmarshal([]byte(text), &te))
		assert.Equal(t, ErrCodeInvalidInput, te.Code)
		assert.Equal(t, "field X is required", te.Message)
		assert.Equal(t, "x", te.Field)
		assert.Equal(t, "Provide the x field", te.Suggestion)
		assert.Equal(t, []string{"tool_a", "tool_b"}, te.Related)
	})

	t.Run("newStructuredError minimal", func(t *testing.T) {
		result := newStructuredError(ErrCodeNotFound, "item not found")
		require.NotNil(t, result)
		assert.True(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var te ToolError
		require.NoError(t, json.Unmarshal([]byte(text), &te))
		assert.Equal(t, ErrCodeNotFound, te.Code)
		assert.Equal(t, "item not found", te.Message)
		assert.Empty(t, te.Field)
		assert.Empty(t, te.Suggestion)
		assert.Nil(t, te.Related)
	})

	t.Run("catalog_inspect uses structured errors", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{"kind": "invalid-kind", "reference": "test"}
		result, err := srv.handleCatalogInspect(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)

		text := extractText(t, result)
		var te ToolError
		require.NoError(t, json.Unmarshal([]byte(text), &te))
		assert.Equal(t, ErrCodeInvalidInput, te.Code)
		assert.Contains(t, te.Message, "invalid kind")
		assert.Equal(t, "kind", te.Field)
		assert.NotEmpty(t, te.Suggestion)
	})

	t.Run("preview_action uses structured errors for missing path", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{}
		result, err := srv.handlePreviewAction(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)

		text := extractText(t, result)
		var te ToolError
		require.NoError(t, json.Unmarshal([]byte(text), &te))
		assert.Equal(t, ErrCodeInvalidInput, te.Code)
		assert.Equal(t, "path", te.Field)
	})
}

// --- Phase 5D: get_version tool ---

func TestHandleGetVersion(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	result, err := srv.handleGetVersion(context.Background(), request)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	text := extractText(t, result)
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &data))
	assert.Contains(t, data, "version")
	assert.Contains(t, data, "commit")
	assert.Contains(t, data, "buildTime")
	// Default values from settings.VersionInformation
	assert.NotEmpty(t, data["version"])
	assert.NotEmpty(t, data["commit"])
	assert.NotEmpty(t, data["buildTime"])
}

// --- Phase 5C: Latency hints in serverInstructions ---

func TestServerInstructionsContainLatencyGuide(t *testing.T) {
	assert.Contains(t, serverInstructions, "Tool Latency Guide")
	assert.Contains(t, serverInstructions, "Instant")
	assert.Contains(t, serverInstructions, "Fast")
	assert.Contains(t, serverInstructions, "Variable")
	assert.Contains(t, serverInstructions, "get_version")
}
