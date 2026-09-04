// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

import (
	"context"
	"errors"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/executionregistry"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"golang.org/x/sync/errgroup"
)

// EnsureAndAcquireFunc loads every plugin dependency in deps and pins a
// reference on each for the duration of a unit of work, returning a release
// function that drops all acquired references and must be called when the caller
// is done (typically via defer). Both *plugin.Pool.EnsureAndAcquire and
// *plugin.VersionPool.EnsureAndAcquire satisfy this signature, so a caller can
// pass the method value directly. Passing nil is valid only when there are no
// external plugins to pin; PoolBinder errors with errNilAcquire if a nil
// acquire is reached with a non-empty dependency set.
type EnsureAndAcquireFunc func(ctx context.Context, deps []solution.PluginDependency) (release func(), err error)

// fetchPluginFunc fetches a single plugin dependency. *plugin.Fetcher.FetchPlugin
// satisfies this signature, so callers pass the method value directly and tests
// can pass a stub.
type fetchPluginFunc func(ctx context.Context, dep solution.PluginDependency, lockPlugins []bundler.LockPlugin) (plugin.FetchResult, error)

// fetchDependencies fetches each dependency individually, returning the fetch
// results in the same order as deps. The positional correspondence is
// guaranteed: out[i] is the result for deps[i], so callers can pair results back
// to dependencies by index without carrying the dependency alongside each
// result. Fetches run concurrently, bounded by settings.DefaultFetchConcurrency;
// if any fetch fails, the first error is returned and partial results are
// discarded.
func fetchDependencies(
	ctx context.Context,
	deps []solution.PluginDependency,
	lockPlugins []bundler.LockPlugin,
	fetch fetchPluginFunc,
) ([]plugin.FetchResult, error) {
	out := make([]plugin.FetchResult, len(deps))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(settings.DefaultFetchConcurrency)
	for i, dep := range deps {
		g.Go(func() error {
			res, err := fetch(ctx, dep, lockPlugins)
			if err != nil {
				return fmt.Errorf("fetching %s: %w", dep.DisplayName(), err)
			}
			out[i] = res
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

// fetchAndRegisterIntoComposite fetches plugin dependencies and registers them
// into the composite registry's external (versioned) tier. Before fetching it
// records the indexes of deps that arrive without a catalog, then -- relying on
// the positional order guaranteed by fetchDependencies -- backfills each such
// dep's Catalog from its fetch result before registration. deps is mutated in
// place, so on return every dep that could be resolved carries a concrete
// catalog and callers keying on it (e.g. the execution-registry resolution map)
// route to the external tier. Returns the fetch results (deps-order) and the
// versioned clients started during registration.
func fetchAndRegisterIntoComposite(
	ctx context.Context,
	composite *provider.CompositeRegistry,
	deps []solution.PluginDependency,
	cfg *prepareConfig,
) ([]plugin.FetchResult, []*plugin.VersionedClient, error) {
	// Record which deps arrive catalogless so we can backfill the resolved
	// catalog after fetch (positional order is guaranteed by fetchDependenciesV2).
	catalogless := make([]int, 0, len(deps))
	for i, dep := range deps {
		if dep.CatalogName() == "" {
			catalogless = append(catalogless, i)
		}
	}

	fetchResults, fetchErr := fetchDependencies(ctx, deps, cfg.lockPlugins, cfg.pluginFetcher.FetchPlugin)
	if fetchErr != nil {
		return nil, nil, fmt.Errorf("auto-fetching plugins: %w", fetchErr)
	}

	// Backfill the resolved catalog onto each catalogless dep before
	// registration so downstream routing keys on the concrete catalog.
	for _, i := range catalogless {
		if fetchResults[i].Catalog != "" {
			deps[i].Catalog = fetchResults[i].Catalog
		}
	}

	vClients, regErr := plugin.RegisterFetchedVersionedPlugins(
		ctx, composite, fetchResults, cfg.pluginCfg, nil, nil, cfg.clientOpts...,
	)
	if regErr != nil {
		return nil, nil, fmt.Errorf("registering fetched plugins: %w", regErr)
	}

	return fetchResults, vClients, nil
}

// autoResolveOfficialVersioned resolves official providers that are referenced
// in the solution but missing from the composite registry. Unlike
// autoResolveOfficialProviders (which registers into the flat *Registry), this
// registers into the composite's versioned external tier with
// official.CatalogName hardcoded on each dependency.
//
// existingResolution is the resolution map from fetchAndRegisterVersioned;
// providers already present there are skipped so bundle.plugins always wins.
//
// Returns additional resolution map entries (provider short name →
// PluginDependency with Catalog set) and the versioned clients started during
// registration. The caller merges these into fetchAndRegisterVersionedResult
// before building a ExecutionRegistry.
func autoResolveOfficialVersioned(
	ctx context.Context,
	sol *solution.Solution,
	composite *provider.CompositeRegistry,
	existingResolution map[string]solution.PluginDependency,
	cfg *prepareConfig,
) (map[string]solution.PluginDependency, []*plugin.VersionedClient, error) {
	if cfg.officialProviders == nil || cfg.officialProviders.Len() == 0 {
		return nil, nil, nil
	}

	lgr := logger.FromContext(ctx)

	// Find providers referenced in the solution that are not yet registered
	// in either the builtin or external tier, and not already in the
	// resolution map (which means fetchAndRegisterVersioned already handled them).
	var missing []official.Provider
	for _, name := range sol.Spec.ReferencedProviderNames() {
		if composite.HasBase(name) {
			continue
		}
		if _, resolved := existingResolution[name]; resolved {
			continue
		}
		if p, ok := cfg.officialProviders.Get(name); ok {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return nil, nil, nil
	}

	// Strict mode: refuse to auto-resolve, require explicit bundle.plugins.
	if cfg.strict {
		names := make([]string, len(missing))
		for i, p := range missing {
			names[i] = p.Name
		}
		return nil, nil, fmt.Errorf(
			"strict mode: providers %v are official but not declared in bundle.plugins; "+
				"add them explicitly or disable strict mode",
			names,
		)
	}

	if cfg.pluginFetcher == nil {
		names := make([]string, len(missing))
		for i, p := range missing {
			names[i] = p.Name
		}
		return nil, nil, fmt.Errorf("official providers %v need auto-resolution but no plugin fetcher is available", names)
	}

	// Build synthetic deps with Catalog hardcoded to the official catalog.
	deps := make([]solution.PluginDependency, len(missing))
	for i, p := range missing {
		dep := p.ToPluginDependency()
		dep.Catalog = official.CatalogName
		deps[i] = dep
	}

	if lgr != nil {
		names := make([]string, len(missing))
		for i, p := range missing {
			names[i] = p.Name
		}
		lgr.V(0).Info("auto-resolving official providers (versioned)", "providers", names)
	}

	_, vClients, err := fetchAndRegisterIntoComposite(ctx, composite, deps, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("auto-resolving official providers: %w", err)
	}

	// Build resolution map entries: short provider name → dep with catalog.
	// Catalog is already hardcoded to official.CatalogName on each dep.
	resolution := make(map[string]solution.PluginDependency, len(missing))
	for i, p := range missing {
		resolution[p.Name] = deps[i]
	}

	return resolution, vClients, nil
}

// Boundary preconditions for the execution-registry wrappers. Each wrapper is
// the single point where its collaborators cross from "not guaranteed" to
// "guaranteed": it validates them here, unconditionally, so every stage
// downstream (and each injected binder) may trust them without re-checking.
// They are package-level sentinels so callers and tests can assert with
// errors.Is.
var (
	errNilAliasResolver     = errors.New("prepare: registry alias resolver is required")
	errNilPrepareConfig     = errors.New("prepare: prepare config is required")
	errNilPluginFetcher     = errors.New("prepare: plugin fetcher is required")
	errNilCompositeRegistry = errors.New("prepare: provider registry is required")
	errNilPluginResolver    = errors.New("prepare: plugin resolver is required")
	errNilAcquire           = errors.New("prepare: plugin acquire function is required")
)

// buildExecutionRegistryCLI is the fetch-path wrapper: it supplies no plugin
// resolver (noopCatalogBinder) so short-name deps stay catalogless and learn
// their catalog from fetch results, and binds providers with FetchBinder, which
// builds a fresh composite off reg and fetches + registers the declared
// plugins. The returned cleanup kills the versioned clients started during
// registration.
//
// It validates its fetch-path collaborators up front: cfg, reg, and resolver
// must all be present before the pipeline runs. The plugin fetcher is validated
// lazily by FetchBinder at the point of use, so builtin-only and pure-CEL
// solutions (which never fetch) do not require one.
func buildExecutionRegistryCLI(
	ctx context.Context,
	sol *solution.Solution,
	composite *provider.CompositeRegistry,
	resolver RegistryAliasResolver,
	cfg *prepareConfig,
) (*executionregistry.ExecutionRegistry[solution.PluginDependency], func(), error) {
	if cfg == nil {
		return nil, nil, errNilPrepareConfig
	}
	if composite == nil {
		return nil, nil, errNilCompositeRegistry
	}
	if resolver == nil {
		return nil, nil, errNilAliasResolver
	}
	isBuiltin := func(s string) bool {
		return composite.HasBase(s)
	}
	return buildExecutionRegistry(
		ctx, sol, cfg.lockMode, cfg.lockFile, resolver,
		executionRegistryStrategy{
			extractDependency: extractProviders,
			isBuiltin:         isBuiltin,
			bindCatalogs:      noopCatalogBinder,
			binder:            FetchBinder(composite, cfg),
			extractReferences: nil,
		},
	)
}

// buildExecutionRegistryAPI is the pool-path wrapper: it supplies a real batch
// plugin resolver so every short-name dep is bound to a concrete catalog before
// the pool sees it, and binds providers with PoolBinder, which acquires
// references on the shared composite. The returned cleanup releases those
// references.
//
// It validates its pool-path collaborators up front: shared and resolver must
// be present before the pipeline runs. pluginResolver and acquire are both
// optional and validated lazily at the point of use: pluginResolver defaults to
// poisonPluginResolver (which fails only if a catalogless best-effort dep
// actually needs upstream catalog resolution), and acquire is only required by
// PoolBinder when there are external plugins to pin, so builtin-only and
// pure-CEL solutions may omit both.
func BuildExecutionRegistryAPI(
	ctx context.Context,
	sol *solution.Solution,
	shared *provider.CompositeRegistry,
	resolver RegistryAliasResolver,
	pluginResolver ResolvePluginsFunc,
	acquire EnsureAndAcquireFunc,
	mode LockMode,
	lock *bundler.LockFile,
) (*executionregistry.ExecutionRegistry[solution.PluginDependency], func(), error) {
	if shared == nil {
		return nil, nil, errNilCompositeRegistry
	}
	if resolver == nil {
		return nil, nil, errNilAliasResolver
	}
	if pluginResolver == nil {
		pluginResolver = poisonPluginResolver
	}
	isBuiltin := func(s string) bool {
		return shared.HasBase(s)
	}
	return buildExecutionRegistry(
		ctx, sol, mode, lock, resolver,
		executionRegistryStrategy{
			extractDependency: extractProviders,
			isBuiltin:         isBuiltin,
			bindCatalogs:      registryCatalogBinder(pluginResolver),
			binder:            PoolBinder(shared, acquire),
		},
	)
}
