// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newEntraMock creates a mock handler with Entra-like capabilities (supports per-request scopes).
func newEntraMock() *auth.MockHandler {
	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapScopesOnTokenRequest,
		auth.CapTenantID,
		auth.CapFederatedToken,
	}
	return mock
}

// newGitHubMock creates a mock handler with GitHub-like capabilities (no per-request scopes).
func newGitHubMock() *auth.MockHandler {
	mock := auth.NewMockHandler("github")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapHostname,
	}
	return mock
}

func TestCommandToken_UnknownHandler(t *testing.T) {
	ctx, _ := newTestContext(t)
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"unknown", "--scope", "test"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth handler")
}

func TestCommandToken_MissingHandler(t *testing.T) {
	ctx, _ := newTestContext(t)
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument: <handler>")
}

func TestCommandToken_MissingScopeForEntra(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := newEntraMock()
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scope")
	assert.Contains(t, err.Error(), "required")
}

func TestCommandToken_ScopeRejectedForGitHub(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := newGitHubMock()
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github", "--scope", "repo"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support per-request scopes")
	assert.Contains(t, err.Error(), "scafctl auth login")
}

func TestCommandToken_GitHubSuccessWithoutScope(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newGitHubMock()
	mock.SetToken(&auth.Token{
		AccessToken: "gho_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "repo read:user",
	})
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify GetToken was called with empty scope
	require.Len(t, mock.GetTokenCalls, 1)
	assert.Equal(t, "", mock.GetTokenCalls[0].Scope)

	output := buf.String()
	assert.Contains(t, output, "handler")
	assert.Contains(t, output, "github")
}

func TestCommandToken_Success(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiIsIng1dCI6Ik1uQ19WWmNBVGZNNXBP",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "https://graph.microsoft.com/.default",
	})

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "https://graph.microsoft.com/.default"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify GetToken was called
	require.Len(t, mock.GetTokenCalls, 1)
	assert.Equal(t, "https://graph.microsoft.com/.default", mock.GetTokenCalls[0].Scope)

	// Verify output includes all fields
	output := buf.String()
	assert.Contains(t, output, "handler")
	assert.Contains(t, output, "entra")
	assert.Contains(t, output, "scope")
	assert.Contains(t, output, "graph.microsoft.com")
	assert.Contains(t, output, "tokenType")
	assert.Contains(t, output, "Bearer")
	assert.Contains(t, output, "accessToken") // Full token is present, not masked
}

func TestCommandToken_JSONOutput(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiIsIng1dCI6Ik1uQ19WWmNBVGZNNXBP",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "https://graph.microsoft.com/.default",
	})

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "https://graph.microsoft.com/.default", "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify output includes full token in JSON
	output := buf.String()
	assert.Contains(t, output, "accessToken")
	assert.Contains(t, output, "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiIsIng1dCI6Ik1uQ19WWmNBVGZNNXBP")
}

func TestCommandToken_WithMinValidFor(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(10 * time.Minute),
		Scope:       "test-scope",
	})

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "test-scope", "--min-valid-for", "5m"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify GetToken was called with correct min-valid-for
	require.Len(t, mock.GetTokenCalls, 1)
	assert.Equal(t, 5*time.Minute, mock.GetTokenCalls[0].MinValidFor)
}

func TestCommandToken_ForceRefresh(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "fresh-token-value-that-is-long-enough",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "test-scope",
	})

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "test-scope", "--force-refresh"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify GetToken was called with ForceRefresh=true
	require.Len(t, mock.GetTokenCalls, 1)
	assert.True(t, mock.GetTokenCalls[0].ForceRefresh)
}

func TestCommandToken_NotAuthenticated(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetTokenError(auth.ErrNotAuthenticated)

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "test-scope"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get token")
}

func TestCommandToken_TokenError(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetTokenError(errors.New("token refresh failed"))

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "test-scope"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get token")
	assert.Contains(t, err.Error(), "token refresh failed")
}

func TestCommandToken_ShortToken(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "short", // Less than 20 chars
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "test-scope",
	})

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "test-scope"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Short token should be shown without masking
	output := buf.String()
	assert.Contains(t, output, "short")
	assert.NotContains(t, output, "...")
}
