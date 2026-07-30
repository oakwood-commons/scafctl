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
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleListSolutions(t *testing.T) {
	t.Run("returns empty when no catalog", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_solutions"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListSolutions(context.Background(), request)
		require.NoError(t, err)
		// Either returns an empty list or an error about catalog — both are acceptable
		require.NotNil(t, result)
	})

	t.Run("accepts name filter", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_solutions"
		request.Params.Arguments = map[string]any{
			"name": "nonexistent-solution",
		}

		result, err := srv.handleListSolutions(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
	})
}

func TestHandleInspectSolution(t *testing.T) {
	t.Run("returns error when path is missing", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "inspect_solution"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleInspectSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("returns error for nonexistent solution", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "inspect_solution"
		request.Params.Arguments = map[string]any{
			"path": "/nonexistent/solution.yaml",
		}

		result, err := srv.handleInspectSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "loading solution")
	})

	t.Run("returns explanation for valid solution file", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		// Create a valid solution file
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "valid-solution.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-inspect
  version: 1.0.0
  description: A solution for inspecting
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'Hello!'"
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		request := mcp.CallToolRequest{}
		request.Params.Name = "inspect_solution"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handleInspectSolution(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var explanation map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &explanation))
		assert.Contains(t, explanation, "name")
		assert.Equal(t, "test-inspect", explanation["name"])

		// Verify resolvers include source positions
		resolvers := explanation["resolvers"].([]any)
		require.NotEmpty(t, resolvers)
		firstResolver := resolvers[0].(map[string]any)
		sourcePos := firstResolver["sourcePos"]
		require.NotNil(t, sourcePos, "sourcePos should be present in inspection resolvers")
		sp := sourcePos.(map[string]any)
		assert.Greater(t, sp["line"], float64(0), "line should be > 0")
	})
}

func TestHandleLintSolution(t *testing.T) {
	t.Run("returns error when file is missing", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "lint_solution"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleLintSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("returns error for nonexistent file", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "lint_solution"
		request.Params.Arguments = map[string]any{
			"file": "/nonexistent/solution.yaml",
		}

		result, err := srv.handleLintSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "loading solution")
	})

	t.Run("lints a valid solution file", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		// Use one of the example solution files in the repo
		request := mcp.CallToolRequest{}
		request.Params.Name = "lint_solution"
		request.Params.Arguments = map[string]any{
			"file": "../../examples/actions/conditional-execution.yaml",
		}

		result, err := srv.handleLintSolution(context.Background(), request)
		require.NoError(t, err)
		// Whether it has findings or not, it should return valid JSON
		require.NotNil(t, result)
		if !result.IsError {
			text := result.Content[0].(mcp.TextContent).Text
			var lintResult map[string]any
			require.NoError(t, json.Unmarshal([]byte(text), &lintResult))
			assert.Contains(t, lintResult, "file")
			assert.Contains(t, lintResult, "findings")
		}
	})

	t.Run("severity filter works", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "lint_solution"
		request.Params.Arguments = map[string]any{
			"file":     "../../examples/actions/conditional-execution.yaml",
			"severity": "error",
		}

		result, err := srv.handleLintSolution(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
	})
}

func TestHandleRenderSolution(t *testing.T) {
	t.Parallel()
	t.Run("missing path returns error", func(t *testing.T) {
		t.Parallel()
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_solution"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleRenderSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("nonexistent path returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_solution"
		request.Params.Arguments = map[string]any{
			"path": "/nonexistent/solution.yaml",
		}

		result, err := srv.handleRenderSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "loading solution")
	})

	t.Run("render action graph from example", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_solution"
		request.Params.Arguments = map[string]any{
			"path": "../../examples/actions/hello-world.yaml",
		}

		result, err := srv.handleRenderSolution(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		if !result.IsError {
			text := result.Content[0].(mcp.TextContent).Text
			assert.NotEmpty(t, text)
			// Action graph is rendered as JSON text
			assert.Contains(t, text, "actions")
		}
	})

	t.Run("render resolver graph from example", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_solution"
		request.Params.Arguments = map[string]any{
			"path":       "../../examples/actions/hello-world.yaml",
			"graph_type": "resolver",
		}

		result, err := srv.handleRenderSolution(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		if !result.IsError {
			text := result.Content[0].(mcp.TextContent).Text
			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(text), &parsed))
			assert.Contains(t, parsed, "nodes")
			assert.Contains(t, parsed, "phases")
		}
	})

	t.Run("render action-deps graph from example", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_solution"
		request.Params.Arguments = map[string]any{
			"path":       "../../examples/actions/hello-world.yaml",
			"graph_type": "action-deps",
		}

		result, err := srv.handleRenderSolution(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		if !result.IsError {
			text := result.Content[0].(mcp.TextContent).Text
			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(text), &parsed))
			assert.Contains(t, parsed, "phases")
			assert.Contains(t, parsed, "stats")
		}
	})

	t.Run("solution without workflow auto-falls back to resolver graph", func(t *testing.T) {
		// Create a solution with only resolvers, no workflow
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "resolvers-only.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolvers-only
  version: 1.0.0
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
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_solution"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handleRenderSolution(context.Background(), request)
		require.NoError(t, err)
		// Should auto-fallback to resolver graph instead of erroring
		assert.False(t, result.IsError, "resolver-only solution should auto-fallback to resolver graph")
		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Contains(t, parsed, "nodes")
	})

	t.Run("solution without resolvers returns error for resolver graph", func(t *testing.T) {
		// Create a solution with workflow but no resolvers
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "workflow-only.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: workflow-only
  version: 1.0.0
spec:
  workflow:
    actions:
      greet:
        provider: message
        inputs:
          message: "hello"
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_solution"
		request.Params.Arguments = map[string]any{
			"path":       solFile,
			"graph_type": "resolver",
		}

		result, err := srv.handleRenderSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "resolvers")
	})

	t.Run("invalid graph_type defaults to action", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_solution"
		request.Params.Arguments = map[string]any{
			"path":       "../../examples/actions/hello-world.yaml",
			"graph_type": "unknown-type",
		}

		result, err := srv.handleRenderSolution(context.Background(), request)
		require.NoError(t, err)
		// Default case falls through to action graph rendering
		require.NotNil(t, result)
	})

	t.Run("invalid params type returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_solution"
		request.Params.Arguments = map[string]any{
			"path":   "../../examples/actions/hello-world.yaml",
			"params": "not-an-object",
		}

		result, err := srv.handleRenderSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "params")
	})
}

func TestHandleRenderEffectiveSolution(t *testing.T) {
	t.Parallel()

	t.Run("missing path returns error", func(t *testing.T) {
		t.Parallel()
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_effective_solution"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleRenderEffectiveSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("nonexistent path returns error", func(t *testing.T) {
		t.Parallel()
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_effective_solution"
		request.Params.Arguments = map[string]any{
			"path": "/nonexistent/solution.yaml",
		}

		result, err := srv.handleRenderEffectiveSolution(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "loading solution")
	})

	t.Run("renders effective document as yaml by default", func(t *testing.T) {
		t.Parallel()
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_effective_solution"
		request.Params.Arguments = map[string]any{
			"path": "../../examples/actions/hello-world.yaml",
		}

		result, err := srv.handleRenderEffectiveSolution(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.False(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "apiVersion:")
		assert.Contains(t, text, "kind: Solution")
	})

	t.Run("renders workflow section as json", func(t *testing.T) {
		t.Parallel()
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_effective_solution"
		request.Params.Arguments = map[string]any{
			"path":    "../../examples/actions/hello-world.yaml",
			"section": "workflow",
			"format":  "json",
		}

		result, err := srv.handleRenderEffectiveSolution(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.False(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Contains(t, parsed, "actions")
	})

	t.Run("invalid section returns error", func(t *testing.T) {
		t.Parallel()
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "render_effective_solution"
		request.Params.Arguments = map[string]any{
			"path":    "../../examples/actions/hello-world.yaml",
			"section": "bogus",
		}

		result, err := srv.handleRenderEffectiveSolution(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "rendering effective solution")
	})
}

func TestHandlePreviewResolvers(t *testing.T) {
	t.Run("missing path returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_resolvers"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handlePreviewResolvers(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("nonexistent path returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_resolvers"
		request.Params.Arguments = map[string]any{
			"path": "/nonexistent/solution.yaml",
		}

		result, err := srv.handlePreviewResolvers(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("solution without resolvers returns empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "no-resolvers.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: no-resolvers
  version: 1.0.0
spec:
  workflow:
    actions:
      greet:
        provider: message
        inputs:
          message: "hello"
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_resolvers"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handlePreviewResolvers(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Contains(t, parsed, "message")
	})

	t.Run("previews static resolvers", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "static-resolvers.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: static-resolvers
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'Hello World'"
    count:
      type: int
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "42"
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_resolvers"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handlePreviewResolvers(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, float64(2), parsed["total"])
		assert.Equal(t, float64(2), parsed["resolved"])

		resolvers := parsed["resolvers"].(map[string]any)
		greeting := resolvers["greeting"].(map[string]any)
		assert.Equal(t, "Hello World", greeting["value"])
		assert.Equal(t, "resolved", greeting["status"])
		assert.Equal(t, "cel", greeting["provider"])

		// Verify source position is included
		sourcePos := greeting["sourcePos"]
		require.NotNil(t, sourcePos, "sourcePos should be present for resolvers")
		sp := sourcePos.(map[string]any)
		assert.Greater(t, sp["line"], float64(0), "line should be > 0")
		assert.Greater(t, sp["column"], float64(0), "column should be > 0")
	})

	t.Run("invalid params type returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "sol.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test
  version: 1.0.0
spec:
  resolvers:
    x:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'ok'"
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_resolvers"
		request.Params.Arguments = map[string]any{
			"path":   solFile,
			"params": "not-an-object",
		}

		result, err := srv.handlePreviewResolvers(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("response includes __plan topology", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "plan-preview.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: plan-preview
  version: 1.0.0
spec:
  resolvers:
    base:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'root'"
    derived:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "_.base + '-derived'"
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_resolvers"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handlePreviewResolvers(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))

		// __plan topology should be present in the response
		plan, ok := parsed["plan"]
		assert.True(t, ok, "plan should be present in preview_resolvers response")
		require.NotNil(t, plan)

		planMap, ok := plan.(map[string]any)
		require.True(t, ok, "plan should be map[string]any")
		assert.Contains(t, planMap, "base", "plan should contain base resolver")
		assert.Contains(t, planMap, "derived", "plan should contain derived resolver")

		basePlan, ok := planMap["base"].(map[string]any)
		require.True(t, ok, "base plan entry should be map[string]any")
		assert.Equal(t, float64(1), basePlan["phase"], "base should be in phase 1")
		assert.Equal(t, float64(0), basePlan["dependencyCount"], "base should have no dependencies")

		derivedPlan, ok := planMap["derived"].(map[string]any)
		require.True(t, ok, "derived plan entry should be map[string]any")
		assert.Equal(t, float64(2), derivedPlan["phase"], "derived should be in phase 2")
		assert.Equal(t, float64(1), derivedPlan["dependencyCount"], "derived should have 1 dependency")
	})

	t.Run("validation failure is non-fatal by default and includes values plus diagnostics", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "badval.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: badval
  version: 1.0.0
spec:
  resolvers:
    name:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "Bob"
      validate:
        with:
          - provider: validation
            inputs:
              match: "^Alice$"
              message: "name must be Alice"
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_resolvers"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handlePreviewResolvers(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError, "validation failure must be non-fatal by default")

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))

		assert.Equal(t, false, parsed["valid"], "valid should be false when validation fails")
		diagnostics, ok := parsed["diagnostics"].([]any)
		require.True(t, ok, "diagnostics should be present")
		assert.NotEmpty(t, diagnostics)

		resolvers := parsed["resolvers"].(map[string]any)
		name := resolvers["name"].(map[string]any)
		assert.Equal(t, "Bob", name["value"], "partial value must still be returned")
		assert.Equal(t, "failed", name["status"])
	})

	t.Run("strict mode reports error but still includes values", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "badval-strict.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: badval-strict
  version: 1.0.0
spec:
  resolvers:
    name:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "Bob"
      validate:
        with:
          - provider: validation
            inputs:
              match: "^Alice$"
              message: "name must be Alice"
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_resolvers"
		request.Params.Arguments = map[string]any{
			"path":   solFile,
			"strict": true,
		}

		result, err := srv.handlePreviewResolvers(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError, "strict mode must report an error on validation failure")

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))

		assert.Equal(t, false, parsed["valid"])
		resolvers := parsed["resolvers"].(map[string]any)
		name := resolvers["name"].(map[string]any)
		assert.Equal(t, "Bob", name["value"], "values must still be included in strict mode")
	})

	t.Run("resolve-phase failure preserves successfully-resolved values plus diagnostics", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "resolve-fail.yaml")
		// "good" resolves to a plain string; "bad" hard-fails in the resolve
		// phase (an undefined Go-template function fails to parse, so the source
		// produces no value). The successful value must survive.
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolve-fail
  version: 1.0.0
spec:
  resolvers:
    good:
      resolve:
        with:
          - provider: static
            inputs:
              value: i-resolved
    bad:
      resolve:
        with:
          - provider: go-template
            inputs:
              template: "{{ undefinedFunc .x }}"
              data:
                x: hello
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_resolvers"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handlePreviewResolvers(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError,
			"a resolve-phase failure must be non-fatal by default (values preserved)")

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))

		assert.Equal(t, false, parsed["valid"], "valid should be false on a resolve-phase failure")
		diagnostics, ok := parsed["diagnostics"].([]any)
		require.True(t, ok, "diagnostics should be present on a resolve-phase failure")
		assert.NotEmpty(t, diagnostics)

		resolvers := parsed["resolvers"].(map[string]any)
		good := resolvers["good"].(map[string]any)
		assert.Equal(t, "i-resolved", good["value"],
			"a successfully-resolved value must survive a sibling resolve-phase failure")
		assert.Equal(t, "resolved", good["status"])

		bad := resolvers["bad"].(map[string]any)
		assert.Equal(t, "failed", bad["status"], "the failed resolver must be reported as failed")
	})
}

// TestHandlePreviewResolvers_RelativeToSolution verifies that resolver file
// reads using relativeTo: solution resolve against the solution file's
// directory rather than the MCP server's process CWD (issue #607). The server
// process CWD (the test package directory) intentionally differs from the
// solution's temp directory, so a read that succeeds proves the solution
// directory is anchored on the context.
func TestHandlePreviewResolvers_RelativeToSolution(t *testing.T) {
	const fileContent = "hello from solution dir"

	newSolution := func(t *testing.T) string {
		t.Helper()
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "data.txt"), []byte(fileContent), 0o644))
		solFile := filepath.Join(tmpDir, "reads-relative.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: reads-relative
  version: 1.0.0
spec:
  resolvers:
    fileData:
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: data.txt
              relativeTo: solution
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))
		return solFile
	}

	assertResolved := func(t *testing.T, result *mcp.CallToolResult) {
		t.Helper()
		require.False(t, result.IsError, "read with relativeTo: solution should succeed regardless of server CWD")

		require.NotEmpty(t, result.Content, "tool result should include content")
		textContent, ok := result.Content[0].(mcp.TextContent)
		require.True(t, ok, "first content item should be text, got %T", result.Content[0])
		text := textContent.Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))

		resolvers := parsed["resolvers"].(map[string]any)
		fileData := resolvers["fileData"].(map[string]any)
		assert.Equal(t, "resolved", fileData["status"])
		value := fileData["value"].(map[string]any)
		assert.Equal(t, fileContent, value["content"])
	}

	t.Run("without cwd anchors to solution dir", func(t *testing.T) {
		solFile := newSolution(t)

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_resolvers"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handlePreviewResolvers(context.Background(), request)
		require.NoError(t, err)
		assertResolved(t, result)
	})

	t.Run("cwd override does not affect relativeTo solution", func(t *testing.T) {
		solFile := newSolution(t)
		// An unrelated working directory that does NOT contain data.txt.
		otherDir := t.TempDir()

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "preview_resolvers"
		request.Params.Arguments = map[string]any{
			"path": solFile,
			"cwd":  otherDir,
		}

		result, err := srv.handlePreviewResolvers(context.Background(), request)
		require.NoError(t, err)
		assertResolved(t, result)
	})
}

// TestHandleSolutionExecutors_RelativeToSolution verifies that the remaining
// solution-executing tools (render_solution, run_solution, dry_run_solution,
// preview_action) anchor resolver file reads using relativeTo: solution to the
// solution file's directory rather than the MCP server's process CWD
// (issue #607). Each solution reads data.txt via relativeTo: solution and
// surfaces the content through a message action, so the resolved value appears
// in every tool's output. The server process CWD (the test package directory)
// intentionally differs from the solution's temp directory, so the content
// only appears when the solution directory is anchored on the context.
func TestHandleSolutionExecutors_RelativeToSolution(t *testing.T) {
	const fileContent = "hello from solution dir"

	newSolution := func(t *testing.T) string {
		t.Helper()
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "data.txt"), []byte(fileContent), 0o644))
		solFile := filepath.Join(tmpDir, "reads-relative.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: reads-relative
  version: 1.0.0
spec:
  resolvers:
    fileData:
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: data.txt
              relativeTo: solution
  workflow:
    actions:
      showData:
        provider: message
        inputs:
          message:
            expr: _.fileData.content
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))
		return solFile
	}

	type handlerFn func(*Server, context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)

	tools := []struct {
		tool    string
		handler handlerFn
	}{
		{"render_solution", (*Server).handleRenderSolution},
		{"run_solution", (*Server).handleRunSolution},
		{"dry_run_solution", (*Server).handleDryRunSolution},
		{"preview_action", (*Server).handlePreviewAction},
	}

	scenarios := []struct {
		name    string
		withCwd bool
	}{
		{"without cwd anchors to solution dir", false},
		{"cwd override does not affect relativeTo solution", true},
	}

	for _, tc := range tools {
		for _, sc := range scenarios {
			t.Run(tc.tool+"/"+sc.name, func(t *testing.T) {
				solFile := newSolution(t)

				srv, err := NewServer(WithServerVersion("test"))
				require.NoError(t, err)

				args := map[string]any{"path": solFile}
				if sc.withCwd {
					// An unrelated working directory that does NOT contain data.txt.
					args["cwd"] = t.TempDir()
				}

				request := mcp.CallToolRequest{}
				request.Params.Name = tc.tool
				request.Params.Arguments = args

				result, err := tc.handler(srv, context.Background(), request)
				require.NoError(t, err)
				require.False(t, result.IsError, "read with relativeTo: solution should succeed regardless of server CWD")

				require.NotEmpty(t, result.Content, "tool result should include content")
				textContent, ok := result.Content[0].(mcp.TextContent)
				require.True(t, ok, "first content item should be text, got %T", result.Content[0])
				text := textContent.Text
				assert.Contains(t, text, fileContent, "resolved file content should appear in the %s output", tc.tool)
			})
		}
	}
}

// TestWithSolutionDir verifies the withSolutionDir helper: an empty directory
// leaves the context untouched (stdin / unbundled catalog runs), while a
// non-empty directory is anchored on the context for solution-relative reads.
func TestWithSolutionDir(t *testing.T) {
	t.Run("empty solutionDir leaves context unchanged", func(t *testing.T) {
		ctx := context.Background()
		got := withSolutionDir(ctx, "")

		_, ok := provider.SolutionDirectoryFromContext(got)
		assert.False(t, ok, "no solution directory should be set for empty input")
	})

	t.Run("non-empty solutionDir is anchored on the context", func(t *testing.T) {
		dir := t.TempDir()
		got := withSolutionDir(context.Background(), dir)

		solDir, ok := provider.SolutionDirectoryFromContext(got)
		require.True(t, ok, "solution directory should be set")
		assert.Equal(t, dir, solDir)
	})
}

func TestHandleGetRunCommand(t *testing.T) {
	t.Run("missing path returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_run_command"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleGetRunCommand(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("solution with workflow returns run solution", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "with-workflow.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: with-workflow
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'hello'"
  workflow:
    actions:
      greet:
        provider: message
        inputs:
          message: "hello"
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_run_command"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handleGetRunCommand(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, "scafctl run solution", parsed["subcommand"])
		assert.Equal(t, true, parsed["hasWorkflow"])
		assert.Equal(t, true, parsed["hasResolvers"])
		assert.Contains(t, parsed["command"], "run solution")
	})

	t.Run("solution without workflow returns run resolver", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "resolvers-only.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolvers-only
  version: 1.0.0
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
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_run_command"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handleGetRunCommand(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		assert.Equal(t, "scafctl run resolver", parsed["subcommand"])
		assert.Equal(t, false, parsed["hasWorkflow"])
		assert.Contains(t, parsed["command"], "run resolver")
	})

	t.Run("solution with parameter resolvers includes flags", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "with-params.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: with-params
  version: 1.0.0
spec:
  resolvers:
    env:
      type: string
      description: "Target environment"
      resolve:
        with:
          - provider: parameter
            inputs:
              prompt: "Enter environment"
    region:
      type: string
      description: "AWS region"
      example: "us-east-1"
      resolve:
        with:
          - provider: parameter
            inputs:
              prompt: "Enter region"
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_run_command"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handleGetRunCommand(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))

		// Check parameters are detected
		params := parsed["parameters"].([]any)
		assert.Len(t, params, 2)

		// Check command includes positional key=value (resolver-only uses positional syntax)
		cmd := parsed["command"].(string)
		assert.Contains(t, cmd, "env=")
		assert.Contains(t, cmd, "region=us-east-1")
		assert.NotContains(t, cmd, "-r env=")
		assert.NotContains(t, cmd, "-r region=")
	})

	t.Run("empty solution returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "empty.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: empty
  version: 1.0.0
spec: {}
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_run_command"
		request.Params.Arguments = map[string]any{
			"path": solFile,
		}

		result, err := srv.handleGetRunCommand(context.Background(), request)
		require.NoError(t, err)
		// Should return a result with error info (not IsError, but contains 'error' key)
		assert.False(t, result.IsError)
		text := result.Content[0].(mcp.TextContent).Text
		assert.Contains(t, text, "error")
	})

	t.Run("bare filename gets ./ prefix in command", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "my-solution.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-solution
  version: 1.0.0
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
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		// Change to tmpDir so bare filename resolves
		origDir, _ := os.Getwd()
		require.NoError(t, os.Chdir(tmpDir))
		t.Cleanup(func() { os.Chdir(origDir) })

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_run_command"
		request.Params.Arguments = map[string]any{
			"path": "my-solution.yaml", // bare filename, no ./
		}

		result, err := srv.handleGetRunCommand(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		cmd := parsed["command"].(string)
		assert.Contains(t, cmd, "-f ./my-solution.yaml", "bare filenames should get ./ prefix")
	})

	t.Run("on_conflict flag appended to command", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "conflict-test.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: conflict-test
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'hello'"
  workflow:
    actions:
      write-file:
        provider: file
        inputs:
          operation: write
          path: out.txt
          content:
            rslvr: greeting
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_run_command"
		request.Params.Arguments = map[string]any{
			"path":        solFile,
			"on_conflict": "overwrite",
		}

		result, err := srv.handleGetRunCommand(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		cmd := parsed["command"].(string)
		assert.Contains(t, cmd, "--on-conflict overwrite")
	})

	t.Run("backup flag appended to command", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "backup-test.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: backup-test
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'hello'"
  workflow:
    actions:
      write-file:
        provider: file
        inputs:
          operation: write
          path: out.txt
          content:
            rslvr: greeting
`
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_run_command"
		request.Params.Arguments = map[string]any{
			"path":        solFile,
			"on_conflict": "skip",
			"backup":      true,
		}

		result, err := srv.handleGetRunCommand(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		cmd := parsed["command"].(string)
		assert.Contains(t, cmd, "--on-conflict skip")
		assert.Contains(t, cmd, "--backup")
	})

	t.Run("on_conflict rejected for resolver-only solution", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "resolvers-only.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolvers-only
  version: 1.0.0
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
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_run_command"
		request.Params.Arguments = map[string]any{
			"path":        solFile,
			"on_conflict": "overwrite",
		}

		result, err := srv.handleGetRunCommand(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError, "should return error for on_conflict with resolver-only solution")
	})

	t.Run("backup rejected for resolver-only solution", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "resolvers-only.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolvers-only
  version: 1.0.0
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
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_run_command"
		request.Params.Arguments = map[string]any{
			"path":   solFile,
			"backup": true,
		}

		result, err := srv.handleGetRunCommand(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError, "should return error for backup with resolver-only solution")
	})

	t.Run("show_execution flag appended to command", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "show-execution-test.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: show-execution-test
  version: 1.0.0
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
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_run_command"
		request.Params.Arguments = map[string]any{
			"path":           solFile,
			"show_execution": true,
		}

		result, err := srv.handleGetRunCommand(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		cmd, ok := parsed["command"].(string)
		require.True(t, ok, "command should be a string")
		assert.Contains(t, cmd, "--show-execution", "show_execution=true should add --show-execution flag")
	})

	t.Run("show_execution false omits flag", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "no-show-execution.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: no-show-execution
  version: 1.0.0
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
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_run_command"
		request.Params.Arguments = map[string]any{
			"path":           solFile,
			"show_execution": false,
		}

		result, err := srv.handleGetRunCommand(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		cmd, ok := parsed["command"].(string)
		require.True(t, ok, "command should be a string")
		assert.NotContains(t, cmd, "--show-execution", "show_execution=false should not add --show-execution flag")
	})
}

func TestHandleRunSolutionTests(t *testing.T) {
	t.Run("missing path returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "run_solution_tests"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleRunSolutionTests(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("nonexistent path returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)
		missingPath := filepath.Join(t.TempDir(), "does-not-exist")

		request := mcp.CallToolRequest{}
		request.Params.Name = "run_solution_tests"
		request.Params.Arguments = map[string]any{
			"path": missingPath,
		}

		result, err := srv.handleRunSolutionTests(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("directory with no tests returns empty results", func(t *testing.T) {
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "no-tests.yaml")
		solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: no-tests
  version: 1.0.0
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
		require.NoError(t, os.WriteFile(solFile, []byte(solContent), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "run_solution_tests"
		request.Params.Arguments = map[string]any{
			"path":          solFile,
			"skip_builtins": true,
		}

		result, err := srv.handleRunSolutionTests(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &parsed))
		summary := parsed["summary"].(map[string]any)
		assert.Equal(t, float64(0), summary["total"])
	})
}

func TestBuildTestResultItems(t *testing.T) {
	relaxed := soltesting.TestResult{
		Solution:    "sol",
		Test:        "golden",
		Status:      soltesting.StatusPass,
		Relaxed:     true,
		MaskMatches: map[string]int{"greeting": 1, "uuid": 2},
	}
	plain := soltesting.TestResult{
		Solution: "sol",
		Test:     "plain",
		Status:   soltesting.StatusPass,
		Assertions: []soltesting.AssertionResult{
			{Type: "contains", Input: "x", Passed: true},
		},
	}
	failed := soltesting.TestResult{
		Solution: "sol",
		Test:     "broken",
		Status:   soltesting.StatusFail,
		Message:  "assertion failed",
		Assertions: []soltesting.AssertionResult{
			{Type: "contains", Input: "y", Passed: false, Message: "missing"},
		},
	}

	t.Run("surfaces relaxed and mask matches", func(t *testing.T) {
		items := buildTestResultItems([]soltesting.TestResult{relaxed}, false)
		require.Len(t, items, 1)
		assert.True(t, items[0].Relaxed)
		assert.Equal(t, map[string]int{"greeting": 1, "uuid": 2}, items[0].MaskMatches)
	})

	t.Run("omits relaxed fields for non-relaxed results", func(t *testing.T) {
		items := buildTestResultItems([]soltesting.TestResult{plain}, false)
		require.Len(t, items, 1)
		assert.False(t, items[0].Relaxed)
		assert.Nil(t, items[0].MaskMatches)
	})

	t.Run("includes assertions only for failures when not verbose", func(t *testing.T) {
		items := buildTestResultItems([]soltesting.TestResult{plain, failed}, false)
		require.Len(t, items, 2)
		assert.Empty(t, items[0].Assertions, "passing test should omit assertions when not verbose")
		assert.Len(t, items[1].Assertions, 1, "failed test should include assertions")
	})

	t.Run("includes assertions for all tests when verbose", func(t *testing.T) {
		items := buildTestResultItems([]soltesting.TestResult{plain, failed}, true)
		require.Len(t, items, 2)
		assert.Len(t, items[0].Assertions, 1)
		assert.Len(t, items[1].Assertions, 1)
	})

	t.Run("serializes relaxed fields to JSON and omits when empty", func(t *testing.T) {
		items := buildTestResultItems([]soltesting.TestResult{relaxed, plain}, false)
		data, err := json.Marshal(items)
		require.NoError(t, err)
		var decoded []map[string]any
		require.NoError(t, json.Unmarshal(data, &decoded))
		require.Len(t, decoded, 2)
		assert.Equal(t, true, decoded[0]["relaxed"])
		assert.Contains(t, decoded[0], "maskMatches")
		assert.NotContains(t, decoded[1], "relaxed")
		assert.NotContains(t, decoded[1], "maskMatches")
	})
}

func TestHandleGetSolutionUsage(t *testing.T) {
	t.Parallel()

	const usageSol = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: usage-mcp
  version: 1.0.0
  description: Fallback description
  usage:
    synopsis: Consume via the action parameter
spec:
  resolvers:
    action:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: action
              default: show
  workflow:
    actions:
      show:
        description: Show summary
        when: '_.action == "show"'
        provider: message
        inputs:
          message: showing
      refresh:
        description: Refresh data
        when: '_.action == "refresh"'
        provider: message
        inputs:
          message: refreshing
`

	t.Run("returns structured usage view", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solFile := filepath.Join(tmpDir, "usage.yaml")
		require.NoError(t, os.WriteFile(solFile, []byte(usageSol), 0o644))

		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_solution_usage"
		request.Params.Arguments = map[string]any{"path": solFile}

		result, err := srv.handleGetSolutionUsage(context.Background(), request)
		require.NoError(t, err)
		require.False(t, result.IsError)

		text := result.Content[0].(mcp.TextContent).Text
		var usage map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &usage))

		assert.Equal(t, "usage-mcp", usage["name"])
		assert.Equal(t, "Consume via the action parameter", usage["synopsis"])
		params := usage["parameters"].([]any)
		require.Len(t, params, 1)
		p0 := params[0].(map[string]any)
		assert.Equal(t, "show", p0["default"])
		allowed := p0["allowedValues"].([]any)
		assert.ElementsMatch(t, []any{"refresh", "show"}, allowed)
		assert.Len(t, usage["actions"].([]any), 2)
	})

	t.Run("missing path returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_solution_usage"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleGetSolutionUsage(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("nonexistent solution returns load error", func(t *testing.T) {
		t.Parallel()
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_solution_usage"
		request.Params.Arguments = map[string]any{"path": "/nonexistent/solution.yaml"}

		result, err := srv.handleGetSolutionUsage(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}
