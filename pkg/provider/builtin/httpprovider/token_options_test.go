// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"testing"
	"time"

	sdkauth "github.com/oakwood-commons/scafctl-plugin-sdk/auth"
	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
)

func TestWithServerContext_AlwaysSetsDelegate(t *testing.T) {
	opts := auth.TokenOptions{Scope: "https://graph.microsoft.com/.default"}

	result := withServerContext(context.Background(), "entra", opts)

	assert.Equal(t, sdkauth.ServerContextDelegated, result.ServerContext)
}

func TestWithAssertion_MatchingProvider(t *testing.T) {
	ctx := middleware.WithOIDCProvider(context.Background(), "entra")
	ctx = middleware.WithAccessToken(ctx, "eyJ0eXAiOiJKV1Qi.user-jwt-token")
	opts := auth.TokenOptions{Scope: "scope"}

	result := withAssertion(ctx, "entra", opts)

	assert.Equal(t, "eyJ0eXAiOiJKV1Qi.user-jwt-token", result.Assertion)
}

func TestWithAssertion_NonMatchingProvider(t *testing.T) {
	ctx := middleware.WithOIDCProvider(context.Background(), "entra")
	ctx = middleware.WithAccessToken(ctx, "eyJ0eXAiOiJKV1Qi.user-jwt-token")
	opts := auth.TokenOptions{Scope: "scope"}

	result := withAssertion(ctx, "github", opts)

	assert.Empty(t, result.Assertion)
}

func TestWithAssertion_NoOIDCProvider(t *testing.T) {
	opts := auth.TokenOptions{Scope: "scope"}

	result := withAssertion(context.Background(), "entra", opts)

	assert.Empty(t, result.Assertion)
}

func TestWithCallerType_UserToken(t *testing.T) {
	ctx := middleware.WithOIDCProvider(context.Background(), "entra")
	ctx = middleware.WithAuthClaims(ctx, &middleware.AuthClaims{
		Subject: "user-oid",
		IDType:  "", // absence means user
	})
	opts := auth.TokenOptions{}

	result := withCallerType(ctx, "entra", opts)

	assert.Equal(t, sdkauth.CallerUser, result.Caller)
}

func TestWithCallerType_AppToken(t *testing.T) {
	ctx := middleware.WithOIDCProvider(context.Background(), "entra")
	ctx = middleware.WithAuthClaims(ctx, &middleware.AuthClaims{
		Subject: "app-oid",
		IDType:  "app",
	})
	opts := auth.TokenOptions{}

	result := withCallerType(ctx, "entra", opts)

	assert.Equal(t, sdkauth.CallerMachine, result.Caller)
}

func TestWithCallerType_NonMatchingProvider(t *testing.T) {
	ctx := middleware.WithOIDCProvider(context.Background(), "entra")
	ctx = middleware.WithAuthClaims(ctx, &middleware.AuthClaims{
		Subject: "user-oid",
		IDType:  "",
	})
	opts := auth.TokenOptions{}

	result := withCallerType(ctx, "github", opts)

	assert.Equal(t, sdkauth.CallerType(""), result.Caller)
}

func TestWithCallerType_NoClaims(t *testing.T) {
	ctx := middleware.WithOIDCProvider(context.Background(), "entra")
	opts := auth.TokenOptions{}

	result := withCallerType(ctx, "entra", opts)

	assert.Equal(t, sdkauth.CallerType(""), result.Caller)
}

func TestTokenOptionChain_AllApplied(t *testing.T) {
	ctx := middleware.WithOIDCProvider(context.Background(), "entra")
	ctx = middleware.WithAccessToken(ctx, "user-assertion-jwt")
	ctx = middleware.WithAuthClaims(ctx, &middleware.AuthClaims{
		Subject: "user-sub",
		IDType:  "",
	})

	opts := auth.TokenOptions{
		Scope:       "https://graph.microsoft.com/.default",
		MinValidFor: 90 * time.Second,
	}

	result := tokenOptionChain(ctx, "entra", opts, withServerContext, withAssertion, withCallerType)

	assert.Equal(t, sdkauth.ServerContextDelegated, result.ServerContext)
	assert.Equal(t, "user-assertion-jwt", result.Assertion)
	assert.Equal(t, sdkauth.CallerUser, result.Caller)
	// Original fields preserved.
	assert.Equal(t, "https://graph.microsoft.com/.default", result.Scope)
	assert.Equal(t, 90*time.Second, result.MinValidFor)
}

func TestTokenOptionChain_NoOIDCContext(t *testing.T) {
	opts := auth.TokenOptions{
		Scope:       "scope",
		MinValidFor: 60 * time.Second,
	}

	result := tokenOptionChain(context.Background(), "entra", opts, withServerContext, withAssertion, withCallerType)

	// ServerContext is always set to Delegate.
	assert.Equal(t, sdkauth.ServerContextDelegated, result.ServerContext)
	// Without OIDC middleware context, assertion and caller are not set.
	assert.Empty(t, result.Assertion)
	assert.Equal(t, sdkauth.CallerType(""), result.Caller)
}

func TestTokenOptionChain_EmptyFuncs(t *testing.T) {
	opts := auth.TokenOptions{Scope: "original"}

	result := tokenOptionChain(context.Background(), "entra", opts)

	assert.Equal(t, "original", result.Scope)
}

func TestWithTokenHostname(t *testing.T) {
	t.Parallel()
	opts := auth.TokenOptions{Scope: "scope"}
	updated := withTokenHostname("https://api.cluster-a.example.com:6443")(context.Background(), "openshift", opts)
	assert.Equal(t, "https://api.cluster-a.example.com:6443", updated.Hostname)

	unchanged := withTokenHostname("")(context.Background(), "openshift", updated)
	assert.Equal(t, "https://api.cluster-a.example.com:6443", unchanged.Hostname)
}

func TestDeriveOriginHostname(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		rawURL  string
		want    string
		wantErr bool
	}{
		{
			name:   "https with custom port and path",
			rawURL: "https://api.cluster-a.example.com:6443/apis/v1/namespaces?limit=10#top",
			want:   "https://api.cluster-a.example.com:6443",
		},
		{
			name:   "normalizes uppercase scheme and host and strips userinfo and trailing slash",
			rawURL: "HTTPS://admin:secret@API.Cluster-B.Example.COM:6443/",
			want:   "https://api.cluster-b.example.com:6443",
		},
		{
			name:   "preserves bracketed IPv6 with port",
			rawURL: "https://[2001:DB8::1]:6443/api/v1/pods",
			want:   "https://[2001:db8::1]:6443",
		},
		{
			name:   "preserves loopback IPv6 with port",
			rawURL: "https://[::1]:6443/version",
			want:   "https://[::1]:6443",
		},
		{
			name:   "standard https without explicit port",
			rawURL: "https://api.example.com/v1/items",
			want:   "https://api.example.com",
		},
		{
			name:    "rejects relative URL",
			rawURL:  "/api/v1/pods",
			wantErr: true,
		},
		{
			name:    "rejects missing host",
			rawURL:  "https:///api/v1/pods",
			wantErr: true,
		},
		{
			name:    "rejects malformed URL",
			rawURL:  "://bad-url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := deriveOriginHostname(tt.rawURL)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
