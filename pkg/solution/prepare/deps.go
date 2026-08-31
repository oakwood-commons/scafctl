package prepare

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/executionregistry"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// ErrUndeclaredProvider is returned when a solution references a provider that
// is neither a builtin nor declared in bundle.plugins.
var ErrUndeclaredProvider = errors.New("undeclared provider")

// ErrMissingLockFile is returned when a strict or constrained resolution needs
// a lock file but none was provided. It is a sentinel so callers (notably the
// API boundary) can match with errors.Is and classify it as a client error
// (400) rather than a server fault (500).
var ErrMissingLockFile = errors.New("missing lock file")

// RegistryAliasResolver maps a plugin's canonical OCI origin ("registry" or
// "registry/repository") to a configured catalog alias. It is the single lookup
// dependency-scoping needs from the catalog topology; *catalogindex.Index
// satisfies it and is nil-safe. An origin the resolver does not recognize names
// an unconfigured catalog and must be rejected rather than fetched.
type RegistryAliasResolver interface {
	AliasForRegistry(origin string) (string, bool)
}

// ResolvePluginFunc binds a short name + version constraint to a concrete
// catalog artifact. *plugin.Fetcher.ResolvePlugin has this exact shape, so you
// pass the method value directly: prepare.ResolveDeps(ctx, fetcher.ResolvePlugin, deps).
//
// Implementations must be safe for concurrent use: resolvePluginDependencies
// fans out and may invoke the function from multiple goroutines at once.
type ResolvePluginFunc func(ctx context.Context, kind catalog.ArtifactKind, name, version, catalog string) (catalog.ArtifactInfo, error)

// ResolvePluginsFunc resolves a batch of short-name dependencies in one call.
// It returns exactly one ArtifactInfo per input dep, in the same order; callers
// rely on that positional correspondence to scatter results back.
type ResolvePluginsFunc func(ctx context.Context, deps []solution.PluginDependency) ([]catalog.ArtifactInfo, error)

// resolveRegistryToCatalog maps a sourced plugin's raw registry origin
// (PluginSource.Registry) to a configured catalog alias. The resolver is
// authoritative: an origin it does not resolve (including a nil resolver) names
// an unconfigured registry and is rejected rather than fetched from an
// unintended catalog. This preserves the invariant that a plugin is only
// fetched from a catalog the user has explicitly configured.
func resolveRegistryToCatalog(registry string, resolver RegistryAliasResolver) (string, error) {
	if alias, ok := resolver.AliasForRegistry(registry); ok {
		// Normalize to lowercase so a catalog identity (dep.Catalog, the dedupe
		// and lock key) is uniform regardless of the alias's configured case.
		return strings.ToLower(alias), nil
	}
	return "", fmt.Errorf(
		"provider registry %q is not a configured catalog; add it to the catalogs "+
			"configuration or reference a configured registry", registry)
}

// UndeclaredProviderError lists provider references that are neither builtins
// nor declared in bundle.plugins. Use errors.Is(err, ErrUndeclaredProvider) to
// detect it and errors.As to access the offending names.
type UndeclaredProviderError struct {
	Providers []string
}

var _ error = (*UndeclaredProviderError)(nil)

func (e *UndeclaredProviderError) Error() string {
	return fmt.Sprintf(
		"providers %v are not builtins and are not declared in bundle.plugins; add them to bundle.plugins",
		e.Providers)
}

func (e *UndeclaredProviderError) Is(target error) bool {
	return target == ErrUndeclaredProvider
}

// ErrUnconfiguredCatalog is returned when a provider dependency names a registry
// origin that is not a configured catalog.
var ErrUnconfiguredCatalog = errors.New("unconfigured catalog")

// UnconfiguredCatalogError lists provider registry origins that are not
// configured catalogs. Use errors.Is(err, ErrUnconfiguredCatalog) to detect it
// and errors.As to access the offending registries.
type UnconfiguredCatalogError struct {
	Registries []string
}

var _ error = (*UnconfiguredCatalogError)(nil)

func (e *UnconfiguredCatalogError) Error() string {
	return fmt.Sprintf(
		"provider registries %v are not configured catalogs; add them to the catalogs "+
			"configuration or reference a configured registry",
		e.Registries)
}

func (e *UnconfiguredCatalogError) Is(target error) bool {
	return target == ErrUnconfiguredCatalog
}

func ValidateExternalProviders(sol *solution.Solution, isBuiltin func(string) bool, extractProviderReferences func(*solution.Solution) []string) error {
	if extractProviderReferences == nil {
		extractProviderReferences = func(sol *solution.Solution) []string {
			return sol.Spec.ReferencedProviderNames()
		}
	}
	return validateExternalProviderInternal(extractProviderReferences(sol), sol.Bundle.Plugins, isBuiltin)
}

// validateExternalProviders checks that every provider referenced in the solution is either a builtin or declared in bundle.plugins.
// It returns an error listing any undeclared providers.
func validateExternalProviders(sol *solution.Solution, isBuiltin func(string) bool, extractProviderReferences func(*solution.Solution) []string) error {
	if extractProviderReferences == nil {
		extractProviderReferences = func(sol *solution.Solution) []string {
			return sol.Spec.ReferencedProviderNames()
		}
	}
	return validateExternalProviderInternal(extractProviderReferences(sol), sol.Bundle.Plugins, isBuiltin)
}

func validateExternalProviderInternal(providerReferences []string, dependencies []solution.PluginDependency, builtIn func(string) bool) error {
	dependenciesByKind := extractByKind(dependencies, solution.PluginKindProvider)
	dependenciesMap := make(map[string]solution.PluginDependency)
	for _, dep := range dependenciesByKind {
		dependenciesMap[dep.LocalName()] = dep
	}

	var unknownProviders []string
	for _, provider := range providerReferences {
		if builtIn != nil && builtIn(provider) {
			continue
		}
		if _, ok := dependenciesMap[provider]; !ok {
			unknownProviders = append(unknownProviders, provider)
		}
	}
	if len(unknownProviders) > 0 {
		return &UndeclaredProviderError{Providers: unknownProviders}
	}
	return nil
}

func extractByKind(deps []solution.PluginDependency, kind solution.PluginKind) []solution.PluginDependency {
	var result []solution.PluginDependency
	for _, dep := range deps {
		if dep.Kind == kind {
			result = append(result, dep)
		}
	}
	return result
}

type providerDependency struct {
	reference string
	solution.PluginDependency
}

// externalProviders returns a slice of providerDependency for every external provider referenced in the solution. must be called after validateProviders to ensure all providers are declared in bundle.plugins. It returns an error if any referenced provider is not declared.
func externalProviders(sol *solution.Solution, extractDependencyFunc func([]solution.PluginDependency) []solution.PluginDependency, isBuiltin func(string) bool, extractReferences func(*solution.Solution) []string) ([]providerDependency, error) {
	if extractReferences == nil {
		extractReferences = func(sol *solution.Solution) []string {
			return sol.Spec.ReferencedProviderNames()
		}
	}
	return externalProviderInternal(extractReferences(sol), sol.Bundle.Plugins, extractDependencyFunc, isBuiltin)
}

func externalProviderInternal(providerReferences []string, dependencies []solution.PluginDependency, extractDependencyFunc func([]solution.PluginDependency) []solution.PluginDependency, isBuiltin func(string) bool) ([]providerDependency, error) {
	nameToDep := make(map[string]solution.PluginDependency)
	for _, dep := range extractDependencyFunc(dependencies) {
		nameToDep[dep.LocalName()] = dep
	}

	var (
		result           []providerDependency
		unknownProviders []string
		seen             = make(map[string]struct{})
	)
	for _, provider := range providerReferences {
		if _, dup := seen[provider]; dup {
			continue
		}
		seen[provider] = struct{}{}

		if isBuiltin(provider) {
			continue
		}

		dep, ok := nameToDep[provider]
		if !ok {
			unknownProviders = append(unknownProviders, provider)
			continue
		}
		result = append(result, providerDependency{
			reference:        provider,
			PluginDependency: dep,
		})
	}

	if len(unknownProviders) > 0 {
		return result, &UndeclaredProviderError{Providers: unknownProviders}
	}
	return result, nil
}

// resolveRemoteRegistry resolves the registry origins of sourced provider dependencies to configured catalog aliases. It returns an error if any registry is not a configured catalog.
func resolveRemoteRegistry(deps []providerDependency, resolver RegistryAliasResolver) ([]providerDependency, error) {
	var unconfiguredRegistries []string
	for i, dep := range deps {
		if dep.HasRegistry() {
			catalogAlias, err := resolveRegistryToCatalog(dep.Registry(), resolver)
			if err != nil {
				unconfiguredRegistries = append(unconfiguredRegistries, dep.Registry())
				continue
			}
			deps[i].Catalog = catalogAlias
		}
	}
	if len(unconfiguredRegistries) > 0 {
		return deps, &UnconfiguredCatalogError{Registries: unconfiguredRegistries}
	}
	return deps, nil
}

func resolveLockMode(deps []providerDependency, lock *bundler.LockFile, mode LockMode, resolver RegistryAliasResolver) ([]providerDependency, error) {
	for i, dep := range deps {
		resolved, err := resolveByLockModeInternal(dep.PluginDependency, lock, mode, resolver)
		if err != nil {
			return nil, err
		}
		deps[i].PluginDependency = resolved
	}
	return deps, nil
}

// resolveDepByMode applies the lock-mode resolution strategy to a single
// provider dependency, returning the resolved dep or an error.
func resolveByLockModeInternal(dep solution.PluginDependency, lock *bundler.LockFile, mode LockMode, resolver RegistryAliasResolver) (solution.PluginDependency, error) {
	switch mode.OrDefault() {
	case LockModeStrict:
		return resolveStrict(dep, lock, resolver)
	case LockModeConstrained:
		return resolveConstrained(dep, lock, resolver)
	case LockModeBestEffort:
		return resolveBestEffort(dep, lock, resolver)
	default:
		return solution.PluginDependency{}, fmt.Errorf("provider %q: unknown lock mode %d", dep.ArtifactName(), mode)
	}
}

// findLockEntry is the shared preamble for strict and constrained modes. It
// validates that a lock file is present and finds the entry matching the
// dependency's name, kind, and version constraint. It does not resolve or
// require a catalog: the caller derives the catalog from the entry's
// ResolvedCanonical after this returns.
func findLockEntry(dep solution.PluginDependency, lock *bundler.LockFile, mode LockMode) (*bundler.LockPlugin, error) {
	if lock == nil {
		return nil, fmt.Errorf(
			"provider %q: %s mode requires a lock file but none was provided: %w",
			dep.ArtifactName(), mode, ErrMissingLockFile)
	}
	lp := lock.FindPluginByVersionConstraint(dep)
	if lp == nil {
		return nil, fmt.Errorf(
			"provider %q: no lock entry matches constraint %q; "+
				"the solution lock is out of sync with bundle.plugins",
			dep.ArtifactName(), dep.Version)
	}
	return lp, nil
}

func resolveStrict(dep solution.PluginDependency, lock *bundler.LockFile, resolver RegistryAliasResolver) (solution.PluginDependency, error) {
	lp, err := findLockEntry(dep, lock, LockModeStrict)
	if err != nil {
		return solution.PluginDependency{}, err
	}
	if lp.Version == "" {
		return solution.PluginDependency{}, fmt.Errorf(
			"provider %q: lock entry has no resolved version; "+
				"cannot pin in strict mode", dep.ArtifactName())
	}
	catalog, ok := resolver.AliasForRegistry(lp.ResolvedCanonical)
	if !ok {
		return solution.PluginDependency{}, fmt.Errorf(
			"provider %q: lock entry resolved to registry %q, which is not a configured catalog",
			dep.ArtifactName(), lp.ResolvedCanonical)
	}
	dep.Catalog = catalog
	dep.Version = lp.Version
	return dep, nil
}

func resolveConstrained(dep solution.PluginDependency, lock *bundler.LockFile, resolver RegistryAliasResolver) (solution.PluginDependency, error) {
	lp, err := findLockEntry(dep, lock, LockModeConstrained)
	if err != nil {
		return solution.PluginDependency{}, err
	}
	version := lp.Constraint
	if version == "" {
		version = dep.Version
	}
	if version == "" {
		return solution.PluginDependency{}, fmt.Errorf(
			"provider %q: no version constraint in bundle.plugins or lock entry; "+
				"cannot resolve in constrained mode", dep.ArtifactName())
	}
	catalog, ok := resolver.AliasForRegistry(lp.ResolvedCanonical)
	if !ok {
		return solution.PluginDependency{}, fmt.Errorf(
			"provider %q: lock entry resolved to registry %q, which is not a configured catalog",
			dep.ArtifactName(), lp.ResolvedCanonical)
	}
	dep.Version = version
	dep.Catalog = catalog
	return dep, nil
}

func resolveBestEffort(dep solution.PluginDependency, lock *bundler.LockFile, resolver RegistryAliasResolver) (solution.PluginDependency, error) {
	// Opportunistically consult the lock when available; the catalog is assumed
	// to be resolved already, so best-effort only refines the version.
	if lock != nil {
		if lp := lock.FindPluginByVersionConstraint(dep); lp != nil {
			version := lp.Constraint
			if version == "" {
				version = dep.Version
			}
			dep.Version = version
			catalog, ok := resolver.AliasForRegistry(lp.ResolvedCanonical)
			if ok {
				dep.Catalog = catalog
			}
		}
	}
	return dep, nil
}

func dependencyMap(deps []solution.PluginDependency) map[string]solution.PluginDependency {
	resolution := make(map[string]solution.PluginDependency, len(deps))
	for _, dep := range deps {
		resolution[dep.LocalName()] = dep
	}
	return resolution
}

// resolveProviderDependencies resolves the external provider dependencies of a solution according to the given lock mode and lock file. It validates that all referenced providers are declared in bundle.plugins, resolves their catalogs, and applies the lock mode resolution strategy. It returns a slice of resolved providerDependency or an error if any issues are encountered.
func resolveProviderDependencies(ctx context.Context, sol *solution.Solution, mode LockMode, lock *bundler.LockFile, resolver RegistryAliasResolver, strategy executionRegistryStrategy) ([]providerDependency, error) {
	err := validateExternalProviders(sol, strategy.isBuiltin, strategy.extractReferences)
	if err != nil {
		return nil, err
	}
	dependencies, err := externalProviders(sol, strategy.extractDependency, strategy.isBuiltin, strategy.extractReferences)
	if err != nil {
		return nil, err
	}
	dependencies, err = resolveRemoteRegistry(dependencies, resolver)
	if err != nil {
		return nil, err
	}
	dependencies, err = resolveLockMode(dependencies, lock, mode, resolver)
	if err != nil {
		return nil, err
	}
	// Bind each catalogless dep to a concrete catalog per the injected strategy
	// (noop for the fetch path, registry resolution for the pool path). Only
	// best-effort deps reach here still catalogless; strict/constrained already
	// resolved catalog and version from the lock file.
	dependencies, err = strategy.bindCatalogs(ctx, dependencies)
	if err != nil {
		return nil, err
	}

	return dependencies, nil
}

// catalogBinder ensures each external provider dependency is bound to a
// concrete catalog before the pipeline hands off to the ProviderBinder. Each
// implementation is total: it defines and satisfies its own postcondition, so
// downstream stages trust the result without re-checking. The fetch path
// injects noopCatalogBinder; the pool path injects registryCatalogBinder.
type catalogBinder func(ctx context.Context, deps []providerDependency) ([]providerDependency, error)

// noopCatalogBinder is the fetch-path strategy: it deliberately leaves
// short-name deps catalogless. The fetch path learns each dep's catalog from
// fetch results (backfill), so no pre-resolution is wanted here.
// Postcondition: deps are returned unchanged.
func noopCatalogBinder(_ context.Context, deps []providerDependency) ([]providerDependency, error) {
	return deps, nil
}

// poisonPluginResolver is the default plugin resolver injected when a caller
// supplies none. A resolver is only genuinely needed when a catalogless
// best-effort dep reaches registryCatalogBinder (the "need the upstream
// catalog" case); callers whose deps are all builtin or fully qualified never
// invoke it. Deferring the failure to the point of use lets those callers
// succeed while still failing loudly, with errNilPluginResolver, exactly when
// upstream catalog resolution is actually required.
func poisonPluginResolver(_ context.Context, _ []solution.PluginDependency) ([]catalog.ArtifactInfo, error) {
	return nil, errNilPluginResolver
}

// registryCatalogBinder is the pool-path strategy: it resolves every catalogless
// dep to a concrete catalog via the registry batch resolver and guarantees a
// non-empty catalog on every dep, erroring if the resolver leaves one unbound.
// Postcondition: no catalogless deps remain.
func registryCatalogBinder(resolve ResolvePluginsFunc) catalogBinder {
	return func(ctx context.Context, deps []providerDependency) ([]providerDependency, error) {
		depsToResolve := make([]solution.PluginDependency, 0, len(deps))
		originalIndices := make([]int, 0, len(deps))
		for i, dep := range deps {
			if dep.CatalogName() == "" {
				depsToResolve = append(depsToResolve, dep.PluginDependency)
				originalIndices = append(originalIndices, i)
			}
		}
		if len(depsToResolve) == 0 {
			return deps, nil
		}

		artifacts, err := resolve(ctx, depsToResolve)
		if err != nil {
			return nil, fmt.Errorf("resolving plugin dependencies: %w", err)
		}
		if len(artifacts) != len(depsToResolve) {
			return nil, fmt.Errorf(
				"resolver returned %d artifacts for %d dependencies",
				len(artifacts), len(depsToResolve))
		}

		for i, artifact := range artifacts {
			originalIndex := originalIndices[i]
			if artifact.Catalog == "" {
				return nil, fmt.Errorf(
					"resolver returned no catalog for provider %q",
					deps[originalIndex].ArtifactName())
			}
			deps[originalIndex].Catalog = artifact.Catalog
			if ref := artifact.Reference; ref.Version != nil {
				deps[originalIndex].Version = ref.Version.String()
			}
		}

		return deps, nil
	}
}

type ProviderBinder func(ctx context.Context, deps []solution.PluginDependency) (*provider.CompositeRegistry, func(), error)

// API: pool-managed, base is the pre-existing shared composite.
func PoolBinder(composite *provider.CompositeRegistry, acquire EnsureAndAcquireFunc) ProviderBinder {
	return func(ctx context.Context, deps []solution.PluginDependency) (*provider.CompositeRegistry, func(), error) {
		release := func() {}
		if len(deps) > 0 {
			// There are external plugins to pin, so an acquire function is
			// genuinely required here. A nil acquire is only valid when there
			// is nothing to pin (the early pass-through below).
			if acquire == nil {
				return nil, nil, errNilAcquire
			}
			rel, err := acquire(ctx, deps)
			if err != nil {
				return nil, nil, fmt.Errorf("acquiring plugins: %w", err)
			}
			if rel != nil {
				release = rel
			}
		}
		return composite, release, nil
	}
}

// CLI: build a fresh composite, fetch + register, cleanup kills the clients.
func FetchBinder(composite *provider.CompositeRegistry, cfg *prepareConfig) ProviderBinder {
	return func(ctx context.Context, deps []solution.PluginDependency) (*provider.CompositeRegistry, func(), error) {
		if len(deps) == 0 {
			return composite, func() {}, nil
		}
		// A fetcher is only required once there are external deps to fetch;
		// builtin-only and pure-CEL solutions reach the early return above.
		if cfg.pluginFetcher == nil {
			return nil, nil, errNilPluginFetcher
		}
		fetchResults, vClients, err := fetchAndRegisterIntoComposite(ctx, composite, deps, cfg)
		if err != nil {
			return composite, nil, err
		}
		if lgr := logger.FromContext(ctx); lgr != nil {
			for _, r := range fetchResults {
				src := "catalog"
				if r.FromCache {
					src = "cache"
				}
				lgr.V(1).Info("plugin loaded", "name", r.Name, "version", r.Version, "source", src)
			}
		}
		return composite, func() {
			for _, c := range vClients {
				c.Kill()
			}
		}, nil
	}
}

// executionRegistryStrategy bundles the per-path strategy functions the
// execution-registry pipeline is parameterized over: how to extract provider
// deps, how to recognize builtins, how to bind catalogs, and how to bind
// providers. The two wrappers are the only place these differ.
type executionRegistryStrategy struct {
	extractDependency func([]solution.PluginDependency) []solution.PluginDependency
	extractReferences func(*solution.Solution) []string
	isBuiltin         func(string) bool
	bindCatalogs      catalogBinder
	binder            ProviderBinder
}

func buildExecutionRegistry(
	ctx context.Context,
	sol *solution.Solution,
	mode LockMode,
	lock *bundler.LockFile,
	resolver RegistryAliasResolver,
	strategy executionRegistryStrategy,
) (*executionregistry.ExecutionRegistry[solution.PluginDependency], func(), error) {
	providerDependencies, err := resolveProviderDependencies(ctx, sol, mode, lock, resolver, strategy)
	if err != nil {
		return nil, nil, err
	}

	deps := make([]solution.PluginDependency, len(providerDependencies))
	for i, pd := range providerDependencies {
		deps[i] = pd.PluginDependency
	}

	composite, cleanup, err := strategy.binder(ctx, deps)
	if err != nil {
		return nil, nil, err
	}
	resolution := dependencyMap(deps)
	execRegistry := executionregistry.NewExecutionRegistry(composite, resolution)
	return execRegistry, cleanup, nil
}

// extractProviders is the provider-kind filter externalProviderInternal expects.
func extractProviders(deps []solution.PluginDependency) []solution.PluginDependency {
	return extractByKind(deps, solution.PluginKindProvider)
}
