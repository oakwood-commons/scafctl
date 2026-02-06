package entra

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetServicePrincipalCredentials_AllSet(t *testing.T) {
	// Set up environment
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")

	creds := GetServicePrincipalCredentials()

	require.NotNil(t, creds)
	assert.Equal(t, "test-client-id", creds.ClientID)
	assert.Equal(t, "test-tenant-id", creds.TenantID)
	assert.Equal(t, "test-secret", creds.ClientSecret)
}

func TestGetServicePrincipalCredentials_MissingClientID(t *testing.T) {
	// Only set some vars
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")
	os.Unsetenv(EnvAzureClientID)

	creds := GetServicePrincipalCredentials()

	assert.Nil(t, creds)
}

func TestGetServicePrincipalCredentials_MissingTenantID(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")
	os.Unsetenv(EnvAzureTenantID)

	creds := GetServicePrincipalCredentials()

	assert.Nil(t, creds)
}

func TestGetServicePrincipalCredentials_MissingSecret(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	os.Unsetenv(EnvAzureClientSecret)

	creds := GetServicePrincipalCredentials()

	assert.Nil(t, creds)
}

func TestHasServicePrincipalCredentials(t *testing.T) {
	// Clear all first
	os.Unsetenv(EnvAzureClientID)
	os.Unsetenv(EnvAzureTenantID)
	os.Unsetenv(EnvAzureClientSecret)

	assert.False(t, HasServicePrincipalCredentials())

	// Set all
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")

	assert.True(t, HasServicePrincipalCredentials())
}

func TestHandler_SupportedFlows_IncludesServicePrincipal(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	flows := handler.SupportedFlows()

	assert.Contains(t, flows, auth.FlowDeviceCode)
	assert.Contains(t, flows, auth.FlowServicePrincipal)
}

func TestHandler_ServicePrincipalStatus_NoCredentials(t *testing.T) {
	// Clear env vars
	os.Unsetenv(EnvAzureClientID)
	os.Unsetenv(EnvAzureTenantID)
	os.Unsetenv(EnvAzureClientSecret)

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	status, err := handler.servicePrincipalStatus(ctx)

	require.NoError(t, err)
	assert.False(t, status.Authenticated)
}

func TestHandler_ServicePrincipalStatus_WithCredentials(t *testing.T) {
	t.Setenv(EnvAzureClientID, "12345678-1234-1234-1234-123456789012")
	t.Setenv(EnvAzureTenantID, "tenant-id")
	t.Setenv(EnvAzureClientSecret, "secret")

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	status, err := handler.servicePrincipalStatus(ctx)

	require.NoError(t, err)
	assert.True(t, status.Authenticated)
	assert.Equal(t, auth.IdentityTypeServicePrincipal, status.IdentityType)
	assert.Equal(t, "12345678-1234-1234-1234-123456789012", status.ClientID)
	assert.Equal(t, "tenant-id", status.TenantID)
	assert.NotEmpty(t, status.Claims.Name) // Should be formatted as "Service Principal (12345678...)"
}

func TestHandler_ServicePrincipalLogin_Success(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")

	// Mock HTTP client that returns a valid token
	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusOK, TokenResponse{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	})

	store := secrets.NewMockStore()
	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := handler.servicePrincipalLogin(ctx, auth.LoginOptions{
		Flow: auth.FlowServicePrincipal,
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-client-id", result.Claims.ClientID)
	assert.Equal(t, "test-tenant-id", result.Claims.TenantID)
}

func TestHandler_ServicePrincipalLogin_NoCredentials(t *testing.T) {
	os.Unsetenv(EnvAzureClientID)
	os.Unsetenv(EnvAzureTenantID)
	os.Unsetenv(EnvAzureClientSecret)

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.servicePrincipalLogin(ctx, auth.LoginOptions{
		Flow: auth.FlowServicePrincipal,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "service principal credentials not configured")
}

func TestHandler_ServicePrincipalLogin_TokenAcquisitionFailed(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "invalid-secret")

	// Mock HTTP client that returns an error
	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusUnauthorized, map[string]string{
		"error":             "invalid_client",
		"error_description": "Invalid client credentials",
	})

	store := secrets.NewMockStore()
	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.servicePrincipalLogin(ctx, auth.LoginOptions{
		Flow: auth.FlowServicePrincipal,
	})

	require.Error(t, err)
}

func TestHandler_GetServicePrincipalToken_Success(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")

	// Mock HTTP client
	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusOK, TokenResponse{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	})

	store := secrets.NewMockStore()
	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()
	token, err := handler.getServicePrincipalToken(ctx, auth.TokenOptions{
		Scope: "https://management.azure.com/.default",
	})

	require.NoError(t, err)
	assert.Equal(t, "test-access-token", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.Equal(t, "https://management.azure.com/.default", token.Scope)

	// Verify request was made with correct parameters
	requests := mockHTTP.GetRequests()
	require.Len(t, requests, 1)
	assert.Equal(t, "client_credentials", requests[0].Data.Get("grant_type"))
	assert.Equal(t, "test-client-id", requests[0].Data.Get("client_id"))
	assert.Equal(t, "test-secret", requests[0].Data.Get("client_secret"))
	assert.Equal(t, "https://management.azure.com/.default", requests[0].Data.Get("scope"))
}

func TestHandler_GetServicePrincipalToken_UsesCache(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")

	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusOK, TokenResponse{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	})

	store := secrets.NewMockStore()
	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// First call should hit the endpoint
	_, err = handler.getServicePrincipalToken(ctx, auth.TokenOptions{
		Scope: "https://management.azure.com/.default",
	})
	require.NoError(t, err)
	assert.Len(t, mockHTTP.GetRequests(), 1)

	// Second call should use cache
	_, err = handler.getServicePrincipalToken(ctx, auth.TokenOptions{
		Scope: "https://management.azure.com/.default",
	})
	require.NoError(t, err)
	assert.Len(t, mockHTTP.GetRequests(), 1) // Still 1, cache was used
}

func TestHandler_GetServicePrincipalToken_ForceRefreshBypassesCache(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")

	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusOK, TokenResponse{
		AccessToken: "test-access-token-1",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	})
	mockHTTP.AddResponse(http.StatusOK, TokenResponse{
		AccessToken: "test-access-token-2",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	})

	store := secrets.NewMockStore()
	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// First call
	token1, err := handler.getServicePrincipalToken(ctx, auth.TokenOptions{
		Scope: "https://management.azure.com/.default",
	})
	require.NoError(t, err)
	assert.Equal(t, "test-access-token-1", token1.AccessToken)

	// Second call with ForceRefresh
	token2, err := handler.getServicePrincipalToken(ctx, auth.TokenOptions{
		Scope:        "https://management.azure.com/.default",
		ForceRefresh: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "test-access-token-2", token2.AccessToken)
	assert.Len(t, mockHTTP.GetRequests(), 2) // Called again due to ForceRefresh
}

func TestHandler_GetServicePrincipalToken_MissingScope(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.getServicePrincipalToken(ctx, auth.TokenOptions{
		Scope: "",
	})

	require.Error(t, err)
	assert.Equal(t, auth.ErrInvalidScope, err)
}

func TestHandler_GetServicePrincipalToken_NoCredentials(t *testing.T) {
	os.Unsetenv(EnvAzureClientID)
	os.Unsetenv(EnvAzureTenantID)
	os.Unsetenv(EnvAzureClientSecret)

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.getServicePrincipalToken(ctx, auth.TokenOptions{
		Scope: "https://management.azure.com/.default",
	})

	require.Error(t, err)
	assert.Equal(t, auth.ErrNotAuthenticated, err)
}

func TestHandler_Login_RouteToServicePrincipal(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")

	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusOK, TokenResponse{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	})

	store := secrets.NewMockStore()
	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := handler.Login(ctx, auth.LoginOptions{
		Flow: auth.FlowServicePrincipal,
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-client-id", result.Claims.ClientID)
}

func TestHandler_Login_AutoDetectServicePrincipal(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")

	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusOK, TokenResponse{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	})

	store := secrets.NewMockStore()
	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()
	// Empty flow - should auto-detect SP credentials
	result, err := handler.Login(ctx, auth.LoginOptions{
		Flow: "",
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-client-id", result.Claims.ClientID)
}

func TestHandler_Status_WithServicePrincipalCredentials(t *testing.T) {
	t.Setenv(EnvAzureClientID, "12345678-1234-1234-1234-123456789012")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	status, err := handler.Status(ctx)

	require.NoError(t, err)
	assert.True(t, status.Authenticated)
	assert.Equal(t, auth.IdentityTypeServicePrincipal, status.IdentityType)
	assert.Equal(t, "12345678-1234-1234-1234-123456789012", status.ClientID)
}

func TestHandler_GetToken_WithServicePrincipalCredentials(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")

	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusOK, TokenResponse{
		AccessToken: "sp-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	})

	store := secrets.NewMockStore()
	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	ctx := context.Background()
	token, err := handler.GetToken(ctx, auth.TokenOptions{
		Scope: "https://management.azure.com/.default",
	})

	require.NoError(t, err)
	assert.Equal(t, "sp-access-token", token.AccessToken)
}
