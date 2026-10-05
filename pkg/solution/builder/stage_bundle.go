// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
)

// readFiles reads every file to bundle once, so the build fingerprint and
// the archive are computed from the same bytes. Vendored dependencies are
// taken from memory rather than read back from disk.
func (p *Packager) readFiles(_ context.Context, st *pipelineState) (bool, error) {
	opts := []bundler.SnapshotOption{bundler.WithSnapshotMaxSize(p.archive.Limits.MaxSize)}
	if st.deps != nil {
		opts = append(opts, bundler.WithSnapshotContents(st.deps.Contents()))
	}
	files, err := bundler.ReadFileSnapshot(p.src.BundleRoot, st.discovery.LocalFiles, opts...)
	if err != nil {
		return false, fmt.Errorf("failed to read bundle files: %w", err)
	}
	st.files = files
	return false, nil
}

// lookupBuildCache fingerprints the build and finishes the pipeline when an
// identical build is already in the catalog.
func (p *Packager) lookupBuildCache(ctx context.Context, st *pipelineState) (bool, error) {
	lgr := logger.FromContext(ctx)
	build := st.result.Build
	fp := bundler.ComputeBuildFingerprint(st.content, st.files,
		collectPluginEntries(p.src.Solution), lockDigest(build.LockData),
		solutionAnnotations(p.src.Solution, p.provenance()))
	build.BuildFingerprint = fp
	build.InputFileCount = len(st.discovery.LocalFiles)

	entry, hit := bundler.CheckBuildCache(p.cacheDir(), fp)
	if !hit || !p.cachedArtifactExists(ctx, entry) {
		lgr.V(1).Info("build cache miss", "fingerprint", fp)
		return false, nil
	}
	build.CacheHit, build.CacheEntry = true, entry
	return true, nil
}

// cachedArtifactExists reports whether the artifact a build-cache entry
// points at is still the built copy in the catalog. It may have been
// deleted or rebuilt since the entry was written.
func (p *Packager) cachedArtifactExists(ctx context.Context, entry *bundler.BuildCacheEntry) bool {
	version, err := semver.NewVersion(entry.ArtifactVersion)
	if err != nil {
		return false
	}
	info, err := ResolveBuiltSolution(ctx, p.catalog, entry.ArtifactName, version)
	if err != nil || info.Digest != entry.ArtifactDigest {
		logger.FromContext(ctx).V(1).Info("build cache entry is stale, rebuilding",
			"name", entry.ArtifactName, "version", entry.ArtifactVersion)
		return false
	}
	return true
}

// lockDigest returns the fingerprint input for the lock layer; empty when
// there is no lock.
func lockDigest(lockData []byte) string {
	if len(lockData) == 0 {
		return ""
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(lockData))
}

// archiveBundle packs the discovered files into a deduplicated (v2) or
// single-tar (v1) bundle.
func (p *Packager) archiveBundle(_ context.Context, st *pipelineState) (bool, error) {
	if p.archive.Dedupe {
		return false, p.archiveDeduplicated(st)
	}
	return false, p.archiveTar(st)
}

// archiveDeduplicated creates a content-addressable bundle: large files as
// their own layers, small files in one tar.
func (p *Packager) archiveDeduplicated(st *pipelineState) error {
	build := st.result.Build
	dedup, err := bundler.CreateDeduplicatedBundle(st.files,
		collectPluginEntries(p.src.Solution),
		bundler.WithDedupeThreshold(p.archive.Limits.DedupeThreshold))
	if err != nil {
		return fmt.Errorf("failed to create deduplicated bundle: %w", err)
	}
	build.Dedup = dedup
	build.InputFileCount = len(st.discovery.LocalFiles)
	st.addMessagef("Bundled %d file(s) (%s, deduplicated: %d layer(s))",
		len(dedup.Manifest.Files), format.Bytes(dedup.TotalSize), len(dedup.LargeBlobs)+1) // +1 for the small-files tar
	return nil
}

// archiveTar creates a single tar bundle.
func (p *Packager) archiveTar(st *pipelineState) error {
	build := st.result.Build
	tarData, manifest, err := bundler.CreateBundleTar(st.files, collectPluginEntries(p.src.Solution))
	if err != nil {
		return fmt.Errorf("failed to create bundle: %w", err)
	}
	build.TarData = tarData
	build.InputFileCount = len(st.discovery.LocalFiles)
	st.addMessagef("Bundled %d file(s) (%s)", len(manifest.Files), format.Bytes(int64(len(tarData))))
	return nil
}

// writeBuildCache records the published artifact under the build
// fingerprint, so an unchanged rebuild is a cache hit.
func (p *Packager) writeBuildCache(ctx context.Context, st *pipelineState) (bool, error) {
	stored := st.result.Stored
	stored.CacheWritten = writeBuildCacheEntry(ctx, p.cacheDir(), st.result.Build, stored.Info)
	return false, nil
}
