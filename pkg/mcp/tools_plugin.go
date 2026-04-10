// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
)

// registerPluginTools registers all plugin-related MCP tools.
func (s *Server) registerPluginTools() {
	listPluginsTool := mcp.NewTool("list_plugins",
		mcp.WithDescription(fmt.Sprintf(
			"List cached plugin binaries in the %s plugin cache. Returns name, version, platform, path, and size for each cached plugin. Plugins are cached after being fetched from catalogs or installed locally.",
			s.name,
		)),
		mcp.WithTitleAnnotation("List Cached Plugins"),
		mcp.WithToolIcons(toolIcons["plugin"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.mcpServer.AddTool(listPluginsTool, s.handleListPlugins)

	pluginCachePathTool := mcp.NewTool("get_plugin_cache_path",
		mcp.WithDescription(fmt.Sprintf(
			"Get the path to the %s plugin cache directory. Useful for debugging plugin discovery and installation issues.",
			s.name,
		)),
		mcp.WithTitleAnnotation("Get Plugin Cache Path"),
		mcp.WithToolIcons(toolIcons["plugin"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.mcpServer.AddTool(pluginCachePathTool, s.handleGetPluginCachePath)
}

// handleListPlugins lists cached plugin binaries.
func (s *Server) handleListPlugins(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cache := plugin.NewCache(paths.PluginCacheDir())
	plugins, err := cache.List()
	if err != nil {
		return newStructuredError(ErrCodeConfigError, fmt.Sprintf("failed to list plugins: %v", err),
			WithSuggestion("Check that the plugin cache directory exists and is readable"),
		), nil
	}

	if len(plugins) == 0 {
		return mcp.NewToolResultText("No cached plugins found. Use 'plugins install -f <solution>' to fetch plugins from catalogs, or copy plugin binaries to the plugin cache directory."), nil
	}

	return mcp.NewToolResultJSON(plugins)
}

// handleGetPluginCachePath returns the plugin cache directory path.
func (s *Server) handleGetPluginCachePath(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(paths.PluginCacheDir()), nil
}
