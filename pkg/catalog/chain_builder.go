// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// PluginPolicy describes what is allowed from a single catalog.
type PluginPolicy struct {
	AllowAll bool     // true = wildcard ("catalog/*"), Plugins is ignored
	Plugins  []string // explicit plugin names (only meaningful when AllowAll is false)
}

type ChainCatalogOptions struct {
	allowedCatalogs     map[string]bool
	perCatalogArtifacts map[string]PluginPolicy
}

type ChainCatalogOption func(*ChainCatalogOptions)

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
	var options ChainCatalogOptions
	for _, opt := range opts {
		opt(&options)
	}

	// 1. Local catalog always comes first.
	localCat, err := NewLocalCatalog(logger)
	switch {
	case err != nil:
		logger.V(1).Info("local catalog not available", "error", err)
	case !isCatalogAllowed(localCat.Name(), options.allowedCatalogs):
		logger.V(1).Info("local catalog not in allowed list, skipping")
	default:
		catalogs = append(catalogs, localCat)
	}

	// 2. Add embedder/user catalogs from config (skip reserved names).
	var credStore *CredentialStore
	if cfg != nil {
		cs, credErr := NewCredentialStore(logger)
		if credErr != nil {
			logger.V(1).Info("credential store not available, remote catalogs will use anonymous auth", "error", credErr)
		} else {
			credStore = cs
		}

		for _, catCfg := range cfg.Catalogs {
			// Skip reserved catalogs -- they are pinned in position.
			if catCfg.Name == config.CatalogNameLocal || catCfg.Name == config.CatalogNameOfficial {
				continue
			}

			if catCfg.Type != config.CatalogTypeOCI || catCfg.URL == "" {
				continue
			}

			if !isCatalogAllowed(catCfg.Name, options.allowedCatalogs) {
				logger.V(1).Info("catalog not in allowed list, skipping", "catalog", catCfg.Name)
				continue
			}

			remoteCat, remoteCatErr := buildRemoteCatalog(catCfg, credStore, authRegistry, logger)
			if remoteCatErr != nil {
				continue
			}
			catalogs = append(catalogs, remoteCat)
		}

		// 3. Official catalog always comes last (unless disabled).
		if !cfg.Settings.DisableOfficialCatalog {
			if !isCatalogAllowed(config.CatalogNameOfficial, options.allowedCatalogs) {
				logger.V(1).Info("official catalog not in allowed list, skipping")
			} else if officialCfg, ok := cfg.GetCatalog(config.CatalogNameOfficial); ok {
				officialCat, officialErr := buildRemoteCatalog(*officialCfg, credStore, authRegistry, logger)
				if officialErr == nil {
					catalogs = append(catalogs, officialCat)
				}
			}
		}
	}

	if len(catalogs) == 0 {
		return nil, fmt.Errorf("no catalogs available (local catalog could not be initialized)")
	}

	// Wrap catalogs with per-catalog artifact allowlists when configured.
	if options.perCatalogArtifacts != nil {
		for i, cat := range catalogs {
			policy, ok := options.perCatalogArtifacts[cat.Name()]
			switch {
			case ok && policy.AllowAll:
				// Wildcard: no restriction needed
			case ok:
				catalogs[i] = NewAllowlistCatalog(cat, policy.Plugins)
			default:
				// Catalog not in policy map: deny all when restrictions are active
				catalogs[i] = NewAllowlistCatalog(cat, []string{})
			}
		}
	}

	return NewChainCatalog(logger, catalogs...)
}

// BuildRemoteCatalogChain creates a ChainCatalog containing only remote
// catalogs (user/embedder catalogs and the official catalog). The local
// filesystem catalog is excluded.
//
// This is used by operations that need to check remote registries for newer
// versions without being shadowed by locally cached artifacts (e.g. plugin
// update checks).
func BuildRemoteCatalogChain(cfg *config.Config, authRegistry *auth.Registry, logger logr.Logger) (*ChainCatalog, error) {
	var catalogs []Catalog

	var credStore *CredentialStore
	if cfg != nil {
		cs, credErr := NewCredentialStore(logger)
		if credErr != nil {
			logger.V(1).Info("credential store not available, remote catalogs will use anonymous auth", "error", credErr)
		} else {
			credStore = cs
		}

		for _, catCfg := range cfg.Catalogs {
			if catCfg.Name == config.CatalogNameLocal || catCfg.Name == config.CatalogNameOfficial {
				continue
			}

			if catCfg.Type != config.CatalogTypeOCI || catCfg.URL == "" {
				continue
			}

			remoteCat, remoteCatErr := buildRemoteCatalog(catCfg, credStore, authRegistry, logger)
			if remoteCatErr != nil {
				continue
			}
			catalogs = append(catalogs, remoteCat)
		}

		if !cfg.Settings.DisableOfficialCatalog {
			if officialCfg, ok := cfg.GetCatalog(config.CatalogNameOfficial); ok {
				officialCat, officialErr := buildRemoteCatalog(*officialCfg, credStore, authRegistry, logger)
				if officialErr == nil {
					catalogs = append(catalogs, officialCat)
				}
			}
		}
	}

	if len(catalogs) == 0 {
		return nil, fmt.Errorf("no remote catalogs available")
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

// isOfficialByURL reports whether the given URL matches the configured official
// catalog's URL. Used to detect that a default catalog is the official catalog
// even when it is referenced under a different (mirror) name. The official
// catalog is only ever considered when it is an OCI catalog (matching the
// type guard applied throughout DefaultListCatalogs), so a non-OCI official
// entry never causes a default to be treated as official-by-URL.
func isOfficialByURL(cfg *config.Config, url string) bool {
	if url == "" {
		return false
	}
	official, ok := cfg.GetCatalog(config.CatalogNameOfficial)
	return ok && official.Type == config.CatalogTypeOCI && official.URL == url
}
