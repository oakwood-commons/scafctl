// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeIDToken(t testing.TB, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	body := base64.RawURLEncoding.EncodeToString(payload)
	return fmt.Sprintf("%s.%s.fakesig", header, body)
}

func TestDeviceCodeLogin_Success(t *testing.T) {
	t.Parallel()
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	idToken := makeIDToken(t, map[string]any{
		"sub":   "12345",
		"email": "user@example.com",
		"name":  "Test User",
		"iss":   "https://accounts.google.com",
	})

	// Response 1: device code request
	mockHTTP.AddResponse(200, DeviceCodeResponse{
		DeviceCode:      "test-device-code",
		UserCode:        "ABCD-1234",
		VerificationURL: "https://www.google.com/device",
		ExpiresIn:       1800,
		Interval:        1,
	})

	// Response 2: token response (immediate success)
	mockHTTP.AddResponse(200, TokenResponse{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "openid email",
		IDToken:      idToken,
	})

	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	var callbackCalled bool
	var callbackCode, callbackURL string

	result, err := handler.deviceCodeLogin(context.Background(), auth.LoginOptions{
		Flow:    auth.FlowDeviceCode,
		Timeout: 10 * time.Second,
		DeviceCodeCallback: func(code, url, message string) {
			callbackCalled = true
			callbackCode = code
			callbackURL = url
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, callbackCalled)
	assert.Equal(t, "ABCD-1234", callbackCode)
	assert.Equal(t, "https://www.google.com/device", callbackURL)
	assert.Equal(t, "user@example.com", result.Claims.Email)
	assert.Equal(t, "Test User", result.Claims.Name)

	// Verify device code request was made correctly
	require.Len(t, mockHTTP.Requests, 2)
	assert.Equal(t, deviceCodeEndpoint, mockHTTP.Requests[0].Endpoint)
	assert.Equal(t, DefaultADCClientID, mockHTTP.Requests[0].Data.Get("client_id"))

	// Verify token request
	assert.Equal(t, tokenEndpoint, mockHTTP.Requests[1].Endpoint)
	assert.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", mockHTTP.Requests[1].Data.Get("grant_type"))
	assert.Equal(t, "test-device-code", mockHTTP.Requests[1].Data.Get("device_code"))
}

func TestDeviceCodeLogin_CustomClientID(t *testing.T) {
	t.Parallel()
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	idToken := makeIDToken(t, map[string]any{
		"sub":   "12345",
		"email": "user@example.com",
		"iss":   "https://accounts.google.com",
	})

	mockHTTP.AddResponse(200, DeviceCodeResponse{
		DeviceCode:      "dc",
		UserCode:        "CODE",
		VerificationURL: "https://www.google.com/device",
		ExpiresIn:       1800,
		Interval:        1,
	})
	mockHTTP.AddResponse(200, TokenResponse{
		AccessToken: "token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
		IDToken:     idToken,
	})

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
		WithConfig(&Config{ClientID: "custom-id", ClientSecret: "custom-secret"}),
	)
	require.NoError(t, err)

	result, err := handler.deviceCodeLogin(context.Background(), auth.LoginOptions{
		Timeout: 10 * time.Second,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify custom client ID was used
	assert.Equal(t, "custom-id", mockHTTP.Requests[0].Data.Get("client_id"))
	assert.Equal(t, "custom-id", mockHTTP.Requests[1].Data.Get("client_id"))
	assert.Equal(t, "custom-secret", mockHTTP.Requests[1].Data.Get("client_secret"))
}

func TestDeviceCodeLogin_PollPending(t *testing.T) {
	t.Parallel()
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	idToken := makeIDToken(t, map[string]any{
		"sub": "12345", "email": "user@example.com", "iss": "https://accounts.google.com",
	})

	// Device code response
	mockHTTP.AddResponse(200, DeviceCodeResponse{
		DeviceCode:      "dc",
		UserCode:        "CODE",
		VerificationURL: "https://www.google.com/device",
		ExpiresIn:       1800,
		Interval:        0, // Will be floored to defaultDeviceCodePollInterval
	})

	// First poll: authorization_pending
	mockHTTP.AddResponse(200, struct {
		TokenResponse
		TokenErrorResponse
	}{TokenErrorResponse: TokenErrorResponse{Error: "authorization_pending"}})

	// Second poll: success
	mockHTTP.AddResponse(200, TokenResponse{
		AccessToken: "token", TokenType: "Bearer", ExpiresIn: 3600, IDToken: idToken,
	})

	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	result, err := handler.deviceCodeLogin(context.Background(), auth.LoginOptions{
		Timeout: 30 * time.Second,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should have made 3 requests: device code + 2 polls
	assert.Len(t, mockHTTP.Requests, 3)
}

func TestDeviceCodeLogin_AccessDenied(t *testing.T) {
	t.Parallel()
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	mockHTTP.AddResponse(200, DeviceCodeResponse{
		DeviceCode:      "dc",
		UserCode:        "CODE",
		VerificationURL: "https://www.google.com/device",
		ExpiresIn:       1800,
		Interval:        0,
	})

	mockHTTP.AddResponse(200, struct {
		TokenResponse
		TokenErrorResponse
	}{TokenErrorResponse: TokenErrorResponse{Error: "access_denied"}})

	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	_, err = handler.deviceCodeLogin(context.Background(), auth.LoginOptions{
		Timeout: 10 * time.Second,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrUserCancelled)
}

func TestDeviceCodeLogin_ExpiredToken(t *testing.T) {
	t.Parallel()
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	mockHTTP.AddResponse(200, DeviceCodeResponse{
		DeviceCode:      "dc",
		UserCode:        "CODE",
		VerificationURL: "https://www.google.com/device",
		ExpiresIn:       1800,
		Interval:        0,
	})

	mockHTTP.AddResponse(200, struct {
		TokenResponse
		TokenErrorResponse
	}{TokenErrorResponse: TokenErrorResponse{Error: "expired_token"}})

	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	_, err = handler.deviceCodeLogin(context.Background(), auth.LoginOptions{
		Timeout: 10 * time.Second,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrTimeout)
}

func TestDeviceCodeLogin_DeviceCodeRequestFails(t *testing.T) {
	t.Parallel()
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	mockHTTP.AddResponse(400, TokenErrorResponse{
		Error:            "invalid_client",
		ErrorDescription: "bad client ID",
	})

	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	_, err = handler.deviceCodeLogin(context.Background(), auth.LoginOptions{
		Timeout: 10 * time.Second,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device code request failed")
}

func TestDeviceCodeLogin_DefaultScopes(t *testing.T) {
	t.Parallel()
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	idToken := makeIDToken(t, map[string]any{
		"sub": "12345", "email": "user@example.com", "iss": "https://accounts.google.com",
	})

	mockHTTP.AddResponse(200, DeviceCodeResponse{
		DeviceCode: "dc", UserCode: "CODE", VerificationURL: "https://www.google.com/device",
		ExpiresIn: 1800, Interval: 1,
	})
	mockHTTP.AddResponse(200, TokenResponse{
		AccessToken: "token", TokenType: "Bearer", ExpiresIn: 3600, IDToken: idToken,
	})

	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	_, err = handler.deviceCodeLogin(context.Background(), auth.LoginOptions{
		Timeout: 10 * time.Second,
	})
	require.NoError(t, err)

	// Should use default scopes
	scopeParam := mockHTTP.Requests[0].Data.Get("scope")
	assert.Contains(t, scopeParam, "openid")
	assert.Contains(t, scopeParam, "email")
	assert.Contains(t, scopeParam, "cloud-platform")
}

func TestDeviceCodeLogin_ContextCancelled(t *testing.T) {
	t.Parallel()
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	mockHTTP.AddResponse(200, DeviceCodeResponse{
		DeviceCode: "dc", UserCode: "CODE", VerificationURL: "https://www.google.com/device",
		ExpiresIn: 1800, Interval: 1,
	})

	// No token response — context will cancel first

	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = handler.deviceCodeLogin(ctx, auth.LoginOptions{
		Timeout: 1 * time.Millisecond,
	})
	require.Error(t, err)
}

func TestLogin_RoutesToDeviceCode(t *testing.T) {
	t.Parallel()
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	idToken := makeIDToken(t, map[string]any{
		"sub": "12345", "email": "user@example.com", "iss": "https://accounts.google.com",
	})

	mockHTTP.AddResponse(200, DeviceCodeResponse{
		DeviceCode: "dc", UserCode: "CODE", VerificationURL: "https://www.google.com/device",
		ExpiresIn: 1800, Interval: 1,
	})
	mockHTTP.AddResponse(200, TokenResponse{
		AccessToken: "token", TokenType: "Bearer", ExpiresIn: 3600, IDToken: idToken,
	})

	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	result, err := handler.Login(context.Background(), auth.LoginOptions{
		Flow:    auth.FlowDeviceCode,
		Timeout: 10 * time.Second,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "user@example.com", result.Claims.Email)
}

func BenchmarkDeviceCodeLogin(b *testing.B) {
	store := secrets.NewMockStore()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		mockHTTP := NewMockHTTPClient()
		idToken := makeIDToken(b, map[string]any{
			"sub": "12345", "email": "user@example.com", "iss": "https://accounts.google.com",
		})
		mockHTTP.AddResponse(200, DeviceCodeResponse{
			DeviceCode: "dc", UserCode: "CODE", VerificationURL: "https://www.google.com/device",
			ExpiresIn: 1800, Interval: 1,
		})
		mockHTTP.AddResponse(200, TokenResponse{
			AccessToken: "token", TokenType: "Bearer", ExpiresIn: 3600, IDToken: idToken,
		})
		handler, _ := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
		_, _ = handler.deviceCodeLogin(context.Background(), auth.LoginOptions{
			Timeout: 10 * time.Second,
		})
	}
}
