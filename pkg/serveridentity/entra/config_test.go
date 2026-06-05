package entra

import (
	"context"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEntraIdentityFromConfig_NilConfig(t *testing.T) {
	_, err := NewEntraIdentityFromConfig(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "APIEntraIdentityConfig is required")
}

func TestNewEntraIdentityFromConfig_InvalidCredential(t *testing.T) {
	cfg := &config.APIEntraIdentityConfig{
		TenantID: "t",
		ClientID: "c",
		Credential: config.ServerCredentialConfig{
			Type: "invalid",
		},
	}

	_, err := NewEntraIdentityFromConfig(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credential")
}

func TestNewEntraIdentityFromConfig_SecretCredential(t *testing.T) {
	t.Setenv("TEST_CONFIG_SECRET", "resolved-secret")

	cfg := &config.APIEntraIdentityConfig{
		TenantID: "00000000-0000-0000-0000-000000000001",
		ClientID: "00000000-0000-0000-0000-000000000002",
		Credential: config.ServerCredentialConfig{
			Type:         "secret",
			ClientSecret: "env://TEST_CONFIG_SECRET",
		},
	}

	identity, err := NewEntraIdentityFromConfig(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "entra", identity.Name())
	assert.Equal(t, cfg.TenantID, identity.TenantID)
	assert.Equal(t, cfg.ClientID, identity.ClientID)
}

func TestNewEntraIdentityFromConfig_WIFCredential(t *testing.T) {
	cfg := &config.APIEntraIdentityConfig{
		TenantID: "00000000-0000-0000-0000-000000000001",
		ClientID: "00000000-0000-0000-0000-000000000002",
		Credential: config.ServerCredentialConfig{
			Type:         "wif",
			WIFTokenPath: "/var/run/secrets/azure/tokens/federated-token",
		},
	}

	identity, err := NewEntraIdentityFromConfig(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "entra", identity.Name())
}

func TestNewEntraIdentityFromConfig_SecretResolveFails(t *testing.T) {
	cfg := &config.APIEntraIdentityConfig{
		TenantID: "t",
		ClientID: "c",
		Credential: config.ServerCredentialConfig{
			Type:         "secret",
			ClientSecret: "env://NONEXISTENT_SECRET_VAR_XYZ",
		},
	}

	_, err := NewEntraIdentityFromConfig(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving client secret")
}

func TestNewEntraIdentityFromConfig_WithTokenManager(t *testing.T) {
	t.Setenv("TEST_CONFIG_SECRET_MGR", "secret")

	cfg := &config.APIEntraIdentityConfig{
		TenantID: "00000000-0000-0000-0000-000000000001",
		ClientID: "00000000-0000-0000-0000-000000000002",
		Credential: config.ServerCredentialConfig{
			Type:         "secret",
			ClientSecret: "env://TEST_CONFIG_SECRET_MGR",
		},
		TokenManager: &config.TokenManagerConfig{
			CacheSize:       256,
			ExpiryBuffer:    "5m",
			CleanupInterval: "2m",
		},
	}

	identity, err := NewEntraIdentityFromConfig(context.Background(), cfg)
	require.NoError(t, err)
	assert.NotNil(t, identity.manager)
}

func TestNewEntraIdentityFromConfig_InvalidTokenManagerDuration(t *testing.T) {
	t.Setenv("TEST_CONFIG_SECRET_DUR", "secret")

	cfg := &config.APIEntraIdentityConfig{
		TenantID: "t",
		ClientID: "c",
		Credential: config.ServerCredentialConfig{
			Type:         "secret",
			ClientSecret: "env://TEST_CONFIG_SECRET_DUR",
		},
		TokenManager: &config.TokenManagerConfig{
			ExpiryBuffer: "not-a-duration",
		},
	}

	_, err := NewEntraIdentityFromConfig(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token manager config")
}

func TestResolveTokenManagerDefaults_AllDefaults(t *testing.T) {
	s, err := resolveTokenManagerDefaults(&config.TokenManagerConfig{})

	require.NoError(t, err)
	assert.Equal(t, defaultCacheSize, s.CacheSize)
	assert.Equal(t, defaultExpiryBuffer, s.ExpiryBuffer)
	assert.Equal(t, defaultCleanupInterval, s.CleanupInterval)
	assert.Equal(t, defaultExpiryThreshold, s.ExpiryThreshold)
	assert.Equal(t, defaultSlowThreshold, s.SlowThreshold)
	assert.Equal(t, defaultRetryOnFollowerErrors, s.RetryOnError)
}

func TestResolveTokenManagerDefaults_CustomValues(t *testing.T) {
	retryFalse := false
	s, err := resolveTokenManagerDefaults(&config.TokenManagerConfig{
		CacheSize:            256,
		ExpiryBuffer:         "3m",
		CleanupInterval:      "1m",
		ExpiryThreshold:      "15m",
		SlowThreshold:        "250ms",
		RetryFollowerOnError: &retryFalse,
	})

	require.NoError(t, err)
	assert.Equal(t, 256, s.CacheSize)
	assert.Equal(t, 3*time.Minute, s.ExpiryBuffer)
	assert.Equal(t, 1*time.Minute, s.CleanupInterval)
	assert.Equal(t, 15*time.Minute, s.ExpiryThreshold)
	assert.Equal(t, 250*time.Millisecond, s.SlowThreshold)
	assert.False(t, s.RetryOnError)
}

func TestResolveTokenManagerDefaults_InvalidDurations(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.TokenManagerConfig
	}{
		{"bad expiryBuffer", &config.TokenManagerConfig{ExpiryBuffer: "xyz"}},
		{"bad cleanupInterval", &config.TokenManagerConfig{CleanupInterval: "xyz"}},
		{"bad expiryThreshold", &config.TokenManagerConfig{ExpiryThreshold: "xyz"}},
		{"bad slowThreshold", &config.TokenManagerConfig{SlowThreshold: "xyz"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveTokenManagerDefaults(tc.cfg)
			require.Error(t, err)
		})
	}
}
