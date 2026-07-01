// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/tokenprovider"
)

// stubTokenProvider is a minimal tokenprovider.TokenProvider used to exercise
// defaultToken's success and error paths without real credentials.
type stubTokenProvider struct {
	name  string
	token tokenprovider.Token
	err   error
}

func (s stubTokenProvider) Name() string { return s.name }

func (s stubTokenProvider) GetToken(context.Context, tokenprovider.RequestOptions) (tokenprovider.Token, error) {
	return s.token, s.err
}

// registerStub returns a context carrying a registry with the given stub source.
func registerStub(t *testing.T, src stubTokenProvider) context.Context {
	t.Helper()
	reg := tokenprovider.NewRegistry()
	require.NoError(t, reg.Register(src))
	return tokenprovider.WithRegistry(context.Background(), reg)
}

// TestDefaultToken_NoRegistryReturnsError verifies defaultToken surfaces the
// underlying token-provider error (and no token) when no provider registry is
// available, rather than triggering an interactive login.
func TestDefaultToken_NoRegistryReturnsError(t *testing.T) {
	t.Parallel()

	tok, err := defaultToken(context.Background(), "entra", "api://example/.default")

	require.Error(t, err)
	assert.Empty(t, tok)
}

// TestDefaultToken_ReturnsAccessToken verifies the success path returns the
// access token from the resolved provider.
func TestDefaultToken_ReturnsAccessToken(t *testing.T) {
	t.Parallel()

	ctx := registerStub(t, stubTokenProvider{
		name:  "entra",
		token: tokenprovider.Token{AccessToken: "abc123"},
	})

	tok, err := defaultToken(ctx, "entra", "api://example/.default")

	require.NoError(t, err)
	assert.Equal(t, "abc123", tok)
}

// TestDefaultToken_PropagatesProviderError verifies a provider error is
// surfaced verbatim and no token is returned.
func TestDefaultToken_PropagatesProviderError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("cached token expired")
	ctx := registerStub(t, stubTokenProvider{name: "entra", err: wantErr})

	tok, err := defaultToken(ctx, "entra", "")

	require.ErrorIs(t, err, wantErr)
	assert.Empty(t, tok)
}
