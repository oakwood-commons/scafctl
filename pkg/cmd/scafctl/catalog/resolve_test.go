// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

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

			url, err := resolveCatalogURL(ctx, tt.catalogFlag)

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
			assert.Equal(t, tt.want, looksLikeCatalogURL(tt.input))
		})
	}
}
