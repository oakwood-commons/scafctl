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

	sanitized := sanitizeConfig(cfg)
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

// sanitizedConfig mirrors config.Config but with sensitive fields redacted.
type sanitizedConfig struct {
	Version    int                     `json:"version,omitempty"`
	Catalogs   []sanitizedCatalog      `json:"catalogs"`
	Settings   config.Settings         `json:"settings"`
	Logging    config.LoggingConfig    `json:"logging"`
	HTTPClient config.HTTPClientConfig `json:"httpClient"`
	CEL        config.CELConfig        `json:"cel"`
	Resolver   config.ResolverConfig   `json:"resolver"`
	Action     config.ActionConfig     `json:"action"`
	Auth       sanitizedAuth           `json:"auth"`
	Build      config.BuildConfig      `json:"build"`
}

// sanitizedCatalog redacts auth tokens from catalog config.
type sanitizedCatalog struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Path     string            `json:"path,omitempty"`
	URL      string            `json:"url,omitempty"`
	Auth     *sanitizedCatAuth `json:"auth,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type sanitizedCatAuth struct {
	Type        string `json:"type"`
	TokenEnvVar string `json:"tokenEnvVar,omitempty"`
}

// sanitizedAuth redacts client secrets and tokens from auth config.
type sanitizedAuth struct {
	Entra  *sanitizedEntraAuth  `json:"entra,omitempty"`
	GitHub *sanitizedGitHubAuth `json:"github,omitempty"`
	GCP    *sanitizedGCPAuth    `json:"gcp,omitempty"`
}

type sanitizedEntraAuth struct {
	ClientID      string   `json:"clientId,omitempty"`
	TenantID      string   `json:"tenantId,omitempty"`
	DefaultScopes []string `json:"defaultScopes,omitempty"`
}

type sanitizedGitHubAuth struct {
	ClientID      string   `json:"clientId,omitempty"`
	Hostname      string   `json:"hostname,omitempty"`
	DefaultScopes []string `json:"defaultScopes,omitempty"`
}

type sanitizedGCPAuth struct {
	ClientID                  string   `json:"clientId,omitempty"`
	GCPClientCredential       string   `json:"gcpClientCredential,omitempty"` // will be redacted
	DefaultScopes             []string `json:"defaultScopes,omitempty"`
	ImpersonateServiceAccount string   `json:"impersonateServiceAccount,omitempty"`
	Project                   string   `json:"project,omitempty"`
}

const redactedValue = "***REDACTED***"

// sanitizeConfig creates a sanitized copy of the config with sensitive values redacted.
func sanitizeConfig(cfg *config.Config) sanitizedConfig {
	s := sanitizedConfig{
		Version:    cfg.Version,
		Settings:   cfg.Settings,
		Logging:    cfg.Logging,
		HTTPClient: cfg.HTTPClient,
		CEL:        cfg.CEL,
		Resolver:   cfg.Resolver,
		Action:     cfg.Action,
		Build:      cfg.Build,
	}

	// Sanitize catalogs
	s.Catalogs = make([]sanitizedCatalog, 0, len(cfg.Catalogs))
	for _, cat := range cfg.Catalogs {
		sc := sanitizedCatalog{
			Name:     cat.Name,
			Type:     cat.Type,
			Path:     cat.Path,
			URL:      cat.URL,
			Metadata: cat.Metadata,
		}
		if cat.Auth != nil {
			sc.Auth = &sanitizedCatAuth{
				Type:        cat.Auth.Type,
				TokenEnvVar: cat.Auth.TokenEnvVar,
			}
		}
		s.Catalogs = append(s.Catalogs, sc)
	}

	// Sanitize auth — redact secrets
	if cfg.Auth.Entra != nil {
		s.Auth.Entra = &sanitizedEntraAuth{
			ClientID:      cfg.Auth.Entra.ClientID,
			TenantID:      cfg.Auth.Entra.TenantID,
			DefaultScopes: cfg.Auth.Entra.DefaultScopes,
		}
	}
	if cfg.Auth.GitHub != nil {
		s.Auth.GitHub = &sanitizedGitHubAuth{
			ClientID:      cfg.Auth.GitHub.ClientID,
			Hostname:      cfg.Auth.GitHub.Hostname,
			DefaultScopes: cfg.Auth.GitHub.DefaultScopes,
		}
	}
	if cfg.Auth.GCP != nil {
		gcp := &sanitizedGCPAuth{
			ClientID:                  cfg.Auth.GCP.ClientID,
			DefaultScopes:             cfg.Auth.GCP.DefaultScopes,
			ImpersonateServiceAccount: cfg.Auth.GCP.ImpersonateServiceAccount,
			Project:                   cfg.Auth.GCP.Project,
		}
		if cfg.Auth.GCP.ClientSecret != "" {
			gcp.GCPClientCredential = redactedValue
		}
		s.Auth.GCP = gcp
	}

	return s
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
