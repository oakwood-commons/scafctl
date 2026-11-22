// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	hostnameresolver "github.com/oakwood-commons/scafctl/pkg/auth/hostname"
	"github.com/oakwood-commons/scafctl/pkg/config"
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

	token, err := getToken(ctx, "entra", "scope", "https://api.example.com/v1", "", 30*time.Second, cfg)

	require.NoError(t, err)
	assert.Equal(t, "header-bearer-token", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
}

func TestGetToken_HeaderTokenRejectsExplicitHostname(t *testing.T) {
	t.Parallel()
	tokens := map[string]string{"Openshift": "header-bearer-token"}
	ctx := middleware.WithContextTokens(context.Background(), tokens)
	cfg := &httpc.ClientConfig{}

	_, err := getToken(ctx, "openshift", "", "https://127.0.0.1:16443/api/v1", "cluster-a", 30*time.Second, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "request-header token pass-through")
}

func TestGetToken_HeaderTokenEmpty_Error(t *testing.T) {
	t.Parallel()
	tokens := map[string]string{"Entra": ""}
	ctx := middleware.WithContextTokens(context.Background(), tokens)
	cfg := &httpc.ClientConfig{}

	_, err := getToken(ctx, "entra", "scope", "https://api.example.com/v1", "", 30*time.Second, cfg)

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

	token, err := getToken(ctx, "entra", "", "https://api.example.com/v1", "", 30*time.Second, cfg)

	require.NoError(t, err)
	assert.Equal(t, "handler-token", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
	require.Len(t, mockHandler.GetTokenCalls, 1)
	assert.Empty(t, mockHandler.GetTokenCalls[0].Hostname)
}

func TestGetToken_FallsBackToHandlerToken_NoRegistry_Error(t *testing.T) {
	t.Parallel()
	// No header tokens and no auth registry in context.
	ctx := context.Background()
	cfg := &httpc.ClientConfig{}

	_, err := getToken(ctx, "entra", "scope", "https://api.example.com/v1", "", 30*time.Second, cfg)

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

	token, err := getToken(ctx, "entra", "scope", "https://api.example.com/v1", "", 30*time.Second, cfg)

	require.NoError(t, err)
	assert.Equal(t, "from-header", token.AccessToken)
	// Handler should never have been called.
	assert.Empty(t, mockHandler.GetTokenCalls)
}

func TestHandlerToken_CapTokenHostname_DerivesOriginAndPreservesOnRefresh(t *testing.T) {
	t.Parallel()
	mockHandler := auth.NewMockHandler("openshift")
	mockHandler.CapabilitiesValue = []auth.Capability{auth.CapHostname, auth.CapTokenHostname}
	mockHandler.GetTokenResult = &auth.Token{
		AccessToken: "cluster-a-token",
		TokenType:   "Bearer",
	}

	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), reg)
	cfg := &httpc.ClientConfig{}

	token, err := handlerToken(ctx, "openshift", "", "HTTPS://user:pass@API.Cluster-A.Example.COM:6443/apis/apps/v1?watch=true#frag", "", 30*time.Second, cfg)
	require.NoError(t, err)
	assert.Equal(t, "cluster-a-token", token.AccessToken)
	require.Len(t, mockHandler.GetTokenCalls, 1)
	assert.Equal(t, "https://api.cluster-a.example.com:6443", mockHandler.GetTokenCalls[0].Hostname)

	// Trigger OnUnauthorized and confirm ForceRefresh keeps the same Hostname.
	require.NotNil(t, cfg.OnUnauthorized)
	mockHandler.SetToken(&auth.Token{AccessToken: "refreshed-cluster-a-token", TokenType: "Bearer"})
	refreshedHeader, err := cfg.OnUnauthorized(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Bearer refreshed-cluster-a-token", refreshedHeader)
	require.Len(t, mockHandler.GetTokenCalls, 2)
	assert.True(t, mockHandler.GetTokenCalls[1].ForceRefresh)
	assert.Equal(t, "https://api.cluster-a.example.com:6443", mockHandler.GetTokenCalls[1].Hostname)
}

func TestHandlerToken_CapTokenHostname_ExplicitAliasResolution(t *testing.T) {
	t.Parallel()
	mockHandler := auth.NewMockHandler("openshift")
	mockHandler.CapabilitiesValue = []auth.Capability{auth.CapHostname, auth.CapTokenHostname}
	mockHandler.GetTokenResult = &auth.Token{
		AccessToken: "alias-token",
		TokenType:   "Bearer",
	}

	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mockHandler))

	appCfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				"openshift": {
					Hostname: &config.HostnameConfig{
						Aliases: map[string]string{
							"cluster-a": "https://api.cluster-a.example.com:6443",
						},
					},
				},
			},
		},
	}
	ctx := auth.WithRegistry(config.WithConfig(context.Background(), appCfg), reg)
	cfg := &httpc.ClientConfig{}

	token, err := handlerToken(ctx, "openshift", "", "https://127.0.0.1:16443/api/v1/pods", "cluster-a", 30*time.Second, cfg)
	require.NoError(t, err)
	assert.Equal(t, "alias-token", token.AccessToken)
	require.Len(t, mockHandler.GetTokenCalls, 1)
	assert.Equal(t, "https://api.cluster-a.example.com:6443", mockHandler.GetTokenCalls[0].Hostname)

	// Unknown alias selector fails closed with ErrSelectorNotFound.
	_, err = handlerToken(ctx, "openshift", "", "https://127.0.0.1:16443/api/v1/pods", "unknown-cluster", 30*time.Second, cfg)
	require.Error(t, err)
	assert.ErrorIs(t, err, hostnameresolver.ErrSelectorNotFound)
}

func TestHandlerToken_CapTokenHostname_InvalidURL(t *testing.T) {
	t.Parallel()
	mockHandler := auth.NewMockHandler("openshift")
	mockHandler.CapabilitiesValue = []auth.Capability{auth.CapTokenHostname}
	mockHandler.GetTokenResult = &auth.Token{AccessToken: "tok", TokenType: "Bearer"}

	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), reg)
	cfg := &httpc.ClientConfig{}

	_, err := handlerToken(ctx, "openshift", "", "/relative-only", "", 30*time.Second, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must include scheme and host")
	assert.Empty(t, mockHandler.GetTokenCalls)
}

func TestHandlerToken_WithoutCapTokenHostname_RejectsExplicitHostname(t *testing.T) {
	t.Parallel()
	mockHandler := auth.NewMockHandler("github")
	mockHandler.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin, auth.CapHostname}
	mockHandler.GetTokenResult = &auth.Token{AccessToken: "gh-tok", TokenType: "Bearer"}

	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mockHandler))
	ctx := auth.WithRegistry(context.Background(), reg)
	cfg := &httpc.ClientConfig{}

	_, err := handlerToken(ctx, "github", "", "https://127.0.0.1:8443/api/v3/user", "github.example.com", 30*time.Second, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `http: hostname input is not supported by authProvider "github" (handler does not advertise token_hostname capability)`)
	assert.Empty(t, mockHandler.GetTokenCalls)
}

func TestResolveAuthProviderContext_InvalidSelectors(t *testing.T) {
	t.Parallel()
	cases := []string{
		"@work",
		"github@",
		"github@bad/profile",
		"github@two@parts",
	}
	for _, selector := range cases {
		t.Run(selector, func(t *testing.T) {
			t.Parallel()
			_, _, err := resolveAuthProviderContext(context.Background(), selector)
			require.Error(t, err)
		})
	}
}
