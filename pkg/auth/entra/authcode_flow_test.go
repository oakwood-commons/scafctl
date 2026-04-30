// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/oauth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// simulateBrowserRedirect makes an HTTP GET to the callback server's redirect URI
// with the given authorization code and state, simulating the browser redirect from Entra.
func simulateBrowserRedirect(redirectURI, code, state string) error {
	u := redirectURI + "/?code=" + url.QueryEscape(code)
	if state != "" {
		u += "&state=" + url.QueryEscape(state)
	}
	resp, err := http.Get(u) //nolint:noctx // test helper
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// simulateBrowserError makes an HTTP GET to the callback server's redirect URI
// with an error, simulating Entra rejecting the auth request.
func simulateBrowserError(redirectURI, errorCode, errorDesc string) error {
	resp, err := http.Get(fmt.Sprintf("%s/?error=%s&error_description=%s", //nolint:noctx // test helper
		redirectURI,
		url.QueryEscape(errorCode),
		url.QueryEscape(errorDesc),
	))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func TestAuthCodeLogin_Success(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(DefaultConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	// Override browser opener to capture the auth URL and simulate redirect
	var capturedAuthURL string
	originalOpener := BrowserOpener
	BrowserOpener = func(ctx context.Context, authURL string) error {
		capturedAuthURL = authURL

		// Parse the redirect_uri and state from the auth URL
		parsed, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		redirectURI := parsed.Query().Get("redirect_uri")
		state := parsed.Query().Get("state")

		// Simulate browser redirect with auth code (in a goroutine to avoid blocking)
		go func() {
			// Small delay to let the callback server start listening
			time.Sleep(50 * time.Millisecond)
			_ = simulateBrowserRedirect(redirectURI, "test-auth-code-123", state)
		}()
		return nil
	}
	defer func() { BrowserOpener = originalOpener }()

	// Mock token exchange response
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "openid profile offline_access",
		"id_token":      authCodeTestIDToken(),
	})

	ctx := context.Background()
	result, err := handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowInteractive,
		Timeout: 10 * time.Second,
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.ExpiresAt.IsZero())

	// Verify the auth URL was well-formed
	assert.Contains(t, capturedAuthURL, "/oauth2/v2.0/authorize")
	assert.Contains(t, capturedAuthURL, "response_type=code")
	assert.Contains(t, capturedAuthURL, "code_challenge_method=S256")
	// prompt= must NOT be set by default so the browser sends the device PRT
	// (Primary Refresh Token) for Conditional Access device compliance.
	assert.NotContains(t, capturedAuthURL, "prompt=",
		"default auth URL must not include prompt= so the browser's SSO/PRT is used")
	assert.Contains(t, capturedAuthURL, "client_id=")

	// Verify token exchange request
	requests := mockHTTP.GetRequests()
	require.Len(t, requests, 1)
	assert.Contains(t, requests[0].Endpoint, "/oauth2/v2.0/token")
	assert.Equal(t, "authorization_code", requests[0].Data.Get("grant_type"))
	assert.Equal(t, "test-auth-code-123", requests[0].Data.Get("code"))
	assert.NotEmpty(t, requests[0].Data.Get("code_verifier"))
	assert.NotEmpty(t, requests[0].Data.Get("redirect_uri"))

	// Verify credentials were stored
	exists, _ := store.Exists(ctx, SecretKeyRefreshToken)
	assert.True(t, exists)
	exists, _ = store.Exists(ctx, SecretKeyMetadata)
	assert.True(t, exists)
}

func TestAuthCodeLogin_CustomTenantAndScopes(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(DefaultConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	var capturedAuthURL string
	originalOpener := BrowserOpener
	BrowserOpener = func(ctx context.Context, authURL string) error {
		capturedAuthURL = authURL
		parsed, _ := url.Parse(authURL)
		redirectURI := parsed.Query().Get("redirect_uri")
		state := parsed.Query().Get("state")
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = simulateBrowserRedirect(redirectURI, "test-code", state)
		}()
		return nil
	}
	defer func() { BrowserOpener = originalOpener }()

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "https://graph.microsoft.com/.default offline_access",
	})

	ctx := context.Background()
	result, err := handler.Login(ctx, auth.LoginOptions{
		Flow:     auth.FlowInteractive,
		TenantID: "my-tenant-id",
		Scopes:   []string{"https://graph.microsoft.com/.default"},
		Timeout:  10 * time.Second,
	})

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify custom tenant in auth URL
	assert.Contains(t, capturedAuthURL, "my-tenant-id/oauth2/v2.0/authorize")

	// Verify custom scope + offline_access auto-added
	parsedURL, _ := url.Parse(capturedAuthURL)
	scopeParam := parsedURL.Query().Get("scope")
	assert.Contains(t, scopeParam, "https://graph.microsoft.com/.default")
	assert.Contains(t, scopeParam, "offline_access")

	// Verify custom tenant in token exchange
	requests := mockHTTP.GetRequests()
	require.Len(t, requests, 1)
	assert.Contains(t, requests[0].Endpoint, "my-tenant-id/oauth2/v2.0/token")
}

func TestAuthCodeLogin_OfflineAccessAlreadyPresent(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(DefaultConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	var capturedAuthURL string
	originalOpener := BrowserOpener
	BrowserOpener = func(ctx context.Context, authURL string) error {
		capturedAuthURL = authURL
		parsed, _ := url.Parse(authURL)
		redirectURI := parsed.Query().Get("redirect_uri")
		state := parsed.Query().Get("state")
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = simulateBrowserRedirect(redirectURI, "test-code", state)
		}()
		return nil
	}
	defer func() { BrowserOpener = originalOpener }()

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "openid offline_access",
	})

	ctx := context.Background()
	_, err = handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowInteractive,
		Scopes:  []string{"openid", "offline_access"},
		Timeout: 10 * time.Second,
	})
	require.NoError(t, err)

	// offline_access should not be duplicated
	parsedURL, _ := url.Parse(capturedAuthURL)
	scopeParam := parsedURL.Query().Get("scope")
	count := 0
	for _, s := range splitScopeString(scopeParam) {
		if s == "offline_access" {
			count++
		}
	}
	assert.Equal(t, 1, count, "offline_access should appear exactly once")
}

func TestAuthCodeLogin_BrowserError(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(DefaultConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	// Browser opener simulates Entra returning an error
	originalOpener := BrowserOpener
	BrowserOpener = func(ctx context.Context, authURL string) error {
		parsed, _ := url.Parse(authURL)
		redirectURI := parsed.Query().Get("redirect_uri")
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = simulateBrowserError(redirectURI, "access_denied", "user cancelled the authentication")
		}()
		return nil
	}
	defer func() { BrowserOpener = originalOpener }()

	ctx := context.Background()
	_, err = handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowInteractive,
		Timeout: 10 * time.Second,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "access_denied")
}

func TestAuthCodeLogin_Timeout(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(DefaultConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	// Browser opener does nothing (no redirect), causing a timeout
	originalOpener := BrowserOpener
	BrowserOpener = func(_ context.Context, _ string) error {
		return nil
	}
	defer func() { BrowserOpener = originalOpener }()

	ctx := context.Background()
	_, err = handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowInteractive,
		Timeout: 200 * time.Millisecond,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrTimeout)
}

func TestAuthCodeLogin_ContextCancellation(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(DefaultConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	originalOpener := BrowserOpener
	BrowserOpener = func(_ context.Context, _ string) error {
		return nil
	}
	defer func() { BrowserOpener = originalOpener }()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err = handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowInteractive,
		Timeout: 30 * time.Second,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrUserCancelled)
}

func TestAuthCodeLogin_TokenExchangeFails(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(DefaultConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	originalOpener := BrowserOpener
	BrowserOpener = func(ctx context.Context, authURL string) error {
		parsed, _ := url.Parse(authURL)
		redirectURI := parsed.Query().Get("redirect_uri")
		state := parsed.Query().Get("state")
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = simulateBrowserRedirect(redirectURI, "test-code", state)
		}()
		return nil
	}
	defer func() { BrowserOpener = originalOpener }()

	// Mock token exchange error
	mockHTTP.AddResponse(http.StatusBadRequest, map[string]any{
		"error":             "invalid_grant",
		"error_description": "AADSTS12345: The code has expired",
	})

	ctx := context.Background()
	_, err = handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowInteractive,
		Timeout: 10 * time.Second,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "AADSTS12345")
}

func TestAuthCodeLogin_BrowserOpenFails_PrintsURL(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(DefaultConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	// Browser fails to open, but we simulate the redirect manually
	// (as a user would by copying the URL)
	var callbackMessage string

	// Use a buffered channel to safely pass captured values from BrowserOpener
	// to the goroutine that simulates the redirect -- avoids data race.
	type authCapture struct {
		redirectURI string
		state       string
	}
	captureCh := make(chan authCapture, 1)
	originalOpener := BrowserOpener
	BrowserOpener = func(_ context.Context, authURL string) error {
		parsed, _ := url.Parse(authURL)
		captureCh <- authCapture{
			redirectURI: parsed.Query().Get("redirect_uri"),
			state:       parsed.Query().Get("state"),
		}
		return fmt.Errorf("no browser available")
	}
	defer func() { BrowserOpener = originalOpener }()

	// Mock token exchange response
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "openid profile offline_access",
	})

	// Simulate the user manually opening the URL after seeing the message.
	// Waits for BrowserOpener to send captured values before redirecting.
	go func() {
		select {
		case info := <-captureCh:
			_ = simulateBrowserRedirect(info.redirectURI, "manual-code", info.state)
		case <-time.After(5 * time.Second):
		}
	}()

	ctx := context.Background()
	result, err := handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowInteractive,
		Timeout: 10 * time.Second,
		DeviceCodeCallback: func(_, _, message string) {
			callbackMessage = message
		},
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	// Callback should have been called with the URL
	assert.Contains(t, callbackMessage, "Open this URL")
	_ = callbackMessage
}

func TestDefaultFlowIsInteractive(t *testing.T) {
	// Verify that DefaultConfig() picks up the interactive default from
	// embedded defaults.yaml without actually triggering a Login() call
	// (which would open a real browser).
	cfg := DefaultConfig()
	assert.Equal(t, string(auth.FlowInteractive), cfg.DefaultFlow,
		"embedded defaults.yaml should set defaultFlow to interactive")

	// Verify DetectFlow uses the configured default when no explicit flow
	// is set and no environment credentials are detected.
	result := auth.DetectFlow("", nil, auth.Flow(cfg.DefaultFlow))
	assert.Equal(t, auth.FlowInteractive, result.Flow,
		"DetectFlow should resolve to interactive when that is the configured default")
}

func TestDefaultFlowOverrideDeviceCode(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	cfg := fastPollConfig()
	cfg.DefaultFlow = string(auth.FlowDeviceCode)

	handler, err := New(
		WithConfig(cfg),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	// Mock device code + token responses
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "WXYZ-5678",
		"verification_uri": "https://microsoft.com/devicelogin",
		"expires_in":       900,
		"interval":         0,
		"message":          "To sign in, use a web browser...",
	})
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "openid profile offline_access",
	})

	var callbackCalled bool
	ctx := context.Background()
	result, err := handler.Login(ctx, auth.LoginOptions{
		Timeout: 10 * time.Second,
		DeviceCodeCallback: func(userCode, verificationURI, message string) {
			callbackCalled = true
		},
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, callbackCalled, "device code callback should have been called")

	requests := mockHTTP.GetRequests()
	require.GreaterOrEqual(t, len(requests), 1)
	assert.Contains(t, requests[0].Endpoint, "/devicecode",
		"overridden flow should hit the device code endpoint")
}

func TestExchangeAuthCode_NoClientSecret(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(DefaultConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "openid profile offline_access",
	})

	ctx := context.Background()
	_, err = handler.exchangeAuthCode(ctx, "common", "test-code", "http://localhost:12345", "test-verifier")
	require.NoError(t, err)

	// Verify no client_secret was sent (public client + PKCE)
	requests := mockHTTP.GetRequests()
	require.Len(t, requests, 1)
	assert.Empty(t, requests[0].Data.Get("client_secret"), "public client should not send client_secret")
	assert.Equal(t, "test-verifier", requests[0].Data.Get("code_verifier"))
}

func TestAuthCodeLogin_CustomCallbackPort(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(DefaultConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	var capturedAuthURL string
	originalOpener := BrowserOpener
	BrowserOpener = func(ctx context.Context, authURL string) error {
		capturedAuthURL = authURL
		parsed, _ := url.Parse(authURL)
		redirectURI := parsed.Query().Get("redirect_uri")
		state := parsed.Query().Get("state")
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = simulateBrowserRedirect(redirectURI, "port-test-code", state)
		}()
		return nil
	}
	defer func() { BrowserOpener = originalOpener }()

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "openid profile offline_access",
		"id_token":      authCodeTestIDToken(),
	})

	ctx := context.Background()
	result, err := handler.Login(ctx, auth.LoginOptions{
		Flow:         auth.FlowInteractive,
		Timeout:      10 * time.Second,
		CallbackPort: 18949,
	})

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify the auth URL uses the fixed port
	parsedURL, _ := url.Parse(capturedAuthURL)
	redirectURI := parsedURL.Query().Get("redirect_uri")
	assert.Equal(t, "http://localhost:18949", redirectURI)

	// Verify the token exchange also used the fixed redirect URI
	requests := mockHTTP.GetRequests()
	require.Len(t, requests, 1)
	assert.Equal(t, "http://localhost:18949", requests[0].Data.Get("redirect_uri"))
}

func TestAuthCodeLogin_BrowserError_AADSTS(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(DefaultConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	// Simulate Entra returning an AADSTS500113 error via redirect
	originalOpener := BrowserOpener
	BrowserOpener = func(ctx context.Context, authURL string) error {
		parsed, _ := url.Parse(authURL)
		redirectURI := parsed.Query().Get("redirect_uri")
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = simulateBrowserError(redirectURI, "invalid_request", "AADSTS500113: No reply address is registered for the application.")
		}()
		return nil
	}
	defer func() { BrowserOpener = originalOpener }()

	ctx := context.Background()
	_, err = handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowInteractive,
		Timeout: 10 * time.Second,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "AADSTS500113")
	assert.Contains(t, err.Error(), "redirect URI")
	assert.Contains(t, err.Error(), "http://localhost")
}

func TestAuthCodeLogin_TimeoutMentionsRedirectURI(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(DefaultConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	originalOpener := BrowserOpener
	BrowserOpener = func(_ context.Context, _ string) error {
		return nil
	}
	defer func() { BrowserOpener = originalOpener }()

	ctx := context.Background()
	_, err = handler.Login(ctx, auth.LoginOptions{
		Flow:    auth.FlowInteractive,
		Timeout: 200 * time.Millisecond,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrTimeout)
	// The improved timeout message should mention redirect URI registration
	assert.Contains(t, err.Error(), "redirect URI")
	assert.Contains(t, err.Error(), "device-code")
}

// authCodeTestIDToken creates a minimal test JWT ID token for auth code flow tests.
func authCodeTestIDToken() string {
	// Minimal JWT: header.payload.signature (no actual signing)
	header := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9" // {"alg":"RS256","typ":"JWT"}
	// {"iss":"https://login.microsoftonline.com/test-tenant/v2.0","sub":"test-subject","tid":"test-tenant","email":"test@example.com","preferred_username":"testuser","name":"Test User","iat":1234567890,"exp":9999999999}
	payload := "eyJpc3MiOiJodHRwczovL2xvZ2luLm1pY3Jvc29mdG9ubGluZS5jb20vdGVzdC10ZW5hbnQvdjIuMCIsInN1YiI6InRlc3Qtc3ViamVjdCIsInRpZCI6InRlc3QtdGVuYW50IiwiZW1haWwiOiJ0ZXN0QGV4YW1wbGUuY29tIiwicHJlZmVycmVkX3VzZXJuYW1lIjoidGVzdHVzZXIiLCJuYW1lIjoiVGVzdCBVc2VyIiwiaWF0IjoxMjM0NTY3ODkwLCJleHAiOjk5OTk5OTk5OTl9"
	signature := "placeholder-signature"
	return header + "." + payload + "." + signature
}

// splitScopeString splits a space-separated scope string into individual scopes.
func splitScopeString(s string) []string {
	var scopes []string
	for _, scope := range splitBySpace(s) {
		if scope != "" {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

func splitBySpace(s string) []string {
	result := []string{}
	current := ""
	for _, c := range s {
		if c == ' ' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

// Ensure the oauth package is used (prevents unused import error in tests).
var _ = oauth.GenerateCodeChallenge
