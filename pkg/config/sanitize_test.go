// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeConfig(t *testing.T) {
	t.Run("redacts GCP client secret", func(t *testing.T) {
		cfg := &Config{
			Auth: GlobalAuthConfig{
				GCP: &GCPAuthConfig{
					ClientID:     "visible-id",
					ClientSecret: "should-be-hidden",
					Project:      "my-proj",
				},
			},
		}

		sanitized := SanitizeConfig(cfg)
		assert.Equal(t, "visible-id", sanitized.Auth.GCP.ClientID)
		assert.Equal(t, RedactedValue, sanitized.Auth.GCP.GCPClientCredential)
		assert.Equal(t, "my-proj", sanitized.Auth.GCP.Project)
	})

	t.Run("empty secret not redacted", func(t *testing.T) {
		cfg := &Config{
			Auth: GlobalAuthConfig{
				GCP: &GCPAuthConfig{
					ClientID:     "visible-id",
					ClientSecret: "",
				},
			},
		}

		sanitized := SanitizeConfig(cfg)
		assert.Equal(t, "", sanitized.Auth.GCP.GCPClientCredential)
	})

	t.Run("nil auth sections", func(t *testing.T) {
		cfg := &Config{
			Auth: GlobalAuthConfig{},
		}

		sanitized := SanitizeConfig(cfg)
		assert.Nil(t, sanitized.Auth.Entra)
		assert.Nil(t, sanitized.Auth.GitHub)
		assert.Nil(t, sanitized.Auth.GCP)
	})

	t.Run("catalogs with auth", func(t *testing.T) {
		cfg := &Config{
			Catalogs: []CatalogConfig{
				{
					Name: "remote",
					Type: "oci",
					URL:  "https://registry.example.com",
					Auth: &AuthConfig{
						Type:        "token",
						TokenEnvVar: "MY_TOKEN",
					},
				},
			},
		}

		sanitized := SanitizeConfig(cfg)
		require.Len(t, sanitized.Catalogs, 1)
		assert.Equal(t, "remote", sanitized.Catalogs[0].Name)
		require.NotNil(t, sanitized.Catalogs[0].Auth)
		assert.Equal(t, "token", sanitized.Catalogs[0].Auth.Type)
		assert.Equal(t, "MY_TOKEN", sanitized.Catalogs[0].Auth.TokenEnvVar)
	})

	t.Run("MCP upstream URLs redacted", func(t *testing.T) {
		enabled := true
		cfg := &Config{
			MCP: MCPConfig{
				Servers: map[string]MCPServerConfig{
					"internal": {
						Enabled: &enabled,
						URL:     "http://svc.cluster.local:8080/mcp",
						Auth: MCPServerAuthConfig{
							Handler: "entra",
							Scope:   "api://app-id/.default",
						},
						Timeout:    "60s",
						ToolPrefix: "remote_",
						Tools:      []string{"deploy_*"},
					},
				},
			},
		}

		sanitized := SanitizeConfig(cfg)
		require.Len(t, sanitized.MCP.Servers, 1)

		srv := sanitized.MCP.Servers["internal"]
		assert.Equal(t, RedactedValue, srv.URL, "URL should be redacted")
		assert.Equal(t, &enabled, srv.Enabled)
		assert.Equal(t, "entra", srv.Auth.Handler)
		assert.Equal(t, "api://app-id/.default", srv.Auth.Scope)
		assert.Equal(t, "60s", srv.Timeout)
		assert.Equal(t, "remote_", srv.ToolPrefix)
		assert.Equal(t, []string{"deploy_*"}, srv.Tools)
	})

	t.Run("MCP empty URL not redacted", func(t *testing.T) {
		cfg := &Config{
			MCP: MCPConfig{
				Servers: map[string]MCPServerConfig{
					"no-url": {URL: ""},
				},
			},
		}

		sanitized := SanitizeConfig(cfg)
		assert.Equal(t, "", sanitized.MCP.Servers["no-url"].URL)
	})

	t.Run("MCP nil servers unchanged", func(t *testing.T) {
		cfg := &Config{}

		sanitized := SanitizeConfig(cfg)
		assert.Nil(t, sanitized.MCP.Servers)
	})

	t.Run("APIServer TLS key path redacted; cert path preserved", func(t *testing.T) {
		cfg := &Config{
			APIServer: APIServerConfig{
				TLS: APITLSConfig{
					Enabled: true,
					Cert:    "/etc/ssl/cert.pem",
					Key:     "/etc/ssl/key.pem",
				},
			},
		}
		sanitized := SanitizeConfig(cfg)
		assert.Equal(t, "/etc/ssl/cert.pem", sanitized.APIServer.TLS.Cert)
		assert.Equal(t, RedactedValue, sanitized.APIServer.TLS.Key)
	})

	t.Run("APIServer opaque auth Handlers map values redacted; keys preserved", func(t *testing.T) {
		cfg := &Config{
			APIServer: APIServerConfig{
				Auth: APIAuthConfig{
					Handlers: map[string]any{
						"github":    map[string]any{"secret": "s"},
						"openshift": "opaque-token-string",
					},
				},
			},
		}
		sanitized := SanitizeConfig(cfg)
		assert.Contains(t, sanitized.APIServer.Auth.Handlers, "github")
		assert.Contains(t, sanitized.APIServer.Auth.Handlers, "openshift")
		assert.Equal(t, RedactedValue, sanitized.APIServer.Auth.Handlers["github"])
		assert.Equal(t, RedactedValue, sanitized.APIServer.Auth.Handlers["openshift"])
	})

	t.Run("auth handlers opaque Settings map dropped, Hostname preserved", func(t *testing.T) {
		cfg := &Config{
			Auth: GlobalAuthConfig{
				Handlers: map[string]HandlerConfig{
					"gh": {
						Hostname: &HostnameConfig{
							Aliases: map[string]string{"saas": "https://api.github.com/"},
						},
						Settings: map[string]any{"opaque": "should-not-appear"},
					},
				},
			},
		}
		sanitized := SanitizeConfig(cfg)
		gh, ok := sanitized.Auth.Handlers["gh"]
		require.True(t, ok, "handler must be present")
		require.NotNil(t, gh.Hostname)
		assert.Equal(t, "https://api.github.com/", gh.Hostname.Aliases["saas"],
			"user-visible aliases must survive")
	})

	t.Run("hostname resolver source Headers values redacted", func(t *testing.T) {
		cfg := &Config{
			Auth: GlobalAuthConfig{
				Handlers: map[string]HandlerConfig{
					"gh": {
						Hostname: &HostnameConfig{
							Resolver: &HostnameResolverConfig{
								Source: HostnameResolverSource{
									URL: "https://inventory.example.com",
									Headers: map[string]string{
										"Authorization": "Bearer super-secret-token",
										"X-API-Key":     "another-secret",
									},
								},
							},
						},
					},
				},
			},
		}
		sanitized := SanitizeConfig(cfg)
		src := sanitized.Auth.Handlers["gh"].Hostname.Resolver.Source
		assert.Equal(t, "https://inventory.example.com", src.URL, "URL preserved for verification")
		assert.Equal(t, RedactedValue, src.Headers["Authorization"])
		assert.Equal(t, RedactedValue, src.Headers["X-API-Key"])
	})

	t.Run("customOAuth2 client secret dropped, other fields preserved", func(t *testing.T) {
		cfg := &Config{
			Auth: GlobalAuthConfig{
				CustomOAuth2: []CustomOAuth2Config{
					{
						Name:         "quay",
						ClientID:     "visible-id",
						ClientSecret: "should-not-appear",
						TokenExchange: &TokenExchangeConfig{
							URL:         "https://quay.io/api/v1/user/apptoken",
							RequestBody: "should-not-appear-either",
						},
					},
				},
			},
		}
		sanitized := SanitizeConfig(cfg)
		require.Len(t, sanitized.Auth.CustomOAuth2, 1)
		oc := sanitized.Auth.CustomOAuth2[0]
		assert.Equal(t, "quay", oc.Name)
		assert.Equal(t, "visible-id", oc.ClientID)
		// SanitizedCustomOAuth2 has no ClientSecret field, so nothing to check
		// there -- what matters is that the marshaled output cannot contain it.
		require.NotNil(t, oc.TokenExchange)
		assert.Equal(t, "https://quay.io/api/v1/user/apptoken", oc.TokenExchange.URL)
		// SanitizedTokenExchange omits RequestBody entirely.
	})

	t.Run("kube cluster aliases preserved verbatim", func(t *testing.T) {
		cfg := &Config{
			Kube: KubeConfig{
				Clusters: ClusterResolutionConfig{
					Aliases: map[string]ClusterAlias{
						"lab": {
							Server:         "https://api.lab.example.com:6443",
							DefaultHandler: "openshift",
						},
					},
				},
			},
		}
		sanitized := SanitizeConfig(cfg)
		lab, ok := sanitized.Kube.Clusters.Aliases["lab"]
		require.True(t, ok)
		assert.Equal(t, "https://api.lab.example.com:6443", lab.Server)
		assert.Equal(t, "openshift", lab.DefaultHandler)
	})

	t.Run("kube resolver Headers redacted like auth resolver", func(t *testing.T) {
		cfg := &Config{
			Kube: KubeConfig{
				Clusters: ClusterResolutionConfig{
					Resolver: &HostnameResolverConfig{
						Source: HostnameResolverSource{
							URL: "https://clusters.example.com",
							Headers: map[string]string{
								"Authorization": "Bearer secret",
							},
						},
					},
				},
			},
		}
		sanitized := SanitizeConfig(cfg)
		require.NotNil(t, sanitized.Kube.Clusters.Resolver)
		assert.Equal(t, RedactedValue,
			sanitized.Kube.Clusters.Resolver.Source.Headers["Authorization"])
	})

	t.Run("Telemetry / GoTemplate / Discovery / Plugins pass through unchanged", func(t *testing.T) {
		cfg := &Config{
			Telemetry:  TelemetryConfig{Endpoint: "https://otlp.example.com", ServiceName: "svc"},
			GoTemplate: GoTemplateConfig{CacheSize: 500},
			Discovery:  DiscoveryConfig{ActionFiles: []string{"actions.yaml"}},
			Plugins:    PluginsConfig{FetchCooldown: "30s"},
		}
		sanitized := SanitizeConfig(cfg)
		assert.Equal(t, "https://otlp.example.com", sanitized.Telemetry.Endpoint)
		assert.Equal(t, "svc", sanitized.Telemetry.ServiceName)
		assert.Equal(t, 500, sanitized.GoTemplate.CacheSize)
		assert.Equal(t, []string{"actions.yaml"}, sanitized.Discovery.ActionFiles)
		assert.Equal(t, "30s", sanitized.Plugins.FetchCooldown)
	})
}

// TestSanitizeAPIServer_PreservesNonSecretFields is a regression guard for the
// sanitized view silently dropping fields. `config view` uses a fail-closed
// allowlist, so every non-secret field must be mirrored explicitly or it
// disappears from the operator's view of their own configuration -- which is
// exactly how the idle/header/host hardening settings went missing.
func TestSanitizeAPIServer_PreservesNonSecretFields(t *testing.T) {
	t.Parallel()

	in := APIServerConfig{
		Host:           "0.0.0.0",
		Port:           9090,
		IdleTimeout:    "45s",
		MaxHeaderBytes: 4096,
		AllowedHosts:   []string{"api.example.com", "*.internal.example.com"},
	}

	out := sanitizeAPIServer(in)

	assert.Equal(t, in.IdleTimeout, out.IdleTimeout, "idleTimeout must survive sanitization")
	assert.Equal(t, in.MaxHeaderBytes, out.MaxHeaderBytes, "maxHeaderBytes must survive sanitization")
	assert.Equal(t, in.AllowedHosts, out.AllowedHosts, "allowedHosts must survive sanitization")
	assert.Equal(t, in.Host, out.Host)
	assert.Equal(t, in.Port, out.Port)
}
