// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"context"
	"fmt"

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
