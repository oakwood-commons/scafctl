// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// BuildCatalogChain creates a ChainCatalog from the application configuration.
// It always includes the local catalog first, then adds configured remote
// catalogs of type "oci". It returns the constructed chain catalog and any
// error encountered during initialization.
// If authRegistry is provided, catalogs with an authProvider field will use
// the corresponding auth handler for dynamic token injection.
func BuildCatalogChain(cfg *config.Config, authRegistry *auth.Registry, logger logr.Logger) (*ChainCatalog, error) {
	var catalogs []Catalog

	// Local catalog always comes first
	localCat, err := NewLocalCatalog(logger)
	if err != nil {
		logger.V(1).Info("local catalog not available", "error", err)
	} else {
		catalogs = append(catalogs, localCat)
	}

	// Add configured remote catalogs
	var credStore *CredentialStore
	if cfg != nil {
		cs, credErr := NewCredentialStore(logger)
		if credErr != nil {
			logger.V(1).Info("credential store not available, remote catalogs will use anonymous auth", "error", credErr)
		} else {
			credStore = cs
		}

		for _, catCfg := range cfg.Catalogs {
			if catCfg.Type != config.CatalogTypeOCI {
				continue
			}
			if catCfg.URL == "" {
				continue
			}

			remoteCfg := RemoteCatalogConfig{
				Name:            catCfg.Name,
				Registry:        catCfg.URL,
				Repository:      "",
				CredentialStore: credStore,
				Logger:          logger,
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
				continue
			}
			catalogs = append(catalogs, remoteCat)
		}
	}

	// Append the official catalog as the last resort unless disabled.
	// credStore may be nil when no config is loaded or credential setup failed;
	// NewRemoteCatalog treats nil CredentialStore as anonymous auth, which is
	// sufficient because the official catalog is a public OCI registry.
	disableOfficial := cfg != nil && cfg.Settings.DisableOfficialCatalog
	if !disableOfficial {
		officialRegistry, officialRepo := ParseCatalogURL(settings.OfficialCatalogURL)
		officialCat, officialErr := NewRemoteCatalog(RemoteCatalogConfig{
			Name:            settings.OfficialCatalogName,
			Registry:        officialRegistry,
			Repository:      officialRepo,
			CredentialStore: credStore,
			Logger:          logger,
		})
		if officialErr != nil {
			logger.V(1).Info("official catalog not available", "error", officialErr)
		} else {
			catalogs = append(catalogs, officialCat)
		}
	}

	if len(catalogs) == 0 {
		return nil, fmt.Errorf("no catalogs available (local catalog could not be initialized)")
	}

	return NewChainCatalog(logger, catalogs...)
}
