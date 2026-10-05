// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// AuthProviderForRegistry returns the auth handler name for a registry host,
// derived from the host alone: the authProvider of a configured catalog on the
// same host, otherwise the handler inferred from the host (InferAuthHandler).
// It returns "" when neither applies.
//
// It never falls back to the default catalog's authProvider: a handler minted
// for one registry must not have its token sent to an unrelated host.
func AuthProviderForRegistry(ctx context.Context, registry string) string {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return InferAuthHandler(registry, nil)
	}
	for _, cat := range cfg.Catalogs {
		if cat.URL == "" || cat.AuthProvider == "" {
			continue
		}
		if host, _ := ParseCatalogURL(cat.URL); host == registry {
			return cat.AuthProvider
		}
	}
	return InferAuthHandler(registry, cfg.Auth.CustomOAuth2)
}

// AuthHandlerForRegistry returns the auth handler named by
// AuthProviderForRegistry, or nil when there is none or it is not registered.
// A nil handler is not an error: the credential store still applies.
func AuthHandlerForRegistry(ctx context.Context, registry string) auth.Handler {
	name := AuthProviderForRegistry(ctx, registry)
	if name == "" {
		return nil
	}
	handler, err := auth.GetHandler(ctx, name)
	if err != nil {
		return nil
	}
	return handler
}

// AuthScopeForRegistry returns the authScope of a configured catalog on the
// same registry host, otherwise the default scope inferred for well-known
// registries (InferDefaultScope, e.g. GCP Artifact Registry).
func AuthScopeForRegistry(ctx context.Context, registry string) string {
	if cfg := config.FromContext(ctx); cfg != nil {
		for _, cat := range cfg.Catalogs {
			if cat.URL == "" || cat.AuthScope == "" {
				continue
			}
			if host, _ := ParseCatalogURL(cat.URL); host == registry {
				return cat.AuthScope
			}
		}
	}
	return InferDefaultScope(registry)
}

// ContextRemoteFactory returns a RemoteFactory that builds a RemoteCatalog for
// a "registry[/repository]" origin that is not a configured catalog, so a
// fully qualified reference can be fetched without configuring its registry.
//
// Auth comes from the registry host only (the credential store plus
// AuthHandlerForRegistry / AuthScopeForRegistry), and the remote is always
// HTTPS: a registry that needs plain HTTP must be configured as a catalog with
// insecure set.
func ContextRemoteFactory(lgr logr.Logger) RemoteFactory {
	return func(ctx context.Context, remoteID string) (SolutionRemote, error) {
		registry, repository := ParseCatalogURL(remoteID)
		if registry == "" {
			return nil, fmt.Errorf("invalid remote %q: no registry", remoteID)
		}

		credStore, err := NewCredentialStore(lgr)
		if err != nil {
			lgr.V(1).Info("credential store not available, using anonymous auth", "remote", remoteID, "error", err)
		}

		rc, err := NewRemoteCatalog(RemoteCatalogConfig{
			Registry:        registry,
			Repository:      repository,
			CredentialStore: credStore,
			AuthHandler:     AuthHandlerForRegistry(ctx, registry),
			AuthScope:       AuthScopeForRegistry(ctx, registry),
			Logger:          lgr,
		})
		if err != nil {
			return nil, fmt.Errorf("creating remote catalog for %q: %w", remoteID, err)
		}
		return rc, nil
	}
}
