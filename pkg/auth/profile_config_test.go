// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
)

func TestResolveActiveProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ctx         context.Context
		handlerName string
		want        string
	}{
		{
			name:        "no profile set anywhere",
			ctx:         context.Background(),
			handlerName: "github",
			want:        "",
		},
		{
			name:        "per-command profile takes precedence",
			ctx:         WithProfile(WithGlobalProfile(context.Background(), "global"), "command"),
			handlerName: "github",
			want:        "command",
		},
		{
			name:        "global profile used when no per-command",
			ctx:         WithGlobalProfile(context.Background(), "global"),
			handlerName: "github",
			want:        "global",
		},
		{
			name: "config active profile for github",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{
					GitHub: &config.GitHubAuthConfig{ActiveProfile: "work"},
				},
			}),
			handlerName: "github",
			want:        "work",
		},
		{
			name: "config active profile for entra",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{ActiveProfile: "prod"},
				},
			}),
			handlerName: "entra",
			want:        "prod",
		},
		{
			name: "config active profile for gcp",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{
					GCP: &config.GCPAuthConfig{ActiveProfile: "personal"},
				},
			}),
			handlerName: "gcp",
			want:        "personal",
		},
		{
			name: "global profile overrides config",
			ctx: config.WithConfig(
				WithGlobalProfile(context.Background(), "flag-profile"),
				&config.Config{
					Auth: config.GlobalAuthConfig{
						GitHub: &config.GitHubAuthConfig{ActiveProfile: "config-profile"},
					},
				},
			),
			handlerName: "github",
			want:        "flag-profile",
		},
		{
			name:        "unknown handler returns empty",
			ctx:         context.Background(),
			handlerName: "unknown",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveActiveProfile(tt.ctx, tt.handlerName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestListConfiguredProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ctx         context.Context
		handlerName string
		want        []string
	}{
		{
			name:        "no config",
			ctx:         context.Background(),
			handlerName: "github",
			want:        nil,
		},
		{
			name: "github profiles",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{
					GitHub: &config.GitHubAuthConfig{
						Profiles: map[string]*config.GitHubProfileConfig{
							"work":     {},
							"personal": {},
						},
					},
				},
			}),
			handlerName: "github",
			want:        []string{"work", "personal"},
		},
		{
			name: "empty profiles map",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{
					GitHub: &config.GitHubAuthConfig{},
				},
			}),
			handlerName: "github",
			want:        nil,
		},
		{
			name: "entra profiles",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{
						Profiles: map[string]*config.EntraProfileConfig{
							"prod": {},
						},
					},
				},
			}),
			handlerName: "entra",
			want:        []string{"prod"},
		},
		{
			name: "gcp profiles",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{
					GCP: &config.GCPAuthConfig{
						Profiles: map[string]*config.GCPProfileConfig{
							"corp": {},
						},
					},
				},
			}),
			handlerName: "gcp",
			want:        []string{"corp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ListConfiguredProfiles(tt.ctx, tt.handlerName)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				assert.ElementsMatch(t, tt.want, got)
			}
		})
	}
}

func TestValidateAuthProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: false,
		},
		{
			name:    "empty config",
			cfg:     &config.Config{},
			wantErr: false,
		},
		{
			name: "valid profiles",
			cfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					GitHub: &config.GitHubAuthConfig{
						ActiveProfile: "work",
						Profiles: map[string]*config.GitHubProfileConfig{
							"work":     {},
							"personal": {},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid active profile",
			cfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					GitHub: &config.GitHubAuthConfig{
						ActiveProfile: "has spaces",
					},
				},
			},
			wantErr: true,
			errMsg:  "auth.github.activeProfile",
		},
		{
			name: "invalid profile map key",
			cfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{
						Profiles: map[string]*config.EntraProfileConfig{
							"valid": {},
							"@bad":  {},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "auth.entra.profiles",
		},
		{
			name: "profile name with separator",
			cfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					GCP: &config.GCPAuthConfig{
						Profiles: map[string]*config.GCPProfileConfig{
							"has@sep": {},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "auth.gcp.profiles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateAuthProfiles(tt.cfg)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGlobalProfileContext(t *testing.T) {
	t.Parallel()

	t.Run("empty context returns empty", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", GlobalProfileFromContext(context.Background()))
	})

	t.Run("set and get", func(t *testing.T) {
		t.Parallel()
		ctx := WithGlobalProfile(context.Background(), "work")
		assert.Equal(t, "work", GlobalProfileFromContext(ctx))
	})

	t.Run("does not interfere with per-command profile", func(t *testing.T) {
		t.Parallel()
		ctx := WithGlobalProfile(context.Background(), "global")
		ctx = WithProfile(ctx, "command")
		assert.Equal(t, "global", GlobalProfileFromContext(ctx))
		assert.Equal(t, "command", ProfileFromContext(ctx))
	})
}

func TestEnsureProfileRegistered(t *testing.T) {
	t.Parallel()

	t.Run("nil config is a no-op", func(t *testing.T) {
		t.Parallel()
		EnsureProfileRegistered(nil, "github", "work")
		// no panic
	})

	t.Run("empty profile is a no-op", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		EnsureProfileRegistered(cfg, "github", "")
		assert.Nil(t, cfg.Auth.GitHub)
	})

	t.Run("github creates handler and profiles map", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		EnsureProfileRegistered(cfg, "github", "work")
		assert.NotNil(t, cfg.Auth.GitHub)
		assert.Contains(t, cfg.Auth.GitHub.Profiles, "work")
		assert.NotNil(t, cfg.Auth.GitHub.Profiles["work"])
	})

	t.Run("github does not overwrite existing profile", func(t *testing.T) {
		t.Parallel()
		existing := &config.GitHubProfileConfig{ClientID: "custom-client"}
		cfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				GitHub: &config.GitHubAuthConfig{
					Profiles: map[string]*config.GitHubProfileConfig{
						"work": existing,
					},
				},
			},
		}
		EnsureProfileRegistered(cfg, "github", "work")
		assert.Equal(t, "custom-client", cfg.Auth.GitHub.Profiles["work"].ClientID)
	})

	t.Run("entra creates handler and profiles map", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		EnsureProfileRegistered(cfg, "entra", "personal")
		assert.NotNil(t, cfg.Auth.Entra)
		assert.Contains(t, cfg.Auth.Entra.Profiles, "personal")
	})

	t.Run("gcp creates handler and profiles map", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		EnsureProfileRegistered(cfg, "gcp", "staging")
		assert.NotNil(t, cfg.Auth.GCP)
		assert.Contains(t, cfg.Auth.GCP.Profiles, "staging")
	})

	t.Run("unknown handler is a no-op", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		EnsureProfileRegistered(cfg, "unknown", "work")
		assert.Nil(t, cfg.Auth.GitHub)
		assert.Nil(t, cfg.Auth.Entra)
		assert.Nil(t, cfg.Auth.GCP)
	})

	t.Run("multiple profiles accumulate", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		EnsureProfileRegistered(cfg, "github", "work")
		EnsureProfileRegistered(cfg, "github", "personal")
		assert.Len(t, cfg.Auth.GitHub.Profiles, 2)
		assert.Contains(t, cfg.Auth.GitHub.Profiles, "work")
		assert.Contains(t, cfg.Auth.GitHub.Profiles, "personal")
	})
}

func TestDeleteProfile(t *testing.T) {
	t.Parallel()

	t.Run("nil config returns error", func(t *testing.T) {
		t.Parallel()
		found, err := DeleteProfile(nil, "github", "work")
		assert.Error(t, err)
		assert.False(t, found)
	})

	t.Run("empty profile returns error", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		found, err := DeleteProfile(cfg, "github", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot delete the unnamed built-in profile")
		assert.False(t, found)
	})

	t.Run("unknown handler returns error", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		found, err := DeleteProfile(cfg, "unknown", "work")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown handler")
		assert.False(t, found)
	})

	t.Run("github profile not found", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				GitHub: &config.GitHubAuthConfig{
					Profiles: map[string]*config.GitHubProfileConfig{
						"work": {},
					},
				},
			},
		}
		found, err := DeleteProfile(cfg, "github", "personal")
		assert.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("github delete existing profile", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				GitHub: &config.GitHubAuthConfig{
					Profiles: map[string]*config.GitHubProfileConfig{
						"work":     {},
						"personal": {},
					},
				},
			},
		}
		found, err := DeleteProfile(cfg, "github", "work")
		assert.NoError(t, err)
		assert.True(t, found)
		assert.NotContains(t, cfg.Auth.GitHub.Profiles, "work")
		assert.Contains(t, cfg.Auth.GitHub.Profiles, "personal")
	})

	t.Run("github resets active profile when deleting active", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				GitHub: &config.GitHubAuthConfig{
					ActiveProfile: "staging",
					Profiles: map[string]*config.GitHubProfileConfig{
						"staging": {},
						"work":    {},
					},
				},
			},
		}
		found, err := DeleteProfile(cfg, "github", "staging")
		assert.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "", cfg.Auth.GitHub.ActiveProfile)
	})

	t.Run("github keeps active profile when deleting non-active", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				GitHub: &config.GitHubAuthConfig{
					ActiveProfile: "work",
					Profiles: map[string]*config.GitHubProfileConfig{
						"staging": {},
						"work":    {},
					},
				},
			},
		}
		found, err := DeleteProfile(cfg, "github", "staging")
		assert.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "work", cfg.Auth.GitHub.ActiveProfile)
	})

	t.Run("entra delete profile", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				Entra: &config.EntraAuthConfig{
					ActiveProfile: "prod",
					Profiles: map[string]*config.EntraProfileConfig{
						"prod": {},
						"dev":  {},
					},
				},
			},
		}
		found, err := DeleteProfile(cfg, "entra", "prod")
		assert.NoError(t, err)
		assert.True(t, found)
		assert.NotContains(t, cfg.Auth.Entra.Profiles, "prod")
		assert.Equal(t, "", cfg.Auth.Entra.ActiveProfile)
	})

	t.Run("gcp delete profile", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				GCP: &config.GCPAuthConfig{
					Profiles: map[string]*config.GCPProfileConfig{
						"internal": {},
						"external": {},
					},
				},
			},
		}
		found, err := DeleteProfile(cfg, "gcp", "internal")
		assert.NoError(t, err)
		assert.True(t, found)
		assert.NotContains(t, cfg.Auth.GCP.Profiles, "internal")
		assert.Contains(t, cfg.Auth.GCP.Profiles, "external")
	})

	t.Run("nil handler config returns not found", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		found, err := DeleteProfile(cfg, "github", "work")
		assert.NoError(t, err)
		assert.False(t, found)
	})
}

func TestConfigActiveProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ctx         context.Context
		handlerName string
		want        string
	}{
		{
			name:        "nil config returns empty",
			ctx:         context.Background(),
			handlerName: "entra",
			want:        "",
		},
		{
			name: "entra active profile",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{ActiveProfile: "work"},
				},
			}),
			handlerName: "entra",
			want:        "work",
		},
		{
			name: "github active profile",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{
					GitHub: &config.GitHubAuthConfig{ActiveProfile: "oss"},
				},
			}),
			handlerName: "github",
			want:        "oss",
		},
		{
			name: "gcp active profile",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{
					GCP: &config.GCPAuthConfig{ActiveProfile: "staging"},
				},
			}),
			handlerName: "gcp",
			want:        "staging",
		},
		{
			name: "unknown handler returns empty",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{ActiveProfile: "work"},
				},
			}),
			handlerName: "unknown",
			want:        "",
		},
		{
			name: "nil entra config returns empty",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{},
			}),
			handlerName: "entra",
			want:        "",
		},
		{
			name: "nil github config returns empty",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{},
			}),
			handlerName: "github",
			want:        "",
		},
		{
			name: "nil gcp config returns empty",
			ctx: config.WithConfig(context.Background(), &config.Config{
				Auth: config.GlobalAuthConfig{},
			}),
			handlerName: "gcp",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ConfigActiveProfile(tt.ctx, tt.handlerName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveEntraConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *config.EntraAuthConfig
		profile string
		check   func(t *testing.T, result *config.EntraAuthConfig)
	}{
		{
			name: "nil config returns nil",
			cfg:  nil,
			check: func(t *testing.T, result *config.EntraAuthConfig) {
				assert.Nil(t, result)
			},
		},
		{
			name: "empty profile returns top-level without profiles/activeProfile",
			cfg: &config.EntraAuthConfig{
				ClientID:      "top-level-client",
				TenantID:      "top-level-tenant",
				ActiveProfile: "work",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {ClientID: "work-client"},
				},
			},
			profile: "",
			check: func(t *testing.T, result *config.EntraAuthConfig) {
				assert.Equal(t, "top-level-client", result.ClientID)
				assert.Equal(t, "top-level-tenant", result.TenantID)
				assert.Empty(t, result.ActiveProfile)
				assert.Nil(t, result.Profiles)
			},
		},
		{
			name: "profile overrides clientId and tenantId",
			cfg: &config.EntraAuthConfig{
				ClientID: "top-level-client",
				TenantID: "top-level-tenant",
				Profiles: map[string]*config.EntraProfileConfig{
					"machine": {
						ClientID: "machine-client",
						TenantID: "machine-tenant",
					},
				},
			},
			profile: "machine",
			check: func(t *testing.T, result *config.EntraAuthConfig) {
				assert.Equal(t, "machine-client", result.ClientID)
				assert.Equal(t, "machine-tenant", result.TenantID)
			},
		},
		{
			name: "profile inherits unset fields from top-level",
			cfg: &config.EntraAuthConfig{
				ClientID:  "top-level-client",
				TenantID:  "top-level-tenant",
				Authority: "https://login.microsoftonline.com",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {ClientID: "work-client"},
				},
			},
			profile: "work",
			check: func(t *testing.T, result *config.EntraAuthConfig) {
				assert.Equal(t, "work-client", result.ClientID)
				assert.Equal(t, "top-level-tenant", result.TenantID)
				assert.Equal(t, "https://login.microsoftonline.com", result.Authority)
			},
		},
		{
			name: "profile with WIF fields",
			cfg: &config.EntraAuthConfig{
				ClientID: "top-level-client",
				TenantID: "contoso",
				Profiles: map[string]*config.EntraProfileConfig{
					"wif-a": {
						ClientID:           "wif-client-a",
						FederatedTokenFile: "/var/run/secrets/token-a",
					},
				},
			},
			profile: "wif-a",
			check: func(t *testing.T, result *config.EntraAuthConfig) {
				assert.Equal(t, "wif-client-a", result.ClientID)
				assert.Equal(t, "contoso", result.TenantID)
				assert.Equal(t, "/var/run/secrets/token-a", result.FederatedTokenFile)
			},
		},
		{
			name: "profile with client secret for service principal",
			cfg: &config.EntraAuthConfig{
				ClientID: "top-level-client",
				TenantID: "contoso",
				Profiles: map[string]*config.EntraProfileConfig{
					"sp": {
						ClientID:     "sp-client",
						ClientSecret: "sp-secret",
					},
				},
			},
			profile: "sp",
			check: func(t *testing.T, result *config.EntraAuthConfig) {
				assert.Equal(t, "sp-client", result.ClientID)
				assert.Equal(t, "sp-secret", result.ClientSecret)
			},
		},
		{
			name: "unknown profile returns top-level",
			cfg: &config.EntraAuthConfig{
				ClientID: "top-level-client",
				Profiles: map[string]*config.EntraProfileConfig{
					"work": {ClientID: "work-client"},
				},
			},
			profile: "nonexistent",
			check: func(t *testing.T, result *config.EntraAuthConfig) {
				assert.Equal(t, "top-level-client", result.ClientID)
			},
		},
		{
			name: "profile scopes override top-level",
			cfg: &config.EntraAuthConfig{
				DefaultScopes: []string{"openid", "profile"},
				Profiles: map[string]*config.EntraProfileConfig{
					"custom": {DefaultScopes: []string{"api://custom/.default"}},
				},
			},
			profile: "custom",
			check: func(t *testing.T, result *config.EntraAuthConfig) {
				assert.Equal(t, []string{"api://custom/.default"}, result.DefaultScopes)
			},
		},
		{
			name: "profile overrides authority and default flow",
			cfg: &config.EntraAuthConfig{
				Authority:   "https://login.microsoftonline.com/common",
				DefaultFlow: "device_code",
				Profiles: map[string]*config.EntraProfileConfig{
					"tenant": {
						Authority:   "https://login.microsoftonline.com/contoso.com",
						DefaultFlow: "interactive",
					},
				},
			},
			profile: "tenant",
			check: func(t *testing.T, result *config.EntraAuthConfig) {
				assert.Equal(t, "https://login.microsoftonline.com/contoso.com", result.Authority)
				assert.Equal(t, "interactive", result.DefaultFlow)
			},
		},
		{
			name: "profile overrides federated token inline",
			cfg: &config.EntraAuthConfig{
				Profiles: map[string]*config.EntraProfileConfig{
					"wif-inline": {
						FederatedToken: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
						ClientID:       "wif-inline-client",
					},
				},
			},
			profile: "wif-inline",
			check: func(t *testing.T, result *config.EntraAuthConfig) {
				assert.Equal(t, "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...", result.FederatedToken)
				assert.Equal(t, "wif-inline-client", result.ClientID)
			},
		},
		{
			name: "empty profile returns top-level copy",
			cfg: &config.EntraAuthConfig{
				ClientID: "top-level",
				TenantID: "contoso",
			},
			profile: "",
			check: func(t *testing.T, result *config.EntraAuthConfig) {
				assert.Equal(t, "top-level", result.ClientID)
				assert.Equal(t, "contoso", result.TenantID)
				assert.Empty(t, result.ActiveProfile)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ResolveEntraConfig(tt.cfg, tt.profile)
			tt.check(t, result)
		})
	}
}

func TestResolveGitHubConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *config.GitHubAuthConfig
		profile string
		check   func(t *testing.T, result *config.GitHubAuthConfig)
	}{
		{
			name: "nil config returns nil",
			cfg:  nil,
			check: func(t *testing.T, result *config.GitHubAuthConfig) {
				assert.Nil(t, result)
			},
		},
		{
			name: "profile overrides app ID and installation ID",
			cfg: &config.GitHubAuthConfig{
				ClientID: "top-level-client",
				Profiles: map[string]*config.GitHubProfileConfig{
					"bot": {
						AppID:          12345,
						InstallationID: 67890,
						PrivateKeyPath: "/path/to/bot-key.pem",
					},
				},
			},
			profile: "bot",
			check: func(t *testing.T, result *config.GitHubAuthConfig) {
				assert.Equal(t, "top-level-client", result.ClientID)
				assert.Equal(t, int64(12345), result.AppID)
				assert.Equal(t, int64(67890), result.InstallationID)
				assert.Equal(t, "/path/to/bot-key.pem", result.PrivateKeyPath)
			},
		},
		{
			name: "profile overrides hostname for GHES",
			cfg: &config.GitHubAuthConfig{
				Hostname: "github.com",
				Profiles: map[string]*config.GitHubProfileConfig{
					"enterprise": {Hostname: "github.example.com"},
				},
			},
			profile: "enterprise",
			check: func(t *testing.T, result *config.GitHubAuthConfig) {
				assert.Equal(t, "github.example.com", result.Hostname)
			},
		},
		{
			name: "empty profile returns top-level copy",
			cfg: &config.GitHubAuthConfig{
				ClientID: "top-level-client",
				Hostname: "github.com",
			},
			profile: "",
			check: func(t *testing.T, result *config.GitHubAuthConfig) {
				assert.Equal(t, "top-level-client", result.ClientID)
				assert.Equal(t, "github.com", result.Hostname)
				assert.Empty(t, result.ActiveProfile)
			},
		},
		{
			name: "profile overrides client secret and scopes",
			cfg: &config.GitHubAuthConfig{
				ClientID:      "top-level-client",
				ClientSecret:  "top-level-secret",
				DefaultScopes: []string{"repo"},
				Profiles: map[string]*config.GitHubProfileConfig{
					"ci": {
						ClientSecret:  "ci-secret",
						DefaultScopes: []string{"repo", "packages:write"},
					},
				},
			},
			profile: "ci",
			check: func(t *testing.T, result *config.GitHubAuthConfig) {
				assert.Equal(t, "ci-secret", result.ClientSecret)
				assert.Equal(t, []string{"repo", "packages:write"}, result.DefaultScopes)
			},
		},
		{
			name: "profile overrides private key fields",
			cfg: &config.GitHubAuthConfig{
				Profiles: map[string]*config.GitHubProfileConfig{
					"app": {
						PrivateKey:           "inline-key-data",
						PrivateKeySecretName: "gh-app-key",
						ClientID:             "app-client",
					},
				},
			},
			profile: "app",
			check: func(t *testing.T, result *config.GitHubAuthConfig) {
				assert.Equal(t, "inline-key-data", result.PrivateKey)
				assert.Equal(t, "gh-app-key", result.PrivateKeySecretName)
				assert.Equal(t, "app-client", result.ClientID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ResolveGitHubConfig(tt.cfg, tt.profile)
			tt.check(t, result)
		})
	}
}

func TestResolveGCPConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *config.GCPAuthConfig
		profile string
		check   func(t *testing.T, result *config.GCPAuthConfig)
	}{
		{
			name: "nil config returns nil",
			cfg:  nil,
			check: func(t *testing.T, result *config.GCPAuthConfig) {
				assert.Nil(t, result)
			},
		},
		{
			name: "profile with service account key file",
			cfg: &config.GCPAuthConfig{
				ClientID: "top-level-client",
				Project:  "my-project",
				Profiles: map[string]*config.GCPProfileConfig{
					"deploy": {
						ServiceAccountKeyFile: "/path/to/deploy-sa.json",
						Project:               "deploy-project",
					},
				},
			},
			profile: "deploy",
			check: func(t *testing.T, result *config.GCPAuthConfig) {
				assert.Equal(t, "top-level-client", result.ClientID)
				assert.Equal(t, "deploy-project", result.Project)
				assert.Equal(t, "/path/to/deploy-sa.json", result.ServiceAccountKeyFile)
			},
		},
		{
			name: "profile with external account file for WIF",
			cfg: &config.GCPAuthConfig{
				Profiles: map[string]*config.GCPProfileConfig{
					"wif": {
						ExternalAccountFile: "/path/to/wif-config.json",
					},
				},
			},
			profile: "wif",
			check: func(t *testing.T, result *config.GCPAuthConfig) {
				assert.Equal(t, "/path/to/wif-config.json", result.ExternalAccountFile)
			},
		},
		{
			name: "profile overrides impersonate service account",
			cfg: &config.GCPAuthConfig{
				ImpersonateServiceAccount: "default@proj.iam.gserviceaccount.com",
				Profiles: map[string]*config.GCPProfileConfig{
					"admin": {
						ImpersonateServiceAccount: "admin@proj.iam.gserviceaccount.com",
					},
				},
			},
			profile: "admin",
			check: func(t *testing.T, result *config.GCPAuthConfig) {
				assert.Equal(t, "admin@proj.iam.gserviceaccount.com", result.ImpersonateServiceAccount)
			},
		},
		{
			name: "unknown profile returns top-level",
			cfg: &config.GCPAuthConfig{
				ClientID: "top-level-client",
			},
			profile: "nonexistent",
			check: func(t *testing.T, result *config.GCPAuthConfig) {
				assert.Equal(t, "top-level-client", result.ClientID)
			},
		},
		{
			name: "empty profile returns top-level copy",
			cfg: &config.GCPAuthConfig{
				ClientID: "gcp-client",
				Project:  "default-project",
			},
			profile: "",
			check: func(t *testing.T, result *config.GCPAuthConfig) {
				assert.Equal(t, "gcp-client", result.ClientID)
				assert.Equal(t, "default-project", result.Project)
				assert.Empty(t, result.ActiveProfile)
			},
		},
		{
			name: "profile overrides client secret and scopes",
			cfg: &config.GCPAuthConfig{
				ClientID:      "gcp-client",
				ClientSecret:  "top-secret",
				DefaultScopes: []string{"cloud-platform"},
				Profiles: map[string]*config.GCPProfileConfig{
					"ci": {
						ClientSecret:  "ci-secret",
						DefaultScopes: []string{"devstorage.read_only"},
					},
				},
			},
			profile: "ci",
			check: func(t *testing.T, result *config.GCPAuthConfig) {
				assert.Equal(t, "ci-secret", result.ClientSecret)
				assert.Equal(t, []string{"devstorage.read_only"}, result.DefaultScopes)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ResolveGCPConfig(tt.cfg, tt.profile)
			tt.check(t, result)
		})
	}
}
