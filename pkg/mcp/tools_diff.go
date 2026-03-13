// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/soldiff"
)

// registerDiffTools registers diff/comparison MCP tools.
func (s *Server) registerDiffTools() {
	// diff_solution
	diffSolutionTool := mcp.NewTool("diff_solution",
		mcp.WithDescription("Compare two solution files and show structural differences. Identifies added, removed, and changed resolvers, actions, metadata, and test cases. Useful for reviewing changes before committing or understanding what was modified."),
		mcp.WithTitleAnnotation("Diff Solution"),
		mcp.WithToolIcons(toolIcons["diff"]),
		mcp.WithDeferLoading(true),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("path_a",
			mcp.Required(),
			mcp.Description("Path to the first solution file (e.g., the original version)"),
		),
		mcp.WithString("path_b",
			mcp.Required(),
			mcp.Description("Path to the second solution file (e.g., the modified version)"),
		),
		mcp.WithString("cwd",
			mcp.Description("Working directory for path resolution. When set, relative paths resolve against this directory instead of the process CWD."),
		),
	)
	s.mcpServer.AddTool(diffSolutionTool, s.handleDiffSolution)
}

// handleDiffSolution compares two solutions structurally.
func (s *Server) handleDiffSolution(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pathA, err := request.RequireString("path_a")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path_a"),
			WithSuggestion("Provide two solution file paths to compare"),
		), nil
	}
	pathB, err := request.RequireString("path_b")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path_b"),
			WithSuggestion("Provide two solution file paths to compare"),
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

	result, err := soldiff.CompareFiles(ctx, pathA, pathB)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("diff failed: %v", err),
			WithSuggestion("Check that both file paths exist and contain valid solution YAML"),
			WithRelatedTools("inspect_solution", "lint_solution"),
		), nil
	}

	return mcp.NewToolResultJSON(map[string]any{
		"pathA":   result.PathA,
		"pathB":   result.PathB,
		"changes": result.Changes,
		"summary": result.Summary,
	})
}
