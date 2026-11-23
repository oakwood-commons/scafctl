// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// PluginPolicy describes what is allowed from a single catalog.
type PluginPolicy struct {
	AllowAll bool     // true = wildcard ("catalog/*"), Plugins is ignored
	Plugins  []string // explicit plugin names (only meaningful when AllowAll is false)
}

// CheckPluginPolicy reports whether pluginName may be served by the catalog
// named catalogName, per a per-catalog allowlist. It is the single source of
// truth for per-catalog plugin-allowlist semantics, shared by the catalog
// index gate (catalogindex.Index.CheckPluginAllowed) and the plugin version
// pool.
//
// Semantics (parity with BuildCatalogChain): a nil/empty policies map is an
// open gate (allows everything); when the map is set, an empty catalogName is
// rejected (an unverifiable origin must not pass a restrictive policy), a
// catalog ABSENT from the map is deny-all, a catalog with AllowAll is
// unrestricted, and otherwise pluginName must appear in the catalog's explicit
// list. Catalog keys are matched case-insensitively, so callers must store the
// map with lowercased keys.
func CheckPluginPolicy(policies map[string]PluginPolicy, catalogName, pluginName string) error {
	if len(policies) == 0 {
		return nil
	}
	if catalogName == "" {
		return fmt.Errorf("plugin origin unknown; cannot verify plugin %q against per-catalog allowlist", pluginName)
	}
	policy, ok := policies[strings.ToLower(catalogName)]
	if !ok {
		return fmt.Errorf("catalog %q is not permitted to serve any plugins", catalogName)
	}
	if policy.AllowAll {
		return nil
	}
	for _, p := range policy.Plugins {
		if p == pluginName {
			return nil
		}
	}
	return fmt.Errorf("plugin %q is not in catalog %q's allowed plugins list", pluginName, catalogName)
}

type ChainCatalogOptions struct {
	allowedCatalogs      map[string]bool
	perCatalogArtifacts  map[string]PluginPolicy
	withoutLocalBuilds   bool
	localCatalog         *LocalCatalog
	pluginChainMode      ChainMode
	hasPluginChainMode   bool
	pluginWarningHandler PluginChainWarningFunc
}

type ChainCatalogOption func(*ChainCatalogOptions)

// WithPluginChainMode sets the ChainMode used by BuildPluginFetcher and
// BuildRemotePluginFetcher when walking unpinned plugin sources. When unset,
// BuildPluginFetcher and BuildRemotePluginFetcher default to
// ChainModeBestEffort so an unreachable or unauthenticated higher-priority
// catalog warns and falls through to subsequent catalogs (such as official).
// Pass ChainModeStrict for fail-closed paths (--strict, API server).
func WithPluginChainMode(mode ChainMode) ChainCatalogOption {
	return func(opt *ChainCatalogOptions) {
		opt.pluginChainMode = mode
		opt.hasPluginChainMode = true
	}
}

// WithPluginWarningHandler registers a callback invoked at most once per
// failing remote catalog when a best-effort plugin chain falls through past a
// non-404 error.
func WithPluginWarningHandler(fn PluginChainWarningFunc) ChainCatalogOption {
	return func(opt *ChainCatalogOptions) {
		opt.pluginWarningHandler = fn
	}
}

// WithLocalCatalog supplies an already-open LocalCatalog instance instead of
// opening the default XDG local catalog on disk. Callers that already hold a
// LocalCatalog handle (such as builder.Packager or embedder tests) pass it here
// so the chain and pull-through caches share that exact catalog instance.
func WithLocalCatalog(local *LocalCatalog) ChainCatalogOption {
	return func(opt *ChainCatalogOptions) {
		opt.localCatalog = local
	}
}

// WithoutLocalBuilds excludes locally built artifacts as a plugin source in
// BuildPluginFetcher, so plugin references resolve only against remote
// catalogs and a stray local build cannot shadow a published plugin. The
// local catalog still backs the remotes as their pull-through cache. It has
// no effect on BuildCatalogChain.
func WithoutLocalBuilds() ChainCatalogOption {
	return func(opt *ChainCatalogOptions) {
		opt.withoutLocalBuilds = true
	}
}

func WithAllowedCatalogs(catalogNames []string) ChainCatalogOption {
	return func(opt *ChainCatalogOptions) {
		if len(catalogNames) == 0 {
			return
		}
		if opt.allowedCatalogs == nil {
			opt.allowedCatalogs = make(map[string]bool)
		}
		for _, name := range catalogNames {
			opt.allowedCatalogs[name] = true
		}
	}
}

// WithPerCatalogArtifacts restricts which artifacts each catalog may serve.
// The map keys are catalog names; values describe the policy for that catalog.
// Catalogs present with AllowAll=true are unrestricted. Catalogs present with
// an explicit Plugins list are wrapped with an AllowlistCatalog. Catalogs NOT
// in the map are wrapped with an empty allowlist (deny all) when restrictions
// are active.
func WithPerCatalogArtifacts(perCatalog map[string]PluginPolicy) ChainCatalogOption {
	return func(opt *ChainCatalogOptions) {
		opt.perCatalogArtifacts = perCatalog
	}
}

func isCatalogAllowed(name string, allowed map[string]bool) bool {
	return allowed == nil || allowed[name]
}

// CatalogBuildError is returned when a catalog that passed the allowed-catalog
// filter cannot be built. Callers can use errors.As to distinguish this from
// other chain-build failures (e.g. an empty chain).
type CatalogBuildError struct { //nolint:revive // stutters as catalog.CatalogBuildError but renaming would break consumers
	Catalog string // catalog name that failed
	Err     error  // underlying build error
}

func (e *CatalogBuildError) Error() string {
	return fmt.Sprintf("building catalog %q: %s", e.Catalog, e.Err)
}

func (e *CatalogBuildError) Unwrap() error { return e.Err }

// BuildCatalogChain creates a ChainCatalog from the application configuration.
//
// The chain order is deterministic:
//  1. Local filesystem catalog (always first)
//  2. Embedder/user catalogs from config (in config order, excluding reserved names)
//  3. Official catalog (always last, unless disabled via settings.disableOfficialCatalog)
//
// If authRegistry is provided, catalogs with an authProvider field will use
// the corresponding auth handler for dynamic token injection.
func BuildCatalogChain(cfg *config.Config, authRegistry *auth.Registry, logger logr.Logger, opts ...ChainCatalogOption) (*ChainCatalog, error) {
	var catalogs []Catalog

	// Apply options
	options := applyChainCatalogOptions(opts)

	// 1. Local catalog always comes first.
	localCat, localAllowed, err := resolveLocalCatalog(logger, options.allowedCatalogs, options.localCatalog)
	if err != nil {
		return nil, err
	}
	if localCat != nil && localAllowed {
		catalogs = append(catalogs, localCat)
	}

	// 2-3. Embedder/user catalogs from config, then the official catalog.
	remotes, err := buildConfiguredRemoteCatalogs(cfg, authRegistry, logger, options.allowedCatalogs, true, nil)
	if err != nil {
		return nil, err
	}
	for _, remote := range remotes {
		var cat Catalog = remote.catalog
		if len(remote.config.ArtifactKinds) > 0 {
			cat = NewKindFilterCatalog(cat, ParseConfiguredArtifactKinds(remote.config.ArtifactKinds))
		}
		catalogs = append(catalogs, cat)
	}

	if len(catalogs) == 0 {
		return nil, fmt.Errorf("no catalogs available (local catalog could not be initialized)")
	}

	// Wrap catalogs with per-catalog artifact allowlists when configured.
	for i, cat := range catalogs {
		if allowed, restrict := artifactAllowlist(cat.Name(), options.perCatalogArtifacts); restrict {
			catalogs[i] = NewAllowlistCatalog(cat, allowed)
		}
	}

	return NewChainCatalog(logger, catalogs...)
}

// BuildPluginFetcher creates a PluginFetcher from the application
// configuration. Source order matches BuildCatalogChain: local built
// artifacts first (when the local catalog is allowed), then embedder/user
// catalogs, then the official catalog. Catalogs configured with artifactKinds
// that include neither provider nor auth-handler are skipped entirely. Each
// remote is backed by the local catalog as a pull-through cache, so pulled
// copies are attributed to the remote that served them and remain usable when
// it is unreachable. The local catalog serves as that cache even when it is
// not an allowed source itself.
func BuildPluginFetcher(cfg *config.Config, authRegistry *auth.Registry, logger logr.Logger, opts ...ChainCatalogOption) (*PluginFetcher, error) {
	options := applyChainCatalogOptions(opts)

	localCat, localAllowed, err := resolveLocalCatalog(logger, options.allowedCatalogs, options.localCatalog)
	if err != nil {
		return nil, err
	}

	remotes, err := buildConfiguredRemoteCatalogs(cfg, authRegistry, logger, options.allowedCatalogs, true, func(catCfg config.CatalogConfig) bool {
		return catCfg.ServesPluginKinds()
	})
	if err != nil {
		return nil, err
	}

	sources := make([]PluginSource, 0, len(remotes))
	for _, remote := range remotes {
		var src PluginSource = remote.catalog
		if localCat != nil {
			cached, cachedErr := NewCachedRemoteCatalog(localCat, remote.catalog)
			if cachedErr != nil {
				return nil, &CatalogBuildError{Catalog: remote.catalog.Name(), Err: cachedErr}
			}
			src = cached
		}
		if !servesAllPluginKinds(remote.config) {
			src = newKindFilterPluginSource(src, ParseConfiguredArtifactKinds(remote.config.ArtifactKinds))
		}
		if allowed, restrict := artifactAllowlist(remote.catalog.Name(), options.perCatalogArtifacts); restrict {
			src = newAllowlistPluginSource(src, allowed)
		}
		sources = append(sources, src)
	}

	var local *LocalCatalog
	if localCat != nil && localAllowed && !options.withoutLocalBuilds {
		local = localCat
		if allowed, restrict := artifactAllowlist(localCat.Name(), options.perCatalogArtifacts); restrict {
			// The built-only filter must sit beneath the allowlist, so a
			// restricted local source is passed as an ordinary source.
			sources = append([]PluginSource{newAllowlistPluginSource(newLocalPlugin(localCat), allowed)}, sources...)
			local = nil
		}
	}

	if local == nil && len(sources) == 0 {
		return nil, fmt.Errorf("no catalogs available for plugin resolution")
	}

	return NewPluginFetcherWithOptions(logger, local, options.fetcherOptions(), sources...)
}

// BuildRemotePluginFetcher creates a PluginFetcher over remote catalogs only
// (user/embedder catalogs and the official catalog), with no local source and
// no local cache fallback. Catalogs that fail to build or serve no plugin
// artifact kinds are skipped.
//
// This is used by operations that must observe the live state of remote
// registries (e.g. plugin update checks): a cache fallback would report a
// stale version as current when a registry is unreachable.
func BuildRemotePluginFetcher(cfg *config.Config, authRegistry *auth.Registry, logger logr.Logger, opts ...ChainCatalogOption) (*PluginFetcher, error) {
	options := applyChainCatalogOptions(opts)

	remotes, err := buildConfiguredRemoteCatalogs(cfg, authRegistry, logger, options.allowedCatalogs, false, func(catCfg config.CatalogConfig) bool {
		return catCfg.ServesPluginKinds()
	})
	if err != nil {
		return nil, err
	}
	if len(remotes) == 0 {
		return nil, fmt.Errorf("no remote catalogs available")
	}
	sources := make([]PluginSource, 0, len(remotes))
	for _, remote := range remotes {
		var src PluginSource = remote.catalog
		if !servesAllPluginKinds(remote.config) {
			src = newKindFilterPluginSource(src, ParseConfiguredArtifactKinds(remote.config.ArtifactKinds))
		}
		if allowed, restrict := artifactAllowlist(remote.catalog.Name(), options.perCatalogArtifacts); restrict {
			src = newAllowlistPluginSource(src, allowed)
		}
		sources = append(sources, src)
	}
	return NewPluginFetcherWithOptions(logger, nil, options.fetcherOptions(), sources...)
}

func (o ChainCatalogOptions) fetcherOptions() []PluginFetcherOption {
	mode := ChainModeBestEffort
	if o.hasPluginChainMode {
		mode = o.pluginChainMode
	}
	fetcherOpts := []PluginFetcherOption{WithFetcherChainMode(mode)}
	if o.pluginWarningHandler != nil {
		fetcherOpts = append(fetcherOpts, WithFetcherWarningHandler(o.pluginWarningHandler))
	}
	return fetcherOpts
}

// ParseConfiguredArtifactKinds parses raw config artifact kind strings into
// typed ArtifactKind values. A nil/empty input returns nil ("all kinds"); a
// non-empty input returns a non-nil slice (which may be empty when no entries
// are valid, causing callers such as NewKindFilterCatalog to fail closed).
func ParseConfiguredArtifactKinds(raw []string) []ArtifactKind {
	if len(raw) == 0 {
		return nil
	}
	kinds := make([]ArtifactKind, 0, len(raw))
	for _, s := range raw {
		if k, ok := ParseArtifactKind(s); ok {
			kinds = append(kinds, k)
		}
	}
	return kinds
}

// FilterCatalogKinds intersects requested artifact kinds against catCfg's
// configured ArtifactKinds. It returns (effectiveKinds, true) when the catalog
// can serve at least one requested kind, or (nil, false) when the catalog
// should be skipped without contacting it. When requested is empty ("all
// kinds"), effectiveKinds remains nil so callers enumerate once and filter via
// NewKindFilterCatalog.
func FilterCatalogKinds(catCfg config.CatalogConfig, requested []ArtifactKind) ([]ArtifactKind, bool) {
	if len(catCfg.ArtifactKinds) > 0 && len(ParseConfiguredArtifactKinds(catCfg.ArtifactKinds)) == 0 {
		return nil, false
	}
	if len(requested) == 0 {
		return nil, true
	}
	filtered := make([]ArtifactKind, 0, len(requested))
	for _, k := range requested {
		if k == "" || catCfg.ServesArtifactKind(string(k)) {
			filtered = append(filtered, k)
		}
	}
	if len(filtered) == 0 {
		return nil, false
	}
	return filtered, true
}

func servesAllPluginKinds(catCfg config.CatalogConfig) bool {
	return catCfg.ServesArtifactKind(config.CatalogArtifactKindProvider) &&
		catCfg.ServesArtifactKind(config.CatalogArtifactKindAuthHandler)
}

func applyChainCatalogOptions(opts []ChainCatalogOption) ChainCatalogOptions {
	var options ChainCatalogOptions
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

// resolveLocalCatalog uses provided when non-nil and otherwise opens the
// default local catalog on disk.
func resolveLocalCatalog(logger logr.Logger, allowedCatalogs map[string]bool, provided *LocalCatalog) (*LocalCatalog, bool, error) {
	if provided != nil {
		allowed := isCatalogAllowed(provided.Name(), allowedCatalogs)
		if !allowed {
			logger.V(1).Info("local catalog not in allowed list, skipping")
		}
		return provided, allowed, nil
	}
	return buildLocalCatalog(logger, allowedCatalogs)
}

// buildLocalCatalog opens the local catalog and reports whether it is an
// allowed source. A build failure is an error only when local is allowed;
// otherwise it is logged and a nil catalog is returned.
func buildLocalCatalog(logger logr.Logger, allowedCatalogs map[string]bool) (*LocalCatalog, bool, error) {
	allowed := isCatalogAllowed(config.CatalogNameLocal, allowedCatalogs)
	localCat, err := NewLocalCatalog(logger)
	switch {
	case err != nil && allowed:
		return nil, false, &CatalogBuildError{Catalog: config.CatalogNameLocal, Err: err}
	case err != nil:
		logger.V(1).Info("local catalog not available", "error", err)
		return nil, false, nil
	}
	allowed = isCatalogAllowed(localCat.Name(), allowedCatalogs)
	if !allowed {
		logger.V(1).Info("local catalog not in allowed list, skipping")
	}
	return localCat, allowed, nil
}

type configuredRemoteCatalog struct {
	catalog *RemoteCatalog
	config  config.CatalogConfig
}

// buildConfiguredRemoteCatalogs builds the remote catalogs from cfg in chain
// order: embedder/user OCI catalogs (config order, reserved names excluded),
// then the official catalog unless disabled. Catalogs outside allowedCatalogs
// or rejected by filter are skipped. When strict is true a catalog that fails
// to build is a CatalogBuildError; otherwise it is skipped.
func buildConfiguredRemoteCatalogs(
	cfg *config.Config,
	authRegistry *auth.Registry,
	logger logr.Logger,
	allowedCatalogs map[string]bool,
	strict bool,
	filter func(config.CatalogConfig) bool,
) ([]configuredRemoteCatalog, error) {
	if cfg == nil {
		return nil, nil
	}

	var credStore *CredentialStore
	cs, credErr := NewCredentialStore(logger)
	if credErr != nil {
		logger.V(1).Info("credential store not available, remote catalogs will use anonymous auth", "error", credErr)
	} else {
		credStore = cs
	}

	var remotes []configuredRemoteCatalog
	add := func(catCfg config.CatalogConfig) error {
		if filter != nil && !filter(catCfg) {
			logger.V(1).Info("catalog excluded by artifact kind filter, skipping", "catalog", catCfg.Name, "artifactKinds", catCfg.ArtifactKinds)
			return nil
		}
		remoteCat, err := buildRemoteCatalog(catCfg, credStore, authRegistry, logger)
		if err != nil {
			if strict {
				return &CatalogBuildError{Catalog: catCfg.Name, Err: err}
			}
			return nil
		}
		remotes = append(remotes, configuredRemoteCatalog{catalog: remoteCat, config: catCfg})
		return nil
	}

	for _, catCfg := range cfg.Catalogs {
		// Skip reserved catalogs -- they are pinned in position.
		if catCfg.Name == config.CatalogNameLocal || catCfg.Name == config.CatalogNameOfficial {
			continue
		}
		if catCfg.Type != config.CatalogTypeOCI || catCfg.URL == "" {
			continue
		}
		if !isCatalogAllowed(catCfg.Name, allowedCatalogs) {
			logger.V(1).Info("catalog not in allowed list, skipping", "catalog", catCfg.Name)
			continue
		}
		if err := add(catCfg); err != nil {
			return nil, err
		}
	}

	// Official catalog always comes last (unless disabled).
	if cfg.Settings.DisableOfficialCatalog {
		return remotes, nil
	}
	if !isCatalogAllowed(config.CatalogNameOfficial, allowedCatalogs) {
		logger.V(1).Info("official catalog not in allowed list, skipping")
		return remotes, nil
	}
	if officialCfg, ok := cfg.GetCatalog(config.CatalogNameOfficial); ok {
		if err := add(*officialCfg); err != nil {
			return nil, err
		}
	}
	return remotes, nil
}

// artifactAllowlist reports whether the catalog named catalogName must be
// restricted under perCatalog and, if so, the artifact names it may serve.
// No restriction applies when perCatalog is nil or the catalog's policy is
// AllowAll; a catalog absent from an active policy map may serve nothing.
func artifactAllowlist(catalogName string, perCatalog map[string]PluginPolicy) ([]string, bool) {
	if perCatalog == nil {
		return nil, false
	}
	policy, ok := perCatalog[catalogName]
	switch {
	case ok && policy.AllowAll:
		return nil, false
	case ok && policy.Plugins != nil:
		return policy.Plugins, true
	default:
		return []string{}, true
	}
}

// BuildRemoteCatalogChain creates a ChainCatalog containing only remote
// catalogs (user/embedder catalogs and the official catalog). The local
// filesystem catalog is excluded.
//
// This is used by operations that need to check remote registries for newer
// versions without being shadowed by locally cached artifacts (e.g. plugin
// update checks).
func BuildRemoteCatalogChain(cfg *config.Config, authRegistry *auth.Registry, logger logr.Logger) (*ChainCatalog, error) {
	remotes, err := buildConfiguredRemoteCatalogs(cfg, authRegistry, logger, nil, false, nil)
	if err != nil {
		return nil, err
	}
	if len(remotes) == 0 {
		return nil, fmt.Errorf("no remote catalogs available")
	}
	catalogs := make([]Catalog, 0, len(remotes))
	for _, remote := range remotes {
		var cat Catalog = remote.catalog
		if len(remote.config.ArtifactKinds) > 0 {
			cat = NewKindFilterCatalog(cat, ParseConfiguredArtifactKinds(remote.config.ArtifactKinds))
		}
		catalogs = append(catalogs, cat)
	}
	return NewChainCatalog(logger, catalogs...)
}

// buildRemoteCatalog creates a RemoteCatalog from a CatalogConfig.
func buildRemoteCatalog(catCfg config.CatalogConfig, credStore *CredentialStore, authRegistry *auth.Registry, logger logr.Logger) (*RemoteCatalog, error) {
	return BuildRemoteCatalogFromConfig(catCfg, credStore, authRegistry, logger)
}

// BuildRemoteCatalogFromConfig creates a RemoteCatalog from a CatalogConfig.
// Exported for use by packages that need to construct remote catalogs outside
// the catalog chain (e.g. MCP tools for index-based search).
func BuildRemoteCatalogFromConfig(catCfg config.CatalogConfig, credStore *CredentialStore, authRegistry *auth.Registry, logger logr.Logger) (*RemoteCatalog, error) {
	registry, repository := ParseCatalogURL(catCfg.URL)

	remoteCfg := RemoteCatalogConfig{
		Name:              catCfg.Name,
		Registry:          registry,
		Repository:        repository,
		CredentialStore:   credStore,
		DiscoveryStrategy: catCfg.DiscoveryStrategy,
		Insecure:          catCfg.Insecure,
		Logger:            logger,
	}

	// Wire auth handler if configured.
	if catCfg.AuthProvider != "" {
		// Verify the handler is registered (no fallback) to avoid triggering
		// lazy plugin resolution from within a builder function. This prevents
		// a circular dependency when BuildRemoteCatalogFromConfig is called
		// inside the auth handler fallback resolver path.
		if authRegistry != nil {
			handler, exists := authRegistry.GetRegistered(catCfg.AuthProvider)
			if !exists {
				logger.V(1).Info("auth provider not yet registered for catalog, skipping dynamic auth",
					"catalog", catCfg.Name,
					"authProvider", catCfg.AuthProvider)
			} else {
				remoteCfg.AuthHandler = handler
				remoteCfg.AuthScope = catCfg.AuthScope
			}
		}
	}

	remoteCat, err := NewRemoteCatalog(remoteCfg)
	if err != nil {
		logger.V(1).Info("failed to create remote catalog, skipping",
			"catalog", catCfg.Name,
			"error", err)
		return nil, err
	}
	return remoteCat, nil
}

// DefaultListCatalogs returns the ordered set of OCI catalogs a bare
// `catalog list` (no --catalog, no --all) should query: the configured default
// catalog first (primary), followed by the built-in official catalog as an
// anonymous fallback. This mirrors the ordered chain the resolver consumes (see
// BuildCatalogChain) so a private or unauthenticated default catalog does not
// hide artifacts that are anonymously available from the official catalog
// (issue #692).
//
// The official catalog is omitted when settings.disableOfficialCatalog is set --
// including the case where the default catalog IS the official catalog, so a
// disabled official catalog is never listed. Non-OCI catalogs are skipped, and
// the official catalog is de-duplicated when the default catalog IS the official
// catalog (by reserved name or identical URL).
func DefaultListCatalogs(cfg *config.Config) []config.CatalogConfig {
	if cfg == nil {
		return nil
	}

	var catalogs []config.CatalogConfig

	defCat, hasDefault := cfg.GetDefaultCatalog()
	defaultIsOfficial := hasDefault &&
		(defCat.Name == config.CatalogNameOfficial || isOfficialByURL(cfg, defCat.URL))

	// Include the default catalog first, unless it IS the official catalog and
	// the official catalog is disabled (in which case it must not be listed).
	if hasDefault && defCat.Type == config.CatalogTypeOCI &&
		(!defaultIsOfficial || !cfg.Settings.DisableOfficialCatalog) {
		catalogs = append(catalogs, *defCat)
	}

	if cfg.Settings.DisableOfficialCatalog {
		return catalogs
	}

	official, hasOfficial := cfg.GetCatalog(config.CatalogNameOfficial)
	if !hasOfficial || official.Type != config.CatalogTypeOCI {
		return catalogs
	}

	// Skip official if the default already IS official (same reserved name or
	// identical URL) to avoid querying and listing it twice.
	if hasDefault && (defCat.Name == config.CatalogNameOfficial || defCat.URL == official.URL) {
		return catalogs
	}

	return append(catalogs, *official)
}

// isOfficialByURL reports whether the given URL matches the official catalog's
// URL. Used to detect that a default catalog is the official catalog even when
// it is referenced under a different (mirror) name.
//
// When an official catalog entry is present in the config it is only considered
// if it is an OCI catalog (matching the type guard applied throughout
// DefaultListCatalogs), so a non-OCI official entry never causes a default to be
// treated as official-by-URL. When settings.disableOfficialCatalog is set the
// config merge omits the official entry entirely; in that case we fall back to
// the embedded default official URL so the URL-based disable/dedup semantics
// still recognize an aliased default that points at the official registry.
func isOfficialByURL(cfg *config.Config, url string) bool {
	if url == "" {
		return false
	}
	if official, ok := cfg.GetCatalog(config.CatalogNameOfficial); ok {
		return official.Type == config.CatalogTypeOCI && official.URL == url
	}
	return url == config.DefaultOfficialCatalogURL
}
