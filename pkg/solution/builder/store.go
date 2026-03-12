// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"time"

	"github.com/Masterminds/semver/v3"
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
	lgr := logger.FromContext(ctx)

	ref := catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    name,
		Version: version,
	}

	annotations := catalog.NewAnnotationBuilder().
		Set(catalog.AnnotationSource, opts.Source).
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
