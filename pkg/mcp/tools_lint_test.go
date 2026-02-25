// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	pkglint "github.com/oakwood-commons/scafctl/pkg/lint"
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
		for name, rule := range pkglint.KnownRules {
			assert.NotEmpty(t, rule.Rule, "rule %q missing Rule field", name)
			assert.NotEmpty(t, rule.Description, "rule %q missing Description", name)
			assert.NotEmpty(t, rule.Fix, "rule %q missing Fix", name)
			assert.NotEmpty(t, rule.Why, "rule %q missing Why", name)
			assert.NotEmpty(t, rule.Category, "rule %q missing Category", name)
			assert.NotEmpty(t, rule.Severity, "rule %q missing Severity", name)
		}
	})
}

func TestHandleListLintRules(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("all rules", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "list_lint_rules"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListLintRules(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		count := int(output["count"].(float64))
		assert.Equal(t, len(pkglint.KnownRules), count)

		rules := output["rules"].([]any)
		assert.Len(t, rules, len(pkglint.KnownRules))

		// Verify sorted — errors should come before warnings, warnings before info
		var prevSeverityOrder int
		severityOrder := map[string]int{"error": 0, "warning": 1, "info": 2}
		for _, r := range rules {
			rule := r.(map[string]any)
			so := severityOrder[rule["severity"].(string)]
			assert.GreaterOrEqual(t, so, prevSeverityOrder, "rules should be sorted by severity priority")
			prevSeverityOrder = so
		}
	})

	t.Run("filter by severity", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "list_lint_rules"
		request.Params.Arguments = map[string]any{
			"severity": "error",
		}

		result, err := srv.handleListLintRules(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))

		rules := output["rules"].([]any)
		assert.Greater(t, len(rules), 0)
		for _, r := range rules {
			rule := r.(map[string]any)
			assert.Equal(t, "error", rule["severity"])
		}
	})

	t.Run("filter by category", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "list_lint_rules"
		request.Params.Arguments = map[string]any{
			"category": "provider",
		}

		result, err := srv.handleListLintRules(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))

		rules := output["rules"].([]any)
		assert.Greater(t, len(rules), 0)
		for _, r := range rules {
			rule := r.(map[string]any)
			assert.Equal(t, "provider", rule["category"])
		}
	})

	t.Run("filter by severity and category", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "list_lint_rules"
		request.Params.Arguments = map[string]any{
			"severity": "error",
			"category": "provider",
		}

		result, err := srv.handleListLintRules(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))

		rules := output["rules"].([]any)
		assert.Greater(t, len(rules), 0)
		for _, r := range rules {
			rule := r.(map[string]any)
			assert.Equal(t, "error", rule["severity"])
			assert.Equal(t, "provider", rule["category"])
		}
	})

	t.Run("no matches returns empty", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "list_lint_rules"
		request.Params.Arguments = map[string]any{
			"category": "nonexistent-category",
		}

		result, err := srv.handleListLintRules(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Equal(t, float64(0), output["count"])
	})

	t.Run("each rule has summary fields", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "list_lint_rules"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListLintRules(context.Background(), request)
		require.NoError(t, err)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))

		rules := output["rules"].([]any)
		for _, r := range rules {
			rule := r.(map[string]any)
			assert.NotEmpty(t, rule["rule"], "rule should have a name")
			assert.NotEmpty(t, rule["severity"], "rule should have severity")
			assert.NotEmpty(t, rule["category"], "rule should have category")
			assert.NotEmpty(t, rule["description"], "rule should have description")
		}
	})
}
