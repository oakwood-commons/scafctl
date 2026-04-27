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

	// Source is the path to the solution file, recorded as an annotation.
	Source string `json:"source,omitempty" yaml:"source,omitempty" doc:"Source file path for annotation"`

	// DisplayName is the human-readable name, stored as an OCI annotation.
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-readable display name"`

	// Description is the solution description, stored as an OCI annotation.
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Solution description"`

	// Category classifies the solution, stored as an OCI annotation.
	Category string `json:"category,omitempty" yaml:"category,omitempty" doc:"Solution category"`

	// Tags are searchable keywords, stored as a comma-separated OCI annotation.
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Searchable tags"`

	// ArtifactCacheDir is the path to the artifact cache directory.
	// When non-empty, the corresponding artifact cache entry is invalidated
	// after a successful store to prevent stale reads on subsequent runs.
	ArtifactCacheDir string `json:"-" yaml:"-" doc:"Artifact cache directory path"`

	// ArtifactCacheTTL is the TTL for the artifact cache.
	// Required when ArtifactCacheDir is set.
	ArtifactCacheTTL time.Duration `json:"-" yaml:"-" doc:"Artifact cache TTL"`
}

// StoreResult holds the outcome of a store operation.
type StoreResult struct {
	// Info is the catalog artifact info returned after storing.
	Info catalog.ArtifactInfo `json:"info" yaml:"info" doc:"Artifact info from the catalog"`

	// CacheWritten indicates whether a build cache entry was written.
	CacheWritten bool `json:"cacheWritten,omitempty" yaml:"cacheWritten,omitempty" doc:"Whether the build cache entry was written"`
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

	// Write build cache entry after successful store
	if br != nil && br.BuildFingerprint != "" && br.BuildCacheDir != "" {
		cacheEntry := &bundler.BuildCacheEntry{
			Fingerprint:     br.BuildFingerprint,
			ArtifactName:    info.Reference.Name,
			ArtifactVersion: info.Reference.Version.String(),
			ArtifactDigest:  info.Digest,
			CreatedAt:       time.Now(),
			InputFiles:      br.InputFileCount,
		}
		if cacheErr := bundler.WriteBuildCache(br.BuildCacheDir, br.BuildFingerprint, cacheEntry); cacheErr != nil {
			lgr.V(1).Info("failed to write build cache (non-fatal)", "error", cacheErr)
		} else {
			lgr.V(1).Info("wrote build cache entry", "fingerprint", br.BuildFingerprint)
			result.CacheWritten = true
		}
	}

	return result, nil
}
