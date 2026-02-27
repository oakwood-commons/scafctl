// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testCreds struct {
	clientID string
}

func setupAcquireTest() (context.Context, *TokenCache, Flow, string) {
	store := secrets.NewMockStore()
	cache := NewTokenCache(store, "scafctl.auth.test.acq.") //nolint:gosec // test constant
	lgr := logger.GetNoopLogger()
	ctx := logger.WithLogger(context.Background(), lgr)
	flow := FlowServicePrincipal
	scope := "https://example.com/.default"
	return ctx, cache, flow, scope
}

func TestGetCachedOrAcquireToken_AcquiresAndCaches(t *testing.T) {
	ctx, cache, flow, scope := setupAcquireTest()

	acquired := false
	token, err := GetCachedOrAcquireToken(
		ctx, cache,
		TokenOptions{Scope: scope},
		flow, scope,
		func() (*testCreds, error) { return &testCreds{clientID: "c1"}, nil },
		func(c *testCreds) bool { return c == nil },
		func(c *testCreds) string { return FingerprintHash(c.clientID) },
		func(_ context.Context, _ *testCreds, _ string) (*Token, error) {
			acquired = true
			return &Token{AccessToken: "fresh", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)}, nil
		},
		"test",
	)
	require.NoError(t, err)
	assert.Equal(t, "fresh", token.AccessToken)
	assert.True(t, acquired)

	// Token should now be in cache
	cached, err := cache.Get(ctx, flow, FingerprintHash("c1"), scope)
	require.NoError(t, err)
	require.NotNil(t, cached)
	assert.Equal(t, "fresh", cached.AccessToken)
}

func TestGetCachedOrAcquireToken_ReturnsCachedToken(t *testing.T) {
	ctx, cache, flow, scope := setupAcquireTest()

	fp := FingerprintHash("c1")
	err := cache.Set(ctx, flow, fp, scope, &Token{
		AccessToken: "cached-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	acquired := false
	token, err := GetCachedOrAcquireToken(
		ctx, cache,
		TokenOptions{Scope: scope},
		flow, scope,
		func() (*testCreds, error) { return &testCreds{clientID: "c1"}, nil },
		func(c *testCreds) bool { return c == nil },
		func(c *testCreds) string { return FingerprintHash(c.clientID) },
		func(_ context.Context, _ *testCreds, _ string) (*Token, error) {
			acquired = true
			return &Token{AccessToken: "new"}, nil
		},
		"test",
	)
	require.NoError(t, err)
	assert.Equal(t, "cached-token", token.AccessToken)
	assert.False(t, acquired, "should not acquire when cache has valid token")
}

func TestGetCachedOrAcquireToken_ForceRefreshBypassesCache(t *testing.T) {
	ctx, cache, flow, scope := setupAcquireTest()

	fp := FingerprintHash("c1")
	err := cache.Set(ctx, flow, fp, scope, &Token{
		AccessToken: "cached-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	token, err := GetCachedOrAcquireToken(
		ctx, cache,
		TokenOptions{Scope: scope, ForceRefresh: true},
		flow, scope,
		func() (*testCreds, error) { return &testCreds{clientID: "c1"}, nil },
		func(c *testCreds) bool { return c == nil },
		func(c *testCreds) string { return FingerprintHash(c.clientID) },
		func(_ context.Context, _ *testCreds, _ string) (*Token, error) {
			return &Token{AccessToken: "refreshed", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)}, nil
		},
		"test",
	)
	require.NoError(t, err)
	assert.Equal(t, "refreshed", token.AccessToken)
}

func TestGetCachedOrAcquireToken_NilCredsReturnsNotAuthenticated(t *testing.T) {
	ctx, cache, flow, scope := setupAcquireTest()

	_, err := GetCachedOrAcquireToken(
		ctx, cache,
		TokenOptions{Scope: scope},
		flow, scope,
		func() (*testCreds, error) { return nil, nil },
		func(c *testCreds) bool { return c == nil },
		func(c *testCreds) string { return "" },
		func(_ context.Context, _ *testCreds, _ string) (*Token, error) {
			return nil, errors.New("should not be called")
		},
		"test",
	)
	require.Error(t, err)
	assert.True(t, IsNotAuthenticated(err))
}

func TestGetCachedOrAcquireToken_GetCredsError(t *testing.T) {
	ctx, cache, flow, scope := setupAcquireTest()

	credsErr := errors.New("creds unavailable")
	_, err := GetCachedOrAcquireToken(
		ctx, cache,
		TokenOptions{Scope: scope},
		flow, scope,
		func() (*testCreds, error) { return nil, credsErr },
		func(c *testCreds) bool { return c == nil },
		func(c *testCreds) string { return "" },
		func(_ context.Context, _ *testCreds, _ string) (*Token, error) {
			return nil, errors.New("should not be called")
		},
		"test",
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, credsErr))
}

func TestGetCachedOrAcquireToken_AcquireError(t *testing.T) {
	ctx, cache, flow, scope := setupAcquireTest()

	acquireErr := errors.New("token endpoint unreachable")
	_, err := GetCachedOrAcquireToken(
		ctx, cache,
		TokenOptions{Scope: scope},
		flow, scope,
		func() (*testCreds, error) { return &testCreds{clientID: "c1"}, nil },
		func(c *testCreds) bool { return c == nil },
		func(c *testCreds) string { return FingerprintHash(c.clientID) },
		func(_ context.Context, _ *testCreds, _ string) (*Token, error) {
			return nil, acquireErr
		},
		"test",
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, acquireErr))
}

func TestGetCachedOrAcquireToken_DefaultMinValidFor(t *testing.T) {
	ctx, cache, flow, scope := setupAcquireTest()

	// Cache a token that expires in 30s — less than DefaultMinValidFor (60s).
	fp := FingerprintHash("c1")
	err := cache.Set(ctx, flow, fp, scope, &Token{
		AccessToken: "soon-expiring",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(30 * time.Second),
	})
	require.NoError(t, err)

	// With MinValidFor=0 the default (60s) kicks in and the cached token is stale.
	token, err := GetCachedOrAcquireToken(
		ctx, cache,
		TokenOptions{Scope: scope, MinValidFor: 0},
		flow, scope,
		func() (*testCreds, error) { return &testCreds{clientID: "c1"}, nil },
		func(c *testCreds) bool { return c == nil },
		func(c *testCreds) string { return FingerprintHash(c.clientID) },
		func(_ context.Context, _ *testCreds, _ string) (*Token, error) {
			return &Token{AccessToken: "new-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)}, nil
		},
		"test",
	)
	require.NoError(t, err)
	assert.Equal(t, "new-token", token.AccessToken, "should acquire fresh token when cached is below default min validity")
}

func TestGetCachedOrAcquireToken_CustomMinValidFor(t *testing.T) {
	ctx, cache, flow, scope := setupAcquireTest()

	// Cache a token that expires in 30s.
	fp := FingerprintHash("c1")
	err := cache.Set(ctx, flow, fp, scope, &Token{
		AccessToken: "short-lived",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(30 * time.Second),
	})
	require.NoError(t, err)

	// With MinValidFor=10s the cached token is still valid.
	token, err := GetCachedOrAcquireToken(
		ctx, cache,
		TokenOptions{Scope: scope, MinValidFor: 10 * time.Second},
		flow, scope,
		func() (*testCreds, error) { return &testCreds{clientID: "c1"}, nil },
		func(c *testCreds) bool { return c == nil },
		func(c *testCreds) string { return FingerprintHash(c.clientID) },
		func(_ context.Context, _ *testCreds, _ string) (*Token, error) {
			return &Token{AccessToken: "should-not-reach"}, nil
		},
		"test",
	)
	require.NoError(t, err)
	assert.Equal(t, "short-lived", token.AccessToken, "should use cached token when valid for at least MinValidFor")
}
