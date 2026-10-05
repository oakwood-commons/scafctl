// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const gcpCloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

func TestAuthProviderForRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		cfg      *config.Config
		want     string
	}{
		{name: "nil config infers from host", registry: "ghcr.io", want: "github"},
		{name: "nil config unknown host", registry: "registry.example.com", want: ""},
		{
			name:     "host-matched catalog wins over inference",
			registry: "ghcr.io",
			cfg: &config.Config{Catalogs: []config.CatalogConfig{
				{Name: "gh", URL: "oci://ghcr.io/myorg", AuthProvider: "custom-gh"},
			}},
			want: "custom-gh",
		},
		{
			name:     "catalog on another host is ignored",
			registry: "us-central1-docker.pkg.dev",
			cfg: &config.Config{Catalogs: []config.CatalogConfig{
				{Name: "quay", URL: "oci://quay.io/myorg", AuthProvider: "quay"},
			}},
			want: "gcp",
		},
		{
			name:     "custom OAuth2 registry mapping",
			registry: "quay.io",
			cfg: &config.Config{Auth: config.GlobalAuthConfig{CustomOAuth2: []config.CustomOAuth2Config{
				{Name: "quay", Registry: "quay.io"},
			}}},
			want: "quay",
		},
		{
			// The default catalog's handler must never be sent to an unrelated host.
			name:     "default catalog is not a fallback",
			registry: "registry.example.com",
			cfg: &config.Config{
				Catalogs: []config.CatalogConfig{
					{Name: "default", URL: "oci://us-central1-docker.pkg.dev/proj/repo", AuthProvider: "gcp"},
				},
				Settings: config.Settings{DefaultCatalog: "default"},
			},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tc.cfg != nil {
				ctx = config.WithConfig(ctx, tc.cfg)
			}
			assert.Equal(t, tc.want, AuthProviderForRegistry(ctx, tc.registry))
		})
	}
}

func TestAuthHandlerForRegistry(t *testing.T) {
	t.Parallel()

	t.Run("registered handler is returned", func(t *testing.T) {
		t.Parallel()
		ctx := configureAuthRegistry(t, context.Background(), auth.NewMockHandler("github"))
		h := AuthHandlerForRegistry(ctx, "ghcr.io")
		require.NotNil(t, h)
		assert.Equal(t, "github", h.Name())
	})

	t.Run("unregistered handler is nil", func(t *testing.T) {
		t.Parallel()
		ctx := configureAuthRegistry(t, context.Background(), auth.NewMockHandler("gcp"))
		assert.Nil(t, AuthHandlerForRegistry(ctx, "ghcr.io"))
	})

	t.Run("no provider is nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, AuthHandlerForRegistry(context.Background(), "registry.example.com"))
	})
}

func TestAuthScopeForRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		cfg      *config.Config
		want     string
	}{
		{
			name:     "matches catalog by registry host",
			registry: "us-central1-docker.pkg.dev",
			cfg: &config.Config{Catalogs: []config.CatalogConfig{
				{Name: "gcp", URL: "oci://us-central1-docker.pkg.dev/proj/repo", AuthScope: "custom-scope"},
			}},
			want: "custom-scope",
		},
		{
			name:     "no match falls back to InferDefaultScope for GCP",
			registry: "us-central1-docker.pkg.dev",
			cfg: &config.Config{Catalogs: []config.CatalogConfig{
				{Name: "other", URL: "oci://ghcr.io/myorg"},
			}},
			want: gcpCloudPlatformScope,
		},
		{
			name:     "no match and no inference returns empty",
			registry: "ghcr.io",
			cfg: &config.Config{Catalogs: []config.CatalogConfig{
				{Name: "gcp", URL: "oci://us-central1-docker.pkg.dev/proj/repo", AuthScope: "cloud-platform"},
			}},
			want: "",
		},
		{name: "nil config falls back to InferDefaultScope", registry: "us-east1-docker.pkg.dev", want: gcpCloudPlatformScope},
		{name: "nil config unknown registry returns empty", registry: "ghcr.io", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tc.cfg != nil {
				ctx = config.WithConfig(ctx, tc.cfg)
			}
			assert.Equal(t, tc.want, AuthScopeForRegistry(ctx, tc.registry))
		})
	}
}

func TestContextRemoteFactory(t *testing.T) {
	t.Parallel()

	factory := ContextRemoteFactory(logr.Discard())

	t.Run("builds an HTTPS remote keyed by the origin", func(t *testing.T) {
		t.Parallel()
		ctx := configureAuthRegistry(t, context.Background(), auth.NewMockHandler("github"))

		remote, err := factory(ctx, "ghcr.io/myorg/solutions")
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/myorg/solutions", remote.CanonicalID())

		rc, ok := remote.(*RemoteCatalog)
		require.True(t, ok)
		assert.Equal(t, "ghcr.io", rc.Registry())
		assert.Equal(t, "myorg/solutions", rc.Repository())
		assert.False(t, rc.insecure, "factory-built remotes must never allow plain HTTP")
		assert.Equal(t, "github", rc.authHandlerUsed)
	})

	t.Run("registry without repository", func(t *testing.T) {
		t.Parallel()
		remote, err := factory(context.Background(), "registry.example.com")
		require.NoError(t, err)
		assert.Equal(t, "registry.example.com", remote.CanonicalID())
	})

	t.Run("empty origin is rejected", func(t *testing.T) {
		t.Parallel()
		_, err := factory(context.Background(), "")
		require.ErrorContains(t, err, "no registry")
	})
}
