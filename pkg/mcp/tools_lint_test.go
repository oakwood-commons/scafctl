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

func TestHandleExplainLintRule(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("known rule", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "explain_lint_rule"
		request.Params.Arguments = map[string]any{
			"rule": "empty-solution",
		}

		result, err := srv.handleExplainLintRule(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Equal(t, "empty-solution", output["rule"])
		assert.NotEmpty(t, output["description"])
		assert.NotEmpty(t, output["fix"])
		assert.NotEmpty(t, output["why"])
	})

	t.Run("unknown rule", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "explain_lint_rule"
		request.Params.Arguments = map[string]any{
			"rule": "nonexistent-rule",
		}

		result, err := srv.handleExplainLintRule(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "nonexistent-rule")
	})

	t.Run("missing required param", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "explain_lint_rule"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleExplainLintRule(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("all known rules have required fields", func(t *testing.T) {
		for name, explanation := range lintRuleExplanations {
			assert.NotEmpty(t, explanation.Rule, "rule %q missing Rule field", name)
			assert.NotEmpty(t, explanation.Description, "rule %q missing Description", name)
			assert.NotEmpty(t, explanation.Fix, "rule %q missing Fix", name)
			assert.NotEmpty(t, explanation.Why, "rule %q missing Why", name)
			assert.NotEmpty(t, explanation.Category, "rule %q missing Category", name)
			assert.NotEmpty(t, explanation.Severity, "rule %q missing Severity", name)
		}
	})
}
