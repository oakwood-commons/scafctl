// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPluginFetcher_WithDefaultContext(t *testing.T) {
	// With a bare context (no config), BuildCatalogChain should still
	// succeed using the local catalog as a fallback.
	ctx := context.Background()
	fetcher, err := buildPluginFetcher(ctx)
	require.NoError(t, err)
	assert.NotNil(t, fetcher)
}

func TestBuildPluginFetcher_WithConfig(t *testing.T) {
	cfg := &config.Config{}
	ctx := config.WithConfig(context.Background(), cfg)

	fetcher, err := buildPluginFetcher(ctx)
	require.NoError(t, err)
	assert.NotNil(t, fetcher)
}

func TestAutoResolveProviderByName_NilRegistry(t *testing.T) {
	// With no official registry in context, returns error.
	ctx := context.Background()
	reg := provider.NewRegistry()

	_, err := autoResolveProviderByName(ctx, "exec", reg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "official registry not available")
}

func TestAutoResolveProviderByName_UnknownProvider(t *testing.T) {
	officialReg := official.NewRegistry()
	ctx := official.WithRegistry(context.Background(), officialReg)
	reg := provider.NewRegistry()

	_, err := autoResolveProviderByName(ctx, "nonexistent-provider", reg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not an official provider")
}

func TestAutoResolveProviderByName_FetcherFails(t *testing.T) {
	// When the provider is known but no catalog or cache has it available,
	// the function should return a wrapped error.
	officialReg := official.NewRegistryFrom([]official.Provider{
		{Name: "nonexistent-fake", CatalogRef: "nonexistent-fake", DefaultVersion: "latest"},
	})
	ctx := official.WithRegistry(context.Background(), officialReg)
	ctx = config.WithConfig(ctx, &config.Config{})
	reg := provider.NewRegistry()

	// "nonexistent-fake" is in our custom registry but no catalog or cache has it.
	_, err := autoResolveProviderByName(ctx, "nonexistent-fake", reg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fetching provider")
}

func TestLoadLockPlugins_MissingFile(t *testing.T) {
	result := loadLockPlugins("/nonexistent/path/solution.yaml")
	assert.Nil(t, result)
}
