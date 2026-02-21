// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/explain"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/lint"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/run"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
)

// registerSolutionTools registers all solution-related MCP tools.
func (s *Server) registerSolutionTools() {
	// list_solutions
	listSolutionsTool := mcp.NewTool("list_solutions",
		mcp.WithDescription("List available solutions from the local catalog. Returns solution names, versions, descriptions, and tags."),
		mcp.WithTitleAnnotation("List Solutions"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("name",
			mcp.Description("Filter solutions by name (substring match). Omit to list all."),
		),
	)
	s.mcpServer.AddTool(listSolutionsTool, s.handleListSolutions)

	// inspect_solution
	inspectSolutionTool := mcp.NewTool("inspect_solution",
		mcp.WithDescription("Get full solution metadata including resolvers, actions, tags, links, maintainers, and catalog info. Accepts a local file path, catalog name, or URL."),
		mcp.WithTitleAnnotation("Inspect Solution"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to solution file, catalog name, or URL"),
		),
	)
	s.mcpServer.AddTool(inspectSolutionTool, s.handleInspectSolution)

	// lint_solution
	lintSolutionTool := mcp.NewTool("lint_solution",
		mcp.WithDescription("Validate a solution file and return structured lint findings. Checks for unused resolvers, invalid dependencies, missing providers, invalid expressions, and more."),
		mcp.WithTitleAnnotation("Lint Solution"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("file",
			mcp.Required(),
			mcp.Description("Path to the solution YAML file"),
		),
		mcp.WithString("severity",
			mcp.Description("Minimum severity to return: error, warning, info (default: info)"),
			mcp.Enum("error", "warning", "info"),
		),
	)
	s.mcpServer.AddTool(lintSolutionTool, s.handleLintSolution)

	// render_solution
	renderSolutionTool := mcp.NewTool("render_solution",
		mcp.WithDescription("Render a solution's action graph, resolver dependency graph, or action dependency graph. Executes resolvers and builds the graph as structured JSON. Use graph_type to select the visualization: 'action' (default) renders the executable action graph, 'resolver' shows resolver dependency phases, 'action-deps' shows action dependency visualization."),
		mcp.WithTitleAnnotation("Render Solution"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to solution file, catalog name, or URL"),
		),
		mcp.WithObject("params",
			mcp.Description("Resolver input parameters as key-value pairs (e.g., {\"env\": \"prod\", \"region\": \"us-east-1\"})"),
		),
		mcp.WithString("graph_type",
			mcp.Description("Graph type to render: action (default, executable action graph), resolver (resolver dependency graph), action-deps (action dependency visualization)"),
			mcp.Enum("action", "resolver", "action-deps"),
		),
	)
	s.mcpServer.AddTool(renderSolutionTool, s.handleRenderSolution)

	// preview_resolvers
	previewResolversTool := mcp.NewTool("preview_resolvers",
		mcp.WithDescription("Execute a solution's resolver chain and return each resolver's resolved value. This is the 'does it actually work?' step between writing YAML and running the full solution. Shows the resolved value, type, and status for every resolver. Accepts optional input parameters for parameter-type resolvers. Use the 'resolver' parameter to debug a single resolver and see its resolve/transform/validate pipeline in detail."),
		mcp.WithTitleAnnotation("Preview Resolvers"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to solution file, catalog name, or URL"),
		),
		mcp.WithObject("params",
			mcp.Description("Input parameters as key-value pairs for parameter-type resolvers (e.g., {\"env\": \"prod\", \"region\": \"us-east-1\"})"),
		),
		mcp.WithString("resolver",
			mcp.Description("Debug a single resolver by name. Returns detailed pipeline info (resolve, transform, validate phases) for just this resolver and its dependencies."),
		),
	)
	s.mcpServer.AddTool(previewResolversTool, s.handlePreviewResolvers)

	// run_solution_tests
	runSolutionTestsTool := mcp.NewTool("run_solution_tests",
		mcp.WithDescription("Execute functional tests defined in a solution YAML file (spec.testing.cases) or in a tests/ directory. Returns structured test results with pass/fail status, duration, and assertion details. Closes the write → lint → test loop entirely within the AI session."),
		mcp.WithTitleAnnotation("Run Solution Tests"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to solution YAML file or directory containing solution files with tests"),
		),
		mcp.WithString("filter",
			mcp.Description("Glob pattern to filter test names (e.g., 'render-*', 'validation-*')"),
		),
		mcp.WithString("tag",
			mcp.Description("Run only tests with this tag (e.g., 'smoke', 'validation')"),
		),
		mcp.WithBoolean("skip_builtins",
			mcp.Description("Skip built-in tests (lint, parse). Default: false"),
		),
		mcp.WithBoolean("verbose",
			mcp.Description("Include assertion details for ALL tests (not just failures). Useful for verifying why tests pass. Default: false"),
		),
	)
	s.mcpServer.AddTool(runSolutionTestsTool, s.handleRunSolutionTests)

	// get_run_command
	getRunCommandTool := mcp.NewTool("get_run_command",
		mcp.WithDescription("Get the exact CLI command to run a solution. Analyzes the solution to determine whether to use 'run solution' or 'run resolver', identifies required parameter-type resolvers, and returns the complete command with correct flags. Eliminates guesswork about which command form to use."),
		mcp.WithTitleAnnotation("Get Run Command"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to solution file, catalog name, or URL"),
		),
	)
	s.mcpServer.AddTool(getRunCommandTool, s.handleGetRunCommand)
}

// handleListSolutions lists available solutions from the local catalog.
func (s *Server) handleListSolutions(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := request.GetString("name", "")

	localCatalog, err := catalog.NewLocalCatalog(s.logger)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to initialize local catalog: %v", err)), nil
	}

	items, err := localCatalog.List(s.ctx, catalog.ArtifactKindSolution, name)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list solutions: %v", err)), nil
	}

	if len(items) == 0 {
		return mcp.NewToolResultJSON(map[string]any{
			"solutions": []any{},
			"message":   "No solutions found in local catalog",
		})
	}

	return mcp.NewToolResultJSON(items)
}

// handleInspectSolution gets full solution metadata.
func (s *Server) handleInspectSolution(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	sol, err := explain.LoadSolution(s.ctx, path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading solution: %v", err)), nil
	}

	explanation := explain.BuildSolutionExplanation(sol)

	return mcp.NewToolResultJSON(explanation)
}

// handleLintSolution validates a solution file and returns structured findings.
func (s *Server) handleLintSolution(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	file, err := request.RequireString("file")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	severity := request.GetString("severity", "info")

	// Use prepare.Solution which handles catalog resolution and registry setup
	prepResult, err := prepare.Solution(s.ctx, file,
		prepare.WithRegistry(s.registry),
	)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading solution: %v", err)), nil
	}
	if prepResult.Cleanup != nil {
		defer prepResult.Cleanup()
	}

	// Run linting
	result := lint.Solution(prepResult.Solution, file, prepResult.Registry)

	// Filter by severity
	if severity != "info" {
		result = lint.FilterBySeverity(result, severity)
	}

	return mcp.NewToolResultJSON(result)
}

// handleRenderSolution renders a solution's action graph, resolver graph, or action dependency graph.
func (s *Server) handleRenderSolution(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	graphType := request.GetString("graph_type", "action")

	// Parse params from arguments
	var params map[string]any
	args := request.GetArguments()
	if p, ok := args["params"]; ok && p != nil {
		if pm, ok := p.(map[string]any); ok {
			params = pm
		} else {
			return mcp.NewToolResultError("'params' must be an object (key-value pairs)"), nil
		}
	}

	// Load solution via prepare.Solution
	prepResult, err := prepare.Solution(s.ctx, path,
		prepare.WithRegistry(s.registry),
	)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading solution: %v", err)), nil
	}
	if prepResult.Cleanup != nil {
		defer prepResult.Cleanup()
	}

	sol := prepResult.Solution
	reg := prepResult.Registry

	switch graphType {
	case "resolver":
		return s.renderResolverGraph(sol, reg)
	case "action-deps":
		return s.renderActionDepsGraph(sol, params)
	default:
		return s.renderActionGraph(sol, params)
	}
}

// renderResolverGraph builds and returns the resolver dependency graph.
func (s *Server) renderResolverGraph(sol *solution.Solution, reg *provider.Registry) (*mcp.CallToolResult, error) {
	if !sol.Spec.HasResolvers() {
		return mcp.NewToolResultError("solution does not define any resolvers"), nil
	}

	resolvers := sol.Spec.ResolversToSlice()

	var lookup resolver.DescriptorLookup
	if reg != nil {
		lookup = reg.DescriptorLookup()
	}

	graph, err := resolver.BuildGraph(resolvers, lookup)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to build resolver graph: %v", err)), nil
	}

	// Also include ASCII diagram in the response for readability
	var asciiBuf bytes.Buffer
	if err := graph.RenderASCII(&asciiBuf); err == nil {
		graph.Diagrams = &resolver.GraphDiagrams{
			ASCII: asciiBuf.String(),
		}
		var mermaidBuf bytes.Buffer
		if err := graph.RenderMermaid(&mermaidBuf); err == nil {
			graph.Diagrams.Mermaid = mermaidBuf.String()
		}
	}

	return mcp.NewToolResultJSON(graph)
}

// renderActionGraph executes resolvers, builds, and renders the action graph.
func (s *Server) renderActionGraph(sol *solution.Solution, params map[string]any) (*mcp.CallToolResult, error) { //nolint:unparam // error is always nil per MCP pattern
	if !sol.Spec.HasWorkflow() {
		return mcp.NewToolResultError("solution does not define a workflow"), nil
	}

	// Execute resolvers to get data for action inputs
	resolverData, err := s.executeResolversForRender(sol, params)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolver execution failed: %v", err)), nil
	}

	// Build the action graph
	graph, err := action.BuildGraph(s.ctx, sol.Spec.Workflow, resolverData, nil)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to build action graph: %v", err)), nil
	}

	// Render as JSON
	renderOpts := &action.RenderOptions{
		Format:           "json",
		IncludeTimestamp: false,
		PrettyPrint:      true,
	}

	rendered, err := action.Render(graph, renderOpts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to render action graph: %v", err)), nil
	}

	// Embed resolver data alongside the graph for a complete picture
	if len(resolverData) > 0 {
		type actionGraphWithResolvers struct {
			Graph        json.RawMessage `json:"graph"`
			ResolverData map[string]any  `json:"resolverData"`
		}
		return mcp.NewToolResultJSON(actionGraphWithResolvers{
			Graph:        json.RawMessage(rendered),
			ResolverData: resolverData,
		})
	}

	return mcp.NewToolResultText(string(rendered)), nil
}

// renderActionDepsGraph builds and returns the action dependency visualization.
func (s *Server) renderActionDepsGraph(sol *solution.Solution, params map[string]any) (*mcp.CallToolResult, error) {
	if !sol.Spec.HasWorkflow() {
		return mcp.NewToolResultError("solution does not define a workflow"), nil
	}

	// Execute resolvers to get data for action inputs
	resolverData, err := s.executeResolversForRender(sol, params)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolver execution failed: %v", err)), nil
	}

	// Build the action graph
	graph, err := action.BuildGraph(s.ctx, sol.Spec.Workflow, resolverData, nil)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to build action graph: %v", err)), nil
	}

	// Build visualization
	viz := action.BuildVisualization(graph)

	// Build response with ASCII and Mermaid diagrams
	type vizResponse struct {
		*action.GraphVisualization
		Diagrams map[string]string `json:"diagrams,omitempty"`
	}
	resp := vizResponse{GraphVisualization: viz}

	var asciiBuf bytes.Buffer
	if err := viz.RenderASCII(&asciiBuf); err == nil {
		resp.Diagrams = map[string]string{
			"ascii": asciiBuf.String(),
		}
		var mermaidBuf bytes.Buffer
		if err := viz.RenderMermaid(&mermaidBuf); err == nil {
			resp.Diagrams["mermaid"] = mermaidBuf.String()
		}
	}

	return mcp.NewToolResultJSON(resp)
}

// executeResolversForRender runs resolver execution for render operations.
func (s *Server) executeResolversForRender(sol *solution.Solution, params map[string]any) (map[string]any, error) {
	resolverData := make(map[string]any)

	if !sol.Spec.HasResolvers() {
		return resolverData, nil
	}

	reg := s.registry
	if reg == nil {
		// Build a default registry if none was provided
		var err error
		reg, err = builtin.DefaultRegistry(s.ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider registry: %w", err)
		}
	}

	cfg := run.ResolverExecutionConfigFromContext(s.ctx)
	result, err := run.ExecuteResolvers(s.ctx, sol, params, reg, cfg)
	if err != nil {
		return nil, err
	}

	return result.Data, nil
}

// handlePreviewResolvers executes a solution's resolver chain and returns each resolver's value.
func (s *Server) handlePreviewResolvers(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Parse params
	var params map[string]any
	args := request.GetArguments()
	if p, ok := args["params"]; ok && p != nil {
		if pm, ok := p.(map[string]any); ok {
			params = pm
		} else {
			return mcp.NewToolResultError("'params' must be an object (key-value pairs)"), nil
		}
	}

	// Load solution
	prepResult, err := prepare.Solution(s.ctx, path,
		prepare.WithRegistry(s.registry),
	)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading solution: %v", err)), nil
	}
	if prepResult.Cleanup != nil {
		defer prepResult.Cleanup()
	}

	sol := prepResult.Solution
	reg := prepResult.Registry

	if !sol.Spec.HasResolvers() {
		return mcp.NewToolResultJSON(map[string]any{
			"resolvers": map[string]any{},
			"message":   "Solution does not define any resolvers",
		})
	}

	if reg == nil {
		reg, err = builtin.DefaultRegistry(s.ctx)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create provider registry: %v", err)), nil
		}
	}

	cfg := run.ResolverExecutionConfigFromContext(s.ctx)
	// Check if we're debugging a single resolver
	resolverFilter := request.GetString("resolver", "")

	result, err := run.ExecuteResolvers(s.ctx, sol, params, reg, cfg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolver execution failed: %v", err)), nil
	}

	// Build structured response with per-resolver details
	type resolverPhaseInfo struct {
		Provider string `json:"provider,omitempty"`
		Inputs   any    `json:"inputs,omitempty"`
	}

	type resolverPreview struct {
		Value       any                 `json:"value"`
		Type        string              `json:"type,omitempty"`
		Description string              `json:"description,omitempty"`
		Status      string              `json:"status"`
		Provider    string              `json:"provider,omitempty"`
		DependsOn   []string            `json:"dependsOn,omitempty"`
		Resolve     []resolverPhaseInfo `json:"resolve,omitempty"`
		Transform   []resolverPhaseInfo `json:"transform,omitempty"`
		Validate    []resolverPhaseInfo `json:"validate,omitempty"`
		When        string              `json:"when,omitempty"`
	}

	resolvers := make(map[string]resolverPreview, len(sol.Spec.Resolvers))
	for name, rslvr := range sol.Spec.Resolvers {
		// If filtering to a single resolver, check if this one or its deps match
		if resolverFilter != "" && name != resolverFilter {
			// Include dependencies of the filtered resolver
			if !isResolverDependency(sol, resolverFilter, name) {
				continue
			}
		}

		preview := resolverPreview{
			Description: rslvr.Description,
			Type:        string(rslvr.Type),
		}

		// Get the primary provider name
		if rslvr.Resolve != nil && len(rslvr.Resolve.With) > 0 {
			preview.Provider = rslvr.Resolve.With[0].Provider
		}

		// Add pipeline details for single-resolver debugging
		if resolverFilter != "" {
			preview.DependsOn = rslvr.DependsOn
			if rslvr.When != nil && rslvr.When.Expr != nil {
				preview.When = string(*rslvr.When.Expr)
			}
			if rslvr.Resolve != nil {
				for _, step := range rslvr.Resolve.With {
					preview.Resolve = append(preview.Resolve, resolverPhaseInfo{
						Provider: step.Provider,
						Inputs:   step.Inputs,
					})
				}
			}
			if rslvr.Transform != nil {
				for _, step := range rslvr.Transform.With {
					preview.Transform = append(preview.Transform, resolverPhaseInfo{
						Provider: step.Provider,
						Inputs:   step.Inputs,
					})
				}
			}
			if rslvr.Validate != nil {
				for _, step := range rslvr.Validate.With {
					preview.Validate = append(preview.Validate, resolverPhaseInfo{
						Provider: step.Provider,
						Inputs:   step.Inputs,
					})
				}
			}
		}

		if val, ok := result.Data[name]; ok {
			preview.Value = val
			preview.Status = "resolved"
		} else {
			preview.Status = "unresolved"
		}

		resolvers[name] = preview
	}

	// Verify the filtered resolver exists
	if resolverFilter != "" {
		if _, ok := sol.Spec.Resolvers[resolverFilter]; !ok {
			availableNames := make([]string, 0, len(sol.Spec.Resolvers))
			for name := range sol.Spec.Resolvers {
				availableNames = append(availableNames, name)
			}
			sort.Strings(availableNames)
			return mcp.NewToolResultError(fmt.Sprintf("resolver %q not found. Available resolvers: %v", resolverFilter, availableNames)), nil
		}
	}

	response := map[string]any{
		"resolvers": resolvers,
		"total":     len(sol.Spec.Resolvers),
		"resolved":  len(result.Data),
	}
	if resolverFilter != "" {
		response["filter"] = resolverFilter
		response["total"] = len(resolvers)
	}

	return mcp.NewToolResultJSON(response)
}

// handleRunSolutionTests executes functional tests for a solution.
func (s *Server) handleRunSolutionTests(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	filter := request.GetString("filter", "")
	tag := request.GetString("tag", "")
	skipBuiltins := false
	if sb, ok := request.GetArguments()["skip_builtins"]; ok {
		if b, ok := sb.(bool); ok {
			skipBuiltins = b
		}
	}

	// Verify the path exists
	if _, err := os.Stat(path); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("path not found: %v", err)), nil
	}

	// Discover solutions with tests
	solutions, err := soltesting.DiscoverSolutions(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("test discovery failed: %v", err)), nil
	}

	if len(solutions) == 0 {
		return mcp.NewToolResultJSON(map[string]any{
			"results": []any{},
			"summary": map[string]any{
				"total":   0,
				"message": "No solutions with tests found",
			},
		})
	}

	// Apply skip builtins
	if skipBuiltins {
		for i := range solutions {
			if solutions[i].Config == nil {
				solutions[i].Config = &soltesting.TestConfig{}
			}
			solutions[i].Config.SkipBuiltins = soltesting.SkipBuiltinsValue{All: true}
		}
	}

	// Resolve binary path for subprocess execution
	binaryPath, err := os.Executable()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to resolve executable path: %v", err)), nil
	}

	// Build runner
	runner := &soltesting.Runner{
		BinaryPath:  binaryPath,
		Concurrency: 1, // Sequential for MCP to keep output clean
		FailFast:    false,
		TestTimeout: 30 * time.Second,
		IOStreams:   &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
	}

	// Apply filters
	if filter != "" {
		runner.Filter.NamePatterns = []string{filter}
	}
	if tag != "" {
		runner.Filter.Tags = []string{tag}
	}

	// Execute tests
	start := time.Now()
	results, err := runner.Run(s.ctx, solutions)
	elapsed := time.Since(start)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("test execution failed: %v", err)), nil
	}

	// Build structured response
	summary := soltesting.Summarize(results)
	summary.WallDuration = elapsed

	type testResultItem struct {
		Solution   string                       `json:"solution"`
		Test       string                       `json:"test"`
		Status     string                       `json:"status"`
		Duration   string                       `json:"duration"`
		Message    string                       `json:"message,omitempty"`
		Assertions []soltesting.AssertionResult `json:"assertions,omitempty"`
	}

	items := make([]testResultItem, 0, len(results))
	for _, r := range results {
		item := testResultItem{
			Solution: r.Solution,
			Test:     r.Test,
			Status:   string(r.Status),
			Duration: r.Duration.String(),
			Message:  r.Message,
		}
		// Include assertions for failed tests always, or all tests if verbose
		verbose := request.GetBool("verbose", false)
		if len(r.Assertions) > 0 && (verbose || r.Status == soltesting.StatusFail) {
			item.Assertions = r.Assertions
		}
		items = append(items, item)
	}

	return mcp.NewToolResultJSON(map[string]any{
		"results": items,
		"summary": map[string]any{
			"total":    summary.Total,
			"passed":   summary.Passed,
			"failed":   summary.Failed,
			"errors":   summary.Errors,
			"skipped":  summary.Skipped,
			"duration": summary.ElapsedDuration().String(),
		},
	})
}

// handleGetRunCommand analyzes a solution and returns the exact CLI command to run it.
func (s *Server) handleGetRunCommand(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	sol, err := explain.LoadSolution(s.ctx, path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading solution: %v", err)), nil
	}

	hasResolvers := sol.Spec.HasResolvers()
	hasWorkflow := sol.Spec.HasWorkflow()

	// Determine command variant
	var command string
	var explanation string
	switch {
	case hasWorkflow:
		command = "scafctl run solution"
		explanation = "Solution has a workflow with actions — use 'run solution'"
	case hasResolvers:
		command = "scafctl run resolver"
		explanation = "Solution has resolvers but no workflow — use 'run resolver'"
	default:
		return mcp.NewToolResultJSON(map[string]any{
			"error":       "Solution has neither resolvers nor a workflow",
			"explanation": "Nothing to run — the solution needs either resolvers or a workflow section",
		})
	}

	// Find parameter-type resolvers (these require -r flags)
	type paramInfo struct {
		Name        string `json:"name"`
		Type        string `json:"type,omitempty"`
		Description string `json:"description,omitempty"`
		Example     any    `json:"example,omitempty"`
	}

	var parameters []paramInfo
	if hasResolvers {
		// Collect resolver names sorted for deterministic output
		names := make([]string, 0, len(sol.Spec.Resolvers))
		for name := range sol.Spec.Resolvers {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			rslvr := sol.Spec.Resolvers[name]
			if rslvr.Resolve == nil || len(rslvr.Resolve.With) == 0 {
				continue
			}
			// Check if the primary provider is "parameter"
			if rslvr.Resolve.With[0].Provider == "parameter" {
				p := paramInfo{
					Name:        name,
					Type:        string(rslvr.Type),
					Description: rslvr.Description,
					Example:     rslvr.Example,
				}
				parameters = append(parameters, p)
			}
		}
	}

	// Build the full command string
	fullCommand := fmt.Sprintf("%s -f %s", command, path)
	for _, p := range parameters {
		exampleVal := "<value>"
		if p.Example != nil {
			exampleVal = fmt.Sprintf("%v", p.Example)
		}
		fullCommand += fmt.Sprintf(" -r %s=%s", p.Name, exampleVal)
	}

	return mcp.NewToolResultJSON(map[string]any{
		"command":      fullCommand,
		"subcommand":   command,
		"explanation":  explanation,
		"parameters":   parameters,
		"hasWorkflow":  hasWorkflow,
		"hasResolvers": hasResolvers,
	})
}

// isResolverDependency checks if candidateName is a direct or transitive dependency
// of the targetResolver within the solution's resolver graph.
func isResolverDependency(sol *solution.Solution, targetResolver, candidateName string) bool {
	target, ok := sol.Spec.Resolvers[targetResolver]
	if !ok {
		return false
	}

	// Check direct dependencies
	for _, dep := range target.DependsOn {
		if dep == candidateName {
			return true
		}
		// Recurse into transitive dependencies
		if isResolverDependency(sol, dep, candidateName) {
			return true
		}
	}
	return false
}
