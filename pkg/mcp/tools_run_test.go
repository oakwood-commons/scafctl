// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleRunSolution_MissingPath(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "run_solution"
	request.Params.Arguments = map[string]any{}

	result, err := srv.handleRunSolution(context.Background(), request)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestHandleRunSolution_InvalidConflictStrategy(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "run_solution"
	request.Params.Arguments = map[string]any{
		"path":        "some-solution",
		"on_conflict": "invalid",
	}

	result, err := srv.handleRunSolution(context.Background(), request)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	text := result.Content[0].(mcp.TextContent).Text
	assert.Contains(t, text, "invalid on_conflict")
}

func TestHandleRunSolution_InvalidParams(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "run_solution"
	request.Params.Arguments = map[string]any{
		"path":   "some-solution",
		"params": "not-an-object",
	}

	result, err := srv.handleRunSolution(context.Background(), request)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	text := result.Content[0].(mcp.TextContent).Text
	assert.Contains(t, text, "must be an object")
}

func TestHandleRunSolution_NonexistentSolution(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "run_solution"
	request.Params.Arguments = map[string]any{
		"path": "/nonexistent/solution.yaml",
	}

	result, err := srv.handleRunSolution(context.Background(), request)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestHandleRunSolution_ResolverOnly(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	xdg.Reload()
	t.Cleanup(func() { xdg.Reload() })

	solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolver-only-test
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'hello'"
`
	solFile := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "run_solution"
	request.Params.Arguments = map[string]any{
		"path": solFile,
	}

	result, err := srv.handleRunSolution(context.Background(), request)
	require.NoError(t, err)
	require.False(t, result.IsError, "expected success, got: %+v", result.Content)

	var body map[string]any
	text := result.Content[0].(mcp.TextContent).Text
	require.NoError(t, json.Unmarshal([]byte(text), &body))
	assert.Equal(t, "success", body["status"])
	assert.Equal(t, "resolver-only", body["type"])
	assert.Equal(t, "resolver-only-test", body["solution"])
}

func TestHandleRunSolution_WithWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	xdg.Reload()
	t.Cleanup(func() { xdg.Reload() })

	outputDir := filepath.Join(tmpDir, "output")

	solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: workflow-test
spec:
  resolvers:
    filename:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'test.txt'"
  workflow:
    actions:
      write-file:
        provider: file
        inputs:
          operation: write
          path: "{{ ._.filename }}"
          content: "hello from MCP"
`
	solFile := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "run_solution"
	request.Params.Arguments = map[string]any{
		"path":       solFile,
		"output_dir": outputDir,
	}

	result, err := srv.handleRunSolution(context.Background(), request)
	require.NoError(t, err)
	require.False(t, result.IsError, "expected success, got: %+v", result.Content)

	var body map[string]any
	text := result.Content[0].(mcp.TextContent).Text
	require.NoError(t, json.Unmarshal([]byte(text), &body))
	assert.Equal(t, "succeeded", body["status"])
	assert.Equal(t, "workflow-test", body["solution"])

	// Verify action results are present.
	actions, ok := body["actions"]
	assert.True(t, ok, "expected actions in response")
	actionList := actions.([]any)
	assert.Len(t, actionList, 1)
}

func TestBuildRunResult(t *testing.T) {
	t.Parallel()

	// Minimal smoke test for the result builder.
	sol := &solution.Solution{}
	sol.Metadata.Name = "test-sol"

	now := time.Now()
	later := now.Add(100 * time.Millisecond)
	actionResult := &action.ExecutionResult{
		FinalStatus: action.ExecutionSucceeded,
		StartTime:   now,
		EndTime:     later,
		Actions: map[string]*action.ActionResult{
			"step-1": {
				Status:    action.StatusSucceeded,
				StartTime: &now,
				EndTime:   &later,
			},
		},
	}

	result := buildRunResult(sol, map[string]any{"key": "val"}, actionResult, true)
	assert.Equal(t, "succeeded", result["status"])
	assert.Equal(t, "test-sol", result["solution"])
	assert.NotNil(t, result["duration"])
	assert.NotNil(t, result["resolverData"])

	actions := result["actions"].([]map[string]any)
	assert.Len(t, actions, 1)
	assert.Equal(t, "step-1", actions[0]["name"])
}
