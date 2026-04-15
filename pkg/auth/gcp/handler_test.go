// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_DefaultConfig(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)
	require.NotNil(t, handler)

	assert.Equal(t, HandlerName, handler.Name())
	assert.Equal(t, HandlerDisplayName, handler.DisplayName())
}

func TestNew_WithConfig(t *testing.T) {
	store := secrets.NewMockStore()
	cfg := &Config{
		ClientID:                  "custom-client-id",
		ClientSecret:              "custom-secret",
		ImpersonateServiceAccount: "sa@test.iam.gserviceaccount.com",
		Project:                   "my-project",
		DefaultScopes:             []string{"custom-scope"},
	}

	handler, err := New(WithSecretStore(store), WithConfig(cfg))
	require.NoError(t, err)
	require.NotNil(t, handler)

	assert.Equal(t, "custom-client-id", handler.config.ClientID)
	assert.Equal(t, "custom-secret", handler.config.ClientSecret)
	assert.Equal(t, "sa@test.iam.gserviceaccount.com", handler.config.ImpersonateServiceAccount)
	assert.Equal(t, "my-project", handler.config.Project)
	assert.Equal(t, []string{"custom-scope"}, handler.config.DefaultScopes)
}

func TestNew_NilConfig(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store), WithConfig(nil))
	require.NoError(t, err)
	require.NotNil(t, handler)
	// Should keep default config
	assert.Equal(t, DefaultConfig().DefaultScopes, handler.config.DefaultScopes)
}

func TestNew_WithHTTPClient(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)
	require.NotNil(t, handler)
	assert.Equal(t, mockHTTP, handler.httpClient)
}

func TestNew_DeferredSecretError(t *testing.T) {
	// When no store is provided and default store creation fails,
	// the handler should still be created with a deferred error
	handler, err := New()
	require.NoError(t, err) // New always succeeds
	require.NotNil(t, handler)

	// Metadata operations should work
	assert.Equal(t, HandlerName, handler.Name())
	assert.Equal(t, HandlerDisplayName, handler.DisplayName())
}

func TestHandler_Name(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	assert.Equal(t, "gcp", handler.Name())
}

func TestHandler_DisplayName(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	assert.Equal(t, "Google Cloud Platform", handler.DisplayName())
}

func TestHandler_SupportedFlows(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	flows := handler.SupportedFlows()
	assert.Contains(t, flows, auth.FlowInteractive)
	assert.Contains(t, flows, auth.FlowServicePrincipal)
	assert.Contains(t, flows, auth.FlowWorkloadIdentity)
	assert.Contains(t, flows, auth.FlowMetadata)
	assert.Contains(t, flows, auth.FlowGcloudADC)
	assert.Len(t, flows, 5)
}

func TestHandler_Capabilities(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	caps := handler.Capabilities()
	assert.Contains(t, caps, auth.CapScopesOnLogin)
	assert.Contains(t, caps, auth.CapScopesOnTokenRequest)
	assert.Contains(t, caps, auth.CapFederatedToken)
}

func TestHandler_Status_NotAuthenticated(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	// Clear any environment-based credentials that could interfere
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")
	// Point gcloud ADC to a nonexistent directory to prevent local fallback
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())

	status, err := handler.Status(ctx)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.False(t, status.Authenticated)
}

func TestHandler_Status_WithStoredMetadata(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	// Clear any environment-based credentials
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")

	// Store metadata
	metadata := &TokenMetadata{
		Claims: &auth.Claims{
			Email:   "user@example.com",
			Name:    "Test User",
			Subject: "12345",
			Issuer:  "https://accounts.google.com",
		},
		Flow:   auth.FlowInteractive,
		Scopes: []string{"openid", "email"},
	}
	metadataBytes, err := json.Marshal(metadata)
	require.NoError(t, err)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	status, err := handler.Status(ctx)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.True(t, status.Authenticated)
	assert.Equal(t, "user@example.com", status.Claims.Email)
	assert.Equal(t, "Test User", status.Claims.Name)
	assert.Equal(t, auth.IdentityTypeUser, status.IdentityType)
}

func TestHandler_Status_ServicePrincipalMetadata(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")

	metadata := &TokenMetadata{
		Claims: &auth.Claims{
			Email:   "sa@project.iam.gserviceaccount.com",
			Subject: "sa-id",
			Issuer:  "https://accounts.google.com",
		},
		Flow:                auth.FlowServicePrincipal,
		ServiceAccountEmail: "sa@project.iam.gserviceaccount.com",
	}
	metadataBytes, err := json.Marshal(metadata)
	require.NoError(t, err)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	status, err := handler.Status(ctx)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.True(t, status.Authenticated)
	assert.Equal(t, auth.IdentityTypeServicePrincipal, status.IdentityType)
}

func TestHandler_Status_StoredCredentials_RAPTExpired(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	ctx := context.Background()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")

	// Store metadata (as if user logged in via interactive flow)
	metadata := &TokenMetadata{
		Claims: &auth.Claims{
			Email:   "user@example.com",
			Name:    "Test User",
			Subject: "12345",
			Issuer:  "https://accounts.google.com",
		},
		Flow:   auth.FlowInteractive,
		Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
	}
	metadataBytes, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, store.Set(ctx, SecretKeyMetadata, metadataBytes))

	// Store a refresh token so the probe actually runs
	require.NoError(t, store.Set(ctx, SecretKeyRefreshToken, []byte("fake-refresh-token")))

	// Mock a RAPT-expired token refresh response
	mockHTTP.AddResponse(400, TokenErrorResponse{
		Error:            "invalid_rapt",
		ErrorDescription: "Reauthentication required by RAPT policy",
	})

	status, err := handler.Status(ctx)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.False(t, status.Authenticated)
	assert.Equal(t, "expired (RAPT policy)", status.Reason)
	assert.Equal(t, "user@example.com", status.Claims.Email)
}

func TestHandler_Status_StoredCredentials_InvalidGrant(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	ctx := context.Background()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")

	metadata := &TokenMetadata{
		Claims: &auth.Claims{
			Email:   "user@example.com",
			Subject: "12345",
			Issuer:  "https://accounts.google.com",
		},
		Flow:   auth.FlowInteractive,
		Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
	}
	metadataBytes, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, store.Set(ctx, SecretKeyMetadata, metadataBytes))
	require.NoError(t, store.Set(ctx, SecretKeyRefreshToken, []byte("fake-refresh-token")))

	// Mock an invalid_grant response (revoked credentials)
	// The handler calls Logout on invalid_grant, which calls revokeRefreshToken (1 HTTP call).
	mockHTTP.AddResponse(400, TokenErrorResponse{
		Error:            "invalid_grant",
		ErrorDescription: "Token has been revoked",
	})
	// Logout's revokeRefreshToken call
	mockHTTP.AddResponse(200, map[string]string{"status": "ok"})

	status, err := handler.Status(ctx)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.False(t, status.Authenticated)
	assert.Equal(t, "expired or revoked", status.Reason)
}

func TestHandler_Status_InvalidGrant_EmbedderBinaryName(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	params := settings.NewCliParams()
	params.BinaryName = "mycli"
	ctx := settings.IntoContext(context.Background(), params)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())

	metadata := &TokenMetadata{
		Claims: &auth.Claims{
			Email:  "user@example.com",
			Issuer: "https://accounts.google.com",
		},
		Flow:   auth.FlowInteractive,
		Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
	}
	metadataBytes, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, store.Set(ctx, SecretKeyMetadata, metadataBytes))
	require.NoError(t, store.Set(ctx, SecretKeyRefreshToken, []byte("fake-refresh-token")))

	mockHTTP.AddResponse(400, TokenErrorResponse{
		Error:            "invalid_grant",
		ErrorDescription: "Token has been revoked",
	})
	// Logout's revokeRefreshToken call
	mockHTTP.AddResponse(200, map[string]string{"status": "ok"})

	status, err := handler.Status(ctx)
	require.NoError(t, err)
	assert.False(t, status.Authenticated)
	assert.Equal(t, "expired or revoked", status.Reason)
}

func TestHandler_Status_StoredCredentials_ValidToken(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	ctx := context.Background()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")

	metadata := &TokenMetadata{
		Claims: &auth.Claims{
			Email:   "user@example.com",
			Name:    "Test User",
			Subject: "12345",
			Issuer:  "https://accounts.google.com",
		},
		Flow:   auth.FlowInteractive,
		Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
	}
	metadataBytes, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, store.Set(ctx, SecretKeyMetadata, metadataBytes))
	require.NoError(t, store.Set(ctx, SecretKeyRefreshToken, []byte("fake-refresh-token")))

	// Mock a successful token refresh
	mockHTTP.AddResponse(200, map[string]any{
		"access_token": "new-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
	})

	status, err := handler.Status(ctx)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.True(t, status.Authenticated)
	assert.Empty(t, status.Reason)
	assert.Equal(t, "user@example.com", status.Claims.Email)
}

func TestHandler_Logout(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	ctx := context.Background()

	// Populate store with credentials
	err = store.Set(ctx, SecretKeyRefreshToken, []byte("refresh-token"))
	require.NoError(t, err)

	metadataBytes, _ := json.Marshal(&TokenMetadata{
		Claims: &auth.Claims{Email: "user@example.com"},
	})
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	// Mock revoke endpoint response
	mockHTTP.AddResponse(200, map[string]string{"status": "ok"})

	err = handler.Logout(ctx)
	require.NoError(t, err)

	// Verify credentials are removed
	exists, _ := store.Exists(ctx, SecretKeyRefreshToken)
	assert.False(t, exists)
	exists, _ = store.Exists(ctx, SecretKeyMetadata)
	assert.False(t, exists)
}

func TestHandler_Logout_NoCredentials(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	ctx := context.Background()

	// Logout should succeed even with no stored credentials
	err = handler.Logout(ctx)
	require.NoError(t, err)
}

func TestHandler_GetToken_NotAuthenticated(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")
	// Point gcloud ADC to a nonexistent directory to prevent local fallback
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())

	_, err = handler.GetToken(ctx, auth.TokenOptions{
		Scope: "https://www.googleapis.com/auth/cloud-platform",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrNotAuthenticated)
}

func TestHandler_GetToken_GcloudADCFallback_SetsFlow(t *testing.T) {
	// Verify that when gcloud ADC is the credential source (e.g. after
	// 'scafctl auth logout gcp' which only clears scafctl-managed creds),
	// the returned token has Flow set to FlowGcloudADC so callers can see
	// why the token was issued.
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	ctx := context.Background()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")

	// Write a fake gcloud ADC credentials file
	tmpDir := t.TempDir()
	t.Setenv(EnvCloudSDKConfig, tmpDir)
	adcPath := filepath.Join(tmpDir, "application_default_credentials.json")
	adcData, _ := json.Marshal(GcloudADCCredentials{
		ClientID:     "fake-client-id",
		ClientSecret: "fake-client-secret",
		RefreshToken: "fake-refresh-token",
		Type:         "authorized_user",
	})
	require.NoError(t, os.WriteFile(adcPath, adcData, 0o600))

	// Mock the token endpoint response
	mockHTTP.AddResponse(200, TokenResponse{
		AccessToken: "gcloud-fallback-token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	})

	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope: "https://www.googleapis.com/auth/cloud-platform",
	})
	require.NoError(t, err)
	require.NotNil(t, token)
	assert.Equal(t, "gcloud-fallback-token", token.AccessToken)
	assert.Equal(t, auth.FlowGcloudADC, token.Flow, "Flow should identify gcloud ADC as the credential source")
}

func TestHandler_GetToken_FromCache(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	ctx := context.Background()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")

	// Store a refresh token so resolveSourceTokenFunc returns getStoredRefreshToken
	err = store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh-token"))
	require.NoError(t, err)

	scope := "https://www.googleapis.com/auth/cloud-platform"

	// Pre-cache a valid token
	cachedToken := &auth.Token{
		AccessToken: "cached-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		Scope:       scope,
	}
	err = handler.tokenCache.Set(ctx, "", "_", scope, cachedToken)
	require.NoError(t, err)

	// GetToken should return the cached token without any HTTP calls
	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope: scope,
	})
	require.NoError(t, err)
	require.NotNil(t, token)
	assert.Equal(t, "cached-access-token", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)

	// Verify no HTTP calls were made
	assert.Empty(t, mockHTTP.Requests)
}

func TestHandler_GetToken_ForceRefresh(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	ctx := context.Background()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")
	// Point gcloud ADC to a nonexistent directory to prevent local fallback
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())

	// Store a refresh token
	err = store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh-token"))
	require.NoError(t, err)

	// Store metadata (required by mintToken)
	metadata := &TokenMetadata{
		Claims: &auth.Claims{
			Email: "user@example.com",
		},
		Flow:     auth.FlowInteractive,
		ClientID: "test-client-id",
		Scopes:   []string{"openid"},
	}
	metadataBytes, err := json.Marshal(metadata)
	require.NoError(t, err)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	scope := "https://www.googleapis.com/auth/cloud-platform"

	// Pre-cache a valid token
	cachedToken := &auth.Token{
		AccessToken: "cached-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		Scope:       scope,
	}
	err = handler.tokenCache.Set(ctx, auth.FlowInteractive, "_", scope, cachedToken)
	require.NoError(t, err)

	// Mock token endpoint response for refresh
	mockHTTP.AddResponse(200, map[string]any{
		"access_token": "refreshed-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"scope":        scope,
	})

	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope:        scope,
		ForceRefresh: true,
	})
	require.NoError(t, err)
	require.NotNil(t, token)
	assert.Equal(t, "refreshed-access-token", token.AccessToken)

	// Verify an HTTP call was made (force refresh bypasses cache)
	assert.Len(t, mockHTTP.Requests, 1)
}

func TestHandler_GetToken_NoScope(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")

	// Store a refresh token
	err = store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh-token"))
	require.NoError(t, err)

	// GetToken with no scope should return ErrInvalidScope
	_, err = handler.GetToken(ctx, auth.TokenOptions{})
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrInvalidScope)
}

func TestHandler_InjectAuth(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	ctx := context.Background()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")

	scope := "https://www.googleapis.com/auth/cloud-platform"

	// Store a refresh token
	err = store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh-token"))
	require.NoError(t, err)

	// Cache a valid token
	cachedToken := &auth.Token{
		AccessToken: "inject-test-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		Scope:       scope,
	}
	err = handler.tokenCache.Set(ctx, "", "_", scope, cachedToken)
	require.NoError(t, err)

	// Create a request and inject auth
	req, _ := newTestRequest(t)
	err = handler.InjectAuth(ctx, req, auth.TokenOptions{Scope: scope})
	require.NoError(t, err)

	assert.Equal(t, "Bearer inject-test-token", req.Header.Get("Authorization"))
}

// Compile-time check for interface implementation.
var _ auth.Handler = (*Handler)(nil)

func TestGCPHandler_Capabilities(t *testing.T) {
	store := secrets.NewMockStore()
	h, err := New(WithSecretStore(store))
	require.NoError(t, err)
	caps := h.Capabilities()
	assert.NotEmpty(t, caps)
}

func TestGCPHandler_WithHTTPClientConfig(t *testing.T) {
	cfg := &config.HTTPClientConfig{}
	opt := WithHTTPClientConfig(cfg)
	h := &Handler{}
	opt(h)
	assert.NotNil(t, h.httpClientConfig)
}

func TestGCPHandler_WithLogger(t *testing.T) {
	lgr := logr.Discard()
	opt := WithLogger(lgr)
	h := &Handler{}
	opt(h)
	assert.Equal(t, lgr, h.logger)
}

func TestGCPHandler_ListCachedTokens_Empty(t *testing.T) {
	store := secrets.NewMockStore()
	h, err := New(WithSecretStore(store))
	require.NoError(t, err)

	results, err := h.ListCachedTokens(context.Background())
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestGCPHandler_ListCachedTokens_WithCache(t *testing.T) {
	store := secrets.NewMockStore()
	h, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_EXTERNAL_ACCOUNT", "")

	require.NoError(t, store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh")))

	scope := "https://www.googleapis.com/auth/cloud-platform"
	cachedToken := &auth.Token{
		AccessToken: "list-test-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		Scope:       scope,
	}
	require.NoError(t, h.tokenCache.Set(ctx, "", "_", scope, cachedToken))

	results, err := h.ListCachedTokens(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestGCPHandler_PurgeExpiredTokens(t *testing.T) {
	store := secrets.NewMockStore()
	h, err := New(WithSecretStore(store))
	require.NoError(t, err)

	n, err := h.PurgeExpiredTokens(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
