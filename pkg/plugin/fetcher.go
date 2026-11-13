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
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

var ErrDedupTimeout = errors.New("operation timed out while waiting for concurrent request")

// ErrBuiltinProvider is returned when a fetch is requested for a built-in
// provider, which is compiled into the binary and cannot be fetched from a
// catalog.
var ErrBuiltinProvider = errors.New("built-in provider cannot be fetched from a catalog")

// ErrCatalogConflict is returned when a plugin dependency's source requires one
// catalog but the dependency is already bound to another, or its source names
// both a registry and the locally built copy.
var ErrCatalogConflict = errors.New("plugin dependency is bound to a catalog its source does not allow")

// ErrUnconfiguredRegistry is returned when a source.registry plugin dependency
// names a registry that is not one of the fetcher's configured catalogs. It is
// rejected rather than resolved by name from an unintended catalog.
var ErrUnconfiguredRegistry = errors.New("plugin registry is not a configured catalog")

// ErrLocalBuildNotAllowed is returned when a fetcher configured with
// DisallowLocalBuilds is asked for a locally built plugin: a source.built
// dependency, a lock entry locked from a local build, or a resolution that
// lands on a local-catalog artifact with no published origin.
var ErrLocalBuildNotAllowed = errors.New("locally built plugins are not allowed")

type resolver interface {
	bindCatalog(dep pluginArtifact) (pluginArtifact, error)
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
	HasBuilt() bool
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
//
// Sourced deps are bound to the catalog their source requires before any
// cache, lock, or catalog lookup (see bindCatalog): source.built to the local
// catalog, source.registry to the configured catalog for that registry.
type Fetcher struct {
	binaryName     string
	catalogFetcher CatalogFetcher
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
	// disallowLocalBuilds rejects every plugin that would be served from a
	// local build (see FetcherConfig.DisallowLocalBuilds).
	disallowLocalBuilds bool
	// tracerProvider is the source of plugin.fetch spans; nil uses the
	// global provider.
	tracerProvider trace.TracerProvider
}
type pluginFetchResult struct {
	bytes []byte
	info  catalog.ArtifactInfo
}

// CatalogFetcher resolves and fetches plugin artifacts from catalogs. Every
// method honors catalog.WithCatalog to scope the call to one named catalog.
// catalog.BuildPluginFetcher returns the standard implementation.
type CatalogFetcher interface {
	ResolvePlugin(ctx context.Context, name string, kind catalog.ArtifactKind, versionConstraint string, opts ...catalog.Option) (catalog.ArtifactInfo, error)
	FetchPlugin(ctx context.Context, name string, kind catalog.ArtifactKind, version, platform string, opts ...catalog.Option) ([]byte, catalog.ArtifactInfo, error)
	ResolveContentDigest(ctx context.Context, name string, kind catalog.ArtifactKind, version, platform, mediaType string, opts ...catalog.Option) (catalog.ContentDigestInfo, error)
}

// catalogsLister is implemented by CatalogFetchers that can report the
// catalogs they read from, so the Fetcher can derive its catalog index.
type catalogsLister interface {
	Catalogs() []catalog.Catalog
}

var (
	_ CatalogFetcher = (*catalog.PluginFetcher)(nil)
	_ catalogsLister = (*catalog.PluginFetcher)(nil)
)

// FetcherConfig configures a Fetcher.
type FetcherConfig struct {
	// CatalogFetcher resolves and fetches plugins from catalogs. When it also
	// implements Catalogs() []catalog.CatalogFetcher, those catalogs define the
	// catalog index (alias <-> canonical origin).
	CatalogFetcher CatalogFetcher

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

	// DisallowLocalBuilds rejects plugins served from a local build: artifacts
	// in a local catalog with no published (remote) origin. Plugins resolved
	// from a remote catalog, including its locally cached copies, remain
	// allowed. Long-lived hosts such as the API server set it so they only run
	// published plugins, never whatever happens to be built on the host. Fails
	// with ErrLocalBuildNotAllowed.
	DisallowLocalBuilds bool

	// TracerProvider is the source of plugin.fetch spans. When nil, the global
	// OpenTelemetry provider is used.
	TracerProvider trace.TracerProvider

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

	// Build the catalog topology from the fetcher's own catalogs so the index
	// describes exactly the catalogs this fetcher can reach, then layer the
	// allowlist and per-catalog plugin policy gates on top (copy-on-write,
	// leaving the base topology shared).
	var cats []catalog.Catalog
	if lister, ok := cfg.CatalogFetcher.(catalogsLister); ok {
		cats = lister.Catalogs()
	}
	catalogIndex := catalogindex.FromCatalogs(cats).
		WithAllowed(cfg.AllowedCatalogs).
		WithPluginPolicies(cfg.PerCatalogArtifacts)

	return &Fetcher{
		binaryName:     binaryName,
		catalogFetcher: cfg.CatalogFetcher,
		cache:          cache,
		platform:       platform,
		noCache:        cfg.NoCache,
		logger:         cfg.Logger.WithName("plugin-fetcher"),
		sigPolicy:      cfg.SignaturePolicy,
		sigVerifier:    sigVerifier,
		catalogIndex:   catalogIndex,
		tracerProvider: cfg.TracerProvider,

		disallowLocalBuilds: cfg.DisallowLocalBuilds,
	}
}

// errLocalBuild is the error returned for a local build when
// disallowLocalBuilds is set.
func errLocalBuild(reason string) error {
	return fmt.Errorf("%s; publish the plugin to a registry and reference it from there: %w", reason, ErrLocalBuildNotAllowed)
}

// checkLocalBuild rejects a resolved artifact that is a local build when
// disallowLocalBuilds is set. A local build has no published origin; remote
// catalogs (and their cached copies) always report one. An artifact from an
// unknown catalog with no origin is treated as a local build (fail closed).
func (f *Fetcher) checkLocalBuild(info catalog.ArtifactInfo) error {
	if !f.disallowLocalBuilds || info.Canonical != "" || f.isConfiguredRemote(info.Catalog) {
		return nil
	}
	return errLocalBuild(fmt.Sprintf("resolved to a local build in catalog %q", info.Catalog))
}

// isConfiguredRemote reports whether catalogName is the alias of a configured
// remote catalog. An unknown or empty alias is not (a bare identity derived
// for an unknown alias defaults to the remote kind, so it cannot be used).
func (f *Fetcher) isConfiguredRemote(catalogName string) bool {
	id, ok := f.catalogIndex.IdentityForAlias(catalogName)
	return ok && id.IsRemote()
}

// checkLocalSource rejects serving a plugin from a configured local catalog
// when disallowLocalBuilds is set. The plugin fetcher serves only built
// artifacts from a local catalog, so the catalog alone decides it, whatever a
// lock entry claims about the plugin's origin. The local catalog's alias is
// reserved, so it is rejected even where no local catalog is configured (e.g. a
// server), instead of failing later as an unconfigured catalog.
func (f *Fetcher) checkLocalSource(dep pluginArtifact, catalogName string) error {
	if !f.disallowLocalBuilds {
		return nil
	}
	if id, ok := f.catalogIndex.IdentityForAlias(catalogName); strings.EqualFold(catalogName, catalog.LocalCatalogName) || (ok && id.IsLocal()) {
		return errLocalBuild(fmt.Sprintf("plugin %s would be served from local catalog %q", dep.ArtifactName(), catalogName))
	}
	return nil
}

// errUnverifiable is the error for a fetched plugin that has no registry image
// to verify a signature against. Signature verification fails closed on it.
//
// TODO(built-plugins): exempt local builds (no Canonical) once 'catalog load'
// can no longer import origin-less archives that look like local builds; until
// then an exemption would let any loaded tarball skip enforcement.
func errUnverifiable(dep pluginArtifact, version string, info catalog.ArtifactInfo) error {
	if info.Canonical == "" {
		return fmt.Errorf("plugin %s@%s from catalog %q has no published origin (as for a local build), so it has no registry signature: %w",
			dep.ArtifactName(), version, info.Catalog, ErrSignatureUnverifiable)
	}
	return fmt.Errorf("plugin %s@%s from catalog %q: %w",
		dep.ArtifactName(), version, info.Catalog, ErrSignatureUnverifiable)
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
			bound, err := s.bindCatalog(dep)
			if err != nil {
				return catalog.ArtifactInfo{}, fmt.Errorf("plugin %s (%s): %w", dep.DisplayName(), dep.PluginKind(), err)
			}
			return s.resolvePlugin(ctx, catalog.ArtifactKind(bound.PluginKind()),
				bound.ArtifactName(), bound.VersionConstraint(), bound.CatalogName())
		})
	if err != nil {
		// Preserve the existing contract: return nil (not partial) on error.
		return nil, err
	}
	return infos, nil
}

// boundArtifact overrides a dependency's catalog with the one it must be
// served from, so every downstream lookup sees it through CatalogName().
type boundArtifact struct {
	pluginArtifact
	catalog string
}

func (b boundArtifact) CatalogName() string { return b.catalog }

// bindCatalog returns dep scoped to the catalog its source requires.
// source.built deps are bound to the local catalog, the only catalog that
// serves built artifacts; source.registry deps are bound to the configured
// catalog for that registry, and an unconfigured registry fails closed. A dep
// already bound to a different catalog, or whose source names both a registry
// and built, fails closed. Short-name deps are returned unchanged.
func (f *Fetcher) bindCatalog(dep pluginArtifact) (pluginArtifact, error) {
	var target string
	switch {
	case dep.HasBuilt() && dep.HasRegistry():
		return nil, fmt.Errorf("source names both a registry and built: %w", ErrCatalogConflict)
	case dep.HasBuilt():
		if f.disallowLocalBuilds {
			return nil, errLocalBuild("source.built plugins are local builds")
		}
		target = catalog.LocalCatalogName
	case dep.HasRegistry():
		alias, ok := f.catalogIndex.AliasForRegistry(dep.Registry())
		if !ok {
			return nil, fmt.Errorf("registry %q: %w", dep.Registry(), ErrUnconfiguredRegistry)
		}
		target = alias
	default:
		if err := f.checkLocalSource(dep, dep.CatalogName()); err != nil {
			return nil, err
		}
		return dep, nil
	}
	if c := dep.CatalogName(); c != "" && !strings.EqualFold(c, target) {
		return nil, fmt.Errorf("bound to catalog %q but its source requires catalog %q: %w", c, target, ErrCatalogConflict)
	}
	return boundArtifact{pluginArtifact: dep, catalog: target}, nil
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
	info, err := s.resolveCatalogPlugin(ctx, kind, name, version, catalogName)
	if err != nil {
		return catalog.ArtifactInfo{}, err
	}
	if err := s.checkLocalBuild(info); err != nil {
		return catalog.ArtifactInfo{}, err
	}
	return info, nil
}

// resolveCatalogPlugin resolves a version selector against the catalogs,
// optionally scoped to one named catalog, without applying catalog policy.
// Concurrent identical requests are deduplicated.
func (s *Fetcher) resolveCatalogPlugin(ctx context.Context, kind catalog.ArtifactKind, name, version, catalogName string) (catalog.ArtifactInfo, error) {
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
// When the dep names a catalog, only that catalog's cache is consulted; the
// legacy flat layout (unknown origin) is never used for a scoped dep.
//
// Filesystem catalogs (the local catalog of built plugins) are skipped: their
// artifacts can be rebuilt in place at the same version, so the newest cached
// binary may be stale. Those deps resolve their digest from disk (no network)
// and fetchAndVerify's digest-checked cache lookup still reuses a matching
// binary.
func (f *Fetcher) tryCachedAcrossCatalogs(dep pluginArtifact) (FetchResult, bool) {
	cacheName := PluginCacheKey(dep.ArtifactName(), dep.PluginKind())
	catalogName := dep.CatalogName()
	for _, id := range f.catalogIndex.All() {
		if id.IsLocal() {
			continue
		}
		if catalogName != "" && !strings.EqualFold(catalogName, id.Alias) {
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
	// Legacy / no-catalog-info pass. Entries in the flat (no registry hash)
	// layout have an unknown origin, so they can only satisfy an unscoped dep.
	// A dep bound to a catalog (registry alias, or local for built plugins)
	// must be served from that catalog's cache or fail closed via resolution.
	// An unknown origin cannot be proven not to be a local build either.
	if catalogName != "" || f.disallowLocalBuilds {
		return FetchResult{}, false
	}
	pinPath, cachedVer, release, ok := f.cache.GetLatestCachedPin(cacheName, f.platform, WithFlatLayoutOnly())
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
func (f *Fetcher) fetchOne(ctx context.Context, dep pluginArtifact, locked *bundler.LockPlugin) (result FetchResult, err error) {
	ctx, span := pluginTracer(f.tracerProvider).Start(ctx, spanFetch, trace.WithAttributes(
		attrName.String(dep.ArtifactName()),
		attrCatalog.String(dep.CatalogName()),
		attrVersionConstraint.String(dep.VersionConstraint()),
	))
	defer func() { endSpan(span, err) }()

	start := time.Now()
	result, err = f.doFetchOne(ctx, dep, locked)
	duration := time.Since(start).Seconds()

	source := fetchSourceRegistry
	if err == nil && result.FromCache {
		source = fetchSourceCache
	}
	span.SetAttributes(attrSource.String(source))
	if err == nil {
		span.SetAttributes(attrVersion.String(result.Version))
		if result.Catalog != "" {
			span.SetAttributes(attrCatalog.String(result.Catalog))
		}
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
	// Bind sourced deps to their required catalog before any cache, lock, or
	// catalog lookup, so a same-named artifact from another catalog is never
	// served.
	//
	// TODO(built-plugins): a bundle run on a machine without the local build
	// fails with a plain not-found from the local catalog; decide whether to
	// embed built binaries in the bundle or surface a dedicated error.
	dep, err := f.bindCatalog(dep)
	if err != nil {
		return FetchResult{}, withStage(StageBind, err)
	}
	kind := pluginKindToArtifactKind(dep.PluginKind())

	// Determine whether the lock entry is usable for this dep's current
	// constraint. Three dispositions:
	//   locked.Version == constraint  → strict mode, use lock
	//   locked.Constraint == constraint → constrained mode, re-resolve
	//   neither matches               → stale lock, error
	locked, err = f.disposeLock(dep, locked)
	if err != nil {
		return FetchResult{}, withStage(StageLock, err)
	}
	trace.SpanFromContext(ctx).SetAttributes(attrFromLock.Bool(locked != nil))

	var resolved resolvedPlugin
	if locked != nil {
		r, err := f.resolveFromLock(dep, locked)
		if err != nil {
			return FetchResult{}, withStage(StageResolve, err)
		}
		resolved = r
	} else {
		earlyResult, r, err := f.resolveFromCatalog(ctx, dep, kind)
		if err != nil {
			return FetchResult{}, withStage(StageResolve, err)
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
	if f.disallowLocalBuilds && locked.IsBuilt() {
		return resolvedPlugin{}, errLocalBuild("the lock entry was locked from a local build")
	}
	version := locked.Version
	// A built entry has no portable origin; it is served by the local catalog
	// by definition.
	resolvedFrom := catalog.LocalCatalogName
	if !locked.IsBuilt() {
		resolvedFrom = f.aliasForCanonical(locked.ResolvedCanonical)
	}
	if resolvedFrom == "" {
		resolvedFrom = locked.ResolvedFrom
		f.logger.V(1).Info("lock file catalog alias is stale or missing; using stored alias, which may differ from the current catalog configuration and cause digest verification to fail",
			"name", dep.ArtifactName(),
			"version", version,
			"storedAlias", locked.ResolvedFrom,
			"resolvedCanonical", locked.ResolvedCanonical)
	}
	// A dep bound to a catalog must be served from it; a lock entry recorded
	// from another catalog (e.g. the source changed since the build) is stale.
	if bound := dep.CatalogName(); bound != "" && !strings.EqualFold(bound, resolvedFrom) {
		return resolvedPlugin{}, fmt.Errorf(
			"plugin %s@%s: lock entry was resolved from catalog %q but the dependency requires catalog %q; "+
				"run '%s build solution' to refresh the lock: %w",
			dep.ArtifactName(), version, resolvedFrom, bound, f.binaryName, ErrLockStale,
		)
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

// cacheReadsAllowed reports whether cached plugin binaries may be served. In
// enforce mode, cache reads are skipped so every execution performs a fresh
// fetch with signature verification.
func (f *Fetcher) cacheReadsAllowed() bool {
	return !f.noCache && (f.sigPolicy == nil || f.sigPolicy.Mode != SignatureModeEnforce)
}

// localBuildShadows reports whether resolving an unscoped dep could select a
// local build, which outranks every remote, so a cached remote copy must not
// short-circuit resolution. The probe reads only the local catalog (no
// network). A dep bound to a catalog is never shadowed. Any probe failure other
// than not-found also reports true, leaving full resolution to surface it.
func (f *Fetcher) localBuildShadows(ctx context.Context, dep pluginArtifact, kind catalog.ArtifactKind) bool {
	if dep.CatalogName() != "" {
		return false
	}
	if id, ok := f.catalogIndex.IdentityForAlias(catalog.LocalCatalogName); !ok || !id.IsLocal() {
		return false
	}
	_, err := f.resolveCatalogPlugin(ctx, kind, dep.ArtifactName(), dep.VersionConstraint(), catalog.LocalCatalogName)
	return !errors.Is(err, catalog.ErrArtifactNotFound)
}

// resolveFromCatalog resolves a plugin without a lock file. It checks the cache
// first, then resolves the version constraint against the catalog and retrieves
// the content digest. Returns (FetchResult, nil, nil) for an early cache hit,
// or (zero, &resolvedPlugin, nil) when resolution succeeded and a fetch is needed.
func (f *Fetcher) resolveFromCatalog(ctx context.Context, dep pluginArtifact, kind catalog.ArtifactKind) (FetchResult, *resolvedPlugin, error) {
	// Prefer cached version to avoid network latency. The shortcut only covers
	// remote catalogs; see tryCachedAcrossCatalogs. Enforce mode disables it
	// (including the offline fallback below) so every binary is signature-checked.
	useCache := f.cacheReadsAllowed()
	if useCache && !f.localBuildShadows(ctx, dep, kind) {
		if result, ok := f.tryCachedAcrossCatalogs(dep); ok {
			return result, nil, nil
		}
	}

	f.logger.V(0).Info("WARNING: resolving plugin without lock file — version may differ between runs",
		"name", dep.ArtifactName(),
		"constraint", dep.VersionConstraint(),
		"hint", fmt.Sprintf("Run '%s build solution' to pin plugin versions", f.binaryName))

	// Resolve the version selector (exact, constraint, or ""/"latest") to a
	// concrete artifact, then resolve its content-layer digest from the same
	// catalog. resolveContentDigest selects the correct platform manifest from
	// the OCI image index -- this is where platform selection happens.
	constraint := dep.VersionConstraint()
	cdInfo, err := f.resolveCatalogDigest(ctx, dep, kind)
	if err != nil {
		if useCache {
			if result, ok := f.tryCachedAcrossCatalogs(dep); ok {
				f.logger.V(0).Info("catalog resolution failed, using cached version",
					"name", dep.ArtifactName(),
					"version", result.Version,
					"path", result.Path,
					"error", err)
				return result, nil, nil
			}
		}
		return FetchResult{}, nil, err
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

// resolveCatalogDigest resolves dep's version selector to a concrete artifact
// (scoped to dep's catalog when named) and then resolves that artifact's
// content-layer digest for the target platform from the catalog that served
// it, so the version and digest always come from the same source.
func (f *Fetcher) resolveCatalogDigest(ctx context.Context, dep pluginArtifact, kind catalog.ArtifactKind) (catalog.ContentDigestInfo, error) {
	info, err := f.resolveCatalogPlugin(ctx, kind, dep.ArtifactName(), dep.VersionConstraint(), dep.CatalogName())
	if err != nil {
		return catalog.ContentDigestInfo{}, fmt.Errorf("resolving version: %w", err)
	}
	if err := f.checkLocalBuild(info); err != nil {
		return catalog.ContentDigestInfo{}, err
	}
	var version string
	if info.Reference.Version != nil {
		version = info.Reference.Version.String()
	}

	mediaType := catalog.MediaTypeForKind(kind)
	cdInfo, err := f.resolveContentDigest(ctx, kind, dep.ArtifactName(), version, f.platform, mediaType, info.Catalog)
	if err != nil {
		return catalog.ContentDigestInfo{}, fmt.Errorf("resolving content digest: %w", err)
	}
	return cdInfo, nil
}

// fetchAndVerify performs the shared fetch path: checks the local cache,
// downloads the binary, verifies the digest and signature, and writes to cache.
func (f *Fetcher) fetchAndVerify(ctx context.Context, dep pluginArtifact, locked *bundler.LockPlugin, kind catalog.ArtifactKind, resolved resolvedPlugin) (FetchResult, error) {
	version := resolved.version
	expectedDigest := resolved.expectedDigest
	resolvedFrom := resolved.resolvedFrom

	// Record the resolved artifact before fetching so failed acquisitions
	// can be filtered by the version and catalog that failed.
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attrVersion.String(version))
	if resolvedFrom != "" {
		span.SetAttributes(attrCatalog.String(resolvedFrom))
	}

	if err := f.checkLocalSource(dep, resolvedFrom); err != nil {
		return FetchResult{}, withStage(StageBind, err)
	}

	// Derive the registry hash now that we know the origin.
	identity := f.resolveCatalogIdentity(resolvedFrom)
	registryHash := identity.RegistryHash()
	cacheName := PluginCacheKey(dep.ArtifactName(), dep.PluginKind())

	// Check local cache. A cache entry carries no provenance of its own, so
	// with disallowLocalBuilds only one attributable to a configured remote
	// catalog may be served: an empty or unknown origin (e.g. a crafted lock
	// entry) cannot prove it is not a local build, and the expected digest
	// comes from that same lock entry. Such a plugin is fetched instead, and
	// checkLocalBuild judges what the catalog actually returns.
	if f.cacheReadsAllowed() && (!f.disallowLocalBuilds || f.isConfiguredRemote(resolvedFrom)) {
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
		return FetchResult{}, withStage(StageDownload, fmt.Errorf("fetching binary: %w", err))
	}
	if err := f.checkLocalBuild(fetchInfo); err != nil {
		return FetchResult{}, withStage(StageBind, err)
	}

	// For multi-platform artifacts, FetchPlugin returns the per-platform
	// content digest. Update expectedDigest for the unlocked path.
	if locked == nil && fetchInfo.Digest != "" {
		expectedDigest = fetchInfo.Digest
	}

	// Verify the downloaded binary matches the expected digest.
	if expectedDigest == "" {
		return FetchResult{}, withStage(StageDigest, fmt.Errorf(
			"plugin %s@%s: no digest available for verification; "+
				"run '%s build solution' to generate a lock file with pinned digests",
			dep.ArtifactName(), version, f.binaryName,
		))
	}
	actualDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	if actualDigest != expectedDigest && locked != nil && (dep.HasBuilt() || locked.IsBuilt()) {
		return FetchResult{}, withStage(StageDigest, fmt.Errorf(
			"built plugin %s@%s does not match the lock file: expected %s, got %s; "+
				"it was rebuilt after the lock was written -- run '%s build solution' to refresh the lock",
			dep.ArtifactName(), version, expectedDigest, actualDigest, f.binaryName,
		))
	}
	if actualDigest != expectedDigest {
		return FetchResult{}, withStage(StageDigest, fmt.Errorf(
			"plugin binary digest mismatch for %s@%s: expected %s, got %s (possible supply chain attack or corrupted download)",
			dep.ArtifactName(), version, expectedDigest, actualDigest,
		))
	}

	// Signature verification (after digest passes, before caching).
	var sigResult *SignatureResult
	if f.sigPolicy.IsEnabled() {
		if fetchInfo.ImageRef == "" {
			if err := HandleVerificationError(f.sigPolicy, errUnverifiable(dep, version, fetchInfo), f.logger,
				"name", dep.ArtifactName(), "version", version); err != nil {
				return FetchResult{}, withStage(StageSignature, err)
			}
		} else {
			sigResult, err = f.verifySignature(ctx, dep.ArtifactName(), version, fetchInfo.ImageRef)
			if err != nil {
				return FetchResult{}, withStage(StageSignature, err)
			}
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
		return FetchResult{}, withStage(StageCache, fmt.Errorf("caching binary: %w", err))
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
	return RegisterFetchedAuthHandlerPluginsNamed(ctx, registry, results, cfg, "", clientOpts...)
}

// RegisterFetchedAuthHandlerPluginsNamed is RegisterFetchedAuthHandlerPlugins
// restricted to the handler named only: any other handler a plugin binary
// advertises is skipped with a warning. Use it when the client options are
// scoped to that one handler (see WithSecretScope), since extra handlers
// sharing the client would be denied access to their own secret namespace.
// An empty only registers every advertised handler.
func RegisterFetchedAuthHandlerPluginsNamed(ctx context.Context, registry *auth.Registry, results []FetchResult, cfg *ProviderConfig, only string, clientOpts ...ClientOption) ([]*AuthHandlerClient, error) {
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

		if only != "" {
			handlers = filterAuthHandlers(ctx, handlers, only, r.Name)
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

// filterAuthHandlers keeps only the handler named only, warning about every
// other handler the plugin advertises.
func filterAuthHandlers(ctx context.Context, handlers []AuthHandlerInfo, only, pluginName string) []AuthHandlerInfo {
	var kept []AuthHandlerInfo
	for _, h := range handlers {
		if h.Name == only {
			kept = append(kept, h)
			continue
		}
		logr.FromContextOrDiscard(ctx).V(0).Info("WARNING: skipping auth handler not requested from plugin",
			"plugin", pluginName, "handler", h.Name, "requested", only)
	}
	return kept
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
