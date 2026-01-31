package entra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Integration Tests with Mock OAuth Server
// These tests use httptest.Server to simulate real Entra OAuth endpoints
// ============================================================================

// mockEntraServer creates a test server that simulates Entra ID OAuth endpoints.
type mockEntraServer struct {
	server *httptest.Server
	t      *testing.T

	// Device code endpoint configuration
	deviceCodeResponse     map[string]any
	deviceCodeStatusCode   int
	deviceCodeRequestCount int32

	// Token endpoint configuration
	tokenResponses     []tokenEndpointResponse
	tokenResponseIndex int32

	// Tracking
	lastDeviceCodeRequest map[string]string
	lastTokenRequest      map[string]string
}

type tokenEndpointResponse struct {
	statusCode int
	body       map[string]any
}

func newMockEntraServer(t *testing.T) *mockEntraServer {
	m := &mockEntraServer{
		t:                    t,
		deviceCodeStatusCode: http.StatusOK,
	}

	mux := http.NewServeMux()

	// Device code endpoint - matches /{tenantID}/oauth2/v2.0/devicecode
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Route based on path suffix
		path := r.URL.Path

		if containsSuffix(path, "/oauth2/v2.0/devicecode") {
			m.handleDeviceCode(w, r)
			return
		}

		if containsSuffix(path, "/oauth2/v2.0/token") {
			m.handleToken(w, r)
			return
		}

		// Unknown endpoint
		w.WriteHeader(http.StatusNotFound)
	})

	m.server = httptest.NewServer(mux)
	return m
}

func containsSuffix(path, suffix string) bool {
	return len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix
}

func (m *mockEntraServer) handleDeviceCode(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt32(&m.deviceCodeRequestCount, 1)

	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	m.lastDeviceCodeRequest = make(map[string]string)
	for k, v := range r.Form {
		if len(v) > 0 {
			m.lastDeviceCodeRequest[k] = v[0]
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(m.deviceCodeStatusCode)
	if m.deviceCodeResponse != nil {
		_ = json.NewEncoder(w).Encode(m.deviceCodeResponse)
	}
}

func (m *mockEntraServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	m.lastTokenRequest = make(map[string]string)
	for k, v := range r.Form {
		if len(v) > 0 {
			m.lastTokenRequest[k] = v[0]
		}
	}

	idx := atomic.AddInt32(&m.tokenResponseIndex, 1) - 1
	if int(idx) < len(m.tokenResponses) {
		resp := m.tokenResponses[idx]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.statusCode)
		_ = json.NewEncoder(w).Encode(resp.body)
	} else if len(m.tokenResponses) > 0 {
		// Return last response if we've exhausted the queue
		resp := m.tokenResponses[len(m.tokenResponses)-1]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.statusCode)
		_ = json.NewEncoder(w).Encode(resp.body)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (m *mockEntraServer) Close() {
	m.server.Close()
}

func (m *mockEntraServer) URL() string {
	return m.server.URL
}

func (m *mockEntraServer) SetDeviceCodeResponse(statusCode int, body map[string]any) {
	m.deviceCodeStatusCode = statusCode
	m.deviceCodeResponse = body
}

func (m *mockEntraServer) AddTokenResponse(statusCode int, body map[string]any) {
	m.tokenResponses = append(m.tokenResponses, tokenEndpointResponse{
		statusCode: statusCode,
		body:       body,
	})
}

// ============================================================================
// Device Code Flow Integration Tests
// ============================================================================

func TestIntegration_DeviceCodeFlow_Success(t *testing.T) {
	server := newMockEntraServer(t)
	defer server.Close()

	// Configure device code response
	server.SetDeviceCodeResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code-12345",
		"user_code":        "ABCD-EFGH",
		"verification_uri": "https://microsoft.com/devicelogin",
		"expires_in":       900,
		"interval":         0, // No delay for tests
		"message":          "To sign in, use a web browser...",
	})

	// Configure token response (immediate success)
	server.AddTokenResponse(http.StatusOK, map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "openid profile offline_access",
		"id_token":      createTestIDToken(t),
	})

	// Create handler with custom authority pointing to test server
	store := secrets.NewMockStore()
	cfg := &Config{
		ClientID:        "test-client-id",
		TenantID:        "test-tenant",
		Authority:       server.URL(),
		MinPollInterval: 10 * time.Millisecond,
	}

	handler, err := New(
		WithConfig(cfg),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	ctx := context.Background()

	var callbackInvoked bool
	var receivedUserCode string
	var receivedVerificationURI string

	result, err := handler.Login(ctx, auth.LoginOptions{
		Timeout: 5 * time.Second,
		DeviceCodeCallback: func(userCode, verificationURI, message string) {
			callbackInvoked = true
			receivedUserCode = userCode
			receivedVerificationURI = verificationURI
		},
	})

	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify callback was invoked with correct values
	assert.True(t, callbackInvoked)
	assert.Equal(t, "ABCD-EFGH", receivedUserCode)
	assert.Equal(t, "https://microsoft.com/devicelogin", receivedVerificationURI)

	// Verify device code request was made correctly
	assert.Equal(t, "test-client-id", server.lastDeviceCodeRequest["client_id"])
	assert.Contains(t, server.lastDeviceCodeRequest["scope"], "openid")

	// Verify credentials were stored
	exists, _ := store.Exists(ctx, SecretKeyRefreshToken)
	assert.True(t, exists)

	refreshToken, _ := store.Get(ctx, SecretKeyRefreshToken)
	assert.Equal(t, "test-refresh-token", string(refreshToken))
}

func TestIntegration_DeviceCodeFlow_AuthorizationPending(t *testing.T) {
	server := newMockEntraServer(t)
	defer server.Close()

	server.SetDeviceCodeResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "TEST-1234",
		"verification_uri": "https://microsoft.com/devicelogin",
		"expires_in":       900,
		"interval":         0,
		"message":          "Waiting...",
	})

	// First response: authorization pending
	server.AddTokenResponse(http.StatusBadRequest, map[string]any{
		"error":             "authorization_pending",
		"error_description": "The user has not yet authenticated",
	})

	// Second response: success
	server.AddTokenResponse(http.StatusOK, map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
	})

	store := secrets.NewMockStore()
	cfg := &Config{
		ClientID:        "test-client-id",
		TenantID:        "test-tenant",
		Authority:       server.URL(),
		MinPollInterval: 10 * time.Millisecond,
	}

	handler, err := New(
		WithConfig(cfg),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	ctx := context.Background()

	result, err := handler.Login(ctx, auth.LoginOptions{
		Timeout:            5 * time.Second,
		DeviceCodeCallback: func(userCode, verificationURI, message string) {},
	})

	require.NoError(t, err)
	require.NotNil(t, result)

	// Should have polled twice (pending + success)
	assert.GreaterOrEqual(t, int(atomic.LoadInt32(&server.tokenResponseIndex)), 2)
}

func TestIntegration_DeviceCodeFlow_AccessDenied(t *testing.T) {
	server := newMockEntraServer(t)
	defer server.Close()

	server.SetDeviceCodeResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "TEST-1234",
		"verification_uri": "https://microsoft.com/devicelogin",
		"expires_in":       900,
		"interval":         0,
		"message":          "Please authenticate...",
	})

	// User denied access - Azure returns "authorization_declined"
	server.AddTokenResponse(http.StatusBadRequest, map[string]any{
		"error":             "authorization_declined",
		"error_description": "The user declined the authorization request",
	})

	store := secrets.NewMockStore()
	cfg := &Config{
		ClientID:        "test-client-id",
		TenantID:        "test-tenant",
		Authority:       server.URL(),
		MinPollInterval: 10 * time.Millisecond,
	}

	handler, err := New(
		WithConfig(cfg),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	ctx := context.Background()

	result, err := handler.Login(ctx, auth.LoginOptions{
		Timeout:            5 * time.Second,
		DeviceCodeCallback: func(userCode, verificationURI, message string) {},
	})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, auth.ErrUserCancelled)
}

func TestIntegration_DeviceCodeFlow_ExpiredCode(t *testing.T) {
	server := newMockEntraServer(t)
	defer server.Close()

	server.SetDeviceCodeResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "TEST-1234",
		"verification_uri": "https://microsoft.com/devicelogin",
		"expires_in":       900,
		"interval":         0,
		"message":          "Please authenticate...",
	})

	// Code expired
	server.AddTokenResponse(http.StatusBadRequest, map[string]any{
		"error":             "expired_token",
		"error_description": "The device code has expired",
	})

	store := secrets.NewMockStore()
	cfg := &Config{
		ClientID:        "test-client-id",
		TenantID:        "test-tenant",
		Authority:       server.URL(),
		MinPollInterval: 10 * time.Millisecond,
	}

	handler, err := New(
		WithConfig(cfg),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	ctx := context.Background()

	result, err := handler.Login(ctx, auth.LoginOptions{
		Timeout:            5 * time.Second,
		DeviceCodeCallback: func(userCode, verificationURI, message string) {},
	})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, auth.ErrTimeout)
}

// ============================================================================
// Token Refresh Integration Tests
// ============================================================================

func TestIntegration_TokenRefresh_Success(t *testing.T) {
	server := newMockEntraServer(t)
	defer server.Close()

	// Configure token refresh response
	server.AddTokenResponse(http.StatusOK, map[string]any{
		"access_token":  "new-access-token",
		"refresh_token": "new-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "https://graph.microsoft.com/.default",
	})

	store := secrets.NewMockStore()
	ctx := context.Background()

	// Pre-populate with existing refresh token
	err := store.Set(ctx, SecretKeyRefreshToken, []byte("existing-refresh-token"))
	require.NoError(t, err)

	metadata := &TokenMetadata{
		Claims:                &auth.Claims{Subject: "test-user"},
		RefreshTokenExpiresAt: time.Now().Add(24 * time.Hour),
		TenantID:              "test-tenant",
	}
	metadataBytes, _ := json.Marshal(metadata)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	cfg := &Config{
		ClientID:  "test-client-id",
		TenantID:  "test-tenant",
		Authority: server.URL(),
	}

	handler, err := New(
		WithConfig(cfg),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	// Request a token
	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope: "https://graph.microsoft.com/.default",
	})

	require.NoError(t, err)
	require.NotNil(t, token)
	assert.Equal(t, "new-access-token", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)

	// Verify refresh token request was made correctly
	assert.Equal(t, "refresh_token", server.lastTokenRequest["grant_type"])
	assert.Equal(t, "existing-refresh-token", server.lastTokenRequest["refresh_token"])
	assert.Equal(t, "test-client-id", server.lastTokenRequest["client_id"])
}

func TestIntegration_TokenRefresh_InvalidGrant(t *testing.T) {
	server := newMockEntraServer(t)
	defer server.Close()

	// Refresh token is invalid/revoked
	server.AddTokenResponse(http.StatusBadRequest, map[string]any{
		"error":             "invalid_grant",
		"error_description": "The refresh token has been revoked",
	})

	store := secrets.NewMockStore()
	ctx := context.Background()

	// Pre-populate with existing refresh token
	err := store.Set(ctx, SecretKeyRefreshToken, []byte("revoked-refresh-token"))
	require.NoError(t, err)

	metadata := &TokenMetadata{
		Claims:                &auth.Claims{Subject: "test-user"},
		RefreshTokenExpiresAt: time.Now().Add(24 * time.Hour),
		TenantID:              "test-tenant",
	}
	metadataBytes, _ := json.Marshal(metadata)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	cfg := &Config{
		ClientID:  "test-client-id",
		TenantID:  "test-tenant",
		Authority: server.URL(),
	}

	handler, err := New(
		WithConfig(cfg),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	// Request a token
	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope: "https://graph.microsoft.com/.default",
	})

	require.Error(t, err)
	assert.Nil(t, token)
	// Should return token expired error since refresh token is invalid
	assert.ErrorIs(t, err, auth.ErrTokenExpired)
}

// ============================================================================
// Token Caching Integration Tests
// ============================================================================

func TestIntegration_TokenCaching_UseCachedToken(t *testing.T) {
	server := newMockEntraServer(t)
	defer server.Close()

	store := secrets.NewMockStore()
	ctx := context.Background()
	scope := "https://graph.microsoft.com/.default"

	// Pre-populate with existing refresh token
	err := store.Set(ctx, SecretKeyRefreshToken, []byte("refresh-token"))
	require.NoError(t, err)

	metadata := &TokenMetadata{
		Claims:                &auth.Claims{Subject: "test-user"},
		RefreshTokenExpiresAt: time.Now().Add(24 * time.Hour),
		TenantID:              "test-tenant",
	}
	metadataBytes, _ := json.Marshal(metadata)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	// Pre-populate cache with a valid token
	cache := NewTokenCache(store)
	cachedToken := &auth.Token{
		AccessToken: "cached-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		Scope:       scope,
	}
	err = cache.Set(ctx, scope, cachedToken)
	require.NoError(t, err)

	cfg := &Config{
		ClientID:  "test-client-id",
		TenantID:  "test-tenant",
		Authority: server.URL(),
	}

	handler, err := New(
		WithConfig(cfg),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	// Request a token
	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope:       scope,
		MinValidFor: 5 * time.Minute,
	})

	require.NoError(t, err)
	require.NotNil(t, token)

	// Should use cached token (no server request)
	assert.Equal(t, "cached-access-token", token.AccessToken)
	assert.Equal(t, int32(0), atomic.LoadInt32(&server.tokenResponseIndex))
}

func TestIntegration_TokenCaching_MinValidFor_RefreshNeeded(t *testing.T) {
	server := newMockEntraServer(t)
	defer server.Close()

	// Configure token refresh response
	server.AddTokenResponse(http.StatusOK, map[string]any{
		"access_token":  "fresh-access-token",
		"refresh_token": "new-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
	})

	store := secrets.NewMockStore()
	ctx := context.Background()
	scope := "https://graph.microsoft.com/.default"

	// Pre-populate with existing refresh token
	err := store.Set(ctx, SecretKeyRefreshToken, []byte("refresh-token"))
	require.NoError(t, err)

	metadata := &TokenMetadata{
		Claims:                &auth.Claims{Subject: "test-user"},
		RefreshTokenExpiresAt: time.Now().Add(24 * time.Hour),
		TenantID:              "test-tenant",
	}
	metadataBytes, _ := json.Marshal(metadata)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	// Pre-populate cache with a token that expires soon
	cache := NewTokenCache(store)
	cachedToken := &auth.Token{
		AccessToken: "expiring-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(2 * time.Minute), // Expires in 2 minutes
		Scope:       scope,
	}
	err = cache.Set(ctx, scope, cachedToken)
	require.NoError(t, err)

	cfg := &Config{
		ClientID:  "test-client-id",
		TenantID:  "test-tenant",
		Authority: server.URL(),
	}

	handler, err := New(
		WithConfig(cfg),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	// Request a token that must be valid for 5 minutes
	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope:       scope,
		MinValidFor: 5 * time.Minute, // Cached token only valid for 2 min
	})

	require.NoError(t, err)
	require.NotNil(t, token)

	// Should have refreshed (cache token didn't meet MinValidFor)
	assert.Equal(t, "fresh-access-token", token.AccessToken)
	assert.Equal(t, int32(1), atomic.LoadInt32(&server.tokenResponseIndex))
}

func TestIntegration_TokenCaching_ForceRefresh(t *testing.T) {
	server := newMockEntraServer(t)
	defer server.Close()

	// Configure token refresh response
	server.AddTokenResponse(http.StatusOK, map[string]any{
		"access_token":  "force-refreshed-token",
		"refresh_token": "new-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
	})

	store := secrets.NewMockStore()
	ctx := context.Background()
	scope := "https://graph.microsoft.com/.default"

	// Pre-populate with existing refresh token
	err := store.Set(ctx, SecretKeyRefreshToken, []byte("refresh-token"))
	require.NoError(t, err)

	metadata := &TokenMetadata{
		Claims:                &auth.Claims{Subject: "test-user"},
		RefreshTokenExpiresAt: time.Now().Add(24 * time.Hour),
		TenantID:              "test-tenant",
	}
	metadataBytes, _ := json.Marshal(metadata)
	err = store.Set(ctx, SecretKeyMetadata, metadataBytes)
	require.NoError(t, err)

	// Pre-populate cache with a perfectly valid token
	cache := NewTokenCache(store)
	cachedToken := &auth.Token{
		AccessToken: "cached-but-should-skip",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
	}
	err = cache.Set(ctx, scope, cachedToken)
	require.NoError(t, err)

	cfg := &Config{
		ClientID:  "test-client-id",
		TenantID:  "test-tenant",
		Authority: server.URL(),
	}

	handler, err := New(
		WithConfig(cfg),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	// Request a token with ForceRefresh
	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope:        scope,
		ForceRefresh: true,
	})

	require.NoError(t, err)
	require.NotNil(t, token)

	// Should have refreshed despite valid cache
	assert.Equal(t, "force-refreshed-token", token.AccessToken)
	assert.Equal(t, int32(1), atomic.LoadInt32(&server.tokenResponseIndex))
}

// ============================================================================
// Helper Functions
// ============================================================================

// createTestIDToken creates a minimal JWT-like ID token for testing.
// This is NOT a valid JWT, just enough structure for parsing.
func createTestIDToken(t *testing.T) string {
	t.Helper()

	// Create a simple payload
	payload := map[string]any{
		"iss":                "https://login.microsoftonline.com/test-tenant/v2.0",
		"sub":                "test-subject-id",
		"aud":                "test-client-id",
		"exp":                time.Now().Add(time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"name":               "Test User",
		"preferred_username": "testuser@example.com",
		"tid":                "test-tenant-id",
		"oid":                "test-object-id",
	}

	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	// Create a fake JWT (header.payload.signature)
	// The handler should be able to extract claims from the payload
	header := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9" // {"alg":"RS256","typ":"JWT"}
	payloadB64 := base64URLEncode(payloadBytes)
	signature := "fake-signature"

	return header + "." + payloadB64 + "." + signature
}

func base64URLEncode(data []byte) string {
	encoded := make([]byte, len(data)*2)
	n := copy(encoded, data)
	// Simple base64url encoding without padding
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	result := make([]byte, 0, (n*8+5)/6)
	for i := 0; i < n; i += 3 {
		var b uint32
		remaining := n - i
		if remaining >= 3 {
			b = uint32(data[i])<<16 | uint32(data[i+1])<<8 | uint32(data[i+2])
			result = append(result, alphabet[b>>18], alphabet[(b>>12)&63], alphabet[(b>>6)&63], alphabet[b&63])
		} else if remaining == 2 {
			b = uint32(data[i])<<16 | uint32(data[i+1])<<8
			result = append(result, alphabet[b>>18], alphabet[(b>>12)&63], alphabet[(b>>6)&63])
		} else {
			b = uint32(data[i]) << 16
			result = append(result, alphabet[b>>18], alphabet[(b>>12)&63])
		}
	}
	return string(result)
}
