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
	local    *LocalCatalog
	catalogs []Catalog
	logger   logr.Logger
	mu       sync.RWMutex
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
func (r *Registry) Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	r.mu.RLock()
	catalogs := r.catalogs
	r.mu.RUnlock()

	var lastErr error
	for _, catalog := range catalogs {
		content, info, err := catalog.Fetch(ctx, ref)
		if err == nil {
			r.logger.V(1).Info("fetched artifact",
				"name", ref.Name,
				"version", info.Reference.Version.String(),
				"catalog", catalog.Name())
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
