// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
)

func TestResolveAuthScope_FromNamedCatalog(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name:      "my-registry",
				Type:      appconfig.CatalogTypeOCI,
				URL:       "oci://ghcr.io/myorg",
				AuthScope: "repo:read",
			},
		},
	}

	ctx := appconfig.WithConfig(context.Background(), cfg)
	scope := resolveAuthScope(ctx, "my-registry")
	assert.Equal(t, "repo:read", scope)
}

func TestResolveAuthScope_EmptyWhenNoCatalog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scope := resolveAuthScope(ctx, "nonexistent")
	assert.Equal(t, "", scope)
}

func TestResolveAuthScope_EmptyWhenNoConfig(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scope := resolveAuthScope(ctx, "")
	assert.Equal(t, "", scope)
}

func TestResolveAuthScope_EmptyWhenCatalogHasNoScope(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name: "my-registry",
				Type: appconfig.CatalogTypeOCI,
				URL:  "oci://ghcr.io/myorg",
			},
		},
	}

	ctx := appconfig.WithConfig(context.Background(), cfg)
	scope := resolveAuthScope(ctx, "my-registry")
	assert.Equal(t, "", scope)
}

func TestResolveAuthHandler_NilWhenNoConfig(t *testing.T) {
	t.Parallel()

	w := writer.New(
		testIOStreams(),
		settings.NewCliParams(),
	)
	ctx := writer.WithWriter(context.Background(), w)

	handler := resolveAuthHandler(ctx, "ghcr.io", "")
	assert.Nil(t, handler, "should return nil when no config in context")
}

func TestResolveAuthHandler_NilWhenNoMatchingCatalog(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name: "my-registry",
				Type: appconfig.CatalogTypeOCI,
				URL:  "oci://ghcr.io/myorg",
			},
		},
	}

	w := writer.New(
		testIOStreams(),
		settings.NewCliParams(),
	)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = appconfig.WithConfig(ctx, cfg)

	handler := resolveAuthHandler(ctx, "custom.io", "nonexistent")
	assert.Nil(t, handler, "should return nil when catalog not found and no inference match")
}

func TestResolveAuthHandler_FromCatalogConfig(t *testing.T) {
	t.Parallel()

	cfg := &appconfig.Config{
		Catalogs: []appconfig.CatalogConfig{
			{
				Name:         "gh-registry",
				Type:         appconfig.CatalogTypeOCI,
				URL:          "oci://ghcr.io/myorg",
				AuthProvider: "github",
			},
		},
	}

	w := writer.New(
		testIOStreams(),
		settings.NewCliParams(),
	)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = appconfig.WithConfig(ctx, cfg)

	// github handler may or may not be loadable in test environment,
	// but the function should at least try (not panic)
	_ = resolveAuthHandler(ctx, "ghcr.io", "gh-registry")
}

func testIOStreams() *terminal.IOStreams {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	return ioStreams
}
