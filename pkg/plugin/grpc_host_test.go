// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestValidatePastedRedirectURL_SchemeLessLoopback(t *testing.T) {
	t.Parallel()

	okCases := []struct {
		name        string
		value       string
		redirectURI string
		want        string
	}{
		{
			name:        "scheme-less localhost with callback path",
			value:       "localhost:8400/callback?code=abc&state=xyz",
			redirectURI: "http://localhost:8400/callback",
			want:        "http://localhost:8400/callback?code=abc&state=xyz",
		},
		{
			name:        "scheme-less localhost root path with leading and trailing whitespace",
			value:       "  localhost:45189/?code=abc&state=xyz  ",
			redirectURI: "http://localhost:45189",
			want:        "http://localhost:45189/?code=abc&state=xyz",
		},
		{
			name:        "scheme-less localhost query without slash",
			value:       "localhost:45189?code=abc",
			redirectURI: "http://localhost:45189",
			want:        "http://localhost:45189?code=abc",
		},
		{
			name:        "scheme-less localhost fragment token",
			value:       "localhost:45189#access_token=tok",
			redirectURI: "http://localhost:45189",
			want:        "http://localhost:45189#access_token=tok",
		},
		{
			name:        "uppercase host canonicalized while query case preserved",
			value:       "http://LOCALHOST:8400/callback?code=MixedCase",
			redirectURI: "http://localhost:8400/callback",
			want:        "http://localhost:8400/callback?code=MixedCase",
		},
		{
			name:        "scheme-less 127.0.0.1 loopback",
			value:       "127.0.0.1:8400/callback?code=abc",
			redirectURI: "http://127.0.0.1:8400/callback",
			want:        "http://127.0.0.1:8400/callback?code=abc",
		},
		{
			name:        "scheme-less 127.0.0.2 loopback",
			value:       "127.0.0.2:8400/callback?code=abc",
			redirectURI: "http://127.0.0.2:8400/callback",
			want:        "http://127.0.0.2:8400/callback?code=abc",
		},
		{
			name:        "scheme-less IPv6 loopback",
			value:       "[::1]:8400/callback?code=abc",
			redirectURI: "http://[::1]:8400/callback",
			want:        "http://[::1]:8400/callback?code=abc",
		},
		{
			name:        "scheme-less localhost with URL-valued error_uri query parameter",
			value:       "localhost:8400/callback?error=access_denied&error_uri=https://example.com/help",
			redirectURI: "http://localhost:8400/callback",
			want:        "http://localhost:8400/callback?error=access_denied&error_uri=https://example.com/help",
		},
		{
			name:        "scheme-less 127.0.0.1 with URL-valued iss parameter",
			value:       "127.0.0.1:8400/callback?code=abc&iss=https://issuer.example.com",
			redirectURI: "http://127.0.0.1:8400/callback",
			want:        "http://127.0.0.1:8400/callback?code=abc&iss=https://issuer.example.com",
		},
	}

	for _, tc := range okCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizePastedRedirectURL(tc.value, tc.redirectURI)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.NoError(t, validatePastedRedirectURL(tc.value, tc.redirectURI))
		})
	}

	errCases := []struct {
		name        string
		value       string
		redirectURI string
	}{
		{
			name:        "whitespace-only value rejected",
			value:       "   \t\n  ",
			redirectURI: "http://localhost:8400/callback",
		},
		{
			name:        "localhost used as outer scheme rejected",
			value:       "localhost://callback?code=SECRET",
			redirectURI: "http://localhost",
		},
		{
			name:        "scheme-less port prefix lookalike rejected",
			value:       "localhost:84000/callback?code=SECRET",
			redirectURI: "http://localhost:8400/callback",
		},
		{
			name:        "scheme-less wrong port rejected",
			value:       "localhost:9999/callback?code=SECRET",
			redirectURI: "http://localhost:8400/callback",
		},
		{
			name:        "scheme-less wrong path rejected",
			value:       "localhost:8400/wrong?code=SECRET",
			redirectURI: "http://localhost:8400/callback",
		},
		{
			name:        "path segment boundary lookalike rejected",
			value:       "http://localhost:8400/callback-evil?code=SECRET",
			redirectURI: "http://localhost:8400/callback",
		},
		{
			name:        "userinfo in loopback URL rejected",
			value:       "http://evil@localhost:8400/callback?code=SECRET",
			redirectURI: "http://localhost:8400/callback",
		},
		{
			name:        "scheme-less rejected when expected redirect uses https",
			value:       "localhost:8400/callback?code=SECRET",
			redirectURI: "https://localhost:8400/callback",
		},
		{
			name:        "scheme-less non-loopback host rejected",
			value:       "example.com:8400/callback?code=SECRET",
			redirectURI: "http://example.com:8400/callback",
		},
	}

	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validatePastedRedirectURL(tc.value, tc.redirectURI)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "SECRET", "validation error must never leak pasted value")
		})
	}
}

func TestHostServiceServer_PromptAuthResponse_NormalizesSchemeLessLoopbackPaste(t *testing.T) {
	t.Parallel()

	broker := &AuthPromptBroker{}
	server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
	defer broker.Begin("entra", func(_ context.Context, _, _ string) (string, error) {
		return "  localhost:45189/?code=abc&state=xyz  ", nil
	})()

	resp, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
		HandlerName:      "entra",
		AuthorizationUrl: "https://login.example/authorize",
		RedirectUri:      "http://localhost:45189",
	})
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:45189/?code=abc&state=xyz", resp.Value)
}

func TestHostServiceServer_PromptAuthResponse_RejectsMismatchedSchemeLessPaste(t *testing.T) {
	t.Parallel()

	broker := &AuthPromptBroker{}
	server := &HostServiceServer{Deps: HostServiceDeps{PromptBroker: broker}}
	defer broker.Begin("entra", func(_ context.Context, _, _ string) (string, error) {
		return "localhost:9999/?code=SECRET", nil
	})()

	_, err := server.PromptAuthResponse(context.Background(), &proto.PromptAuthResponseRequest{
		HandlerName:      "entra",
		AuthorizationUrl: "https://login.example/authorize",
		RedirectUri:      "http://localhost:45189",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.NotContains(t, err.Error(), "SECRET")
}

func BenchmarkNormalizePastedRedirectURL_SchemeLess(b *testing.B) {
	paste := "localhost:8400/callback?code=a-long-authorization-code&state=some-state"
	redirect := "http://localhost:8400/callback"
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := normalizePastedRedirectURL(paste, redirect); err != nil {
			b.Fatal(err)
		}
	}
}
