// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// aadstsHint unit tests
// ============================================================================

func TestAadstsHint_AADSTS700016(t *testing.T) {
	desc := "AADSTS700016: Application with identifier 'abc123' was not found in the directory 'contoso.onmicrosoft.com'."
	hint := aadstsHint(desc)
	assert.NotEmpty(t, hint)
	assert.Contains(t, hint, EnvAzureClientID)
	assert.Contains(t, hint, EnvAzureTenantID)
}

func TestAadstsHint_AADSTS90002(t *testing.T) {
	desc := "AADSTS90002: Tenant 'bad-tenant-guid' not found."
	hint := aadstsHint(desc)
	assert.NotEmpty(t, hint)
	assert.Contains(t, hint, EnvAzureTenantID)
}

func TestAadstsHint_AADSTS7000215(t *testing.T) {
	desc := "AADSTS7000215: Invalid client secret provided. Ensure the secret being sent is the client secret value."
	hint := aadstsHint(desc)
	assert.NotEmpty(t, hint)
	assert.Contains(t, hint, EnvAzureClientSecret)
}

func TestAadstsHint_AADSTS70011(t *testing.T) {
	desc := "AADSTS70011: The provided request must include a 'scope' input parameter."
	hint := aadstsHint(desc)
	assert.NotEmpty(t, hint)
	assert.Contains(t, hint, "scope")
}

func TestAadstsHint_AADSTS500011(t *testing.T) {
	desc := "AADSTS500011: The resource principal named 'api://abc' was not found in the tenant."
	hint := aadstsHint(desc)
	assert.NotEmpty(t, hint)
	assert.Contains(t, hint, "admin consent")
}

func TestAadstsHint_AADSTS500113(t *testing.T) {
	desc := "AADSTS500113: No reply address is registered for the application."
	hint := aadstsHint(desc)
	assert.NotEmpty(t, hint)
	assert.Contains(t, hint, "redirect URI")
	assert.Contains(t, hint, "http://localhost")
	assert.Contains(t, hint, "device-code")
}

func TestAadstsHint_AADSTS53003(t *testing.T) {
	desc := "AADSTS53003: Access has been blocked by Conditional Access policies."
	hint := aadstsHint(desc)
	assert.NotEmpty(t, hint)
	assert.Contains(t, hint, "Conditional Access")
	assert.Contains(t, hint, "re-authenticate")
}

func TestAadstsHint_UnknownCode(t *testing.T) {
	// A code we have no specific guidance for should return an empty string
	// so callers can fall back to the raw message.
	hint := aadstsHint("AADSTS99999: Some future error code.")
	assert.Empty(t, hint)
}

func TestAadstsHint_NoAADSTSCode(t *testing.T) {
	hint := aadstsHint("invalid_client: bad stuff happened")
	assert.Empty(t, hint)
}

func TestFormatAADSTSError_WithHint(t *testing.T) {
	errResp := TokenErrorResponse{
		Error:            "invalid_client",
		ErrorDescription: "AADSTS700016: Application not found.",
	}
	err := formatAADSTSError("token request failed", errResp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AADSTS700016")
	assert.Contains(t, err.Error(), "Hint:")
	assert.Contains(t, err.Error(), EnvAzureClientID)
}

func TestFormatAADSTSError_WithoutHint(t *testing.T) {
	errResp := TokenErrorResponse{
		Error:            "server_error",
		ErrorDescription: "Something went wrong on the server.",
	}
	err := formatAADSTSError("token request failed", errResp)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "Hint:")
}

// ============================================================================
// Service principal error handling integration
// ============================================================================

func TestAcquireServicePrincipalToken_AppNotFoundInDirectory(t *testing.T) {
	t.Setenv(EnvAzureClientID, "wrong-client-id")
	t.Setenv(EnvAzureTenantID, "wrong-tenant-id")
	t.Setenv(EnvAzureClientSecret, "some-secret")

	const aadsts700016Desc = "AADSTS700016: Application with identifier 'wrong-client-id' was not found " +
		"in the directory 'wrong-tenant-id'. This can happen if the application has not been installed by the administrator."

	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusBadRequest, map[string]string{
		"error":             "invalid_client",
		"error_description": aadsts700016Desc,
	})

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.acquireServicePrincipalToken(ctx, GetServicePrincipalCredentials(), "https://management.azure.com/.default")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "AADSTS700016")
	assert.Contains(t, err.Error(), EnvAzureClientID)
	assert.Contains(t, err.Error(), EnvAzureTenantID)
}

func TestAcquireServicePrincipalToken_ExpiredSecret(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "expired-secret-value")

	const aadsts7000215Desc = "AADSTS7000215: Invalid client secret provided. Ensure the secret being " +
		"sent in the request is the client secret value, not the client secret ID."

	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusUnauthorized, map[string]string{
		"error":             "invalid_client",
		"error_description": aadsts7000215Desc,
	})

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.acquireServicePrincipalToken(ctx, GetServicePrincipalCredentials(), "https://management.azure.com/.default")

	require.Error(t, err)
	// Must reference the env var so the user knows what to update
	assert.Contains(t, err.Error(), EnvAzureClientSecret)
	// Must mention the AADSTS code
	assert.Contains(t, err.Error(), "AADSTS7000215")
}

func TestAcquireServicePrincipalToken_UnauthorizedClient(t *testing.T) {
	t.Setenv(EnvAzureClientID, "test-client-id")
	t.Setenv(EnvAzureTenantID, "test-tenant-id")
	t.Setenv(EnvAzureClientSecret, "test-secret")

	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusUnauthorized, map[string]string{
		"error":             "unauthorized_client",
		"error_description": "The client does not have permission to request a token using this method.",
	})

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.acquireServicePrincipalToken(ctx, GetServicePrincipalCredentials(), "https://management.azure.com/.default")

	require.Error(t, err)
	// Should guide the user to the portal
	assert.Contains(t, err.Error(), "admin consent")
	assert.Contains(t, err.Error(), "API permissions")
}

// ============================================================================
// Device code flow error handling
// ============================================================================

func TestRequestDeviceCode_AppNotFoundInDirectory(t *testing.T) {
	const aadsts700016Desc = "AADSTS700016: Application with identifier 'bad-id' was not found in directory 'contoso'."

	server := newMockEntraServer(t)
	defer server.Close()
	server.SetDeviceCodeResponse(http.StatusBadRequest, map[string]any{
		"error":             "invalid_client",
		"error_description": aadsts700016Desc,
	})

	store := secrets.NewMockStore()
	handler, err := New(
		WithConfig(&Config{
			ClientID:        "bad-id",
			TenantID:        "contoso",
			Authority:       server.URL(),
			MinPollInterval: 10 * time.Millisecond,
		}),
		WithSecretStore(store),
	)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = handler.requestDeviceCode(ctx, "contoso", []string{"https://management.azure.com/.default"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "AADSTS700016")
	assert.Contains(t, err.Error(), EnvAzureClientID)
}
