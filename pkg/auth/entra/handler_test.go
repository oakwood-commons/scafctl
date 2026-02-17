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

	"github.com/oakwood-commons/scafctl/pkg/auth"
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
	cache := NewTokenCache(store)
	err = cache.Set(ctx, "test-scope", &auth.Token{
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
	token, err := cache.Get(ctx, "test-scope")
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
	cache := NewTokenCache(store)
	cachedToken := &auth.Token{
		AccessToken: "cached-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
	}
	err = cache.Set(ctx, scope, cachedToken)
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
	cache := NewTokenCache(store)
	err = cache.Set(ctx, scope, &auth.Token{
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
	cache := NewTokenCache(store)
	err = cache.Set(ctx, scope, &auth.Token{
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

func TestBase64URLDecode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "no padding needed",
			input: "dGVzdA",
			want:  "test",
		},
		{
			name:  "one padding needed",
			input: "dGVzdDE",
			want:  "test1",
		},
		{
			name:  "two padding needed",
			input: "dGVzdDEy",
			want:  "test12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := base64URLDecode(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, string(got))
			}
		})
	}
}

func TestSplitJWT(t *testing.T) {
	parts := splitJWT("header.payload.signature")
	assert.Len(t, parts, 3)
	assert.Equal(t, "header", parts[0])
	assert.Equal(t, "payload", parts[1])
	assert.Equal(t, "signature", parts[2])
}
