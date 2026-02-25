// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
)

// ChainCatalog tries each catalog in order, returning the first successful
// result. It implements the Catalog interface for read operations (Fetch,
// Resolve, List, Exists). Write operations (Store, Delete) are forwarded
// to the first catalog in the chain.
type ChainCatalog struct {
	catalogs []Catalog
	logger   logr.Logger
}

// NewChainCatalog creates a ChainCatalog that tries catalogs in order.
// At least one catalog must be provided.
func NewChainCatalog(logger logr.Logger, catalogs ...Catalog) (*ChainCatalog, error) {
	if len(catalogs) == 0 {
		return nil, fmt.Errorf("at least one catalog is required")
	}
	return &ChainCatalog{
		catalogs: catalogs,
		logger:   logger.WithName("chain-catalog"),
	}, nil
}

// Name returns a composite name.
func (c *ChainCatalog) Name() string {
	return "chain"
}

// Catalogs returns the underlying catalogs.
func (c *ChainCatalog) Catalogs() []Catalog {
	return c.catalogs
}

// Store delegates to the first catalog.
func (c *ChainCatalog) Store(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool) (ArtifactInfo, error) {
	return c.catalogs[0].Store(ctx, ref, content, bundleData, annotations, force)
}

// Fetch tries each catalog in order, returning the first successful result.
func (c *ChainCatalog) Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	var lastErr error
	for _, cat := range c.catalogs {
		content, info, err := cat.Fetch(ctx, ref)
		if err == nil {
			c.logger.V(1).Info("fetched artifact", "catalog", cat.Name(), "ref", ref.String())
			return content, info, nil
		}
		if !errors.Is(err, ErrArtifactNotFound) {
			c.logger.V(1).Info("catalog fetch error (non-404)", "catalog", cat.Name(), "ref", ref.String(), "error", err)
		}
		lastErr = err
	}
	return nil, ArtifactInfo{}, fmt.Errorf("artifact %q not found in any catalog: %w", ref.String(), lastErr)
}

// FetchWithBundle tries each catalog in order.
func (c *ChainCatalog) FetchWithBundle(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	var lastErr error
	for _, cat := range c.catalogs {
		content, bundle, info, err := cat.FetchWithBundle(ctx, ref)
		if err == nil {
			return content, bundle, info, nil
		}
		if !errors.Is(err, ErrArtifactNotFound) {
			c.logger.V(1).Info("catalog fetch error (non-404)", "catalog", cat.Name(), "ref", ref.String(), "error", err)
		}
		lastErr = err
	}
	return nil, nil, ArtifactInfo{}, fmt.Errorf("artifact %q not found in any catalog: %w", ref.String(), lastErr)
}

// Resolve tries each catalog in order, returning the first successful result.
func (c *ChainCatalog) Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	var lastErr error
	for _, cat := range c.catalogs {
		info, err := cat.Resolve(ctx, ref)
		if err == nil {
			c.logger.V(1).Info("resolved artifact", "catalog", cat.Name(), "ref", ref.String())
			return info, nil
		}
		if !errors.Is(err, ErrArtifactNotFound) {
			c.logger.V(1).Info("catalog resolve error (non-404)", "catalog", cat.Name(), "ref", ref.String(), "error", err)
		}
		lastErr = err
	}
	return ArtifactInfo{}, fmt.Errorf("artifact %q not found in any catalog: %w", ref.String(), lastErr)
}

// List returns artifacts from all catalogs (deduplicated by name+version).
func (c *ChainCatalog) List(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error) {
	seen := make(map[string]bool)
	var results []ArtifactInfo

	for _, cat := range c.catalogs {
		items, err := cat.List(ctx, kind, name)
		if err != nil {
			c.logger.V(1).Info("catalog list error", "catalog", cat.Name(), "error", err)
			continue
		}
		for _, item := range items {
			key := item.Reference.String()
			if !seen[key] {
				seen[key] = true
				results = append(results, item)
			}
		}
	}

	return results, nil
}

// Exists returns true if the artifact exists in any catalog.
func (c *ChainCatalog) Exists(ctx context.Context, ref Reference) (bool, error) {
	for _, cat := range c.catalogs {
		ok, err := cat.Exists(ctx, ref)
		if err != nil {
			continue
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// Delete delegates to the first catalog.
func (c *ChainCatalog) Delete(ctx context.Context, ref Reference) error {
	return c.catalogs[0].Delete(ctx, ref)
}
