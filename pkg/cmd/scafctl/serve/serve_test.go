// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package serve

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandServe(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandServe(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "serve", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)
}

func TestCommandServe_HasOpenAPISubcommand(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandServe(cliParams, ioStreams, "scafctl")

	subCmds := cmd.Commands()
	require.Len(t, subCmds, 1, "should have 1 subcommand: openapi")
	assert.Equal(t, "openapi", subCmds[0].Name())
}

func TestCommandServe_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandServe(cliParams, ioStreams, "scafctl")

	flags := []string{"host", "port", "tls-cert", "tls-key", "enable-tls", "api-version"}
	for _, flagName := range flags {
		f := cmd.Flags().Lookup(flagName)
		assert.NotNilf(t, f, "expected flag %q to be registered", flagName)
	}
}

func TestCommandOpenAPI(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandOpenAPI(cliParams, ioStreams)

	require.NotNil(t, cmd)
	assert.Equal(t, "openapi", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.True(t, cmd.SilenceUsage)
}

func TestCommandOpenAPI_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandOpenAPI(cliParams, ioStreams)

	formatFlag := cmd.Flags().Lookup("format")
	require.NotNil(t, formatFlag)
	assert.Equal(t, "json", formatFlag.DefValue)

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
}

func BenchmarkCommandServe(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	for b.Loop() {
		CommandServe(cliParams, ioStreams, "scafctl")
	}
}

func TestPreloadOfficialProviders_SkipsRegistered(t *testing.T) {
	ctx := context.Background()
	reg, err := builtin.DefaultRegistry(ctx)
	require.NoError(t, err)

	// Create an official registry containing "http" which is already a builtin.
	// preloadOfficialProviders should skip it since it's already registered.
	officialReg := official.NewRegistryFrom([]official.Provider{
		{Name: "http", CatalogRef: "http", DefaultVersion: "latest"},
	})

	// No plugin fetcher needed -- all providers are already registered
	clients, preloadErr := preloadOfficialProviders(ctx, reg, officialReg, nil)
	assert.NoError(t, preloadErr)
	assert.Empty(t, clients)
}

func TestPreloadOfficialProviders_NilFetcher(t *testing.T) {
	ctx := context.Background()
	reg := provider.NewRegistry()
	officialReg := official.NewRegistry()

	// Without a fetcher, should return nil (not panic)
	clients, preloadErr := preloadOfficialProviders(ctx, reg, officialReg, nil)
	assert.Nil(t, clients)
	assert.Nil(t, preloadErr)
}

func TestBuildPluginPool_AuthWiring(t *testing.T) {
	// When an auth registry is in context, buildPluginPool should wire
	// auth client options into the pool.
	authReg := auth.NewRegistry()
	ctx := auth.WithRegistry(context.Background(), authReg)
	reg := provider.NewRegistry()
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			Plugins: config.APIPluginConfig{
				AllowExternal: true,
			},
		},
	}

	pool := buildPluginPool(ctx, cfg, nil, reg, &lgr, nil)
	defer pool.Shutdown()

	// Pool should have been created (non-nil) with auth options forwarded.
	// We can't inspect internal opts directly, but verify pool is functional.
	assert.NotNil(t, pool)
}

func TestBuildPluginPool_SanitizesEnv(t *testing.T) {
	// API server pools should sanitize env by default (the default is true).
	ctx := context.Background()
	reg := provider.NewRegistry()
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			Plugins: config.APIPluginConfig{
				AllowExternal: false,
			},
		},
	}

	pool := buildPluginPool(ctx, cfg, nil, reg, &lgr, nil)
	defer pool.Shutdown()

	// API server pools sanitize env by default
	assert.True(t, pool.SanitizeEnv())
}

func TestBuildPluginPool_AllowedPlugins(t *testing.T) {
	ctx := context.Background()
	reg := provider.NewRegistry()
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			Plugins: config.APIPluginConfig{
				AllowExternal:  true,
				AllowedPlugins: []string{"foo", "bar"},
			},
		},
	}

	pool := buildPluginPool(ctx, cfg, nil, reg, &lgr, nil)
	defer pool.Shutdown()

	// Verify that a non-allowed plugin is rejected
	_, err := pool.EnsureAndAcquire(ctx, []solution.PluginDependency{
		{Name: "not-allowed", Kind: solution.PluginKindProvider},
	})
	assert.ErrorIs(t, err, plugin.ErrPluginNotAllowed)
}

func TestBuildPluginPool_GRPCMaxMessageSize(t *testing.T) {
	// When cfg.Plugins.GRPCMaxMessageSize is non-zero, buildPluginPool should
	// wire it as a client option so plugin processes use the configured limit.
	ctx := context.Background()
	reg := provider.NewRegistry()
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			Plugins: config.APIPluginConfig{AllowExternal: true},
		},
		Plugins: config.PluginsConfig{
			GRPCMaxMessageSize: 128 * 1024 * 1024, // 128 MB
		},
	}

	pool := buildPluginPool(ctx, cfg, nil, reg, &lgr, nil)
	defer pool.Shutdown()

	// Exactly one client option should have been added (the gRPC max message size).
	assert.Equal(t, 1, pool.ClientOptsLen(), "pool should have exactly one client opt for gRPC max message size")
}

func TestBuildPluginPool_GRPCMaxMessageSizeZeroIsNoop(t *testing.T) {
	// When cfg.Plugins.GRPCMaxMessageSize is zero (default), no extra client
	// option should be added -- connectPlugin will use the default internally.
	ctx := context.Background()
	reg := provider.NewRegistry()
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			Plugins: config.APIPluginConfig{AllowExternal: true},
		},
	}

	pool := buildPluginPool(ctx, cfg, nil, reg, &lgr, nil)
	defer pool.Shutdown()

	assert.Equal(t, 0, pool.ClientOptsLen(), "zero GRPCMaxMessageSize should not add a client opt")
}
