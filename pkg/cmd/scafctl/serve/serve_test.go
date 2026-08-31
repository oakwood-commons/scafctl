// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package serve

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
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

// fakeBuildDeps returns a buildPreloadDepsFunc that always yields the given deps.
func fakeBuildDeps(deps []solution.PluginDependency) buildPreloadDepsFunc {
	return func(_ *official.Registry, _ *preLoadOptions, _ *logr.Logger) []solution.PluginDependency {
		return deps
	}
}

func TestPreloadVersionedOfficialProviders_EmptyDepsReturnsNil(t *testing.T) {
	fetchCalled := false
	fetch := func(_ context.Context, _ []solution.PluginDependency, _ []bundler.LockPlugin) ([]plugin.FetchResult, error) {
		fetchCalled = true
		return nil, nil
	}

	clients, err := preloadVersionedOfficialProviders(
		context.Background(),
		provider.NewCompositeRegistry(),
		official.NewRegistry(),
		fetch,
		fakeBuildDeps(nil),
	)

	require.NoError(t, err)
	assert.Nil(t, clients)
	assert.False(t, fetchCalled, "fetchPlugins must not be called when there are no deps")
}

func TestPreloadVersionedOfficialProviders_SetsCatalogOnDeps(t *testing.T) {
	var captured []solution.PluginDependency
	// Return an error to short-circuit before registration (which needs real binaries).
	fetch := func(_ context.Context, plugins []solution.PluginDependency, _ []bundler.LockPlugin) ([]plugin.FetchResult, error) {
		captured = plugins
		return nil, errors.New("stop")
	}
	deps := []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "1.0.0"},
		{Name: "git", Kind: solution.PluginKindProvider, Version: "2.0.0"},
	}

	_, err := preloadVersionedOfficialProviders(
		context.Background(),
		provider.NewCompositeRegistry(),
		official.NewRegistry(),
		fetch,
		fakeBuildDeps(deps),
	)

	require.Error(t, err)
	require.Len(t, captured, 2)
	for _, d := range captured {
		assert.Equal(t, official.CatalogName, d.Catalog)
	}
}

func TestPreloadVersionedOfficialProviders_NilFetchReturnsNil(t *testing.T) {
	deps := []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}

	clients, err := preloadVersionedOfficialProviders(
		context.Background(),
		provider.NewCompositeRegistry(),
		official.NewRegistry(),
		nil,
		fakeBuildDeps(deps),
	)

	require.NoError(t, err)
	assert.Nil(t, clients)
}

func TestPreloadVersionedOfficialProviders_FetchErrorWrapped(t *testing.T) {
	sentinel := errors.New("boom")
	fetch := func(_ context.Context, _ []solution.PluginDependency, _ []bundler.LockPlugin) ([]plugin.FetchResult, error) {
		return nil, sentinel
	}
	deps := []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}

	clients, err := preloadVersionedOfficialProviders(
		context.Background(),
		provider.NewCompositeRegistry(),
		official.NewRegistry(),
		fetch,
		fakeBuildDeps(deps),
	)

	require.Error(t, err)
	assert.Nil(t, clients)
	assert.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), "fetching official providers")
}

func TestPreloadVersionedOfficialProviders_ReleasesPinsOnFetchError(t *testing.T) {
	released := 0
	results := []plugin.FetchResult{
		{Name: "exec", Release: func() { released++ }},
		{Name: "git", Release: func() { released++ }},
		{Name: "nopin"}, // nil Release must be skipped without panicking
	}
	fetch := func(_ context.Context, _ []solution.PluginDependency, _ []bundler.LockPlugin) ([]plugin.FetchResult, error) {
		return results, errors.New("boom")
	}
	deps := []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}

	_, err := preloadVersionedOfficialProviders(
		context.Background(),
		provider.NewCompositeRegistry(),
		official.NewRegistry(),
		fetch,
		fakeBuildDeps(deps),
	)

	require.Error(t, err)
	assert.Equal(t, 2, released, "Release must run for each pinned result even on fetch error")
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

	pool := buildPluginPool(ctx, cfg, nil, reg, &lgr, nil, nil)
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

	pool := buildPluginPool(ctx, cfg, nil, reg, &lgr, nil, nil)
	defer pool.Shutdown()

	// API server pools sanitize env by default
	assert.True(t, pool.SanitizeEnv())
}

func TestBuildPluginPool_HostStaticMetadata(t *testing.T) {
	// The API server pool should carry a host-static base config reporting the
	// "api" entrypoint so pooled plugins receive host runtime metadata.
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

	pool := buildPluginPool(ctx, cfg, nil, reg, &lgr, nil, nil)
	defer pool.Shutdown()

	base := pool.BaseProviderConfig()
	raw, ok := base.Settings["metadata"]
	require.True(t, ok, "base config should carry host metadata settings")

	var meta map[string]any
	require.NoError(t, json.Unmarshal(raw, &meta))
	assert.Equal(t, prepare.EntrypointAPI, meta["entrypoint"])
	assert.Contains(t, meta, "buildVersion")
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

	pool := buildPluginPool(ctx, cfg, nil, reg, &lgr, nil, []string{"foo", "bar"})
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

	pool := buildPluginPool(ctx, cfg, nil, reg, &lgr, nil, nil)
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

	pool := buildPluginPool(ctx, cfg, nil, reg, &lgr, nil, nil)
	defer pool.Shutdown()

	assert.Equal(t, 0, pool.ClientOptsLen(), "zero GRPCMaxMessageSize should not add a client opt")
}

func TestBuildVersionedPluginPool_AuthWiring(t *testing.T) {
	// When an auth registry is in context, buildVersionedPluginPool should wire
	// auth client options into the version pool.
	authReg := auth.NewRegistry()
	ctx := auth.WithRegistry(context.Background(), authReg)
	reg := provider.NewCompositeRegistry()
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			Plugins: config.APIPluginConfig{
				AllowExternal: true,
			},
		},
	}

	pool := buildVersionedPluginPool(ctx, cfg, nil, reg, &lgr, nil, nil)
	defer pool.Shutdown()

	// Pool should have been created (non-nil) with auth options forwarded.
	assert.NotNil(t, pool)
}

func TestBuildVersionedPluginPool_SanitizesEnv(t *testing.T) {
	// API server pools should sanitize env by default (the default is true).
	ctx := context.Background()
	reg := provider.NewCompositeRegistry()
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			Plugins: config.APIPluginConfig{
				AllowExternal: false,
			},
		},
	}

	pool := buildVersionedPluginPool(ctx, cfg, nil, reg, &lgr, nil, nil)
	defer pool.Shutdown()

	// API server pools sanitize env by default
	assert.True(t, pool.SanitizeEnv())
}

func TestBuildVersionedPluginPool_HostStaticMetadata(t *testing.T) {
	// The API server pool should carry a host-static base config reporting the
	// "api" entrypoint so pooled plugins receive host runtime metadata.
	ctx := context.Background()
	reg := provider.NewCompositeRegistry()
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			Plugins: config.APIPluginConfig{
				AllowExternal: false,
			},
		},
	}

	pool := buildVersionedPluginPool(ctx, cfg, nil, reg, &lgr, nil, nil)
	defer pool.Shutdown()

	base := pool.BaseProviderConfig()
	raw, ok := base.Settings["metadata"]
	require.True(t, ok, "base config should carry host metadata settings")

	var meta map[string]any
	require.NoError(t, json.Unmarshal(raw, &meta))
	assert.Equal(t, prepare.EntrypointAPI, meta["entrypoint"])
	assert.Contains(t, meta, "buildVersion")
}

func TestBuildVersionedPluginPool_AllowedPlugins(t *testing.T) {
	ctx := context.Background()
	reg := provider.NewCompositeRegistry()
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			Plugins: config.APIPluginConfig{
				AllowExternal:  true,
				AllowedPlugins: []string{"foo", "bar"},
			},
		},
	}

	pool := buildVersionedPluginPool(ctx, cfg, nil, reg, &lgr, nil, map[string]catalog.PluginPolicy{
		"official": {Plugins: []string{"foo", "bar"}},
	})
	defer pool.Shutdown()

	// Verify that a non-allowed plugin is rejected. The dependency is
	// catalog-qualified so it clears the unresolved-catalog guard and reaches
	// the per-catalog allowlist gate.
	_, err := pool.EnsureAndAcquire(ctx, []solution.PluginDependency{
		{Name: "not-allowed", Catalog: "official", Kind: solution.PluginKindProvider},
	})
	assert.ErrorIs(t, err, plugin.ErrPluginNotAllowed)
}

func TestBuildVersionedPluginPool_GRPCMaxMessageSize(t *testing.T) {
	// When cfg.Plugins.GRPCMaxMessageSize is non-zero, buildVersionedPluginPool should
	// wire it as a client option so plugin processes use the configured limit.
	ctx := context.Background()
	reg := provider.NewCompositeRegistry()
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			Plugins: config.APIPluginConfig{AllowExternal: true},
		},
		Plugins: config.PluginsConfig{
			GRPCMaxMessageSize: 128 * 1024 * 1024, // 128 MB
		},
	}

	pool := buildVersionedPluginPool(ctx, cfg, nil, reg, &lgr, nil, nil)
	defer pool.Shutdown()

	// Exactly one client option should have been added (the gRPC max message size).
	assert.Equal(t, 1, pool.ClientOptsLen(), "pool should have exactly one client opt for gRPC max message size")
}

func TestBuildVersionedPluginPool_GRPCMaxMessageSizeZeroIsNoop(t *testing.T) {
	// When cfg.Plugins.GRPCMaxMessageSize is zero (default), no extra client
	// option should be added -- connectPlugin will use the default internally.
	ctx := context.Background()
	reg := provider.NewCompositeRegistry()
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			Plugins: config.APIPluginConfig{AllowExternal: true},
		},
	}

	pool := buildVersionedPluginPool(ctx, cfg, nil, reg, &lgr, nil, nil)
	defer pool.Shutdown()

	assert.Equal(t, 0, pool.ClientOptsLen(), "zero GRPCMaxMessageSize should not add a client opt")
}

func TestBuildPreloadDeps(t *testing.T) {
	tests := []struct {
		name         string
		providers    []official.Provider
		registered   []string
		allowed      []string // nil means no allowlist (allow all)
		wantDepNames []string
	}{
		{
			name: "allowlist filters to only permitted providers",
			providers: []official.Provider{
				{Name: "exec", CatalogRef: "exec", DefaultVersion: "1.0.0"},
				{Name: "git", CatalogRef: "git", DefaultVersion: "1.0.0"},
				{Name: "directory", CatalogRef: "directory", DefaultVersion: "1.0.0"},
			},
			allowed:      []string{"exec", "directory"},
			wantDepNames: []string{"exec", "directory"},
		},
		{
			name: "nil allowlist permits all providers",
			providers: []official.Provider{
				{Name: "exec", CatalogRef: "exec", DefaultVersion: "1.0.0"},
				{Name: "git", CatalogRef: "git", DefaultVersion: "1.0.0"},
			},
			allowed:      nil,
			wantDepNames: []string{"exec", "git"},
		},
		{
			name: "allowlist with no matches produces empty deps",
			providers: []official.Provider{
				{Name: "exec", CatalogRef: "exec", DefaultVersion: "1.0.0"},
			},
			allowed:      []string{"nonexistent"},
			wantDepNames: nil,
		},
		{
			name: "already-registered providers are skipped",
			providers: []official.Provider{
				{Name: "http", CatalogRef: "http", DefaultVersion: "1.0.0"},
				{Name: "git", CatalogRef: "git", DefaultVersion: "1.0.0"},
			},
			registered:   []string{"http"},
			wantDepNames: []string{"git"},
		},
		{
			name: "combined allowlist and already registered",
			providers: []official.Provider{
				{Name: "http", CatalogRef: "http", DefaultVersion: "1.0.0"},
				{Name: "git", CatalogRef: "git", DefaultVersion: "1.0.0"},
				{Name: "directory", CatalogRef: "directory", DefaultVersion: "1.0.0"},
			},
			registered:   []string{"http"},
			allowed:      []string{"http", "git"},
			wantDepNames: []string{"git"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			officialReg := official.NewRegistryFrom(tt.providers)

			var reg *provider.Registry
			if len(tt.registered) > 0 {
				reg, _ = builtin.DefaultRegistry(context.Background())
			} else {
				reg = provider.NewRegistry()
			}

			var options preLoadOptions
			if tt.allowed != nil {
				options.allowedPlugins = make(map[string]bool, len(tt.allowed))
				for _, a := range tt.allowed {
					options.allowedPlugins[a] = true
				}
			}

			lgr := logr.Discard()
			deps := buildPreloadDeps(officialReg, reg, &options, &lgr)

			var gotNames []string
			for _, d := range deps {
				gotNames = append(gotNames, d.Name)
			}

			assert.ElementsMatch(t, tt.wantDepNames, gotNames)
		})
	}
}

func TestConfigurePreloadOptions(t *testing.T) {
	tests := []struct {
		name           string
		perCatalog     map[string]catalog.PluginPolicy
		catalogName    string
		wantOptsLen    int
		wantNilAllowed bool
		wantAllowed    map[string]bool
	}{
		{
			name:           "nil perCatalog applies no filter",
			perCatalog:     nil,
			catalogName:    "official",
			wantOptsLen:    0,
			wantNilAllowed: true,
		},
		{
			name:           "wildcard policy applies no filter",
			perCatalog:     map[string]catalog.PluginPolicy{"official": {AllowAll: true}},
			catalogName:    "official",
			wantOptsLen:    0,
			wantNilAllowed: true,
		},
		{
			name:           "catalog absent from map denies all",
			perCatalog:     map[string]catalog.PluginPolicy{"other": {Plugins: []string{"foo"}}},
			catalogName:    "official",
			wantOptsLen:    1,
			wantNilAllowed: false,
			wantAllowed:    map[string]bool{},
		},
		{
			name:           "explicit list filters to named plugins",
			perCatalog:     map[string]catalog.PluginPolicy{"official": {Plugins: []string{"exec", "git"}}},
			catalogName:    "official",
			wantOptsLen:    1,
			wantNilAllowed: false,
			wantAllowed:    map[string]bool{"exec": true, "git": true},
		},
		{
			name:           "empty map with catalog absent denies all",
			perCatalog:     map[string]catalog.PluginPolicy{},
			catalogName:    "official",
			wantOptsLen:    1,
			wantNilAllowed: false,
			wantAllowed:    map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := configurePreloadOptions(tt.perCatalog, nil, tt.catalogName)

			assert.Len(t, opts, tt.wantOptsLen)

			options := &preLoadOptions{}
			for _, opt := range opts {
				opt(options)
			}

			if tt.wantNilAllowed {
				assert.Nil(t, options.allowedPlugins)
			} else {
				require.NotNil(t, options.allowedPlugins)
				assert.Equal(t, tt.wantAllowed, options.allowedPlugins)
			}
		})
	}
}

func TestPluginCacheMaxSize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{
			name:  "empty uses default",
			input: "",
			want:  512 * 1024 * 1024,
		},
		{
			name:  "explicit MB",
			input: "256MB",
			want:  256 * 1024 * 1024,
		},
		{
			name:  "explicit GB",
			input: "2GB",
			want:  2 * 1024 * 1024 * 1024,
		},
		{
			name:  "lowercase",
			input: "128mb",
			want:  128 * 1024 * 1024,
		},
		{
			name:    "invalid string",
			input:   "not-a-size",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.APIServer.Plugins.DiskCacheMaxSize = tt.input

			got, err := pluginCacheMaxSize(cfg)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// mockServerModeHandler embeds MockHandler and implements auth.ServerMode.
type mockServerModeHandler struct {
	*auth.MockHandler
	ActivateErr   error
	ActivateCalls []json.RawMessage
}

func newMockServerModeHandler(name string) *mockServerModeHandler {
	return &mockServerModeHandler{
		MockHandler: auth.NewMockHandler(name),
	}
}

func (m *mockServerModeHandler) ActivateServerMode(_ context.Context, settings json.RawMessage) error {
	m.ActivateCalls = append(m.ActivateCalls, settings)
	return m.ActivateErr
}

var _ auth.ServerMode = (*mockServerModeHandler)(nil)

func TestActivateAuthPluginServerMode_EmptyAuthPlugins(t *testing.T) {
	lgr := logr.Discard()
	registry := auth.NewRegistry()
	cfg := &config.Config{}

	err := activateAuthPluginServerMode(context.Background(), registry, cfg, &lgr)

	require.NoError(t, err)
}

func TestActivateAuthPluginServerMode_NilRegistry(t *testing.T) {
	lgr := logr.Discard()
	cfg := &config.Config{}
	cfg.APIServer.Auth.Handlers = map[string]any{
		"entra": map[string]any{"clientId": "abc"},
	}

	err := activateAuthPluginServerMode(context.Background(), nil, cfg, &lgr)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth registry not available for server-mode activation")
}

func TestActivateAuthPluginServerMode_HandlerNotFound(t *testing.T) {
	lgr := logr.Discard()
	registry := auth.NewRegistry()
	cfg := &config.Config{}
	cfg.APIServer.Auth.Handlers = map[string]any{
		"missing": map[string]any{"clientId": "abc"},
	}

	err := activateAuthPluginServerMode(context.Background(), registry, cfg, &lgr)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `auth handler "missing" not found in registry`)
}

func TestActivateAuthPluginServerMode_HandlerDoesNotSupportServerMode(t *testing.T) {
	lgr := logr.Discard()
	registry := auth.NewRegistry()
	// MockHandler does NOT implement ServerMode.
	handler := auth.NewMockHandler("basic")
	require.NoError(t, registry.Register(handler))

	cfg := &config.Config{}
	cfg.APIServer.Auth.Handlers = map[string]any{
		"basic": map[string]any{"clientId": "abc"},
	}

	err := activateAuthPluginServerMode(context.Background(), registry, cfg, &lgr)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `auth handler "basic" does not support server mode`)
}

func TestActivateAuthPluginServerMode_NilSettings(t *testing.T) {
	lgr := logr.Discard()
	registry := auth.NewRegistry()
	handler := newMockServerModeHandler("entra")
	require.NoError(t, registry.Register(handler))

	cfg := &config.Config{}
	// Key exists but value is nil.
	cfg.APIServer.Auth.Handlers = map[string]any{
		"entra": nil,
	}

	err := activateAuthPluginServerMode(context.Background(), registry, cfg, &lgr)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `auth handler "entra" has no server mode settings configured`)
}

func TestActivateAuthPluginServerMode_ActivateError(t *testing.T) {
	lgr := logr.Discard()
	registry := auth.NewRegistry()
	handler := newMockServerModeHandler("entra")
	handler.ActivateErr = errors.New("plugin crashed")
	require.NoError(t, registry.Register(handler))

	cfg := &config.Config{}
	cfg.APIServer.Auth.Handlers = map[string]any{
		"entra": map[string]any{"clientId": "abc", "tenantId": "xyz"},
	}

	err := activateAuthPluginServerMode(context.Background(), registry, cfg, &lgr)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `activating server mode on auth handler "entra"`)
	assert.Contains(t, err.Error(), "plugin crashed")
}

func TestActivateAuthPluginServerMode_Success(t *testing.T) {
	lgr := logr.Discard()
	registry := auth.NewRegistry()
	handler := newMockServerModeHandler("entra")
	require.NoError(t, registry.Register(handler))

	cfg := &config.Config{}
	cfg.APIServer.Auth.Handlers = map[string]any{
		"entra": map[string]any{"clientId": "abc", "tenantId": "xyz"},
	}

	err := activateAuthPluginServerMode(context.Background(), registry, cfg, &lgr)

	require.NoError(t, err)
	require.Len(t, handler.ActivateCalls, 1)
	// Verify settings were passed as marshaled JSON.
	var got map[string]any
	require.NoError(t, json.Unmarshal(handler.ActivateCalls[0], &got))
	assert.Equal(t, "abc", got["clientId"])
	assert.Equal(t, "xyz", got["tenantId"])
}

func TestActivateAuthPluginServerMode_MultipleHandlers(t *testing.T) {
	lgr := logr.Discard()
	registry := auth.NewRegistry()
	h1 := newMockServerModeHandler("entra")
	h2 := newMockServerModeHandler("github")
	require.NoError(t, registry.Register(h1))
	require.NoError(t, registry.Register(h2))

	cfg := &config.Config{}
	cfg.APIServer.Auth.Handlers = map[string]any{
		"entra":  map[string]any{"clientId": "abc"},
		"github": map[string]any{"appId": "123"},
	}

	err := activateAuthPluginServerMode(context.Background(), registry, cfg, &lgr)

	require.NoError(t, err)
	assert.Len(t, h1.ActivateCalls, 1)
	assert.Len(t, h2.ActivateCalls, 1)
}

func TestAuthPluginSettings_NotFound(t *testing.T) {
	cfg := &config.Config{}
	cfg.APIServer.Auth.Handlers = map[string]any{}

	data, err := authPluginSettings(cfg, "missing")

	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestAuthPluginSettings_NilValue(t *testing.T) {
	cfg := &config.Config{}
	cfg.APIServer.Auth.Handlers = map[string]any{"entra": nil}

	data, err := authPluginSettings(cfg, "entra")

	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestAuthPluginSettings_Success(t *testing.T) {
	cfg := &config.Config{}
	cfg.APIServer.Auth.Handlers = map[string]any{
		"entra": map[string]any{"clientId": "abc"},
	}

	data, err := authPluginSettings(cfg, "entra")

	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "abc", got["clientId"])
}

func TestAuthPluginSettings_UnmarshalableValue(t *testing.T) {
	cfg := &config.Config{}
	// A channel cannot be marshaled to JSON.
	cfg.APIServer.Auth.Handlers = map[string]any{
		"entra": make(chan int),
	}

	_, err := authPluginSettings(cfg, "entra")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal auth plugin settings")
}

func TestActivateAuthPluginServerMode_MarshalError(t *testing.T) {
	lgr := logr.Discard()
	registry := auth.NewRegistry()
	handler := newMockServerModeHandler("entra")
	require.NoError(t, registry.Register(handler))

	cfg := &config.Config{}
	// A channel cannot be marshaled — triggers the authPluginSettings error path.
	cfg.APIServer.Auth.Handlers = map[string]any{
		"entra": make(chan int),
	}

	err := activateAuthPluginServerMode(context.Background(), registry, cfg, &lgr)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `auth handler "entra" settings`)
}

func TestDisableNonServerModeHandlers(t *testing.T) {
	lgr := logr.Discard()
	registry := auth.NewRegistry()
	activated := newMockServerModeHandler("entra")
	inactive := auth.NewMockHandler("github")
	require.NoError(t, registry.Register(activated))
	require.NoError(t, registry.Register(inactive))

	cfg := &config.Config{}
	cfg.APIServer.Auth.Handlers = map[string]any{
		"entra": map[string]any{"clientId": "abc"},
	}

	disableNonServerModeHandlers(registry, cfg, &lgr)

	// "entra" should still work (not disabled).
	h, err := registry.Get("entra")
	require.NoError(t, err)
	assert.False(t, auth.IsDisabled(h))

	// "github" should be disabled.
	h2, err := registry.Get("github")
	require.NoError(t, err)
	assert.True(t, auth.IsDisabled(h2))

	_, tokenErr := h2.GetToken(context.Background(), auth.TokenOptions{})
	require.Error(t, tokenErr)
	assert.ErrorIs(t, tokenErr, auth.ErrHandlerDisabled)
	assert.Contains(t, tokenErr.Error(), "not configured for server mode")
}

func TestDisableNonServerModeHandlers_NoAuthHandlers(t *testing.T) {
	lgr := logr.Discard()
	registry := auth.NewRegistry()
	handler := auth.NewMockHandler("github")
	require.NoError(t, registry.Register(handler))

	cfg := &config.Config{}
	// No auth handlers configured — all handlers get disabled.
	cfg.APIServer.Auth.Handlers = nil

	disableNonServerModeHandlers(registry, cfg, &lgr)

	h, err := registry.Get("github")
	require.NoError(t, err)
	assert.True(t, auth.IsDisabled(h))
}
