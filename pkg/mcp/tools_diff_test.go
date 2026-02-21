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

func TestHandleDiffSolution(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("missing path_a", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "diff_solution"
		request.Params.Arguments = map[string]any{
			"path_b": "/some/path.yaml",
		}

		result, err := srv.handleDiffSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("missing path_b", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "diff_solution"
		request.Params.Arguments = map[string]any{
			"path_a": "/some/path.yaml",
		}

		result, err := srv.handleDiffSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("identical solutions", func(t *testing.T) {
		dir := t.TempDir()
		solFile := filepath.Join(dir, "solution.yaml")
		err := os.WriteFile(solFile, []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: identical
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
		request.Params.Name = "diff_solution"
		request.Params.Arguments = map[string]any{
			"path_a": solFile,
			"path_b": solFile,
		}

		result, err := srv.handleDiffSolution(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))

		summary := output["summary"].(map[string]any)
		assert.Equal(t, float64(0), summary["total"])
	})

	t.Run("different solutions", func(t *testing.T) {
		dir := t.TempDir()
		solA := filepath.Join(dir, "a.yaml")
		err := os.WriteFile(solA, []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: solution-a
  version: "1.0.0"
  description: "First version"
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "hello"
    old-resolver:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "old"
`), 0o644)
		require.NoError(t, err)

		solB := filepath.Join(dir, "b.yaml")
		err = os.WriteFile(solB, []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: solution-b
  version: "2.0.0"
  description: "Second version"
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "hello"
    new-resolver:
      type: int
      resolve:
        with:
          - provider: static
            inputs:
              value: 42
`), 0o644)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "diff_solution"
		request.Params.Arguments = map[string]any{
			"path_a": solA,
			"path_b": solB,
		}

		result, err := srv.handleDiffSolution(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))

		summary := output["summary"].(map[string]any)
		assert.Greater(t, summary["total"], float64(0))

		// Should detect: name changed, description changed, version changed,
		// old-resolver removed, new-resolver added
		changes := output["changes"].([]any)
		assert.NotEmpty(t, changes)

		// Verify some changes exist
		changeFields := make(map[string]string)
		for _, c := range changes {
			cm := c.(map[string]any)
			changeFields[cm["field"].(string)] = cm["type"].(string)
		}

		assert.Equal(t, "changed", changeFields["metadata.name"])
		assert.Equal(t, "changed", changeFields["metadata.description"])
		assert.Equal(t, "added", changeFields["spec.resolvers.new-resolver"])
		assert.Equal(t, "removed", changeFields["spec.resolvers.old-resolver"])
	})

	t.Run("nonexistent file", func(t *testing.T) {
		dir := t.TempDir()
		solFile := filepath.Join(dir, "exists.yaml")
		err := os.WriteFile(solFile, []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: exists
  version: "1.0.0"
spec: {}
`), 0o644)
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "diff_solution"
		request.Params.Arguments = map[string]any{
			"path_a": solFile,
			"path_b": "/nonexistent/path.yaml",
		}

		result, err := srv.handleDiffSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}
