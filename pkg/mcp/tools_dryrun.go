// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/run"
	"github.com/oakwood-commons/scafctl/pkg/dryrun"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
)

// registerDryRunTools registers dry-run related MCP tools.
func (s *Server) registerDryRunTools() {
	dryRunTool := mcp.NewTool("dry_run_solution",
		mcp.WithDescription("Perform a full dry-run of a solution: resolves all resolvers with providers in dry-run/mock mode (no side effects), then builds the action graph to show what each action WOULD do. Returns resolver outputs (mock values), action execution plan with materialized inputs, provider mock behaviors, and any warnings. Use this to understand the complete execution plan before actually running a solution."),
		mcp.WithTitleAnnotation("Dry-Run Solution"),
		mcp.WithToolIcons(toolIcons["dryrun"]),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithRawOutputSchema(outputSchemaDryRun),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to solution file, catalog name, or URL"),
		),
		mcp.WithObject("params",
			mcp.Description("Input parameters as key-value pairs for parameter-type resolvers"),
		),
	)
	s.mcpServer.AddTool(dryRunTool, s.handleDryRunSolution)
}

// handleDryRunSolution performs a full dry-run of a solution using the shared dryrun package.
func (s *Server) handleDryRunSolution(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path"),
			WithSuggestion("Provide the path to a solution file, catalog name, or URL"),
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
				WithSuggestion("Provide params as a JSON object, e.g. {\"key\": \"value\"}"),
			), nil
		}
	}

	// Load solution
	prepResult, err := prepare.Solution(s.ctx, path,
		prepare.WithRegistry(s.registry),
	)
	if err != nil {
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
	if reg == nil {
		reg, err = builtin.DefaultRegistry(s.ctx)
		if err != nil {
			return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to create provider registry: %v", err),
				WithSuggestion("This is an internal error — please report it"),
			), nil
		}
	}

	// Send progress notifications during dry-run
	progress := newProgressReporter(s, request)
	progress.setTotal(3)
	progress.report(s.ctx, 1, "Loaded solution, executing resolvers in dry-run mode")

	// Execute resolvers in dry-run mode
	var resolverData map[string]any
	if sol.Spec.HasResolvers() {
		cfg := run.ResolverExecutionConfigFromContext(s.ctx)
		cfg.DryRun = true

		result, err := run.ExecuteResolvers(s.ctx, sol, params, reg, cfg)
		if err != nil {
			resolverData = make(map[string]any)
		} else {
			resolverData = result.Data
		}
	}

	progress.report(s.ctx, 2, "Building action graph and generating report")

	// Generate structured report
	report, err := dryrun.Generate(s.ctx, sol, dryrun.Options{
		Params:       params,
		Registry:     reg,
		ResolverData: resolverData,
	})
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("dry-run failed: %v", err),
			WithSuggestion("Check the solution configuration and try lint_solution first"),
			WithRelatedTools("lint_solution", "inspect_solution"),
		), nil
	}

	progress.report(s.ctx, 3, "Dry-run complete")

	return mcp.NewToolResultJSON(report)
}
