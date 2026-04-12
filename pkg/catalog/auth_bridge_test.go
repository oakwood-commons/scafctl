// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBridgeAuthToRegistry(t *testing.T) {
	tests := []struct {
		name         string
		handlerName  string
		registryHost string
		scope        string
		token        string
		claims       *auth.Claims
		wantUsername string
		wantPassword string
		wantErr      bool
	}{
		{
			name:         "github handler for ghcr.io",
			handlerName:  "github",
			registryHost: "ghcr.io",
			token:        "ghs_abc123",
			claims:       &auth.Claims{Username: "octocat"},
			wantUsername: "octocat",
			wantPassword: "ghs_abc123",
		},
		{
			name:         "github handler with no username in claims",
			handlerName:  "github",
			registryHost: "ghcr.io",
			token:        "ghs_abc123",
			claims:       &auth.Claims{},
			wantUsername: RegistryUsernameDefault,
			wantPassword: "ghs_abc123",
		},
		{
			name:         "gcp handler for gcr.io",
			handlerName:  "gcp",
			registryHost: "gcr.io",
			token:        "ya29.gcp-token",
			claims:       &auth.Claims{},
			wantUsername: RegistryUsernameDefault,
			wantPassword: "ya29.gcp-token",
		},
		{
			name:         "gcp handler for artifact registry",
			handlerName:  "gcp",
			registryHost: "us-docker.pkg.dev",
			token:        "ya29.gcp-token",
			claims:       &auth.Claims{},
			wantUsername: RegistryUsernameDefault,
			wantPassword: "ya29.gcp-token",
		},
		{
			name:         "entra handler for ACR",
			handlerName:  "entra",
			registryHost: "myacr.azurecr.io",
			token:        "eyJ0entra-token",
			claims:       &auth.Claims{},
			wantUsername: RegistryUsernameACR,
			wantPassword: "eyJ0entra-token",
		},
		{
			name:         "generic handler uses oauth2accesstoken",
			handlerName:  "quay",
			registryHost: "quay.io",
			token:        "quay-token",
			claims:       &auth.Claims{},
			wantUsername: RegistryUsernameDefault,
			wantPassword: "quay-token",
		},
		{
			name:         "with scope",
			handlerName:  "gcp",
			registryHost: "gcr.io",
			scope:        "https://www.googleapis.com/auth/cloud-platform",
			token:        "scoped-token",
			claims:       &auth.Claims{},
			wantUsername: RegistryUsernameDefault,
			wantPassword: "scoped-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := auth.NewMockHandler(tt.handlerName)
			mock.GetTokenResult = &auth.Token{
				AccessToken: tt.token,
				ExpiresAt:   time.Now().Add(time.Hour),
			}
			mock.StatusResult = &auth.Status{
				Authenticated: true,
				Claims:        tt.claims,
			}

			username, password, err := BridgeAuthToRegistry(
				context.Background(), mock, tt.registryHost, tt.scope)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantUsername, username)
			assert.Equal(t, tt.wantPassword, password)
		})
	}
}

func TestBridgeAuthToRegistry_TokenError(t *testing.T) {
	mock := auth.NewMockHandler("github")
	mock.GetTokenErr = auth.ErrNotAuthenticated

	_, _, err := BridgeAuthToRegistry(context.Background(), mock, "ghcr.io", "")
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrNotAuthenticated)
}

func TestInferAuthHandler(t *testing.T) {
	customHandlers := []config.CustomOAuth2Config{
		{Name: "quay", Registry: "quay.io"},
		{Name: "gitlab", Registry: "registry.gitlab.com"},
		{Name: "myapi"}, // no registry field
	}

	tests := []struct {
		host        string
		wantHandler string
	}{
		{"ghcr.io", "github"},
		{"us-docker.pkg.dev", "gcp"},
		{"gcr.io", "gcp"},
		{"us.gcr.io", "gcp"},
		{"myacr.azurecr.io", "entra"},
		{"quay.io", "quay"},
		{"registry.gitlab.com", "gitlab"},
		{"registry-1.docker.io", ""},
		{"unknown-registry.example.com", ""},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			result := InferAuthHandler(tt.host, customHandlers)
			assert.Equal(t, tt.wantHandler, result)
		})
	}
}

func TestInferAuthHandler_NoCustomHandlers(t *testing.T) {
	assert.Equal(t, "github", InferAuthHandler("ghcr.io", nil))
	assert.Equal(t, "", InferAuthHandler("quay.io", nil))
}

func TestIsBuiltinHandlerName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"github", true},
		{"gcp", true},
		{"entra", true},
		{"quay", false},
		{"custom", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsBuiltinHandlerName(tt.name))
		})
	}
}

// Benchmarks

func BenchmarkInferAuthHandler(b *testing.B) {
	customHandlers := []config.CustomOAuth2Config{
		{Name: "quay", Registry: "quay.io"},
		{Name: "gitlab", Registry: "registry.gitlab.com"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		InferAuthHandler("ghcr.io", customHandlers)
	}
}

func BenchmarkBridgeAuthToRegistry(b *testing.B) {
	mock := auth.NewMockHandler("github")
	mock.GetTokenResult = &auth.Token{
		AccessToken: "ghs_abc123",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	mock.StatusResult = &auth.Status{
		Authenticated: true,
		Claims:        &auth.Claims{Username: "octocat"},
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = BridgeAuthToRegistry(ctx, mock, "ghcr.io", "")
	}
}

func TestInferDefaultScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		registry string
		want     string
	}{
		{"us-central1-docker.pkg.dev", "https://www.googleapis.com/auth/cloud-platform"},
		{"gcr.io", "https://www.googleapis.com/auth/cloud-platform"},
		{"us.gcr.io", "https://www.googleapis.com/auth/cloud-platform"},
		{"ghcr.io", ""},
		{"myregistry.azurecr.io", ""},
		{"docker.io", ""},
	}

	for _, tc := range tests {
		t.Run(tc.registry, func(t *testing.T) {
			t.Parallel()
			got := InferDefaultScope(tc.registry)
			assert.Equal(t, tc.want, got)
		})
	}
}
