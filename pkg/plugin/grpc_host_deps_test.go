// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostDepsFromAuthRegistry_NilRegistry(t *testing.T) {
	deps := HostDepsFromAuthRegistry(nil)
	assert.Nil(t, deps)
}

func TestHostDepsFromAuthRegistry_TokenFunc(t *testing.T) {
	reg := auth.NewRegistry()
	mock := auth.NewMockHandler("test-handler")
	mock.GetTokenResult = &auth.Token{
		AccessToken: "tok-123",
		TokenType:   "Bearer",
		ExpiresAt:   time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		Scope:       "read",
	}
	require.NoError(t, reg.Register(mock))

	deps := HostDepsFromAuthRegistry(reg)
	require.NotNil(t, deps)
	require.NotNil(t, deps.AuthTokenFunc)

	resp, err := deps.AuthTokenFunc(context.Background(), "test-handler", "read", 60, false)
	require.NoError(t, err)
	assert.Equal(t, "tok-123", resp.AccessToken)
	assert.Equal(t, "Bearer", resp.TokenType)
	assert.Equal(t, "read", resp.Scope)
	assert.Equal(t, time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC).Unix(), resp.ExpiresAtUnix)
	assert.Empty(t, resp.Error)

	// Verify ForceRefresh and MinValidFor are forwarded.
	require.Len(t, mock.GetTokenCalls, 1)
	assert.Equal(t, "read", mock.GetTokenCalls[0].Scope)
	assert.Equal(t, 60*time.Second, mock.GetTokenCalls[0].MinValidFor)
	assert.False(t, mock.GetTokenCalls[0].ForceRefresh)
}

func TestHostDepsFromAuthRegistry_TokenFunc_ForceRefresh(t *testing.T) {
	reg := auth.NewRegistry()
	mock := auth.NewMockHandler("h")
	mock.GetTokenResult = &auth.Token{AccessToken: "tok"}
	require.NoError(t, reg.Register(mock))

	deps := HostDepsFromAuthRegistry(reg)
	_, err := deps.AuthTokenFunc(context.Background(), "h", "", 0, true)
	require.NoError(t, err)
	require.Len(t, mock.GetTokenCalls, 1)
	assert.True(t, mock.GetTokenCalls[0].ForceRefresh)
}

func TestHostDepsFromAuthRegistry_TokenFunc_UnknownHandler(t *testing.T) {
	reg := auth.NewRegistry()
	deps := HostDepsFromAuthRegistry(reg)

	resp, err := deps.AuthTokenFunc(context.Background(), "unknown", "", 0, false)
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "unknown")
}

func TestHostDepsFromAuthRegistry_TokenFunc_NegativeMinValidForClamped(t *testing.T) {
	reg := auth.NewRegistry()
	mock := auth.NewMockHandler("h")
	mock.GetTokenResult = &auth.Token{AccessToken: "tok", TokenType: "Bearer"}
	require.NoError(t, reg.Register(mock))

	deps := HostDepsFromAuthRegistry(reg)
	resp, err := deps.AuthTokenFunc(context.Background(), "h", "scope", -999, false)
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "tok", resp.AccessToken)
	// Negative value should be clamped to 0 (triggers DefaultMinValidFor in token acquisition).
	require.Len(t, mock.GetTokenCalls, 1)
	assert.Equal(t, time.Duration(0), mock.GetTokenCalls[0].MinValidFor)
}

func TestHostDepsFromAuthRegistry_TokenFunc_EmptyHandlerResolvesToDefault(t *testing.T) {
	reg := auth.NewRegistry()
	mockA := auth.NewMockHandler("alpha")
	mockA.GetTokenResult = &auth.Token{AccessToken: "alpha-tok", TokenType: "Bearer"}
	mockB := auth.NewMockHandler("beta")
	mockB.GetTokenResult = &auth.Token{AccessToken: "beta-tok", TokenType: "Bearer"}
	require.NoError(t, reg.Register(mockA))
	require.NoError(t, reg.Register(mockB))

	deps := HostDepsFromAuthRegistry(reg)
	resp, err := deps.AuthTokenFunc(context.Background(), "", "scope", 0, false)
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	// List() returns sorted handlers; "alpha" is first, so it's the default.
	assert.Equal(t, "alpha-tok", resp.AccessToken)
}

func TestHostDepsFromAuthRegistry_TokenFunc_EmptyHandlerNoHandlers(t *testing.T) {
	reg := auth.NewRegistry()
	deps := HostDepsFromAuthRegistry(reg)

	resp, err := deps.AuthTokenFunc(context.Background(), "", "", 0, false)
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "no auth handlers registered")
}

func TestHostDepsFromAuthRegistry_TokenFunc_GetTokenError(t *testing.T) {
	reg := auth.NewRegistry()
	mock := auth.NewMockHandler("h")
	mock.GetTokenErr = fmt.Errorf("token expired")
	require.NoError(t, reg.Register(mock))

	deps := HostDepsFromAuthRegistry(reg)
	resp, err := deps.AuthTokenFunc(context.Background(), "h", "", 0, false)
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "token expired")
}

func TestHostDepsFromAuthRegistry_HandlersFunc(t *testing.T) {
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(auth.NewMockHandler("alpha")))
	require.NoError(t, reg.Register(auth.NewMockHandler("beta")))

	deps := HostDepsFromAuthRegistry(reg)
	require.NotNil(t, deps.AuthHandlersFunc)

	handlers, defaultH, err := deps.AuthHandlersFunc(context.Background())
	require.NoError(t, err)
	assert.Len(t, handlers, 2)
	assert.Contains(t, handlers, "alpha")
	assert.Contains(t, handlers, "beta")
	assert.NotEmpty(t, defaultH)
}

func TestHostDepsFromAuthRegistry_HandlersFunc_Empty(t *testing.T) {
	reg := auth.NewRegistry()
	deps := HostDepsFromAuthRegistry(reg)

	handlers, defaultH, err := deps.AuthHandlersFunc(context.Background())
	require.NoError(t, err)
	assert.Empty(t, handlers)
	assert.Empty(t, defaultH)
}

func BenchmarkHostDepsFromAuthRegistry(b *testing.B) {
	reg := auth.NewRegistry()
	mock := auth.NewMockHandler("bench")
	mock.GetTokenResult = &auth.Token{AccessToken: "tok", TokenType: "Bearer"}
	_ = reg.Register(mock)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		deps := HostDepsFromAuthRegistry(reg)
		_, _ = deps.AuthTokenFunc(context.Background(), "bench", "scope", 0, false)
	}
}

func TestAuthClientOptsFromContext_NoRegistry(t *testing.T) {
	ctx := context.Background()
	opts := AuthClientOptsFromContext(ctx)
	assert.Nil(t, opts)
}

func TestAuthClientOptsFromContext_WithRegistry(t *testing.T) {
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(auth.NewMockHandler("h")))
	ctx := auth.WithRegistry(context.Background(), reg)

	opts := AuthClientOptsFromContext(ctx)
	assert.Len(t, opts, 1)
}

func TestAuthClientOptsFromContext_SetsProfileResolver(t *testing.T) {
	reg := auth.NewRegistry()
	mock := auth.NewMockHandler("github")
	mock.GetTokenResult = &auth.Token{
		AccessToken: "work-tok",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	require.NoError(t, reg.Register(mock))

	ctx := auth.WithRegistry(context.Background(), reg)
	ctx = auth.WithGlobalProfile(ctx, "work")

	opts := AuthClientOptsFromContext(ctx)
	require.Len(t, opts, 1)

	// Apply options to a clientOptions and verify ProfileResolverFunc is set.
	cfg := &clientOptions{}
	for _, o := range opts {
		o(cfg)
	}
	require.NotNil(t, cfg.hostDeps)
	require.NotNil(t, cfg.hostDeps.ProfileResolverFunc)
	assert.Equal(t, "work", cfg.hostDeps.ProfileResolverFunc("github"))
}
