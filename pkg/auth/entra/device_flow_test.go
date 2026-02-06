package entra

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fastPollConfig returns a config with minimal poll interval for fast tests.
func fastPollConfig() *Config {
	cfg := DefaultConfig()
	cfg.MinPollInterval = 10 * time.Millisecond
	cfg.SlowDownIncrement = 10 * time.Millisecond
	return cfg
}

func TestDeviceCodeLogin_Success(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(fastPollConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// Mock device code response
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "ABCD-1234",
		"verification_uri": "https://microsoft.com/devicelogin",
		"expires_in":       900,
		"interval":         0,
		"message":          "To sign in, use a web browser to open the page...",
	})

	// Mock token response (immediate success for test)
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "openid profile offline_access",
	})

	var callbackCalled bool
	var receivedUserCode string

	result, err := handler.Login(ctx, auth.LoginOptions{
		Timeout: 30 * time.Second,
		DeviceCodeCallback: func(userCode, verificationURI, message string) {
			callbackCalled = true
			receivedUserCode = userCode
		},
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, callbackCalled)
	assert.Equal(t, "ABCD-1234", receivedUserCode)

	// Verify credentials were stored
	exists, _ := store.Exists(ctx, SecretKeyRefreshToken)
	assert.True(t, exists)
	exists, _ = store.Exists(ctx, SecretKeyMetadata)
	assert.True(t, exists)
}

func TestDeviceCodeLogin_CustomTenantAndScopes(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(fastPollConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "ABCD-1234",
		"verification_uri": "https://microsoft.com/devicelogin",
		"expires_in":       900,
		"interval":         0,
		"message":          "Test message",
	})

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "custom-scope offline_access",
	})

	_, err = handler.Login(ctx, auth.LoginOptions{
		TenantID: "custom-tenant",
		Scopes:   []string{"custom-scope"},
		Timeout:  30 * time.Second,
	})

	require.NoError(t, err)

	// Verify the device code request used custom tenant
	requests := mockHTTP.GetRequests()
	require.Len(t, requests, 2)
	assert.Contains(t, requests[0].Endpoint, "custom-tenant")
	// offline_access should be auto-added
	assert.Contains(t, requests[0].Data.Get("scope"), "offline_access")
}

func TestDeviceCodeLogin_AuthorizationPending(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(fastPollConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// Mock device code response
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "ABCD-1234",
		"verification_uri": "https://microsoft.com/devicelogin",
		"expires_in":       900,
		"interval":         0,
		"message":          "Test message",
	})

	// First poll: pending
	mockHTTP.AddResponse(http.StatusBadRequest, map[string]string{
		"error": "authorization_pending",
	})

	// Second poll: success
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "openid profile offline_access",
	})

	result, err := handler.Login(ctx, auth.LoginOptions{
		Timeout: 30 * time.Second,
	})

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Should have made 3 requests: device code + 2 token polls
	assert.Len(t, mockHTTP.GetRequests(), 3)
}

func TestDeviceCodeLogin_UserDeclined(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(fastPollConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "ABCD-1234",
		"verification_uri": "https://microsoft.com/devicelogin",
		"expires_in":       900,
		"interval":         0,
		"message":          "Test message",
	})

	mockHTTP.AddResponse(http.StatusBadRequest, map[string]string{
		"error":             "authorization_declined",
		"error_description": "The user declined to authorize",
	})

	_, err = handler.Login(ctx, auth.LoginOptions{
		Timeout: 30 * time.Second,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrUserCancelled)
}

func TestDeviceCodeLogin_Timeout(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(fastPollConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "ABCD-1234",
		"verification_uri": "https://microsoft.com/devicelogin",
		"expires_in":       900,
		"interval":         0, // 1 second poll interval
		"message":          "Test message",
	})

	// Keep returning pending - will timeout
	for range 20 {
		mockHTTP.AddResponse(http.StatusBadRequest, map[string]string{
			"error": "authorization_pending",
		})
	}

	_, err = handler.Login(ctx, auth.LoginOptions{
		Timeout: 100 * time.Millisecond, // Very short timeout for fast test
	})

	require.Error(t, err)
	// Should be a timeout error (wrapped in auth.Error)
	var authErr *auth.Error
	assert.ErrorAs(t, err, &authErr)
}

func TestDeviceCodeLogin_DeviceCodeRequestFailed(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(fastPollConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	mockHTTP.AddResponse(http.StatusBadRequest, map[string]string{
		"error":             "invalid_client",
		"error_description": "Client not found",
	})

	_, err = handler.Login(ctx, auth.LoginOptions{
		Timeout: 30 * time.Second,
	})

	require.Error(t, err)
	var authErr *auth.Error
	assert.ErrorAs(t, err, &authErr)
	assert.Equal(t, "device_code_request", authErr.Operation)
}

func TestDeviceCodeLogin_SlowDown(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(fastPollConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "ABCD-1234",
		"verification_uri": "https://microsoft.com/devicelogin",
		"expires_in":       900,
		"interval":         0,
		"message":          "Test message",
	})

	// Slow down response
	mockHTTP.AddResponse(http.StatusBadRequest, map[string]string{
		"error": "slow_down",
	})

	// Then success
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "openid",
	})

	result, err := handler.Login(ctx, auth.LoginOptions{
		Timeout: 30 * time.Second,
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestDeviceCodeLogin_ExpiredToken(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithConfig(fastPollConfig()),
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"device_code":      "test-device-code",
		"user_code":        "ABCD-1234",
		"verification_uri": "https://microsoft.com/devicelogin",
		"expires_in":       900,
		"interval":         0,
		"message":          "Test message",
	})

	mockHTTP.AddResponse(http.StatusBadRequest, map[string]string{
		"error": "expired_token",
	})

	_, err = handler.Login(ctx, auth.LoginOptions{
		Timeout: 30 * time.Second,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrTimeout)
}
