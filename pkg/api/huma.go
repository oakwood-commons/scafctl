// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// InitAPI initialises the Huma API on the server's router.
// It must be called after SetupMiddleware so the router is ready.
func (s *Server) InitAPI() {
	apiVersion := s.cfg.APIServer.APIVersion
	if apiVersion == "" {
		apiVersion = settings.DefaultAPIVersion
	}

	cfg := BuildHumaConfig(apiVersion, s.cfg)
	s.api = humachi.New(s.router, cfg)
}

// BuildHumaConfig creates a Huma config with OpenAPI info, servers, security
// schemes, and documentation paths. It is exported so that standalone spec
// generators (CLI openapi command, MCP tools) produce an identical spec to the
// live server, including all security scheme definitions.
// appCfg is optional; when non-nil, OpenAPI server entries are derived from the
// active APIServer configuration (host, port, TLS) instead of hard-coded defaults.
func BuildHumaConfig(apiVersion string, appCfg *config.Config) huma.Config {
	cfg := huma.DefaultConfig("scafctl API", settings.VersionInformation.BuildVersion)

	configureOpenAPIInfo(&cfg)
	configureOpenAPIServers(&cfg, appCfg)
	configureSecuritySchemes(&cfg)
	configureDocsPaths(&cfg, apiVersion)

	return cfg
}

// configureOpenAPIInfo sets top-level OpenAPI metadata.
func configureOpenAPIInfo(cfg *huma.Config) {
	cfg.Info = &huma.Info{
		Title:       "scafctl API",
		Version:     settings.VersionInformation.BuildVersion,
		Description: "REST API for scafctl configuration discovery and scaffolding",
		Contact: &huma.Contact{
			Name: "scafctl maintainers",
		},
		License: &huma.License{
			Name: "Apache-2.0",
		},
	}
}

// configureOpenAPIServers adds OpenAPI server entries.
// When appCfg is provided, the server URL is derived from the active host, port,
// and TLS settings; otherwise a localhost default is used.
func configureOpenAPIServers(cfg *huma.Config, appCfg *config.Config) {
	if appCfg != nil && len(appCfg.APIServer.OpenAPI.Servers) > 0 {
		for _, s := range appCfg.APIServer.OpenAPI.Servers {
			cfg.Servers = append(cfg.Servers, &huma.Server{URL: s.URL, Description: s.Description})
		}
		return
	}

	host := settings.DefaultAPIHost
	port := settings.DefaultAPIPort
	scheme := "http"

	if appCfg != nil {
		if appCfg.APIServer.Host != "" {
			host = appCfg.APIServer.Host
		}
		if appCfg.APIServer.Port != 0 {
			port = appCfg.APIServer.Port
		}
		if appCfg.APIServer.TLS.Enabled {
			scheme = "https"
		}
	}

	if host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}

	cfg.Servers = []*huma.Server{
		{URL: fmt.Sprintf("%s://%s:%d", scheme, host, port), Description: "Local development"},
	}
}

// configureSecuritySchemes sets up OAuth2 / Entra OIDC security schemes.
func configureSecuritySchemes(cfg *huma.Config) {
	cfg.Components = &huma.Components{
		SecuritySchemes: map[string]*huma.SecurityScheme{
			"oauth2": {
				Type: "oauth2",
				Flows: &huma.OAuthFlows{
					AuthorizationCode: &huma.OAuthFlow{ //nolint:gosec // Not credentials, OpenAPI spec template
						AuthorizationURL: "https://login.microsoftonline.com/{tenantId}/oauth2/v2.0/authorize",
						TokenURL:         "https://login.microsoftonline.com/{tenantId}/oauth2/v2.0/token",
						Scopes: map[string]string{
							"api://{clientId}/.default": "Default API access",
						},
					},
				},
				Description: "Azure Entra ID OAuth2 authentication",
			},
		},
	}
}

// configureDocsPaths sets OpenAPI spec and docs paths.
func configureDocsPaths(cfg *huma.Config, apiVersion string) {
	cfg.OpenAPIPath = fmt.Sprintf("/%s/openapi", apiVersion)
	cfg.DocsPath = fmt.Sprintf("/%s/docs", apiVersion)
}
