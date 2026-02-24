// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/examples"
)

// registerExampleTools registers example-related MCP tools.
func (s *Server) registerExampleTools() {
	// list_examples — list available example files
	listExamplesTool := mcp.NewTool("list_examples",
		mcp.WithDescription("List available scafctl example files. Examples demonstrate best practices for solutions, resolvers, actions, providers, and more. Filter by category (solutions, resolvers, actions, providers, exec, config, mcp, snapshots, catalog) or get all."),
		mcp.WithTitleAnnotation("List Examples"),
		mcp.WithToolIcons(toolIcons["example"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("category",
			mcp.Description("Filter by category: solutions, resolvers, actions, providers, exec, config, mcp, snapshots, catalog. Omit to list all."),
			mcp.Enum("solutions", "resolvers", "actions", "providers", "exec", "config", "mcp", "snapshots", "catalog"),
		),
	)
	s.mcpServer.AddTool(listExamplesTool, s.handleListExamples)

	// get_example — read an example file's contents
	getExampleTool := mcp.NewTool("get_example",
		mcp.WithDescription("Read the contents of a scafctl example file. Use list_examples first to find available examples, then use the path returned there."),
		mcp.WithTitleAnnotation("Get Example"),
		mcp.WithToolIcons(toolIcons["example"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path of the example file (as returned by list_examples, e.g., 'solutions/email-notifier/solution.yaml')"),
		),
	)
	s.mcpServer.AddTool(getExampleTool, s.handleGetExample)
}

// handleListExamples lists available example files.
func (s *Server) handleListExamples(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	category := request.GetString("category", "")

	items, err := examples.Scan(category)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to scan examples: %v", err),
			WithSuggestion("Check that the examples are properly embedded in the binary"),
		), nil
	}

	if len(items) == 0 {
		msg := "No examples found"
		if category != "" {
			msg += fmt.Sprintf(" in category %q", category)
		}
		return mcp.NewToolResultJSON(map[string]any{
			"examples": []any{},
			"message":  msg,
		})
	}

	return mcp.NewToolResultJSON(map[string]any{
		"examples": items,
		"count":    len(items),
	})
}

// handleGetExample reads and returns the contents of an example file.
func (s *Server) handleGetExample(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path"),
			WithSuggestion("Use list_examples to see available example paths"),
			WithRelatedTools("list_examples"),
		), nil
	}

	content, err := examples.Read(path)
	if err != nil {
		return newStructuredError(ErrCodeNotFound, fmt.Sprintf("failed to read example: %v", err),
			WithField("path"),
			WithSuggestion("Use list_examples to see available example paths"),
			WithRelatedTools("list_examples"),
		), nil
	}

	return mcp.NewToolResultText(content), nil
}
