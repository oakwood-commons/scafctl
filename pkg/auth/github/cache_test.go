// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenCache_GetSet(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()

	scope := "repo read:user"
	token := &auth.Token{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
	}

	got, err := cache.Get(ctx, scope)
	require.NoError(t, err)
	assert.Nil(t, got)

	err = cache.Set(ctx, scope, token)
	require.NoError(t, err)

	got, err = cache.Get(ctx, scope)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, token.AccessToken, got.AccessToken)
	assert.Equal(t, token.TokenType, got.TokenType)
	assert.Equal(t, token.Scope, got.Scope)
}

func TestTokenCache_Delete(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()

	scope := "repo"
	token := &auth.Token{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
	}

	err := cache.Set(ctx, scope, token)
	require.NoError(t, err)

	err = cache.Delete(ctx, scope)
	require.NoError(t, err)

	got, err := cache.Get(ctx, scope)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTokenCache_Clear(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()

	for _, scope := range []string{"repo", "read:user", "read:org"} {
		err := cache.Set(ctx, scope, &auth.Token{
			AccessToken: "token-" + scope,
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
			Scope:       scope,
		})
		require.NoError(t, err)
	}

	err := cache.Clear(ctx)
	require.NoError(t, err)

	for _, scope := range []string{"repo", "read:user", "read:org"} {
		got, err := cache.Get(ctx, scope)
		require.NoError(t, err)
		assert.Nil(t, got)
	}
}

func TestTokenCache_ListCachedScopes(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()

	scopes, err := cache.ListCachedScopes(ctx)
	require.NoError(t, err)
	assert.Empty(t, scopes)

	for _, scope := range []string{"repo", "read:user"} {
		err := cache.Set(ctx, scope, &auth.Token{
			AccessToken: "token-" + scope,
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
			Scope:       scope,
		})
		require.NoError(t, err)
	}

	scopes, err = cache.ListCachedScopes(ctx)
	require.NoError(t, err)
	assert.Len(t, scopes, 2)
	assert.Contains(t, scopes, "repo")
	assert.Contains(t, scopes, "read:user")
}

func TestTokenCache_ScopeToKeyRoundtrip(t *testing.T) {
	cache := &TokenCache{}

	testScopes := []string{
		"repo",
		"read:user",
		"repo read:user read:org",
	}

	for _, scope := range testScopes {
		key := cache.scopeToKey(scope)
		assert.True(t, len(key) > len(SecretKeyTokenPrefix))

		roundtripped := cache.keyToScope(key)
		assert.Equal(t, scope, roundtripped)
	}
}

func TestTokenCache_KeyToScope_InvalidKey(t *testing.T) {
	cache := &TokenCache{}
	assert.Equal(t, "", cache.keyToScope("not.a.token.key"))
}

func TestTokenCache_ClearDoesNotDeleteOtherSecrets(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()

	err := store.Set(ctx, SecretKeyRefreshToken, []byte("refresh-token"))
	require.NoError(t, err)

	err = cache.Set(ctx, "repo", &auth.Token{
		AccessToken: "cached",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "repo",
	})
	require.NoError(t, err)

	err = cache.Clear(ctx)
	require.NoError(t, err)

	exists, err := store.Exists(ctx, SecretKeyRefreshToken)
	require.NoError(t, err)
	assert.True(t, exists)
}
