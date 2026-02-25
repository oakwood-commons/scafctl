// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateTestRSAKey generates a test RSA private key and returns the PEM bytes.
func generateTestRSAKey(t *testing.T) ([]byte, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	keyBytes := x509.MarshalPKCS1PrivateKey(key)
	pemBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: keyBytes,
	}
	pemBytes := pem.EncodeToMemory(pemBlock)
	return pemBytes, key
}

// generateTestPKCS8Key generates a test RSA private key in PKCS#8 format.
func generateTestPKCS8Key(t *testing.T) ([]byte, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	}
	pemBytes := pem.EncodeToMemory(pemBlock)
	return pemBytes, key
}

func TestCreateAppJWT(t *testing.T) {
	_, key := generateTestRSAKey(t)

	tokenStr, err := createAppJWT(12345, key)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenStr)

	// Parse and validate the JWT using the library
	parsed, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		// Verify signing method is RS256
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return &key.PublicKey, nil
	})
	require.NoError(t, err)
	assert.True(t, parsed.Valid)

	// Verify claims
	iss, err := parsed.Claims.GetIssuer()
	require.NoError(t, err)
	assert.Equal(t, strconv.FormatInt(12345, 10), iss)

	exp, err := parsed.Claims.GetExpirationTime()
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(10*time.Minute), exp.Time, 2*time.Second)

	iat, err := parsed.Claims.GetIssuedAt()
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(-60*time.Second), iat.Time, 2*time.Second)
}

func TestParseRSAPrivateKey_PKCS1(t *testing.T) {
	pemBytes, _ := generateTestRSAKey(t)

	key, err := parseRSAPrivateKey(pemBytes)
	require.NoError(t, err)
	assert.NotNil(t, key)
}

func TestParseRSAPrivateKey_PKCS8(t *testing.T) {
	pemBytes, _ := generateTestPKCS8Key(t)

	key, err := parseRSAPrivateKey(pemBytes)
	require.NoError(t, err)
	assert.NotNil(t, key)
}

func TestParseRSAPrivateKey_InvalidPEM(t *testing.T) {
	_, err := parseRSAPrivateKey([]byte("not a pem"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse RSA private key")
}

func TestParseRSAPrivateKey_UnsupportedType(t *testing.T) {
	block := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: []byte("fake"),
	}
	_, err := parseRSAPrivateKey(pem.EncodeToMemory(block))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse RSA private key")
}

func TestHandler_AppLogin(t *testing.T) {
	pemBytes, _ := generateTestRSAKey(t)

	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
		WithConfig(&Config{
			AppID:          12345,
			InstallationID: 67890,
			PrivateKey:     string(pemBytes),
		}),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// Mock: GET /app
	mockHTTP.AddResponse(http.StatusOK, &AppInfo{
		ID:   12345,
		Slug: "my-test-app",
		Name: "My Test App",
		Owner: struct {
			Login string `json:"login"`
			ID    int64  `json:"id"`
		}{Login: "my-org", ID: 1},
	})

	// Mock: POST /app/installations/{id}/access_tokens
	mockHTTP.AddResponse(http.StatusCreated, &InstallationTokenResponse{
		Token:     "ghs_installation_token_123",
		ExpiresAt: time.Now().Add(1 * time.Hour),
		Permissions: map[string]string{
			"contents": "read",
			"metadata": "read",
		},
	})

	result, err := handler.Login(ctx, auth.LoginOptions{
		Flow: auth.FlowGitHubApp,
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "app/my-test-app", result.Claims.Subject)
	assert.Equal(t, "My Test App", result.Claims.Name)
	assert.Equal(t, "my-test-app", result.Claims.Username)
	assert.Equal(t, "12345", result.Claims.ObjectID)
	assert.False(t, result.ExpiresAt.IsZero())

	// Verify stored access token
	storedToken, err := store.Get(ctx, SecretKeyAccessToken)
	require.NoError(t, err)
	assert.Equal(t, "ghs_installation_token_123", string(storedToken))

	// Verify metadata
	metaBytes, err := store.Get(ctx, SecretKeyMetadata)
	require.NoError(t, err)
	var metadata TokenMetadata
	err = json.Unmarshal(metaBytes, &metadata)
	require.NoError(t, err)
	assert.Equal(t, "app/my-test-app", metadata.Claims.Subject)
	assert.Equal(t, string(auth.IdentityTypeServicePrincipal), metadata.IdentityType)

	// Verify requests
	reqs := mockHTTP.GetRequests()
	require.Len(t, reqs, 2)
	assert.Equal(t, "GET", reqs[0].Method)
	assert.Contains(t, reqs[0].Endpoint, "/app")
	assert.Equal(t, "POST", reqs[1].Method)
	assert.Contains(t, reqs[1].Endpoint, "/app/installations/67890/access_tokens")
}

func TestHandler_AppLogin_MissingAppID(t *testing.T) {
	pemBytes, _ := generateTestRSAKey(t)

	store := secrets.NewMockStore()
	handler, err := New(
		WithSecretStore(store),
		WithConfig(&Config{
			InstallationID: 67890,
			PrivateKey:     string(pemBytes),
		}),
	)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.Login(ctx, auth.LoginOptions{
		Flow: auth.FlowGitHubApp,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "app ID is required")
}

func TestHandler_AppLogin_MissingInstallationID(t *testing.T) {
	pemBytes, _ := generateTestRSAKey(t)

	store := secrets.NewMockStore()
	handler, err := New(
		WithSecretStore(store),
		WithConfig(&Config{
			AppID:      12345,
			PrivateKey: string(pemBytes),
		}),
	)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.Login(ctx, auth.LoginOptions{
		Flow: auth.FlowGitHubApp,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "installation ID is required")
}

func TestHandler_AppLogin_MissingPrivateKey(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(
		WithSecretStore(store),
		WithConfig(&Config{
			AppID:          12345,
			InstallationID: 67890,
		}),
	)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.Login(ctx, auth.LoginOptions{
		Flow: auth.FlowGitHubApp,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no private key configured")
}

func TestHandler_AppLogin_InvalidAppResponse(t *testing.T) {
	pemBytes, _ := generateTestRSAKey(t)

	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
		WithConfig(&Config{
			AppID:          12345,
			InstallationID: 67890,
			PrivateKey:     string(pemBytes),
		}),
	)
	require.NoError(t, err)

	// Mock: GET /app returns 401 (bad JWT)
	mockHTTP.AddResponse(http.StatusUnauthorized, map[string]any{
		"message": "A JSON web token could not be decoded",
	})

	ctx := context.Background()
	_, err = handler.Login(ctx, auth.LoginOptions{
		Flow: auth.FlowGitHubApp,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
}

func TestHandler_AppLogin_InvalidInstallationResponse(t *testing.T) {
	pemBytes, _ := generateTestRSAKey(t)

	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
		WithConfig(&Config{
			AppID:          12345,
			InstallationID: 99999,
			PrivateKey:     string(pemBytes),
		}),
	)
	require.NoError(t, err)

	// Mock: GET /app succeeds
	mockHTTP.AddResponse(http.StatusOK, &AppInfo{
		ID:   12345,
		Slug: "my-app",
		Name: "My App",
	})

	// Mock: POST /app/installations/{id}/access_tokens returns 404
	mockHTTP.AddResponse(http.StatusNotFound, map[string]any{
		"message": "Not Found",
	})

	ctx := context.Background()
	_, err = handler.Login(ctx, auth.LoginOptions{
		Flow: auth.FlowGitHubApp,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 404")
}

func TestConfig_GetPrivateKey_Inline(t *testing.T) {
	cfg := &Config{
		PrivateKey: "-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----",
	}

	ctx := context.Background()
	key, err := cfg.GetPrivateKey(ctx, nil)
	require.NoError(t, err)
	assert.Contains(t, string(key), "RSA PRIVATE KEY")
}

func TestConfig_GetPrivateKey_EnvVar(t *testing.T) {
	pemContent := "-----BEGIN RSA PRIVATE KEY-----\nenvtest\n-----END RSA PRIVATE KEY-----"
	t.Setenv(EnvGitHubAppPrivateKey, pemContent)

	cfg := &Config{}
	ctx := context.Background()
	key, err := cfg.GetPrivateKey(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, pemContent, string(key))
}

func TestConfig_GetPrivateKey_FilePath(t *testing.T) {
	pemContent := "-----BEGIN RSA PRIVATE KEY-----\nfiletest\n-----END RSA PRIVATE KEY-----"
	tmpFile := filepath.Join(t.TempDir(), "test-key.pem")
	err := os.WriteFile(tmpFile, []byte(pemContent), 0o600)
	require.NoError(t, err)

	cfg := &Config{
		PrivateKeyPath: tmpFile,
	}

	ctx := context.Background()
	key, err := cfg.GetPrivateKey(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, pemContent, string(key))
}

func TestConfig_GetPrivateKey_SecretStore(t *testing.T) {
	pemContent := "-----BEGIN RSA PRIVATE KEY-----\nsecrettest\n-----END RSA PRIVATE KEY-----"
	store := secrets.NewMockStore()
	ctx := context.Background()
	err := store.Set(ctx, "my-github-app-key", []byte(pemContent))
	require.NoError(t, err)

	cfg := &Config{
		PrivateKeySecretName: "my-github-app-key",
	}

	key, err := cfg.GetPrivateKey(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, pemContent, string(key))
}

func TestConfig_GetPrivateKey_NoneConfigured(t *testing.T) {
	cfg := &Config{}
	ctx := context.Background()
	_, err := cfg.GetPrivateKey(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no private key configured")
}

func TestConfig_GetAppID_FromConfig(t *testing.T) {
	cfg := &Config{AppID: 42}
	assert.Equal(t, int64(42), cfg.GetAppID())
}

func TestConfig_GetAppID_FromEnv(t *testing.T) {
	t.Setenv(EnvGitHubAppID, "99999")
	cfg := &Config{}
	assert.Equal(t, int64(99999), cfg.GetAppID())
}

func TestConfig_GetAppID_ConfigOverridesEnv(t *testing.T) {
	t.Setenv(EnvGitHubAppID, "99999")
	cfg := &Config{AppID: 42}
	assert.Equal(t, int64(42), cfg.GetAppID())
}

func TestConfig_GetInstallationID_FromConfig(t *testing.T) {
	cfg := &Config{InstallationID: 555}
	assert.Equal(t, int64(555), cfg.GetInstallationID())
}

func TestConfig_GetInstallationID_FromEnv(t *testing.T) {
	t.Setenv(EnvGitHubAppInstallationID, "777")
	cfg := &Config{}
	assert.Equal(t, int64(777), cfg.GetInstallationID())
}

func TestConfig_ValidateAppConfig_Success(t *testing.T) {
	pemBytes, _ := generateTestRSAKey(t)
	cfg := &Config{
		AppID:          12345,
		InstallationID: 67890,
		PrivateKey:     string(pemBytes),
	}
	ctx := context.Background()
	assert.NoError(t, cfg.ValidateAppConfig(ctx, nil))
}

func TestConfig_ValidateAppConfig_MissingAppID(t *testing.T) {
	pemBytes, _ := generateTestRSAKey(t)
	cfg := &Config{
		InstallationID: 67890,
		PrivateKey:     string(pemBytes),
	}
	ctx := context.Background()
	err := cfg.ValidateAppConfig(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "app ID is required")
}

func TestConfig_ValidateAppConfig_MissingInstallationID(t *testing.T) {
	pemBytes, _ := generateTestRSAKey(t)
	cfg := &Config{
		AppID:      12345,
		PrivateKey: string(pemBytes),
	}
	ctx := context.Background()
	err := cfg.ValidateAppConfig(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "installation ID is required")
}
