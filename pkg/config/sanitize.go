// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

// RedactedValue is the placeholder inserted for sensitive fields.
const RedactedValue = "***REDACTED***"

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
