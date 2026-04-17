// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

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

// ============================================================================
// OBO cache unit tests
// ============================================================================

func TestOBOCacheKey_Deterministic(t *testing.T) {
	k1 := oboCacheKey("token-abc", "https://graph.microsoft.com/.default")
	k2 := oboCacheKey("token-abc", "https://graph.microsoft.com/.default")
	assert.Equal(t, k1, k2)
}

func TestOBOCacheKey_DifferentAssertions(t *testing.T) {
	k1 := oboCacheKey("token-a", "scope")
	k2 := oboCacheKey("token-b", "scope")
	assert.NotEqual(t, k1, k2)
}

func TestOBOCacheKey_DifferentScopes(t *testing.T) {
	k1 := oboCacheKey("token", "scope-a")
	k2 := oboCacheKey("token", "scope-b")
	assert.NotEqual(t, k1, k2)
}

func TestOBOCache_GetMiss(t *testing.T) {
	c := newOBOCache()
	token, ok := c.get("assertion", "scope")
	assert.False(t, ok)
	assert.Nil(t, token)
}

func TestOBOCache_SetAndGet(t *testing.T) {
	c := newOBOCache()
	tok := &auth.Token{
		AccessToken: "access-token-123",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "scope",
	}
	c.set("assertion", "scope", tok)

	got, ok := c.get("assertion", "scope")
	assert.True(t, ok)
	assert.Equal(t, "access-token-123", got.AccessToken)
}

func TestOBOCache_Expired(t *testing.T) {
	c := newOBOCache()
	tok := &auth.Token{
		AccessToken: "expired-token",
		ExpiresAt:   time.Now().Add(-time.Minute),
		Scope:       "scope",
	}
	c.set("assertion", "scope", tok)

	_, ok := c.get("assertion", "scope")
	assert.False(t, ok)
}

func TestOBOCache_ExpiredEntryEvicted(t *testing.T) {
	c := newOBOCache()
	tok := &auth.Token{
		AccessToken: "expired-token",
		ExpiresAt:   time.Now().Add(-time.Minute),
		Scope:       "scope",
	}
	c.set("assertion", "scope", tok)

	// First get should miss and evict
	_, ok := c.get("assertion", "scope")
	assert.False(t, ok)

	// Verify the entry was removed from the map
	c.mu.RLock()
	assert.Empty(t, c.items, "expired entry should be evicted from cache")
	c.mu.RUnlock()
}

func TestOBOCache_ExpiryBuffer(t *testing.T) {
	c := newOBOCache()
	// Token expires in 20 seconds — within the 30-second buffer
	tok := &auth.Token{
		AccessToken: "almost-expired",
		ExpiresAt:   time.Now().Add(20 * time.Second),
		Scope:       "scope",
	}
	c.set("assertion", "scope", tok)

	_, ok := c.get("assertion", "scope")
	assert.False(t, ok, "token within expiry buffer should not be returned")
}

// ============================================================================
// GetOBOToken tests
// ============================================================================

func TestGetOBOToken_Success(t *testing.T) {
	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token": "obo-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"scope":        "https://graph.microsoft.com/.default",
	})

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	token, err := handler.GetOBOToken(context.Background(), OBOTokenOptions{
		Assertion:    "upstream-token",
		Scope:        "https://graph.microsoft.com/.default",
		ClientSecret: "my-secret",
	})
	require.NoError(t, err)
	assert.Equal(t, "obo-access-token", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.Equal(t, auth.Flow(FlowOnBehalfOf), token.Flow)
	assert.Equal(t, "https://graph.microsoft.com/.default", token.Scope)

	// Verify request parameters
	require.Len(t, mockHTTP.Requests, 1)
	req := mockHTTP.Requests[0]
	assert.Equal(t, OBOGrantType, req.Data.Get("grant_type"))
	assert.Equal(t, "upstream-token", req.Data.Get("assertion"))
	assert.Equal(t, OBORequestedTokenUse, req.Data.Get("requested_token_use"))
	assert.Equal(t, "my-secret", req.Data.Get("client_secret"))
}

func TestGetOBOToken_CachesResult(t *testing.T) {
	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token": "cached-obo-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
	})

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	opts := OBOTokenOptions{
		Assertion:    "upstream-token",
		Scope:        "https://graph.microsoft.com/.default",
		ClientSecret: "my-secret",
	}

	// First call hits the endpoint
	token1, err := handler.GetOBOToken(context.Background(), opts)
	require.NoError(t, err)

	// Second call should use cache (no additional HTTP request)
	token2, err := handler.GetOBOToken(context.Background(), opts)
	require.NoError(t, err)

	assert.Equal(t, token1.AccessToken, token2.AccessToken)
	assert.Len(t, mockHTTP.Requests, 1, "second call should use cache")
}

func TestGetOBOToken_MissingAssertion(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	_, err = handler.GetOBOToken(context.Background(), OBOTokenOptions{
		Scope:        "https://graph.microsoft.com/.default",
		ClientSecret: "my-secret",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrNotAuthenticated)
}

func TestGetOBOToken_MissingScope(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	_, err = handler.GetOBOToken(context.Background(), OBOTokenOptions{
		Assertion:    "upstream-token",
		ClientSecret: "my-secret",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrInvalidScope)
}

func TestGetOBOToken_MissingClientSecret(t *testing.T) {
	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store))
	require.NoError(t, err)

	_, err = handler.GetOBOToken(context.Background(), OBOTokenOptions{
		Assertion: "upstream-token",
		Scope:     "https://graph.microsoft.com/.default",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrAuthenticationFailed)
}

func TestGetOBOToken_ServerError(t *testing.T) {
	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusBadRequest, map[string]string{
		"error":             "invalid_grant",
		"error_description": "AADSTS70000: The provided grant is invalid.",
	})

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	_, err = handler.GetOBOToken(context.Background(), OBOTokenOptions{
		Assertion:    "bad-token",
		Scope:        "https://graph.microsoft.com/.default",
		ClientSecret: "my-secret",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AADSTS70000")
}

func TestGetOBOToken_QualifiesBareScope(t *testing.T) {
	mockHTTP := NewMockHTTPClient()
	mockHTTP.AddResponse(http.StatusOK, map[string]any{
		"access_token": "qualified-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
	})

	store := secrets.NewMockStore()
	handler, err := New(WithSecretStore(store), WithHTTPClient(mockHTTP))
	require.NoError(t, err)

	_, err = handler.GetOBOToken(context.Background(), OBOTokenOptions{
		Assertion:    "upstream-token",
		Scope:        "User.Read",
		ClientSecret: "my-secret",
	})
	require.NoError(t, err)

	require.Len(t, mockHTTP.Requests, 1)
	assert.Equal(t, "https://graph.microsoft.com/User.Read", mockHTTP.Requests[0].Data.Get("scope"))
}

// ============================================================================
// Benchmarks
// ============================================================================

func BenchmarkOBOCacheKey(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		oboCacheKey("eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.long-token-value", "https://graph.microsoft.com/.default")
	}
}

func BenchmarkOBOCache_GetHit(b *testing.B) {
	c := newOBOCache()
	tok := &auth.Token{
		AccessToken: "token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	c.set("assertion", "scope", tok)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		c.get("assertion", "scope")
	}
}

func BenchmarkOBOCache_GetMiss(b *testing.B) {
	c := newOBOCache()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		c.get("assertion", "scope")
	}
}
