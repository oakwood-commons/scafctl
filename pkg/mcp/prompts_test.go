// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleCreateSolutionPrompt(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("with all arguments", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "create_solution"
		request.Params.Arguments = map[string]string{
			"name":        "my-solution",
			"description": "Test solution for CI",
			"features":    "resolvers, actions, validation",
		}

		result, err := srv.handleCreateSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Messages)
		assert.Equal(t, mcp.RoleUser, result.Messages[0].Role)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "my-solution")
		assert.Contains(t, text, "Test solution for CI")
		assert.Contains(t, text, "resolvers, actions, validation")
		assert.Contains(t, text, "get_solution_schema")
		assert.Contains(t, text, "run resolver", "prompt must mention 'run resolver' for solutions without a workflow")
		assert.Contains(t, text, "run solution", "prompt must mention 'run solution' for solutions with a workflow")
	})

	t.Run("with minimal arguments", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "create_solution"
		request.Params.Arguments = map[string]string{
			"name":        "minimal",
			"description": "Minimal solution",
		}

		result, err := srv.handleCreateSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Messages)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "resolvers, actions") // default features
	})
}

func TestHandleDebugSolutionPrompt(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("with all arguments", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "debug_solution"
		request.Params.Arguments = map[string]string{
			"path":    "solutions/my-solution/solution.yaml",
			"problem": "Resolvers are not resolving correctly",
		}

		result, err := srv.handleDebugSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Messages)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "solutions/my-solution/solution.yaml")
		assert.Contains(t, text, "Resolvers are not resolving correctly")
		assert.Contains(t, text, "inspect_solution")
		assert.Contains(t, text, "lint_solution")
	})

	t.Run("with no problem specified", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "debug_solution"
		request.Params.Arguments = map[string]string{
			"path": "test.yaml",
		}

		result, err := srv.handleDebugSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "not working as expected") // default problem text
	})
}

func TestHandleAddResolverPrompt(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("with all arguments", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "add_resolver"
		request.Params.Arguments = map[string]string{
			"provider": "parameter",
			"purpose":  "get user input for deployment region",
		}

		result, err := srv.handleAddResolverPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Messages)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "parameter")
		assert.Contains(t, text, "get user input for deployment region")
		assert.Contains(t, text, "get_provider_schema")
		assert.Contains(t, text, "explain_kind")
	})

	t.Run("with provider only", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "add_resolver"
		request.Params.Arguments = map[string]string{
			"provider": "env",
		}

		result, err := srv.handleAddResolverPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "env")
		assert.Contains(t, text, "resolve a value") // default purpose
	})
}

func TestHandleAddActionPrompt(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("with all arguments", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "add_action"
		request.Params.Arguments = map[string]string{
			"provider": "exec",
			"purpose":  "run a deployment script",
		}

		result, err := srv.handleAddActionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Messages)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "exec")
		assert.Contains(t, text, "run a deployment script")
		assert.Contains(t, text, "get_provider_schema")
		assert.Contains(t, text, "explain_kind")
		assert.Contains(t, text, "forEach")
		assert.Contains(t, text, "retry")
	})

	t.Run("with provider only", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "add_action"
		request.Params.Arguments = map[string]string{
			"provider": "directory",
		}

		result, err := srv.handleAddActionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "directory")
		assert.Contains(t, text, "perform an operation") // default purpose
	})
}
