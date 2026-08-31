// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
)

// PlatformAwareCatalog extends Catalog with multi-platform image index support.
// Catalogs that store multi-platform artifacts (e.g. LocalCatalog) implement
// this interface to allow transparent platform-specific fetching.
type PlatformAwareCatalog interface {
	Catalog

	// FetchByPlatform fetches a plugin binary for the given platform,
	// transparently handling both single-platform manifests and
	// multi-platform image indexes.
	FetchByPlatform(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error)

	// ListPlatforms returns the platforms available for a multi-platform artifact.
	// Returns nil if the artifact is single-platform.
	ListPlatforms(ctx context.Context, ref Reference) ([]string, error)

	// ResolveContentDigest resolves the content-layer digest for a
	// platform-specific binary without downloading the blob. The mediaType
	// parameter selects which layer to read the digest from (e.g.
	// MediaTypeProviderBinary). For a multi-platform image index it selects
	// the platform-specific manifest; for a single-platform manifest the
	// platform argument is ignored.
	ResolveContentDigest(ctx context.Context, ref Reference, platform, mediaType string) (ContentDigestInfo, error)
}

// PluginFetcher fetches plugin binaries from a catalog with platform awareness.
// It resolves plugin references, selects the appropriate platform variant via
// the OCI image index (preferred) or AnnotationPlatform annotation (fallback),
// and returns the raw binary data.
type PluginFetcher struct {
	catalog   Catalog
	logger    logr.Logger
	byCatalog map[string]Catalog
}

// Option configures a PluginFetcher call.
type Option func(*fetcherOptions)

type fetcherOptions struct {
	catalog string
}

// WithCatalog restricts the operation to a single named catalog (explicit,
// no fallback). Empty is the default implicit/chain behavior.
func WithCatalog(name string) Option {
	return func(o *fetcherOptions) { o.catalog = name }
}

func applyOptions(opts []Option) fetcherOptions {
	var o fetcherOptions
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// targetCatalog selects the catalog an operation should run against. An empty
// name uses the full backing catalog (the chain), preserving the default
// ordered "try each in turn" fallback. A non-empty name selects that single
// catalog explicitly: if it is not configured, this is a hard error with no
// fallback, mirroring an explicit-registry pull.
func (f *PluginFetcher) targetCatalog(name string) (Catalog, error) {
	if name == "" {
		return f.catalog, nil
	}
	cat, ok := f.byCatalog[name]
	if !ok {
		return nil, fmt.Errorf("catalog %q not configured", name)
	}
	return cat, nil
}

// NewPluginFetcher creates a PluginFetcher backed by the given catalog.
func NewPluginFetcher(catalog Catalog, logger logr.Logger) *PluginFetcher {
	byCatalog := make(map[string]Catalog)
	if chain, ok := catalog.(*ChainCatalog); ok {
		for _, c := range chain.catalogs {
			byCatalog[c.Name()] = c
		}
	} else if catalog != nil {
		byCatalog[catalog.Name()] = catalog
	}
	return &PluginFetcher{
		catalog:   catalog,
		logger:    logger.WithName("plugin-fetcher"),
		byCatalog: byCatalog,
	}
}

// List returns all published versions of a plugin. It delegates to the backing
// catalog's List method, respecting the WithCatalog option to scope to a single
// configured catalog.
func (f *PluginFetcher) List(ctx context.Context, kind ArtifactKind, name string, opts ...Option) ([]ArtifactInfo, error) {
	o := applyOptions(opts)
	target, err := f.targetCatalog(o.catalog)
	if err != nil {
		return nil, err
	}
	return target.List(ctx, kind, name)
}

// ResolvePlugin resolves a plugin by name, kind, and version constraint,
// returning its artifact info. If versionConstraint is empty, the latest
// version is returned.
func (f *PluginFetcher) ResolvePlugin(ctx context.Context, name string, kind ArtifactKind, versionConstraint string, opts ...Option) (ArtifactInfo, error) {
	o := applyOptions(opts)
	target, err := f.targetCatalog(o.catalog)
	if err != nil {
		return ArtifactInfo{}, err
	}

	refStr := name
	if versionConstraint != "" {
		refStr = name + "@" + versionConstraint
	}

	ref, err := ParseReference(kind, refStr)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("invalid plugin reference %q: %w", refStr, err)
	}

	info, err := target.Resolve(ctx, ref)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("resolving plugin %s (%s): %w", name, kind, err)
	}

	f.logger.V(1).Info("resolved plugin",
		"name", name,
		"kind", kind,
		"version", info.Reference.Version,
		"catalog", info.Catalog)

	return info, nil
}

// FetchPlugin fetches a plugin binary for the given platform.
// It uses the following resolution strategy:
//  1. If the catalog implements PlatformAwareCatalog, use FetchByPlatform
//     which handles OCI image indexes (fat manifests) transparently.
//  2. Otherwise, fall back to listing artifacts and matching the
//     AnnotationPlatform annotation on individual manifests.
//  3. If no platform-specific artifact is found, attempt a direct fetch
//     (single-platform fallback).
func (f *PluginFetcher) FetchPlugin(ctx context.Context, name string, kind ArtifactKind, version, platform string, opts ...Option) ([]byte, ArtifactInfo, error) {
	o := applyOptions(opts)
	target, err := f.targetCatalog(o.catalog)
	if err != nil {
		return nil, ArtifactInfo{}, err
	}

	// Strategy 1: Use OCI image index via PlatformAwareCatalog
	if pac, ok := target.(PlatformAwareCatalog); ok {
		ref, err := f.buildRef(name, kind, version)
		if err == nil {
			data, info, err := pac.FetchByPlatform(ctx, ref, platform)
			if err == nil {
				f.logger.V(1).Info("fetched plugin via image index",
					"name", name,
					"version", version,
					"platform", platform)
				return data, info, nil
			}
			// If platform not found in an image index, don't fall back — the
			// artifact is explicitly multi-platform and the requested platform
			// is unavailable.
			if IsPlatformNotFound(err) {
				return nil, ArtifactInfo{}, err
			}
			f.logger.V(1).Info("image index fetch failed, trying annotation-based fallback",
				"name", name, "error", err)
		}
	}

	// Strategy 2: Annotation-based matching (legacy)
	artifacts, err := target.List(ctx, kind, name)
	if err != nil {
		f.logger.V(1).Info("could not list plugin artifacts for platform selection, falling back to direct fetch",
			"name", name, "error", err)
		return f.directFetch(ctx, target, name, kind, version)
	}

	// Look for a platform-specific artifact matching the requested version
	for _, artifact := range artifacts {
		if artifact.Reference.Version == nil {
			continue
		}
		if artifact.Reference.Version.String() != version {
			continue
		}
		artifactPlatform := artifact.Annotations[AnnotationPlatform]
		if artifactPlatform == platform {
			f.logger.V(1).Info("found platform-specific plugin artifact",
				"name", name,
				"version", version,
				"platform", platform,
				"catalog", artifact.Catalog)
			return f.fetchByInfo(ctx, target, artifact)
		}
	}

	// Strategy 3: Direct fetch fallback (single-platform)
	f.logger.V(1).Info("no platform-specific artifact found, falling back to direct fetch",
		"name", name,
		"version", version,
		"platform", platform)
	return f.directFetch(ctx, target, name, kind, version)
}

// directFetch fetches a plugin by constructing a direct reference.
func (f *PluginFetcher) directFetch(ctx context.Context, cat Catalog, name string, kind ArtifactKind, version string) ([]byte, ArtifactInfo, error) {
	refStr := name
	if version != "" {
		refStr = name + "@" + version
	}

	ref, err := ParseReference(kind, refStr)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("invalid plugin reference %q: %w", refStr, err)
	}

	content, info, err := cat.Fetch(ctx, ref)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("fetching plugin %s@%s: %w", name, version, err)
	}

	return content, info, nil
}

// fetchByInfo fetches a plugin using a known ArtifactInfo.
func (f *PluginFetcher) fetchByInfo(ctx context.Context, cat Catalog, info ArtifactInfo) ([]byte, ArtifactInfo, error) {
	content, fetchedInfo, err := cat.Fetch(ctx, info.Reference)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("fetching plugin %s: %w", info.Reference.String(), err)
	}
	return content, fetchedInfo, nil
}

// ResolveContentDigest resolves the content-layer digest for a plugin binary
// without downloading the blob. The target catalog must implement
// ContentDigestResolver.
func (f *PluginFetcher) ResolveContentDigest(ctx context.Context, name string, kind ArtifactKind, version, platform, mediaType string, opts ...Option) (ContentDigestInfo, error) {
	o := applyOptions(opts)
	target, err := f.targetCatalog(o.catalog)
	if err != nil {
		return ContentDigestInfo{}, err
	}
	pac, ok := target.(PlatformAwareCatalog)
	if !ok {
		return ContentDigestInfo{}, fmt.Errorf("catalog %q does not support content digest resolution", target.Name())
	}
	ref, err := f.buildRef(name, kind, version)
	if err != nil {
		return ContentDigestInfo{}, err
	}
	info, err := pac.ResolveContentDigest(ctx, ref, platform, mediaType)
	if err != nil {
		return ContentDigestInfo{}, fmt.Errorf("resolving content digest for %s (%s): %w", name, kind, err)
	}
	f.logger.V(1).Info("resolved content digest",
		"name", name,
		"kind", kind,
		"version", version,
		"platform", platform,
		"contentDigest", info.ContentDigest)
	return info, nil
}

// buildRef constructs a Reference from name, kind, and version string.
func (f *PluginFetcher) buildRef(name string, kind ArtifactKind, version string) (Reference, error) {
	refStr := name
	if version != "" {
		refStr = name + "@" + version
	}
	return ParseReference(kind, refStr)
}
