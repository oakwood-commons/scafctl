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
	"github.com/oakwood-commons/scafctl/pkg/clock"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_AuthCodeLogin(t *testing.T) {
	ctx := context.Background()

	// Override browser opener to prevent actual browser launch
	origOpener := BrowserOpener
	BrowserOpener = func(_ context.Context, _ string) error { return nil }
	defer func() { BrowserOpener = origOpener }()
	t.Run("exchange_auth_code", func(t *testing.T) {
		mockHTTP2 := NewMockHTTPClient()
		h2, err := New(
			WithSecretStore(secrets.NewMockStore()),
			WithHTTPClient(mockHTTP2),
		)
		require.NoError(t, err)

		mockHTTP2.AddResponse(http.StatusOK, map[string]any{
			"access_token": "gho_exchanged_token",
			"token_type":   "Bearer",
			"scope":        "repo",
		})

		tokenResp, err := h2.exchangeAuthCode(ctx, "test-auth-code", "http://localhost:12345", "test-verifier")
		require.NoError(t, err)
		assert.Equal(t, "gho_exchanged_token", tokenResp.AccessToken)
		assert.Equal(t, "Bearer", tokenResp.TokenType)
		assert.Equal(t, "repo", tokenResp.Scope)

		// Verify request parameters
		reqs := mockHTTP2.GetRequests()
		require.Len(t, reqs, 1)
		assert.Equal(t, "POST", reqs[0].Method)
		assert.Contains(t, reqs[0].Endpoint, "/login/oauth/access_token")
		assert.Equal(t, "test-auth-code", reqs[0].Data.Get("code"))
		assert.Equal(t, "http://localhost:12345", reqs[0].Data.Get("redirect_uri"))
		assert.Equal(t, "test-verifier", reqs[0].Data.Get("code_verifier"))
		assert.Equal(t, DefaultClientID, reqs[0].Data.Get("client_id"))
	})

	t.Run("exchange_auth_code_error", func(t *testing.T) {
		mockHTTP2 := NewMockHTTPClient()
		h2, err := New(
			WithSecretStore(secrets.NewMockStore()),
			WithHTTPClient(mockHTTP2),
		)
		require.NoError(t, err)

		mockHTTP2.AddResponse(http.StatusOK, map[string]any{
			"error":             "bad_verification_code",
			"error_description": "The code passed is incorrect or expired.",
		})

		_, err = h2.exchangeAuthCode(ctx, "bad-code", "http://localhost:12345", "test-verifier")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "bad_verification_code")
	})

	t.Run("exchange_auth_code_with_refresh_token", func(t *testing.T) {
		mockHTTP2 := NewMockHTTPClient()
		h2, err := New(
			WithSecretStore(secrets.NewMockStore()),
			WithHTTPClient(mockHTTP2),
		)
		require.NoError(t, err)

		mockHTTP2.AddResponse(http.StatusOK, map[string]any{
			"access_token":             "ghu_access_via_authcode",
			"token_type":               "Bearer",
			"scope":                    "repo",
			"expires_in":               28800,
			"refresh_token":            "ghr_refresh_via_authcode",
			"refresh_token_expires_in": 15897600,
		})

		tokenResp, err := h2.exchangeAuthCode(ctx, "test-code", "http://localhost:12345", "test-verifier")
		require.NoError(t, err)
		assert.Equal(t, "ghu_access_via_authcode", tokenResp.AccessToken)
		assert.Equal(t, "ghr_refresh_via_authcode", tokenResp.RefreshToken)
		assert.Equal(t, 28800, tokenResp.ExpiresIn)
		assert.Equal(t, 15897600, tokenResp.RefreshTokenExpiresIn)
	})
}

func TestHandler_AuthCodeLogin_SupportedFlows(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	flows := handler.SupportedFlows()
	assert.Contains(t, flows, auth.FlowInteractive)
	assert.Contains(t, flows, auth.FlowDeviceCode)
	assert.Contains(t, flows, auth.FlowPAT)
	assert.Contains(t, flows, auth.FlowGitHubApp)
}

func TestHandler_AuthCodeLogin_Capabilities(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	caps := handler.Capabilities()
	assert.Contains(t, caps, auth.CapScopesOnLogin)
	assert.Contains(t, caps, auth.CapHostname)
	assert.Contains(t, caps, auth.CapCallbackPort)
}

func TestHandler_LoginRoutingInteractive(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	// ClientSecret must be set to use auth code + PKCE flow.
	// Without it the handler falls back to interactiveDeviceCodeLogin.
	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
		WithConfig(&Config{ClientSecret: "test-client-secret"}),
	)
	require.NoError(t, err)

	// Override browser opener
	origOpener := BrowserOpener
	var browserOpened bool
	BrowserOpener = func(_ context.Context, _ string) error {
		browserOpened = true
		return nil
	}
	defer func() { BrowserOpener = origOpener }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Login with FlowInteractive + client_secret → auth code flow (will timeout since no callback)
	_, err = handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowInteractive,
		Timeout: 50 * time.Millisecond,
	})

	// Should have attempted to open browser
	assert.True(t, browserOpened)
	// Should timeout since no one is completing the callback
	assert.Error(t, err)
}

func TestHandler_LoginRoutingDeviceCode(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	mockClock := clock.NewMock()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
		WithClock(mockClock),
	)
	require.NoError(t, err)

	// Setup device code flow mocks
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "DCFL-1234",
		"verification_uri": "https://github.com/login/device",
		"expires_in":       900,
		"interval":         5,
	})

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token": "gho_dc_token",
		"token_type":   "Bearer",
		"scope":        "repo",
	})

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"login": "dcuser",
		"id":    1,
		"name":  "DC User",
	})

	ctx := context.Background()
	go func() { time.Sleep(10 * time.Millisecond); mockClock.Add(5 * time.Second) }()

	result, err := handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowDeviceCode,
		Timeout: 10 * time.Second,
	})

	require.NoError(t, err)
	assert.Equal(t, "dcuser", result.Claims.Subject)
}

func TestHandler_LoginRoutingDefault(t *testing.T) {
	// With no explicit flow and no PAT env vars and no client_secret, default should
	// be interactiveDeviceCodeLogin (device code with browser auto-open).
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	// Queue a device code response so the flow can fetch the code and open the browser
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "dev-code-xyz",
		"user_code":        "ABCD-1234",
		"verification_uri": "https://github.com/login/device",
		"expires_in":       900,
		"interval":         5,
	})

	origOpener := BrowserOpener
	var browserOpened bool
	var browserURL string
	BrowserOpener = func(_ context.Context, url string) error {
		browserOpened = true
		browserURL = url
		return nil
	}
	defer func() { BrowserOpener = origOpener }()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Login with empty flow (no client_secret) → interactiveDeviceCodeLogin
	_, _ = handler.Login(ctx, auth.LoginOptions{
		Timeout: 50 * time.Millisecond,
	})

	// Should have opened browser to the device verification URI
	assert.True(t, browserOpened)
	assert.Contains(t, browserURL, "github.com/login/device")
}

func TestHandler_StoreCredentialsFromAuthCode(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// Mock /user
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"login": "authuser",
		"id":    100,
		"name":  "Auth User",
		"email": "auth@example.com",
	})

	tokenResp := &TokenResponse{
		AccessToken:           "gho_stored_token",
		RefreshToken:          "ghr_stored_refresh",
		TokenType:             "Bearer",
		Scope:                 "repo read:org",
		ExpiresIn:             28800,
		RefreshTokenExpiresIn: 15897600,
	}

	claims, err := handler.storeCredentials(ctx, tokenResp, []string{"repo", "read:org"}, "")
	require.NoError(t, err)
	assert.Equal(t, "authuser", claims.Subject)
	assert.Equal(t, "Auth User", claims.Name)

	// Verify stored access token
	storedAccess, err := store.Get(ctx, SecretKeyAccessToken)
	require.NoError(t, err)
	assert.Equal(t, "gho_stored_token", string(storedAccess))

	// Verify stored refresh token
	storedRefresh, err := store.Get(ctx, SecretKeyRefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "ghr_stored_refresh", string(storedRefresh))

	// Verify metadata
	metaBytes, err := store.Get(ctx, SecretKeyMetadata)
	require.NoError(t, err)
	var metadata TokenMetadata
	err = json.Unmarshal(metaBytes, &metadata)
	require.NoError(t, err)
	assert.Equal(t, "authuser", metadata.Claims.Subject)
	assert.Equal(t, DefaultClientID, metadata.ClientID)
	assert.Equal(t, []string{"repo", "read:org"}, metadata.Scopes)
	assert.NotEmpty(t, metadata.SessionID)
}
