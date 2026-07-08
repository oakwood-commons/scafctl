// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/auth"
)

// registerStub returns a context carrying an auth registry with the given mock handler.
func registerStub(t *testing.T, mock *auth.MockHandler) context.Context {
	t.Helper()
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mock))
	return auth.WithRegistry(context.Background(), reg)
}

// TestDefaultToken_NoRegistryReturnsError verifies defaultToken surfaces the
// underlying error (and no token) when no auth registry is available,
// rather than triggering an interactive login.
func TestDefaultToken_NoRegistryReturnsError(t *testing.T) {
	t.Parallel()

	tok, err := defaultToken(context.Background(), "entra", "api://example/.default")

	require.Error(t, err)
	assert.Empty(t, tok)
}

// TestDefaultToken_ReturnsAccessToken verifies the success path returns the
// access token from the resolved handler.
func TestDefaultToken_ReturnsAccessToken(t *testing.T) {
	t.Parallel()

	mock := auth.NewMockHandler("entra")
	mock.SetToken(&auth.Token{AccessToken: "abc123", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})
	ctx := registerStub(t, mock)

	tok, err := defaultToken(ctx, "entra", "api://example/.default")

	require.NoError(t, err)
	assert.Equal(t, "abc123", tok)
}

// TestDefaultToken_PropagatesProviderError verifies a provider error is
// surfaced verbatim and no token is returned.
func TestDefaultToken_PropagatesProviderError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("cached token expired")
	mock := auth.NewMockHandler("entra")
	mock.SetTokenError(wantErr)
	ctx := registerStub(t, mock)

	tok, err := defaultToken(ctx, "entra", "")

	require.ErrorIs(t, err, wantErr)
	assert.Empty(t, tok)
}
