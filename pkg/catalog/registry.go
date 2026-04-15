// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
)

// NewRegistry creates a registry with the built-in local catalog.
func NewRegistry(logger logr.Logger) (*Registry, error) {
	local, err := NewLocalCatalog(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create local catalog: %w", err)
	}

	return &Registry{
		local:    local,
		catalogs: []Catalog{local},
		logger:   logger.WithName("registry"),
	}, nil
}

// NewRegistryWithLocal creates a registry with a custom local catalog.
// Use for testing with a custom catalog directory.
func NewRegistryWithLocal(local *LocalCatalog, logger logr.Logger) *Registry {
	return &Registry{
		local:    local,
		catalogs: []Catalog{local},
		logger:   logger.WithName("registry"),
	}
}

// Registry manages multiple catalogs and provides unified access.
// The local catalog is always first in resolution order.
type Registry struct {
	local                *LocalCatalog
	catalogs             []Catalog
	logger               logr.Logger
	mu                   sync.RWMutex
	cacheRemoteArtifacts bool
}

// Local returns the built-in local catalog.
func (r *Registry) Local() *LocalCatalog {
	return r.local
}

// AddCatalog adds a catalog to the registry.
// Catalogs are searched in the order they are added (after local).
func (r *Registry) AddCatalog(catalog Catalog) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.catalogs = append(r.catalogs, catalog)
}

// SetCacheRemoteArtifacts enables or disables auto-caching of remote catalog
// fetches into the local catalog. When enabled, artifacts fetched from remote
// catalogs are automatically stored locally so subsequent fetches are served
// from the local catalog without network access.
func (r *Registry) SetCacheRemoteArtifacts(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cacheRemoteArtifacts = enabled
}

// isRemoteCatalog returns true if the catalog is not the local catalog.
// Compares by path to handle cases where multiple LocalCatalog instances
// might share the same name but point to different directories.
func (r *Registry) isRemoteCatalog(cat Catalog) bool {
	type pather interface {
		Path() string
	}
	lp, lok := interface{}(r.local).(pather)
	cp, cok := cat.(pather)
	if lok && cok {
		return lp.Path() != cp.Path()
	}
	return cat.Name() != r.local.Name()
}

// Catalogs returns all registered catalogs.
func (r *Registry) Catalogs() []Catalog {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Catalog, len(r.catalogs))
	copy(result, r.catalogs)
	return result
}

// Resolve finds an artifact in the first catalog that has it.
// Searches catalogs in order: local first, then configured catalogs.
func (r *Registry) Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	r.mu.RLock()
	catalogs := r.catalogs
	r.mu.RUnlock()

	var lastErr error
	for _, catalog := range catalogs {
		info, err := catalog.Resolve(ctx, ref)
		if err == nil {
			r.logger.V(1).Info("resolved artifact",
				"name", ref.Name,
				"version", info.Reference.Version.String(),
				"catalog", catalog.Name())
			return info, nil
		}

		if IsArtifactNotFoundError(err) {
			lastErr = err
			continue
		}

		// Non-not-found error - return immediately
		return ArtifactInfo{}, err
	}

	if lastErr != nil {
		return ArtifactInfo{}, lastErr
	}

	return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: "registry"}
}

// Fetch retrieves an artifact from the first catalog that has it.
// If cacheRemoteArtifacts is enabled, artifacts fetched from remote catalogs
// are automatically stored in the local catalog for subsequent offline access.
func (r *Registry) Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	r.mu.RLock()
	catalogs := r.catalogs
	cacheEnabled := r.cacheRemoteArtifacts
	r.mu.RUnlock()

	var lastErr error
	for _, catalog := range catalogs {
		content, info, err := catalog.Fetch(ctx, ref)
		if err == nil {
			r.logger.V(1).Info("fetched artifact",
				"name", ref.Name,
				"version", info.Reference.Version.String(),
				"catalog", catalog.Name())

			// Auto-cache remote fetches into local catalog
			if cacheEnabled && r.isRemoteCatalog(catalog) {
				r.cacheArtifact(ctx, info.Reference, content, nil, catalog.Name())
			}

			return content, info, nil
		}

		if IsArtifactNotFoundError(err) {
			lastErr = err
			continue
		}

		// Non-not-found error - return immediately
		return nil, ArtifactInfo{}, err
	}

	if lastErr != nil {
		return nil, ArtifactInfo{}, lastErr
	}

	return nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: "registry"}
}

// FetchWithBundle retrieves an artifact with its bundle layer from the first catalog that has it.
// If cacheRemoteArtifacts is enabled, artifacts fetched from remote catalogs
// are automatically stored in the local catalog for subsequent offline access.
func (r *Registry) FetchWithBundle(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	r.mu.RLock()
	catalogs := r.catalogs
	cacheEnabled := r.cacheRemoteArtifacts
	r.mu.RUnlock()

	var lastErr error
	for _, cat := range catalogs {
		content, bundleData, info, err := cat.FetchWithBundle(ctx, ref)
		if err == nil {
			r.logger.V(1).Info("fetched artifact with bundle",
				"name", ref.Name,
				"version", info.Reference.Version.String(),
				"catalog", cat.Name(),
				"hasBundle", len(bundleData) > 0)

			// Auto-cache remote fetches into local catalog
			if cacheEnabled && r.isRemoteCatalog(cat) {
				r.cacheArtifact(ctx, info.Reference, content, bundleData, cat.Name())
			}

			return content, bundleData, info, nil
		}

		if IsArtifactNotFoundError(err) {
			lastErr = err
			continue
		}

		// Non-not-found error - return immediately
		return nil, nil, ArtifactInfo{}, err
	}

	if lastErr != nil {
		return nil, nil, ArtifactInfo{}, lastErr
	}

	return nil, nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: "registry"}
}

// cacheArtifact stores a remotely-fetched artifact into the local catalog.
// Errors are logged but do not fail the fetch — caching is best-effort.
func (r *Registry) cacheArtifact(ctx context.Context, ref Reference, content, bundleData []byte, sourceCatalog string) {
	annotations := NewAnnotationBuilder().
		Set(AnnotationSource, "auto-cached").
		Set(AnnotationOrigin, fmt.Sprintf("auto-cached from %s", sourceCatalog)).
		Build()

	// Use force=true to update if a stale version already exists locally
	_, err := r.local.Store(ctx, ref, content, bundleData, annotations, true)
	if err != nil {
		r.logger.V(1).Info("failed to cache remote artifact locally (non-fatal)",
			"name", ref.Name,
			"error", err)
		return
	}

	r.logger.V(1).Info("cached remote artifact locally",
		"name", ref.Name,
		"version", ref.Version.String())
}

// List returns all artifacts matching the criteria from all catalogs.
func (r *Registry) List(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error) {
	r.mu.RLock()
	catalogs := r.catalogs
	r.mu.RUnlock()

	var results []ArtifactInfo
	for _, catalog := range catalogs {
		artifacts, err := catalog.List(ctx, kind, name)
		if err != nil {
			r.logger.Error(err, "failed to list from catalog", "catalog", catalog.Name())
			continue
		}
		results = append(results, artifacts...)
	}

	return results, nil
}
