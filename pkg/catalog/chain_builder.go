// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// BuildCatalogChain creates a ChainCatalog from the application configuration.
// It always includes the local catalog first, then adds configured remote
// catalogs of type "oci". Returns the chain and a cleanup function.
func BuildCatalogChain(cfg *config.Config, logger logr.Logger) (*ChainCatalog, error) {
	var catalogs []Catalog

	// Local catalog always comes first
	localCat, err := NewLocalCatalog(logger)
	if err != nil {
		logger.V(1).Info("local catalog not available", "error", err)
	} else {
		catalogs = append(catalogs, localCat)
	}

	// Add configured remote catalogs
	if cfg != nil {
		credStore, credErr := NewCredentialStore(logger)
		if credErr != nil {
			logger.V(1).Info("credential store not available, remote catalogs will use anonymous auth", "error", credErr)
		}

		for _, catCfg := range cfg.Catalogs {
			if catCfg.Type != config.CatalogTypeOCI {
				continue
			}
			if catCfg.URL == "" {
				continue
			}

			remoteCat, err := NewRemoteCatalog(RemoteCatalogConfig{
				Name:            catCfg.Name,
				Registry:        catCfg.URL,
				Repository:      "",
				CredentialStore: credStore,
				Logger:          logger,
			})
			if err != nil {
				logger.V(1).Info("failed to create remote catalog, skipping",
					"catalog", catCfg.Name,
					"error", err)
				continue
			}
			catalogs = append(catalogs, remoteCat)
		}
	}

	if len(catalogs) == 0 {
		return nil, fmt.Errorf("no catalogs available (local catalog could not be initialized)")
	}

	return NewChainCatalog(logger, catalogs...)
}
