package entra

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetWorkloadIdentityCredentials_AllSet(t *testing.T) {
	// Create a temp token file
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	err := os.WriteFile(tokenFile, []byte("test-jwt-token"), 0o600)
	require.NoError(t, err)

	// Set up environment
	t.Setenv(EnvAzureFederatedTokenFile, tokenFile)
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")

	creds := GetWorkloadIdentityCredentials()

	require.NotNil(t, creds)
	assert.Equal(t, "test-client-id", creds.ClientID)
	assert.Equal(t, "test-tenant-id", creds.TenantID)
	assert.Equal(t, tokenFile, creds.TokenFile)
	assert.Equal(t, "https://login.microsoftonline.com", creds.Authority)
}

func TestGetWorkloadIdentityCredentials_WithCustomAuthority(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	err := os.WriteFile(tokenFile, []byte("test-jwt-token"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvAzureFederatedTokenFile, tokenFile)
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureAuthorityHost, "https://login.microsoftonline.us")

	creds := GetWorkloadIdentityCredentials()

	require.NotNil(t, creds)
	assert.Equal(t, "https://login.microsoftonline.us", creds.Authority)
}

func TestGetWorkloadIdentityCredentials_MissingTokenFile(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	os.Unsetenv(EnvAzureFederatedTokenFile)
	os.Unsetenv(EnvAzureFederatedToken)

	creds := GetWorkloadIdentityCredentials()

	assert.Nil(t, creds)
}

func TestGetWorkloadIdentityCredentials_DirectToken(t *testing.T) {
	// No token file, but direct token is set
	os.Unsetenv(EnvAzureFederatedTokenFile)
	t.Setenv(EnvAzureFederatedToken, "direct-jwt-token")
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")

	creds := GetWorkloadIdentityCredentials()

	require.NotNil(t, creds)
	assert.Equal(t, "test-client-id", creds.ClientID)
	assert.Equal(t, "test-tenant-id", creds.TenantID)
	assert.Equal(t, "direct-jwt-token", creds.Token)
	assert.Equal(t, "", creds.TokenFile) // No file, just direct token
}

func TestGetWorkloadIdentityCredentials_DirectTokenTakesPrecedence(t *testing.T) {
	// Both token file and direct token are set - direct token takes precedence
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	err := os.WriteFile(tokenFile, []byte("file-jwt-token"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvAzureFederatedTokenFile, tokenFile)
	t.Setenv(EnvAzureFederatedToken, "direct-jwt-token")
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")

	creds := GetWorkloadIdentityCredentials()

	require.NotNil(t, creds)
	assert.Equal(t, "direct-jwt-token", creds.Token)
	assert.Equal(t, tokenFile, creds.TokenFile) // Both are stored

	// GetFederatedToken should return direct token
	token, err := creds.GetFederatedToken()
	require.NoError(t, err)
	assert.Equal(t, "direct-jwt-token", token)
}

func TestGetWorkloadIdentityCredentials_TokenFileDoesNotExist(t *testing.T) {
	t.Setenv(EnvAzureFederatedTokenFile, "/nonexistent/path/to/token")
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")

	creds := GetWorkloadIdentityCredentials()

	assert.Nil(t, creds)
}

func TestGetWorkloadIdentityCredentials_MissingClientID(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	err := os.WriteFile(tokenFile, []byte("test-jwt-token"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvAzureFederatedTokenFile, tokenFile)
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	os.Unsetenv(EnvAzureClientID)

	creds := GetWorkloadIdentityCredentials()

	assert.Nil(t, creds)
}

func TestGetWorkloadIdentityCredentials_MissingTenantID(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	err := os.WriteFile(tokenFile, []byte("test-jwt-token"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvAzureFederatedTokenFile, tokenFile)
	t.Setenv(EnvAzureClientID, "test-client-id")
	os.Unsetenv(EnvAzureTenantID)

	creds := GetWorkloadIdentityCredentials()

	assert.Nil(t, creds)
}

func TestHasWorkloadIdentityCredentials(t *testing.T) {
	// Clear all first
	os.Unsetenv(EnvAzureFederatedTokenFile)
	os.Unsetenv(EnvAzureClientID)
	os.Unsetenv(EnvAzureTenantID)

	assert.False(t, HasWorkloadIdentityCredentials())

	// Create token file and set all
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	err := os.WriteFile(tokenFile, []byte("test-jwt-token"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvAzureFederatedTokenFile, tokenFile)
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")

	assert.True(t, HasWorkloadIdentityCredentials())
}

func TestWorkloadIdentityCredentials_GetFederatedToken(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")

	expectedToken := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test-payload.signature"
	err := os.WriteFile(tokenFile, []byte(expectedToken), 0o600)
	require.NoError(t, err)

	creds := &WorkloadIdentityCredentials{
		TokenFile: tokenFile,
	}

	token, err := creds.GetFederatedToken()

	require.NoError(t, err)
	assert.Equal(t, expectedToken, token)
}

func TestWorkloadIdentityCredentials_GetFederatedToken_DirectToken(t *testing.T) {
	// Direct token should be returned without reading file
	creds := &WorkloadIdentityCredentials{
		Token:     "direct-jwt-token",
		TokenFile: "/nonexistent/path", // File doesn't exist but shouldn't matter
	}

	token, err := creds.GetFederatedToken()

	require.NoError(t, err)
	assert.Equal(t, "direct-jwt-token", token)
}

func TestWorkloadIdentityCredentials_GetFederatedToken_FileNotFound(t *testing.T) {
	creds := &WorkloadIdentityCredentials{
		TokenFile: "/nonexistent/path/to/token",
	}

	_, err := creds.GetFederatedToken()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read federated token file")
}

func TestWorkloadIdentityCredentials_GetFederatedToken_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	err := os.WriteFile(tokenFile, []byte(""), 0o600)
	require.NoError(t, err)

	creds := &WorkloadIdentityCredentials{
		TokenFile: tokenFile,
	}

	_, err = creds.GetFederatedToken()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "federated token file is empty")
}

func TestHandler_SupportedFlows_IncludesWorkloadIdentity(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	flows := handler.SupportedFlows()

	assert.Contains(t, flows, auth.FlowDeviceCode)
	assert.Contains(t, flows, auth.FlowServicePrincipal)
	assert.Contains(t, flows, auth.FlowWorkloadIdentity)
}

func TestHandler_WorkloadIdentityStatus_NoCredentials(t *testing.T) {
	// Clear env vars
	os.Unsetenv(EnvAzureFederatedTokenFile)
	os.Unsetenv(EnvAzureClientID)
	os.Unsetenv(EnvAzureTenantID)

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	status, err := handler.workloadIdentityStatus(ctx)

	require.NoError(t, err)
	assert.False(t, status.Authenticated)
}

func TestHandler_WorkloadIdentityStatus_WithCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	err := os.WriteFile(tokenFile, []byte("test-jwt-token"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvAzureFederatedTokenFile, tokenFile)
	t.Setenv(EnvAzureClientID, "12345678-1234-1234-1234-123456789012")
	t.Setenv(EnvAzureTenantID, "tenant-id")

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	status, err := handler.workloadIdentityStatus(ctx)

	require.NoError(t, err)
	assert.True(t, status.Authenticated)
	assert.Equal(t, auth.IdentityTypeWorkloadIdentity, status.IdentityType)
	assert.Equal(t, "12345678-1234-1234-1234-123456789012", status.ClientID)
	assert.Equal(t, "tenant-id", status.TenantID)
	assert.Equal(t, tokenFile, status.TokenFile)
	assert.NotEmpty(t, status.Claims.Name) // Should be formatted as "Workload Identity (12345678...)"
}

func TestHandler_WorkloadIdentityLogin_Success(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	federatedToken := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test-payload.signature"
	err := os.WriteFile(tokenFile, []byte(federatedToken), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvAzureFederatedTokenFile, tokenFile)
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")

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
	result, err := handler.workloadIdentityLogin(ctx, auth.LoginOptions{
		Flow: auth.FlowWorkloadIdentity,
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-client-id", result.Claims.ClientID)
	assert.Equal(t, "test-tenant-id", result.Claims.TenantID)

	// Verify request was made with correct parameters
	requests := mockHTTP.GetRequests()
	require.Len(t, requests, 1)
	assert.Equal(t, "client_credentials", requests[0].Data.Get("grant_type"))
	assert.Equal(t, "test-client-id", requests[0].Data.Get("client_id"))
	assert.Equal(t, federatedToken, requests[0].Data.Get("client_assertion"))
	assert.Equal(t, "urn:ietf:params:oauth:client-assertion-type:jwt-bearer", requests[0].Data.Get("client_assertion_type"))
}

func TestHandler_WorkloadIdentityLogin_NoCredentials(t *testing.T) {
	os.Unsetenv(EnvAzureFederatedTokenFile)
	os.Unsetenv(EnvAzureClientID)
	os.Unsetenv(EnvAzureTenantID)

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.workloadIdentityLogin(ctx, auth.LoginOptions{
		Flow: auth.FlowWorkloadIdentity,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "workload identity not configured")
}

func TestHandler_WorkloadIdentityLogin_TokenFileReadError(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	// Create but don't write any content (file exists but we'll delete it to simulate read error)
	err := os.WriteFile(tokenFile, []byte("initial"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvAzureFederatedTokenFile, tokenFile)
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")

	// Delete the file after env is set but before login
	os.Remove(tokenFile)

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.workloadIdentityLogin(ctx, auth.LoginOptions{
		Flow: auth.FlowWorkloadIdentity,
	})

	// The file doesn't exist, so we get a "not found" error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token file not found")
}

func TestHandler_WorkloadIdentityLogin_TokenAcquisitionFailed(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	err := os.WriteFile(tokenFile, []byte("test-jwt-token"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvAzureFederatedTokenFile, tokenFile)
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")

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
	_, err = handler.workloadIdentityLogin(ctx, auth.LoginOptions{
		Flow: auth.FlowWorkloadIdentity,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_client")
}

func TestHandler_GetWorkloadIdentityToken_Success(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	err := os.WriteFile(tokenFile, []byte("test-jwt-token"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvAzureFederatedTokenFile, tokenFile)
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")

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
	token, err := handler.getWorkloadIdentityToken(ctx, auth.TokenOptions{
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
	assert.Equal(t, "https://management.azure.com/.default", requests[0].Data.Get("scope"))
}

func TestHandler_GetWorkloadIdentityToken_UsesCache(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	err := os.WriteFile(tokenFile, []byte("test-jwt-token"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvAzureFederatedTokenFile, tokenFile)
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")

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
	_, err = handler.getWorkloadIdentityToken(ctx, auth.TokenOptions{
		Scope: "https://management.azure.com/.default",
	})
	require.NoError(t, err)
	assert.Len(t, mockHTTP.GetRequests(), 1)

	// Second call should use cache
	_, err = handler.getWorkloadIdentityToken(ctx, auth.TokenOptions{
		Scope: "https://management.azure.com/.default",
	})
	require.NoError(t, err)
	assert.Len(t, mockHTTP.GetRequests(), 1) // Still 1, cache was used
}

func TestHandler_GetWorkloadIdentityToken_ForceRefreshBypassesCache(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	err := os.WriteFile(tokenFile, []byte("test-jwt-token"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvAzureFederatedTokenFile, tokenFile)
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")

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
	token1, err := handler.getWorkloadIdentityToken(ctx, auth.TokenOptions{
		Scope: "https://management.azure.com/.default",
	})
	require.NoError(t, err)
	assert.Equal(t, "test-access-token-1", token1.AccessToken)

	// Force refresh
	token2, err := handler.getWorkloadIdentityToken(ctx, auth.TokenOptions{
		Scope:        "https://management.azure.com/.default",
		ForceRefresh: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "test-access-token-2", token2.AccessToken)
	assert.Len(t, mockHTTP.GetRequests(), 2)
}

func TestHandler_GetWorkloadIdentityToken_NoCredentials(t *testing.T) {
	os.Unsetenv(EnvAzureFederatedTokenFile)
	os.Unsetenv(EnvAzureClientID)
	os.Unsetenv(EnvAzureTenantID)

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.getWorkloadIdentityToken(ctx, auth.TokenOptions{
		Scope: "https://management.azure.com/.default",
	})

	require.Error(t, err)
	// The error comes from GetToken which returns a generic "not authenticated" error
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestHandler_WorkloadIdentityLogin_ErrorMessages(t *testing.T) {
	tests := []struct {
		name        string
		tokenFile   string
		clientID    string
		tenantID    string
		errContains string
	}{
		{
			name:        "missing token file env var",
			tokenFile:   "",
			clientID:    "test-client-id",
			tenantID:    "test-tenant-id",
			errContains: EnvAzureFederatedTokenFile,
		},
		{
			name:        "token file does not exist",
			tokenFile:   "/some/nonexistent/path",
			clientID:    "test-client-id",
			tenantID:    "test-tenant-id",
			errContains: "token file not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Clear all
			os.Unsetenv(EnvAzureFederatedTokenFile)
			os.Unsetenv(EnvAzureClientID)
			os.Unsetenv(EnvAzureTenantID)

			if tc.tokenFile != "" {
				t.Setenv(EnvAzureFederatedTokenFile, tc.tokenFile)
			}
			if tc.clientID != "" {
				t.Setenv(EnvAzureClientID, tc.clientID)
			}
			if tc.tenantID != "" {
				t.Setenv(EnvAzureTenantID, tc.tenantID)
			}

			store := secrets.NewMockStore()
			handler, err := New(WithSecretStore(store))
			require.NoError(t, err)

			ctx := context.Background()
			_, err = handler.workloadIdentityLogin(ctx, auth.LoginOptions{
				Flow: auth.FlowWorkloadIdentity,
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}
