// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptCompletionProvider(t *testing.T) {
	reg := provider.NewRegistry()

	p := &promptCompletionProvider{registry: reg}

	t.Run("provider argument returns empty when no providers", func(t *testing.T) {
		completion, err := p.CompletePromptArgument(context.Background(), "some_prompt", mcp.CompleteArgument{
			Name:  "provider",
			Value: "",
		}, mcp.CompleteContext{})
		require.NoError(t, err)
		assert.Empty(t, completion.Values)
	})

	t.Run("migration argument returns types", func(t *testing.T) {
		completion, err := p.CompletePromptArgument(context.Background(), "migrate_solution", mcp.CompleteArgument{
			Name:  "migration",
			Value: "",
		}, mcp.CompleteContext{})
		require.NoError(t, err)
		assert.NotEmpty(t, completion.Values)
		assert.Contains(t, completion.Values, "composition")
		assert.Contains(t, completion.Values, "templates")
	})

	t.Run("migration argument filters by prefix", func(t *testing.T) {
		completion, err := p.CompletePromptArgument(context.Background(), "migrate_solution", mcp.CompleteArgument{
			Name:  "migration",
			Value: "comp",
		}, mcp.CompleteContext{})
		require.NoError(t, err)
		assert.Equal(t, []string{"composition"}, completion.Values)
	})

	t.Run("features argument returns feature list", func(t *testing.T) {
		completion, err := p.CompletePromptArgument(context.Background(), "explain_feature", mcp.CompleteArgument{
			Name:  "features",
			Value: "",
		}, mcp.CompleteContext{})
		require.NoError(t, err)
		assert.NotEmpty(t, completion.Values)
		assert.Contains(t, completion.Values, "resolvers")
		assert.Contains(t, completion.Values, "actions")
	})

	t.Run("unknown argument returns empty", func(t *testing.T) {
		completion, err := p.CompletePromptArgument(context.Background(), "some_prompt", mcp.CompleteArgument{
			Name:  "unknown_arg",
			Value: "",
		}, mcp.CompleteContext{})
		require.NoError(t, err)
		assert.Empty(t, completion.Values)
	})

	t.Run("nil registry is safe", func(t *testing.T) {
		provider := &promptCompletionProvider{registry: nil}
		completion, err := provider.CompletePromptArgument(context.Background(), "some_prompt", mcp.CompleteArgument{
			Name:  "provider",
			Value: "",
		}, mcp.CompleteContext{})
		require.NoError(t, err)
		assert.Empty(t, completion.Values)
	})
}

func TestCompleteMigrationTypes(t *testing.T) {
	t.Run("empty prefix returns all types", func(t *testing.T) {
		completion, err := completeMigrationTypes("")
		require.NoError(t, err)
		assert.Len(t, completion.Values, 5)
	})

	t.Run("prefix filters results", func(t *testing.T) {
		completion, err := completeMigrationTypes("up")
		require.NoError(t, err)
		assert.Equal(t, []string{"upgrade"}, completion.Values)
	})

	t.Run("no match returns empty", func(t *testing.T) {
		completion, err := completeMigrationTypes("xyz")
		require.NoError(t, err)
		assert.Empty(t, completion.Values)
	})
}

func TestCompleteFeatures(t *testing.T) {
	t.Run("empty prefix returns all features", func(t *testing.T) {
		completion, err := completeFeatures("")
		require.NoError(t, err)
		assert.NotEmpty(t, completion.Values)
	})

	t.Run("prefix filters results", func(t *testing.T) {
		completion, err := completeFeatures("res")
		require.NoError(t, err)
		assert.Equal(t, []string{"resolvers"}, completion.Values)
	})
}

func TestToolArgCompletions(t *testing.T) {
	t.Run("provider names with nil registry", func(t *testing.T) {
		c := &toolArgCompletions{registry: nil}
		names := c.ProviderNames()
		assert.Nil(t, names)
	})

	t.Run("provider names with empty registry", func(t *testing.T) {
		c := &toolArgCompletions{registry: provider.NewRegistry()}
		names := c.ProviderNames()
		assert.Empty(t, names)
	})

	t.Run("lint rule names returns rules", func(t *testing.T) {
		c := &toolArgCompletions{}
		names := c.LintRuleNames()
		// Should return at least some rules from the lint package
		assert.NotEmpty(t, names)
	})

	t.Run("CEL function names returns functions", func(t *testing.T) {
		c := &toolArgCompletions{}
		names := c.CELFunctionNames()
		// Should return at least some CEL functions
		assert.NotEmpty(t, names)
	})

	t.Run("example names returns examples", func(t *testing.T) {
		c := &toolArgCompletions{}
		names := c.ExampleNames()
		// Should return at least some examples from embedded FS
		assert.NotEmpty(t, names)
	})
}
