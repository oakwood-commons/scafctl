// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"context"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenCache_SetAndGet(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()
	scope := "https://www.googleapis.com/auth/cloud-platform"

	token := &auth.Token{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
	}

	err := cache.Set(ctx, scope, token)
	require.NoError(t, err)

	got, err := cache.Get(ctx, scope)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "test-access-token", got.AccessToken)
	assert.Equal(t, "Bearer", got.TokenType)
	assert.Equal(t, scope, got.Scope)
}

func TestTokenCache_Get_NotFound(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()

	got, err := cache.Get(ctx, "nonexistent-scope")
	require.NoError(t, err) // returns nil, nil for not found
	assert.Nil(t, got)
}

func TestTokenCache_Delete(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()
	scope := "https://www.googleapis.com/auth/cloud-platform"

	token := &auth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
	}

	err := cache.Set(ctx, scope, token)
	require.NoError(t, err)

	err = cache.Delete(ctx, scope)
	require.NoError(t, err)

	got, err := cache.Get(ctx, scope)
	require.NoError(t, err) // returns nil, nil for deleted key
	assert.Nil(t, got)
}

func TestTokenCache_Clear(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()

	// Add multiple tokens
	scopes := []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/bigquery",
	}
	for _, scope := range scopes {
		token := &auth.Token{
			AccessToken: "token-for-" + scope,
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
			Scope:       scope,
		}
		err := cache.Set(ctx, scope, token)
		require.NoError(t, err)
	}

	err := cache.Clear(ctx)
	require.NoError(t, err)

	// All tokens should be gone
	for _, scope := range scopes {
		got, err := cache.Get(ctx, scope)
		require.NoError(t, err) // returns nil, nil for deleted keys
		assert.Nil(t, got)
	}
}

func TestTokenCache_ScopeToKey(t *testing.T) {
	cache := NewTokenCache(nil)

	key1 := cache.scopeToKey("https://www.googleapis.com/auth/cloud-platform")
	key2 := cache.scopeToKey("https://www.googleapis.com/auth/bigquery")

	// Keys should be different for different scopes
	assert.NotEqual(t, key1, key2)

	// Keys should have the correct prefix
	assert.Contains(t, key1, SecretKeyTokenPrefix)
	assert.Contains(t, key2, SecretKeyTokenPrefix)
}

func TestTokenCache_ListCachedScopes(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()

	// Add a token
	scope := "https://www.googleapis.com/auth/cloud-platform"
	token := &auth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
	}
	err := cache.Set(ctx, scope, token)
	require.NoError(t, err)

	scopes, err := cache.ListCachedScopes(ctx)
	require.NoError(t, err)
	assert.Len(t, scopes, 1)
	assert.Contains(t, scopes, scope)
}

func TestTokenCache_ExpiredToken(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()
	scope := "https://www.googleapis.com/auth/cloud-platform"

	// Cache an expired token
	token := &auth.Token{
		AccessToken: "expired-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
		Scope:       scope,
	}
	err := cache.Set(ctx, scope, token)
	require.NoError(t, err)

	// Should still return the expired token (caller decides validity)
	got, err := cache.Get(ctx, scope)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "expired-token", got.AccessToken)
	assert.False(t, got.IsValidFor(1*time.Minute))
}
