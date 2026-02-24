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

func TestHandleGenerateTestScaffold(t *testing.T) {
	t.Run("generates scaffold for solution with resolvers", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		// Find a solution file with resolvers
		solPath := findTestSolutionFile(t)

		request := mcp.CallToolRequest{}
		request.Params.Name = "generate_test_scaffold"
		request.Params.Arguments = map[string]any{
			"path": solPath,
		}

		result, err := srv.handleGenerateTestScaffold(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		assert.NotEmpty(t, data["yaml"])
		testCount := data["testCount"].(float64)
		assert.Greater(t, testCount, float64(0))

		coverage, ok := data["coverage"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, coverage, "resolversWithTests")
		assert.Contains(t, coverage, "resolversWithoutTests")

		nextSteps, ok := data["nextSteps"].([]any)
		require.True(t, ok)
		assert.NotEmpty(t, nextSteps)
	})

	t.Run("requires path parameter", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleGenerateTestScaffold(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("errors on nonexistent file", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"path": "/nonexistent/solution.yaml",
		}

		result, err := srv.handleGenerateTestScaffold(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

func TestHandleListTests(t *testing.T) {
	t.Run("discovers tests in solution file", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		// Find a solution with tests
		solPath := findTestSolutionWithTests(t)
		if solPath == "" {
			t.Skip("no solution with tests found")
		}

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_tests"
		request.Params.Arguments = map[string]any{
			"path": solPath,
		}

		result, err := srv.handleListTests(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		totalTests := data["totalTests"].(float64)
		assert.Greater(t, totalTests, float64(0))

		totalSolutions := data["totalSolutions"].(float64)
		assert.Greater(t, totalSolutions, float64(0))

		solutions, ok := data["solutions"].([]any)
		require.True(t, ok)
		assert.NotEmpty(t, solutions)

		firstSol := solutions[0].(map[string]any)
		assert.NotEmpty(t, firstSol["solution"])
		tests := firstSol["tests"].([]any)
		assert.NotEmpty(t, tests)
	})

	t.Run("errors on nonexistent path", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"path": "/nonexistent/dir",
		}

		result, err := srv.handleListTests(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

// findTestSolutionFile locates a solution YAML file suitable for testing.
func findTestSolutionFile(t *testing.T) string {
	t.Helper()

	// Try examples directory first
	candidates := []string{
		"examples/solutions/email-notifier/solution.yaml",
		"examples/solutions/comprehensive/solution.yaml",
		"examples/solutions/terraform/solution.yaml",
	}

	for _, c := range candidates {
		path := filepath.Join(projectRoot(t), c)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	t.Fatal("no test solution file found")
	return ""
}

// findTestSolutionWithTests locates a solution file that has spec.testing.cases.
func findTestSolutionWithTests(t *testing.T) string {
	t.Helper()

	candidates := []string{
		"examples/solutions/tested-solution/solution.yaml",
		"examples/solutions/comprehensive/solution.yaml",
	}

	for _, c := range candidates {
		path := filepath.Join(projectRoot(t), c)
		if _, err := os.Stat(path); err == nil {
			content, err := os.ReadFile(path)
			if err == nil && contains(string(content), "testing:") {
				return path
			}
		}
	}

	return ""
}

// projectRoot returns the project root directory.
func projectRoot(t *testing.T) string {
	t.Helper()
	// pkg/mcp/ is 2 levels from root
	dir, err := filepath.Abs(filepath.Join(".", "..", ".."))
	require.NoError(t, err)
	return dir
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && len(substr) > 0 && findString(s, substr) >= 0
}

func findString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
