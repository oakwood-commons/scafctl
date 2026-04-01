// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"

	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// InitAPI initialises the Huma API on the server's router.
// It must be called after SetupMiddleware so the router is ready.
func (s *Server) InitAPI() {
	apiVersion := s.cfg.APIServer.APIVersion
	if apiVersion == "" {
		apiVersion = settings.DefaultAPIVersion
	}

	cfg := BuildHumaConfig(apiVersion)
	s.api = humachi.New(s.router, cfg)
}

// BuildHumaConfig creates a Huma config with OpenAPI info, servers, security
// schemes, and documentation paths. It is exported so that standalone spec
// generators (CLI openapi command, MCP tools) produce an identical spec to the
// live server, including all security scheme definitions.
func BuildHumaConfig(apiVersion string) huma.Config {
	cfg := huma.DefaultConfig("scafctl API", settings.VersionInformation.BuildVersion)

	configureOpenAPIInfo(&cfg)
	configureOpenAPIServers(&cfg)
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

// configureOpenAPIServers adds default and localhost server entries.
func configureOpenAPIServers(cfg *huma.Config) {
	cfg.Servers = []*huma.Server{
		{URL: fmt.Sprintf("http://localhost:%d", settings.DefaultAPIPort), Description: "Local development"},
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
