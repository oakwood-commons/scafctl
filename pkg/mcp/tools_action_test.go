// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlePreviewAction(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("missing path", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_action"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handlePreviewAction(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("nonexistent solution", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_action"
		request.Params.Arguments = map[string]any{
			"path": "/nonexistent/solution.yaml",
		}

		result, err := srv.handlePreviewAction(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("solution without workflow", func(t *testing.T) {
		// Create a temp solution with no workflow
		dir := t.TempDir()
		solFile := filepath.Join(dir, "solution.yaml")
		err := os.WriteFile(solFile, []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: no-workflow
  version: "1.0.0"
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "hello"
`), 0o644)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_action"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handlePreviewAction(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "workflow")
	})

	t.Run("solution with workflow", func(t *testing.T) {
		dir := t.TempDir()
		solFile := filepath.Join(dir, "solution.yaml")
		err := os.WriteFile(solFile, []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: with-workflow
  version: "1.0.0"
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "hello"
  workflow:
    actions:
      echo:
        description: "Echo greeting"
        provider: exec
        inputs:
          command:
            tmpl: "echo {{ .resolvers.greeting }}"
`), 0o644)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_action"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handlePreviewAction(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError, "unexpected error: %v", result.Content)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.NotNil(t, output["actions"])
		assert.NotNil(t, output["resolverData"])

		// Verify source position is included in action previews
		actions := output["actions"].([]any)
		require.NotEmpty(t, actions)
		firstAction := actions[0].(map[string]any)
		sourcePos := firstAction["sourcePos"]
		require.NotNil(t, sourcePos, "sourcePos should be present for actions")
		sp := sourcePos.(map[string]any)
		assert.Greater(t, sp["line"], float64(0), "line should be > 0")
		assert.Greater(t, sp["column"], float64(0), "column should be > 0")
	})

	t.Run("filter nonexistent action", func(t *testing.T) {
		dir := t.TempDir()
		solFile := filepath.Join(dir, "solution.yaml")
		err := os.WriteFile(solFile, []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: filter-test
  version: "1.0.0"
spec:
  resolvers:
    val:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "x"
  workflow:
    actions:
      step1:
        provider: exec
        inputs:
          command: "echo step1"
`), 0o644)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_action"
		request.Params.Arguments = map[string]any{
			"path":   solFile,
			"action": "nonexistent",
		}

		result, err := srv.handlePreviewAction(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "nonexistent")
	})
}
