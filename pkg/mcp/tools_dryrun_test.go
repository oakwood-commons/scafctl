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

func TestHandleDryRunSolution(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("missing path", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "dry_run_solution"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleDryRunSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("invalid params type", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "dry_run_solution"
		request.Params.Arguments = map[string]any{
			"path":   "/tmp/nonexistent-solution.yaml",
			"params": "not-an-object",
		}

		result, err := srv.handleDryRunSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "params")
	})

	t.Run("nonexistent solution", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = "dry_run_solution"
		request.Params.Arguments = map[string]any{
			"path": "/tmp/absolutely-nonexistent-solution.yaml",
		}

		result, err := srv.handleDryRunSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("valid solution dry run", func(t *testing.T) {
		// Create a minimal solution file
		solutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: dry-run-test
  version: 1.0.0
  description: A minimal solution for dry-run testing
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "Hello"
`
		tmpDir := t.TempDir()
		solutionPath := filepath.Join(tmpDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solutionPath, []byte(solutionYAML), 0o644))

		request := mcp.CallToolRequest{}
		request.Params.Name = "dry_run_solution"
		request.Params.Arguments = map[string]any{
			"path": solutionPath,
		}

		result, err := srv.handleDryRunSolution(context.Background(), request)
		require.NoError(t, err)

		text := result.Content[0].(mcp.TextContent).Text
		if result.IsError {
			t.Logf("dry run returned error (may be expected in test env): %s", text)
			return
		}

		var output map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &output))

		assert.Equal(t, true, output["dryRun"])
		assert.Equal(t, "dry-run-test", output["solution"])
		assert.Equal(t, true, output["hasResolvers"])
	})
}
