// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
)

// registerRunTools registers solution execution MCP tools.
func (s *Server) registerRunTools() {
	runSolutionTool := mcp.NewTool("run_solution",
		mcp.WithDescription(fmt.Sprintf(
			"Execute a solution end-to-end: resolves all inputs, then runs the action workflow. "+
				"This is the MCP equivalent of '%s run solution <path>'. "+
				"Solutions are loaded from file paths, catalog names, or URLs. "+
				"Use 'catalog_search' or 'catalog_list_solutions' to discover solutions, "+
				"then 'inspect_solution' to see required parameters before running. "+
				"Use 'dry_run_solution' first to preview what will happen without making changes.", s.name)),
		mcp.WithTitleAnnotation("Run Solution"),
		mcp.WithToolIcons(toolIcons["action"]),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to solution file, catalog name (e.g. 'hello-world'), or URL"),
		),
		mcp.WithObject("params",
			mcp.Description("Input parameters as key-value pairs for parameter-type resolvers. Use 'inspect_solution' to see which parameters are available."),
		),
		mcp.WithString("output_dir",
			mcp.Description("Target directory for file output. Actions resolve relative paths against this directory instead of CWD. Created automatically if it does not exist."),
		),
		mcp.WithString("on_conflict",
			mcp.Description("Conflict strategy for file write actions when a target file already exists. Valid values: error (fail), overwrite (replace), skip (keep existing), skip-unchanged (overwrite only if content differs), append (add to end)."),
			mcp.Enum("error", "overwrite", "skip", "skip-unchanged", "append"),
		),
		mcp.WithBoolean("backup",
			mcp.Description("Create .bak backup files before overwriting existing files."),
		),
		mcp.WithString("cwd",
			mcp.Description("Working directory for path resolution. Relative paths resolve against this directory instead of the process CWD."),
		),
		mcp.WithBoolean("show_execution",
			mcp.Description("Include execution metadata (timing, phases, providers) in the response."),
		),
	)
	s.mcpServer.AddTool(runSolutionTool, s.handleRunSolution)
}

// handleRunSolution executes a solution through the domain layer.
func (s *Server) handleRunSolution(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path"),
			WithSuggestion("Provide a solution file path, catalog name, or URL. Use 'catalog_search' to find solutions."),
			WithRelatedTools("catalog_search", "catalog_list_solutions"),
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

	// Parse conflict strategy.
	onConflict := request.GetString("on_conflict", "")
	if onConflict != "" {
		if !fileprovider.ConflictStrategy(onConflict).IsValid() {
			return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("invalid on_conflict value %q", onConflict),
				WithField("on_conflict"),
				WithSuggestion("Use one of: error, overwrite, skip, skip-unchanged, append"),
			), nil
		}
		ctx = provider.WithConflictStrategy(ctx, onConflict)
	}
	if backup := request.GetBool("backup", false); backup {
		ctx = provider.WithBackup(ctx, true)
	}

	// Parse params.
	var params map[string]any
	args := request.GetArguments()
	if p, ok := args["params"]; ok && p != nil {
		if pm, ok := p.(map[string]any); ok {
			params = pm
		} else {
			return newStructuredError(ErrCodeInvalidInput, "'params' must be an object (key-value pairs)",
				WithField("params"),
				WithSuggestion("Provide params as a JSON object, e.g. {\"name\": \"my-project\"}"),
			), nil
		}
	}

	// Capture caller's CWD before prepare.Solution may os.Chdir to a bundle temp dir.
	originalCwd, err := provider.GetWorkingDirectory(ctx)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to get working directory: %v", err),
			WithSuggestion("This is an internal error -- please report it"),
		), nil
	}

	// Load solution.
	prepResult, err := prepare.Solution(ctx, path,
		prepare.WithRegistry(s.registry),
	)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", err),
			WithField("path"),
			WithSuggestion("Check the file exists and contains valid solution YAML. Use 'lint_solution' to diagnose issues."),
			WithRelatedTools("lint_solution", "inspect_solution"),
		), nil
	}
	if prepResult.Cleanup != nil {
		defer prepResult.Cleanup()
	}

	// If bundle extraction changed the process CWD, pin the resolver context
	// to the bundle dir so file reads resolve within the extracted bundle.
	if bundleCwd, cwdErr := os.Getwd(); cwdErr == nil && bundleCwd != originalCwd {
		ctx = provider.WithWorkingDirectory(ctx, bundleCwd)
	}

	// Action context resolves paths against the caller's CWD.
	actionCtx := provider.WithWorkingDirectory(ctx, originalCwd)

	sol := prepResult.Solution
	reg := prepResult.Registry
	if reg == nil {
		reg, err = builtin.DefaultRegistry(s.ctx)
		if err != nil {
			return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to create provider registry: %v", err),
				WithSuggestion("This is an internal error -- please report it"),
			), nil
		}
	}

	// Validate the solution.
	validation := execute.ValidateSolution(ctx, sol, reg)
	if !validation.Valid {
		return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("solution validation failed: %v", validation.Errors),
			WithField("path"),
			WithSuggestion("Fix the validation errors and try again. Use 'lint_solution' for detailed diagnostics."),
			WithRelatedTools("lint_solution"),
		), nil
	}

	// Progress reporting.
	progress := newProgressReporter(s, request)
	totalSteps := float64(2)
	if sol.Spec.HasWorkflow() {
		totalSteps = 3
	}
	progress.setTotal(totalSteps)

	// Step 1: Execute resolvers.
	var resolverData map[string]any
	if sol.Spec.HasResolvers() {
		progress.report(ctx, 1, "Executing resolvers")

		cfg := execute.ResolverExecutionConfigFromContext(ctx)
		result, resolverErr := execute.Resolvers(ctx, sol, params, reg, cfg)
		if resolverErr != nil {
			return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("resolver execution failed: %v", resolverErr),
				WithSuggestion("Check parameter values and resolver configuration. Use 'preview_resolvers' to debug resolver behavior."),
				WithRelatedTools("preview_resolvers", "inspect_solution"),
			), nil
		}
		resolverData = result.Data
	} else {
		resolverData = make(map[string]any)
	}

	// Step 2: Execute actions (if workflow exists).
	if !sol.Spec.HasWorkflow() {
		progress.report(ctx, 2, "Complete -- resolver-only solution")

		return mcp.NewToolResultJSON(map[string]any{
			"status":       "success",
			"type":         "resolver-only",
			"resolverData": resolverData,
			"solution":     sol.Metadata.Name,
		})
	}

	progress.report(ctx, 2, "Executing actions")

	outputDir := request.GetString("output_dir", "")
	actionCfg := execute.ActionExecutionConfigFromContext(actionCtx)
	if outputDir != "" {
		actionCfg.OutputDir = outputDir
	}
	actionCfg.Cwd = originalCwd

	actionResult, err := execute.Actions(actionCtx, sol, resolverData, reg, actionCfg)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("action execution failed: %v", err),
			WithSuggestion("Use 'dry_run_solution' to preview what a solution does before running it."),
			WithRelatedTools("dry_run_solution", "inspect_solution"),
		), nil
	}

	progress.report(ctx, 3, "Execution complete")

	return mcp.NewToolResultJSON(buildRunResult(sol, resolverData, actionResult.Result, request.GetBool("show_execution", false)))
}

// buildRunResult constructs the structured response from a solution execution.
func buildRunResult(sol *solution.Solution, resolverData map[string]any, result *action.ExecutionResult, showExecution bool) map[string]any {
	response := map[string]any{
		"status":   string(result.FinalStatus),
		"solution": sol.Metadata.Name,
	}

	// Summarize actions.
	actionSummary := make([]map[string]any, 0, len(result.Actions))
	names := make([]string, 0, len(result.Actions))
	for name := range result.Actions {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		ar := result.Actions[name]
		entry := map[string]any{
			"name":   name,
			"status": string(ar.Status),
		}
		if ar.Error != "" {
			entry["error"] = ar.Error
		}
		if ar.Results != nil {
			entry["results"] = ar.Results
		}
		if showExecution {
			entry["duration"] = ar.Duration().String()
		}
		actionSummary = append(actionSummary, entry)
	}
	response["actions"] = actionSummary

	if len(result.FailedActions) > 0 {
		response["failedActions"] = result.FailedActions
	}
	if len(result.SkippedActions) > 0 {
		response["skippedActions"] = result.SkippedActions
	}

	if showExecution {
		response["duration"] = result.Duration().String()
		response["resolverData"] = resolverData
	}

	return response
}
