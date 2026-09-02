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
}

func TestRedactConfigMap(t *testing.T) {
	t.Parallel()

	t.Run("redacts known sensitive leaves case-insensitively", func(t *testing.T) {
		t.Parallel()
		m := map[string]any{
			"auth": map[string]any{
				"entra": map[string]any{
					"clientId":           "visible-id",
					"clientSecret":       "should-be-hidden",
					"federatedToken":     "raw-token",
					"federatedTokenFile": "/etc/oidc/token",
				},
				"github": map[string]any{
					"clientId":     "visible-gh-id",
					"clientSecret": "gh-secret",
					"privateKey":   "-----BEGIN RSA PRIVATE KEY-----...",
				},
				"gcp": map[string]any{
					"clientId":     "gcp-id",
					"clientSecret": "gcp-secret",
				},
			},
		}

		RedactConfigMap(m)

		auth := m["auth"].(map[string]any)
		entra := auth["entra"].(map[string]any)
		assert.Equal(t, "visible-id", entra["clientId"], "non-sensitive fields survive")
		assert.Equal(t, RedactedValue, entra["clientSecret"])
		assert.Equal(t, RedactedValue, entra["federatedToken"])
		assert.Equal(t, RedactedValue, entra["federatedTokenFile"])

		gh := auth["github"].(map[string]any)
		assert.Equal(t, RedactedValue, gh["clientSecret"])
		assert.Equal(t, RedactedValue, gh["privateKey"])

		gcp := auth["gcp"].(map[string]any)
		assert.Equal(t, RedactedValue, gcp["clientSecret"])
	})

	t.Run("preserves auth.handlers structure", func(t *testing.T) {
		t.Parallel()
		m := map[string]any{
			"auth": map[string]any{
				"handlers": map[string]any{
					"github": map[string]any{
						"hostname": map[string]any{
							"aliases": map[string]any{
								"github-saas": "https://api.github.com/",
							},
						},
					},
				},
			},
		}
		RedactConfigMap(m)
		alias := m["auth"].(map[string]any)["handlers"].(map[string]any)["github"].(map[string]any)["hostname"].(map[string]any)["aliases"].(map[string]any)
		assert.Equal(t, "https://api.github.com/", alias["github-saas"],
			"non-sensitive nested paths must survive")
	})

	t.Run("redacts profile-level secrets in slices", func(t *testing.T) {
		t.Parallel()
		m := map[string]any{
			"auth": map[string]any{
				"customOAuth2": []any{
					map[string]any{
						"name":         "quay",
						"clientId":     "visible",
						"clientSecret": "hidden",
					},
				},
			},
		}
		RedactConfigMap(m)
		entry := m["auth"].(map[string]any)["customOAuth2"].([]any)[0].(map[string]any)
		assert.Equal(t, "visible", entry["clientId"])
		assert.Equal(t, RedactedValue, entry["clientSecret"])
	})

	t.Run("redacts MCP server URLs which may embed tokens", func(t *testing.T) {
		t.Parallel()
		m := map[string]any{
			"mcp": map[string]any{
				"servers": map[string]any{
					"upstream": map[string]any{
						"url":        "https://token@example.com/mcp",
						"toolPrefix": "up_",
					},
				},
			},
		}
		RedactConfigMap(m)
		srv := m["mcp"].(map[string]any)["servers"].(map[string]any)["upstream"].(map[string]any)
		assert.Equal(t, RedactedValue, srv["url"])
		assert.Equal(t, "up_", srv["toolPrefix"], "non-sensitive MCP fields survive")
	})

	t.Run("field names like privateKeySecretName are not redacted", func(t *testing.T) {
		t.Parallel()
		// PrivateKeySecretName is a keyring entry name, not a credential.
		m := map[string]any{
			"auth": map[string]any{
				"github": map[string]any{
					"privateKeySecretName": "github-app-key",
				},
			},
		}
		RedactConfigMap(m)
		gh := m["auth"].(map[string]any)["github"].(map[string]any)
		assert.Equal(t, "github-app-key", gh["privateKeySecretName"],
			"keyring reference names are safe to display")
	})

	t.Run("nil and empty map are no-ops", func(t *testing.T) {
		t.Parallel()
		require.NotPanics(t, func() { RedactConfigMap(nil) })
		m := map[string]any{}
		require.NotPanics(t, func() { RedactConfigMap(m) })
		assert.Empty(t, m)
	})
}
