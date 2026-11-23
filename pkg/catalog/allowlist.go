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

// allowlistPluginSource restricts a PluginSource to an artifact name
// allowlist. Disallowed names report not-found so a strict chain falls
// through to the next source. A nil allowlist permits every name; an empty
// non-nil allowlist denies every name.
type allowlistPluginSource struct {
	inner   PluginSource
	allowed map[string]bool
}

// newAllowlistPluginSource wraps inner with an artifact name allowlist (see
// NewAllowlistCatalog for nil vs. empty semantics).
func newAllowlistPluginSource(inner PluginSource, allowedArtifacts []string) *allowlistPluginSource {
	var allowed map[string]bool
	if allowedArtifacts != nil {
		allowed = make(map[string]bool, len(allowedArtifacts))
		for _, name := range allowedArtifacts {
			allowed[name] = true
		}
	}
	return &allowlistPluginSource{inner: inner, allowed: allowed}
}

func (s *allowlistPluginSource) isAllowed(name string) bool {
	return s.allowed == nil || s.allowed[name]
}

func (s *allowlistPluginSource) notFound(ref Reference) error {
	return &ArtifactNotFoundError{Reference: ref, Catalog: s.inner.Name()}
}

func (s *allowlistPluginSource) Name() string {
	return s.inner.Name()
}

func (s *allowlistPluginSource) unwrapPluginSource() PluginSource {
	return s.inner
}

func (s *allowlistPluginSource) FetchByPlatform(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error) {
	if !s.isAllowed(ref.Name) {
		return nil, ArtifactInfo{}, s.notFound(ref)
	}
	return s.inner.FetchByPlatform(ctx, ref, platform)
}

func (s *allowlistPluginSource) ListPlatforms(ctx context.Context, ref Reference) ([]string, error) {
	if !s.isAllowed(ref.Name) {
		return nil, s.notFound(ref)
	}
	return s.inner.ListPlatforms(ctx, ref)
}

func (s *allowlistPluginSource) ResolveContentDigest(ctx context.Context, ref Reference, platform, mediaType string) (ContentDigestInfo, error) {
	if !s.isAllowed(ref.Name) {
		return ContentDigestInfo{}, s.notFound(ref)
	}
	return s.inner.ResolveContentDigest(ctx, ref, platform, mediaType)
}

func (s *allowlistPluginSource) Find(ctx context.Context, query ArtifactQuery) (ArtifactInfo, error) {
	if !s.isAllowed(query.Name) {
		return ArtifactInfo{}, s.notFound(Reference{Kind: query.Kind, Name: query.Name})
	}
	return s.inner.Find(ctx, query)
}

// KindFilterCatalog wraps any Catalog and immediately returns
// ArtifactNotFoundError for artifact kinds not in the allowed set. When
// allowedKinds is empty (nil or zero-length), all kinds are permitted.
type KindFilterCatalog struct {
	inner   Catalog
	allowed map[ArtifactKind]bool // nil means allow all kinds
}

var (
	_ Catalog              = (*KindFilterCatalog)(nil)
	_ PlatformAwareCatalog = (*KindFilterCatalog)(nil)
)

var allArtifactKinds = []ArtifactKind{
	ArtifactKindSolution,
	ArtifactKindProvider,
	ArtifactKindAuthHandler,
}

// NewKindFilterCatalog wraps a catalog with an artifact-kind filter.
// A nil allowedKinds slice permits all kinds; a non-nil empty slice denies all kinds.
func NewKindFilterCatalog(inner Catalog, allowedKinds []ArtifactKind) *KindFilterCatalog {
	var allowed map[ArtifactKind]bool
	if allowedKinds != nil {
		allowed = make(map[ArtifactKind]bool, len(allowedKinds))
		for _, kind := range allowedKinds {
			allowed[kind] = true
		}
	}
	return &KindFilterCatalog{
		inner:   inner,
		allowed: allowed,
	}
}

func (c *KindFilterCatalog) isAllowed(kind ArtifactKind) bool {
	return c.allowed == nil || c.allowed[kind]
}

func (c *KindFilterCatalog) allowedKindOrder() []ArtifactKind {
	var kinds []ArtifactKind
	for _, k := range allArtifactKinds {
		if c.allowed[k] {
			kinds = append(kinds, k)
		}
	}
	return kinds
}

func (c *KindFilterCatalog) notFound(ref Reference) error {
	return &ArtifactNotFoundError{Reference: ref, Catalog: c.inner.Name()}
}

// Name returns the underlying catalog's name.
func (c *KindFilterCatalog) Name() string {
	return c.inner.Name()
}

// Inner returns the wrapped catalog so callers such as catalogindex.FromCatalogs
// can unwrap decorators to inspect the concrete catalog identity.
func (c *KindFilterCatalog) Inner() Catalog {
	return c.inner
}

// Store delegates to the underlying catalog without restriction.
func (c *KindFilterCatalog) Store(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool, extraLayers ...Layer) (ArtifactInfo, error) {
	return c.inner.Store(ctx, ref, content, bundleData, annotations, force, extraLayers...)
}

// Fetch retrieves an artifact, rejecting unserved kinds without contacting the inner catalog.
func (c *KindFilterCatalog) Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	if ref.Kind == "" && c.allowed != nil {
		for _, k := range c.allowedKindOrder() {
			probed := ref
			probed.Kind = k
			data, info, err := c.inner.Fetch(ctx, probed)
			if err == nil || !IsNotFound(err) {
				return data, info, err
			}
		}
		return nil, ArtifactInfo{}, c.notFound(ref)
	}
	if !c.isAllowed(ref.Kind) {
		return nil, ArtifactInfo{}, c.notFound(ref)
	}
	return c.inner.Fetch(ctx, ref)
}

// FetchWithBundle retrieves an artifact and bundle, rejecting unserved kinds.
func (c *KindFilterCatalog) FetchWithBundle(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	if ref.Kind == "" && c.allowed != nil {
		for _, k := range c.allowedKindOrder() {
			probed := ref
			probed.Kind = k
			content, bundle, info, err := c.inner.FetchWithBundle(ctx, probed)
			if err == nil || !IsNotFound(err) {
				return content, bundle, info, err
			}
		}
		return nil, nil, ArtifactInfo{}, c.notFound(ref)
	}
	if !c.isAllowed(ref.Kind) {
		return nil, nil, ArtifactInfo{}, c.notFound(ref)
	}
	return c.inner.FetchWithBundle(ctx, ref)
}

// FetchWithLayer retrieves an artifact and auxiliary layers, rejecting unserved kinds.
func (c *KindFilterCatalog) FetchWithLayer(ctx context.Context, ref Reference, mediaTypes ...string) ([]byte, map[string][]byte, ArtifactInfo, error) {
	if ref.Kind == "" && c.allowed != nil {
		for _, k := range c.allowedKindOrder() {
			probed := ref
			probed.Kind = k
			content, layers, info, err := c.inner.FetchWithLayer(ctx, probed, mediaTypes...)
			if err == nil || !IsNotFound(err) {
				return content, layers, info, err
			}
		}
		return nil, nil, ArtifactInfo{}, c.notFound(ref)
	}
	if !c.isAllowed(ref.Kind) {
		return nil, nil, ArtifactInfo{}, c.notFound(ref)
	}
	return c.inner.FetchWithLayer(ctx, ref, mediaTypes...)
}

// Resolve finds the best matching version, rejecting unserved kinds.
func (c *KindFilterCatalog) Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	if ref.Kind == "" && c.allowed != nil {
		for _, k := range c.allowedKindOrder() {
			probed := ref
			probed.Kind = k
			info, err := c.inner.Resolve(ctx, probed)
			if err == nil || !IsNotFound(err) {
				return info, err
			}
		}
		return ArtifactInfo{}, c.notFound(ref)
	}
	if !c.isAllowed(ref.Kind) {
		return ArtifactInfo{}, c.notFound(ref)
	}
	return c.inner.Resolve(ctx, ref)
}

// List returns matching artifacts, skipping the inner catalog when a specific
// unserved kind is requested and filtering results when kind is empty.
func (c *KindFilterCatalog) List(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error) {
	if kind != "" && !c.isAllowed(kind) {
		return []ArtifactInfo{}, nil
	}
	if kind == "" && c.allowed != nil && len(c.allowed) == 0 {
		return []ArtifactInfo{}, nil
	}
	items, err := c.inner.List(ctx, kind, name)
	if err != nil || c.allowed == nil || kind != "" {
		return items, err
	}
	filtered := make([]ArtifactInfo, 0, len(items))
	for _, item := range items {
		if c.isAllowed(item.Reference.Kind) {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

// Exists checks if an artifact exists, returning false without error for unserved kinds.
func (c *KindFilterCatalog) Exists(ctx context.Context, ref Reference) (bool, error) {
	if ref.Kind == "" && c.allowed != nil {
		for _, k := range c.allowedKindOrder() {
			probed := ref
			probed.Kind = k
			exists, err := c.inner.Exists(ctx, probed)
			if err != nil || exists {
				return exists, err
			}
		}
		return false, nil
	}
	if !c.isAllowed(ref.Kind) {
		return false, nil
	}
	return c.inner.Exists(ctx, ref)
}

// Delete delegates to the underlying catalog without restriction.
func (c *KindFilterCatalog) Delete(ctx context.Context, ref Reference) error {
	return c.inner.Delete(ctx, ref)
}

// FetchByPlatform fetches a platform-specific binary, rejecting unserved kinds.
func (c *KindFilterCatalog) FetchByPlatform(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error) {
	if !c.isAllowed(ref.Kind) {
		return nil, ArtifactInfo{}, c.notFound(ref)
	}
	pac, ok := c.inner.(PlatformAwareCatalog)
	if !ok {
		return nil, ArtifactInfo{}, c.notFound(ref)
	}
	return pac.FetchByPlatform(ctx, ref, platform)
}

// ListPlatforms returns the platforms available for a multi-platform artifact, rejecting unserved kinds.
func (c *KindFilterCatalog) ListPlatforms(ctx context.Context, ref Reference) ([]string, error) {
	if !c.isAllowed(ref.Kind) {
		return nil, c.notFound(ref)
	}
	pac, ok := c.inner.(PlatformAwareCatalog)
	if !ok {
		return nil, c.notFound(ref)
	}
	return pac.ListPlatforms(ctx, ref)
}

// ResolveContentDigest resolves the content-layer digest, rejecting unserved kinds.
func (c *KindFilterCatalog) ResolveContentDigest(ctx context.Context, ref Reference, platform, mediaType string) (ContentDigestInfo, error) {
	if !c.isAllowed(ref.Kind) {
		return ContentDigestInfo{}, c.notFound(ref)
	}
	pac, ok := c.inner.(PlatformAwareCatalog)
	if !ok {
		return ContentDigestInfo{}, c.notFound(ref)
	}
	return pac.ResolveContentDigest(ctx, ref, platform, mediaType)
}

// kindFilterPluginSource restricts a PluginSource to a set of allowed artifact
// kinds. Unserved kinds immediately report ArtifactNotFoundError without network
// access so the source chain moves to the next catalog.
type kindFilterPluginSource struct {
	inner   PluginSource
	allowed map[ArtifactKind]bool
}

func newKindFilterPluginSource(inner PluginSource, allowedKinds []ArtifactKind) *kindFilterPluginSource {
	var allowed map[ArtifactKind]bool
	if allowedKinds != nil {
		allowed = make(map[ArtifactKind]bool, len(allowedKinds))
		for _, kind := range allowedKinds {
			allowed[kind] = true
		}
	}
	return &kindFilterPluginSource{inner: inner, allowed: allowed}
}

func (s *kindFilterPluginSource) isAllowed(kind ArtifactKind) bool {
	return s.allowed == nil || s.allowed[kind]
}

func (s *kindFilterPluginSource) notFound(ref Reference) error {
	return &ArtifactNotFoundError{Reference: ref, Catalog: s.inner.Name()}
}

func (s *kindFilterPluginSource) Name() string {
	return s.inner.Name()
}

func (s *kindFilterPluginSource) unwrapPluginSource() PluginSource {
	return s.inner
}

func (s *kindFilterPluginSource) FetchByPlatform(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error) {
	if !s.isAllowed(ref.Kind) {
		return nil, ArtifactInfo{}, s.notFound(ref)
	}
	return s.inner.FetchByPlatform(ctx, ref, platform)
}

func (s *kindFilterPluginSource) ListPlatforms(ctx context.Context, ref Reference) ([]string, error) {
	if !s.isAllowed(ref.Kind) {
		return nil, s.notFound(ref)
	}
	return s.inner.ListPlatforms(ctx, ref)
}

func (s *kindFilterPluginSource) ResolveContentDigest(ctx context.Context, ref Reference, platform, mediaType string) (ContentDigestInfo, error) {
	if !s.isAllowed(ref.Kind) {
		return ContentDigestInfo{}, s.notFound(ref)
	}
	return s.inner.ResolveContentDigest(ctx, ref, platform, mediaType)
}

func (s *kindFilterPluginSource) Find(ctx context.Context, query ArtifactQuery) (ArtifactInfo, error) {
	if !s.isAllowed(query.Kind) {
		return ArtifactInfo{}, s.notFound(Reference{Kind: query.Kind, Name: query.Name})
	}
	return s.inner.Find(ctx, query)
}
