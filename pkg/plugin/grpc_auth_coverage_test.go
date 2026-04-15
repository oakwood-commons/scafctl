// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── claimsToProto / protoToClaims round-trip tests ────────────────────────────

func TestClaimsToProto_NilInput(t *testing.T) {
	t.Parallel()
	result := claimsToProto(nil)
	assert.Nil(t, result)
}

func TestProtoToClaims_NilInput(t *testing.T) {
	t.Parallel()
	result := protoToClaims(nil)
	assert.Nil(t, result)
}

func TestClaimsRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	original := &auth.Claims{
		Issuer:    "https://login.microsoftonline.com/tenant-id/v2.0",
		Subject:   "subject-123",
		TenantID:  "tenant-id",
		ObjectID:  "object-id",
		ClientID:  "client-id",
		Email:     "user@example.com",
		Name:      "Test User",
		Username:  "testuser",
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}

	protoClaims := claimsToProto(original)
	require.NotNil(t, protoClaims)

	roundTripped := protoToClaims(protoClaims)
	require.NotNil(t, roundTripped)

	assert.Equal(t, original.Issuer, roundTripped.Issuer)
	assert.Equal(t, original.Subject, roundTripped.Subject)
	assert.Equal(t, original.TenantID, roundTripped.TenantID)
	assert.Equal(t, original.ObjectID, roundTripped.ObjectID)
	assert.Equal(t, original.ClientID, roundTripped.ClientID)
	assert.Equal(t, original.Email, roundTripped.Email)
	assert.Equal(t, original.Name, roundTripped.Name)
	assert.Equal(t, original.Username, roundTripped.Username)
	assert.Equal(t, original.IssuedAt.Unix(), roundTripped.IssuedAt.Unix())
	assert.Equal(t, original.ExpiresAt.Unix(), roundTripped.ExpiresAt.Unix())
}

// ── statusToProto / protoToStatus round-trip tests ────────────────────────────

func TestStatusToProto_NilInput(t *testing.T) {
	t.Parallel()
	result := statusToProto(nil)
	require.NotNil(t, result) // returns empty response, not nil
	assert.False(t, result.Authenticated)
}

func TestProtoToStatus_NilInput(t *testing.T) {
	t.Parallel()
	result := protoToStatus(nil)
	require.NotNil(t, result) // returns empty status, not nil
	assert.False(t, result.Authenticated)
}

func TestStatusRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	original := &auth.Status{
		Authenticated: true,
		Claims: &auth.Claims{
			Email: "user@example.com",
			Name:  "Test User",
		},
		ExpiresAt:    now.Add(time.Hour),
		LastRefresh:  now,
		TenantID:     "tenant-id",
		IdentityType: auth.IdentityType("user"),
		ClientID:     "client-id",
		TokenFile:    "/tmp/token",
		Scopes:       []string{"openid", "profile"},
	}

	protoStatus := statusToProto(original)
	require.NotNil(t, protoStatus)
	assert.True(t, protoStatus.Authenticated)

	roundTripped := protoToStatus(protoStatus)
	require.NotNil(t, roundTripped)

	assert.Equal(t, original.Authenticated, roundTripped.Authenticated)
	assert.Equal(t, original.TenantID, roundTripped.TenantID)
	assert.Equal(t, original.ClientID, roundTripped.ClientID)
	assert.Equal(t, original.TokenFile, roundTripped.TokenFile)
	assert.Equal(t, original.Scopes, roundTripped.Scopes)
	assert.Equal(t, original.IdentityType, roundTripped.IdentityType)
	assert.Equal(t, original.ExpiresAt.Unix(), roundTripped.ExpiresAt.Unix())
	assert.Equal(t, original.LastRefresh.Unix(), roundTripped.LastRefresh.Unix())
}

// ── tokenResponseToProto / protoToTokenResponse round-trip tests ──────────────

func TestTokenResponseToProto_NilInput(t *testing.T) {
	t.Parallel()
	result := tokenResponseToProto(nil)
	require.NotNil(t, result) // returns empty response
}

func TestProtoToTokenResponse_NilInput(t *testing.T) {
	t.Parallel()
	result := protoToTokenResponse(nil)
	require.NotNil(t, result)
}

func TestTokenResponseRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	original := &TokenResponse{
		AccessToken: "eyJhbGciOi...",
		TokenType:   "Bearer",
		ExpiresAt:   now.Add(time.Hour),
		Scope:       "openid profile",
		CachedAt:    now,
		Flow:        auth.FlowDeviceCode,
		SessionID:   "session-123",
	}

	protoResp := tokenResponseToProto(original)
	require.NotNil(t, protoResp)

	roundTripped := protoToTokenResponse(protoResp)
	require.NotNil(t, roundTripped)

	assert.Equal(t, original.AccessToken, roundTripped.AccessToken)
	assert.Equal(t, original.TokenType, roundTripped.TokenType)
	assert.Equal(t, original.ExpiresAt.Unix(), roundTripped.ExpiresAt.Unix())
	assert.Equal(t, original.Scope, roundTripped.Scope)
	assert.Equal(t, original.CachedAt.Unix(), roundTripped.CachedAt.Unix())
	assert.Equal(t, original.Flow, roundTripped.Flow)
	assert.Equal(t, original.SessionID, roundTripped.SessionID)
}

// ── cachedTokenInfoToProto / protoToCachedTokenInfo round-trip tests ──────────

func TestCachedTokenInfoToProto_NilInput(t *testing.T) {
	t.Parallel()
	result := cachedTokenInfoToProto(nil)
	require.NotNil(t, result)
}

func TestProtoCachedTokenInfo_NilInput(t *testing.T) {
	t.Parallel()
	result := protoToCachedTokenInfo(nil)
	require.NotNil(t, result)
}

func TestCachedTokenInfoRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	original := &auth.CachedTokenInfo{
		Handler:   "entra",
		TokenKind: "access_token",
		Scope:     "openid",
		TokenType: "Bearer",
		Flow:      auth.FlowServicePrincipal,
		ExpiresAt: now.Add(time.Hour),
		CachedAt:  now,
		IsExpired: false,
		SessionID: "session-xyz",
	}

	protoInfo := cachedTokenInfoToProto(original)
	require.NotNil(t, protoInfo)

	roundTripped := protoToCachedTokenInfo(protoInfo)
	require.NotNil(t, roundTripped)

	assert.Equal(t, original.Handler, roundTripped.Handler)
	assert.Equal(t, original.TokenKind, roundTripped.TokenKind)
	assert.Equal(t, original.Scope, roundTripped.Scope)
	assert.Equal(t, original.TokenType, roundTripped.TokenType)
	assert.Equal(t, original.Flow, roundTripped.Flow)
	assert.Equal(t, original.ExpiresAt.Unix(), roundTripped.ExpiresAt.Unix())
	assert.Equal(t, original.CachedAt.Unix(), roundTripped.CachedAt.Unix())
	assert.Equal(t, original.IsExpired, roundTripped.IsExpired)
	assert.Equal(t, original.SessionID, roundTripped.SessionID)
}

// ── AuthHandlerGRPCServer conversion tests ────────────────────────────────────

func TestAuthHandlerGRPCServer_GetAuthHandlers_EmptyList(t *testing.T) {
	t.Parallel()

	server := &AuthHandlerGRPCServer{
		Impl: &MockAuthHandlerPlugin{
			handlers: []AuthHandlerInfo{},
		},
	}

	resp, err := server.GetAuthHandlers(t.Context(), &proto.GetAuthHandlersRequest{})
	require.NoError(t, err)
	assert.Empty(t, resp.Handlers)
}

func TestAuthHandlerGRPCServer_GetAuthHandlers_MultipleHandlers(t *testing.T) {
	t.Parallel()

	server := &AuthHandlerGRPCServer{
		Impl: &MockAuthHandlerPlugin{
			handlers: []AuthHandlerInfo{
				{
					Name:         "entra",
					DisplayName:  "Microsoft Entra ID",
					Flows:        []auth.Flow{auth.FlowDeviceCode, auth.FlowServicePrincipal},
					Capabilities: []auth.Capability{auth.CapTenantID, auth.CapScopesOnLogin},
				},
				{
					Name:         "github",
					DisplayName:  "GitHub",
					Flows:        []auth.Flow{auth.FlowInteractive, auth.FlowPAT},
					Capabilities: []auth.Capability{auth.CapHostname},
				},
			},
		},
	}

	resp, err := server.GetAuthHandlers(t.Context(), &proto.GetAuthHandlersRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Handlers, 2)

	assert.Equal(t, "entra", resp.Handlers[0].Name)
	assert.Equal(t, "Microsoft Entra ID", resp.Handlers[0].DisplayName)
	assert.Len(t, resp.Handlers[0].Flows, 2)
	assert.Len(t, resp.Handlers[0].Capabilities, 2)

	assert.Equal(t, "github", resp.Handlers[1].Name)
}

func TestAuthHandlerGRPCServer_Logout(t *testing.T) {
	t.Parallel()

	var logoutCalled string
	server := &AuthHandlerGRPCServer{
		Impl: &MockAuthHandlerPlugin{
			logoutFunc: func(_ context.Context, name string) error {
				logoutCalled = name
				return nil
			},
		},
	}

	resp, err := server.Logout(t.Context(), &proto.LogoutRequest{HandlerName: "entra"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "entra", logoutCalled)
}

func TestAuthHandlerGRPCServer_GetStatus(t *testing.T) {
	t.Parallel()

	server := &AuthHandlerGRPCServer{
		Impl: &MockAuthHandlerPlugin{
			statusFunc: func(_ context.Context, _ string) (*auth.Status, error) {
				return &auth.Status{
					Authenticated: true,
					Claims:        &auth.Claims{Email: "user@test.com"},
				}, nil
			},
		},
	}

	resp, err := server.GetStatus(t.Context(), &proto.GetStatusRequest{HandlerName: "entra"})
	require.NoError(t, err)
	assert.True(t, resp.Authenticated)
	assert.Equal(t, "user@test.com", resp.Claims.Email)
}

func TestAuthHandlerGRPCServer_GetToken(t *testing.T) {
	t.Parallel()

	server := &AuthHandlerGRPCServer{
		Impl: &MockAuthHandlerPlugin{
			tokenFunc: func(_ context.Context, _ string, req TokenRequest) (*TokenResponse, error) {
				return &TokenResponse{
					AccessToken: "test-token",
					TokenType:   "Bearer",
					Scope:       req.Scope,
				}, nil
			},
		},
	}

	resp, err := server.GetToken(t.Context(), &proto.GetTokenRequest{
		HandlerName:        "entra",
		Scope:              "openid",
		MinValidForSeconds: 300,
	})
	require.NoError(t, err)
	assert.Equal(t, "test-token", resp.AccessToken)
	assert.Equal(t, "openid", resp.Scope)
}

func TestAuthHandlerGRPCServer_ListCachedTokens(t *testing.T) {
	t.Parallel()

	now := time.Now()
	server := &AuthHandlerGRPCServer{
		Impl: &MockAuthHandlerPlugin{
			listFunc: func(_ context.Context, _ string) ([]*auth.CachedTokenInfo, error) {
				return []*auth.CachedTokenInfo{
					{Handler: "entra", TokenKind: "access", ExpiresAt: now.Add(time.Hour)},
					{Handler: "entra", TokenKind: "refresh", ExpiresAt: now.Add(24 * time.Hour)},
				}, nil
			},
		},
	}

	resp, err := server.ListCachedTokens(t.Context(), &proto.ListCachedTokensRequest{HandlerName: "entra"})
	require.NoError(t, err)
	assert.Len(t, resp.Tokens, 2)
	assert.Equal(t, "access", resp.Tokens[0].TokenKind)
	assert.Equal(t, "refresh", resp.Tokens[1].TokenKind)
}

func TestAuthHandlerGRPCServer_PurgeExpiredTokens(t *testing.T) {
	t.Parallel()

	server := &AuthHandlerGRPCServer{
		Impl: &MockAuthHandlerPlugin{
			purgeFunc: func(_ context.Context, _ string) (int, error) {
				return 3, nil
			},
		},
	}

	resp, err := server.PurgeExpiredTokens(t.Context(), &proto.PurgeExpiredTokensRequest{HandlerName: "entra"})
	require.NoError(t, err)
	assert.Equal(t, int32(3), resp.PurgedCount)
}

// ── AuthHandlerGRPCPlugin tests ──────────────────────────────────────────────

func TestAuthHandlerPluginName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "auth-handler", AuthHandlerPluginName)
}

// ── Benchmark tests ───────────────────────────────────────────────────────────

func BenchmarkClaimsRoundTrip(b *testing.B) {
	now := time.Now()
	claims := &auth.Claims{
		Issuer:    "issuer",
		Subject:   "subject",
		TenantID:  "tenant",
		Email:     "user@example.com",
		Name:      "User",
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p := claimsToProto(claims)
		_ = protoToClaims(p)
	}
}

func BenchmarkStatusRoundTrip(b *testing.B) {
	now := time.Now()
	status := &auth.Status{
		Authenticated: true,
		Claims:        &auth.Claims{Email: "user@test.com"},
		ExpiresAt:     now.Add(time.Hour),
		Scopes:        []string{"openid", "profile"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p := statusToProto(status)
		_ = protoToStatus(p)
	}
}
