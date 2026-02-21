// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
)

// registerActionTools registers action-related MCP tools.
func (s *Server) registerActionTools() {
	// preview_action
	previewActionTool := mcp.NewTool("preview_action",
		mcp.WithDescription("Preview what each action in a solution's workflow would do WITHOUT executing it. Resolves all inputs (expanding CEL expressions, Go templates, and resolver references) and shows the final computed values each action would receive. This is an 'action dry-run' that shows the full picture before execution."),
		mcp.WithTitleAnnotation("Preview Actions"),
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
		mcp.WithString("action",
			mcp.Description("Preview a specific action by name. If omitted, previews all actions."),
		),
	)
	s.mcpServer.AddTool(previewActionTool, s.handlePreviewAction)
}

// handlePreviewAction shows what each action would do with fully resolved inputs.
func (s *Server) handlePreviewAction(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	actionFilter := request.GetString("action", "")

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

	if !sol.Spec.HasWorkflow() {
		return mcp.NewToolResultError("solution does not define a workflow — use preview_resolvers instead"), nil
	}

	// Execute resolvers to get data for action inputs
	resolverData, err := s.executeResolversForRender(sol, params)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolver execution failed: %v", err)), nil
	}

	// Build the action graph (this materializes inputs)
	graph, err := action.BuildGraph(s.ctx, sol.Spec.Workflow, resolverData, nil)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to build action graph: %v", err)), nil
	}

	// Build structured preview response
	type forEachPreview struct {
		ExpandedFrom string `json:"expandedFrom,omitempty"`
		Item         any    `json:"item,omitempty"`
		Index        int    `json:"index,omitempty"`
	}
	type retryPreview struct {
		MaxAttempts int    `json:"maxAttempts,omitempty"`
		Backoff     string `json:"backoff,omitempty"`
	}
	type actionPreview struct {
		Name               string            `json:"name"`
		Description        string            `json:"description,omitempty"`
		Provider           string            `json:"provider"`
		MaterializedInputs map[string]any    `json:"materializedInputs,omitempty"`
		DeferredInputs     map[string]string `json:"deferredInputs,omitempty"`
		Dependencies       []string          `json:"dependencies,omitempty"`
		When               string            `json:"when,omitempty"`
		Section            string            `json:"section"`
		Phase              int               `json:"phase"`
		ForEach            *forEachPreview   `json:"forEach,omitempty"`
		Retry              *retryPreview     `json:"retry,omitempty"`
		Timeout            string            `json:"timeout,omitempty"`
		OnError            string            `json:"onError,omitempty"`
		Exclusive          []string          `json:"exclusive,omitempty"`
	}

	previews := make([]actionPreview, 0)

	// Build a phase map for execution ordering
	phaseMap := make(map[string]int)
	for i, phase := range graph.ExecutionOrder {
		for _, name := range phase {
			phaseMap[name] = i + 1
		}
	}
	for i, phase := range graph.FinallyOrder {
		for _, name := range phase {
			phaseMap[name] = i + 1
		}
	}

	// Collect and sort action names for deterministic output
	names := make([]string, 0, len(graph.Actions))
	for name := range graph.Actions {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		ea := graph.Actions[name]

		// Apply filter — match against both expanded name and original action name
		if actionFilter != "" && name != actionFilter && ea.Name != actionFilter {
			continue
		}

		preview := actionPreview{
			Name:               name,
			Provider:           ea.Provider,
			MaterializedInputs: ea.MaterializedInputs,
			Dependencies:       ea.Dependencies,
			Section:            ea.Section,
			Phase:              phaseMap[name],
		}

		// Use fields from the embedded Action
		preview.Description = ea.Description
		if ea.When != nil && ea.When.Expr != nil {
			preview.When = string(*ea.When.Expr)
		}
		if ea.Timeout != nil {
			preview.Timeout = ea.Timeout.String()
		}
		if ea.OnError != "" {
			preview.OnError = string(ea.OnError)
		}
		if ea.Retry != nil {
			preview.Retry = &retryPreview{
				MaxAttempts: ea.Retry.MaxAttempts,
				Backoff:     string(ea.Retry.Backoff),
			}
		}

		// Map deferred inputs to their expression strings
		if len(ea.DeferredInputs) > 0 {
			preview.DeferredInputs = make(map[string]string, len(ea.DeferredInputs))
			for k, v := range ea.DeferredInputs {
				expr := v.OriginalExpr
				if expr == "" {
					expr = v.OriginalTmpl
				}
				if expr == "" && v.Deferred {
					expr = "(deferred)"
				}
				preview.DeferredInputs[k] = expr
			}
		}

		// forEach info
		if ea.ForEachMetadata != nil {
			preview.ForEach = &forEachPreview{
				ExpandedFrom: ea.ForEachMetadata.ExpandedFrom,
				Item:         ea.ForEachMetadata.Item,
				Index:        ea.ForEachMetadata.Index,
			}
		}

		// Exclusive
		if len(ea.ExpandedExclusive) > 0 {
			preview.Exclusive = ea.ExpandedExclusive
		}

		previews = append(previews, preview)
	}

	// Verify the filtered action exists
	if actionFilter != "" && len(previews) == 0 {
		availableNames := make([]string, 0, len(graph.Actions))
		for name := range graph.Actions {
			availableNames = append(availableNames, name)
		}
		sort.Strings(availableNames)
		return mcp.NewToolResultError(fmt.Sprintf("action %q not found. Available actions: %v", actionFilter, availableNames)), nil
	}

	response := map[string]any{
		"actions":      previews,
		"totalActions": len(graph.Actions),
		"totalPhases":  graph.TotalPhases(),
		"resolverData": resolverData,
	}
	if actionFilter != "" {
		response["filter"] = actionFilter
	}

	return mcp.NewToolResultJSON(response)
}
