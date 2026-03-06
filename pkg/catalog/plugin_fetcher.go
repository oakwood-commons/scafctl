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
}

// PluginFetcher fetches plugin binaries from a catalog with platform awareness.
// It resolves plugin references, selects the appropriate platform variant via
// the OCI image index (preferred) or AnnotationPlatform annotation (fallback),
// and returns the raw binary data.
type PluginFetcher struct {
	catalog Catalog
	logger  logr.Logger
}

// NewPluginFetcher creates a PluginFetcher backed by the given catalog.
func NewPluginFetcher(catalog Catalog, logger logr.Logger) *PluginFetcher {
	return &PluginFetcher{
		catalog: catalog,
		logger:  logger.WithName("plugin-fetcher"),
	}
}

// ResolvePlugin resolves a plugin by name, kind, and version constraint,
// returning its artifact info. If versionConstraint is empty, the latest
// version is returned.
func (f *PluginFetcher) ResolvePlugin(ctx context.Context, name string, kind ArtifactKind, versionConstraint string) (ArtifactInfo, error) {
	refStr := name
	if versionConstraint != "" {
		refStr = name + "@" + versionConstraint
	}

	ref, err := ParseReference(kind, refStr)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("invalid plugin reference %q: %w", refStr, err)
	}

	info, err := f.catalog.Resolve(ctx, ref)
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
func (f *PluginFetcher) FetchPlugin(ctx context.Context, name string, kind ArtifactKind, version, platform string) ([]byte, ArtifactInfo, error) {
	// Strategy 1: Use OCI image index via PlatformAwareCatalog
	if pac, ok := f.catalog.(PlatformAwareCatalog); ok {
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
	artifacts, err := f.catalog.List(ctx, kind, name)
	if err != nil {
		f.logger.V(1).Info("could not list plugin artifacts for platform selection, falling back to direct fetch",
			"name", name, "error", err)
		return f.directFetch(ctx, name, kind, version)
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
			return f.fetchByInfo(ctx, artifact)
		}
	}

	// Strategy 3: Direct fetch fallback (single-platform)
	f.logger.V(1).Info("no platform-specific artifact found, falling back to direct fetch",
		"name", name,
		"version", version,
		"platform", platform)
	return f.directFetch(ctx, name, kind, version)
}

// directFetch fetches a plugin by constructing a direct reference.
func (f *PluginFetcher) directFetch(ctx context.Context, name string, kind ArtifactKind, version string) ([]byte, ArtifactInfo, error) {
	refStr := name
	if version != "" {
		refStr = name + "@" + version
	}

	ref, err := ParseReference(kind, refStr)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("invalid plugin reference %q: %w", refStr, err)
	}

	content, info, err := f.catalog.Fetch(ctx, ref)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("fetching plugin %s@%s: %w", name, version, err)
	}

	return content, info, nil
}

// fetchByInfo fetches a plugin using a known ArtifactInfo.
func (f *PluginFetcher) fetchByInfo(ctx context.Context, info ArtifactInfo) ([]byte, ArtifactInfo, error) {
	content, fetchedInfo, err := f.catalog.Fetch(ctx, info.Reference)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("fetching plugin %s: %w", info.Reference.String(), err)
	}
	return content, fetchedInfo, nil
}

// buildRef constructs a Reference from name, kind, and version string.
func (f *PluginFetcher) buildRef(name string, kind ArtifactKind, version string) (Reference, error) {
	refStr := name
	if version != "" {
		refStr = name + "@" + version
	}
	return ParseReference(kind, refStr)
}
