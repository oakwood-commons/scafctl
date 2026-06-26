package tokenprovider

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/runmode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock auth.Handler for AuthHandlerAdapter tests
// ---------------------------------------------------------------------------

type mockHandler struct {
	name         string
	token        *auth.Token
	err          error
	lastOpts     auth.TokenOptions
	displayName  string
	capabilities []auth.Capability
}

func (m *mockHandler) Name() string        { return m.name }
func (m *mockHandler) DisplayName() string { return m.displayName }
func (m *mockHandler) Login(_ context.Context, _ auth.LoginOptions) (*auth.Result, error) {
	return nil, nil
}
func (m *mockHandler) Logout(_ context.Context) error                 { return nil }
func (m *mockHandler) Status(_ context.Context) (*auth.Status, error) { return nil, nil }
func (m *mockHandler) GetToken(_ context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	m.lastOpts = opts
	return m.token, m.err
}

func (m *mockHandler) InjectAuth(_ context.Context, _ *http.Request, _ auth.TokenOptions) error {
	return nil
}
func (m *mockHandler) SupportedFlows() []auth.Flow     { return nil }
func (m *mockHandler) Capabilities() []auth.Capability { return m.capabilities }

// ---------------------------------------------------------------------------
// Mock TokenProvider for API mode tests
// ---------------------------------------------------------------------------

type mockTokenProvider struct {
	name  string
	token Token
	err   error
}

func (m *mockTokenProvider) Name() string { return m.name }
func (m *mockTokenProvider) GetToken(_ context.Context, _ RequestOptions) (Token, error) {
	return m.token, m.err
}

// ---------------------------------------------------------------------------
// AuthHandlerAdapter Tests
// ---------------------------------------------------------------------------

func TestAuthHandlerAdapter_Name(t *testing.T) {
	h := &mockHandler{name: "entra"}
	adapter := NewAuthHandlerAdapter(h)
	assert.Equal(t, "entra", adapter.Name())
}

func TestAuthHandlerAdapter_GetToken_Success(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour)
	h := &mockHandler{
		name:         "entra",
		capabilities: []auth.Capability{auth.CapScopesOnTokenRequest},
		token: &auth.Token{
			AccessToken: "cli-token-123",
			TokenType:   "Bearer",
			ExpiresAt:   expiresAt,
		},
	}
	adapter := NewAuthHandlerAdapter(h)

	token, err := adapter.GetToken(context.Background(), RequestOptions{
		Scope:        "https://graph.microsoft.com/.default",
		ForceRefresh: true,
	})

	require.NoError(t, err)
	assert.Equal(t, "cli-token-123", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.Equal(t, expiresAt, token.ExpiresAt)

	// Verify opts were forwarded correctly.
	assert.Equal(t, "https://graph.microsoft.com/.default", h.lastOpts.Scope)
	assert.True(t, h.lastOpts.ForceRefresh)
}

func TestAuthHandlerAdapter_GetToken_Error(t *testing.T) {
	h := &mockHandler{
		name: "entra",
		err:  errors.New("not logged in"),
	}
	adapter := NewAuthHandlerAdapter(h)

	token, err := adapter.GetToken(context.Background(), RequestOptions{})
	require.Error(t, err)
	assert.Zero(t, token)
	assert.Contains(t, err.Error(), "not logged in")
}

// ---------------------------------------------------------------------------
// Build Tests
// ---------------------------------------------------------------------------

func TestBuild_CLIMode(t *testing.T) {
	authReg := auth.NewRegistry()
	require.NoError(t, authReg.Register(&mockHandler{name: "entra", displayName: "Entra"}))
	require.NoError(t, authReg.Register(&mockHandler{name: "github", displayName: "GitHub"}))

	reg, err := Build(runmode.CLI, authReg, nil)
	require.NoError(t, err)

	assert.Equal(t, 2, reg.Len())
	assert.Equal(t, []string{"entra", "github"}, reg.Names())

	src, ok := reg.Get("entra")
	require.True(t, ok)
	assert.Equal(t, "entra", src.Name())
}

func TestBuild_CLIMode_NilAuthRegistry(t *testing.T) {
	reg, err := Build(runmode.CLI, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, reg.Len())
}

func TestBuild_CLIMode_LazyFallbackResolvesOfficialHandler(t *testing.T) {
	// Simulate an official handler that is not eagerly registered at startup
	// (not cached) but is resolvable via the auth registry's fallback (the
	// official-registry download path).
	authReg := auth.NewRegistry(auth.WithFallbackResolver(
		func(_ context.Context, name string) (auth.Handler, error) {
			if name == "github" {
				return &mockHandler{name: "github", displayName: "GitHub"}, nil
			}
			return nil, errors.New("not an official handler")
		},
	))

	reg, err := Build(runmode.CLI, authReg, nil)
	require.NoError(t, err)

	// github is absent from the eager snapshot...
	_, ok := reg.Get("github")
	assert.False(t, ok)

	// ...but GetContext resolves it lazily via the auth registry fallback.
	src, err := reg.GetContext(context.Background(), "github")
	require.NoError(t, err)
	assert.Equal(t, "github", src.Name())

	// Unknown handlers still surface a not-found error.
	_, err = reg.GetContext(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSourceNotFound)
}

func TestBuild_APIMode(t *testing.T) {
	identityReg := NewRegistry()
	require.NoError(t, identityReg.Register(&mockTokenProvider{name: "entra"}))
	require.NoError(t, identityReg.Register(&mockTokenProvider{name: "passthrough"}))

	reg, err := Build(runmode.API, nil, identityReg)
	require.NoError(t, err)

	assert.Equal(t, 2, reg.Len())
	assert.Equal(t, []string{"entra", "passthrough"}, reg.Names())

	src, ok := reg.Get("entra")
	require.True(t, ok)
	assert.Equal(t, "entra", src.Name())
}

func TestBuild_APIMode_NilDelegationRegistry(t *testing.T) {
	reg, err := Build(runmode.API, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, reg.Len())
}

func TestBuild_CLIMode_DoesNotIncludeIdentitySources(t *testing.T) {
	authReg := auth.NewRegistry()
	require.NoError(t, authReg.Register(&mockHandler{name: "entra", displayName: "Entra"}))

	identityReg := NewRegistry()
	require.NoError(t, identityReg.Register(&mockTokenProvider{name: "extra-identity"}))

	reg, err := Build(runmode.CLI, authReg, identityReg)
	require.NoError(t, err)

	// Only CLI sources should be registered.
	assert.Equal(t, 1, reg.Len())
	_, ok := reg.Get("extra-identity")
	assert.False(t, ok)
}

func TestBuild_APIMode_DoesNotIncludeHandlers(t *testing.T) {
	authReg := auth.NewRegistry()
	require.NoError(t, authReg.Register(&mockHandler{name: "entra", displayName: "Entra"}))

	identityReg := NewRegistry()
	require.NoError(t, identityReg.Register(&mockTokenProvider{name: "entra"}))

	reg, err := Build(runmode.API, authReg, identityReg)
	require.NoError(t, err)

	// Only API sources should be registered.
	assert.Equal(t, 1, reg.Len())
}
