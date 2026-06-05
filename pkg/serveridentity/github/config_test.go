// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider/callerscope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGitHubIdentityFromConfig_Success(t *testing.T) {
	key := generateTestKey(t)
	pemKey := encodePrivateKeyToPEM(t, key)

	// Set env var for SecretRef resolution.
	t.Setenv("TEST_GH_PRIVATE_KEY", string(pemKey))

	cfg := &config.APIGitHubIdentityConfig{
		Credential: config.GitHubCredentialConfig{
			Type: "app",
			App: &config.GitHubAppCredentialConfig{
				ClientID:       "Iv23li8abc123",
				InstallationID: 67890,
				PrivateKey:     config.SecretRef("env://TEST_GH_PRIVATE_KEY"),
			},
		},
		Hostname: "github.example.com",
	}

	ctx := context.Background()
	identity, err := NewGitHubIdentityFromConfig(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, identity)
	assert.Equal(t, "github", identity.Name())

	app, ok := identity.(*App)
	require.True(t, ok)
	assert.Equal(t, "Iv23li8abc123", app.ClientID)
	assert.Equal(t, int64(67890), app.InstallationID)
	assert.Equal(t, "github.example.com", app.Hostname)
}

func TestNewGitHubIdentityFromConfig_DefaultHostname(t *testing.T) {
	key := generateTestKey(t)
	pemKey := encodePrivateKeyToPEM(t, key)

	t.Setenv("TEST_GH_PRIVATE_KEY_2", string(pemKey))

	cfg := &config.APIGitHubIdentityConfig{
		Credential: config.GitHubCredentialConfig{
			Type: "app",
			App: &config.GitHubAppCredentialConfig{
				ClientID:       "Iv23li8abc123",
				InstallationID: 2,
				PrivateKey:     config.SecretRef("env://TEST_GH_PRIVATE_KEY_2"),
			},
		},
	}

	ctx := context.Background()
	identity, err := NewGitHubIdentityFromConfig(ctx, cfg)
	require.NoError(t, err)

	app, ok := identity.(*App)
	require.True(t, ok)
	assert.Equal(t, "github.com", app.Hostname)
}

func TestNewGitHubIdentityFromConfig_NilConfig(t *testing.T) {
	ctx := context.Background()
	_, err := NewGitHubIdentityFromConfig(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "APIGitHubIdentityConfig is required")
}

func TestNewGitHubIdentityFromConfig_InvalidConfig(t *testing.T) {
	ctx := context.Background()
	cfg := &config.APIGitHubIdentityConfig{
		Credential: config.GitHubCredentialConfig{
			Type: "unknown",
		},
	}

	_, err := NewGitHubIdentityFromConfig(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validating config")
}

func TestNewGitHubIdentityFromConfig_InvalidPrivateKey(t *testing.T) {
	t.Setenv("TEST_GH_BAD_KEY", "not-a-pem-key")

	cfg := &config.APIGitHubIdentityConfig{
		Credential: config.GitHubCredentialConfig{
			Type: "app",
			App: &config.GitHubAppCredentialConfig{
				ClientID:       "Iv23li8abc123",
				InstallationID: 2,
				PrivateKey:     config.SecretRef("env://TEST_GH_BAD_KEY"),
			},
		},
	}

	ctx := context.Background()
	_, err := NewGitHubIdentityFromConfig(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing private key")
}

func TestNewGitHubIdentityFromConfig_UnresolvableSecret(t *testing.T) {
	// Unset the env var to ensure resolution fails.
	os.Unsetenv("TEST_GH_NONEXISTENT_KEY")

	cfg := &config.APIGitHubIdentityConfig{
		Credential: config.GitHubCredentialConfig{
			Type: "app",
			App: &config.GitHubAppCredentialConfig{
				ClientID:       "Iv23li8abc123",
				InstallationID: 2,
				PrivateKey:     config.SecretRef("env://TEST_GH_NONEXISTENT_KEY"),
			},
		},
	}

	ctx := context.Background()
	_, err := NewGitHubIdentityFromConfig(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving private key")
}

func TestNewGitHubIdentityFromConfig_WithTokenManager(t *testing.T) {
	key := generateTestKey(t)
	pemKey := encodePrivateKeyToPEM(t, key)

	t.Setenv("TEST_GH_KEY_TM", string(pemKey))

	cfg := &config.APIGitHubIdentityConfig{
		Credential: config.GitHubCredentialConfig{
			Type: "app",
			App: &config.GitHubAppCredentialConfig{
				ClientID:       "Iv23li8abc123",
				InstallationID: 2,
				PrivateKey:     config.SecretRef("env://TEST_GH_KEY_TM"),
			},
		},
		TokenManager: &config.TokenManagerConfig{
			CacheSize:    512,
			ExpiryBuffer: "2m",
		},
	}

	ctx := context.Background()
	identity, err := NewGitHubIdentityFromConfig(ctx, cfg)
	require.NoError(t, err)
	assert.NotNil(t, identity)

	app, ok := identity.(*App)
	require.True(t, ok)
	assert.NotNil(t, app.manager)
}

func TestNewGitHubIdentityFromConfig_InvalidTokenManagerDuration(t *testing.T) {
	key := generateTestKey(t)
	pemKey := encodePrivateKeyToPEM(t, key)

	t.Setenv("TEST_GH_KEY_TM_BAD", string(pemKey))

	cfg := &config.APIGitHubIdentityConfig{
		Credential: config.GitHubCredentialConfig{
			Type: "app",
			App: &config.GitHubAppCredentialConfig{
				ClientID:       "Iv23li8abc123",
				InstallationID: 2,
				PrivateKey:     config.SecretRef("env://TEST_GH_KEY_TM_BAD"),
			},
		},
		TokenManager: &config.TokenManagerConfig{
			ExpiryBuffer: "not-a-duration",
		},
	}

	ctx := context.Background()
	_, err := NewGitHubIdentityFromConfig(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token manager config")
}

func TestNewGitHubIdentityFromConfig_PAT_Success(t *testing.T) {
	t.Setenv("TEST_GH_PAT", "ghp_abc123token")

	cfg := &config.APIGitHubIdentityConfig{
		Credential: config.GitHubCredentialConfig{
			Type: "pat",
			PAT: &config.GitHubPATCredentialConfig{
				Token: config.SecretRef("env://TEST_GH_PAT"),
			},
		},
	}

	ctx := context.Background()
	identity, err := NewGitHubIdentityFromConfig(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, identity)
	assert.Equal(t, "github", identity.Name())

	token, err := identity.GetToken(ctx, tokenprovider.RequestOptions{Caller: callerscope.ServerCaller})
	require.NoError(t, err)
	assert.Equal(t, "ghp_abc123token", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.True(t, token.ExpiresAt.IsZero())
}

func TestNewGitHubIdentityFromConfig_PAT_UnresolvableSecret(t *testing.T) {
	os.Unsetenv("TEST_GH_PAT_MISSING")

	cfg := &config.APIGitHubIdentityConfig{
		Credential: config.GitHubCredentialConfig{
			Type: "pat",
			PAT: &config.GitHubPATCredentialConfig{
				Token: config.SecretRef("env://TEST_GH_PAT_MISSING"),
			},
		},
	}

	ctx := context.Background()
	_, err := NewGitHubIdentityFromConfig(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving PAT token")
}

func TestParsePrivateKey_PKCS1(t *testing.T) {
	key := generateTestKey(t)
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	parsed, err := parsePrivateKey(pemData)
	require.NoError(t, err)
	assert.NotNil(t, parsed)
}

func TestParsePrivateKey_PKCS8(t *testing.T) {
	key := generateTestKey(t)
	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8Bytes,
	})

	parsed, err := parsePrivateKey(pemData)
	require.NoError(t, err)
	assert.NotNil(t, parsed)
}

func TestParsePrivateKey_InvalidPEM(t *testing.T) {
	_, err := parsePrivateKey([]byte("not-pem-data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no PEM block found")
}

func TestParsePrivateKey_InvalidKeyData(t *testing.T) {
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: []byte("invalid-key-data"),
	})

	_, err := parsePrivateKey(pemData)
	require.Error(t, err)
}

func TestResolveTokenManagerDefaults(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.TokenManagerConfig
		wantErr string
	}{
		{
			name: "all defaults",
			cfg:  &config.TokenManagerConfig{},
		},
		{
			name: "custom cache size",
			cfg:  &config.TokenManagerConfig{CacheSize: 100},
		},
		{
			name: "custom durations",
			cfg: &config.TokenManagerConfig{
				ExpiryBuffer:    "1m",
				CleanupInterval: "2m",
				ExpiryThreshold: "3m",
				SlowThreshold:   "100ms",
			},
		},
		{
			name:    "invalid expiry buffer",
			cfg:     &config.TokenManagerConfig{ExpiryBuffer: "xyz"},
			wantErr: "parsing expiryBuffer",
		},
		{
			name:    "invalid cleanup interval",
			cfg:     &config.TokenManagerConfig{CleanupInterval: "xyz"},
			wantErr: "parsing cleanupInterval",
		},
		{
			name:    "invalid expiry threshold",
			cfg:     &config.TokenManagerConfig{ExpiryThreshold: "xyz"},
			wantErr: "parsing expiryThreshold",
		},
		{
			name:    "invalid slow threshold",
			cfg:     &config.TokenManagerConfig{SlowThreshold: "xyz"},
			wantErr: "parsing slowThreshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := resolveTokenManagerDefaults(tt.cfg)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Greater(t, s.CacheSize, 0)
				assert.Greater(t, s.ExpiryBuffer, time.Duration(0))
			}
		})
	}
}

func encodePrivateKeyToPEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}
