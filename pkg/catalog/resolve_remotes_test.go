// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
)

func TestRemoteCatalogsFromContext_NilConfig(t *testing.T) {
	t.Parallel()

	remotes := RemoteCatalogsFromContext(context.Background(), logr.Discard())
	assert.Nil(t, remotes)
}

func TestRemoteCatalogsFromContext_NoOCICatalogs(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: "local", Type: config.CatalogTypeFilesystem, Path: "/tmp"},
		},
	}
	ctx := config.WithConfig(context.Background(), cfg)

	remotes := RemoteCatalogsFromContext(ctx, logr.Discard())
	assert.Nil(t, remotes)
}

func TestRemoteCatalogsFromContext_SkipsEmptyURL(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: "empty", Type: config.CatalogTypeOCI, URL: ""},
		},
	}
	ctx := config.WithConfig(context.Background(), cfg)

	remotes := RemoteCatalogsFromContext(ctx, logr.Discard())
	assert.Nil(t, remotes)
}

func TestRemoteCatalogsFromContext_CreatesRemotes(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: "reg-1", Type: config.CatalogTypeOCI, URL: "oci://ghcr.io/myorg/catalog"},
			{Name: "reg-2", Type: config.CatalogTypeOCI, URL: "oci://us-docker.pkg.dev/proj/repo"},
		},
	}
	ctx := config.WithConfig(context.Background(), cfg)

	remotes := RemoteCatalogsFromContext(ctx, logr.Discard())
	assert.Len(t, remotes, 2)
	assert.Equal(t, "reg-1", remotes[0].Name())
	assert.Equal(t, "reg-2", remotes[1].Name())
}
