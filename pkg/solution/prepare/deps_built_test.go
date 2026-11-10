package prepare

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// builtDep returns a source.built provider dependency aliased to the
// "scafctl-echo-provider" artifact.
func builtDep() solution.PluginDependency {
	return solution.PluginDependency{
		Name:    "echo",
		Kind:    solution.PluginKindProvider,
		Version: "^1.0.0",
		Source:  &solution.PluginSource{Artifact: "scafctl-echo-provider", Built: true},
	}
}

// builtLock is the lock VendorAllPlugins writes for builtDep (^1.0.0 locked to
// 1.4.2): Source.Built set, resolved from the local catalog, no canonical origin.
func builtLock() *bundler.LockFile {
	return &bundler.LockFile{Version: 1, Plugins: []bundler.LockPlugin{{
		Name:         "scafctl-echo-provider",
		Kind:         string(solution.PluginKindProvider),
		Constraint:   "^1.0.0",
		Version:      "1.4.2",
		ResolvedFrom: catalog.LocalCatalogName,
		Source:       &bundler.LockPluginSource{Built: true},
	}}}
}

// localShortNameLock is the lock for a short-name dep that the build resolved
// to a built-only copy in the local catalog (no Source, no canonical).
func localShortNameLock(resolvedFrom string) *bundler.LockFile {
	return &bundler.LockFile{Version: 1, Plugins: []bundler.LockPlugin{{
		Name:         "echo",
		Kind:         string(solution.PluginKindProvider),
		Constraint:   "^1.0.0",
		Version:      "1.4.2",
		ResolvedFrom: resolvedFrom,
	}}}
}

func TestResolveBuiltCatalog(t *testing.T) {
	t.Parallel()

	t.Run("built dependency is bound to the local catalog", func(t *testing.T) {
		t.Parallel()
		got, err := resolveBuiltCatalog([]providerDependency{{reference: "echo", PluginDependency: builtDep()}})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, catalog.LocalCatalogName, got[0].Catalog)
	})

	t.Run("short-name and registry dependencies are left untouched", func(t *testing.T) {
		t.Parallel()
		sourced := providerDependency{
			reference: "thing",
			PluginDependency: solution.PluginDependency{
				Name: "thing", Kind: solution.PluginKindProvider, Version: "^2.0.0", Catalog: "prod",
				Source: &solution.PluginSource{Registry: "ghcr.io/other", Artifact: "thing"},
			},
		}
		got, err := resolveBuiltCatalog([]providerDependency{shortDep("http"), sourced})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Empty(t, got[0].Catalog)
		assert.Equal(t, "prod", got[1].Catalog)
	})

	t.Run("a source naming both registry and built is rejected", func(t *testing.T) {
		t.Parallel()
		dep := builtDep()
		dep.Source.Registry = "ghcr.io/other"
		got, err := resolveBuiltCatalog([]providerDependency{{reference: "echo", PluginDependency: dep}})
		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "mutually exclusive")
	})

	t.Run("empty input is a no-op", func(t *testing.T) {
		t.Parallel()
		got, err := resolveBuiltCatalog(nil)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestLockEntryCatalog(t *testing.T) {
	t.Parallel()

	resolver := fakeAliasResolver{"ghcr.io/echo": "prod"}

	t.Run("built entry maps to the local catalog without consulting the resolver", func(t *testing.T) {
		t.Parallel()
		got, ok := lockEntryCatalog(&builtLock().Plugins[0], fakeAliasResolver{})
		require.True(t, ok)
		assert.Equal(t, catalog.LocalCatalogName, got)
	})

	t.Run("remote entry maps through the resolver", func(t *testing.T) {
		t.Parallel()
		got, ok := lockEntryCatalog(&providerLock("^1.0.0", "1.4.2").Plugins[0], resolver)
		require.True(t, ok)
		assert.Equal(t, "prod", got)
	})

	t.Run("unconfigured canonical is not found", func(t *testing.T) {
		t.Parallel()
		_, ok := lockEntryCatalog(&providerLock("^1.0.0", "1.4.2").Plugins[0], fakeAliasResolver{})
		assert.False(t, ok)
	})
}

func TestResolveLockModeBuilt(t *testing.T) {
	t.Parallel()

	// An empty resolver proves the built path never needs a remote alias.
	resolver := fakeAliasResolver{}

	t.Run("strict pins a built dep to the locked version in the local catalog", func(t *testing.T) {
		t.Parallel()
		got, err := resolveStrict(builtDep(), builtLock(), resolver)
		require.NoError(t, err)
		assert.Equal(t, "1.4.2", got.Version)
		assert.Equal(t, catalog.LocalCatalogName, got.Catalog)
		assert.True(t, got.HasBuilt(), "the built source is preserved")
	})

	t.Run("constrained keeps the constraint and binds the local catalog", func(t *testing.T) {
		t.Parallel()
		got, err := resolveConstrained(builtDep(), builtLock(), resolver)
		require.NoError(t, err)
		assert.Equal(t, "^1.0.0", got.Version)
		assert.Equal(t, catalog.LocalCatalogName, got.Catalog)
	})

	t.Run("best-effort binds the local catalog from the lock", func(t *testing.T) {
		t.Parallel()
		got, err := resolveBestEffort(builtDep(), builtLock(), resolver)
		require.NoError(t, err)
		assert.Equal(t, "^1.0.0", got.Version)
		assert.Equal(t, catalog.LocalCatalogName, got.Catalog)
	})

	t.Run("strict does not match a short-name entry for a built dep", func(t *testing.T) {
		t.Parallel()
		_, err := resolveStrict(builtDep(), localShortNameLock(catalog.LocalCatalogName), resolver)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no lock entry matches constraint")
	})

	t.Run("strict binds a short name locked from a built-only local copy", func(t *testing.T) {
		t.Parallel()
		dep := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider, Version: "^1.0.0"}
		got, err := resolveStrict(dep, localShortNameLock(catalog.LocalCatalogName), resolver)
		require.NoError(t, err)
		assert.Equal(t, "1.4.2", got.Version)
		assert.Equal(t, catalog.LocalCatalogName, got.Catalog)
	})

	t.Run("constrained binds a short name locked from a built-only local copy", func(t *testing.T) {
		t.Parallel()
		dep := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider, Version: "^1.0.0"}
		got, err := resolveConstrained(dep, localShortNameLock(catalog.LocalCatalogName), resolver)
		require.NoError(t, err)
		assert.Equal(t, "^1.0.0", got.Version)
		assert.Equal(t, catalog.LocalCatalogName, got.Catalog)
	})

	t.Run("strict still rejects a legacy entry without a canonical from another catalog", func(t *testing.T) {
		t.Parallel()
		dep := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider, Version: "^1.0.0"}
		_, err := resolveStrict(dep, localShortNameLock("official"), resolver)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a configured catalog")
	})

	t.Run("constrained still rejects a legacy entry without a canonical from another catalog", func(t *testing.T) {
		t.Parallel()
		dep := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider, Version: "^1.0.0"}
		_, err := resolveConstrained(dep, localShortNameLock("official"), resolver)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a configured catalog")
	})
}

func TestResolveProviderDependenciesBuilt(t *testing.T) {
	t.Parallel()

	sol := func() *solution.Solution {
		s := &solution.Solution{}
		s.Bundle.Plugins = []solution.PluginDependency{builtDep()}
		return s
	}
	strategy := func(res *countingBatchResolver) executionRegistryStrategy {
		return executionRegistryStrategy{
			extractDependency: extractProviders,
			extractReferences: func(*solution.Solution) []string { return []string{"echo"} },
			isBuiltin:         func(string) bool { return false },
			bindCatalogs:      registryCatalogBinder(res.resolve),
		}
	}

	tests := []struct {
		name        string
		mode        LockMode
		lock        *bundler.LockFile
		wantVersion string
	}{
		{name: "strict", mode: LockModeStrict, lock: builtLock(), wantVersion: "1.4.2"},
		{name: "constrained", mode: LockModeConstrained, lock: builtLock(), wantVersion: "^1.0.0"},
		{name: "best-effort with lock", mode: LockModeBestEffort, lock: builtLock(), wantVersion: "^1.0.0"},
		{name: "best-effort without lock", mode: LockModeBestEffort, lock: nil, wantVersion: "^1.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := &countingBatchResolver{}
			got, err := resolveProviderDependencies(context.Background(), sol(), tt.mode, tt.lock, fakeAliasResolver{}, strategy(res))
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, catalog.LocalCatalogName, got[0].Catalog)
			assert.Equal(t, tt.wantVersion, got[0].Version)
			assert.Equal(t, 0, res.calls, "a built dep is already bound, so the catalog binder must not resolve it across the chain")
		})
	}

	t.Run("a source naming both registry and built fails the pipeline", func(t *testing.T) {
		t.Parallel()
		s := sol()
		s.Bundle.Plugins[0].Source.Registry = "ghcr.io/other"
		resolver := fakeAliasResolver{"ghcr.io/other": "prod"}
		_, err := resolveProviderDependencies(context.Background(), s, LockModeBestEffort, nil, resolver, strategy(&countingBatchResolver{}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mutually exclusive")
	})
}
