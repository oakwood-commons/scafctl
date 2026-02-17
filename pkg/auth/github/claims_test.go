// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"net/http"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchUserClaims_Success(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"login":      "octocat",
		"id":         1,
		"name":       "The Octocat",
		"email":      "octocat@github.com",
		"avatar_url": "https://avatars.githubusercontent.com/u/1?v=4",
	})

	ctx := context.Background()
	claims, err := handler.fetchUserClaims(ctx, "test-token")

	require.NoError(t, err)
	assert.Equal(t, "octocat", claims.Subject)
	assert.Equal(t, "octocat", claims.Username)
	assert.Equal(t, "The Octocat", claims.Name)
	assert.Equal(t, "octocat@github.com", claims.Email)
	assert.Equal(t, "1", claims.ObjectID)
	assert.Equal(t, "github.com", claims.Issuer)
}

func TestFetchUserClaims_APIError(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	mockHTTP.AddResponse(http.StatusUnauthorized, map[string]any{
		"message": "Bad credentials",
	})

	ctx := context.Background()
	_, err = handler.fetchUserClaims(ctx, "bad-token")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
}

func TestFetchUserClaims_GHES(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
		WithConfig(&Config{
			Hostname: "github.example.com",
		}),
	)
	require.NoError(t, err)

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"login": "ghes-user",
		"id":    100,
		"name":  "GHES User",
	})

	ctx := context.Background()
	claims, err := handler.fetchUserClaims(ctx, "test-token")

	require.NoError(t, err)
	assert.Equal(t, "ghes-user", claims.Subject)
	assert.Equal(t, "github.example.com", claims.Issuer)

	reqs := mockHTTP.GetRequests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "https://github.example.com/api/v3/user", reqs[0].Endpoint)
}

func TestFetchUserClaims_AuthorizationHeader(t *testing.T) {
	store := secrets.NewMockStore()
	mockHTTP := NewMockHTTPClient()

	handler, err := New(
		WithSecretStore(store),
		WithHTTPClient(mockHTTP),
	)
	require.NoError(t, err)

	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"login": "testuser",
		"id":    1,
	})

	ctx := context.Background()
	_, err = handler.fetchUserClaims(ctx, "my-secret-token")
	require.NoError(t, err)

	reqs := mockHTTP.GetRequests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "Bearer my-secret-token", reqs[0].Headers["Authorization"])
}
