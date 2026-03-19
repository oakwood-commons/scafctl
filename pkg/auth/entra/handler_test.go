// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_NewWithDefaults(t *testing.T) {
	// This test requires a real secrets store, so we use WithSecretStore to provide a mock
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))

	require.NoError(t, err)
	assert.NotNil(t, handler)
	assert.Equal(t, HandlerName, handler.Name())
	assert.Equal(t, HandlerDisplayName, handler.DisplayName())
	assert.Contains(t, handler.SupportedFlows(), auth.FlowDeviceCode)
}

func TestHandler_NewWithOptions(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	customConfig := &Config{
		ClientID:  "custom-client-id",
		TenantID:  "custom-tenant",
		Authority: "https://custom.authority.com",
	}

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
		WithConfig(customConfig),
	)

	require.NoError(t, err)
	assert.Equal(t, "custom-client-id", handler.config.ClientID)
	assert.Equal(t, "custom-tenant", handler.config.TenantID)
	assert.Equal(t, "https://custom.authority.com", handler.config.Authority)
}

func TestHandler_Status_NotAuthenticated(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	status, err := handler.Status(ctx)

	require.NoError(t, err)
	assert.False(t, status.Authenticated)
}

func TestHandler_Status_Authenticated(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	// Store credentials
	err = store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh-token"))
	require.NoError(t, err)

	metadata := &TokenMetadata{
		Claims: &auth.Claims{
			Subject:  "test-subject",
			Email:    "test@example.com",
			TenantID: "test-tenant",
		},
		RefreshTokenExpiresAt: time.Now().Add(24 * time.Hour),
		LastRefresh:           time.Now(),
		TenantID:              "test-tenant",
	}
	metadataBytes, _ := json.Marshal(metadata)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	status, err := handler.Status(ctx)
	require.NoError(t, err)
	assert.True(t, status.Authenticated)
	assert.Equal(t, "test-subject", status.Claims.Subject)
	assert.Equal(t, "test@example.com", status.Claims.Email)
	assert.Equal(t, "test-tenant", status.TenantID)
}

func TestHandler_Status_ExpiredRefreshToken(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	// Store credentials with expired refresh token
	err = store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh-token"))
	require.NoError(t, err)

	metadata := &TokenMetadata{
		Claims: &auth.Claims{
			Subject: "test-subject",
		},
		RefreshTokenExpiresAt: time.Now().Add(-24 * time.Hour), // Expired
		TenantID:              "test-tenant",
	}
	metadataBytes, _ := json.Marshal(metadata)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	status, err := handler.Status(ctx)
	require.NoError(t, err)
	assert.False(t, status.Authenticated) // Not authenticated due to expired token
}

func TestHandler_Logout(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	// Store some credentials
	err = store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh-token"))
	require.NoError(t, err)
	err = store.Set(ctx, SecretKeyMetadata, []byte("{}"))
	require.NoError(t, err)

	// Store a cached token
	cache := auth.NewTokenCache(store, SecretKeyTokenPrefix)
	err = cache.Set(ctx, "", "_", "test-scope", &auth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})
	require.NoError(t, err)

	// Logout
	err = handler.Logout(ctx)
	require.NoError(t, err)

	// Verify everything is cleared
	exists, _ := store.Exists(ctx, SecretKeyRefreshToken)
	assert.False(t, exists)
	exists, _ = store.Exists(ctx, SecretKeyMetadata)
	assert.False(t, exists)

	// Token should be cleared too
	token, err := cache.Get(ctx, "", "_", "test-scope")
	require.NoError(t, err)
	assert.Nil(t, token)
}

func TestHandler_GetToken_InvalidScope(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	_, err = handler.GetToken(ctx, auth.TokenOptions{Scope: ""})
	require.ErrorIs(t, err, auth.ErrInvalidScope)
}

func TestHandler_GetToken_NotAuthenticated(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	_, err = handler.GetToken(ctx, auth.TokenOptions{Scope: "test-scope"})
	require.ErrorIs(t, err, auth.ErrNotAuthenticated)
}

func TestHandler_GetToken_FromCache(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	scope := "https://graph.microsoft.com/.default"

	// Pre-populate cache
	cache := auth.NewTokenCache(store, SecretKeyTokenPrefix)
	cachedToken := &auth.Token{
		AccessToken: "cached-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
	}
	// Use the same fingerprint the handler will compute from default config
	fp := auth.FingerprintHash(handler.config.ClientID + ":" + handler.config.TenantID)
	err = cache.Set(ctx, "", fp, scope, cachedToken)
	require.NoError(t, err)

	// Also set up refresh token so the handler thinks we're authenticated
	err = store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh-token"))
	require.NoError(t, err)
	metadata := &TokenMetadata{TenantID: "test-tenant"}
	metadataBytes, _ := json.Marshal(metadata)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	// Get token - should come from cache
	token, err := handler.GetToken(ctx, auth.TokenOptions{Scope: scope})
	require.NoError(t, err)
	assert.Equal(t, "cached-access-token", token.AccessToken)
}

func TestHandler_GetToken_MintNew(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()
	scope := "https://graph.microsoft.com/.default"

	// Set up authentication
	err = store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh-token"))
	require.NoError(t, err)
	metadata := &TokenMetadata{TenantID: "test-tenant", ClientID: "04b07795-8ddb-461a-bbee-02f9e1bf7b46"}
	metadataBytes, _ := json.Marshal(metadata)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	// Mock token response
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "new-access-token",
		"refresh_token": "new-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         scope,
	})

	// Get token - should mint new one
	token, err := handler.GetToken(ctx, auth.TokenOptions{Scope: scope})
	require.NoError(t, err)
	assert.Equal(t, "new-access-token", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
}

func TestHandler_GetToken_ForceRefresh(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()
	scope := "https://graph.microsoft.com/.default"

	// Set up authentication
	err = store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh-token"))
	require.NoError(t, err)
	metadata := &TokenMetadata{TenantID: "test-tenant", ClientID: "04b07795-8ddb-461a-bbee-02f9e1bf7b46"}
	metadataBytes, _ := json.Marshal(metadata)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	// Pre-populate cache with valid token
	cache := auth.NewTokenCache(store, SecretKeyTokenPrefix)
	fp := auth.FingerprintHash(handler.config.ClientID + ":" + handler.config.TenantID)
	err = cache.Set(ctx, "", fp, scope, &auth.Token{
		AccessToken: "cached-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})
	require.NoError(t, err)

	// Mock token response
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "fresh-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         scope,
	})

	// Get token with force refresh
	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope:        scope,
		ForceRefresh: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "fresh-access-token", token.AccessToken)
}

func TestHandler_InjectAuth(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	scope := "https://graph.microsoft.com/.default"

	// Pre-populate cache
	cache := auth.NewTokenCache(store, SecretKeyTokenPrefix)
	fp := auth.FingerprintHash(handler.config.ClientID + ":" + handler.config.TenantID)
	err = cache.Set(ctx, "", fp, scope, &auth.Token{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})
	require.NoError(t, err)

	// Set up for authenticated state
	err = store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh-token"))
	require.NoError(t, err)
	metadata := &TokenMetadata{TenantID: "test-tenant"}
	metadataBytes, _ := json.Marshal(metadata)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	// Create request and inject auth
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.microsoft.com/v1.0/me", nil)
	require.NoError(t, err)

	err = handler.InjectAuth(ctx, req, auth.TokenOptions{Scope: scope})
	require.NoError(t, err)

	assert.Equal(t, "Bearer test-access-token", req.Header.Get("Authorization"))
}

func TestHandler_ImplementsInterface(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	// Verify handler implements auth.Handler
	var _ auth.Handler = handler
}

func TestExtractClaims(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	// Create a mock ID token (JWT format: header.payload.signature)
	payload := map[string]any{
		"iss":                "https://login.microsoftonline.com/tenant-id/v2.0",
		"sub":                "test-subject-id",
		"aud":                "client-id",
		"tid":                "tenant-id",
		"oid":                "object-id",
		"email":              "user@example.com",
		"preferred_username": "user@example.com",
		"name":               "Test User",
		"iat":                time.Now().Unix(),
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
	}
	payloadBytes, _ := json.Marshal(payload)
	encodedPayload := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(payloadBytes)

	// Create fake JWT (header and signature don't matter for our parsing)
	idToken := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9." + encodedPayload + ".fake-signature"

	tokenResp := &TokenResponse{
		IDToken: idToken,
	}

	claims, err := handler.extractClaims(tokenResp)
	require.NoError(t, err)
	assert.Equal(t, "test-subject-id", claims.Subject)
	assert.Equal(t, "tenant-id", claims.TenantID)
	assert.Equal(t, "object-id", claims.ObjectID)
	assert.Equal(t, "user@example.com", claims.Email)
	assert.Equal(t, "Test User", claims.Name)
}

func TestExtractClaims_NoIDToken(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	claims, err := handler.extractClaims(&TokenResponse{})
	require.NoError(t, err)
	assert.NotNil(t, claims)
	assert.Empty(t, claims.Subject)
}

func TestHandler_Capabilities(t *testing.T) {
	store := secrets.NewMockStore()
	h, err := New(WithSecretStore(store))
	require.NoError(t, err)
	caps := h.Capabilities()
	assert.NotEmpty(t, caps)
}

func TestHandler_WithHTTPClientConfig(t *testing.T) {
	cfg := &config.HTTPClientConfig{}
	opt := WithHTTPClientConfig(cfg)
	h := &Handler{}
	opt(h)
	assert.NotNil(t, h.httpClientConfig)
}

func TestHandler_WithLogger(t *testing.T) {
	lgr := logr.Discard()
	opt := WithLogger(lgr)
	h := &Handler{}
	opt(h)
	assert.Equal(t, lgr, h.logger)
}

func TestEntraHandler_ListCachedTokens_Empty(t *testing.T) {
	store := secrets.NewMockStore()
	h, err := New(WithSecretStore(store))
	require.NoError(t, err)

	results, err := h.ListCachedTokens(context.Background())
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestEntraHandler_PurgeExpiredTokens(t *testing.T) {
	store := secrets.NewMockStore()
	h, err := New(WithSecretStore(store))
	require.NoError(t, err)

	n, err := h.PurgeExpiredTokens(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
