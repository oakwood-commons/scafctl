// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// PluginResolver resolves plugin artifacts from the catalog.
type PluginResolver interface {
	// ResolvePlugin resolves a plugin by name and kind, returning its metadata.
	// If version constraint is non-empty, the resolver should pick the best
	// matching version. Returns the artifact info with the resolved version and digest.
	ResolvePlugin(ctx context.Context, name string, kind catalog.ArtifactKind, versionConstraint string) (catalog.ArtifactInfo, error)
}

// VendorPluginsOptions configures the plugin vendoring process.
type VendorPluginsOptions struct {
	// PluginResolver resolves plugins from the catalog.
	// If nil, plugin vendoring is skipped.
	PluginResolver PluginResolver

	// PlatformCatalog, if set, is used to obtain the binary content digest
	// for multi-platform (OCI image index) plugins. Without this, the lock
	// file records the OCI manifest/index digest, which differs from the
	// content hash used for runtime verification.
	PlatformCatalog catalog.PlatformAwareCatalog

	// Platform is the target platform string (e.g., "darwin/arm64") used with
	// PlatformCatalog to resolve platform-specific content digests.
	Platform string
}

// VendorPluginsResult describes the outcome of plugin vendoring.
type VendorPluginsResult struct {
	// ResolvedPlugins contains the lock entries for resolved plugins.
	ResolvedPlugins []LockPlugin
}

// VendorPlugins resolves plugin dependencies against the catalog and records
// them in the lock file for reproducible builds. Unlike solution vendoring,
// plugins are not downloaded during build — only their versions and digests
// are pinned. The runtime fetches plugin binaries as needed.
func VendorPlugins(ctx context.Context, plugins []solution.PluginDependency, existingLock *LockFile, opts VendorPluginsOptions) (*VendorPluginsResult, error) {
	if opts.PluginResolver == nil {
		return &VendorPluginsResult{}, nil
	}

	lgr := logger.FromContext(ctx)
	result := &VendorPluginsResult{
		ResolvedPlugins: make([]LockPlugin, 0, len(plugins)),
	}

	for _, p := range plugins {
		kind := pluginKindToArtifactKind(p.Kind)

		// Check existing lock file for replay
		if existingLock != nil {
			if locked := existingLock.FindPlugin(p.Name, string(p.Kind)); locked != nil {
				// Verify the locked version still satisfies the constraint
				satisfies, err := CheckVersionConstraint(p.Version, locked.Version)
				if err == nil && satisfies {
					lgr.V(1).Info("replaying plugin from lock file",
						"name", p.Name,
						"kind", p.Kind,
						"version", locked.Version,
						"digest", locked.Digest)
					result.ResolvedPlugins = append(result.ResolvedPlugins, *locked)
					continue
				}
				lgr.V(1).Info("lock file plugin entry stale, re-resolving",
					"name", p.Name,
					"kind", p.Kind,
					"constraint", p.Version,
					"lockedVersion", locked.Version)
			}
		}

		// Resolve from catalog
		info, err := opts.PluginResolver.ResolvePlugin(ctx, p.Name, kind, p.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve plugin %s (%s): %w", p.Name, p.Kind, err)
		}

		resolvedVersion := ""
		if info.Reference.Version != nil {
			resolvedVersion = info.Reference.Version.String()
		}

		// Verify the resolved version satisfies the constraint
		if resolvedVersion != "" {
			satisfies, err := CheckVersionConstraint(p.Version, resolvedVersion)
			if err != nil {
				return nil, fmt.Errorf("failed to check version constraint for plugin %s: %w", p.Name, err)
			}
			if !satisfies {
				return nil, fmt.Errorf("resolved version %s for plugin %s does not satisfy constraint %s", resolvedVersion, p.Name, p.Version)
			}
		}

		// Compute a digest from the artifact info
		digest := info.Digest

		// If the artifact is a multi-platform image index, the manifest-level
		// digest does not match the binary content hash. Use FetchByPlatform
		// to get the actual content digest for the target platform.
		if opts.PlatformCatalog != nil && opts.Platform != "" {
			ref, refErr := catalog.ParseReference(kind, fmt.Sprintf("%s@%s", p.Name, resolvedVersion))
			if refErr == nil {
				_, contentInfo, fetchErr := opts.PlatformCatalog.FetchByPlatform(ctx, ref, opts.Platform)
				if fetchErr == nil {
					lgr.V(1).Info("resolved content digest via platform fetch",
						"name", p.Name,
						"indexDigest", digest,
						"contentDigest", contentInfo.Digest)
					digest = contentInfo.Digest
				} else {
					lgr.V(1).Info("platform fetch for content digest failed, using manifest digest",
						"name", p.Name,
						"error", fetchErr)
				}
			}
		}

		if digest == "" {
			// Fall back to hashing the version string
			digest = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(resolvedVersion)))
		}

		lockEntry := LockPlugin{
			Name:         p.Name,
			Kind:         string(p.Kind),
			Version:      resolvedVersion,
			Digest:       digest,
			ResolvedFrom: info.Catalog,
		}

		lgr.V(1).Info("resolved plugin",
			"name", p.Name,
			"kind", p.Kind,
			"version", resolvedVersion,
			"digest", digest,
			"catalog", info.Catalog)

		result.ResolvedPlugins = append(result.ResolvedPlugins, lockEntry)
	}

	return result, nil
}

// pluginKindToArtifactKind converts a solution.PluginKind to a catalog.ArtifactKind.
func pluginKindToArtifactKind(kind solution.PluginKind) catalog.ArtifactKind {
	switch kind {
	case solution.PluginKindProvider:
		return catalog.ArtifactKindProvider
	case solution.PluginKindAuthHandler:
		return catalog.ArtifactKindAuthHandler
	default:
		return catalog.ArtifactKind(string(kind))
	}
}
