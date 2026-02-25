// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
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
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))

	require.NoError(t, err)
	assert.NotNil(t, handler)
	assert.Equal(t, HandlerName, handler.Name())
	assert.Equal(t, HandlerDisplayName, handler.DisplayName())
	assert.Contains(t, handler.SupportedFlows(), auth.FlowInteractive)
	assert.Contains(t, handler.SupportedFlows(), auth.FlowDeviceCode)
	assert.Contains(t, handler.SupportedFlows(), auth.FlowPAT)
	assert.Contains(t, handler.SupportedFlows(), auth.FlowGitHubApp)
}

func TestHandler_NewWithOptions(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	customConfig := &Config{
		ClientID: "custom-client-id",
		Hostname: "github.example.com",
	}

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
		WithConfig(customConfig),
	)

	require.NoError(t, err)
	assert.Equal(t, "custom-client-id", handler.config.ClientID)
	assert.Equal(t, "github.example.com", handler.config.Hostname)
}

func TestHandler_NewWithPartialConfig(t *testing.T) {
	store := secrets.NewMockStore()

	handler, err := New(
		WithSecretStore(store),
		WithConfig(&Config{
			Hostname: "github.example.com",
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, DefaultClientID, handler.config.ClientID)
	assert.Equal(t, "github.example.com", handler.config.Hostname)
}

func TestHandler_NewWithNilConfig(t *testing.T) {
	store := secrets.NewMockStore()

	handler, err := New(
		WithSecretStore(store),
		WithConfig(nil),
	)

	require.NoError(t, err)
	assert.Equal(t, DefaultClientID, handler.config.ClientID)
	assert.Equal(t, DefaultHostname, handler.config.Hostname)
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

	err = store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh-token"))
	require.NoError(t, err)

	metadata := &TokenMetadata{
		Claims: &auth.Claims{
			Subject:  "testuser",
			Email:    "test@example.com",
			Name:     "Test User",
			Username: "testuser",
			Issuer:   "github.com",
		},
		RefreshTokenExpiresAt: time.Now().Add(6 * 30 * 24 * time.Hour),
		LastRefresh:           time.Now(),
		Hostname:              "github.com",
		ClientID:              DefaultClientID,
		Scopes:                []string{"repo", "read:user"},
	}
	metadataBytes, err := json.Marshal(metadata)
	require.NoError(t, err)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	status, err := handler.Status(ctx)
	require.NoError(t, err)
	assert.True(t, status.Authenticated)
	assert.Equal(t, "testuser", status.Claims.Subject)
	assert.Equal(t, "test@example.com", status.Claims.Email)
	assert.Equal(t, auth.IdentityTypeUser, status.IdentityType)
	assert.Equal(t, []string{"repo", "read:user"}, status.Scopes)
}

func TestHandler_Status_ExpiredRefreshToken(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	err = store.Set(ctx, SecretKeyRefreshToken, []byte("expired-refresh-token"))
	require.NoError(t, err)

	metadata := &TokenMetadata{
		Claims: &auth.Claims{
			Subject: "testuser",
		},
		RefreshTokenExpiresAt: time.Now().Add(-time.Hour),
		LastRefresh:           time.Now().Add(-8 * time.Hour),
		Hostname:              "github.com",
	}
	metadataBytes, err := json.Marshal(metadata)
	require.NoError(t, err)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	status, err := handler.Status(ctx)
	require.NoError(t, err)
	assert.False(t, status.Authenticated)
}

func TestHandler_Logout(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	err = store.Set(ctx, SecretKeyRefreshToken, []byte("refresh-token"))
	require.NoError(t, err)
	err = store.Set(ctx, SecretKeyAccessToken, []byte("access-token"))
	require.NoError(t, err)
	err = store.Set(ctx, SecretKeyMetadata, []byte("{}"))
	require.NoError(t, err)

	err = handler.Logout(ctx)
	require.NoError(t, err)

	exists, err := store.Exists(ctx, SecretKeyRefreshToken)
	require.NoError(t, err)
	assert.False(t, exists)

	exists, err = store.Exists(ctx, SecretKeyAccessToken)
	require.NoError(t, err)
	assert.False(t, exists)

	exists, err = store.Exists(ctx, SecretKeyMetadata)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestHandler_GetToken_NotAuthenticated(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.GetToken(ctx, auth.TokenOptions{})
	assert.ErrorIs(t, err, auth.ErrNotAuthenticated)
}

func TestHandler_GetToken_EmptyScope(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	// With a stored access token, empty scope should succeed
	// because GitHub scopes are fixed at login time.
	err = store.Set(ctx, SecretKeyAccessToken, []byte("gho_testtoken"))
	require.NoError(t, err)

	token, err := handler.GetToken(ctx, auth.TokenOptions{Scope: ""})
	require.NoError(t, err)
	assert.Equal(t, "gho_testtoken", token.AccessToken)
}

func TestHandler_GetToken_WithStoredAccessToken(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	err = store.Set(ctx, SecretKeyAccessToken, []byte("gho_testtoken"))
	require.NoError(t, err)

	token, err := handler.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "gho_testtoken", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.True(t, token.ExpiresAt.After(time.Now().Add(364*24*time.Hour)))
}

func TestHandler_GetToken_CachedToken(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	cachedToken := &auth.Token{
		AccessToken: "cached-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	err = handler.tokenCache.Set(ctx, defaultCacheKey, cachedToken)
	require.NoError(t, err)

	err = store.Set(ctx, SecretKeyAccessToken, []byte("stored-token"))
	require.NoError(t, err)

	token, err := handler.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "cached-token", token.AccessToken)
}

func TestHandler_GetToken_ForceRefresh(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	cachedToken := &auth.Token{
		AccessToken: "cached-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	err = handler.tokenCache.Set(ctx, defaultCacheKey, cachedToken)
	require.NoError(t, err)

	err = store.Set(ctx, SecretKeyAccessToken, []byte("stored-token"))
	require.NoError(t, err)

	token, err := handler.GetToken(ctx, auth.TokenOptions{
		ForceRefresh: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "stored-token", token.AccessToken)
}

func TestHandler_InjectAuth(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()

	err = store.Set(ctx, SecretKeyAccessToken, []byte("inject-test-token"))
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/test", nil)
	require.NoError(t, err)

	err = handler.InjectAuth(ctx, req, auth.TokenOptions{})
	require.NoError(t, err)

	assert.Equal(t, "Bearer inject-test-token", req.Header.Get("Authorization"))
}

func TestHandler_CompileTimeCheck(t *testing.T) {
	var _ auth.Handler = (*Handler)(nil)
}

func TestHandler_DeviceCodeLogin(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "ABCD-1234",
		"verification_uri": "https://github.com/login/device",
		"expires_in":       900,
		"interval":         5,
	})

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token": "gho_testtoken123",
		"token_type":   "Bearer",
		"scope":        "repo read:user",
	})

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"login":      "octocat",
		"id":         1,
		"name":       "The Octocat",
		"email":      "octocat@github.com",
		"avatar_url": "https://avatars.githubusercontent.com/u/1?v=4",
	})

	var receivedCode, receivedURI string
	result, err := handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowDeviceCode,
		Scopes:  []string{"repo", "read:user"},
		Timeout: 10 * time.Second,
		DeviceCodeCallback: func(userCode, verificationURI, _ string) {
			receivedCode = userCode
			receivedURI = verificationURI
		},
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "octocat", result.Claims.Subject)
	assert.Equal(t, "The Octocat", result.Claims.Name)
	assert.Equal(t, "octocat@github.com", result.Claims.Email)
	assert.Equal(t, "ABCD-1234", receivedCode)
	assert.Equal(t, "https://github.com/login/device", receivedURI)

	stored, err := store.Get(ctx, SecretKeyAccessToken)
	require.NoError(t, err)
	assert.Equal(t, "gho_testtoken123", string(stored))
}

func TestHandler_DeviceCodeLogin_WithRefreshToken(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "EFGH-5678",
		"verification_uri": "https://github.com/login/device",
		"expires_in":       900,
		"interval":         5,
	})

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":             "ghu_access123",
		"token_type":               "Bearer",
		"scope":                    "repo",
		"expires_in":               28800,
		"refresh_token":            "ghr_refresh456",
		"refresh_token_expires_in": 15897600,
	})

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"login": "testuser",
		"id":    42,
		"name":  "Test User",
		"email": "test@example.com",
	})

	result, err := handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowDeviceCode,
		Scopes:  []string{"repo"},
		Timeout: 10 * time.Second,
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "testuser", result.Claims.Subject)

	stored, err := store.Get(ctx, SecretKeyRefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "ghr_refresh456", string(stored))

	metadataBytes, err := store.Get(ctx, SecretKeyMetadata)
	require.NoError(t, err)
	var metadata TokenMetadata
	err = json.Unmarshal(metadataBytes, &metadata)
	require.NoError(t, err)
	assert.Equal(t, DefaultClientID, metadata.ClientID)
	assert.Equal(t, "github.com", metadata.Hostname)
	assert.Equal(t, []string{"repo"}, metadata.Scopes)
}

func TestHandler_MintToken_RefreshFlow(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	err = store.Set(ctx, SecretKeyRefreshToken, []byte("ghr_refresh456"))
	require.NoError(t, err)

	metadata := &TokenMetadata{
		Claims:   &auth.Claims{Subject: "testuser"},
		ClientID: DefaultClientID,
		Hostname: "github.com",
	}
	metadataBytes, err := json.Marshal(metadata)
	require.NoError(t, err)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":             "ghu_new_access",
		"token_type":               "Bearer",
		"scope":                    "repo",
		"expires_in":               28800,
		"refresh_token":            "ghr_new_refresh",
		"refresh_token_expires_in": 15897600,
	})

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"login": "testuser",
		"id":    42,
		"name":  "Test User",
	})

	token, err := handler.GetToken(ctx, auth.TokenOptions{})
	require.NoError(t, err)
	assert.Equal(t, "ghu_new_access", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
}

func TestHandler_Login_DeviceCodeUsedWhenScopesProvided(t *testing.T) {
	// When scopes are explicitly provided, Login should use device code flow
	// even if PAT credentials exist in the environment.
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// Set up mock responses for device code flow
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "SCOP-1234",
		"verification_uri": "https://github.com/login/device",
		"expires_in":       900,
		"interval":         5,
	})

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token": "gho_scoped_token",
		"token_type":   "Bearer",
		"scope":        "admin:org workflow",
	})

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"login": "scopeuser",
		"id":    99,
		"name":  "Scope User",
		"email": "scope@example.com",
	})

	// Login with explicit scopes; should use device code flow
	result, err := handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowDeviceCode,
		Scopes:  []string{"admin:org", "workflow"},
		Timeout: 10 * time.Second,
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "scopeuser", result.Claims.Subject)

	// Verify the token stored has the user-provided scopes, not the defaults
	metadataBytes, err := store.Get(ctx, SecretKeyMetadata)
	require.NoError(t, err)
	var metadata TokenMetadata
	err = json.Unmarshal(metadataBytes, &metadata)
	require.NoError(t, err)
	assert.Equal(t, []string{"admin:org", "workflow"}, metadata.Scopes)
}

func TestHandler_Login_ScopesNotOverriddenByDefaults(t *testing.T) {
	// Verify that when user provides scopes, the defaults are NOT used
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "CUST-5678",
		"verification_uri": "https://github.com/login/device",
		"expires_in":       900,
		"interval":         5,
	})

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token": "gho_custom_token",
		"token_type":   "Bearer",
		"scope":        "notifications",
	})

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"login": "customuser",
		"id":    77,
		"name":  "Custom User",
	})

	// Provide scopes that differ from defaults ("gist", "read:org", "repo", "workflow")
	customScopes := []string{"notifications"}
	result, err := handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowDeviceCode,
		Scopes:  customScopes,
		Timeout: 10 * time.Second,
	})

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify stored metadata has the custom scopes, not the defaults
	metadataBytes, err := store.Get(ctx, SecretKeyMetadata)
	require.NoError(t, err)
	var metadata TokenMetadata
	err = json.Unmarshal(metadataBytes, &metadata)
	require.NoError(t, err)
	assert.Equal(t, customScopes, metadata.Scopes)
	assert.NotEqual(t, []string{"gist", "read:org", "repo", "workflow"}, metadata.Scopes)
}
