// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupGroupsHandler creates a handler wired with mock stores and clients,
// with a stored refresh token and metadata so GetToken succeeds.
func setupGroupsHandler(t *testing.T) (*Handler, *MockHTTPClient, *MockGraphClient, context.Context) {
	t.Helper()

	// Ensure no SP/WI env vars are set (they would reroute GetToken).
	t.Setenv(EnvAzureClientID, "")
	t.Setenv(EnvAzureTenantID, "")
	t.Setenv(EnvAzureClientSecret, "")
	t.Setenv(EnvAzureFederatedToken, "")
	t.Setenv(EnvAzureFederatedTokenFile, "")

	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()
	mockGraph := NewMockGraphClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
		WithGraphClient(mockGraph),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// Store refresh token.
	require.NoError(t, store.Set(ctx, SecretKeyRefreshToken, []byte("test-refresh-token")))

	// Store metadata with tenant & client IDs so mintToken can proceed.
	metadata := &TokenMetadata{
		TenantID:  "test-tenant",
		ClientID:  "04b07795-8ddb-461a-bbee-02f9e1bf7b46",
		LoginFlow: auth.FlowDeviceCode,
	}
	metaBytes, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, store.Set(ctx, SecretKeyMetadata, metaBytes))

	return handler, mockHTTP, mockGraph, ctx
}

// addTokenResponse queues an HTTP token endpoint response for the mock.
func addTokenResponse(mockHTTP *MockHTTPClient) {
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token":  "graph-access-token",
		"refresh_token": "test-refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         graphGroupsScope,
	})
}

func TestGetGroups_SinglePage(t *testing.T) {
	handler, mockHTTP, mockGraph, ctx := setupGroupsHandler(t)
	addTokenResponse(mockHTTP)

	mockGraph.AddResponse(http.StatusOK, map[string]any{
		"value": []map[string]any{
			{"id": "group-1"},
			{"id": "group-2"},
			{"id": "group-3"},
		},
	})

	groups, err := handler.GetGroups(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"group-1", "group-2", "group-3"}, groups)

	// One token request, one Graph request.
	assert.Len(t, mockHTTP.GetRequests(), 1)
	assert.Len(t, mockGraph.GetRequests(), 1)
	assert.Equal(t, graphGroupsMemberOfURL, mockGraph.GetRequests()[0].URL)
	assert.Equal(t, "graph-access-token", mockGraph.GetRequests()[0].BearerToken)
}

func TestGetGroups_Pagination(t *testing.T) {
	handler, mockHTTP, mockGraph, ctx := setupGroupsHandler(t)
	addTokenResponse(mockHTTP)

	nextLink := "https://graph.microsoft.com/v1.0/me/memberOf/microsoft.graph.group?$select=id&$top=999&$skipToken=abc123"

	// Page 1 — contains nextLink.
	mockGraph.AddResponse(http.StatusOK, map[string]any{
		"@odata.nextLink": nextLink,
		"value": []map[string]any{
			{"id": "group-1"},
			{"id": "group-2"},
		},
	})

	// Page 2 — no nextLink, end of pagination.
	mockGraph.AddResponse(http.StatusOK, map[string]any{
		"value": []map[string]any{
			{"id": "group-3"},
			{"id": "group-4"},
		},
	})

	groups, err := handler.GetGroups(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"group-1", "group-2", "group-3", "group-4"}, groups)
	assert.Len(t, mockGraph.GetRequests(), 2)
	assert.Equal(t, graphGroupsMemberOfURL, mockGraph.GetRequests()[0].URL)
	assert.Equal(t, nextLink, mockGraph.GetRequests()[1].URL)
}

func TestGetGroups_EmptyMembership(t *testing.T) {
	handler, mockHTTP, mockGraph, ctx := setupGroupsHandler(t)
	addTokenResponse(mockHTTP)

	mockGraph.AddResponse(http.StatusOK, map[string]any{
		"value": []map[string]any{},
	})

	groups, err := handler.GetGroups(ctx)
	require.NoError(t, err)
	assert.Empty(t, groups)
}

func TestGetGroups_GraphHTTPError(t *testing.T) {
	handler, mockHTTP, mockGraph, ctx := setupGroupsHandler(t)
	addTokenResponse(mockHTTP)

	mockGraph.AddError(fmt.Errorf("network timeout"))

	_, err := handler.GetGroups(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graph memberOf request failed")
}

func TestGetGroups_GraphNon200Status(t *testing.T) {
	handler, mockHTTP, mockGraph, ctx := setupGroupsHandler(t)
	addTokenResponse(mockHTTP)

	mockGraph.AddResponse(http.StatusForbidden, map[string]any{
		"error": map[string]any{
			"code":    "Authorization_RequestDenied",
			"message": "Insufficient privileges to complete the operation.",
		},
	})

	_, err := handler.GetGroups(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 403")
}

func TestGetGroups_GraphInvalidJSON(t *testing.T) {
	handler, mockHTTP, mockGraph, ctx := setupGroupsHandler(t)
	addTokenResponse(mockHTTP)

	// Override the Graph mock to return raw invalid JSON.
	mockGraph.Responses = append(mockGraph.Responses, &MockGetResponse{
		StatusCode: http.StatusOK,
		Body:       "not-json-serialisable", // will be marshalled as a quoted string, then raw override below
	})
	// Reset and use raw response instead.
	mockGraph.Reset()
	mockGraph.Responses = []*MockGetResponse{
		{StatusCode: http.StatusOK, Body: "invalid-json-{{{"},
	}

	_, err := handler.GetGroups(ctx)
	require.Error(t, err)
	// json.Marshal on a plain string produces valid JSON (a quoted string), so the
	// parse will fail at Unmarshal into graphGroupsPage. Confirm the error chain.
	assert.Contains(t, err.Error(), "parse")
}

func TestGetGroups_TokenAcquisitionFailure(t *testing.T) {
	handler, mockHTTP, _, ctx := setupGroupsHandler(t)

	// Token endpoint returns an error.
	mockHTTP.AddResponse(http.StatusUnauthorized, map[string]any{
		"error":             "invalid_grant",
		"error_description": "AADSTS70000: The refresh token has expired.",
	})

	_, err := handler.GetGroups(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire Graph token")
}

func TestGetGroups_NotAuthenticated(t *testing.T) {
	t.Setenv(EnvAzureClientID, "")
	t.Setenv(EnvAzureTenantID, "")
	t.Setenv(EnvAzureClientSecret, "")
	t.Setenv(EnvAzureFederatedToken, "")
	t.Setenv(EnvAzureFederatedTokenFile, "")

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	// No refresh token stored → not authenticated.
	_, getErr := handler.GetGroups(ctx)
	require.Error(t, getErr)
}

func TestGetGroups_ServicePrincipalFlow(t *testing.T) {
	t.Setenv(EnvAzureClientID, "sp-client-id")
	t.Setenv(EnvAzureTenantID, "sp-tenant-id")
	t.Setenv(EnvAzureClientSecret, "sp-secret")
	t.Setenv(EnvAzureFederatedToken, "")
	t.Setenv(EnvAzureFederatedTokenFile, "")

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	_, getErr := handler.GetGroups(ctx)
	require.Error(t, getErr)
	assert.Contains(t, getErr.Error(), "service principal")
	assert.Contains(t, getErr.Error(), "/me/memberOf")
}

func TestGetGroups_WorkloadIdentityFlow(t *testing.T) {
	// Create a temp file so HasWorkloadIdentityCredentials returns true.
	tmpFile, err := os.CreateTemp(t.TempDir(), "wi-token-*")
	require.NoError(t, err)
	_, writeErr := tmpFile.WriteString("federated-token-value")
	require.NoError(t, writeErr)
	tmpFile.Close()

	t.Setenv(EnvAzureClientID, "wi-client-id")
	t.Setenv(EnvAzureTenantID, "wi-tenant-id")
	t.Setenv(EnvAzureClientSecret, "")
	t.Setenv(EnvAzureFederatedToken, "")
	t.Setenv(EnvAzureFederatedTokenFile, tmpFile.Name())

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	ctx := context.Background()
	_, getErr := handler.GetGroups(ctx)
	require.Error(t, getErr)
	assert.Contains(t, getErr.Error(), "workload identity")
	assert.Contains(t, getErr.Error(), "/me/memberOf")
}

func TestGetGroups_UsesGraphScope(t *testing.T) {
	handler, mockHTTP, mockGraph, ctx := setupGroupsHandler(t)
	addTokenResponse(mockHTTP)
	mockGraph.AddResponse(http.StatusOK, map[string]any{"value": []map[string]any{}})

	_, err := handler.GetGroups(ctx)
	require.NoError(t, err)

	// Verify the token request used the Graph scope.
	require.Len(t, mockHTTP.GetRequests(), 1)
	assert.Equal(t, graphGroupsScope, mockHTTP.GetRequests()[0].Data.Get("scope"))
}

func TestGetGroups_TokenCached(t *testing.T) {
	handler, _, mockGraph, ctx := setupGroupsHandler(t)
	// Pre-populate the token cache so no HTTP token request is needed.
	cache := NewTokenCache(handler.secretStore)
	require.NoError(t, cache.Set(ctx, graphGroupsScope, &auth.Token{
		AccessToken: "cached-graph-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       graphGroupsScope,
	}))

	mockGraph.AddResponse(http.StatusOK, map[string]any{
		"value": []map[string]any{{"id": "group-cached"}},
	})

	groups, err := handler.GetGroups(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"group-cached"}, groups)
	assert.Equal(t, "cached-graph-token", mockGraph.GetRequests()[0].BearerToken)
}

func TestGroupsProviderCompileTimeCheck(t *testing.T) {
	// Verify Handler satisfies auth.GroupsProvider at compile time.
	// If groups.go's var _ check is removed, this test still catches the regression.
	store := secrets.NewMockStore()
	h, err := New(WithSecretStore(store))
	require.NoError(t, err)

	var _ auth.GroupsProvider = h
}
