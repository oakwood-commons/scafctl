// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// registerVersionTools registers version-related MCP tools.
func (s *Server) registerVersionTools() {
	getVersionTool := mcp.NewTool("get_version",
		mcp.WithDescription("Return the scafctl version, build time, and commit hash. Useful for environment context, bug reports, and compatibility checks."),
		mcp.WithTitleAnnotation("Get Version"),
		mcp.WithToolIcons(toolIcons["version"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithRawOutputSchema(outputSchemaVersion),
	)
	s.mcpServer.AddTool(getVersionTool, s.handleGetVersion)
}

// handleGetVersion returns the scafctl version information.
func (s *Server) handleGetVersion(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultJSON(map[string]any{
		"version":   settings.VersionInformation.BuildVersion,
		"commit":    settings.VersionInformation.Commit,
		"buildTime": settings.VersionInformation.BuildTime,
	})
}
