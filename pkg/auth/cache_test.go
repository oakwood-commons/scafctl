// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCachePrefix = "scafctl.auth.test.token." //nolint:gosec // test constant, not a credential

func newTestCache(store secrets.Store) *TokenCache {
	return NewTokenCache(store, testCachePrefix)
}

func TestTokenCache_GetSet(t *testing.T) {
	store := secrets.NewMockStore()
	cache := newTestCache(store)
	ctx := context.Background()

	flow := FlowInteractive
	fp := FingerprintHash("client-a:tenant-1")
	scope := "https://graph.microsoft.com/.default"
	token := &Token{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
		Flow:        flow,
	}

	// Initially, token should not exist
	got, err := cache.Get(ctx, flow, fp, scope)
	require.NoError(t, err)
	assert.Nil(t, got)

	// Set the token
	err = cache.Set(ctx, flow, fp, scope, token)
	require.NoError(t, err)

	// Now we should get it back
	got, err = cache.Get(ctx, flow, fp, scope)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, token.AccessToken, got.AccessToken)
	assert.Equal(t, token.TokenType, got.TokenType)
	assert.Equal(t, token.Scope, got.Scope)
	assert.Equal(t, flow, got.Flow)
}

func TestTokenCache_GetNotFound(t *testing.T) {
	store := secrets.NewMockStore()
	cache := newTestCache(store)
	ctx := context.Background()

	got, err := cache.Get(ctx, FlowInteractive, "_", "nonexistent-scope")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTokenCache_Delete(t *testing.T) {
	store := secrets.NewMockStore()
	cache := newTestCache(store)
	ctx := context.Background()

	flow := FlowInteractive
	fp := "_"
	scope := "https://graph.microsoft.com/.default"
	token := &Token{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
		Flow:        flow,
	}

	// Set then delete
	err := cache.Set(ctx, flow, fp, scope, token)
	require.NoError(t, err)

	err = cache.Delete(ctx, flow, fp, scope)
	require.NoError(t, err)

	// Should be gone
	got, err := cache.Get(ctx, flow, fp, scope)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTokenCache_Clear(t *testing.T) {
	store := secrets.NewMockStore()
	cache := newTestCache(store)
	ctx := context.Background()

	flow := FlowInteractive
	fp := "_"
	// Add multiple tokens
	scopes := []string{
		"https://graph.microsoft.com/.default",
		"https://management.azure.com/.default",
	}

	for _, scope := range scopes {
		token := &Token{
			AccessToken: "token-for-" + scope,
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
			Scope:       scope,
			Flow:        flow,
		}
		err := cache.Set(ctx, flow, fp, scope, token)
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
		got, err := cache.Get(ctx, flow, fp, scope)
		require.NoError(t, err)
		assert.Nil(t, got)
	}

	// But other secrets should remain
	val, err := store.Get(ctx, "some.other.secret")
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
}

func TestTokenCache_ListCachedEntries(t *testing.T) {
	store := secrets.NewMockStore()
	cache := newTestCache(store)
	ctx := context.Background()

	fp := "_"
	// Add tokens with different flows
	entries := []struct {
		flow  Flow
		scope string
	}{
		{FlowInteractive, "https://graph.microsoft.com/.default"},
		{FlowServicePrincipal, "https://management.azure.com/.default"},
	}

	for _, e := range entries {
		token := &Token{
			AccessToken: "token-for-" + string(e.flow) + "-" + e.scope,
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
			Scope:       e.scope,
			Flow:        e.flow,
		}
		err := cache.Set(ctx, e.flow, fp, e.scope, token)
		require.NoError(t, err)
	}

	// List cached entries
	cachedEntries, err := cache.ListCachedEntries(ctx)
	require.NoError(t, err)
	assert.Len(t, cachedEntries, 2)

	// Verify entries contain the expected flows and scopes
	foundFlows := make(map[Flow]string)
	for _, entry := range cachedEntries {
		foundFlows[entry.Flow] = entry.Scope
	}
	assert.Equal(t, "https://graph.microsoft.com/.default", foundFlows[FlowInteractive])
	assert.Equal(t, "https://management.azure.com/.default", foundFlows[FlowServicePrincipal])
}

func TestTokenCache_ListCachedEntries_Empty(t *testing.T) {
	store := secrets.NewMockStore()
	cache := newTestCache(store)
	ctx := context.Background()

	entries, err := cache.ListCachedEntries(ctx)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestTokenCache_FlowPartitioning(t *testing.T) {
	store := secrets.NewMockStore()
	cache := newTestCache(store)
	ctx := context.Background()

	fp := "_"
	scope := "https://graph.microsoft.com/.default"

	// Store tokens with different flows for the same scope
	token1 := &Token{
		AccessToken: "interactive-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
		Flow:        FlowInteractive,
	}
	token2 := &Token{
		AccessToken: "wif-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
		Flow:        FlowWorkloadIdentity,
	}

	err := cache.Set(ctx, FlowInteractive, fp, scope, token1)
	require.NoError(t, err)
	err = cache.Set(ctx, FlowWorkloadIdentity, fp, scope, token2)
	require.NoError(t, err)

	// Each flow should return its own token
	got1, err := cache.Get(ctx, FlowInteractive, fp, scope)
	require.NoError(t, err)
	require.NotNil(t, got1)
	assert.Equal(t, "interactive-token", got1.AccessToken)
	assert.Equal(t, FlowInteractive, got1.Flow)

	got2, err := cache.Get(ctx, FlowWorkloadIdentity, fp, scope)
	require.NoError(t, err)
	require.NotNil(t, got2)
	assert.Equal(t, "wif-token", got2.AccessToken)
	assert.Equal(t, FlowWorkloadIdentity, got2.Flow)

	// Deleting one flow should not affect the other
	err = cache.Delete(ctx, FlowInteractive, fp, scope)
	require.NoError(t, err)

	got1, err = cache.Get(ctx, FlowInteractive, fp, scope)
	require.NoError(t, err)
	assert.Nil(t, got1)

	got2, err = cache.Get(ctx, FlowWorkloadIdentity, fp, scope)
	require.NoError(t, err)
	require.NotNil(t, got2)
	assert.Equal(t, "wif-token", got2.AccessToken)
}

func TestTokenCache_FingerprintPartitioning(t *testing.T) {
	store := secrets.NewMockStore()
	cache := newTestCache(store)
	ctx := context.Background()

	flow := FlowWorkloadIdentity
	scope := "https://graph.microsoft.com/.default"
	fpA := FingerprintHash("client-a:tenant-1")
	fpB := FingerprintHash("client-b:tenant-2")

	tokenA := &Token{
		AccessToken: "token-config-A",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
		Flow:        flow,
	}
	tokenB := &Token{
		AccessToken: "token-config-B",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Scope:       scope,
		Flow:        flow,
	}

	// Store under same flow+scope but different fingerprints
	err := cache.Set(ctx, flow, fpA, scope, tokenA)
	require.NoError(t, err)
	err = cache.Set(ctx, flow, fpB, scope, tokenB)
	require.NoError(t, err)

	// Each fingerprint should return its own token
	gotA, err := cache.Get(ctx, flow, fpA, scope)
	require.NoError(t, err)
	require.NotNil(t, gotA)
	assert.Equal(t, "token-config-A", gotA.AccessToken)

	gotB, err := cache.Get(ctx, flow, fpB, scope)
	require.NoError(t, err)
	require.NotNil(t, gotB)
	assert.Equal(t, "token-config-B", gotB.AccessToken)

	// ListCachedEntries should return both
	entries, err := cache.ListCachedEntries(ctx)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestTokenCache_CacheKeyRoundTrip(t *testing.T) {
	cache := newTestCache(nil)

	testCases := []struct {
		flow        Flow
		fingerprint string
		scope       string
	}{
		{FlowInteractive, "_", "https://graph.microsoft.com/.default"},
		{FlowServicePrincipal, FingerprintHash("client:tenant"), "openid profile offline_access"},
		{FlowWorkloadIdentity, FingerprintHash("wif-config"), "api://my-app/user.read"},
		{FlowDeviceCode, "_", "repo"},
		{FlowPAT, FingerprintHash("ghp_xxx"), "read:user"},
		{FlowGitHubApp, FingerprintHash("12345:67890"), "repo read:user read:org"},
	}

	for _, tc := range testCases {
		key := cache.CacheKey(tc.flow, tc.fingerprint, tc.scope)
		assert.True(t, len(key) > len(testCachePrefix))
		assert.Contains(t, key, testCachePrefix)

		gotFlow, gotFP, gotScope, ok := cache.ParseKey(key)
		assert.True(t, ok)
		assert.Equal(t, tc.flow, gotFlow)
		assert.Equal(t, tc.fingerprint, gotFP)
		assert.Equal(t, tc.scope, gotScope)
	}
}

func TestTokenCache_ParseKey_InvalidKey(t *testing.T) {
	cache := newTestCache(nil)

	// Non-token key
	flow, fp, scope, ok := cache.ParseKey("some.other.key")
	assert.False(t, ok)
	assert.Empty(t, flow)
	assert.Empty(t, fp)
	assert.Empty(t, scope)

	// Only one dot (old format, no fingerprint segment)
	_, _, _, ok = cache.ParseKey(testCachePrefix + "interactive.aGVsbG8") //nolint:dogsled // testing all return values are empty
	assert.False(t, ok)
}

func TestTokenCache_GetCorruptedData(t *testing.T) {
	store := secrets.NewMockStore()
	cache := newTestCache(store)
	ctx := context.Background()

	// Store corrupted data directly
	flow := FlowInteractive
	fp := "_"
	scope := "test-scope"
	key := cache.CacheKey(flow, fp, scope)
	err := store.Set(ctx, key, []byte("not valid json"))
	require.NoError(t, err)

	// Should return error for corrupted data
	got, err := cache.Get(ctx, flow, fp, scope)
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestTokenCache_ExpiredToken(t *testing.T) {
	store := secrets.NewMockStore()
	cache := newTestCache(store)
	ctx := context.Background()
	flow := FlowInteractive
	fp := "_"
	scope := "https://www.googleapis.com/auth/cloud-platform"

	// Cache an expired token
	token := &Token{
		AccessToken: "expired-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
		Scope:       scope,
		Flow:        flow,
	}
	err := cache.Set(ctx, flow, fp, scope, token)
	require.NoError(t, err)

	// Should still return the expired token (caller decides validity)
	got, err := cache.Get(ctx, flow, fp, scope)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "expired-token", got.AccessToken)
	assert.False(t, got.IsValidFor(1*time.Minute))
}

func TestTokenCache_PurgeExpired(t *testing.T) {
	store := secrets.NewMockStore()
	cache := newTestCache(store)
	ctx := context.Background()
	fp := "_"

	// Add an expired token
	err := cache.Set(ctx, FlowInteractive, fp, "expired-scope", &Token{
		AccessToken: "expired-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
	})
	require.NoError(t, err)

	// Add a valid token
	err = cache.Set(ctx, FlowInteractive, fp, "valid-scope", &Token{
		AccessToken: "valid-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})
	require.NoError(t, err)

	purged, err := cache.PurgeExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, purged)

	// Expired token should be gone
	got, err := cache.Get(ctx, FlowInteractive, fp, "expired-scope")
	require.NoError(t, err)
	assert.Nil(t, got)

	// Valid token should still exist
	got, err = cache.Get(ctx, FlowInteractive, fp, "valid-scope")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "valid-token", got.AccessToken)
}

func TestTokenCache_ClearDoesNotDeleteOtherSecrets(t *testing.T) {
	store := secrets.NewMockStore()
	cache := newTestCache(store)
	ctx := context.Background()

	// Store a non-cache secret
	err := store.Set(ctx, "scafctl.auth.test.refresh_token", []byte("refresh-token"))
	require.NoError(t, err)

	// Store a cached token
	fp := "_"
	err = cache.Set(ctx, FlowDeviceCode, fp, "repo", &Token{
		AccessToken: "cached",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "repo",
		Flow:        FlowDeviceCode,
	})
	require.NoError(t, err)

	err = cache.Clear(ctx)
	require.NoError(t, err)

	// The non-cache secret should remain
	exists, err := store.Exists(ctx, "scafctl.auth.test.refresh_token")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestTokenCache_PrefixIsolation(t *testing.T) {
	store := secrets.NewMockStore()
	cacheA := NewTokenCache(store, "handler_a.token.")
	cacheB := NewTokenCache(store, "handler_b.token.")
	ctx := context.Background()

	fp := "_"
	scope := "test-scope"

	// Set tokens in both caches with same flow/fingerprint/scope
	err := cacheA.Set(ctx, FlowInteractive, fp, scope, &Token{
		AccessToken: "token-A",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})
	require.NoError(t, err)

	err = cacheB.Set(ctx, FlowInteractive, fp, scope, &Token{
		AccessToken: "token-B",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})
	require.NoError(t, err)

	// Each cache returns its own token
	gotA, err := cacheA.Get(ctx, FlowInteractive, fp, scope)
	require.NoError(t, err)
	require.NotNil(t, gotA)
	assert.Equal(t, "token-A", gotA.AccessToken)

	gotB, err := cacheB.Get(ctx, FlowInteractive, fp, scope)
	require.NoError(t, err)
	require.NotNil(t, gotB)
	assert.Equal(t, "token-B", gotB.AccessToken)

	// Clearing cache A does not affect cache B
	err = cacheA.Clear(ctx)
	require.NoError(t, err)

	gotA, err = cacheA.Get(ctx, FlowInteractive, fp, scope)
	require.NoError(t, err)
	assert.Nil(t, gotA)

	gotB, err = cacheB.Get(ctx, FlowInteractive, fp, scope)
	require.NoError(t, err)
	require.NotNil(t, gotB)
	assert.Equal(t, "token-B", gotB.AccessToken)
}

func TestCachedToken_Marshaling(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond) // JSON loses precision below milliseconds

	cached := CachedToken{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		ExpiresAt:   now.Add(1 * time.Hour),
		Scope:       "test-scope",
		Flow:        FlowInteractive,
		Fingerprint: FingerprintHash("client:tenant"),
		CachedAt:    now,
	}

	data, err := json.Marshal(cached)
	require.NoError(t, err)

	var decoded CachedToken
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, cached.AccessToken, decoded.AccessToken)
	assert.Equal(t, cached.TokenType, decoded.TokenType)
	assert.Equal(t, cached.Scope, decoded.Scope)
	assert.Equal(t, cached.Flow, decoded.Flow)
	assert.Equal(t, cached.Fingerprint, decoded.Fingerprint)
	assert.WithinDuration(t, cached.ExpiresAt, decoded.ExpiresAt, time.Millisecond)
	assert.WithinDuration(t, cached.CachedAt, decoded.CachedAt, time.Millisecond)
}
