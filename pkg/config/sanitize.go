// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

// RedactedValue is the placeholder inserted for sensitive fields.
const RedactedValue = "***REDACTED***"

// SanitizedConfig mirrors Config but with sensitive fields redacted.
type SanitizedConfig struct {
	Version    int                `json:"version,omitempty"`
	Catalogs   []SanitizedCatalog `json:"catalogs"`
	Settings   Settings           `json:"settings"`
	Logging    LoggingConfig      `json:"logging"`
	HTTPClient HTTPClientConfig   `json:"httpClient"`
	CEL        CELConfig          `json:"cel"`
	Resolver   ResolverConfig     `json:"resolver"`
	Action     ActionConfig       `json:"action"`
	Auth       SanitizedAuth      `json:"auth"`
	Build      BuildConfig        `json:"build"`
}

// SanitizedCatalog redacts auth tokens from catalog config.
type SanitizedCatalog struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Path     string            `json:"path,omitempty"`
	URL      string            `json:"url,omitempty"`
	Auth     *SanitizedCatAuth `json:"auth,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// SanitizedCatAuth contains only non-sensitive catalog auth fields.
type SanitizedCatAuth struct {
	Type        string `json:"type"`
	TokenEnvVar string `json:"tokenEnvVar,omitempty"`
}

// SanitizedAuth redacts client secrets and tokens from auth config.
type SanitizedAuth struct {
	Entra  *SanitizedEntraAuth  `json:"entra,omitempty"`
	GitHub *SanitizedGitHubAuth `json:"github,omitempty"`
	GCP    *SanitizedGCPAuth    `json:"gcp,omitempty"`
}

// SanitizedEntraAuth contains only non-sensitive Entra ID fields.
type SanitizedEntraAuth struct {
	ClientID      string   `json:"clientId,omitempty"`
	TenantID      string   `json:"tenantId,omitempty"`
	DefaultScopes []string `json:"defaultScopes,omitempty"`
}

// SanitizedGitHubAuth contains only non-sensitive GitHub auth fields.
type SanitizedGitHubAuth struct {
	ClientID      string   `json:"clientId,omitempty"`
	Hostname      string   `json:"hostname,omitempty"`
	DefaultScopes []string `json:"defaultScopes,omitempty"`
}

// SanitizedGCPAuth contains only non-sensitive GCP auth fields.
type SanitizedGCPAuth struct {
	ClientID                  string   `json:"clientId,omitempty"`
	GCPClientCredential       string   `json:"gcpClientCredential,omitempty"`
	DefaultScopes             []string `json:"defaultScopes,omitempty"`
	ImpersonateServiceAccount string   `json:"impersonateServiceAccount,omitempty"`
	Project                   string   `json:"project,omitempty"`
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
