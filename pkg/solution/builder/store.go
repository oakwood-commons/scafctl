// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/cache"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// StoreOptions configures how a solution artifact is stored.
type StoreOptions struct {
	// Force overwrites an existing version in the catalog.
	Force bool `json:"force,omitempty" yaml:"force,omitempty" doc:"Overwrite existing version"`

	// Source is the origin of the solution (repository URL or file path), stored as org.opencontainers.image.source.
	Source string `json:"source,omitempty" yaml:"source,omitempty" doc:"Source repository URL or file path"`

	// DisplayName is the human-readable name, stored as an OCI annotation.
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-readable display name"`

	// Description is the solution description, stored as an OCI annotation.
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Solution description"`

	// Category classifies the solution, stored as an OCI annotation.
	Category string `json:"category,omitempty" yaml:"category,omitempty" doc:"Solution category"`

	// Tags are searchable keywords, stored as a comma-separated OCI annotation.
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Searchable tags"`

	// Annotations is a free-form map merged into OCI annotations.
	// Engine-set annotations take precedence over user-supplied ones.
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty" doc:"User-supplied annotations"`

	// ArtifactCacheDir is the path to the artifact cache directory.
	// When non-empty, the corresponding artifact cache entry is invalidated
	// after a successful store to prevent stale reads on subsequent runs.
	ArtifactCacheDir string `json:"-" yaml:"-" doc:"Artifact cache directory path"`

	// ArtifactCacheTTL is the TTL for the artifact cache.
	// Required when ArtifactCacheDir is set.
	ArtifactCacheTTL time.Duration `json:"-" yaml:"-" doc:"Artifact cache TTL"`

	// DeferBuildCache, when true, skips writing the build-cache entry during the
	// store. The caller is expected to call WriteBuildCacheEntry only after any
	// post-store verification passes, so a failed verification does not leave a
	// build-cache entry that would let a rerun hit a cached broken artifact and
	// exit 0 without re-verifying.
	DeferBuildCache bool `json:"-" yaml:"-" doc:"Skip writing the build cache entry during store"`
}

// StoreResult holds the outcome of a store operation.
type StoreResult struct {
	// Info is the catalog artifact info returned after storing.
	Info catalog.ArtifactInfo `json:"info" yaml:"info" doc:"Artifact info from the catalog"`

	// CacheWritten indicates whether a build cache entry was written.
	CacheWritten bool `json:"cacheWritten,omitempty" yaml:"cacheWritten,omitempty" doc:"Whether the build cache entry was written"`

	// cache holds just the metadata needed to write a deferred build-cache
	// entry after the caller has verified the stored artifact. It deliberately
	// does NOT retain the full *BuildResult, whose TarData/Dedup payload can be
	// large and is not needed once the artifact is stored.
	cache deferredCacheInfo
}

// deferredCacheInfo carries the minimal fields WriteBuildCacheEntry needs to
// construct a build-cache entry without retaining the whole build result.
type deferredCacheInfo struct {
	fingerprint    string
	cacheDir       string
	inputFileCount int
}

// StoreSolutionArtifact stores a built solution artifact in the local catalog,
// choosing between dedup (v2) and traditional (v1) storage based on the build
// result. It also writes a build cache entry when applicable.
func StoreSolutionArtifact(ctx context.Context, localCatalog *catalog.LocalCatalog, name string, version *semver.Version, content []byte, br *BuildResult, opts StoreOptions) (*StoreResult, error) {
	if version == nil {
		return nil, fmt.Errorf("version is required")
	}

	lgr := logger.FromContext(ctx)

	ref := catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    name,
		Version: version,
	}

	annotations := catalog.NewAnnotationBuilder().
		SetMap(opts.Annotations).
		Set(catalog.AnnotationSource, opts.Source).
		Set(catalog.AnnotationDisplayName, opts.DisplayName).
		Set(catalog.AnnotationDescription, opts.Description).
		Set(catalog.AnnotationCategory, opts.Category).
		SetTags(opts.Tags).
		Build()

	var info catalog.ArtifactInfo
	var err error

	if br != nil && br.Dedup != nil {
		blobLayers := make([][]byte, 0, len(br.Dedup.LargeBlobs))
		for _, blob := range br.Dedup.LargeBlobs {
			blobLayers = append(blobLayers, blob.Content)
		}
		info, err = localCatalog.StoreDedup(ctx, ref, content, br.Dedup.ManifestJSON, br.Dedup.SmallBlobsTar, blobLayers, annotations, opts.Force)
	} else {
		var bundleData []byte
		if br != nil {
			bundleData = br.TarData
		}
		info, err = localCatalog.Store(ctx, ref, content, bundleData, annotations, opts.Force)
	}

	if err != nil {
		return nil, err
	}

	result := &StoreResult{Info: info}
	if br != nil {
		result.cache = deferredCacheInfo{
			fingerprint:    br.BuildFingerprint,
			cacheDir:       br.BuildCacheDir,
			inputFileCount: br.InputFileCount,
		}
	}

	lgr.V(1).Info("built solution",
		"name", info.Reference.Name,
		"version", info.Reference.Version.String(),
		"digest", info.Digest)

	// Invalidate the artifact cache entry so subsequent runs fetch the
	// freshly built artifact instead of a stale cached version.
	if opts.ArtifactCacheDir != "" {
		versionStr := version.String()
		if cacheErr := cache.InvalidateArtifact(opts.ArtifactCacheDir, opts.ArtifactCacheTTL, string(catalog.ArtifactKindSolution), name, versionStr); cacheErr != nil {
			lgr.V(1).Info("failed to invalidate artifact cache (non-fatal)", "error", cacheErr)
		} else {
			lgr.V(1).Info("invalidated artifact cache entry", "name", name, "version", versionStr)
		}
	}

	// Write build cache entry after successful store (unless deferred to the
	// caller, which writes it only after post-store verification passes).
	// WriteBuildCacheEntry sets result.CacheWritten on success.
	if !opts.DeferBuildCache {
		WriteBuildCacheEntry(ctx, result)
	}

	return result, nil
}

// WriteBuildCacheEntry writes the build-cache entry for a stored artifact. It is
// separated from StoreSolutionArtifact so callers can defer the cache write
// until after post-store verification passes (see StoreOptions.DeferBuildCache):
// a build-cache entry must only exist for artifacts that verified successfully,
// otherwise a rerun would hit the cache and exit 0 without re-verifying a broken
// artifact. It returns true when an entry was written.
func WriteBuildCacheEntry(ctx context.Context, result *StoreResult) bool {
	lgr := logger.FromContext(ctx)
	if result == nil {
		return false
	}
	ci := result.cache
	if ci.fingerprint == "" || ci.cacheDir == "" {
		return false
	}
	info := result.Info
	cacheEntry := &bundler.BuildCacheEntry{
		Fingerprint:     ci.fingerprint,
		ArtifactName:    info.Reference.Name,
		ArtifactVersion: info.Reference.Version.String(),
		ArtifactDigest:  info.Digest,
		CreatedAt:       time.Now(),
		InputFiles:      ci.inputFileCount,
	}
	if cacheErr := bundler.WriteBuildCache(ci.cacheDir, ci.fingerprint, cacheEntry); cacheErr != nil {
		lgr.V(1).Info("failed to write build cache (non-fatal)", "error", cacheErr)
		return false
	}
	lgr.V(1).Info("wrote build cache entry", "fingerprint", ci.fingerprint)
	result.CacheWritten = true
	return true
}
