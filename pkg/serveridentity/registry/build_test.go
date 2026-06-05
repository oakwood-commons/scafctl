package registry

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenProviderRegistry_NilEntra(t *testing.T) {
	lgr := logr.Discard()
	cfg := &config.APIServerConfig{}

	reg, err := TokenProviderRegistry(context.Background(), cfg, &lgr)

	require.NoError(t, err)
	assert.Nil(t, reg)
}

func TestTokenProviderRegistry_SecretCredential(t *testing.T) {
	t.Setenv("TEST_ENTRA_SECRET", "my-secret-value")

	lgr := logr.Discard()
	cfg := &config.APIServerConfig{
		Identity: config.APIIdentityConfig{
			Entra: &config.APIEntraIdentityConfig{
				TenantID: "00000000-0000-0000-0000-000000000001",
				ClientID: "00000000-0000-0000-0000-000000000002",
				Credential: config.ServerCredentialConfig{
					Type:         "secret",
					ClientSecret: "env://TEST_ENTRA_SECRET",
				},
			},
		},
	}

	reg, err := TokenProviderRegistry(context.Background(), cfg, &lgr)

	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, []string{"entra"}, reg.Names())
}

func TestTokenProviderRegistry_WIFCredential(t *testing.T) {
	lgr := logr.Discard()
	cfg := &config.APIServerConfig{
		Identity: config.APIIdentityConfig{
			Entra: &config.APIEntraIdentityConfig{
				TenantID: "00000000-0000-0000-0000-000000000001",
				ClientID: "00000000-0000-0000-0000-000000000002",
				Credential: config.ServerCredentialConfig{
					Type:         "wif",
					WIFTokenPath: "/var/run/secrets/azure/tokens/federated-token",
				},
			},
		},
	}

	reg, err := TokenProviderRegistry(context.Background(), cfg, &lgr)

	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, []string{"entra"}, reg.Names())
}

func TestTokenProviderRegistry_WithTokenManager(t *testing.T) {
	t.Setenv("TEST_ENTRA_SECRET_MGR", "my-secret")

	lgr := logr.Discard()
	cfg := &config.APIServerConfig{
		Identity: config.APIIdentityConfig{
			Entra: &config.APIEntraIdentityConfig{
				TenantID: "00000000-0000-0000-0000-000000000001",
				ClientID: "00000000-0000-0000-0000-000000000002",
				Credential: config.ServerCredentialConfig{
					Type:         "secret",
					ClientSecret: "env://TEST_ENTRA_SECRET_MGR",
				},
				TokenManager: &config.TokenManagerConfig{
					CacheSize:       512,
					ExpiryBuffer:    "5m",
					CleanupInterval: "2m",
				},
			},
		},
	}

	reg, err := TokenProviderRegistry(context.Background(), cfg, &lgr)

	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, []string{"entra"}, reg.Names())
}

func TestTokenProviderRegistry_InvalidCredentialValidation(t *testing.T) {
	lgr := logr.Discard()
	cfg := &config.APIServerConfig{
		Identity: config.APIIdentityConfig{
			Entra: &config.APIEntraIdentityConfig{
				TenantID: "00000000-0000-0000-0000-000000000001",
				ClientID: "00000000-0000-0000-0000-000000000002",
				Credential: config.ServerCredentialConfig{
					Type: "invalid",
				},
			},
		},
	}

	_, err := TokenProviderRegistry(context.Background(), cfg, &lgr)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "credential")
}

func TestTokenProviderRegistry_SecretResolveFails(t *testing.T) {
	lgr := logr.Discard()
	cfg := &config.APIServerConfig{
		Identity: config.APIIdentityConfig{
			Entra: &config.APIEntraIdentityConfig{
				TenantID: "00000000-0000-0000-0000-000000000001",
				ClientID: "00000000-0000-0000-0000-000000000002",
				Credential: config.ServerCredentialConfig{
					Type:         "secret",
					ClientSecret: "env://NONEXISTENT_VAR_FOR_TEST_XYZ",
				},
			},
		},
	}

	_, err := TokenProviderRegistry(context.Background(), cfg, &lgr)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving client secret")
}

func TestTokenProviderRegistry_InvalidTokenManagerDuration(t *testing.T) {
	t.Setenv("TEST_ENTRA_SECRET_DUR", "secret-val")

	lgr := logr.Discard()
	cfg := &config.APIServerConfig{
		Identity: config.APIIdentityConfig{
			Entra: &config.APIEntraIdentityConfig{
				TenantID: "00000000-0000-0000-0000-000000000001",
				ClientID: "00000000-0000-0000-0000-000000000002",
				Credential: config.ServerCredentialConfig{
					Type:         "secret",
					ClientSecret: "env://TEST_ENTRA_SECRET_DUR",
				},
				TokenManager: &config.TokenManagerConfig{
					ExpiryBuffer: "not-a-duration",
				},
			},
		},
	}

	_, err := TokenProviderRegistry(context.Background(), cfg, &lgr)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "token manager config")
}
