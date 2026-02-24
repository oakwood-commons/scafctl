// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"regexp"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/examples"
	"github.com/oakwood-commons/scafctl/pkg/schema"
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

func TestHandleUpdateSolutionPrompt(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("with all arguments", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "update_solution"
		request.Params.Arguments = map[string]string{
			"path":   "solutions/my-solution/solution.yaml",
			"change": "add retry logic to the deploy action",
		}

		result, err := srv.handleUpdateSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Messages)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "solutions/my-solution/solution.yaml")
		assert.Contains(t, text, "add retry logic to the deploy action")
		assert.Contains(t, text, "inspect_solution")
		assert.Contains(t, text, "lint_solution")
		assert.Contains(t, text, "preview_resolvers")
		assert.Contains(t, text, "run_solution_tests")
		assert.Contains(t, text, "get_run_command")
		assert.Contains(t, text, "STEP 1")
		assert.Contains(t, text, "STEP 4")
	})
}

func TestHandleAddTestsPrompt(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("with all arguments", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "add_tests"
		request.Params.Arguments = map[string]string{
			"path":  "solutions/my-solution/solution.yaml",
			"scope": "resolvers",
		}

		result, err := srv.handleAddTestsPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Messages)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "solutions/my-solution/solution.yaml")
		assert.Contains(t, text, "resolvers")
		assert.Contains(t, text, "RESOLVER TESTING TIPS")
		assert.Contains(t, text, "lint_solution")
		assert.Contains(t, text, "run_solution_tests")
		assert.Contains(t, text, "explain_kind")
	})

	t.Run("with default scope", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "add_tests"
		request.Params.Arguments = map[string]string{
			"path": "test.yaml",
		}

		result, err := srv.handleAddTestsPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "COMPREHENSIVE TESTING TIPS") // default scope
	})

	t.Run("actions scope", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "add_tests"
		request.Params.Arguments = map[string]string{
			"path":  "test.yaml",
			"scope": "actions",
		}

		result, err := srv.handleAddTestsPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "ACTION TESTING TIPS")
	})
}

func TestHandleComposeSolutionPrompt(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("with all arguments", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "compose_solution"
		request.Params.Arguments = map[string]string{
			"path": "solutions/my-composed",
			"goal": "modular deploy pipeline with separate resolver and action bundles",
		}

		result, err := srv.handleComposeSolutionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Messages)
		assert.Equal(t, mcp.RoleUser, result.Messages[0].Role)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "solutions/my-composed")
		assert.Contains(t, text, "modular deploy pipeline")
		assert.Contains(t, text, "compose")
		assert.Contains(t, text, "partial")
		assert.Contains(t, text, "deep-merge")
	})
}

func TestHandleFixLintPrompt(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("with all arguments", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "fix_lint"
		request.Params.Arguments = map[string]string{
			"path":     "solutions/my-solution/solution.yaml",
			"severity": "error",
		}

		result, err := srv.handleFixLintPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Messages)
		assert.Equal(t, mcp.RoleUser, result.Messages[0].Role)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "solutions/my-solution/solution.yaml")
		assert.Contains(t, text, "error")
		assert.Contains(t, text, "lint_solution")
		assert.Contains(t, text, "explain_lint_rule")
		assert.Contains(t, text, "STEP 1")
		assert.Contains(t, text, "STEP 4")
		assert.Contains(t, text, "preview_resolvers")
	})

	t.Run("with default severity", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "fix_lint"
		request.Params.Arguments = map[string]string{
			"path": "test.yaml",
		}

		result, err := srv.handleFixLintPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "warning") // default severity
	})
}

func TestHandlePrepareExecutionPrompt(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("with all arguments", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "prepare_execution"
		request.Params.Arguments = map[string]string{
			"path":   "solutions/deploy/solution.yaml",
			"params": "env=prod,region=us-east1",
		}

		result, err := srv.handlePrepareExecutionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Messages)
		assert.Equal(t, mcp.RoleUser, result.Messages[0].Role)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "solutions/deploy/solution.yaml")
		assert.Contains(t, text, "env=prod,region=us-east1")
		assert.Contains(t, text, "inspect_solution")
		assert.Contains(t, text, "lint_solution")
		assert.Contains(t, text, "auth_status")
		assert.Contains(t, text, "preview_resolvers")
		assert.Contains(t, text, "preview_action")
		assert.Contains(t, text, "get_run_command")
		assert.Contains(t, text, "DO NOT run the command yourself")
		assert.Contains(t, text, "STEP 1")
		assert.Contains(t, text, "STEP 6")
	})

	t.Run("without params", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "prepare_execution"
		request.Params.Arguments = map[string]string{
			"path": "test.yaml",
		}

		result, err := srv.handlePrepareExecutionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "test.yaml")
		assert.NotContains(t, text, "User-provided parameters")
	})
}

func TestResolvePathArg(t *testing.T) {
	t.Run("normal file path returns as-is", func(t *testing.T) {
		pathRef, inlineNote := resolvePathArg("solutions/my-solution/solution.yaml")
		assert.Equal(t, "solutions/my-solution/solution.yaml", pathRef)
		assert.Empty(t, inlineNote)
	})

	t.Run("absolute file path returns as-is", func(t *testing.T) {
		pathRef, inlineNote := resolvePathArg("/Users/kcloutie/src/test-solution.yaml")
		assert.Equal(t, "/Users/kcloutie/src/test-solution.yaml", pathRef)
		assert.Empty(t, inlineNote)
	})

	t.Run("YAML content with newlines returns placeholder", func(t *testing.T) {
		content := "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: test"
		pathRef, inlineNote := resolvePathArg(content)
		assert.Equal(t, "<solution_file_path>", pathRef)
		assert.Contains(t, inlineNote, "YAML content instead of a file path")
		assert.Contains(t, inlineNote, "determine the actual file system path")
		assert.Contains(t, inlineNote, content)
	})

	t.Run("content starting with apiVersion without newlines", func(t *testing.T) {
		content := "apiVersion: scafctl.io/v1 kind: Solution"
		pathRef, inlineNote := resolvePathArg(content)
		assert.Equal(t, "<solution_file_path>", pathRef)
		assert.Contains(t, inlineNote, "YAML content instead of a file path")
	})

	t.Run("content starting with kind", func(t *testing.T) {
		content := "kind: Solution\napiVersion: scafctl.io/v1"
		pathRef, inlineNote := resolvePathArg(content)
		assert.Equal(t, "<solution_file_path>", pathRef)
		assert.Contains(t, inlineNote, "YAML content instead of a file path")
	})

	t.Run("very long string treated as content", func(t *testing.T) {
		longPath := "a/" + string(make([]byte, 600))
		pathRef, inlineNote := resolvePathArg(longPath)
		assert.Equal(t, "<solution_file_path>", pathRef)
		assert.NotEmpty(t, inlineNote)
	})

	t.Run("short relative path returns as-is", func(t *testing.T) {
		pathRef, inlineNote := resolvePathArg("test.yaml")
		assert.Equal(t, "test.yaml", pathRef)
		assert.Empty(t, inlineNote)
	})
}

func TestPromptHandlersWithInlineContent(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	yamlContent := "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: test-solution\nspec:\n  resolvers:\n    myInput:\n      type: string"

	t.Run("add_tests with inline content", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "add_tests"
		request.Params.Arguments = map[string]string{
			"path":  yamlContent,
			"scope": "resolvers",
		}

		result, err := srv.handleAddTestsPrompt(context.Background(), request)
		require.NoError(t, err)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "<solution_file_path>", "should use placeholder instead of inline content for tool calls")
		assert.Contains(t, text, "YAML content instead of a file path", "should include inline note")
		assert.Contains(t, text, "apiVersion: scafctl.io/v1", "should include the solution content for context")
		assert.NotContains(t, text, `path "apiVersion:`, "should NOT embed content in tool call arguments")
	})

	t.Run("debug_solution with inline content", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "debug_solution"
		request.Params.Arguments = map[string]string{
			"path":    yamlContent,
			"problem": "test problem",
		}

		result, err := srv.handleDebugSolutionPrompt(context.Background(), request)
		require.NoError(t, err)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "<solution_file_path>")
		assert.Contains(t, text, "YAML content instead of a file path")
	})

	t.Run("update_solution with inline content", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "update_solution"
		request.Params.Arguments = map[string]string{
			"path":   yamlContent,
			"change": "add a resolver",
		}

		result, err := srv.handleUpdateSolutionPrompt(context.Background(), request)
		require.NoError(t, err)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "<solution_file_path>")
		assert.Contains(t, text, "YAML content instead of a file path")
	})

	t.Run("fix_lint with inline content", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "fix_lint"
		request.Params.Arguments = map[string]string{
			"path": yamlContent,
		}

		result, err := srv.handleFixLintPrompt(context.Background(), request)
		require.NoError(t, err)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "<solution_file_path>")
		assert.Contains(t, text, "YAML content instead of a file path")
	})

	t.Run("prepare_execution with inline content", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "prepare_execution"
		request.Params.Arguments = map[string]string{
			"path": yamlContent,
		}

		result, err := srv.handlePrepareExecutionPrompt(context.Background(), request)
		require.NoError(t, err)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "<solution_file_path>")
		assert.Contains(t, text, "YAML content instead of a file path")
	})

	t.Run("migrate_solution with inline content", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "migrate_solution"
		request.Params.Arguments = map[string]string{
			"path":      yamlContent,
			"migration": "add-tests",
		}

		result, err := srv.handleMigrateSolutionPrompt(context.Background(), request)
		require.NoError(t, err)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "<solution_file_path>")
		assert.Contains(t, text, "YAML content instead of a file path")
	})

	t.Run("optimize_solution with inline content", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "optimize_solution"
		request.Params.Arguments = map[string]string{
			"path": yamlContent,
		}

		result, err := srv.handleOptimizeSolutionPrompt(context.Background(), request)
		require.NoError(t, err)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "<solution_file_path>")
		assert.Contains(t, text, "YAML content instead of a file path")
	})

	t.Run("compose_solution with inline content", func(t *testing.T) {
		request := mcp.GetPromptRequest{}
		request.Params.Name = "compose_solution"
		request.Params.Arguments = map[string]string{
			"path": yamlContent,
			"goal": "split into modules",
		}

		result, err := srv.handleComposeSolutionPrompt(context.Background(), request)
		require.NoError(t, err)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "<solution_file_path>")
		assert.Contains(t, text, "YAML content instead of a file path")
	})
}

// TestPromptToolReferencesAreValid runs every prompt handler and validates that
// all tool names, explain_kind field paths, and get_example paths referenced in
// the generated prompt text actually exist. This catches bugs like referencing a
// renamed tool, a wrong field path (e.g. "testing" vs "spec.testing"), or a
// nonexistent example file.
func TestPromptToolReferencesAreValid(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	// Collect all registered tool names from the server.
	registeredTools := make(map[string]bool)
	for name := range srv.mcpServer.ListTools() {
		registeredTools[name] = true
	}
	require.NotEmpty(t, registeredTools, "server should have registered tools")

	// Define all prompts with their required arguments so we can invoke each handler.
	promptCalls := []struct {
		name    string
		handler func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error)
		args    map[string]string
	}{
		{
			name:    "create_solution",
			handler: srv.handleCreateSolutionPrompt,
			args:    map[string]string{"name": "test-solution", "description": "Test", "features": "resolvers, actions"},
		},
		{
			name:    "debug_solution",
			handler: srv.handleDebugSolutionPrompt,
			args:    map[string]string{"path": "test.yaml", "problem": "not working"},
		},
		{
			name:    "add_resolver",
			handler: srv.handleAddResolverPrompt,
			args:    map[string]string{"provider": "static", "purpose": "test"},
		},
		{
			name:    "add_action",
			handler: srv.handleAddActionPrompt,
			args:    map[string]string{"provider": "exec", "purpose": "test"},
		},
		{
			name:    "update_solution",
			handler: srv.handleUpdateSolutionPrompt,
			args:    map[string]string{"path": "test.yaml", "change": "add resolver"},
		},
		{
			name:    "add_tests",
			handler: srv.handleAddTestsPrompt,
			args:    map[string]string{"path": "test.yaml", "scope": "all"},
		},
		{
			name:    "compose_solution",
			handler: srv.handleComposeSolutionPrompt,
			args:    map[string]string{"path": "test.yaml", "goal": "split"},
		},
		{
			name:    "fix_lint",
			handler: srv.handleFixLintPrompt,
			args:    map[string]string{"path": "test.yaml", "severity": "warning"},
		},
		{
			name:    "prepare_execution",
			handler: srv.handlePrepareExecutionPrompt,
			args:    map[string]string{"path": "test.yaml", "params": "env=dev"},
		},
		{
			name:    "analyze_execution",
			handler: srv.handleAnalyzeExecutionPrompt,
			args:    map[string]string{"snapshot_path": "/tmp/snap.json", "previous_snapshot": "/tmp/prev.json", "problem": "fail"},
		},
		{
			name:    "migrate_solution",
			handler: srv.handleMigrateSolutionPrompt,
			args:    map[string]string{"path": "test.yaml", "migration": "add-tests", "target_dir": "./out"},
		},
		{
			name:    "optimize_solution",
			handler: srv.handleOptimizeSolutionPrompt,
			args:    map[string]string{"path": "test.yaml", "focus": "all"},
		},
	}

	// Regex patterns to extract references from prompt text.
	toolCallRe := regexp.MustCompile(`Call ([a-z_]+)`)
	explainKindFieldRe := regexp.MustCompile(`explain_kind with kind "([^"]+)" and field "([^"]+)"`)
	getExamplePathRe := regexp.MustCompile(`get_example with path "([^"]+)"`)

	for _, pc := range promptCalls {
		t.Run(pc.name, func(t *testing.T) {
			request := mcp.GetPromptRequest{}
			request.Params.Name = pc.name
			request.Params.Arguments = pc.args

			result, err := pc.handler(context.Background(), request)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotEmpty(t, result.Messages)

			text := result.Messages[0].Content.(mcp.TextContent).Text
			require.NotEmpty(t, text)

			// Validate every "Call <tool_name>" reference is a registered tool.
			toolMatches := toolCallRe.FindAllStringSubmatch(text, -1)
			for _, match := range toolMatches {
				toolName := match[1]
				assert.True(t, registeredTools[toolName],
					"prompt %q references tool %q which is not registered. Available tools: %v",
					pc.name, toolName, registeredTools)
			}

			// Validate every explain_kind field reference actually resolves.
			fieldMatches := explainKindFieldRe.FindAllStringSubmatch(text, -1)
			for _, match := range fieldMatches {
				kindName := match[1]
				fieldPath := match[2]

				kindDef, ok := schema.GetKind(kindName)
				require.True(t, ok, "prompt %q references kind %q which is not registered", pc.name, kindName)

				fieldInfo, err := schema.IntrospectField(kindDef.TypeInstance, fieldPath)
				assert.NoError(t, err,
					"prompt %q references explain_kind(kind=%q, field=%q) but IntrospectField fails: %v",
					pc.name, kindName, fieldPath, err)
				if err == nil {
					assert.NotEmpty(t, fieldInfo.Name,
						"prompt %q: explain_kind(kind=%q, field=%q) resolved but returned empty field name",
						pc.name, kindName, fieldPath)
				}
			}

			// Validate every get_example path reference points to an existing example.
			exampleMatches := getExamplePathRe.FindAllStringSubmatch(text, -1)
			for _, match := range exampleMatches {
				examplePath := match[1]
				_, readErr := examples.Read(examplePath)
				assert.NoError(t, readErr,
					"prompt %q references get_example path %q which does not exist",
					pc.name, examplePath)
			}
		})
	}
}
