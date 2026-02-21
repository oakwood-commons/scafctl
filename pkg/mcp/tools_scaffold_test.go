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

func TestHandleScaffoldSolution(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("minimal scaffold", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "scaffold_solution"
		request.Params.Arguments = map[string]any{
			"name":        "my-test-solution",
			"description": "A test solution",
		}

		result, err := srv.handleScaffoldSolution(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		yaml, ok := output["yaml"].(string)
		require.True(t, ok)
		assert.Contains(t, yaml, "apiVersion: scafctl.io/v1")
		assert.Contains(t, yaml, "name: my-test-solution")
		assert.Contains(t, yaml, "A test solution")
	})

	t.Run("with features and providers", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "scaffold_solution"
		request.Params.Arguments = map[string]any{
			"name":        "full-solution",
			"description": "Full featured solution",
			"features":    []any{"parameters", "actions", "transforms", "validation", "tests", "composition"},
			"providers":   []any{"http", "exec"},
		}

		result, err := srv.handleScaffoldSolution(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		yaml, ok := output["yaml"].(string)
		require.True(t, ok)
		assert.Contains(t, yaml, "apiVersion: scafctl.io/v1")
		assert.Contains(t, yaml, "parameter")
		assert.Contains(t, yaml, "actions")
		assert.Contains(t, yaml, "testing")
		assert.Contains(t, yaml, "compose")
	})

	t.Run("custom version", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "scaffold_solution"
		request.Params.Arguments = map[string]any{
			"name":        "versioned",
			"description": "Custom version",
			"version":     "2.0.0",
		}

		result, err := srv.handleScaffoldSolution(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		yaml, ok := output["yaml"].(string)
		require.True(t, ok)
		assert.Contains(t, yaml, `version: "2.0.0"`)
	})

	t.Run("missing required name", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "scaffold_solution"
		request.Params.Arguments = map[string]any{
			"description": "No name",
		}

		result, err := srv.handleScaffoldSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("missing required description", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "scaffold_solution"
		request.Params.Arguments = map[string]any{
			"name": "no-description",
		}

		result, err := srv.handleScaffoldSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}
