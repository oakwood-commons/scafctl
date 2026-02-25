// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
)

// PluginFetcher fetches plugin binaries from a catalog with platform awareness.
// It resolves plugin references, selects the appropriate platform variant via
// the AnnotationPlatform annotation, and returns the raw binary data.
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
// It first tries to find a platform-specific artifact by listing all versions
// and matching the AnnotationPlatform annotation. If no platform-specific
// artifact is found, it falls back to the default (un-annotated) artifact.
func (f *PluginFetcher) FetchPlugin(ctx context.Context, name string, kind ArtifactKind, version, platform string) ([]byte, ArtifactInfo, error) {
	// List all artifacts for this plugin to find platform-specific variants
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

	// No platform-specific artifact found — try direct fetch (single-platform fallback)
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
