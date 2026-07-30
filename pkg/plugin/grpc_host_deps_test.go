// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

// makeTestJWT builds an unsigned three-part JWT whose payload encodes the given
// claims. Only the payload (part index 1) is decoded by ParseJWTClaims.
func makeTestJWT(t *testing.T, payload map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	header := enc(map[string]string{"alg": "none", "typ": "JWT"})
	return header + "." + enc(payload) + ".sig"
}

func TestHostDepsFromAuthRegistry_IdentityFunc_NoScope_StatusClaims(t *testing.T) {
	reg := auth.NewRegistry()
	mock := auth.NewMockHandler("h")
	mock.StatusResult = &auth.Status{
		Authenticated: true,
		Claims: &auth.Claims{
			Subject: "user-123",
			Email:   "user@example.com",
			Name:    "Jane Doe",
		},
	}
	require.NoError(t, reg.Register(mock))

	deps := HostDepsFromAuthRegistry(reg)
	require.NotNil(t, deps.AuthIdentityFunc)

	claims, err := deps.AuthIdentityFunc(context.Background(), "h", "")
	require.NoError(t, err)
	require.NotNil(t, claims)
	assert.Equal(t, "user-123", claims.Subject)
	assert.Equal(t, "user@example.com", claims.Email)
	assert.Equal(t, "Jane Doe", claims.Name)
}

func TestHostDepsFromAuthRegistry_IdentityFunc_NoScope_NoClaims(t *testing.T) {
	reg := auth.NewRegistry()
	mock := auth.NewMockHandler("h")
	// Authenticated (e.g. device-code) but no cached identity claims.
	mock.StatusResult = &auth.Status{Authenticated: true}
	require.NoError(t, reg.Register(mock))

	deps := HostDepsFromAuthRegistry(reg)
	claims, err := deps.AuthIdentityFunc(context.Background(), "h", "")
	require.Error(t, err)
	assert.Nil(t, claims)
	assert.Contains(t, err.Error(), "scope")
}

func TestHostDepsFromAuthRegistry_IdentityFunc_NotAuthenticated(t *testing.T) {
	reg := auth.NewRegistry()
	mock := auth.NewMockHandler("h")
	mock.StatusResult = &auth.Status{Authenticated: false}
	require.NoError(t, reg.Register(mock))

	deps := HostDepsFromAuthRegistry(reg)
	claims, err := deps.AuthIdentityFunc(context.Background(), "h", "")
	require.Error(t, err)
	assert.Nil(t, claims)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestHostDepsFromAuthRegistry_IdentityFunc_WithScope_JWTClaims(t *testing.T) {
	reg := auth.NewRegistry()
	mock := auth.NewMockHandler("h")
	mock.GetTokenResult = &auth.Token{
		AccessToken: makeTestJWT(t, map[string]any{
			"sub":   "sub-from-jwt",
			"email": "jwt@example.com",
			"tid":   "tenant-1",
			"oid":   "object-1",
		}),
		TokenType: "Bearer",
	}
	require.NoError(t, reg.Register(mock))

	deps := HostDepsFromAuthRegistry(reg)
	claims, err := deps.AuthIdentityFunc(context.Background(), "h", "api://app/.default")
	require.NoError(t, err)
	require.NotNil(t, claims)
	assert.Equal(t, "sub-from-jwt", claims.Subject)
	assert.Equal(t, "jwt@example.com", claims.Email)
	assert.Equal(t, "tenant-1", claims.TenantId)
	assert.Equal(t, "object-1", claims.ObjectId)
}

func TestHostDepsFromAuthRegistry_IdentityFunc_WithScope_OpaqueTokenFallsBack(t *testing.T) {
	reg := auth.NewRegistry()
	mock := auth.NewMockHandler("h")
	// Opaque (non-JWT) access token -> claims derived from Status instead.
	mock.GetTokenResult = &auth.Token{AccessToken: "opaque-token", TokenType: "Bearer"}
	mock.StatusResult = &auth.Status{
		Authenticated: true,
		Claims:        &auth.Claims{Subject: "status-sub"},
	}
	require.NoError(t, reg.Register(mock))

	deps := HostDepsFromAuthRegistry(reg)
	claims, err := deps.AuthIdentityFunc(context.Background(), "h", "api://app/.default")
	require.NoError(t, err)
	require.NotNil(t, claims)
	assert.Equal(t, "status-sub", claims.Subject)
}

func TestHostDepsFromAuthRegistry_IdentityFunc_WithScope_NilToken(t *testing.T) {
	reg := auth.NewRegistry()
	// Handler returns (nil, nil) from GetToken: no token, no error.
	mock := auth.NewMockHandler("h")
	mock.GetTokenResult = nil
	require.NoError(t, reg.Register(mock))

	deps := HostDepsFromAuthRegistry(reg)
	claims, err := deps.AuthIdentityFunc(context.Background(), "h", "api://app/.default")
	require.Error(t, err)
	assert.Nil(t, claims)
	assert.Contains(t, err.Error(), "handler returned no token")
}

func TestHostDepsFromAuthRegistry_IdentityFunc_UnknownHandler(t *testing.T) {
	reg := auth.NewRegistry()
	deps := HostDepsFromAuthRegistry(reg)

	claims, err := deps.AuthIdentityFunc(context.Background(), "", "")
	require.Error(t, err)
	assert.Nil(t, claims)
	assert.Contains(t, err.Error(), "no auth handlers registered")
}

func TestHostDepsFromAuthRegistry_IdentityFunc_EmptyHandlerResolvesToDefault(t *testing.T) {
	reg := auth.NewRegistry()
	mockA := auth.NewMockHandler("alpha")
	mockA.StatusResult = &auth.Status{
		Authenticated: true,
		Claims:        &auth.Claims{Subject: "alpha-sub"},
	}
	require.NoError(t, reg.Register(mockA))
	require.NoError(t, reg.Register(auth.NewMockHandler("beta")))

	deps := HostDepsFromAuthRegistry(reg)
	claims, err := deps.AuthIdentityFunc(context.Background(), "", "")
	require.NoError(t, err)
	require.NotNil(t, claims)
	assert.Equal(t, "alpha-sub", claims.Subject)
}

// mockGroupsHandler embeds *auth.MockHandler and additionally implements
// auth.GroupsProvider so the AuthGroupsFunc closure can type-assert to it.
type mockGroupsHandler struct {
	*auth.MockHandler
	groups   []string
	groupErr error
}

func (m *mockGroupsHandler) GetGroups(_ context.Context) ([]string, error) {
	return m.groups, m.groupErr
}

// Compile-time assertion that mockGroupsHandler satisfies GroupsProvider.
var _ auth.GroupsProvider = (*mockGroupsHandler)(nil)

func TestHostDepsFromAuthRegistry_GroupsFunc(t *testing.T) {
	tests := []struct {
		name       string
		register   func(reg *auth.Registry)
		handler    string
		wantGroups []string
		wantErr    string
	}{
		{
			name: "success returns groups",
			register: func(reg *auth.Registry) {
				require.NoError(t, reg.Register(&mockGroupsHandler{
					MockHandler: auth.NewMockHandler("entra"),
					groups:      []string{"group-a", "group-b"},
				}))
			},
			handler:    "entra",
			wantGroups: []string{"group-a", "group-b"},
		},
		{
			name: "success returns empty membership",
			register: func(reg *auth.Registry) {
				require.NoError(t, reg.Register(&mockGroupsHandler{
					MockHandler: auth.NewMockHandler("entra"),
					groups:      []string{},
				}))
			},
			handler:    "entra",
			wantGroups: []string{},
		},
		{
			name: "empty handler resolves to default",
			register: func(reg *auth.Registry) {
				require.NoError(t, reg.Register(&mockGroupsHandler{
					MockHandler: auth.NewMockHandler("alpha"),
					groups:      []string{"alpha-group"},
				}))
				require.NoError(t, reg.Register(&mockGroupsHandler{
					MockHandler: auth.NewMockHandler("beta"),
					groups:      []string{"beta-group"},
				}))
			},
			handler:    "",
			wantGroups: []string{"alpha-group"},
		},
		{
			name:     "no handlers registered",
			register: func(_ *auth.Registry) {},
			handler:  "",
			wantErr:  "no auth handlers registered",
		},
		{
			name:     "unknown handler",
			register: func(_ *auth.Registry) {},
			handler:  "missing",
			wantErr:  `auth handler "missing"`,
		},
		{
			name: "handler does not implement GroupsProvider",
			register: func(reg *auth.Registry) {
				require.NoError(t, reg.Register(auth.NewMockHandler("plain")))
			},
			handler: "plain",
			wantErr: "does not support group membership queries (GroupsProvider not implemented)",
		},
		{
			name: "GetGroups returns an error",
			register: func(reg *auth.Registry) {
				require.NoError(t, reg.Register(&mockGroupsHandler{
					MockHandler: auth.NewMockHandler("entra"),
					groupErr:    fmt.Errorf("graph API unavailable"),
				}))
			},
			handler: "entra",
			wantErr: `get groups from "entra": graph API unavailable`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := auth.NewRegistry()
			tt.register(reg)

			deps := HostDepsFromAuthRegistry(reg)
			require.NotNil(t, deps.AuthGroupsFunc)

			groups, err := deps.AuthGroupsFunc(context.Background(), tt.handler)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Nil(t, groups)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantGroups, groups)
		})
	}
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
