package entra

import (
	"context"
	"encoding/json"
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

	scope := "https://graph.microsoft.com/.default"
	token := &auth.Token{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
	}

	// Initially, token should not exist
	got, err := cache.Get(ctx, scope)
	require.NoError(t, err)
	assert.Nil(t, got)

	// Set the token
	err = cache.Set(ctx, scope, token)
	require.NoError(t, err)

	// Now we should get it back
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

	scope := "https://graph.microsoft.com/.default"
	token := &auth.Token{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
	}

	// Set then delete
	err := cache.Set(ctx, scope, token)
	require.NoError(t, err)

	err = cache.Delete(ctx, scope)
	require.NoError(t, err)

	// Should be gone
	got, err := cache.Get(ctx, scope)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTokenCache_Clear(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()

	// Add multiple tokens
	scopes := []string{
		"https://graph.microsoft.com/.default",
		"https://management.azure.com/.default",
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

	// Add a non-token secret (should not be cleared)
	err := store.Set(ctx, "some.other.secret", []byte("value"))
	require.NoError(t, err)

	// Clear all token cache
	err = cache.Clear(ctx)
	require.NoError(t, err)

	// Tokens should be gone
	for _, scope := range scopes {
		got, err := cache.Get(ctx, scope)
		require.NoError(t, err)
		assert.Nil(t, got)
	}

	// But other secrets should remain
	val, err := store.Get(ctx, "some.other.secret")
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
}

func TestTokenCache_ListCachedScopes(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()

	// Add multiple tokens
	scopes := []string{
		"https://graph.microsoft.com/.default",
		"https://management.azure.com/.default",
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

	// List cached scopes
	cachedScopes, err := cache.ListCachedScopes(ctx)
	require.NoError(t, err)
	assert.Len(t, cachedScopes, 2)
	assert.Contains(t, cachedScopes, "https://graph.microsoft.com/.default")
	assert.Contains(t, cachedScopes, "https://management.azure.com/.default")
}

func TestTokenCache_ScopeToKeyRoundTrip(t *testing.T) {
	cache := NewTokenCache(nil)

	testScopes := []string{
		"https://graph.microsoft.com/.default",
		"openid profile offline_access",
		"api://my-app/user.read",
	}

	for _, scope := range testScopes {
		key := cache.scopeToKey(scope)
		assert.True(t, len(key) > len(SecretKeyTokenPrefix))
		assert.Contains(t, key, SecretKeyTokenPrefix)

		gotScope := cache.keyToScope(key)
		assert.Equal(t, scope, gotScope)
	}
}

func TestTokenCache_KeyToScope_InvalidKey(t *testing.T) {
	cache := NewTokenCache(nil)

	// Non-token key
	assert.Empty(t, cache.keyToScope("some.other.key"))

	// Invalid base64
	assert.Empty(t, cache.keyToScope(SecretKeyTokenPrefix+"!!!invalid!!!"))
}

func TestTokenCache_GetCorruptedData(t *testing.T) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store)
	ctx := context.Background()

	// Store corrupted data directly
	scope := "test-scope"
	key := cache.scopeToKey(scope)
	err := store.Set(ctx, key, []byte("not valid json"))
	require.NoError(t, err)

	// Should return error for corrupted data
	got, err := cache.Get(ctx, scope)
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestCachedToken_Marshaling(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond) // JSON loses precision below milliseconds

	cached := CachedToken{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		ExpiresAt:   now.Add(1 * time.Hour),
		Scope:       "test-scope",
		CachedAt:    now,
	}

	data, err := json.Marshal(cached)
	require.NoError(t, err)

	var decoded CachedToken
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// Compare with truncated times due to JSON precision
	assert.Equal(t, cached.AccessToken, decoded.AccessToken)
	assert.Equal(t, cached.TokenType, decoded.TokenType)
	assert.Equal(t, cached.Scope, decoded.Scope)
	assert.WithinDuration(t, cached.ExpiresAt, decoded.ExpiresAt, time.Millisecond)
	assert.WithinDuration(t, cached.CachedAt, decoded.CachedAt, time.Millisecond)
}
