package prepare

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAliasResolver maps a lowercased OCI origin to a catalog alias, standing in
// for a *catalogindex.Index in tests that only exercise AliasForRegistry.
type fakeAliasResolver map[string]string

func (f fakeAliasResolver) AliasForRegistry(origin string) (string, bool) {
	alias, ok := f[strings.ToLower(origin)]
	return alias, ok
}

// resolvedInfo builds an ArtifactInfo carrying the catalog and version a
// successful ResolvePlugin would return.
func resolvedInfo(cat, version string) catalog.ArtifactInfo {
	return catalog.ArtifactInfo{
		Catalog:   cat,
		Reference: catalog.Reference{Version: semver.MustParse(version)},
	}
}

func TestValidateProviderInternal(t *testing.T) {
	t.Parallel()

	echo := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider}
	oidc := solution.PluginDependency{Name: "oidc", Kind: solution.PluginKindAuthHandler}

	t.Run("all references declared returns no error", func(t *testing.T) {
		t.Parallel()
		err := validateExternalProviderInternal([]string{"echo"}, []solution.PluginDependency{echo}, provider.IsBuiltinProvider)
		require.NoError(t, err)
	})

	t.Run("undeclared references are reported", func(t *testing.T) {
		t.Parallel()
		err := validateExternalProviderInternal([]string{"echo", "missing"}, []solution.PluginDependency{echo}, provider.IsBuiltinProvider)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUndeclaredProvider)
		var undeclared *UndeclaredProviderError
		require.ErrorAs(t, err, &undeclared)
		assert.Equal(t, []string{"missing"}, undeclared.Providers)
	})

	t.Run("wrong-kind dependency does not satisfy a provider reference", func(t *testing.T) {
		t.Parallel()
		err := validateExternalProviderInternal([]string{"oidc"}, []solution.PluginDependency{oidc}, provider.IsBuiltinProvider)
		require.Error(t, err)
		var undeclared *UndeclaredProviderError
		require.ErrorAs(t, err, &undeclared)
		assert.Equal(t, []string{"oidc"}, undeclared.Providers)
	})
}

func TestExternalProviderInternal(t *testing.T) {
	t.Parallel()

	echo := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider, Version: "^1.0.0"}
	isBuiltin := func(name string) bool { return name == "cel" }

	t.Run("declared external provider is returned", func(t *testing.T) {
		t.Parallel()
		got, err := externalProviderInternal([]string{"echo"}, []solution.PluginDependency{echo}, extractProviders, isBuiltin)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "echo", got[0].reference)
		assert.Equal(t, echo, got[0].PluginDependency)
	})

	t.Run("builtin references are skipped", func(t *testing.T) {
		t.Parallel()
		got, err := externalProviderInternal([]string{"cel"}, []solution.PluginDependency{echo}, extractProviders, isBuiltin)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("duplicate references collapse to one entry", func(t *testing.T) {
		t.Parallel()
		got, err := externalProviderInternal([]string{"echo", "echo"}, []solution.PluginDependency{echo}, extractProviders, isBuiltin)
		require.NoError(t, err)
		assert.Len(t, got, 1)
	})

	t.Run("undeclared reference errors but still returns declared ones", func(t *testing.T) {
		t.Parallel()
		got, err := externalProviderInternal([]string{"echo", "missing"}, []solution.PluginDependency{echo}, extractProviders, isBuiltin)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUndeclaredProvider)
		var undeclared *UndeclaredProviderError
		require.ErrorAs(t, err, &undeclared)
		assert.Equal(t, []string{"missing"}, undeclared.Providers)
		require.Len(t, got, 1)
		assert.Equal(t, "echo", got[0].reference)
	})
}

func TestResolveCatalogs(t *testing.T) {
	t.Parallel()

	resolver := fakeAliasResolver{"ghcr.io/other": "Prod"}

	sourced := func() providerDependency {
		return providerDependency{
			reference: "thing",
			PluginDependency: solution.PluginDependency{Name: "thing", Kind: solution.PluginKindProvider, Version: "^2.0.0", Source: &solution.PluginSource{
				Registry: "ghcr.io/other",
			}},
		}
	}

	t.Run("sourced dependency is bound to its configured catalog (lowercased)", func(t *testing.T) {
		t.Parallel()
		got, err := resolveRemoteRegistry([]providerDependency{sourced()}, resolver)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "prod", got[0].Catalog)
	})

	t.Run("short-name dependency without a registry is left untouched", func(t *testing.T) {
		t.Parallel()
		short := providerDependency{
			reference:        "echo",
			PluginDependency: solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider, Version: "^1.0.0"},
		}
		got, err := resolveRemoteRegistry([]providerDependency{short}, resolver)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Empty(t, got[0].Catalog)
	})

	t.Run("unconfigured registry is reported", func(t *testing.T) {
		t.Parallel()
		got, err := resolveRemoteRegistry([]providerDependency{sourced()}, fakeAliasResolver{})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnconfiguredCatalog)
		var unconfigured *UnconfiguredCatalogError
		require.ErrorAs(t, err, &unconfigured)
		assert.Equal(t, []string{"ghcr.io/other"}, unconfigured.Registries)
		assert.Empty(t, got[0].Catalog, "an unconfigured dep keeps its empty catalog")
	})
}

// providerLock builds a single-entry lock file for an unsourced provider
// dependency, matched by name + kind + constraint (see FindPluginByVersionConstraint).
func providerLock(constraint, version string) *bundler.LockFile {
	return &bundler.LockFile{Version: 1, Plugins: []bundler.LockPlugin{{
		Name:              "echo",
		Kind:              string(solution.PluginKindProvider),
		Constraint:        constraint,
		Version:           version,
		ResolvedCanonical: "ghcr.io/echo",
	}}}
}

func TestFindLockEntry(t *testing.T) {
	t.Parallel()

	// A resolved dep: catalog already bound, version is the requested constraint.
	dep := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider, Version: "^1.0.0", Catalog: "prod"}

	t.Run("nil lock is rejected", func(t *testing.T) {
		t.Parallel()
		lp, err := findLockEntry(dep, nil, LockModeStrict)
		require.Error(t, err)
		assert.Nil(t, lp)
		assert.Contains(t, err.Error(), "requires a lock file")
		assert.ErrorIs(t, err, ErrMissingLockFile)
	})

	t.Run("no matching lock entry is rejected", func(t *testing.T) {
		t.Parallel()
		lp, err := findLockEntry(dep, providerLock("^9.9.9", "1.4.2"), LockModeStrict)
		require.Error(t, err)
		assert.Nil(t, lp)
		assert.Contains(t, err.Error(), "no lock entry matches constraint")
	})

	t.Run("a catalogless dep is accepted; the catalog is derived later", func(t *testing.T) {
		t.Parallel()
		unbound := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider, Version: "^1.0.0"}
		lp, err := findLockEntry(unbound, providerLock("^1.0.0", "1.4.2"), LockModeStrict)
		require.NoError(t, err)
		require.NotNil(t, lp)
		assert.Equal(t, "1.4.2", lp.Version)
	})

	t.Run("matching entry with a resolved catalog is returned", func(t *testing.T) {
		t.Parallel()
		lp, err := findLockEntry(dep, providerLock("^1.0.0", "1.4.2"), LockModeStrict)
		require.NoError(t, err)
		require.NotNil(t, lp)
		assert.Equal(t, "1.4.2", lp.Version)
	})
}

func TestResolveStrict(t *testing.T) {
	t.Parallel()

	dep := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider, Version: "^1.0.0", Catalog: "prod"}
	resolver := fakeAliasResolver{"ghcr.io/echo": "prod"}

	t.Run("pins the dep version to the lock entry's resolved version", func(t *testing.T) {
		t.Parallel()
		got, err := resolveStrict(dep, providerLock("^1.0.0", "1.4.2"), resolver)
		require.NoError(t, err)
		assert.Equal(t, "1.4.2", got.Version)
		assert.Equal(t, "prod", got.Catalog)
	})

	t.Run("a lock entry without a resolved version cannot be pinned", func(t *testing.T) {
		t.Parallel()
		got, err := resolveStrict(dep, providerLock("^1.0.0", ""), resolver)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot pin in strict mode")
		assert.Empty(t, got.Name)
	})

	t.Run("propagates findLockEntry errors", func(t *testing.T) {
		t.Parallel()
		_, err := resolveStrict(dep, nil, resolver)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires a lock file")
	})
}

func TestResolveConstrained(t *testing.T) {
	t.Parallel()

	dep := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider, Version: "^1.0.0", Catalog: "prod"}
	resolver := fakeAliasResolver{"ghcr.io/echo": "prod"}

	t.Run("keeps the constraint rather than pinning the resolved version", func(t *testing.T) {
		t.Parallel()
		got, err := resolveConstrained(dep, providerLock("^1.0.0", "1.4.2"), resolver)
		require.NoError(t, err)
		assert.Equal(t, "^1.0.0", got.Version)
		assert.Equal(t, "prod", got.Catalog)
	})

	t.Run("propagates findLockEntry errors", func(t *testing.T) {
		t.Parallel()
		_, err := resolveConstrained(dep, nil, resolver)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires a lock file")
	})
}

func TestResolveBestEffort(t *testing.T) {
	t.Parallel()

	resolver := fakeAliasResolver{}

	t.Run("nil lock is tolerated and leaves the dep unchanged", func(t *testing.T) {
		t.Parallel()
		dep := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider, Version: "^1.0.0"}
		got, err := resolveBestEffort(dep, nil, resolver)
		require.NoError(t, err)
		assert.Equal(t, "^1.0.0", got.Version)
	})

	t.Run("a missing lock entry leaves the dep unchanged", func(t *testing.T) {
		t.Parallel()
		dep := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider, Version: "^1.0.0"}
		got, err := resolveBestEffort(dep, providerLock("^9.9.9", "1.4.2"), resolver)
		require.NoError(t, err)
		assert.Equal(t, "^1.0.0", got.Version)
	})

	t.Run("does not require a resolved catalog", func(t *testing.T) {
		t.Parallel()
		dep := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider, Version: "^1.0.0"}
		got, err := resolveBestEffort(dep, providerLock("^1.0.0", "1.4.2"), resolver)
		require.NoError(t, err)
		assert.Equal(t, "^1.0.0", got.Version)
		assert.Empty(t, got.Catalog)
	})
}

// countingBatchResolver is a ResolvePluginsFunc test double. It records the
// name of every dep it is asked to resolve and returns exactly one
// ArtifactInfo per input dep, in the same order, each bound to the same
// catalog + version. A batch resolver is invoked once per call, so no locking
// is needed.
type countingBatchResolver struct {
	calls int
	names []string
}

func (c *countingBatchResolver) resolve(_ context.Context, deps []solution.PluginDependency) ([]catalog.ArtifactInfo, error) {
	c.calls++
	out := make([]catalog.ArtifactInfo, len(deps))
	for i, dep := range deps {
		c.names = append(c.names, dep.Name)
		out[i] = resolvedInfo("prod", "1.4.2")
	}
	return out, nil
}

func shortDep(name string) providerDependency {
	return providerDependency{
		reference:        name,
		PluginDependency: solution.PluginDependency{Name: name, Kind: solution.PluginKindProvider, Version: "^1.0.0"},
	}
}

func TestRegistryCatalogBinder(t *testing.T) {
	t.Parallel()

	t.Run("resolves every empty-catalog dep to a concrete catalog and version", func(t *testing.T) {
		t.Parallel()
		res := &countingBatchResolver{}
		got, err := registryCatalogBinder(res.resolve)(context.Background(), []providerDependency{shortDep("echo"), shortDep("http")})
		require.NoError(t, err)
		require.Len(t, got, 2)
		for _, dep := range got {
			assert.Equal(t, "prod", dep.Catalog)
			assert.Equal(t, "1.4.2", dep.Version)
		}
		assert.Equal(t, 1, res.calls, "all empty-catalog deps resolve in a single batch call")
		assert.Equal(t, []string{"echo", "http"}, res.names)
	})

	t.Run("returns the input unchanged when no dep needs resolving", func(t *testing.T) {
		t.Parallel()
		res := &countingBatchResolver{}
		bound := providerDependency{
			reference:        "ghcr.io/other/thing",
			PluginDependency: solution.PluginDependency{Name: "thing", Kind: solution.PluginKindProvider, Version: "^2.0.0", Catalog: "prod"},
		}
		got, err := registryCatalogBinder(res.resolve)(context.Background(), []providerDependency{bound})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "prod", got[0].Catalog)
		assert.Equal(t, "^2.0.0", got[0].Version)
		assert.Equal(t, 0, res.calls, "a fully-bound input must not invoke the resolver")
	})

	t.Run("skips deps that already name a catalog", func(t *testing.T) {
		t.Parallel()
		res := &countingBatchResolver{}
		bound := providerDependency{
			reference:        "ghcr.io/other/thing",
			PluginDependency: solution.PluginDependency{Name: "thing", Kind: solution.PluginKindProvider, Version: "^2.0.0", Catalog: "prod"},
		}
		got, err := registryCatalogBinder(res.resolve)(context.Background(), []providerDependency{bound, shortDep("echo")})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "prod", got[0].Catalog)
		assert.Equal(t, "^2.0.0", got[0].Version, "an already-bound dep is left untouched")
		assert.Equal(t, "1.4.2", got[1].Version)
		assert.Equal(t, []string{"echo"}, res.names, "only the empty-catalog dep is resolved")
	})

	t.Run("scatters results back to the correct deps by position", func(t *testing.T) {
		t.Parallel()
		// Bind each dep to a name-derived catalog so a mis-scatter would surface
		// as the wrong catalog landing on the wrong dep.
		resolve := func(_ context.Context, deps []solution.PluginDependency) ([]catalog.ArtifactInfo, error) {
			out := make([]catalog.ArtifactInfo, len(deps))
			for i, dep := range deps {
				out[i] = resolvedInfo("cat-"+dep.Name, "1.0.0")
			}
			return out, nil
		}
		bound := providerDependency{
			reference:        "ghcr.io/other/thing",
			PluginDependency: solution.PluginDependency{Name: "thing", Kind: solution.PluginKindProvider, Version: "^2.0.0", Catalog: "prod"},
		}
		got, err := registryCatalogBinder(resolve)(context.Background(), []providerDependency{shortDep("echo"), bound, shortDep("http")})
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, "cat-echo", got[0].Catalog)
		assert.Equal(t, "prod", got[1].Catalog, "the bound middle dep is left untouched")
		assert.Equal(t, "cat-http", got[2].Catalog)
	})

	t.Run("propagates the resolver error", func(t *testing.T) {
		t.Parallel()
		boom := func(context.Context, []solution.PluginDependency) ([]catalog.ArtifactInfo, error) {
			return nil, errors.New("boom")
		}
		got, err := registryCatalogBinder(boom)(context.Background(), []providerDependency{shortDep("echo")})
		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "boom")
	})

	t.Run("errors when the resolver returns the wrong artifact count", func(t *testing.T) {
		t.Parallel()
		// Two deps need resolving but the resolver returns a single artifact: the
		// length guard must reject this rather than silently mis-scatter.
		undercount := func(context.Context, []solution.PluginDependency) ([]catalog.ArtifactInfo, error) {
			return []catalog.ArtifactInfo{resolvedInfo("prod", "1.4.2")}, nil
		}
		got, err := registryCatalogBinder(undercount)(context.Background(), []providerDependency{shortDep("echo"), shortDep("http")})
		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "resolver returned 1 artifacts for 2 dependencies")
	})

	t.Run("resolves many deps in one batch", func(t *testing.T) {
		t.Parallel()
		const n = 64
		deps := make([]providerDependency, n)
		for i := range deps {
			deps[i] = shortDep("provider-" + strconv.Itoa(i))
		}
		res := &countingBatchResolver{}
		got, err := registryCatalogBinder(res.resolve)(context.Background(), deps)
		require.NoError(t, err)
		require.Len(t, got, n)
		for _, dep := range got {
			assert.Equal(t, "prod", dep.Catalog)
			assert.Equal(t, "1.4.2", dep.Version)
		}
		assert.Equal(t, 1, res.calls)
	})
}
