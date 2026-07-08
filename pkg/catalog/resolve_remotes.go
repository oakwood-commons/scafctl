// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// RemoteCatalogsFromContext builds Catalog instances for every OCI remote catalog
// in the application config. These are suitable for passing to
// WithResolverRemoteCatalogs so the SolutionResolver can auto-pull artifacts
// that are not in the local catalog.
//
// Catalogs that fail to initialise (e.g. missing credentials) are silently
// skipped — the resolver will simply not search them.
func RemoteCatalogsFromContext(ctx context.Context, lgr logr.Logger) []Catalog {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return nil
	}

	credStore, credErr := NewCredentialStore(lgr)
	if credErr != nil {
		lgr.V(1).Info("credential store not available for remote catalogs", "error", credErr)
	}

	var remotes []Catalog
	for _, catCfg := range cfg.Catalogs {
		if catCfg.Type != config.CatalogTypeOCI || catCfg.URL == "" {
			continue
		}

		registry, repository := ParseCatalogURL(catCfg.URL)
		if registry == "" {
			continue
		}

		var handler auth.Handler
		if catCfg.AuthProvider != "" {
			if h, err := auth.GetHandler(ctx, catCfg.AuthProvider); err == nil {
				handler = h
			}
		}

		rc, err := NewRemoteCatalog(RemoteCatalogConfig{
			Name:            catCfg.Name,
			Registry:        registry,
			Repository:      repository,
			CredentialStore: credStore,
			AuthHandler:     handler,
			AuthScope:       catCfg.AuthScope,
			Logger:          lgr,
		})
		if err != nil {
			lgr.V(1).Info("failed to create remote catalog for auto-pull", "name", catCfg.Name, "error", err)
			continue
		}

		remotes = append(remotes, rc)
	}

	return remotes
}
