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
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	pkglint "github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	provdetail "github.com/oakwood-commons/scafctl/pkg/provider/detail"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
)

// registerSolutionTools registers all solution-related MCP tools.
func (s *Server) registerSolutionTools() {
	// list_solutions
	listSolutionsTool := mcp.NewTool("list_solutions",
		mcp.WithDescription("List available solutions from the local catalog. Returns solution names, versions, descriptions, and tags."),
		mcp.WithTitleAnnotation("List Solutions"),
		mcp.WithToolIcons(toolIcons["solution"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithRawOutputSchema(outputSchemaListSolutions),
		mcp.WithString("name",
			mcp.Description("Filter solutions by name (substring match). Omit to list all."),
		),
	)
	s.mcpServer.AddTool(listSolutionsTool, s.handleListSolutions)

	// inspect_solution
	inspectSolutionTool := mcp.NewTool("inspect_solution",
		mcp.WithDescription("Get full solution metadata including resolvers, actions, tags, links, maintainers, and catalog info. Accepts a local file path, catalog name, or URL."),
		mcp.WithTitleAnnotation("Inspect Solution"),
		mcp.WithToolIcons(toolIcons["solution"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithRawOutputSchema(outputSchemaInspectSolution),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to solution file, catalog name, or URL"),
		),
		mcp.WithString("cwd",
			mcp.Description("Working directory for path resolution. When set, relative paths resolve against this directory instead of the process CWD."),
		),
	)
	s.mcpServer.AddTool(inspectSolutionTool, s.handleInspectSolution)

	// lint_solution
	lintSolutionTool := mcp.NewTool("lint_solution",
		mcp.WithDescription("Validate a solution file and return structured lint findings. Checks for unused resolvers, invalid dependencies, missing providers, invalid expressions, and more."),
		mcp.WithTitleAnnotation("Lint Solution"),
		mcp.WithToolIcons(toolIcons["lint"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithRawOutputSchema(outputSchemaLintResult),
		mcp.WithString("file",
			mcp.Required(),
			mcp.Description("Path to the solution YAML file"),
		),
		mcp.WithString("severity",
			mcp.Description("Minimum severity to return: error, warning, info (default: info)"),
			mcp.Enum("error", "warning", "info"),
		),
		mcp.WithString("cwd",
			mcp.Description("Working directory for path resolution. When set, relative paths resolve against this directory instead of the process CWD."),
		),
	)
	s.mcpServer.AddTool(lintSolutionTool, s.handleLintSolution)

	// render_solution
	renderSolutionTool := mcp.NewTool("render_solution",
		mcp.WithDescription("Render a solution's action graph, resolver dependency graph, or action dependency graph. Executes resolvers and builds the graph as structured JSON. Use graph_type to select the visualization: 'action' (default) renders the executable action graph, 'resolver' shows resolver dependency phases, 'action-deps' shows action dependency visualization."),
		mcp.WithTitleAnnotation("Render Solution"),
		mcp.WithToolIcons(toolIcons["solution"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithRawOutputSchema(outputSchemaRenderSolution),
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
		mcp.WithString("output_dir",
			mcp.Description("Target directory for action output. When set, actions resolve relative paths against this directory instead of CWD. Resolvers always use CWD regardless of this setting."),
		),
		mcp.WithString("cwd",
			mcp.Description("Working directory for path resolution. When set, relative paths (including the solution path itself) resolve against this directory instead of the process CWD."),
		),
	)
	s.mcpServer.AddTool(renderSolutionTool, s.handleRenderSolution)

	// preview_resolvers
	previewResolversTool := mcp.NewTool("preview_resolvers",
		mcp.WithDescription("Execute a solution's resolver chain and return each resolver's resolved value. This is the 'does it actually work?' step between writing YAML and running the full solution. Shows the resolved value, type, and status for every resolver. Accepts optional input parameters for parameter-type resolvers. Use the 'resolver' parameter to debug a single resolver and see its resolve/transform/validate pipeline in detail."),
		mcp.WithTitleAnnotation("Preview Resolvers"),
		mcp.WithToolIcons(toolIcons["solution"]),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithRawOutputSchema(outputSchemaPreviewResolvers),
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
		mcp.WithString("output_dir",
			mcp.Description("Target directory for action output. Included for path preview purposes — resolvers always use CWD regardless of this setting."),
		),
		mcp.WithString("cwd",
			mcp.Description("Working directory for path resolution. When set, relative paths (including the solution path itself) resolve against this directory instead of the process CWD."),
		),
	)
	s.mcpServer.AddTool(previewResolversTool, s.handlePreviewResolvers)

	// run_solution_tests
	runSolutionTestsTool := mcp.NewTool("run_solution_tests",
		mcp.WithDescription("Execute functional tests defined in a solution YAML file (spec.testing.cases) or in a tests/ directory. Returns structured test results with pass/fail status, duration, and assertion details. Closes the write → lint → test loop entirely within the AI session."),
		mcp.WithTitleAnnotation("Run Solution Tests"),
		mcp.WithToolIcons(toolIcons["testing"]),
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
		mcp.WithString("output_dir",
			mcp.Description("Target directory for action output during test execution. When set, actions resolve relative paths against this directory instead of CWD."),
		),
		mcp.WithString("cwd",
			mcp.Description("Working directory for path resolution. When set, relative paths (including the solution path itself) resolve against this directory instead of the process CWD."),
		),
	)
	s.mcpServer.AddTool(runSolutionTestsTool, s.handleRunSolutionTests)

	// get_run_command
	getRunCommandTool := mcp.NewTool("get_run_command",
		mcp.WithDescription("Get the exact CLI command to run a solution. Analyzes the solution to determine whether to use 'run solution' or 'run resolver', identifies required parameter-type resolvers, and returns the complete command with correct flags. Eliminates guesswork about which command form to use."),
		mcp.WithTitleAnnotation("Get Run Command"),
		mcp.WithToolIcons(toolIcons["action"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to solution file, catalog name, or URL"),
		),
		mcp.WithString("on_conflict",
			mcp.Description("Include --on-conflict flag in the generated command. Controls file write behavior when targets exist. Valid values: error, overwrite, skip, skip-unchanged, append."),
			mcp.Enum("error", "overwrite", "skip", "skip-unchanged", "append"),
		),
		mcp.WithBoolean("backup",
			mcp.Description("Include --backup flag in the generated command. Creates .bak backups before overwriting existing files."),
		),
		mcp.WithString("cwd",
			mcp.Description("Working directory for path resolution. When set, relative paths resolve against this directory instead of the process CWD."),
		),
	)
	s.mcpServer.AddTool(getRunCommandTool, s.handleGetRunCommand)
}

// handleListSolutions lists available solutions from the local catalog.
func (s *Server) handleListSolutions(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := request.GetString("name", "")

	localCatalog, err := catalog.NewLocalCatalog(s.logger)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to initialize local catalog: %v", err),
			WithSuggestion("Check your catalog configuration with get_config"),
			WithRelatedTools("get_config", "catalog_list"),
		), nil
	}

	items, err := localCatalog.List(s.ctx, catalog.ArtifactKindSolution, name)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to list solutions: %v", err),
			WithSuggestion("Check your catalog configuration with get_config"),
			WithRelatedTools("get_config", "catalog_list"),
		), nil
	}

	if len(items) == 0 {
		// If catalog has no solutions, try discovering from workspace roots
		workspaceFiles := s.discoverSolutionFiles(s.ctx)
		if len(workspaceFiles) > 0 {
			type discoveredSolution struct {
				Path   string `json:"path"`
				Source string `json:"source"`
			}
			discovered := make([]discoveredSolution, 0, len(workspaceFiles))
			for _, f := range workspaceFiles {
				discovered = append(discovered, discoveredSolution{
					Path:   f,
					Source: "workspace",
				})
			}
			return mcp.NewToolResultJSON(map[string]any{
				"solutions": discovered,
				"message":   "No solutions in catalog, but found solution files in workspace roots",
				"source":    "workspace_roots",
			})
		}

		return mcp.NewToolResultJSON(map[string]any{
			"solutions": []any{},
			"message":   "No solutions found in local catalog or workspace",
		})
	}

	return mcp.NewToolResultJSON(items)
}

// handleInspectSolution gets full solution metadata.
func (s *Server) handleInspectSolution(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path"),
			WithSuggestion("Provide a solution file path, catalog name, or URL"),
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
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", err),
			WithField("path"),
			WithSuggestion("Check the file exists and contains valid YAML"),
			WithRelatedTools("lint_solution"),
		), nil
	}

	explanation := inspect.BuildSolutionExplanation(sol)

	result, err := mcp.NewToolResultJSON(explanation)
	if err != nil {
		return nil, err
	}
	result.Content = append(result.Content,
		mcp.NewResourceLink("solution://"+path, "Solution YAML", "Raw solution YAML content", "application/x-yaml"),
		mcp.NewResourceLink("solution://"+path+"/schema", "Input Schema", "Solution input schema", "application/schema+json"),
		mcp.NewResourceLink("solution://"+path+"/graph", "Dependency Graph", "Resolver dependency graph", "application/json"),
	)
	return result, nil
}

// handleLintSolution validates a solution file and returns structured findings.
func (s *Server) handleLintSolution(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	file, err := request.RequireString("file")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("file"),
			WithSuggestion("Provide the path to a solution YAML file"),
		), nil
	}
	severity := request.GetString("severity", "info")
	cwd := request.GetString("cwd", "")

	ctx, err := s.contextWithCwd(cwd)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("cwd"),
			WithSuggestion("Provide a valid existing directory path"),
		), nil
	}

	// Use prepare.Solution which handles catalog resolution and registry setup
	prepResult, err := prepare.Solution(ctx, file,
		prepare.WithRegistry(s.registry),
	)
	if err != nil {
		errMsg := err.Error()

		// Detect cycle errors and provide actionable __self guidance
		if strings.Contains(errMsg, "cycle detected") {
			return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", err),
				WithField("file"),
				WithSuggestion("Circular dependency detected in resolvers. "+
					"If a resolver's validate or transform phase references its own resolved value "+
					"(e.g., _.myResolver.statusCode), replace it with __self (e.g., __self.statusCode). "+
					"Using _.resolverName in validate/transform creates a self-referencing cycle. "+
					"__self is the correct way to reference the current resolver's value in transform and validate phases."),
				WithRelatedTools("render_solution", "lint_solution"),
			), nil
		}

		// Detect YAML unmarshal errors and provide structural guidance
		if strings.Contains(errMsg, "cannot unmarshal") {
			return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", err),
				WithField("file"),
				WithSuggestion("YAML structure error. Each phase (resolve, transform, validate) must use the 'with' key "+
					"containing an array of provider entries. Example:\n"+
					"  validate:\n    with:\n      - provider: validation\n        inputs:\n          expression: \"__self.size() > 0\"\n"+
					"Do NOT use a bare array directly under the phase key."),
				WithRelatedTools("explain_kind", "get_solution_schema"),
			), nil
		}

		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", err),
			WithField("file"),
			WithSuggestion("Check the file exists and contains valid solution YAML"),
			WithRelatedTools("inspect_solution"),
		), nil
	}
	if prepResult.Cleanup != nil {
		defer prepResult.Cleanup()
	}

	// Run linting
	result := pkglint.Solution(prepResult.Solution, file, prepResult.Registry)

	// Filter by severity
	if severity != "info" {
		result = pkglint.FilterBySeverity(result, severity)
	}

	return mcp.NewToolResultJSON(result)
}

// handleRenderSolution renders a solution's action graph, resolver graph, or action dependency graph.
func (s *Server) handleRenderSolution(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path"),
			WithSuggestion("Provide a solution file path, catalog name, or URL"),
		), nil
	}

	graphType := request.GetString("graph_type", "action")
	outputDir := request.GetString("output_dir", "")
	cwd := request.GetString("cwd", "")

	ctx, err := s.contextWithCwd(cwd)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("cwd"),
			WithSuggestion("Provide a valid existing directory path"),
		), nil
	}

	// Parse params from arguments
	var params map[string]any
	args := request.GetArguments()
	if p, ok := args["params"]; ok && p != nil {
		if pm, ok := p.(map[string]any); ok {
			params = pm
		} else {
			return newStructuredError(ErrCodeInvalidInput, "'params' must be an object (key-value pairs)",
				WithField("params"),
				WithSuggestion("Provide params as a JSON object, e.g. {\"key\": \"value\"}"),
			), nil
		}
	}

	// Load solution via prepare.Solution
	prepResult, err := prepare.Solution(ctx, path,
		prepare.WithRegistry(s.registry),
	)
	if err != nil {
		errMsg := err.Error()

		// Detect cycle errors and provide actionable __self guidance
		if strings.Contains(errMsg, "cycle detected") {
			return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", err),
				WithField("path"),
				WithSuggestion("Circular dependency detected in resolvers. "+
					"If a resolver's validate or transform phase references its own resolved value "+
					"(e.g., _.myResolver.statusCode), replace it with __self (e.g., __self.statusCode). "+
					"__self is the correct way to reference the current resolver's value in transform and validate phases."),
				WithRelatedTools("lint_solution", "inspect_solution"),
			), nil
		}

		// Detect YAML unmarshal errors and provide structural guidance
		if strings.Contains(errMsg, "cannot unmarshal") {
			return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", err),
				WithField("path"),
				WithSuggestion("YAML structure error. Each phase (resolve, transform, validate) must use the 'with' key "+
					"containing an array of provider entries. Do NOT use a bare array directly under the phase key."),
				WithRelatedTools("explain_kind", "get_solution_schema"),
			), nil
		}

		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", err),
			WithField("path"),
			WithSuggestion("Check the file exists and contains valid solution YAML"),
			WithRelatedTools("lint_solution", "inspect_solution"),
		), nil
	}
	if prepResult.Cleanup != nil {
		defer prepResult.Cleanup()
	}

	sol := prepResult.Solution
	reg := prepResult.Registry

	// Auto-fallback: if the user requested the default "action" graph but the
	// solution has no workflow, automatically switch to the resolver graph
	// instead of returning an error. This is the most common mistake when an
	// AI agent calls render_solution on a resolver-only solution.
	if graphType == "action" && !sol.Spec.HasWorkflow() && sol.Spec.HasResolvers() {
		graphType = "resolver"
	}

	switch graphType {
	case "resolver":
		return s.renderResolverGraph(sol, reg)
	case "action-deps":
		return s.renderActionDepsGraph(ctx, sol, params, outputDir)
	default:
		return s.renderActionGraph(ctx, sol, params, outputDir)
	}
}

// renderResolverGraph builds and returns the resolver dependency graph.
func (s *Server) renderResolverGraph(sol *solution.Solution, reg *provider.Registry) (*mcp.CallToolResult, error) {
	if !sol.Spec.HasResolvers() {
		return newStructuredError(ErrCodeValidationError, "solution does not define any resolvers",
			WithSuggestion("Add resolvers to the solution spec.resolvers section"),
			WithRelatedTools("get_solution_schema", "scaffold_solution"),
		), nil
	}

	resolvers := sol.Spec.ResolversToSlice()

	var lookup resolver.DescriptorLookup
	if reg != nil {
		lookup = reg.DescriptorLookup()
	}

	graph, err := resolver.BuildGraph(resolvers, lookup)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to build resolver graph: %v", err),
			WithSuggestion("Check resolver dependencies for cycles or missing references"),
			WithRelatedTools("lint_solution", "extract_resolver_refs"),
		), nil
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
func (s *Server) renderActionGraph(ctx context.Context, sol *solution.Solution, params map[string]any, outputDir string) (*mcp.CallToolResult, error) { //nolint:unparam // error is always nil per MCP pattern
	if !sol.Spec.HasWorkflow() {
		suggestion := "Add a spec.workflow section with actions to the solution"
		if sol.Spec.HasResolvers() {
			suggestion = "This solution only has resolvers and no workflow. Use graph_type='resolver' to render the resolver dependency graph, or add a spec.workflow section with actions"
		}
		return newStructuredError(ErrCodeValidationError, "solution does not define a workflow",
			WithSuggestion(suggestion),
			WithRelatedTools("get_solution_schema", "scaffold_solution"),
		), nil
	}

	// Execute resolvers to get data for action inputs
	resolverData, err := s.executeResolversForRender(ctx, sol, params)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("resolver execution failed: %v", err),
			WithSuggestion("Check resolver configuration with preview_resolvers"),
			WithRelatedTools("preview_resolvers", "lint_solution"),
		), nil
	}

	// Build the action graph
	graph, err := action.BuildGraph(ctx, sol.Spec.Workflow, resolverData, nil)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to build action graph: %v", err),
			WithSuggestion("Check action dependencies and provider configurations"),
			WithRelatedTools("lint_solution"),
		), nil
	}

	// Render as JSON
	renderOpts := &action.RenderOptions{
		Format:           "json",
		IncludeTimestamp: false,
		PrettyPrint:      true,
	}

	rendered, err := action.Render(graph, renderOpts)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to render action graph: %v", err),
			WithSuggestion("This is an internal error — please report it"),
		), nil
	}

	// Embed resolver data alongside the graph for a complete picture
	if len(resolverData) > 0 || outputDir != "" {
		type actionGraphWithResolvers struct {
			Graph        json.RawMessage `json:"graph"`
			ResolverData map[string]any  `json:"resolverData,omitempty"`
			OutputDir    string          `json:"outputDir,omitempty"`
		}
		return mcp.NewToolResultJSON(actionGraphWithResolvers{
			Graph:        json.RawMessage(rendered),
			ResolverData: resolverData,
			OutputDir:    outputDir,
		})
	}

	return mcp.NewToolResultText(string(rendered)), nil
}

// renderActionDepsGraph builds and returns the action dependency visualization.
func (s *Server) renderActionDepsGraph(ctx context.Context, sol *solution.Solution, params map[string]any, outputDir string) (*mcp.CallToolResult, error) {
	if !sol.Spec.HasWorkflow() {
		return newStructuredError(ErrCodeValidationError, "solution does not define a workflow",
			WithSuggestion("Add a spec.workflow section with actions to the solution"),
			WithRelatedTools("get_solution_schema", "scaffold_solution"),
		), nil
	}

	// Execute resolvers to get data for action inputs
	resolverData, err := s.executeResolversForRender(ctx, sol, params)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("resolver execution failed: %v", err),
			WithSuggestion("Check resolver configuration with preview_resolvers"),
			WithRelatedTools("preview_resolvers", "lint_solution"),
		), nil
	}

	// Build the action graph
	graph, err := action.BuildGraph(ctx, sol.Spec.Workflow, resolverData, nil)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to build action graph: %v", err),
			WithSuggestion("Check action dependencies and provider configurations"),
			WithRelatedTools("lint_solution"),
		), nil
	}

	// Build visualization
	viz := action.BuildVisualization(graph)

	// Build response with ASCII and Mermaid diagrams
	type vizResponse struct {
		*action.GraphVisualization
		Diagrams  map[string]string `json:"diagrams,omitempty"`
		OutputDir string            `json:"outputDir,omitempty"`
	}
	resp := vizResponse{GraphVisualization: viz, OutputDir: outputDir}

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
func (s *Server) executeResolversForRender(ctx context.Context, sol *solution.Solution, params map[string]any) (map[string]any, error) {
	return execute.ResolversForPreview(ctx, sol, params, s.registry)
}

// handlePreviewResolvers executes a solution's resolver chain and returns each resolver's value.
func (s *Server) handlePreviewResolvers(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path"),
			WithSuggestion("Provide the path to a solution file"),
		), nil
	}

	outputDir := request.GetString("output_dir", "")
	cwd := request.GetString("cwd", "")

	ctx, err := s.contextWithCwd(cwd)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("cwd"),
			WithSuggestion("Provide a valid existing directory path"),
		), nil
	}

	// Parse params
	var params map[string]any
	args := request.GetArguments()
	if p, ok := args["params"]; ok && p != nil {
		if pm, ok := p.(map[string]any); ok {
			params = pm
		} else {
			return newStructuredError(ErrCodeInvalidInput, "'params' must be an object (key-value pairs)",
				WithField("params"),
				WithSuggestion("Pass params as a JSON object, e.g. {\"key\": \"value\"}"),
			), nil
		}
	}

	// Load solution
	prepResult, err := prepare.Solution(ctx, path,
		prepare.WithRegistry(s.registry),
	)
	if err != nil {
		errMsg := err.Error()

		if strings.Contains(errMsg, "cycle detected") {
			return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", err),
				WithField("path"),
				WithSuggestion("Circular dependency detected. In validate/transform phases, use __self instead of _.resolverName to reference the resolver's own value."),
				WithRelatedTools("lint_solution"),
			), nil
		}

		if strings.Contains(errMsg, "cannot unmarshal") {
			return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", err),
				WithField("path"),
				WithSuggestion("YAML structure error. Each phase (resolve, transform, validate) must use the 'with' key containing an array. Do NOT use a bare array."),
				WithRelatedTools("explain_kind", "get_solution_schema"),
			), nil
		}

		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", err),
			WithField("path"),
			WithSuggestion("Check that the path points to a valid solution file"),
			WithRelatedTools("lint_solution"),
		), nil
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

	// Elicit missing parameter values: find parameter-type resolvers without provided values
	if params == nil {
		params = make(map[string]any)
	}
	var missingParamNames []string
	missingDescriptions := make(map[string]string)
	for name, rslvr := range sol.Spec.Resolvers {
		if rslvr == nil {
			return newStructuredError(ErrCodeLoadFailed,
				fmt.Sprintf("invalid resolver %q: resolver definition is null", name),
				WithField(fmt.Sprintf("resolvers.%s", name)),
				WithSuggestion("Resolver entries must be objects, not null. Define the resolver steps or remove this entry."),
				WithRelatedTools("lint_solution"),
			), nil
		}
		if rslvr.Resolve != nil && len(rslvr.Resolve.With) > 0 && rslvr.Resolve.With[0].Provider == "parameter" {
			if _, provided := params[name]; !provided {
				missingParamNames = append(missingParamNames, name)
				if rslvr.Description != "" {
					missingDescriptions[name] = rslvr.Description
				}
			}
		}
	}
	if len(missingParamNames) > 0 {
		sort.Strings(missingParamNames)
		for k, v := range s.elicitMissingParams(ctx, missingParamNames, missingDescriptions) {
			params[k] = v
		}
	}

	if reg == nil {
		reg, err = builtin.DefaultRegistry(ctx)
		if err != nil {
			return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to create provider registry: %v", err),
				WithSuggestion("Check provider configurations"),
				WithRelatedTools("list_providers"),
			), nil
		}
	}

	cfg := execute.ResolverExecutionConfigFromContext(ctx)
	// Check if we're debugging a single resolver
	resolverFilter := request.GetString("resolver", "")

	// Send progress notifications during execution
	progress := newProgressReporter(s, request)
	progress.setTotal(3)
	progress.report(ctx, 1, "Loading and validating solution")

	progress.report(ctx, 2, fmt.Sprintf("Executing %d resolvers", len(sol.Spec.Resolvers)))
	result, err := execute.Resolvers(ctx, sol, params, reg, cfg)
	if err != nil {
		return buildResolverExecutionError(err, sol), nil
	}

	progress.report(ctx, 3, "Building response")

	// Build structured response with per-resolver details
	type resolverPhaseInfo struct {
		Provider string `json:"provider,omitempty"`
		Inputs   any    `json:"inputs,omitempty"`
	}

	type resolverPreview struct {
		Value        any                 `json:"value"`
		Type         string              `json:"type,omitempty"`
		Description  string              `json:"description,omitempty"`
		Status       string              `json:"status"`
		Provider     string              `json:"provider,omitempty"`
		OutputSchema any                 `json:"outputSchema,omitempty"`
		DependsOn    []string            `json:"dependsOn,omitempty"`
		Resolve      []resolverPhaseInfo `json:"resolve,omitempty"`
		Transform    []resolverPhaseInfo `json:"transform,omitempty"`
		Validate     []resolverPhaseInfo `json:"validate,omitempty"`
		When         string              `json:"when,omitempty"`
		SourcePos    *sourcepos.Position `json:"sourcePos,omitempty"`
	}

	resolvers := make(map[string]resolverPreview, len(sol.Spec.Resolvers))
	for name, rslvr := range sol.Spec.Resolvers {
		// Note: nil resolvers are caught earlier in the param elicitation loop
		// with a structured error, so we can safely skip them here.
		if rslvr == nil {
			continue
		}
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

		// Enrich with source position if available
		if sm := sol.SourceMap(); sm != nil {
			if pos, ok := sm.Get("spec.resolvers." + name); ok {
				preview.SourcePos = &pos
			}
		}

		// Get the primary provider name
		if rslvr.Resolve != nil && len(rslvr.Resolve.With) > 0 {
			preview.Provider = rslvr.Resolve.With[0].Provider
		}

		// Determine output schema: last transform provider's transform schema,
		// or first resolve provider's from schema if no transforms exist.
		{
			var outputProviderName string
			var outputCap provider.Capability
			if rslvr.Transform != nil && len(rslvr.Transform.With) > 0 {
				last := rslvr.Transform.With[len(rslvr.Transform.With)-1]
				outputProviderName = last.Provider
				outputCap = provider.CapabilityTransform
			} else if rslvr.Resolve != nil && len(rslvr.Resolve.With) > 0 {
				outputProviderName = rslvr.Resolve.With[0].Provider
				outputCap = provider.CapabilityFrom
			}
			if outputProviderName != "" && reg != nil {
				if p, ok := reg.Get(outputProviderName); ok {
					desc := p.Descriptor()
					if schema, ok := desc.OutputSchemas[outputCap]; ok {
						preview.OutputSchema = provdetail.BuildSchemaOutput(schema)
					}
				}
			}
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
			return newStructuredError(ErrCodeNotFound, fmt.Sprintf("resolver %q not found. Available resolvers: %v", resolverFilter, availableNames),
				WithField("resolver"),
				WithSuggestion(fmt.Sprintf("Use one of the available resolver names: %v", availableNames)),
			), nil
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
	if outputDir != "" {
		response["outputDir"] = outputDir
	}

	result2, err := mcp.NewToolResultJSON(response)
	if err != nil {
		return nil, err
	}
	result2.Content = append(result2.Content,
		mcp.NewResourceLink("solution://"+path, "Solution YAML", "Raw solution YAML content", "application/x-yaml"),
		mcp.NewResourceLink("solution://"+path+"/graph", "Dependency Graph", "Resolver dependency graph", "application/json"),
	)
	return result2, nil
}

// handleRunSolutionTests executes functional tests for a solution.
func (s *Server) handleRunSolutionTests(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path"),
			WithSuggestion("Provide the path to a solution or directory containing solutions"),
		), nil
	}

	filter := request.GetString("filter", "")
	tag := request.GetString("tag", "")
	outputDir := request.GetString("output_dir", "")
	cwd := request.GetString("cwd", "")
	skipBuiltins := false
	if sb, ok := request.GetArguments()["skip_builtins"]; ok {
		if b, ok := sb.(bool); ok {
			skipBuiltins = b
		}
	}

	ctx, cwdErr := s.contextWithCwd(cwd)
	if cwdErr != nil {
		return newStructuredError(ErrCodeInvalidInput, cwdErr.Error(),
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

	// Verify the path exists
	if _, err := os.Stat(path); err != nil {
		return newStructuredError(ErrCodeNotFound, fmt.Sprintf("path not found: %v", err),
			WithField("path"),
			WithSuggestion("Check the path exists and is accessible"),
		), nil
	}

	// Discover solutions with tests
	solutions, err := soltesting.DiscoverSolutions(path)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("test discovery failed: %v", err),
			WithField("path"),
			WithSuggestion("Ensure the path contains valid solutions with test configurations"),
		), nil
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
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to resolve executable path: %v", err),
			WithSuggestion("Ensure the scafctl binary is accessible"),
		), nil
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

	// Send progress notifications during test execution
	progress := newProgressReporter(s, request)
	progress.setTotal(float64(len(solutions) + 2))
	progress.report(ctx, 1, fmt.Sprintf("Discovered %d solution(s) with tests", len(solutions)))

	// Execute tests
	start := time.Now()
	progress.report(ctx, 2, "Running tests...")
	results, err := runner.Run(ctx, solutions)
	elapsed := time.Since(start)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("test execution failed: %v", err),
			WithSuggestion("Review test configurations and solution definitions"),
			WithRelatedTools("lint_solution", "inspect_solution"),
		), nil
	}
	progress.report(ctx, float64(len(solutions)+2), fmt.Sprintf("Tests complete (%s)", elapsed.String()))

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

	response := map[string]any{
		"results": items,
		"summary": map[string]any{
			"total":    summary.Total,
			"passed":   summary.Passed,
			"failed":   summary.Failed,
			"errors":   summary.Errors,
			"skipped":  summary.Skipped,
			"duration": summary.ElapsedDuration().String(),
		},
	}
	if outputDir != "" {
		response["outputDir"] = outputDir
	}

	return mcp.NewToolResultJSON(response)
}

// handleGetRunCommand analyzes a solution and returns the exact CLI command to run it.
func (s *Server) handleGetRunCommand(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path"),
			WithSuggestion("Provide the path to a solution file"),
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
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", err),
			WithField("path"),
			WithSuggestion("Check that the path points to a valid solution file"),
			WithRelatedTools("lint_solution"),
		), nil
	}

	cmdInfo, err := inspect.BuildRunCommand(sol, path)
	if err != nil {
		return mcp.NewToolResultJSON(map[string]any{
			"error":       err.Error(),
			"explanation": "Nothing to run — the solution needs either resolvers or a workflow section",
		})
	}

	// Append conflict strategy flags to the command if requested.
	// These flags only apply to 'run solution', not 'run resolver'.
	onConflict := request.GetString("on_conflict", "")
	backup := request.GetBool("backup", false)
	if onConflict != "" {
		if !fileprovider.ConflictStrategy(onConflict).IsValid() {
			return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("invalid on_conflict value: %q", onConflict),
				WithField("on_conflict"),
				WithSuggestion("Use one of: error, overwrite, skip, skip-unchanged, append"),
			), nil
		}
		if cmdInfo.Subcommand == "scafctl run solution" {
			cmdInfo.Command += " --on-conflict " + onConflict
		} else {
			// The --on-conflict flag is not supported by 'run resolver'.
			// Return a structured error so the caller knows the flag was ignored.
			return newStructuredError(ErrCodeInvalidInput,
				fmt.Sprintf("--on-conflict is not supported by %q; it only applies to 'scafctl run solution' (solutions with a workflow)", cmdInfo.Subcommand),
				WithField("on_conflict"),
				WithSuggestion("Remove on_conflict, or use a solution that has a workflow with file provider actions."),
			), nil
		}
	}
	if backup {
		if cmdInfo.Subcommand == "scafctl run solution" {
			cmdInfo.Command += " --backup"
		} else {
			return newStructuredError(ErrCodeInvalidInput,
				fmt.Sprintf("--backup is not supported by %q; it only applies to 'scafctl run solution' (solutions with a workflow)", cmdInfo.Subcommand),
				WithField("backup"),
				WithSuggestion("Remove backup, or use a solution that has a workflow with file provider actions."),
			), nil
		}
	}

	// Build result with content annotations.
	// The command is primarily for the assistant, the explanation is for both.
	assistantPriority := 1.0
	userPriority := 0.8
	result, err := mcp.NewToolResultJSON(map[string]any{
		"command":      cmdInfo.Command,
		"subcommand":   cmdInfo.Subcommand,
		"explanation":  cmdInfo.Explanation,
		"parameters":   cmdInfo.Parameters,
		"hasWorkflow":  cmdInfo.HasWorkflow,
		"hasResolvers": cmdInfo.HasResolvers,
	})
	if err != nil {
		return nil, err
	}

	// Add annotated text content: command for assistant, explanation for user
	result.Content = append(result.Content,
		mcp.TextContent{
			Annotated: mcp.Annotated{
				Annotations: &mcp.Annotations{
					Audience: []mcp.Role{mcp.RoleAssistant},
					Priority: &assistantPriority,
				},
			},
			Type: "text",
			Text: fmt.Sprintf("Run command: %s", cmdInfo.Command),
		},
		mcp.TextContent{
			Annotated: mcp.Annotated{
				Annotations: &mcp.Annotations{
					Audience: []mcp.Role{mcp.RoleUser},
					Priority: &userPriority,
				},
			},
			Type: "text",
			Text: fmt.Sprintf("Explanation: %s", cmdInfo.Explanation),
		},
	)

	return result, nil
}

// buildResolverExecutionError converts resolver execution errors into rich structured
// error responses with typed diagnostics and actionable suggestions.
func buildResolverExecutionError(err error, sol *solution.Solution) *mcp.CallToolResult {
	diag := inspect.DiagnoseExecutionError(err, sol)

	opts := make([]ErrorOption, 0, 1+len(diag.Suggestions))
	opts = append(opts, WithRelatedTools("lint_solution", "inspect_solution"))
	for _, s := range diag.Suggestions {
		opts = append(opts, WithSuggestion(s))
	}

	return newStructuredError(ErrCodeExecFailed,
		fmt.Sprintf("resolver execution failed: %s", diag.Details),
		opts...,
	)
}

// isResolverDependency checks if candidateName is a direct or transitive dependency
// of the targetResolver within the solution's resolver graph.
func isResolverDependency(sol *solution.Solution, targetResolver, candidateName string) bool {
	return resolver.IsTransitiveDependency(sol.Spec.Resolvers, targetResolver, candidateName)
}
