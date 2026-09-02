// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import "strings"

// RedactedValue is the placeholder inserted for sensitive fields.
const RedactedValue = "***REDACTED***"

// sensitiveLeafNames enumerates JSON/YAML leaf field names whose values must
// never be printed verbatim. Match is case-insensitive on the leaf key, so
// `clientSecret` catches both `auth.entra.clientSecret` and
// `auth.customOAuth2[0].clientSecret`. Keep the list explicit rather than
// regex-matching "*Secret*" -- fields like `PrivateKeySecretName` are just
// keyring entry names and are safe to display.
var sensitiveConfigLeafNames = map[string]bool{ //nolint:gochecknoglobals // shared redaction policy
	"clientsecret":       true,
	"privatekey":         true,
	"privatekeypath":     true,
	"federatedtoken":     true,
	"federatedtokenfile": true,
}

// RedactConfigMap walks a map representation of a *Config (typically produced
// by kvx.StructToMap) and replaces known sensitive leaves with RedactedValue.
// It preserves structure so callers still see every section (including
// auth.handlers, kube, plugins, etc.) while never exposing raw secrets.
//
// Sensitive leaves are matched by case-insensitive field name (see
// sensitiveConfigLeafNames) plus a special rule for mcp.servers.<name>.url,
// which may embed bearer tokens in its query string.
func RedactConfigMap(m map[string]any) {
	if m == nil {
		return
	}
	redactByLeafName(m)
	if mcp, ok := m["mcp"].(map[string]any); ok {
		if servers, ok := mcp["servers"].(map[string]any); ok {
			for _, entry := range servers {
				if srv, ok := entry.(map[string]any); ok {
					if _, has := srv["url"]; has {
						srv["url"] = RedactedValue
					}
				}
			}
		}
	}
}

// redactByLeafName walks any nested map/slice tree and replaces string leaves
// whose key matches sensitiveConfigLeafNames with RedactedValue.
func redactByLeafName(node any) {
	switch typed := node.(type) {
	case map[string]any:
		for k, v := range typed {
			if sensitiveConfigLeafNames[strings.ToLower(k)] {
				if _, isString := v.(string); isString {
					typed[k] = RedactedValue
					continue
				}
			}
			redactByLeafName(v)
		}
	case []any:
		for _, item := range typed {
			redactByLeafName(item)
		}
	}
}

// SanitizedConfig mirrors Config but with sensitive fields redacted.
type SanitizedConfig struct {
	Version    int                `json:"version,omitempty" yaml:"version,omitempty" doc:"Config file version" maximum:"10" example:"1"`
	Catalogs   []SanitizedCatalog `json:"catalogs" yaml:"catalogs" doc:"Configured solution catalogs" maxItems:"50"`
	Settings   Settings           `json:"settings" yaml:"settings" doc:"General application settings"`
	Logging    LoggingConfig      `json:"logging" yaml:"logging" doc:"Logging configuration"`
	HTTPClient HTTPClientConfig   `json:"httpClient" yaml:"httpClient" doc:"HTTP client configuration"`
	CEL        CELConfig          `json:"cel" yaml:"cel" doc:"CEL expression engine configuration"`
	Resolver   ResolverConfig     `json:"resolver" yaml:"resolver" doc:"Resolver execution configuration"`
	Action     ActionConfig       `json:"action" yaml:"action" doc:"Action execution configuration"`
	Auth       SanitizedAuth      `json:"auth" yaml:"auth" doc:"Authentication configuration (redacted)"`
	Build      BuildConfig        `json:"build" yaml:"build" doc:"Build configuration"`
	APIServer  APIServerConfig    `json:"apiServer,omitempty" yaml:"apiServer,omitempty" doc:"REST API server configuration"`
	MCP        SanitizedMCPConfig `json:"mcp,omitempty" yaml:"mcp,omitempty" doc:"MCP server configuration (URLs redacted)"`
}

// SanitizedCatalog redacts auth tokens from catalog config.
type SanitizedCatalog struct {
	Name     string            `json:"name" yaml:"name" doc:"Catalog name" maxLength:"256" example:"my-catalog"`
	Type     string            `json:"type" yaml:"type" doc:"Catalog type" maxLength:"64" example:"git"`
	Path     string            `json:"path,omitempty" yaml:"path,omitempty" doc:"Local filesystem path" maxLength:"1024" example:"/path/to/catalog"`
	URL      string            `json:"url,omitempty" yaml:"url,omitempty" doc:"Remote URL" maxLength:"2048" example:"https://github.com/org/catalog"`
	Auth     *SanitizedCatAuth `json:"auth,omitempty" yaml:"auth,omitempty" doc:"Authentication settings (redacted)"`
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty" doc:"Additional metadata"`
}

// SanitizedCatAuth contains only non-sensitive catalog auth fields.
type SanitizedCatAuth struct {
	Type        string `json:"type" yaml:"type" doc:"Authentication type" maxLength:"64" example:"token"`
	TokenEnvVar string `json:"tokenEnvVar,omitempty" yaml:"tokenEnvVar,omitempty" doc:"Environment variable name for token" maxLength:"256" example:"CATALOG_TOKEN"`
}

// SanitizedAuth redacts client secrets and tokens from auth config.
type SanitizedAuth struct {
	Entra  *SanitizedEntraAuth  `json:"entra,omitempty" yaml:"entra,omitempty" doc:"Entra ID auth configuration (redacted)"`
	GitHub *SanitizedGitHubAuth `json:"github,omitempty" yaml:"github,omitempty" doc:"GitHub auth configuration (redacted)"`
	GCP    *SanitizedGCPAuth    `json:"gcp,omitempty" yaml:"gcp,omitempty" doc:"GCP auth configuration (redacted)"`
}

// SanitizedEntraAuth contains only non-sensitive Entra ID fields.
type SanitizedEntraAuth struct {
	ClientID      string   `json:"clientId,omitempty" yaml:"clientId,omitempty" doc:"Entra ID application client ID" maxLength:"256" example:"00000000-0000-0000-0000-000000000000"`
	TenantID      string   `json:"tenantId,omitempty" yaml:"tenantId,omitempty" doc:"Entra ID tenant ID" maxLength:"256" example:"00000000-0000-0000-0000-000000000000"`
	DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" doc:"Default OAuth scopes" maxItems:"20"`
	DefaultFlow   string   `json:"defaultFlow,omitempty" yaml:"defaultFlow,omitempty" doc:"Default interactive auth flow" maxLength:"32" example:"device_code"`
}

// SanitizedGitHubAuth contains only non-sensitive GitHub auth fields.
type SanitizedGitHubAuth struct {
	ClientID      string   `json:"clientId,omitempty" yaml:"clientId,omitempty" doc:"GitHub OAuth app client ID" maxLength:"256" example:"Iv1.abc123"`
	Hostname      string   `json:"hostname,omitempty" yaml:"hostname,omitempty" doc:"GitHub hostname" maxLength:"256" example:"github.com"`
	DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" doc:"Default OAuth scopes" maxItems:"20"`
}

// SanitizedGCPAuth contains only non-sensitive GCP auth fields.
type SanitizedGCPAuth struct {
	ClientID                  string   `json:"clientId,omitempty" yaml:"clientId,omitempty" doc:"GCP OAuth client ID" maxLength:"256" example:"123456789.apps.googleusercontent.com"`
	GCPClientCredential       string   `json:"gcpClientCredential,omitempty" yaml:"gcpClientCredential,omitempty" doc:"GCP client credential file path" maxLength:"1024" example:"/path/to/credentials.json"`
	DefaultScopes             []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" doc:"Default OAuth scopes" maxItems:"20"`
	ImpersonateServiceAccount string   `json:"impersonateServiceAccount,omitempty" yaml:"impersonateServiceAccount,omitempty" doc:"Service account to impersonate" maxLength:"512" example:"sa@project.iam.gserviceaccount.com"`
	Project                   string   `json:"project,omitempty" yaml:"project,omitempty" doc:"GCP project ID" maxLength:"256" example:"my-gcp-project"`
}

// SanitizedMCPConfig mirrors MCPConfig but with URLs redacted.
type SanitizedMCPConfig struct {
	Servers map[string]SanitizedMCPServerConfig `json:"servers,omitempty" yaml:"servers,omitempty" doc:"Upstream MCP server configurations (URLs redacted)"`
}

// SanitizedMCPServerConfig contains only non-sensitive upstream MCP server fields.
type SanitizedMCPServerConfig struct {
	Enabled    *bool               `json:"enabled,omitempty" yaml:"enabled,omitempty" doc:"Whether this upstream server is active"`
	URL        string              `json:"url,omitempty" yaml:"url,omitempty" doc:"Upstream URL (redacted)"`
	Auth       MCPServerAuthConfig `json:"auth,omitempty" yaml:"auth,omitempty" doc:"Authentication configuration"`
	Timeout    string              `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Request timeout"`
	ToolPrefix string              `json:"toolPrefix,omitempty" yaml:"toolPrefix,omitempty" doc:"Prefix added to upstream tool names"`
	Tools      []string            `json:"tools,omitempty" yaml:"tools,omitempty" doc:"Tool name allowlist patterns"`
}

// SanitizeConfig creates a sanitized copy of the config with sensitive values redacted.
func SanitizeConfig(cfg *Config) SanitizedConfig {
	s := SanitizedConfig{
		Version:    cfg.Version,
		Settings:   cfg.Settings,
		Logging:    cfg.Logging,
		HTTPClient: cfg.HTTPClient,
		CEL:        cfg.CEL,
		Resolver:   cfg.Resolver,
		Action:     cfg.Action,
		Build:      cfg.Build,
		APIServer:  cfg.APIServer,
	}

	// Sanitize MCP upstream servers — redact URLs.
	if len(cfg.MCP.Servers) > 0 {
		s.MCP.Servers = make(map[string]SanitizedMCPServerConfig, len(cfg.MCP.Servers))
		for name, srv := range cfg.MCP.Servers {
			sanitized := SanitizedMCPServerConfig{
				Enabled:    srv.Enabled,
				Auth:       srv.Auth,
				Timeout:    srv.Timeout,
				ToolPrefix: srv.ToolPrefix,
				Tools:      srv.Tools,
			}
			if srv.URL != "" {
				sanitized.URL = RedactedValue
			}
			s.MCP.Servers[name] = sanitized
		}
	}

	// Sanitize catalogs
	s.Catalogs = make([]SanitizedCatalog, 0, len(cfg.Catalogs))
	for _, cat := range cfg.Catalogs {
		sc := SanitizedCatalog{
			Name:     cat.Name,
			Type:     cat.Type,
			Path:     cat.Path,
			URL:      cat.URL,
			Metadata: cat.Metadata,
		}
		if cat.Auth != nil {
			sc.Auth = &SanitizedCatAuth{
				Type:        cat.Auth.Type,
				TokenEnvVar: cat.Auth.TokenEnvVar,
			}
		}
		s.Catalogs = append(s.Catalogs, sc)
	}

	// Sanitize auth — redact secrets
	if cfg.Auth.Entra != nil {
		s.Auth.Entra = &SanitizedEntraAuth{
			ClientID:      cfg.Auth.Entra.ClientID,
			TenantID:      cfg.Auth.Entra.TenantID,
			DefaultScopes: cfg.Auth.Entra.DefaultScopes,
			DefaultFlow:   cfg.Auth.Entra.DefaultFlow,
		}
	}
	if cfg.Auth.GitHub != nil {
		s.Auth.GitHub = &SanitizedGitHubAuth{
			ClientID:      cfg.Auth.GitHub.ClientID,
			Hostname:      cfg.Auth.GitHub.Hostname,
			DefaultScopes: cfg.Auth.GitHub.DefaultScopes,
		}
	}
	if cfg.Auth.GCP != nil {
		gcp := &SanitizedGCPAuth{
			ClientID:                  cfg.Auth.GCP.ClientID,
			DefaultScopes:             cfg.Auth.GCP.DefaultScopes,
			ImpersonateServiceAccount: cfg.Auth.GCP.ImpersonateServiceAccount,
			Project:                   cfg.Auth.GCP.Project,
		}
		if cfg.Auth.GCP.ClientSecret != "" {
			gcp.GCPClientCredential = RedactedValue
		}
		s.Auth.GCP = gcp
	}

	return s
}
