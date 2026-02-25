// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/paths"
)

// registerConfigTools registers configuration-related MCP tools.
func (s *Server) registerConfigTools() {
	getConfigTool := mcp.NewTool("get_config",
		mcp.WithDescription("Return the current scafctl configuration. Shows catalogs, settings, logging, HTTP client, CEL, resolver, action, auth, and build configuration. Use the optional 'section' parameter to retrieve only a specific section. Sensitive fields (client secrets, tokens) are redacted."),
		mcp.WithTitleAnnotation("Get Configuration"),
		mcp.WithToolIcons(toolIcons["config"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithRawOutputSchema(outputSchemaGetConfig),
		mcp.WithString("section",
			mcp.Description("Optional section to retrieve: 'catalogs', 'settings', 'logging', 'httpClient', 'cel', 'resolver', 'action', 'auth', 'build'. Omit to return the full configuration."),
		),
	)
	s.mcpServer.AddTool(getConfigTool, s.handleGetConfig)

	getConfigPathsTool := mcp.NewTool("get_config_paths",
		mcp.WithDescription("Return all XDG-compliant filesystem paths used by scafctl. Shows config, data, cache, state, catalog, secrets, plugins, runtime, and build-cache directories. Useful for debugging path issues, finding where configuration or cached data is stored, and understanding the filesystem layout."),
		mcp.WithTitleAnnotation("Get Config Paths"),
		mcp.WithToolIcons(toolIcons["config"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithRawOutputSchema(outputSchemaGetConfigPaths),
	)
	s.mcpServer.AddTool(getConfigPathsTool, s.handleGetConfigPaths)
}

// handleGetConfig returns the current configuration (with sensitive fields redacted).
func (s *Server) handleGetConfig(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg := s.config
	if cfg == nil {
		cfg = config.FromContext(s.ctx)
	}
	if cfg == nil {
		var err error
		cfg, err = config.Global()
		if err != nil {
			return newStructuredError(ErrCodeConfigError, fmt.Sprintf("no configuration available: %v", err),
				WithSuggestion("Run 'scafctl config init' to create a default configuration"),
				WithRelatedTools("get_config_paths"),
			), nil
		}
	}

	sanitized := config.SanitizeConfig(cfg)
	section := request.GetString("section", "")

	if section == "" {
		return mcp.NewToolResultJSON(sanitized)
	}

	// Return just the requested section
	validSections := map[string]any{
		"catalogs":   sanitized.Catalogs,
		"settings":   sanitized.Settings,
		"logging":    sanitized.Logging,
		"httpClient": sanitized.HTTPClient,
		"cel":        sanitized.CEL,
		"resolver":   sanitized.Resolver,
		"action":     sanitized.Action,
		"auth":       sanitized.Auth,
		"build":      sanitized.Build,
	}

	sectionData, ok := validSections[section]
	if !ok {
		validNames := make([]string, 0, len(validSections))
		for k := range validSections {
			validNames = append(validNames, k)
		}
		return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("unknown section %q. Valid sections: %v", section, validNames),
			WithField("section"),
			WithSuggestion("Omit the section parameter to get the full config"),
		), nil
	}

	return mcp.NewToolResultJSON(map[string]any{
		"section": section,
		"data":    sectionData,
	})
}

// handleGetConfigPaths returns all XDG-compliant filesystem paths used by scafctl.
func (s *Server) handleGetConfigPaths(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	type pathEntry struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		Description string `json:"description"`
		XDGVariable string `json:"xdgVariable"`
	}

	entries := []pathEntry{
		{Name: "config", Path: paths.ConfigDir(), Description: "Configuration files (config.yaml)", XDGVariable: "XDG_CONFIG_HOME"},
		{Name: "data", Path: paths.DataDir(), Description: "Application data", XDGVariable: "XDG_DATA_HOME"},
		{Name: "cache", Path: paths.CacheDir(), Description: "Cache root", XDGVariable: "XDG_CACHE_HOME"},
		{Name: "state", Path: paths.StateDir(), Description: "Logs, history, session state", XDGVariable: "XDG_STATE_HOME"},
		{Name: "catalog", Path: paths.CatalogDir(), Description: "Local artifact catalogs", XDGVariable: "XDG_DATA_HOME"},
		{Name: "secrets", Path: paths.SecretsDirPath(), Description: "Credential storage", XDGVariable: "XDG_DATA_HOME"},
		{Name: "httpCache", Path: paths.HTTPCacheDir(), Description: "HTTP response cache", XDGVariable: "XDG_CACHE_HOME"},
		{Name: "buildCache", Path: paths.BuildCacheDir(), Description: "Build cache for solutions", XDGVariable: "XDG_CACHE_HOME"},
		{Name: "plugins", Path: paths.PluginCacheDir(), Description: "Plugin cache", XDGVariable: "XDG_CACHE_HOME"},
		{Name: "runtime", Path: paths.RuntimeDir(), Description: "Runtime sockets and pipes", XDGVariable: "XDG_RUNTIME_DIR"},
	}

	return mcp.NewToolResultJSON(map[string]any{
		"paths": entries,
		"count": len(entries),
	})
}
