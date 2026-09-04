// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── autoResolveOfficialVersioned tests ────────────────────────────────────────

func TestAutoResolveOfficialVersioned_NilOfficialRegistry(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	composite := provider.NewCompositeRegistryFromBase(reg)
	cfg := &prepareConfig{} // officialProviders is nil
	sol := &solution.Solution{}

	resolution, clients, err := autoResolveOfficialVersioned(t.Context(), sol, composite, nil, cfg)
	require.NoError(t, err)
	assert.Nil(t, resolution)
	assert.Nil(t, clients)
}

func TestAutoResolveOfficialVersioned_EmptyOfficialRegistry(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	composite := provider.NewCompositeRegistryFromBase(reg)
	officialReg := official.NewRegistryFrom(nil) // empty
	cfg := &prepareConfig{officialProviders: officialReg}
	sol := &solution.Solution{}

	resolution, clients, err := autoResolveOfficialVersioned(t.Context(), sol, composite, nil, cfg)
	require.NoError(t, err)
	assert.Nil(t, resolution)
	assert.Nil(t, clients)
}

func TestAutoResolveOfficialVersioned_AllBuiltin(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(&mockProvider{name: "env"}))
	composite := provider.NewCompositeRegistryFromBase(reg)
	officialReg := official.NewRegistry()
	cfg := &prepareConfig{officialProviders: officialReg}

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myenv": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	resolution, clients, err := autoResolveOfficialVersioned(t.Context(), sol, composite, nil, cfg)
	require.NoError(t, err)
	assert.Nil(t, resolution)
	assert.Nil(t, clients)
}

func TestAutoResolveOfficialVersioned_AlreadyInResolution(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	composite := provider.NewCompositeRegistryFromBase(reg)
	officialReg := official.NewRegistry()
	cfg := &prepareConfig{officialProviders: officialReg}

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myenv": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	// env is already in the existing resolution map (from fetchAndRegisterVersioned).
	existingResolution := map[string]solution.PluginDependency{
		"env": {Name: "env", Kind: solution.PluginKindProvider, Version: "1.0.0", Catalog: "my-catalog"},
	}

	resolution, clients, err := autoResolveOfficialVersioned(t.Context(), sol, composite, existingResolution, cfg)
	require.NoError(t, err)
	assert.Nil(t, resolution, "already-resolved providers must be skipped")
	assert.Nil(t, clients)
}

func TestAutoResolveOfficialVersioned_NotOfficial(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	composite := provider.NewCompositeRegistryFromBase(reg)
	// Registry with only "env" — solution references "custom-provider" which is not official.
	officialReg := official.NewRegistryFrom([]official.Provider{
		{Name: "env", CatalogRef: "env", DefaultVersion: "latest"},
	})
	cfg := &prepareConfig{officialProviders: officialReg}

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"mycustom": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "custom-provider"}},
					},
				},
			},
		},
	}

	resolution, clients, err := autoResolveOfficialVersioned(t.Context(), sol, composite, nil, cfg)
	require.NoError(t, err)
	assert.Nil(t, resolution, "non-official provider must not be resolved")
	assert.Nil(t, clients)
}

func TestAutoResolveOfficialVersioned_StrictModeError(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	composite := provider.NewCompositeRegistryFromBase(reg)
	officialReg := official.NewRegistry()
	cfg := &prepareConfig{
		officialProviders: officialReg,
		strict:            true,
	}

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myenv": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	resolution, clients, err := autoResolveOfficialVersioned(t.Context(), sol, composite, nil, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strict mode")
	assert.Contains(t, err.Error(), "env")
	assert.Nil(t, resolution)
	assert.Nil(t, clients)
}

func TestAutoResolveOfficialVersioned_StrictModeMultipleProviders(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	composite := provider.NewCompositeRegistryFromBase(reg)
	officialReg := official.NewRegistry()
	cfg := &prepareConfig{
		officialProviders: officialReg,
		strict:            true,
	}

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
				"r2": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "exec"}},
					},
				},
			},
		},
	}

	_, _, err := autoResolveOfficialVersioned(t.Context(), sol, composite, nil, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strict mode")
	// Both provider names must appear in the error.
	assert.Contains(t, err.Error(), "env")
	assert.Contains(t, err.Error(), "exec")
}

func TestAutoResolveOfficialVersioned_NoFetcher(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	composite := provider.NewCompositeRegistryFromBase(reg)
	officialReg := official.NewRegistry()
	cfg := &prepareConfig{
		officialProviders: officialReg,
		pluginFetcher:     nil,
	}

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myenv": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	_, _, err := autoResolveOfficialVersioned(t.Context(), sol, composite, nil, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugin fetcher")
	assert.Contains(t, err.Error(), "env")
}

func TestBuildExecutionRegistryCLIPreconditions(t *testing.T) {
	t.Parallel()

	validResolver := fakeAliasResolver{}

	tests := []struct {
		name     string
		reg      *provider.CompositeRegistry
		resolver RegistryAliasResolver
		cfg      *prepareConfig
		wantErr  error
	}{
		{
			name:     "rejects a nil prepare config",
			reg:      provider.NewCompositeRegistry(),
			resolver: validResolver,
			cfg:      nil,
			wantErr:  errNilPrepareConfig,
		},
		{
			name:     "rejects a nil provider registry",
			reg:      nil,
			resolver: validResolver,
			cfg:      &prepareConfig{},
			wantErr:  errNilCompositeRegistry,
		},
		{
			name:     "rejects a nil alias resolver",
			reg:      provider.NewCompositeRegistry(),
			resolver: nil,
			cfg:      &prepareConfig{},
			wantErr:  errNilAliasResolver,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := buildExecutionRegistryCLI(context.Background(), nil, tt.reg, tt.resolver, tt.cfg)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestBuildExecutionRegistryAPIPreconditions(t *testing.T) {
	t.Parallel()

	validResolver := fakeAliasResolver{}
	validPluginResolver := func(context.Context, []solution.PluginDependency) ([]catalog.ArtifactInfo, error) {
		return nil, nil
	}
	validAcquire := func(context.Context, []solution.PluginDependency) (func(), error) {
		return func() {}, nil
	}

	tests := []struct {
		name           string
		shared         *provider.CompositeRegistry
		resolver       RegistryAliasResolver
		pluginResolver ResolvePluginsFunc
		acquire        EnsureAndAcquireFunc
		wantErr        error
	}{
		{
			name:           "rejects a nil shared composite registry",
			shared:         nil,
			resolver:       validResolver,
			pluginResolver: validPluginResolver,
			acquire:        validAcquire,
			wantErr:        errNilCompositeRegistry,
		},
		{
			name:           "rejects a nil alias resolver",
			shared:         provider.NewCompositeRegistry(),
			resolver:       nil,
			pluginResolver: validPluginResolver,
			acquire:        validAcquire,
			wantErr:        errNilAliasResolver,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := BuildExecutionRegistryAPI(context.Background(), nil, tt.shared, tt.resolver, tt.pluginResolver, tt.acquire, LockModeBestEffort, nil)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// poisonPluginResolver is the default injected when a caller supplies no plugin
// resolver. It must fail with errNilPluginResolver whenever it is actually
// invoked, so the deferred "need the upstream catalog" failure stays loud.
func TestPoisonPluginResolver(t *testing.T) {
	t.Parallel()

	artifacts, err := poisonPluginResolver(context.Background(), []solution.PluginDependency{{Name: "exec"}})
	require.ErrorIs(t, err, errNilPluginResolver)
	require.Nil(t, artifacts)
}

// PoolBinder treats a nil acquire lazily: it is a valid no-op when there is
// nothing to pin, but must error with errNilAcquire once a non-empty dependency
// set actually needs pinning.
func TestPoolBinderNilAcquire(t *testing.T) {
	t.Parallel()

	shared := provider.NewCompositeRegistry()

	t.Run("no deps is a valid no-op", func(t *testing.T) {
		t.Parallel()
		reg, release, err := PoolBinder(shared, nil)(context.Background(), nil)
		require.NoError(t, err)
		require.Same(t, shared, reg)
		require.NotNil(t, release)
		release()
	})

	t.Run("errors when deps need pinning", func(t *testing.T) {
		t.Parallel()
		reg, release, err := PoolBinder(shared, nil)(
			context.Background(), []solution.PluginDependency{{Name: "exec"}},
		)
		require.ErrorIs(t, err, errNilAcquire)
		require.Nil(t, reg)
		require.Nil(t, release)
	})
}
