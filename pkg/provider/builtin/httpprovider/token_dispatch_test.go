// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractHeaderToken_Found(t *testing.T) {
	t.Parallel()
	tokens := map[string]string{"Entra": "my-token-value"}
	ctx := middleware.WithContextTokens(context.Background(), tokens)

	found, token := extractHeaderToken(ctx, "entra")

	assert.True(t, found)
	assert.Equal(t, "my-token-value", token)
}

func TestExtractHeaderToken_CanonicalKeyNormalization(t *testing.T) {
	t.Parallel()
	// Keys in the map use canonical form; input should be normalized.
	tokens := map[string]string{"My-Custom-Provider": "custom-token"}
	ctx := middleware.WithContextTokens(context.Background(), tokens)

	found, token := extractHeaderToken(ctx, "my-custom-provider")

	assert.True(t, found)
	assert.Equal(t, "custom-token", token)
}

func TestExtractHeaderToken_NotFound(t *testing.T) {
	t.Parallel()
	tokens := map[string]string{"Github": "gh-token"}
	ctx := middleware.WithContextTokens(context.Background(), tokens)

	found, token := extractHeaderToken(ctx, "entra")

	assert.False(t, found)
	assert.Empty(t, token)
}

func TestExtractHeaderToken_EmptyTokenString(t *testing.T) {
	t.Parallel()
	tokens := map[string]string{"Entra": ""}
	ctx := middleware.WithContextTokens(context.Background(), tokens)

	found, token := extractHeaderToken(ctx, "entra")

	assert.True(t, found)
	assert.Empty(t, token)
}

func TestExtractHeaderToken_NilTokensFromContext(t *testing.T) {
	t.Parallel()
	// No tokens stored in context at all.
	ctx := context.Background()

	found, token := extractHeaderToken(ctx, "entra")

	assert.False(t, found)
	assert.Empty(t, token)
}

func TestGetToken_HeaderTokenPresent(t *testing.T) {
	t.Parallel()
	tokens := map[string]string{"Entra": "header-bearer-token"}
	ctx := middleware.WithContextTokens(context.Background(), tokens)
	cfg := &httpc.ClientConfig{}

	token, err := getToken(ctx, "entra", "scope", 30*time.Second, cfg)

	require.NoError(t, err)
	assert.Equal(t, "header-bearer-token", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
}

func TestGetToken_HeaderTokenEmpty_Error(t *testing.T) {
	t.Parallel()
	tokens := map[string]string{"Entra": ""}
	ctx := middleware.WithContextTokens(context.Background(), tokens)
	cfg := &httpc.ClientConfig{}

	_, err := getToken(ctx, "entra", "scope", 30*time.Second, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "token is empty in request headers")
	assert.Contains(t, err.Error(), "entra")
}

func TestGetToken_FallsBackToHandlerToken(t *testing.T) {
	t.Parallel()
	// No header tokens — should fall through to handlerToken which needs an auth registry.
	mockHandler := auth.NewMockHandler("entra")
	mockHandler.GetTokenResult = &auth.Token{
		AccessToken: "handler-token",
		TokenType:   "Bearer",
	}
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), reg)
	cfg := &httpc.ClientConfig{}

	token, err := getToken(ctx, "entra", "", 30*time.Second, cfg)

	require.NoError(t, err)
	assert.Equal(t, "handler-token", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
}

func TestGetToken_FallsBackToHandlerToken_NoRegistry_Error(t *testing.T) {
	t.Parallel()
	// No header tokens and no auth registry in context.
	ctx := context.Background()
	cfg := &httpc.ClientConfig{}

	_, err := getToken(ctx, "entra", "scope", 30*time.Second, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no auth registry in context")
}

func TestGetToken_HeaderTakesPrecedenceOverHandler(t *testing.T) {
	t.Parallel()
	// Both header token and auth handler are available — header should win.
	tokens := map[string]string{"Entra": "from-header"}
	ctx := middleware.WithContextTokens(context.Background(), tokens)

	mockHandler := auth.NewMockHandler("entra")
	mockHandler.GetTokenResult = &auth.Token{AccessToken: "from-handler", TokenType: "Bearer"}
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mockHandler))
	ctx = auth.WithRegistry(ctx, reg)
	cfg := &httpc.ClientConfig{}

	token, err := getToken(ctx, "entra", "scope", 30*time.Second, cfg)

	require.NoError(t, err)
	assert.Equal(t, "from-header", token.AccessToken)
	// Handler should never have been called.
	assert.Empty(t, mockHandler.GetTokenCalls)
}
