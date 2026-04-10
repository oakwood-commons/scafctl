// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCatalogURL(t *testing.T) {
	tests := []struct {
		name        string
		catalogFlag string
		config      *config.Config
		wantURL     string
		wantErr     string
	}{
		{
			name:        "direct URL with dots",
			catalogFlag: "ghcr.io/myorg",
			wantURL:     "ghcr.io/myorg",
		},
		{
			name:        "direct URL with port",
			catalogFlag: "localhost:5000",
			wantURL:     "localhost:5000",
		},
		{
			name:        "direct URL with oci scheme",
			catalogFlag: "oci://ghcr.io/myorg/scafctl",
			wantURL:     "oci://ghcr.io/myorg/scafctl",
		},
		{
			name:        "catalog name from config",
			catalogFlag: "myregistry",
			config: &config.Config{
				Catalogs: []config.CatalogConfig{
					{Name: "myregistry", Type: "oci", URL: "ghcr.io/myorg"},
				},
			},
			wantURL: "ghcr.io/myorg",
		},
		{
			name:        "catalog name not found",
			catalogFlag: "nonexistent",
			config:      &config.Config{},
			wantErr:     `catalog "nonexistent" not found`,
		},
		{
			name:        "catalog name with no URL",
			catalogFlag: "empty",
			config: &config.Config{
				Catalogs: []config.CatalogConfig{
					{Name: "empty", Type: "oci"},
				},
			},
			wantErr: `catalog "empty" has no URL`,
		},
		{
			name:        "empty flag uses default catalog",
			catalogFlag: "",
			config: &config.Config{
				Settings: config.Settings{DefaultCatalog: "default"},
				Catalogs: []config.CatalogConfig{
					{Name: "default", Type: "oci", URL: "ghcr.io/myorg/default"},
				},
			},
			wantURL: "ghcr.io/myorg/default",
		},
		{
			name:        "empty flag with filesystem default catalog",
			catalogFlag: "",
			config: &config.Config{
				Settings: config.Settings{DefaultCatalog: "local"},
				Catalogs: []config.CatalogConfig{
					{Name: "local", Type: "filesystem", Path: "/data/catalog"},
				},
			},
			wantURL: "/data/catalog",
		},
		{
			name:        "empty flag with no default configured",
			catalogFlag: "",
			config:      &config.Config{},
			wantErr:     "no --catalog specified and no default catalog configured",
		},
		{
			name:        "empty flag with no config in context",
			catalogFlag: "",
			config:      nil,
			wantErr:     "no --catalog specified and no configuration loaded",
		},
		{
			name:        "catalog name with no config in context",
			catalogFlag: "myregistry",
			config:      nil,
			wantErr:     "no configuration loaded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.config != nil {
				ctx = config.WithConfig(ctx, tt.config)
			}

			url, err := ResolveCatalogURL(ctx, tt.catalogFlag)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantURL, url)
			}
		})
	}
}

func TestLooksLikeCatalogURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"ghcr.io/myorg", true},
		{"docker.io/library", true},
		{"localhost:5000", true},
		{"registry.example.com/path", true},
		{"oci://ghcr.io/myorg", true},
		{"https://ghcr.io/myorg", true},
		{"myregistry", false},
		{"local", false},
		{"default", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, LooksLikeCatalogURL(tt.input))
		})
	}
}

func TestParseCatalogURL(t *testing.T) {
	tests := []struct {
		input          string
		wantRegistry   string
		wantRepository string
	}{
		{"ghcr.io/myorg/scafctl", "ghcr.io", "myorg/scafctl"},
		{"ghcr.io/myorg", "ghcr.io", "myorg"},
		{"localhost:5000", "localhost:5000", ""},
		{"oci://ghcr.io/myorg/scafctl", "ghcr.io", "myorg/scafctl"},
		{"https://ghcr.io/myorg", "ghcr.io", "myorg"},
		{"http://localhost:5000/repo", "localhost:5000", "repo"},
		{"ghcr.io/myorg/scafctl/", "ghcr.io", "myorg/scafctl"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			registry, repository := ParseCatalogURL(tt.input)
			assert.Equal(t, tt.wantRegistry, registry)
			assert.Equal(t, tt.wantRepository, repository)
		})
	}
}

func BenchmarkParseCatalogURL(b *testing.B) {
	for b.Loop() {
		ParseCatalogURL("oci://ghcr.io/myorg/scafctl")
	}
}

func BenchmarkLooksLikeCatalogURL(b *testing.B) {
	for b.Loop() {
		LooksLikeCatalogURL("ghcr.io/myorg/scafctl")
	}
}

func BenchmarkResolveCatalogURL(b *testing.B) {
	ctx := config.WithConfig(context.Background(), &config.Config{
		Settings: config.Settings{DefaultCatalog: "default"},
		Catalogs: []config.CatalogConfig{
			{Name: "default", Type: "oci", URL: "ghcr.io/myorg/default"},
		},
	})

	for b.Loop() {
		_, _ = ResolveCatalogURL(ctx, "")
	}
}

func TestInferKindFromLocalCatalog_Found(t *testing.T) {
	tmpDir := t.TempDir()
	cat, err := NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	ctx := context.Background()
	ref, err := ParseReference(ArtifactKindSolution, "my-sol@1.0.0")
	require.NoError(t, err)
	_, err = cat.Store(ctx, ref, []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-sol
  version: 1.0.0
`), nil, nil, false)
	require.NoError(t, err)

	kind, err := InferKindFromLocalCatalog(ctx, cat, "my-sol", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, ArtifactKindSolution, kind)
}

func TestInferKindFromLocalCatalog_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	cat, err := NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	_, err = InferKindFromLocalCatalog(context.Background(), cat, "nonexistent", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestLooksLikeRemoteReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ref      string
		expected bool
	}{
		{
			name:     "simple name is not remote",
			ref:      "my-solution@1.0.0",
			expected: false,
		},
		{
			name:     "localhost prefix is remote",
			ref:      "localhost:5000/solutions/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "localhost without port is remote",
			ref:      "localhost/solutions/my-solution",
			expected: true,
		},
		{
			name:     "host with dot is remote",
			ref:      "ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "host with port is remote",
			ref:      "registry:5000/solutions/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "oci scheme prefix is remote",
			ref:      "oci://ghcr.io/myorg/solutions/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "docker registry is remote",
			ref:      "docker.io/myorg/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "no slash is not remote",
			ref:      "my-solution",
			expected: false,
		},
		{
			name:     "path without dot host is not remote",
			ref:      "myorg/my-solution",
			expected: false,
		},
		{
			name:     "empty string is not remote",
			ref:      "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := LooksLikeRemoteReference(tt.ref)
			assert.Equal(t, tt.expected, result, "LooksLikeRemoteReference(%q)", tt.ref)
		})
	}
}

func BenchmarkLooksLikeRemoteReference(b *testing.B) {
	refs := []string{
		"my-solution@1.0.0",
		"ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0",
		"localhost:5000/solutions/my-solution",
	}
	b.ResetTimer()
	for b.Loop() {
		for _, ref := range refs {
			LooksLikeRemoteReference(ref)
		}
	}
}
