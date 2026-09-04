// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/metrics"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

var ErrDedupTimeout = errors.New("operation timed out while waiting for concurrent request")

// ErrBuiltinProvider is returned when a fetch is requested for a built-in
// provider, which is compiled into the binary and cannot be fetched from a
// catalog.
var ErrBuiltinProvider = errors.New("built-in provider cannot be fetched from a catalog")

type resolver interface {
	resolvePlugin(ctx context.Context, kind catalog.ArtifactKind, name, version, catalogName string) (catalog.ArtifactInfo, error)
}

var _ resolver = (*Fetcher)(nil)

type artifactNamer interface {
	ArtifactName() string
}
type cataloger interface {
	CatalogName() string
}
type versionConstraint interface {
	VersionConstraint() string
}
type pluginKind interface {
	PluginKind() solution.PluginKind
}
type localNamer interface {
	LocalName() string
	DisplayName() string
}
type registryInfo interface {
	HasRegistry() bool
	Registry() string
}
type pluginArtifact interface {
	versionConstraint
	cataloger
	artifactNamer
	pluginKind
	localNamer
	registryInfo
}

var _ pluginArtifact = solution.PluginDependency{}

// Fetcher resolves, downloads, caches, and loads plugin binaries at runtime.
// It checks a local cache first, then falls back to fetching from catalogs.
type Fetcher struct {
	binaryName     string
	catalogFetcher *catalog.PluginFetcher
	cache          *Cache
	platform       string
	noCache        bool
	logger         logr.Logger
	sigPolicy      *SignaturePolicy
	sigVerifier    SignatureVerifier
	sfResolver     singleflight.Group
	sfFetcher      singleflight.Group
	// catalogIndex is the catalog topology (alias <-> canonical, registry hash)
	// plus the allowlist gate, built once from the fetcher's own catalog chain.
	catalogIndex *catalogindex.Index
}
type pluginFetchResult struct {
	bytes []byte
	info  catalog.ArtifactInfo
}

// FetcherConfig configures a Fetcher.
type FetcherConfig struct {
	// Catalog is the catalog (or chain) to fetch plugins from.
	Catalog catalog.Catalog

	// Cache is the local plugin binary cache. If nil, a default cache is created.
	Cache *Cache

	// Platform overrides the target platform. If empty, CurrentPlatform() is used.
	Platform string

	// NoCache bypasses the local cache, forcing a fresh fetch from the catalog.
	// Cached binaries are still written after fetch (the cache is populated but not read).
	NoCache bool

	// BinaryName is the CLI binary name used in user-facing messages (e.g.,
	// "Run 'mycli build solution' to pin..."). Defaults to "scafctl" when empty.
	BinaryName string

	// AllowedCatalogs restricts which catalog names plugins may be fetched
	// from. If empty, all catalogs are allowed.
	AllowedCatalogs []string

	// PerCatalogArtifacts restricts which plugin NAMES each catalog may serve,
	// keyed by catalog name. This is the cache-safe complement to the chain's
	// AllowlistCatalog decorator: the fetcher consults it on every return path
	// (including cache and lock hits, which never touch the chain) so a
	// tightened per-catalog policy cannot be bypassed by an already-cached
	// binary. If nil, no per-catalog plugin restriction is applied.
	PerCatalogArtifacts map[string]catalog.PluginPolicy

	// SignaturePolicy configures Sigstore/cosign signature verification.
	// When nil or Mode is "off", no signature verification is performed.
	SignaturePolicy *SignaturePolicy

	// SignatureVerifier is the implementation used for OCI signature checks.
	// When nil, NewSignatureVerifier() is used (cosign if built with the
	// "cosign" tag, otherwise a no-op stub).
	SignatureVerifier SignatureVerifier

	// Logger for logging operations.
	Logger logr.Logger
}

// NewFetcher creates a new Fetcher.
func NewFetcher(cfg FetcherConfig) *Fetcher {
	cache := cfg.Cache
	if cache == nil {
		cache = NewCache("")
	}

	platform := cfg.Platform
	if platform == "" {
		platform = CurrentPlatform()
	}

	binaryName := cfg.BinaryName
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}

	sigVerifier := cfg.SignatureVerifier
	if sigVerifier == nil {
		sigVerifier = NewSignatureVerifier()
	}

	// Build the catalog topology from the fetcher's own catalog chain so the
	// index describes exactly the catalogs this fetcher can reach, then layer the
	// allowlist and per-catalog plugin policy gates on top (copy-on-write,
	// leaving the base topology shared).
	catalogIndex := catalogindex.FromChain(cfg.Catalog).
		WithAllowed(cfg.AllowedCatalogs).
		WithPluginPolicies(cfg.PerCatalogArtifacts)

	return &Fetcher{
		binaryName:     binaryName,
		catalogFetcher: catalog.NewPluginFetcher(cfg.Catalog, cfg.Logger),
		cache:          cache,
		platform:       platform,
		noCache:        cfg.NoCache,
		logger:         cfg.Logger.WithName("plugin-fetcher"),
		sigPolicy:      cfg.SignaturePolicy,
		sigVerifier:    sigVerifier,
		catalogIndex:   catalogIndex,
	}
}

// resolveCatalogIdentity resolves a catalog alias (from ArtifactInfo.Catalog or
// lock file) to its identity in the catalog index. Returns a zero identity when
// the alias is empty. For an unknown alias it returns a bare identity carrying
// the value as its canonical origin, so the registry hash is still derived
// (computed on demand) rather than lost.
func (f *Fetcher) resolveCatalogIdentity(resolvedFrom string) catalogindex.CatalogIdentity {
	if resolvedFrom == "" {
		return catalogindex.CatalogIdentity{}
	}
	if id, ok := f.catalogIndex.IdentityForAlias(resolvedFrom); ok {
		return id
	}
	// Not a known alias -- treat the value as a literal canonical origin.
	return catalogindex.CatalogIdentity{Canonical: resolvedFrom}
}

// aliasForCanonical returns the configured catalog alias whose canonical origin
// matches the given canonical, or "" when no configured catalog matches. It lets
// a locked plugin recover its current catalog from the rename-proof canonical
// origin recorded in the lock, rather than the potentially stale stored alias.
func (f *Fetcher) aliasForCanonical(canonical string) string {
	if canonical == "" {
		return ""
	}
	if id, ok := f.catalogIndex.IdentityForCanonical(canonical); ok {
		return id.Alias
	}
	return ""
}

func (s *Fetcher) ResolvePlugins(ctx context.Context, deps []solution.PluginDependency) ([]catalog.ArtifactInfo, error) {
	return resolvePlugins(ctx, s, deps)
}

func resolvePlugins[T pluginArtifact](ctx context.Context, s resolver, deps []T) ([]catalog.ArtifactInfo, error) {
	infos, err := mapConcurrent(ctx, settings.DefaultResolveConcurrency, deps,
		func(ctx context.Context, dep T) (catalog.ArtifactInfo, error) {
			return s.resolvePlugin(ctx, catalog.ArtifactKind(dep.PluginKind()),
				dep.ArtifactName(), dep.VersionConstraint(), dep.CatalogName())
		})
	if err != nil {
		// Preserve the existing contract: return nil (not partial) on error.
		return nil, err
	}
	return infos, nil
}

// mapConcurrent applies fn to every item concurrently, bounded by limit, and
// returns index-aligned results. It returns (nil, nil) for empty input. On the
// first error it returns the results collected so far (unfinished slots hold
// zero values) alongside that error.
func mapConcurrent[T, R any](ctx context.Context, limit int, items []T, fn func(context.Context, T) (R, error)) ([]R, error) {
	if len(items) == 0 {
		return nil, nil
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
	results := make([]R, len(items))
	for i, item := range items {
		g.Go(func() error {
			r, err := fn(ctx, item)
			if err != nil {
				return err
			}
			results[i] = r
			return nil
		})
	}
	return results, g.Wait()
}

func (s *Fetcher) resolvePlugin(ctx context.Context, kind catalog.ArtifactKind, name, version, catalogName string) (catalog.ArtifactInfo, error) {
	if err := s.catalogIndex.CheckAllowed(catalogName); err != nil {
		return catalog.ArtifactInfo{}, err
	}
	if err := s.catalogIndex.CheckPluginAllowed(catalogName, name); err != nil {
		return catalog.ArtifactInfo{}, err
	}
	key := name + "/" + string(kind) + "/" + version + "/" + catalogName
	return dedupeRequest(ctx, &s.sfResolver, key, settings.DefaultPluginResolveTimeout, func(ctx context.Context) (catalog.ArtifactInfo, error) {
		var opts []catalog.Option
		if catalogName != "" {
			opts = append(opts, catalog.WithCatalog(catalogName))
		}
		return s.catalogFetcher.ResolvePlugin(ctx, name, kind, version, opts...)
	})
}

// fetchPlugin downloads a plugin binary, optionally restricting the fetch to a
// single named catalog. An empty catalogName preserves the default chain
// (fallback) behavior. The catalog is part of the singleflight key so a scoped
// and an unscoped fetch of the same reference are never conflated.
func (s *Fetcher) fetchPlugin(ctx context.Context, kind catalog.ArtifactKind, name, version, platform, catalogName string) ([]byte, catalog.ArtifactInfo, error) {
	checkKey := name + "/" + string(kind) + "/" + version + "/" + platform + "/" + catalogName
	result, err := dedupeRequest(ctx, &s.sfFetcher, checkKey, settings.DefaultPluginFetchTimeout, func(ctx context.Context) (pluginFetchResult, error) {
		var opts []catalog.Option
		if catalogName != "" {
			opts = append(opts, catalog.WithCatalog(catalogName))
		}
		bytes, info, err := s.catalogFetcher.FetchPlugin(ctx, name, kind, version, platform, opts...)
		fetchResult := pluginFetchResult{
			bytes: bytes,
			info:  info,
		}
		return fetchResult, err
	})
	if err != nil {
		return nil, catalog.ArtifactInfo{}, err
	}
	return result.bytes, result.info, nil
}

func dedupeRequest[T any](ctx context.Context, g *singleflight.Group, key string, timeOut time.Duration, fn func(context.Context) (T, error)) (T, error) {
	ch := g.DoChan(key, func() (any, error) {
		sfCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeOut)
		defer cancel()
		val, err := fn(sfCtx)
		if sfCtx.Err() != nil && errors.Is(err, context.DeadlineExceeded) {
			g.Forget(key)
			return val, fmt.Errorf("%w: %w", ErrDedupTimeout, err)
		}
		return val, err
	})
	var zero T
	select {
	case res := <-ch:
		if res.Err != nil {
			return zero, res.Err
		}
		val, ok := res.Val.(T)
		if !ok {
			return zero, fmt.Errorf("unexpected result type %T", res.Val)
		}
		return val, nil
	case <-ctx.Done():
		// Caller's own context — always ctx.Err() (Canceled or DeadlineExceeded)
		return zero, ctx.Err()
	}
}

// catalogLister adapts the Fetcher's PluginFetcher to bundler.VersionLister,
// optionally scoping the listing to a single named catalog.
type catalogLister struct {
	fetcher     *catalog.PluginFetcher
	catalogName string
}

func (l *catalogLister) List(ctx context.Context, kind catalog.ArtifactKind, name string) ([]catalog.ArtifactInfo, error) {
	var opts []catalog.Option
	if l.catalogName != "" {
		opts = append(opts, catalog.WithCatalog(l.catalogName))
	}
	return l.fetcher.List(ctx, kind, name, opts...)
}

// resolveContentDigest resolves the content-layer digest for a plugin binary
// without downloading the blob. It delegates to PluginFetcher.ResolveContentDigest
// which type-asserts the backing catalog to PlatformAwareCatalog. Concurrent
// requests for the same plugin/platform are deduplicated via singleflight.
func (s *Fetcher) resolveContentDigest(ctx context.Context, kind catalog.ArtifactKind, name, version, platform, mediaType, catalogName string) (catalog.ContentDigestInfo, error) {
	key := "cd/" + name + "/" + string(kind) + "/" + version + "/" + platform + "/" + catalogName
	return dedupeRequest(ctx, &s.sfResolver, key, settings.DefaultPluginResolveTimeout, func(ctx context.Context) (catalog.ContentDigestInfo, error) {
		var opts []catalog.Option
		if catalogName != "" {
			opts = append(opts, catalog.WithCatalog(catalogName))
		}
		return s.catalogFetcher.ResolveContentDigest(ctx, name, kind, version, platform, mediaType, opts...)
	})
}

// tryCachedAcrossCatalogs attempts to find a cached plugin binary across all
// known catalog identities. This is used when no lock file is present and we
// don't yet know which catalog the plugin came from. Returns the first hit.
// Identities are iterated in config-definition order (via the catalog index) to
// ensure deterministic plugin selection across runs.
func (f *Fetcher) tryCachedAcrossCatalogs(dep pluginArtifact) (FetchResult, bool) {
	cacheName := PluginCacheKey(dep.ArtifactName(), dep.PluginKind())
	catalogName := dep.CatalogName()
	for _, id := range f.catalogIndex.All() {
		if catalogName != "" && catalogName != id.Alias {
			continue
		}
		pinPath, cachedVer, release, ok := f.cache.GetLatestCachedPin(cacheName, f.platform, WithRegistryHash(id.RegistryHash()))
		if !ok {
			continue
		}
		satisfies, _ := cachedVersionSatisfies(dep.VersionConstraint(), cachedVer)
		if !satisfies {
			release()
			continue
		}
		if err := f.catalogIndex.CheckAllowed(id.Alias); err != nil {
			release()
			continue
		}
		// Per-catalog plugin allowlist: a cached binary from an allowed catalog
		// must still be one that catalog is permitted to serve. The chain's
		// decorator never sees cache hits, so this is the enforcement point.
		if err := f.catalogIndex.CheckPluginAllowed(id.Alias, dep.ArtifactName()); err != nil {
			release()
			continue
		}
		f.logger.V(1).Info("using cached plugin (no lock file)",
			"name", dep.ArtifactName(),
			"version", cachedVer,
			"path", pinPath,
			"catalog", id.Alias)
		return FetchResult{
			Name:      dep.ArtifactName(),
			Kind:      dep.PluginKind(),
			Version:   cachedVer,
			Path:      pinPath,
			FromCache: true,
			Catalog:   id.Alias,
			Release:   release,
		}, true
	}
	// Also try with zero identity (legacy / no catalog info).
	pinPath, cachedVer, release, ok := f.cache.GetLatestCachedPin(cacheName, f.platform)
	if !ok {
		return FetchResult{}, false
	}
	satisfies, _ := cachedVersionSatisfies(dep.VersionConstraint(), cachedVer)
	if !satisfies {
		release()
		return FetchResult{}, false
	}
	if err := f.catalogIndex.CheckAllowed(""); err != nil {
		release()
		return FetchResult{}, false
	}
	// Per-catalog plugin allowlist. With an unknown origin this rejects when any
	// per-catalog policy is configured, since the plugin cannot be attributed to
	// a permitted catalog.
	if err := f.catalogIndex.CheckPluginAllowed("", dep.ArtifactName()); err != nil {
		release()
		return FetchResult{}, false
	}
	f.logger.V(1).Info("using cached plugin (no lock file, legacy path)",
		"name", dep.ArtifactName(),
		"version", cachedVer,
		"path", pinPath)
	return FetchResult{
		Name:      dep.ArtifactName(),
		Kind:      dep.PluginKind(),
		Version:   cachedVer,
		Path:      pinPath,
		FromCache: true,
		Release:   release,
	}, true
}

// FetchResult contains the result of fetching a single plugin.
type FetchResult struct {
	// Name is the plugin name.
	Name string

	// Kind is the plugin kind.
	Kind solution.PluginKind

	// Version is the resolved version.
	Version string

	// Path is the local filesystem path to the binary.
	Path string

	// Digest is the content digest.
	Digest string

	// FromCache indicates whether the binary was served from cache.
	FromCache bool

	// Signature holds signature verification metadata when verification
	// was performed. Nil when signatures are disabled or the binary was cached.
	//
	// TODO: surface Signature in CLI output/audit log so users can inspect
	// verification results after a fetch.
	Signature *SignatureResult

	// Catalog is the catalog name the plugin was fetched from (empty if cached).
	Catalog string

	// Release releases the pin on the cached binary. Must be called when the
	// binary is no longer in use. Nil when pinning is not active (CLI mode).
	Release func()
}

// FetchPlugins resolves and downloads plugin binaries for all declared
// dependencies. It checks the local cache first, uses lock file entries
// for pinned versions when available, and falls back to catalog resolution.
//
// When a plugin is resolved without a lock file entry, a warning is logged
// about potential reproducibility issues.
//
// Returns a list of FetchResult with local binary paths, suitable for
// passing to RegisterPluginProviders.
func (f *Fetcher) FetchPlugins(ctx context.Context, plugins []solution.PluginDependency, lockPlugins []bundler.LockPlugin) ([]FetchResult, error) {
	return fetchPlugins(ctx, f, plugins, lockPlugins)
}

// fetchPlugins is the generic core of FetchPlugins, mirroring resolvePlugins.
// It filters built-in providers, then fetches every remaining dependency
// concurrently. The lock entry is resolved per-item via findLockPluginByDep
// (which is itself generic over T); everything below fetchOne then operates on
// the resolved *bundler.LockPlugin plus the pluginArtifact view.
func fetchPlugins[T pluginArtifact](ctx context.Context, f *Fetcher, deps []T, lockPlugins []bundler.LockPlugin) ([]FetchResult, error) {
	// Filter out built-in providers — they are compiled into the binary and
	// must not be fetched from catalogs. This is the single enforcement point
	// (analogous to Terraform's BuiltInProviderAvailable check in the
	// provider installer).
	filtered := make([]T, 0, len(deps))
	for _, dep := range deps {
		if dep.PluginKind() == solution.PluginKindProvider && provider.IsBuiltinProvider(dep.LocalName()) {
			f.logger.V(1).Info("skipping builtin provider in plugin fetch", "name", dep.DisplayName())
			continue
		}
		filtered = append(filtered, dep)
	}

	return mapConcurrent(ctx, settings.DefaultFetchConcurrency, filtered,
		func(ctx context.Context, dep T) (FetchResult, error) {
			locked := findLockPluginByDep(lockPlugins, dep)
			result, err := f.fetchOne(ctx, dep, locked)
			if err != nil {
				return FetchResult{}, fmt.Errorf("plugin %s (%s): %w", dep.DisplayName(), dep.PluginKind(), err)
			}
			return result, nil
		})
}

// FetchPlugin resolves and downloads the binary for a single plugin dependency.
// It checks the local cache first, uses a matching lock file entry for a pinned
// version when available, and falls back to catalog resolution.
//
// Built-in providers are compiled into the binary and cannot be fetched; passing
// one returns ErrBuiltinProvider.
func (f *Fetcher) FetchPlugin(ctx context.Context, dep solution.PluginDependency, lockPlugins []bundler.LockPlugin) (FetchResult, error) {
	if dep.Kind == solution.PluginKindProvider && provider.IsBuiltinProvider(dep.LocalName()) {
		return FetchResult{}, fmt.Errorf("plugin %s (%s): %w", dep.DisplayName(), dep.Kind, ErrBuiltinProvider)
	}

	locked := findLockPluginByDep(lockPlugins, dep)
	result, err := f.fetchOne(ctx, dep, locked)
	if err != nil {
		return FetchResult{}, fmt.Errorf("plugin %s (%s): %w", dep.DisplayName(), dep.Kind, err)
	}
	return result, nil
}

// fetchOne resolves and fetches a single plugin dependency. locked is the
// dependency's pre-resolved lock entry (nil when unpinned), supplied by the
// FetchPlugins boundary so the fetch path never re-derives it from the raw dep.
func (f *Fetcher) fetchOne(ctx context.Context, dep pluginArtifact, locked *bundler.LockPlugin) (FetchResult, error) {
	start := time.Now()
	result, err := f.doFetchOne(ctx, dep, locked)
	duration := time.Since(start).Seconds()

	source := "registry"
	if err == nil && result.FromCache {
		source = "cache"
	}

	f.logger.V(1).Info("plugin resolution completed",
		"name", dep.ArtifactName(),
		"source", source,
		"duration_ms", time.Since(start).Milliseconds(),
		"success", err == nil)

	metrics.RecordPluginResolution(ctx, dep.ArtifactName(), source, duration, err == nil)
	return result, err
}

// resolvedPlugin holds the version resolution outcome needed by the shared
// fetch-and-verify path. Both the locked and unlocked resolution strategies
// produce one of these.
type resolvedPlugin struct {
	version        string
	expectedDigest string
	resolvedFrom   string
}

// doFetchOne performs the actual resolution and fetch logic for a single plugin.
// locked is the dependency's pre-resolved lock entry (nil when the dependency is
// not pinned), so this path never re-derives it from the raw dep.
func (f *Fetcher) doFetchOne(ctx context.Context, dep pluginArtifact, locked *bundler.LockPlugin) (FetchResult, error) {
	kind := pluginKindToArtifactKind(dep.PluginKind())

	// Determine whether the lock entry is usable for this dep's current
	// constraint. Three dispositions:
	//   locked.Version == constraint  → strict mode, use lock
	//   locked.Constraint == constraint → constrained mode, re-resolve
	//   neither matches               → stale lock, error
	locked, err := f.disposeLock(dep, locked)
	if err != nil {
		return FetchResult{}, err
	}

	var resolved resolvedPlugin
	if locked != nil {
		r, err := f.resolveFromLock(dep, locked)
		if err != nil {
			return FetchResult{}, err
		}
		resolved = r
	} else {
		earlyResult, r, err := f.resolveFromCatalog(ctx, dep, kind)
		if err != nil {
			return FetchResult{}, err
		}
		if r == nil {
			return earlyResult, nil
		}
		resolved = *r
	}

	return f.fetchAndVerify(ctx, dep, locked, kind, resolved)
}

// ErrLockStale is returned when the lock entry's constraint no longer matches
// the dep's current constraint, indicating the lock file must be regenerated.
var ErrLockStale = errors.New("lock file is out of sync with the current constraint")

// disposeLock classifies a lock entry against the dep's current version
// constraint and returns the appropriate disposition:
//   - locked (non-nil), nil error: strict mode — dep declares the exact pinned version
//   - nil, nil error: constrained/best-effort — re-resolve from catalog
//   - nil, ErrLockStale: constraint changed since lock was created
//
// deps.go already hard-errors for strict/constrained modes before reaching the
// fetcher; the stale error handles the best-effort fallthrough.
func (f *Fetcher) disposeLock(dep pluginArtifact, locked *bundler.LockPlugin) (*bundler.LockPlugin, error) {
	if locked == nil {
		return nil, nil
	}
	constraint := dep.VersionConstraint()
	switch {
	case locked.Version == constraint:
		return locked, nil
	case locked.Constraint == constraint:
		f.logger.V(1).Info("constrained mode: skipping lock entry, re-resolving from catalog",
			"name", dep.ArtifactName(),
			"lockedVersion", locked.Version,
			"constraint", constraint)
		return nil, nil
	default:
		return nil, fmt.Errorf(
			"plugin %s: lock constraint %q does not match current constraint %q; "+
				"run '%s build solution' to refresh the lock: %w",
			dep.ArtifactName(), locked.Constraint, constraint, f.binaryName, ErrLockStale,
		)
	}
}

// resolveFromLock resolves a plugin using its lock file entry. It derives the
// catalog identity, selects the per-platform digest, and enforces allowlists.
func (f *Fetcher) resolveFromLock(dep pluginArtifact, locked *bundler.LockPlugin) (resolvedPlugin, error) {
	version := locked.Version
	resolvedFrom := f.aliasForCanonical(locked.ResolvedCanonical)
	if resolvedFrom == "" {
		resolvedFrom = locked.ResolvedFrom
		f.logger.V(1).Info("lock file catalog alias is stale or missing; using stored alias, which may differ from the current catalog configuration and cause digest verification to fail",
			"name", dep.ArtifactName(),
			"version", version,
			"storedAlias", locked.ResolvedFrom,
			"resolvedCanonical", locked.ResolvedCanonical)
	}

	d, ok := lockDigestForPlatform(locked, f.platform)
	if !ok {
		return resolvedPlugin{}, fmt.Errorf(
			"plugin %s@%s: lock file has no digest for platform %s; "+
				"the plugin does not publish this platform -- run '%s build "+
				"solution' on a host that supports it to refresh the lock",
			dep.ArtifactName(), version, f.platform, f.binaryName,
		)
	}

	f.logger.V(1).Info("using pinned plugin version from lock file",
		"name", dep.ArtifactName(),
		"version", version,
		"digest", d,
		"platform", f.platform)

	if err := f.catalogIndex.CheckAllowed(resolvedFrom); err != nil {
		return resolvedPlugin{}, err
	}
	if err := f.catalogIndex.CheckPluginAllowed(resolvedFrom, dep.ArtifactName()); err != nil {
		return resolvedPlugin{}, err
	}

	return resolvedPlugin{
		version:        version,
		expectedDigest: d,
		resolvedFrom:   resolvedFrom,
	}, nil
}

// resolveFromCatalog resolves a plugin without a lock file. It checks the cache
// first, then resolves the version constraint against the catalog and retrieves
// the content digest. Returns (FetchResult, nil, nil) for an early cache hit,
// or (zero, &resolvedPlugin, nil) when resolution succeeded and a fetch is needed.
func (f *Fetcher) resolveFromCatalog(ctx context.Context, dep pluginArtifact, kind catalog.ArtifactKind) (FetchResult, *resolvedPlugin, error) {
	// Prefer cached version to avoid network latency.
	if !f.noCache {
		if result, ok := f.tryCachedAcrossCatalogs(dep); ok {
			return result, nil, nil
		}
	}

	f.logger.V(0).Info("WARNING: resolving plugin without lock file — version may differ between runs",
		"name", dep.ArtifactName(),
		"constraint", dep.VersionConstraint(),
		"hint", fmt.Sprintf("Run '%s build solution' to pin plugin versions", f.binaryName))

	// Resolve version constraints to a concrete version before calling
	// resolveContentDigest, which only accepts exact versions or "".
	resolvedVersion := ""
	constraint := dep.VersionConstraint()
	if constraint != "" && !strings.EqualFold(constraint, "latest") {
		lister := &catalogLister{fetcher: f.catalogFetcher, catalogName: dep.CatalogName()}
		v, resolveErr := bundler.ResolveVersionConstraint(ctx, lister, kind, dep.ArtifactName(), constraint)
		if resolveErr != nil {
			if !f.noCache {
				if result, ok := f.tryCachedAcrossCatalogs(dep); ok {
					f.logger.V(0).Info("constraint resolution failed, using cached version",
						"name", dep.ArtifactName(),
						"version", result.Version,
						"error", resolveErr)
					return result, nil, nil
				}
			}
			return FetchResult{}, nil, fmt.Errorf("resolving version constraint: %w", resolveErr)
		}
		if v != nil {
			resolvedVersion = v.String()
		}
	}

	// Resolve the content-layer digest using the concrete version.
	// resolveContentDigest selects the correct platform manifest from the
	// OCI image index — this is where platform selection happens.
	mediaType := catalog.MediaTypeForKind(kind)
	cdInfo, err := f.resolveContentDigest(ctx, kind, dep.ArtifactName(), resolvedVersion, f.platform, mediaType, dep.CatalogName())
	if err != nil {
		if !f.noCache {
			if result, ok := f.tryCachedAcrossCatalogs(dep); ok {
				f.logger.V(0).Info("catalog resolution failed, using cached version",
					"name", dep.ArtifactName(),
					"version", result.Version,
					"path", result.Path,
					"error", err)
				return result, nil, nil
			}
		}
		return FetchResult{}, nil, fmt.Errorf("resolving version: %w", err)
	}

	var version string
	if cdInfo.Reference.Version != nil {
		version = cdInfo.Reference.Version.String()
	}
	resolvedFrom := cdInfo.Catalog

	if err := f.catalogIndex.CheckAllowed(resolvedFrom); err != nil {
		return FetchResult{}, nil, err
	}
	if err := f.catalogIndex.CheckPluginAllowed(resolvedFrom, dep.ArtifactName()); err != nil {
		return FetchResult{}, nil, err
	}

	// Defensive post-condition: the resolved version must satisfy the
	// declared constraint.
	if version != "" && constraint != "" && !strings.EqualFold(constraint, "latest") {
		satisfies, chkErr := bundler.CheckVersionConstraint(constraint, version)
		if chkErr != nil {
			return FetchResult{}, nil, fmt.Errorf("checking version constraint: %w", chkErr)
		}
		if !satisfies {
			return FetchResult{}, nil, fmt.Errorf("resolved version %s does not satisfy constraint %s", version, constraint)
		}
	}

	return FetchResult{}, &resolvedPlugin{
		version:        version,
		expectedDigest: cdInfo.ContentDigest,
		resolvedFrom:   resolvedFrom,
	}, nil
}

// fetchAndVerify performs the shared fetch path: checks the local cache,
// downloads the binary, verifies the digest and signature, and writes to cache.
func (f *Fetcher) fetchAndVerify(ctx context.Context, dep pluginArtifact, locked *bundler.LockPlugin, kind catalog.ArtifactKind, resolved resolvedPlugin) (FetchResult, error) {
	version := resolved.version
	expectedDigest := resolved.expectedDigest
	resolvedFrom := resolved.resolvedFrom

	// Derive the registry hash now that we know the origin.
	identity := f.resolveCatalogIdentity(resolvedFrom)
	registryHash := identity.RegistryHash()
	cacheName := PluginCacheKey(dep.ArtifactName(), dep.PluginKind())

	// Check local cache.
	// In enforce mode, cache reads are skipped so every execution performs a
	// fresh fetch with signature verification.
	skipCache := f.noCache || (f.sigPolicy != nil && f.sigPolicy.Mode == SignatureModeEnforce)
	if !skipCache {
		if cachedPath, release, ok := f.cache.GetPin(cacheName, version, f.platform, expectedDigest, WithRegistryHash(registryHash)); ok {
			f.logger.V(1).Info("plugin found in cache",
				"name", dep.ArtifactName(),
				"version", version,
				"path", cachedPath)

			return FetchResult{
				Name:      dep.ArtifactName(),
				Kind:      dep.PluginKind(),
				Version:   version,
				Path:      cachedPath,
				Digest:    expectedDigest,
				FromCache: true,
				Release:   release,
				Catalog:   resolvedFrom,
			}, nil
		}
	}

	// Cache miss — fetch from catalog.
	f.logger.V(1).Info("fetching plugin from catalog",
		"name", dep.ArtifactName(),
		"version", version,
		"platform", f.platform)

	data, fetchInfo, err := f.fetchPlugin(ctx, kind, dep.ArtifactName(), version, f.platform, resolvedFrom)
	if err != nil {
		return FetchResult{}, fmt.Errorf("fetching binary: %w", err)
	}

	// For multi-platform artifacts, FetchPlugin returns the per-platform
	// content digest. Update expectedDigest for the unlocked path.
	if locked == nil && fetchInfo.Digest != "" {
		expectedDigest = fetchInfo.Digest
	}

	// Verify the downloaded binary matches the expected digest.
	if expectedDigest == "" {
		return FetchResult{}, fmt.Errorf(
			"plugin %s@%s: no digest available for verification; "+
				"run '%s build solution' to generate a lock file with pinned digests",
			dep.ArtifactName(), version, f.binaryName,
		)
	}
	actualDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	if actualDigest != expectedDigest {
		return FetchResult{}, fmt.Errorf(
			"plugin binary digest mismatch for %s@%s: expected %s, got %s (possible supply chain attack or corrupted download)",
			dep.ArtifactName(), version, expectedDigest, actualDigest,
		)
	}

	// Signature verification (after digest passes, before caching).
	var sigResult *SignatureResult
	if f.sigPolicy.IsEnabled() && fetchInfo.ImageRef != "" {
		sigResult, err = f.verifySignature(ctx, dep.ArtifactName(), version, fetchInfo.ImageRef)
		if err != nil {
			return FetchResult{}, err
		}
	}

	// Write to cache and pin atomically.
	if resolvedFrom == "" && fetchInfo.Catalog != "" {
		resolvedFrom = fetchInfo.Catalog
		identity = f.resolveCatalogIdentity(resolvedFrom)
		registryHash = identity.RegistryHash()
	}
	cachedPath, release, err := f.cache.SetPin(cacheName, version, f.platform, data, WithRegistryHash(registryHash))
	if err != nil {
		return FetchResult{}, fmt.Errorf("caching binary: %w", err)
	}

	digest := fetchInfo.Digest
	if digest == "" {
		d, err := f.cache.Digest(cacheName, version, f.platform, WithRegistryHash(registryHash))
		if err == nil {
			digest = d
		}
	}

	f.logger.V(1).Info("plugin fetched and cached",
		"name", dep.ArtifactName(),
		"version", version,
		"path", cachedPath,
		"digest", digest,
		"catalog", resolvedFrom)

	return FetchResult{
		Name:      dep.ArtifactName(),
		Kind:      dep.PluginKind(),
		Version:   version,
		Path:      cachedPath,
		Digest:    digest,
		FromCache: false,
		Catalog:   resolvedFrom,
		Signature: sigResult,
		Release:   release,
	}, nil
}

// verifySignature performs Sigstore/cosign signature verification against the
// configured policy. In "warn" mode, failures are logged but do not block
// execution. In "enforce" mode, failures return an error.
func (f *Fetcher) verifySignature(ctx context.Context, name, version, imageRef string) (*SignatureResult, error) {
	f.logger.V(1).Info("verifying plugin signature",
		"name", name,
		"version", version,
		"imageRef", imageRef,
		"mode", string(f.sigPolicy.Mode))

	result, err := f.sigVerifier.VerifySignature(ctx, imageRef, f.sigPolicy)
	if err != nil {
		wrapped := fmt.Errorf("plugin %s@%s: %w", name, version, err)
		return nil, HandleVerificationError(f.sigPolicy, wrapped, f.logger,
			"name", name, "version", version)
	}

	if result != nil && result.Verified {
		f.logger.V(1).Info("plugin signature verified",
			"name", name,
			"version", version,
			"issuer", result.Issuer,
			"identity", result.Identity)
	}

	return result, nil
}

// Paths returns just the binary paths from a slice of FetchResult.
func Paths(results []FetchResult) []string {
	paths := make([]string, 0, len(results))
	for _, r := range results {
		paths = append(paths, r.Path)
	}
	return paths
}

// VersionedClient wraps a running plugin *Client with the resolution metadata
// (version and catalog) that produced it. The embedded *Client exposes all the
// usual client methods, while Version and Catalog record where the binary came
// from so callers can log, audit, or disambiguate multiple versions of the same
// plugin without re-deriving it from a FetchResult.
type VersionedClient struct {
	*Client

	// Version is the resolved plugin version (e.g. "1.2.3").
	Version string

	// Catalog is the catalog name the plugin was fetched from. Empty when the
	// binary was served from cache without catalog metadata.
	Catalog string
}

// NewVersionedClient wraps an existing *Client with its version and catalog
// metadata. It returns nil when client is nil so callers can pass through a
// failed client construction unchanged.
func NewVersionedClient(client *Client, version, catalog string) *VersionedClient {
	if client == nil {
		return nil
	}
	return &VersionedClient{
		Client:  client,
		Version: version,
		Catalog: catalog,
	}
}

// RegisterFetchedPlugins loads and registers fetched plugin binaries into
// the provider registry. Unlike RegisterPluginProviders (which discovers
// plugins from directories), this loads specific binaries by path.
// Returns the created clients (caller should Kill() them on cleanup).
func RegisterFetchedPlugins(ctx context.Context, registry *provider.Registry, results []FetchResult, cfg *ProviderConfig, clientOpts ...ClientOption) ([]*Client, error) {
	var clients []*Client

	for _, r := range results {
		if r.Kind != solution.PluginKindProvider {
			// Non-provider plugins are handled by RegisterFetchedAuthHandlerPlugins.
			continue
		}

		client, err := NewClient(r.Path, clientOpts...)
		if err != nil {
			// Kill any clients we've already started
			for _, c := range clients {
				c.Kill()
			}
			return nil, fmt.Errorf("loading plugin %s from %s: %w", r.Name, r.Path, err)
		}

		providers, err := client.GetProviders(ctx)
		if err != nil {
			client.Kill()
			for _, c := range clients {
				c.Kill()
			}
			return nil, fmt.Errorf("getting providers from plugin %s: %w", r.Name, err)
		}

		for _, providerName := range providers {
			wrapper, err := NewProviderWrapper(client, providerName, WithContext(ctx))
			if err != nil {
				lgr := logr.FromContextOrDiscard(ctx)
				lgr.V(1).Info("failed to create plugin provider wrapper",
					"plugin", r.Name,
					"provider", providerName,
					"error", err)
				continue
			}
			if err := registry.Register(wrapper); err != nil {
				lgr := logr.FromContextOrDiscard(ctx)
				lgr.V(0).Info("WARNING: plugin provider not registered (name already taken by a builtin or another plugin)",
					"plugin", r.Name,
					"provider", providerName,
					"error", err)
				continue
			}
			if cfg != nil {
				if err := wrapper.Configure(ctx, *cfg); err != nil {
					lgr := logr.FromContextOrDiscard(ctx)
					lgr.V(1).Info("failed to configure plugin provider",
						"provider", providerName,
						"error", err)
				}
			}
		}

		clients = append(clients, client)
	}

	return clients, nil
}

// ClientFactory constructs a plugin *Client from a binary path. Its signature
// matches NewClient, so NewClient can be passed directly; tests can inject a
// fake that avoids spawning a real subprocess.
type ClientFactory func(pluginPath string, opts ...ClientOption) (*Client, error)

// WrapperFactory constructs a *ProviderWrapper for a named provider on a client.
// Its signature matches NewProviderWrapper, so NewProviderWrapper can be passed
// directly; tests can inject a fake that avoids the descriptor RPC.
type WrapperFactory func(client *Client, providerName string, opts ...WrapperOption) (*ProviderWrapper, error)

// RegisterFetchedVersionedPlugins loads and registers fetched provider plugin binaries
// into the registry, returning a VersionedClient per started plugin. Each
// client's Version and Catalog come from its FetchResult.
//
// The client and wrapper constructors are injected via factories so callers
// (and tests) can substitute alternate implementations. Nil factories fall back
// to NewClient and NewProviderWrapper. As with RegisterFetchedPlugins, the
// caller should Kill() the returned clients on cleanup.
func RegisterFetchedVersionedPlugins(
	ctx context.Context,
	registry interface {
		RegisterExternal(provider provider.Provider, opts ...provider.VersionedRegistryOptionFunc) error
	},
	results []FetchResult,
	cfg *ProviderConfig,
	newClient ClientFactory,
	newWrapper WrapperFactory,
	clientOpts ...ClientOption,
) ([]*VersionedClient, error) {
	if newClient == nil {
		newClient = NewClient
	}
	if newWrapper == nil {
		newWrapper = NewProviderWrapper
	}

	var clients []*VersionedClient

	killAll := func() {
		for _, c := range clients {
			c.Kill()
		}
	}

	for _, r := range results {
		if r.Kind != solution.PluginKindProvider {
			// Non-provider plugins are handled by RegisterFetchedAuthHandlerPlugins.
			continue
		}

		client, err := newClient(r.Path, clientOpts...)
		if err != nil {
			killAll()
			return nil, fmt.Errorf("loading plugin %s from %s: %w", r.Name, r.Path, err)
		}

		providers, err := client.GetProviders(ctx)
		if err != nil {
			client.Kill()
			killAll()
			return nil, fmt.Errorf("getting providers from plugin %s: %w", r.Name, err)
		}

		regVersion, verr := semver.NewVersion(r.Version)
		if verr != nil {
			lgr := logr.FromContextOrDiscard(ctx)
			lgr.V(0).Info("WARNING: skipping plugin: unparseable version",
				"plugin", r.Name, "version", r.Version, "error", verr)
			client.Kill()
			continue
		}

		for _, providerName := range providers {
			wrapper, err := newWrapper(client, providerName, WithContext(ctx))
			if err != nil {
				lgr := logr.FromContextOrDiscard(ctx)
				lgr.V(1).Info("failed to create plugin provider wrapper",
					"plugin", r.Name,
					"provider", providerName,
					"error", err)
				continue
			}
			if err := registry.RegisterExternal(wrapper,
				provider.WithCatalogName(r.Catalog),
				provider.WithRegistrationVersion(regVersion),
			); err != nil {
				lgr := logr.FromContextOrDiscard(ctx)
				lgr.V(0).Info("WARNING: plugin provider not registered (name already taken by a builtin or another plugin)",
					"plugin", r.Name,
					"provider", providerName,
					"error", err)
				continue
			}
			if cfg != nil {
				if err := wrapper.Configure(ctx, *cfg); err != nil {
					lgr := logr.FromContextOrDiscard(ctx)
					lgr.V(1).Info("failed to configure plugin provider",
						"provider", providerName,
						"error", err)
				}
			}
		}

		clients = append(clients, NewVersionedClient(client, r.Version, r.Catalog))
	}

	return clients, nil
}

// RegisterFetchedAuthHandlerPlugins loads and registers fetched auth handler
// plugin binaries into the auth registry. Returns the created clients
// (caller should Kill() them on cleanup).
func RegisterFetchedAuthHandlerPlugins(ctx context.Context, registry *auth.Registry, results []FetchResult, cfg *ProviderConfig, clientOpts ...ClientOption) ([]*AuthHandlerClient, error) {
	var clients []*AuthHandlerClient

	for _, r := range results {
		if r.Kind != solution.PluginKindAuthHandler {
			continue
		}

		client, err := NewAuthHandlerClient(r.Path, clientOpts...)
		if err != nil {
			for _, c := range clients {
				c.Kill()
			}
			return nil, fmt.Errorf("loading auth handler plugin %s from %s: %w", r.Name, r.Path, err)
		}

		handlers, err := client.GetAuthHandlers(ctx)
		if err != nil {
			client.Kill()
			for _, c := range clients {
				c.Kill()
			}
			return nil, fmt.Errorf("getting auth handlers from plugin %s: %w", r.Name, err)
		}

		registered := configureAndRegisterAuthHandlers(ctx, registry, client, handlers, cfg)
		if len(registered) == 0 {
			// This plugin registered no new handlers -- every name it exposes
			// was already present in the registry. Kill the just-started process
			// immediately so long-lived servers that prepare the same bundle
			// repeatedly do not leak plugin subprocesses.
			client.Kill()
			continue
		}
		propagateStartupLatency(ctx, registry, client, registered)

		clients = append(clients, client)
	}

	return clients, nil
}

// pluginKindToArtifactKind converts solution.PluginKind to catalog.ArtifactKind.
func pluginKindToArtifactKind(kind solution.PluginKind) catalog.ArtifactKind {
	switch kind {
	case solution.PluginKindProvider:
		return catalog.ArtifactKindProvider
	case solution.PluginKindAuthHandler:
		return catalog.ArtifactKindAuthHandler
	default:
		return catalog.ArtifactKind(string(kind))
	}
}

// PluginCacheKey returns the directory name for a plugin in the cache.
// Provider plugins use the bare name; auth-handler plugins are prefixed
// with "auth-handler-". This is the name component only (no registry hash).
func PluginCacheKey(name string, kind solution.PluginKind) string {
	if kind == solution.PluginKindAuthHandler {
		return "auth-handler-" + name
	}
	return name
}

// PluginCachePath builds the relative cache path for a plugin binary directory.
// When registryHash is empty, the registry-hash directory level is omitted.
func PluginCachePath(name string, kind solution.PluginKind, registryHash, version, platform string) string {
	cacheName := PluginCacheKey(name, kind)
	if registryHash != "" {
		return filepath.Join(cacheName, registryHash, version, PlatformCacheKey(platform))
	}
	return filepath.Join(cacheName, version, PlatformCacheKey(platform))
}

// RegisterCachedPlugin looks up a provider plugin by name in the local cache,
// starts it, and registers its providers into the given registry.
// The name should be the cache name (e.g. "aws-provider" or "auth-handler-github").
// Returns the created clients (caller must Kill them on cleanup) or an error
// if the plugin is not cached.
func RegisterCachedPlugin(ctx context.Context, name string, registry *provider.Registry, cfg *ProviderConfig, cacheDir string, clientOpts ...ClientOption) ([]*Client, error) {
	cache := NewCache(cacheDir)
	path, version, ok := cache.GetLatestBinary(name)
	if !ok {
		return nil, fmt.Errorf("plugin %q not found in cache", name)
	}

	results := []FetchResult{{
		Name:      name,
		Kind:      solution.PluginKindProvider,
		Version:   version,
		Path:      path,
		FromCache: true,
	}}

	return RegisterFetchedPlugins(ctx, registry, results, cfg, clientOpts...)
}

// RegisterCachedPluginVersion loads a specific version of a cached plugin into
// the registry. Returns an error if that exact version is not cached.
// The name should be the cache name (e.g. "aws-provider").
func RegisterCachedPluginVersion(ctx context.Context, name, version string, registry *provider.Registry, cfg *ProviderConfig, cacheDir string, clientOpts ...ClientOption) ([]*Client, error) {
	cache := NewCache(cacheDir)
	platform := CurrentPlatform()
	// ResolveVersion searches both the flat (no-registry) layout and every
	// registry-hash layout so catalog-installed plugins are found even when the
	// caller does not know the plugin's catalog registry hash.
	path, ok, err := cache.ResolveVersion(name, version, platform)
	if err != nil {
		return nil, fmt.Errorf("resolving cached plugin %q version %q: %w", name, version, err)
	}
	if !ok {
		return nil, fmt.Errorf("plugin %q version %q not found in cache (platform: %s)", name, version, platform)
	}

	results := []FetchResult{{
		Name:      name,
		Kind:      solution.PluginKindProvider,
		Version:   version,
		Path:      path,
		FromCache: true,
	}}

	return RegisterFetchedPlugins(ctx, registry, results, cfg, clientOpts...)
}

// findLockPlugin looks up a lock plugin entry by name and kind.
func findLockPlugin(plugins []bundler.LockPlugin, name, kind string) *bundler.LockPlugin {
	for i := range plugins {
		if plugins[i].Name == name && plugins[i].Kind == kind {
			return &plugins[i]
		}
	}
	return nil
}

func findLockPluginByDep[T pluginArtifact](plugins []bundler.LockPlugin, dep T) *bundler.LockPlugin {
	return bundler.FindLockPluginByDep(&bundler.LockFile{Plugins: plugins}, dep)
}

// lockDigestForPlatform selects the content digest a locked plugin must verify
// against for the target platform.
//
// It relies on the LockPlugin.Digests invariant: the map is populated ONLY for
// genuine multi-platform (OCI image index) plugins, and is empty for
// single-platform plugins (whose sole digest lives in the primary Digest) and
// for legacy locks predating per-platform digests.
//
//   - Empty Digests: one binary for every os/arch, so the primary Digest is
//     used regardless of platform. It is returned as-is (ok=true) even when
//     empty, so a digest-less-but-cached entry still resolves from cache and an
//     uncached one hits the existing "no digest available" guard downstream --
//     preserving prior behavior for legacy locks.
//   - Populated Digests: an exact per-platform match is required. ok=false when
//     the platform is absent, meaning the plugin genuinely does not publish it,
//     so the caller must not fall back to a wrong-platform digest (which would
//     surface later as a bogus "digest mismatch / supply chain attack").
func lockDigestForPlatform(locked *bundler.LockPlugin, platform string) (string, bool) {
	if len(locked.Digests) == 0 {
		return locked.Digest, true
	}
	d, ok := locked.Digests[platform]
	return d, ok
}

// cachedVersionSatisfies checks whether a cached version satisfies a version
// constraint. Returns true when no constraint is specified or the constraint is
// "latest". Returns false with a nil error when the constraint is simply not
// satisfied. Returns false with a non-nil error when the constraint or cached
// version string cannot be parsed.
func cachedVersionSatisfies(constraint, cachedVer string) (bool, error) {
	if constraint == "" || strings.EqualFold(constraint, "latest") {
		return true, nil
	}
	return bundler.CheckVersionConstraint(constraint, cachedVer)
}
