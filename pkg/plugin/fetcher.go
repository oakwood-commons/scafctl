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
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// Fetcher resolves, downloads, caches, and loads plugin binaries at runtime.
// It checks a local cache first, then falls back to fetching from catalogs.
type Fetcher struct {
	binaryName      string
	catalogFetcher  *catalog.PluginFetcher
	cache           *Cache
	platform        string
	noCache         bool
	logger          logr.Logger
	allowedCatalogs map[string]bool // if non-nil, only these catalog names are permitted
	sigPolicy       *SignaturePolicy
	sigVerifier     SignatureVerifier
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

	var allowedCatalogs map[string]bool
	if len(cfg.AllowedCatalogs) > 0 {
		allowedCatalogs = make(map[string]bool, len(cfg.AllowedCatalogs))
		for _, name := range cfg.AllowedCatalogs {
			allowedCatalogs[strings.ToLower(name)] = true
		}
	}

	sigVerifier := cfg.SignatureVerifier
	if sigVerifier == nil {
		sigVerifier = NewSignatureVerifier()
	}

	return &Fetcher{
		binaryName:      binaryName,
		catalogFetcher:  catalog.NewPluginFetcher(cfg.Catalog, cfg.Logger),
		cache:           cache,
		platform:        platform,
		noCache:         cfg.NoCache,
		logger:          cfg.Logger.WithName("plugin-fetcher"),
		allowedCatalogs: allowedCatalogs,
		sigPolicy:       cfg.SignaturePolicy,
		sigVerifier:     sigVerifier,
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

	// Signature holds signature verification metadata when verification
	// was performed. Nil when signatures are disabled or the binary was cached.
	//
	// TODO: surface Signature in CLI output/audit log so users can inspect
	// verification results after a fetch.
	Signature *SignatureResult

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
	cacheKey := PluginCacheKey(dep.Name, dep.Kind)

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

		// Security: check catalog allowlist even for locked entries.
		if err := f.checkCatalogAllowed(resolvedFrom); err != nil {
			return FetchResult{}, err
		}
	} else {
		// No lock file — prefer cached version to avoid network latency.
		// Only resolve from catalog if no cached version exists.
		if !f.noCache {
			if cachedPath, cachedVer, ok := f.cache.GetLatestCached(cacheKey, f.platform); ok {
				// If a version constraint is specified, verify the cached version satisfies it.
				satisfies, _ := cachedVersionSatisfies(dep.Version, cachedVer)
				if satisfies {
					// Security: reject cached plugins when an allowlist is configured
					// but cache lacks catalog origin metadata.
					if err := f.checkCatalogAllowed(""); err != nil {
						return FetchResult{}, fmt.Errorf("cached plugin %s: %w", dep.Name, err)
					}
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
			// Fallback: if catalog resolution fails, check if a cached version
			// satisfies the requested constraint. If the cached version does not
			// match, fail with the original error so users are never silently
			// given a different version than they requested.
			if !f.noCache {
				if cachedPath, cachedVer, ok := f.cache.GetLatestCached(cacheKey, f.platform); ok {
					// Verify cached version satisfies the requested constraint.
					satisfies, constraintErr := cachedVersionSatisfies(dep.Version, cachedVer)
					if constraintErr != nil {
						return FetchResult{}, fmt.Errorf("resolving version: %w (constraint check failed: %w)", err, constraintErr)
					}
					if !satisfies {
						return FetchResult{}, fmt.Errorf("resolving version: %w (cached version %s does not satisfy %q)", err, cachedVer, dep.Version)
					}

					// Security: reject cached plugins when an allowlist is configured
					// but cache lacks catalog origin metadata.
					if allowErr := f.checkCatalogAllowed(""); allowErr != nil {
						return FetchResult{}, fmt.Errorf("cached plugin %s: %w", dep.Name, allowErr)
					}
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

		// Security: check catalog allowlist before proceeding with fetch.
		if err := f.checkCatalogAllowed(resolvedFrom); err != nil {
			return FetchResult{}, err
		}

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

	// Check local cache.
	//
	// In enforce mode, cache reads are skipped so every execution performs a
	// fresh fetch with signature verification. In warn mode, cache hits are
	// allowed because verification is advisory.
	skipCache := f.noCache || (f.sigPolicy != nil && f.sigPolicy.Mode == SignatureModeEnforce)
	if !skipCache {
		if cachedPath, ok := f.cache.Get(cacheKey, version, f.platform, expectedDigest); ok {
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

	// Signature verification (after digest passes, before caching).
	var sigResult *SignatureResult
	if f.sigPolicy.IsEnabled() && fetchInfo.ImageRef != "" {
		sigResult, err = f.verifySignature(ctx, dep.Name, version, fetchInfo.ImageRef)
		if err != nil {
			return FetchResult{}, err
		}
	}

	// Write to cache
	cachedPath, err := f.cache.Put(cacheKey, version, f.platform, data)
	if err != nil {
		return FetchResult{}, fmt.Errorf("caching binary: %w", err)
	}

	digest := fetchInfo.Digest
	if digest == "" {
		// Compute digest from the downloaded data
		d, err := f.cache.Digest(cacheKey, version, f.platform)
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
		Signature: sigResult,
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

		registered := configureAndRegisterAuthHandlers(ctx, registry, client, handlers, cfg)
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

// PluginCacheKey returns a cache-safe key for a plugin that avoids namespace
// collisions between different plugin kinds. Provider plugins use the bare
// name (e.g. "github") for backward compatibility with existing caches.
// Auth-handler plugins are prefixed (e.g. "auth-handler-github") so a provider
// named "github" and an auth-handler named "github" occupy separate cache slots.
func PluginCacheKey(name string, kind solution.PluginKind) string {
	if kind == solution.PluginKindAuthHandler {
		return "auth-handler-" + name
	}
	return name
}

// findLockPlugin looks up a lock plugin entry by name and kind.
// checkCatalogAllowed returns an error if the catalog is not in the
// configured allowlist. If no allowlist is configured, all catalogs are
// permitted. An empty resolvedFrom (e.g. from cache with no catalog metadata)
// is rejected when an allowlist is configured, since the origin cannot be verified.
func (f *Fetcher) checkCatalogAllowed(resolvedFrom string) error {
	if f.allowedCatalogs == nil {
		return nil
	}
	if resolvedFrom == "" {
		return fmt.Errorf("plugin origin unknown (cached without catalog metadata); cannot verify against allowlist")
	}
	if !f.allowedCatalogs[strings.ToLower(resolvedFrom)] {
		return fmt.Errorf("catalog %q is not in the allowed catalogs list", resolvedFrom)
	}
	return nil
}

// RegisterCachedPlugin looks up a provider plugin by name in the local cache,
// starts it, and registers its providers into the given registry.
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
func RegisterCachedPluginVersion(ctx context.Context, name, version string, registry *provider.Registry, cfg *ProviderConfig, cacheDir string, clientOpts ...ClientOption) ([]*Client, error) {
	cache := NewCache(cacheDir)
	platform := CurrentPlatform()
	path, ok := cache.Get(name, version, platform, "")
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

func findLockPlugin(plugins []bundler.LockPlugin, name, kind string) *bundler.LockPlugin {
	for i := range plugins {
		if plugins[i].Name == name && plugins[i].Kind == kind {
			return &plugins[i]
		}
	}
	return nil
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
