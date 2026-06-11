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

	// Wire auth provider name if configured.
	if catCfg.AuthProvider != "" {
		// Verify the handler is registered (no fallback) to avoid triggering
		// lazy plugin resolution from within a builder function. This prevents
		// a circular dependency when BuildRemoteCatalogFromConfig is called
		// inside the auth handler fallback resolver path.
		if authRegistry != nil {
			_, exists := authRegistry.GetRegistered(catCfg.AuthProvider)
			if !exists {
				logger.V(1).Info("auth provider not yet registered for catalog, skipping dynamic auth",
					"catalog", catCfg.Name,
					"authProvider", catCfg.AuthProvider)
			} else {
				remoteCfg.AuthProvider = catCfg.AuthProvider
				remoteCfg.AuthScope = catCfg.AuthScope
			}
		} else {
			// No auth registry available (e.g. API mode) — set provider name
			// directly; tokenprovider will route to the correct backend.
			remoteCfg.AuthProvider = catCfg.AuthProvider
			remoteCfg.AuthScope = catCfg.AuthScope
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
