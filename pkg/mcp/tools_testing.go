// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
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

	sol, err := inspect.LoadSolution(s.ctx, path)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to load solution: %v", err),
			WithField("path"),
			WithSuggestion("Check the file exists and contains valid solution YAML"),
			WithRelatedTools("lint_solution"),
		), nil
	}

	input := &soltesting.ScaffoldInput{
		Resolvers: sol.Spec.Resolvers,
		Workflow:  sol.Spec.Workflow,
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
	})
}

// handleListTests discovers and lists tests without executing them.
func (s *Server) handleListTests(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", ".")
	filter := request.GetString("filter", "")
	tag := request.GetString("tag", "")

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
		Skip           bool     `json:"skip,omitempty"`
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
				Skip:           tc.Skip,
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
