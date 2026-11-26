// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitAuthConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *GitAuthConfig
		wantErr string
	}{
		{
			name: "nil config is valid",
			cfg:  nil,
		},
		{
			name: "valid rules with pathPrefix, profile, scope, and username",
			cfg: &GitAuthConfig{
				Rules: []GitCredentialRule{
					{
						Host:       "github.com",
						PathPrefix: "work-org",
						Handler:    "github",
						Profile:    "work",
						Username:   "x-access-token",
					},
					{
						Host:    "127.0.0.1:8443",
						Handler: "git-local",
						Profile: "built-in",
						Scope:   "repo:write",
					},
					{
						Host:    "[::1]:8443",
						Handler: "git-ipv6",
					},
				},
			},
		},
		{
			name: "exceeds max rules",
			cfg: &GitAuthConfig{
				Rules: make([]GitCredentialRule, MaxGitCredentialRules+1),
			},
			wantErr: "exceed the maximum",
		},
		{
			name: "empty host",
			cfg: &GitAuthConfig{
				Rules: []GitCredentialRule{{Host: "", Handler: "github"}},
			},
			wantErr: "host: must not be empty",
		},
		{
			name: "host with scheme rejected",
			cfg: &GitAuthConfig{
				Rules: []GitCredentialRule{{Host: "https://github.com", Handler: "github"}},
			},
			wantErr: "bare host without scheme",
		},
		{
			name: "host with path rejected",
			cfg: &GitAuthConfig{
				Rules: []GitCredentialRule{{Host: "github.com/org", Handler: "github"}},
			},
			wantErr: "must not contain path",
		},
		{
			name: "host with invalid port rejected",
			cfg: &GitAuthConfig{
				Rules: []GitCredentialRule{{Host: "github.com:99999", Handler: "github"}},
			},
			wantErr: "port",
		},
		{
			name: "pathPrefix with traversal rejected",
			cfg: &GitAuthConfig{
				Rules: []GitCredentialRule{{Host: "github.com", PathPrefix: "work-org/../secret", Handler: "github"}},
			},
			wantErr: "traversal",
		},
		{
			name: "pathPrefix with only slashes rejected",
			cfg: &GitAuthConfig{
				Rules: []GitCredentialRule{{Host: "github.com", PathPrefix: "///", Handler: "github"}},
			},
			wantErr: "at least one path segment",
		},
		{
			name: "missing handler rejected",
			cfg: &GitAuthConfig{
				Rules: []GitCredentialRule{{Host: "github.com", Handler: ""}},
			},
			wantErr: "handler: must not be empty",
		},
		{
			name: "invalid profile rejected",
			cfg: &GitAuthConfig{
				Rules: []GitCredentialRule{{Host: "github.com", Handler: "github", Profile: "-bad"}},
			},
			wantErr: "profile:",
		},
		{
			name: "control character in username rejected",
			cfg: &GitAuthConfig{
				Rules: []GitCredentialRule{{Host: "github.com", Handler: "github", Username: "bad\nline"}},
			},
			wantErr: "username: must not contain control characters",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestCustomOAuth2Config_GitFieldsAndSanitize(t *testing.T) {
	t.Parallel()

	valid := CustomOAuth2Config{
		Name:         "gitlab-corp",
		AuthorizeURL: "https://git.example.com/oauth/authorize",
		TokenURL:     "https://git.example.com/oauth/token",
		ClientID:     "client-123",
		ClientSecret: "secret://gitlab-secret",
		GitHost:      "git.example.com",
		GitUsername:  "oauth2",
	}
	require.NoError(t, valid.ValidateCredentials())

	invalid := valid
	invalid.GitHost = "https://git.example.com"
	err := invalid.ValidateCredentials()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitHost")

	cfg := &Config{
		Version: 1,
		Auth: GlobalAuthConfig{
			CustomOAuth2: []CustomOAuth2Config{valid},
			Git: &GitAuthConfig{
				AllowInsecureHTTP: false,
				Rules: []GitCredentialRule{
					{
						Host:       "github.com",
						PathPrefix: "work-org",
						Handler:    "github",
						Profile:    "work",
					},
				},
			},
		},
	}
	require.NoError(t, cfg.Validate())

	sanitized := SanitizeConfig(cfg)
	require.Len(t, sanitized.Auth.CustomOAuth2, 1)
	assert.Equal(t, "git.example.com", sanitized.Auth.CustomOAuth2[0].GitHost)
	assert.Equal(t, "oauth2", sanitized.Auth.CustomOAuth2[0].GitUsername)
	assert.Equal(t, RedactedValue, sanitized.Auth.CustomOAuth2[0].ClientSecret)
	require.NotNil(t, sanitized.Auth.Git)
	require.Len(t, sanitized.Auth.Git.Rules, 1)
	assert.Equal(t, "work-org", sanitized.Auth.Git.Rules[0].PathPrefix)
}

func TestManagerLoad_AuthGitRules(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `version: 1
auth:
  git:
    rules:
      - host: github.com
        pathPrefix: work-org
        handler: github
        profile: work
      - host: github.com
        pathPrefix: public-org
        handler: github
        profile: personal
  customOAuth2:
    - name: acme-git
      tokenURL: https://git.example.com/oauth/token
      clientID: cli-app
      clientSecret: env://ACME_SECRET
      defaultFlow: client_credentials
      gitHost: git.example.com
      gitUsername: oauth2
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0o600))

	mgr := NewManager(cfgPath)
	loaded, err := mgr.Load()
	require.NoError(t, err)
	assert.Empty(t, mgr.GetUnknownKeys())
	require.NotNil(t, loaded.Auth.Git)
	require.Len(t, loaded.Auth.Git.Rules, 2)
	assert.Equal(t, "work-org", loaded.Auth.Git.Rules[0].PathPrefix)
	assert.Equal(t, "work", loaded.Auth.Git.Rules[0].Profile)
	require.Len(t, loaded.Auth.CustomOAuth2, 1)
	assert.Equal(t, "git.example.com", loaded.Auth.CustomOAuth2[0].GitHost)
	assert.Equal(t, "oauth2", loaded.Auth.CustomOAuth2[0].GitUsername)
}

func TestManagerSetAndEnv_AuthGitAllowInsecureHTTP(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("version: 1\n"), 0o600))

	mgr := NewManager(cfgPath)
	_, err := mgr.Load()
	require.NoError(t, err)

	mgr.Set("auth.git.allowInsecureHttp", "true")
	require.NoError(t, mgr.Save())

	reloadedMgr := NewManager(cfgPath)
	reloaded, err := reloadedMgr.Load()
	require.NoError(t, err)
	require.NotNil(t, reloaded.Auth.Git)
	assert.True(t, reloaded.Auth.Git.AllowInsecureHTTP)

	// Verify env var override binding works without a config file entry.
	envCfgPath := filepath.Join(tmpDir, "empty-config.yaml")
	require.NoError(t, os.WriteFile(envCfgPath, []byte("version: 1\n"), 0o600))
	t.Setenv("SCAFCTL_AUTH_GIT_ALLOWINSECUREHTTP", "true")
	envMgr := NewManager(envCfgPath)
	envLoaded, err := envMgr.Load()
	require.NoError(t, err)
	require.NotNil(t, envLoaded.Auth.Git)
	assert.True(t, envLoaded.Auth.Git.AllowInsecureHTTP)
}
