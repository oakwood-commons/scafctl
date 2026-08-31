// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
)

// AllowlistCatalog wraps any Catalog and rejects artifact names not in the
// allowed set. When the allowlist is nil, all artifacts are permitted
// (transparent pass-through). This enables per-catalog artifact restriction
// without modifying individual catalog implementations.
type AllowlistCatalog struct {
	inner   Catalog
	allowed map[string]bool // nil means allow all
}

var (
	_ Catalog              = (*AllowlistCatalog)(nil)
	_ PlatformAwareCatalog = (*AllowlistCatalog)(nil)
)

// NewAllowlistCatalog wraps a catalog with an artifact name allowlist.
// If allowedArtifacts is nil, all artifacts are permitted (transparent pass-through).
// If allowedArtifacts is non-nil but empty, all artifacts are denied.
func NewAllowlistCatalog(inner Catalog, allowedArtifacts []string) *AllowlistCatalog {
	var allowed map[string]bool
	if allowedArtifacts != nil {
		allowed = make(map[string]bool, len(allowedArtifacts))
		for _, name := range allowedArtifacts {
			allowed[name] = true
		}
	}
	return &AllowlistCatalog{
		inner:   inner,
		allowed: allowed,
	}
}

// isAllowed returns true if the artifact name is permitted by the allowlist.
func (c *AllowlistCatalog) isAllowed(name string) bool {
	return c.allowed == nil || c.allowed[name]
}

// Name returns the underlying catalog's name.
func (c *AllowlistCatalog) Name() string {
	return c.inner.Name()
}

// Store delegates to the underlying catalog without restriction.
// Allowlists govern reads (Resolve/Fetch), not writes.
func (c *AllowlistCatalog) Store(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool, extraLayers ...Layer) (ArtifactInfo, error) {
	return c.inner.Store(ctx, ref, content, bundleData, annotations, force, extraLayers...)
}

// Fetch retrieves an artifact, rejecting names not in the allowlist.
func (c *AllowlistCatalog) Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	if !c.isAllowed(ref.Name) {
		return nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.inner.Name()}
	}
	return c.inner.Fetch(ctx, ref)
}

// FetchWithBundle retrieves an artifact and bundle, rejecting names not in the allowlist.
func (c *AllowlistCatalog) FetchWithBundle(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	if !c.isAllowed(ref.Name) {
		return nil, nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.inner.Name()}
	}
	return c.inner.FetchWithBundle(ctx, ref)
}

// FetchWithLayer retrieves an artifact and auxiliary layers, rejecting names not in the allowlist.
func (c *AllowlistCatalog) FetchWithLayer(ctx context.Context, ref Reference, mediaTypes ...string) ([]byte, map[string][]byte, ArtifactInfo, error) {
	if !c.isAllowed(ref.Name) {
		return nil, nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.inner.Name()}
	}
	return c.inner.FetchWithLayer(ctx, ref, mediaTypes...)
}

// Resolve finds the best matching version, rejecting names not in the allowlist.
func (c *AllowlistCatalog) Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	if !c.isAllowed(ref.Name) {
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.inner.Name()}
	}
	return c.inner.Resolve(ctx, ref)
}

// List delegates to the underlying catalog. Listing is not restricted by
// the allowlist to preserve discovery/enumeration capabilities.
func (c *AllowlistCatalog) List(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error) {
	return c.inner.List(ctx, kind, name)
}

// Exists checks if an artifact exists, rejecting names not in the allowlist.
func (c *AllowlistCatalog) Exists(ctx context.Context, ref Reference) (bool, error) {
	if !c.isAllowed(ref.Name) {
		return false, nil
	}
	return c.inner.Exists(ctx, ref)
}

// Delete delegates to the underlying catalog without restriction.
// Allowlists govern reads, not mutations.
func (c *AllowlistCatalog) Delete(ctx context.Context, ref Reference) error {
	return c.inner.Delete(ctx, ref)
}

// FetchByPlatform fetches a platform-specific binary, rejecting names not in
// the allowlist. If the inner catalog does not implement PlatformAwareCatalog,
// returns an ArtifactNotFoundError.
func (c *AllowlistCatalog) FetchByPlatform(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error) {
	if !c.isAllowed(ref.Name) {
		return nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.inner.Name()}
	}
	pac, ok := c.inner.(PlatformAwareCatalog)
	if !ok {
		return nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.inner.Name()}
	}
	return pac.FetchByPlatform(ctx, ref, platform)
}

// ListPlatforms returns the platforms available for a multi-platform artifact,
// rejecting names not in the allowlist. If the inner catalog does not implement
// PlatformAwareCatalog, returns an ArtifactNotFoundError.
func (c *AllowlistCatalog) ListPlatforms(ctx context.Context, ref Reference) ([]string, error) {
	if !c.isAllowed(ref.Name) {
		return nil, &ArtifactNotFoundError{Reference: ref, Catalog: c.inner.Name()}
	}
	pac, ok := c.inner.(PlatformAwareCatalog)
	if !ok {
		return nil, &ArtifactNotFoundError{Reference: ref, Catalog: c.inner.Name()}
	}
	return pac.ListPlatforms(ctx, ref)
}

// ResolveContentDigest resolves the content-layer digest, rejecting names not
// in the allowlist. If the inner catalog does not implement
// PlatformAwareCatalog, returns an ArtifactNotFoundError.
func (c *AllowlistCatalog) ResolveContentDigest(ctx context.Context, ref Reference, platform, mediaType string) (ContentDigestInfo, error) {
	if !c.isAllowed(ref.Name) {
		return ContentDigestInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.inner.Name()}
	}
	pac, ok := c.inner.(PlatformAwareCatalog)
	if !ok {
		return ContentDigestInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.inner.Name()}
	}
	return pac.ResolveContentDigest(ctx, ref, platform, mediaType)
}

// Inner returns the wrapped catalog. Useful for type assertions on the
// underlying implementation when needed.
func (c *AllowlistCatalog) Inner() Catalog {
	return c.inner
}
