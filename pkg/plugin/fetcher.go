// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/metrics"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// Fetcher resolves, downloads, caches, and loads plugin binaries at runtime.
// It checks a local cache first, then falls back to fetching from catalogs.
type Fetcher struct {
	binaryName     string
	catalogFetcher *catalog.PluginFetcher
	cache          *Cache
	platform       string
	noCache        bool
	logger         logr.Logger
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
		binaryName = "scafctl" // fallback must match settings.CliBinaryName
	}

	return &Fetcher{
		binaryName:     binaryName,
		catalogFetcher: catalog.NewPluginFetcher(cfg.Catalog, cfg.Logger),
		cache:          cache,
		platform:       platform,
		noCache:        cfg.NoCache,
		logger:         cfg.Logger.WithName("plugin-fetcher"),
	}
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

	// Catalog is the catalog name the plugin was fetched from (empty if cached).
	Catalog string
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
	if len(plugins) == 0 {
		return nil, nil
	}

	results := make([]FetchResult, 0, len(plugins))

	for _, dep := range plugins {
		result, err := f.fetchOne(ctx, dep, lockPlugins)
		if err != nil {
			return nil, fmt.Errorf("plugin %s (%s): %w", dep.Name, dep.Kind, err)
		}
		results = append(results, result)
	}

	return results, nil
}

// fetchOne resolves and fetches a single plugin dependency.
func (f *Fetcher) fetchOne(ctx context.Context, dep solution.PluginDependency, lockPlugins []bundler.LockPlugin) (FetchResult, error) {
	start := time.Now()
	result, err := f.doFetchOne(ctx, dep, lockPlugins)
	duration := time.Since(start).Seconds()

	source := "registry"
	if err == nil && result.FromCache {
		source = "cache"
	}

	f.logger.V(1).Info("plugin resolution completed",
		"name", dep.Name,
		"source", source,
		"duration_ms", time.Since(start).Milliseconds(),
		"success", err == nil)

	metrics.RecordPluginResolution(ctx, dep.Name, source, duration, err == nil)
	return result, err
}

// doFetchOne performs the actual resolution and fetch logic for a single plugin.
func (f *Fetcher) doFetchOne(ctx context.Context, dep solution.PluginDependency, lockPlugins []bundler.LockPlugin) (FetchResult, error) {
	kind := pluginKindToArtifactKind(dep.Kind)

	// Check lock file for a pinned version
	locked := findLockPlugin(lockPlugins, dep.Name, string(dep.Kind))

	var version, expectedDigest, resolvedFrom string

	if locked != nil {
		// Use pinned version from lock file
		version = locked.Version
		expectedDigest = locked.Digest
		resolvedFrom = locked.ResolvedFrom

		f.logger.V(1).Info("using pinned plugin version from lock file",
			"name", dep.Name,
			"version", version,
			"digest", expectedDigest)
	} else {
		// No lock file — prefer cached version to avoid network latency.
		// Only resolve from catalog if no cached version exists.
		if !f.noCache {
			if cachedPath, cachedVer, ok := f.cache.GetLatestCached(dep.Name, f.platform); ok {
				// If a version constraint is specified, verify the cached version satisfies it.
				useCached := true
				if dep.Version != "" && !strings.EqualFold(dep.Version, "latest") {
					satisfies, err := bundler.CheckVersionConstraint(dep.Version, cachedVer)
					if err != nil || !satisfies {
						useCached = false
					}
				}
				if useCached {
					f.logger.V(1).Info("using cached plugin (no lock file)",
						"name", dep.Name,
						"version", cachedVer,
						"path", cachedPath)
					return FetchResult{
						Name:      dep.Name,
						Kind:      dep.Kind,
						Version:   cachedVer,
						Path:      cachedPath,
						FromCache: true,
					}, nil
				}
			}
		}

		// Cache miss or constraint not satisfied — resolve from catalog.
		f.logger.V(0).Info("WARNING: resolving plugin without lock file — version may differ between runs",
			"name", dep.Name,
			"constraint", dep.Version,
			"hint", fmt.Sprintf("Run '%s build solution' to pin plugin versions", f.binaryName))

		info, err := f.catalogFetcher.ResolvePlugin(ctx, dep.Name, kind, dep.Version)
		if err != nil {
			// Fallback: if catalog resolution fails, check if a cached version exists.
			if !f.noCache {
				if cachedPath, cachedVer, ok := f.cache.GetLatestCached(dep.Name, f.platform); ok {
					f.logger.V(0).Info("catalog resolution failed, using cached version",
						"name", dep.Name,
						"version", cachedVer,
						"path", cachedPath,
						"error", err)
					return FetchResult{
						Name:      dep.Name,
						Kind:      dep.Kind,
						Version:   cachedVer,
						Path:      cachedPath,
						FromCache: true,
					}, nil
				}
			}
			return FetchResult{}, fmt.Errorf("resolving version: %w", err)
		}

		if info.Reference.Version != nil {
			version = info.Reference.Version.String()
		}
		expectedDigest = info.Digest
		resolvedFrom = info.Catalog

		// Verify the resolved version satisfies the constraint.
		// "latest" means "whatever the resolver picked" and is not a valid
		// semver constraint, so skip the check in that case.
		if version != "" && dep.Version != "" && !strings.EqualFold(dep.Version, "latest") {
			satisfies, err := bundler.CheckVersionConstraint(dep.Version, version)
			if err != nil {
				return FetchResult{}, fmt.Errorf("checking version constraint: %w", err)
			}
			if !satisfies {
				return FetchResult{}, fmt.Errorf("resolved version %s does not satisfy constraint %s", version, dep.Version)
			}
		}
	}

	// Check local cache
	if !f.noCache {
		if cachedPath, ok := f.cache.Get(dep.Name, version, f.platform, expectedDigest); ok {
			f.logger.V(1).Info("plugin found in cache",
				"name", dep.Name,
				"version", version,
				"path", cachedPath)

			return FetchResult{
				Name:      dep.Name,
				Kind:      dep.Kind,
				Version:   version,
				Path:      cachedPath,
				Digest:    expectedDigest,
				FromCache: true,
			}, nil
		}
	}

	// Cache miss — fetch from catalog
	f.logger.V(1).Info("fetching plugin from catalog",
		"name", dep.Name,
		"version", version,
		"platform", f.platform)

	data, fetchInfo, err := f.catalogFetcher.FetchPlugin(ctx, dep.Name, kind, version, f.platform)
	if err != nil {
		return FetchResult{}, fmt.Errorf("fetching binary: %w", err)
	}

	// For multi-platform artifacts (OCI image indexes), the digest from
	// Resolve is the index digest, not the per-platform binary content
	// digest. FetchPlugin returns the layer-level content digest after
	// selecting the platform-specific manifest. Update expectedDigest so
	// the verification below compares against the correct content hash.
	if locked == nil && fetchInfo.Digest != "" {
		expectedDigest = fetchInfo.Digest
	}

	// Verify the downloaded binary matches the expected digest before caching.
	// Digest verification is mandatory to prevent supply chain attacks via
	// compromised catalogs or man-in-the-middle attacks.
	if expectedDigest == "" {
		return FetchResult{}, fmt.Errorf(
			"plugin %s@%s: no digest available for verification; "+
				"run '%s build solution' to generate a lock file with pinned digests",
			dep.Name, version, f.binaryName,
		)
	}
	actualDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	if actualDigest != expectedDigest {
		return FetchResult{}, fmt.Errorf(
			"plugin binary digest mismatch for %s@%s: expected %s, got %s (possible supply chain attack or corrupted download)",
			dep.Name, version, expectedDigest, actualDigest,
		)
	}

	// Write to cache
	cachedPath, err := f.cache.Put(dep.Name, version, f.platform, data)
	if err != nil {
		return FetchResult{}, fmt.Errorf("caching binary: %w", err)
	}

	digest := fetchInfo.Digest
	if digest == "" {
		// Compute digest from the downloaded data
		d, err := f.cache.Digest(dep.Name, version, f.platform)
		if err == nil {
			digest = d
		}
	}

	if resolvedFrom == "" {
		resolvedFrom = fetchInfo.Catalog
	}

	f.logger.V(1).Info("plugin fetched and cached",
		"name", dep.Name,
		"version", version,
		"path", cachedPath,
		"digest", digest,
		"catalog", resolvedFrom)

	return FetchResult{
		Name:      dep.Name,
		Kind:      dep.Kind,
		Version:   version,
		Path:      cachedPath,
		Digest:    digest,
		FromCache: false,
		Catalog:   resolvedFrom,
	}, nil
}

// Paths returns just the binary paths from a slice of FetchResult.
func Paths(results []FetchResult) []string {
	paths := make([]string, 0, len(results))
	for _, r := range results {
		paths = append(paths, r.Path)
	}
	return paths
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

		configureAndRegisterAuthHandlers(ctx, registry, client, handlers, cfg)

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

// findLockPlugin looks up a lock plugin entry by name and kind.
func findLockPlugin(plugins []bundler.LockPlugin, name, kind string) *bundler.LockPlugin {
	for i := range plugins {
		if plugins[i].Name == name && plugins[i].Kind == kind {
			return &plugins[i]
		}
	}
	return nil
}
