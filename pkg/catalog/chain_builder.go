// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// BuildCatalogChain creates a ChainCatalog from the application configuration.
//
// The chain order is deterministic:
//  1. Local filesystem catalog (always first)
//  2. Embedder/user catalogs from config (in config order, excluding reserved names)
//  3. Official catalog (always last, unless disabled via settings.disableOfficialCatalog)
//
// If authRegistry is provided, catalogs with an authProvider field will use
// the corresponding auth handler for dynamic token injection.
func BuildCatalogChain(cfg *config.Config, authRegistry *auth.Registry, logger logr.Logger) (*ChainCatalog, error) {
	var catalogs []Catalog

	// 1. Local catalog always comes first.
	localCat, err := NewLocalCatalog(logger)
	if err != nil {
		logger.V(1).Info("local catalog not available", "error", err)
	} else {
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

			remoteCat, remoteCatErr := buildRemoteCatalog(catCfg, credStore, authRegistry, logger)
			if remoteCatErr != nil {
				continue
			}
			catalogs = append(catalogs, remoteCat)
		}

		// 3. Official catalog always comes last (unless disabled).
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
		return nil, fmt.Errorf("no catalogs available (local catalog could not be initialized)")
	}

	return NewChainCatalog(logger, catalogs...)
}

// buildRemoteCatalog creates a RemoteCatalog from a CatalogConfig.
func buildRemoteCatalog(catCfg config.CatalogConfig, credStore *CredentialStore, authRegistry *auth.Registry, logger logr.Logger) (*RemoteCatalog, error) {
	registry, repository := ParseCatalogURL(catCfg.URL)

	remoteCfg := RemoteCatalogConfig{
		Name:              catCfg.Name,
		Registry:          registry,
		Repository:        repository,
		CredentialStore:   credStore,
		DiscoveryStrategy: catCfg.DiscoveryStrategy,
		Logger:            logger,
	}

	// Wire auth handler if configured
	if catCfg.AuthProvider != "" && authRegistry != nil {
		handler, err := authRegistry.Get(catCfg.AuthProvider)
		if err != nil {
			logger.V(1).Info("auth provider not found for catalog, skipping dynamic auth",
				"catalog", catCfg.Name,
				"authProvider", catCfg.AuthProvider,
				"error", err)
		} else {
			remoteCfg.AuthHandler = handler
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
