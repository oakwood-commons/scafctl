// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
)

// registerTestingTools registers test scaffold and discovery MCP tools.
func (s *Server) registerTestingTools() {
	// generate_test_scaffold
	genTestTool := mcp.NewTool("generate_test_scaffold",
		mcp.WithDescription("Analyze a solution and generate a starter functional test scaffold. Examines resolvers (types, parameters, transforms) and workflow actions (providers, dependencies) to produce test cases with appropriate assertions. The generated YAML can be added to the solution's spec.testing section."),
		mcp.WithTitleAnnotation("Generate Test Scaffold"),
		mcp.WithToolIcons(toolIcons["testing"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to the solution file to generate tests for"),
		),
		mcp.WithString("cwd",
			mcp.Description("Working directory for path resolution. When set, relative paths resolve against this directory instead of the process CWD."),
		),
	)
	s.mcpServer.AddTool(genTestTool, s.handleGenerateTestScaffold)

	// list_tests
	listTestsTool := mcp.NewTool("list_tests",
		mcp.WithDescription("Discover and list functional tests defined in solutions without executing them. Returns test names, tags, commands, expected behavior, and skip status. Use this to understand what tests exist before calling run_solution_tests."),
		mcp.WithTitleAnnotation("List Tests"),
		mcp.WithToolIcons(toolIcons["testing"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("path",
			mcp.Description("Path to a solution file or directory containing solutions with tests"),
		),
		mcp.WithString("filter",
			mcp.Description("Filter test names by glob pattern (e.g., 'smoke-*')"),
		),
		mcp.WithString("tag",
			mcp.Description("Filter tests by tag (e.g., 'smoke', 'validation')"),
		),
		mcp.WithBoolean("include_builtins",
			mcp.Description("Include built-in tests (lint, parse). Default: false"),
		),
		mcp.WithString("cwd",
			mcp.Description("Working directory for path resolution. When set, relative paths resolve against this directory instead of the process CWD."),
		),
	)
	s.mcpServer.AddTool(listTestsTool, s.handleListTests)
}

// handleGenerateTestScaffold generates a starter test scaffold for a solution.
func (s *Server) handleGenerateTestScaffold(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path"),
			WithSuggestion("Provide the path to a solution file"),
			WithRelatedTools("inspect_solution"),
		), nil
	}
	cwd := request.GetString("cwd", "")

	ctx, err := s.contextWithCwd(cwd)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("cwd"),
			WithSuggestion("Provide a valid existing directory path"),
		), nil
	}

	sol, err := inspect.LoadSolution(ctx, path)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to load solution: %v", err),
			WithField("path"),
			WithSuggestion("Check the file exists and contains valid solution YAML"),
			WithRelatedTools("lint_solution"),
		), nil
	}

	input := &soltesting.ScaffoldInput{
		Resolvers:        sol.Spec.Resolvers,
		Workflow:         sol.Spec.Workflow,
		FileDependencies: discoverFileDeps(sol, path),
	}

	result := soltesting.Scaffold(input)

	yamlBytes, err := soltesting.ScaffoldToYAML(result)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to generate YAML: %v", err),
			WithSuggestion("This is an internal error — please report it"),
		), nil
	}

	// Analyze coverage
	resolversWithTests := []string{}
	resolversWithoutTests := []string{}
	actionsWithTests := []string{}
	actionsWithoutTests := []string{}

	if sol.Spec.HasResolvers() {
		for name := range sol.Spec.Resolvers {
			testName := "resolve-" + name
			if _, ok := result.Cases[testName]; ok {
				resolversWithTests = append(resolversWithTests, name)
			} else {
				resolversWithoutTests = append(resolversWithoutTests, name)
			}
		}
	}

	if sol.Spec.HasWorkflow() && sol.Spec.Workflow.Actions != nil {
		for name := range sol.Spec.Workflow.Actions {
			testName := "action-" + name
			if _, ok := result.Cases[testName]; ok {
				actionsWithTests = append(actionsWithTests, name)
			} else {
				actionsWithoutTests = append(actionsWithoutTests, name)
			}
		}
	}

	return mcp.NewToolResultJSON(map[string]any{
		"yaml":      string(yamlBytes),
		"testCount": len(result.Cases),
		"coverage": map[string]any{
			"resolversWithTests":    resolversWithTests,
			"resolversWithoutTests": resolversWithoutTests,
			"actionsWithTests":      actionsWithTests,
			"actionsWithoutTests":   actionsWithoutTests,
		},
		"nextSteps": []string{
			"Review and customize the test assertions",
			"Add edge case tests for error conditions",
			"Run run_solution_tests to verify tests pass",
		},
		"sandboxGuidance": map[string]any{
			"overview": "Each test case runs in an isolated sandbox directory. The sandbox copies the solution file and any files listed in the test case 'files' field.",
			"filesField": []string{
				"Use relative paths from the solution directory (e.g., 'templates/main.tf')",
				"Use glob patterns to match multiple files (e.g., 'templates/**')",
				"Use directory paths to copy entire directories (e.g., 'configs/')",
				"Files are deduplicated — overlapping globs and directories are safe",
			},
			"fileDependencies": func() string {
				if len(input.FileDependencies) > 0 {
					return fmt.Sprintf("Detected %d file dependencies from provider inputs. These have been auto-populated in each test case's 'files' field.", len(input.FileDependencies))
				}
				return "No file dependencies were detected. If your solution references external files (templates, configs, etc.), add them to each test case's 'files' field."
			}(),
			"commonPatterns": []string{
				"Use 'expr' in assertions to write CEL expressions that check resolved values",
				"Use 'contains' for partial string matching in resolver outputs",
				"Use 'eq' for exact value comparison",
				"Mock external providers (HTTP, exec) with static values for deterministic tests",
			},
		},
	})
}

// handleListTests discovers and lists tests without executing them.
func (s *Server) handleListTests(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", ".")
	filter := request.GetString("filter", "")
	tag := request.GetString("tag", "")
	cwd := request.GetString("cwd", "")

	ctx, err := s.contextWithCwd(cwd)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("cwd"),
			WithSuggestion("Provide a valid existing directory path"),
		), nil
	}

	// Resolve relative path against the working directory
	path, err = provider.AbsFromContext(ctx, path)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("failed to resolve path: %v", err),
			WithField("path"),
		), nil
	}

	solutions, err := soltesting.DiscoverSolutions(path)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to discover tests: %v", err),
			WithField("path"),
			WithSuggestion("Check the path exists and contains solution files with test sections"),
			WithRelatedTools("generate_test_scaffold"),
		), nil
	}

	// Apply filters
	filterOpts := soltesting.FilterOptions{}
	if filter != "" {
		filterOpts.NamePatterns = []string{filter}
	}
	if tag != "" {
		filterOpts.Tags = []string{tag}
	}
	if filterOpts.NamePatterns != nil || filterOpts.Tags != nil {
		solutions = soltesting.FilterTests(solutions, filterOpts)
	}

	// Build response
	type testInfo struct {
		Name           string   `json:"name"`
		Description    string   `json:"description,omitempty"`
		Command        []string `json:"command,omitempty"`
		Tags           []string `json:"tags,omitempty"`
		Skip           string   `json:"skip,omitempty"`
		SkipReason     string   `json:"skipReason,omitempty"`
		AssertionCount int      `json:"assertionCount"`
	}

	type solutionInfo struct {
		Solution string     `json:"solution"`
		File     string     `json:"file"`
		Tests    []testInfo `json:"tests"`
	}

	var solutionResults []solutionInfo
	totalTests := 0

	for _, st := range solutions {
		var tests []testInfo
		for _, name := range soltesting.SortedTestNames(st) {
			tc := st.Cases[name]
			ti := testInfo{
				Name:           name,
				Description:    tc.Description,
				Command:        tc.Command,
				Tags:           tc.Tags,
				Skip:           tc.Skip.String(),
				SkipReason:     tc.SkipReason,
				AssertionCount: len(tc.Assertions),
			}
			tests = append(tests, ti)
		}
		totalTests += len(tests)

		solutionResults = append(solutionResults, solutionInfo{
			Solution: st.SolutionName,
			File:     st.FilePath,
			Tests:    tests,
		})
	}

	return mcp.NewToolResultJSON(map[string]any{
		"solutions":      solutionResults,
		"totalTests":     totalTests,
		"totalSolutions": len(solutionResults),
	})
}

// discoverFileDeps uses bundler.DiscoverFiles to extract local file dependencies
// from a loaded solution. Returns nil on error (best-effort).
func discoverFileDeps(sol *solution.Solution, solutionPath string) []string {
	if sol == nil {
		return nil
	}

	bundleRoot := filepath.Dir(solutionPath)
	result, err := bundler.DiscoverFiles(sol, bundleRoot)
	if err != nil || result == nil {
		return nil
	}

	var deps []string
	for _, f := range result.LocalFiles {
		deps = append(deps, f.RelPath)
	}
	return deps
}
