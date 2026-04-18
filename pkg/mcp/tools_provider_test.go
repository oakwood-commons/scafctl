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

func TestHandleListProviders(t *testing.T) {
	t.Run("returns all providers", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_providers"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListProviders(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		require.NotEmpty(t, result.Content)

		// Verify the response is valid JSON with providers
		text := result.Content[0].(mcp.TextContent).Text
		var items []providerItem
		require.NoError(t, json.Unmarshal([]byte(text), &items))
		assert.NotEmpty(t, items, "expected at least one provider")

		// Verify required fields are populated
		for _, item := range items {
			assert.NotEmpty(t, item.Name, "provider name should not be empty")
			assert.NotEmpty(t, item.Capabilities, "provider should have capabilities")
		}
	})

	t.Run("filters by capability", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_providers"
		request.Params.Arguments = map[string]any{
			"capability": "from",
		}

		result, err := srv.handleListProviders(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var items []providerItem
		require.NoError(t, json.Unmarshal([]byte(text), &items))

		// Each returned provider should have the "from" capability
		for _, item := range items {
			assert.Contains(t, item.Capabilities, "from",
				"provider %q should have 'from' capability", item.Name)
		}
	})

	t.Run("filters by category", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_providers"
		request.Params.Arguments = map[string]any{
			"category": "filesystem",
		}

		result, err := srv.handleListProviders(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var items []providerItem
		require.NoError(t, json.Unmarshal([]byte(text), &items))
		assert.NotEmpty(t, items, "expected at least one filesystem provider")

		for _, item := range items {
			assert.Equal(t, "filesystem", item.Category,
				"provider %q should have 'filesystem' category", item.Name)
		}
	})

	t.Run("no matches for nonexistent category", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_providers"
		request.Params.Arguments = map[string]any{
			"category": "nonexistent-category-xyz",
		}

		result, err := srv.handleListProviders(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var items []providerItem
		require.NoError(t, json.Unmarshal([]byte(text), &items))
		assert.Empty(t, items, "expected no providers for nonexistent category")
	})

	t.Run("returns error when registry nil", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		srv.registry = nil

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_providers"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListProviders(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

func TestHandleGetProviderSchema(t *testing.T) {
	t.Run("returns provider descriptor", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		// Use a known provider name — "static" is always available
		request := mcp.CallToolRequest{}
		request.Params.Name = "get_provider_schema"
		request.Params.Arguments = map[string]any{
			"name": "static",
		}

		result, err := srv.handleGetProviderSchema(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var desc map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &desc))
		assert.Equal(t, "static", desc["name"])
	})

	t.Run("returns error for unknown provider", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_provider_schema"
		request.Params.Arguments = map[string]any{
			"name": "nonexistent-provider",
		}

		result, err := srv.handleGetProviderSchema(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "not found")
	})

	t.Run("returns error when name missing", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_provider_schema"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleGetProviderSchema(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

func TestHandleRunProvider(t *testing.T) {
	t.Run("executes static provider", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "run_provider"
		request.Params.Arguments = map[string]any{
			"provider": "static",
			"inputs":   map[string]any{"value": "hello world"},
		}

		result, err := srv.handleRunProvider(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Equal(t, "static", output["provider"])
		assert.Equal(t, "from", output["capability"])
		assert.NotNil(t, output["data"])
	})

	t.Run("returns error for unknown provider", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "run_provider"
		request.Params.Arguments = map[string]any{
			"provider": "nonexistent-provider",
			"inputs":   map[string]any{},
		}

		result, err := srv.handleRunProvider(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "not found")
	})

	t.Run("returns error when provider name missing", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "run_provider"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleRunProvider(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "INVALID_INPUT")
	})

	t.Run("returns error when registry nil", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		srv.registry = nil

		request := mcp.CallToolRequest{}
		request.Params.Name = "run_provider"
		request.Params.Arguments = map[string]any{
			"provider": "static",
			"inputs":   map[string]any{},
		}

		result, err := srv.handleRunProvider(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "CONFIG_ERROR")
	})

	t.Run("with explicit capability", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "run_provider"
		request.Params.Arguments = map[string]any{
			"provider":   "static",
			"inputs":     map[string]any{"value": "test"},
			"capability": "from",
		}

		result, err := srv.handleRunProvider(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Equal(t, "from", output["capability"])
	})

	t.Run("with unsupported capability", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		// env provider only supports "from", so "action" is unsupported
		request := mcp.CallToolRequest{}
		request.Params.Name = "run_provider"
		request.Params.Arguments = map[string]any{
			"provider":   "env",
			"inputs":     map[string]any{"name": "HOME"},
			"capability": "action",
		}

		result, err := srv.handleRunProvider(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "EXECUTION_FAILED")
	})

	t.Run("with dry run", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "run_provider"
		request.Params.Arguments = map[string]any{
			"provider": "static",
			"inputs":   map[string]any{"value": "test"},
			"dry_run":  true,
		}

		result, err := srv.handleRunProvider(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Equal(t, true, output["dryRun"])
	})

	t.Run("handles nil inputs gracefully", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "run_provider"
		request.Params.Arguments = map[string]any{
			"provider": "static",
			"inputs":   map[string]any{"value": "default"},
		}

		result, err := srv.handleRunProvider(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
	})

	t.Run("returns error when inputs is not an object", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "run_provider"
		request.Params.Arguments = map[string]any{
			"provider": "static",
			"inputs":   "not-an-object",
		}

		result, err := srv.handleRunProvider(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "INVALID_INPUT")
	})

	t.Run("executes env provider", func(t *testing.T) {
		reg, err := builtin.DefaultRegistry(context.Background())
		require.NoError(t, err)
		srv, err := NewServer(
			WithServerRegistry(reg),
			WithServerVersion("test"),
		)
		require.NoError(t, err)

		t.Setenv("TEST_RUN_PROVIDER_VAR", "mcp-test-value")

		request := mcp.CallToolRequest{}
		request.Params.Name = "run_provider"
		request.Params.Arguments = map[string]any{
			"provider":   "env",
			"inputs":     map[string]any{"operation": "get", "name": "TEST_RUN_PROVIDER_VAR"},
			"capability": "from",
		}

		result, err := srv.handleRunProvider(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Equal(t, "env", output["provider"])
	})
}
