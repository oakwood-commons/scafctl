// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/semver/v3"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/memory"

	"github.com/oakwood-commons/scafctl/pkg/cache"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// StoreResult holds the outcome of publishing a built solution.
type StoreResult struct {
	// Info is the catalog artifact info returned after storing.
	Info catalog.ArtifactInfo `json:"info" yaml:"info" doc:"Artifact info from the catalog"`

	// CacheWritten indicates whether a build cache entry was written.
	CacheWritten bool `json:"cacheWritten,omitempty" yaml:"cacheWritten,omitempty" doc:"Whether the build cache entry was written"`
}

// builtStore publishes a built artifact. *catalog.LocalCatalog satisfies it.
type builtStore interface {
	StoreBuilt(ctx context.Context, builder catalog.ArtifactBuilder, force bool) (catalog.ArtifactInfo, error)
}

// builtResolver resolves an artifact reference. *catalog.LocalCatalog
// satisfies it.
type builtResolver interface {
	Resolve(ctx context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error)
}

// BuiltSolutionRef returns the full-identity reference of a solution built
// into the local catalog. Built pins the origin, so lookups are a direct tag
// lookup that never matches (or becomes ambiguous with) a pulled copy of the
// same name@version.
func BuiltSolutionRef(name string, version *semver.Version) catalog.Reference {
	return catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    name,
		Version: version,
		Built:   true,
	}
}

// ResolveBuiltSolution returns the built copy of name@version from the local
// catalog, ignoring any pulled copies. It returns a not-found error when no
// built copy exists.
func ResolveBuiltSolution(ctx context.Context, resolver builtResolver, name string, version *semver.Version) (catalog.ArtifactInfo, error) {
	if version == nil {
		return catalog.ArtifactInfo{}, fmt.Errorf("version is required")
	}
	return resolver.Resolve(ctx, BuiltSolutionRef(name, version))
}

// solutionAnnotations returns the OCI annotations recorded for sol.
// fallbackSource is recorded when metadata.source is empty, so OCI
// provenance is never blank. Engine-set annotations take precedence over
// metadata.annotations.
func solutionAnnotations(sol *solution.Solution, fallbackSource string) map[string]string {
	meta := sol.Metadata
	src := meta.Source
	if src == "" {
		src = fallbackSource
	}
	return catalog.NewAnnotationBuilder().
		SetMap(meta.Annotations).
		Set(catalog.AnnotationSource, src).
		Set(catalog.AnnotationDisplayName, meta.DisplayName).
		Set(catalog.AnnotationDescription, meta.Description).
		Set(catalog.AnnotationCategory, meta.Category).
		SetTags(meta.Tags).
		Build()
}

// solutionArtifact assembles the OCI manifest for a built solution.
// Layer order is a positional contract read back by the catalog:
//
//	v1 (tar):   content, [bundle tar], [lock]
//	v2 (dedup): content, bundle manifest, [small-files tar], large blobs..., [lock]
//
// The lock layer always comes last and is located by media type, so it never
// shifts the dedup manifest's layer indices. A nil build stores the document
// only.
func solutionArtifact(ref catalog.Reference, content []byte, build *BuildResult, annotations map[string]string) *catalog.ManifestBuilder {
	layers := []catalog.Layer{{MediaType: catalog.MediaTypeSolutionContent, Data: content}}
	if build != nil {
		layers = append(layers, bundleLayers(build)...)
		layers = append(layers, lockLayers(build.LockData)...)
	}
	return catalog.NewManifestBuilder(ref).
		Layers(layers...).
		Annotations(annotations)
}

// bundleLayers returns the bundle layers of a build: the dedup layout when
// deduplicated, else the single tar, else nothing.
func bundleLayers(build *BuildResult) []catalog.Layer {
	switch {
	case build.Dedup != nil:
		return dedupLayers(build.Dedup)
	case len(build.TarData) > 0:
		return []catalog.Layer{{MediaType: catalog.MediaTypeSolutionBundle, Data: build.TarData}}
	default:
		return nil
	}
}

// dedupLayers returns the content-addressable bundle layers.
func dedupLayers(dedup *bundler.DedupeResult) []catalog.Layer {
	layers := []catalog.Layer{{MediaType: catalog.MediaTypeSolutionBundleManifest, Data: dedup.ManifestJSON}}
	if len(dedup.SmallBlobsTar) > 0 {
		layers = append(layers, catalog.Layer{MediaType: catalog.MediaTypeSolutionBundleSmallTar, Data: dedup.SmallBlobsTar})
	}
	for _, blob := range dedup.LargeBlobs {
		layers = append(layers, catalog.Layer{MediaType: catalog.MediaTypeSolutionBundleBlob, Data: blob.Content})
	}
	return layers
}

// lockLayers returns the lock layer, or nothing when there is no lock.
func lockLayers(lockData []byte) []catalog.Layer {
	if len(lockData) == 0 {
		return nil
	}
	return []catalog.Layer{{MediaType: catalog.MediaTypeSolutionLock, Data: lockData}}
}

// stagedArtifact is a built artifact held in memory, so it can be read back
// and verified before anything is written to the catalog.
type stagedArtifact struct {
	builder *catalog.ManifestBuilder
	store   *memory.Store
	desc    ocispec.Descriptor
}

// stageArtifact pushes the artifact into an in-memory store.
func stageArtifact(ctx context.Context, builder *catalog.ManifestBuilder) (*stagedArtifact, error) {
	store := memory.New()
	desc, err := builder.Push(ctx, store)
	if err != nil {
		return nil, fmt.Errorf("staging artifact: %w", err)
	}
	return &stagedArtifact{builder: builder, store: store, desc: desc}, nil
}

// read returns the staged solution document and its reassembled bundle
// (nil when the artifact has no bundle), as the catalog would serve them.
func (a *stagedArtifact) read(ctx context.Context) (doc, bundle []byte, err error) {
	return catalog.ReadSolutionArtifact(ctx, a.store, a.desc)
}

// publishArtifact stores the staged artifact in the catalog. The reported
// size is the solution document size, matching the catalog's Store contract.
func publishArtifact(ctx context.Context, store builtStore, staged *stagedArtifact, contentSize int, force bool) (catalog.ArtifactInfo, error) {
	info, err := store.StoreBuilt(ctx, staged.builder, force)
	if err != nil {
		return catalog.ArtifactInfo{}, err
	}
	info.Size = int64(contentSize)
	return info, nil
}

// invalidateArtifactCache drops the artifact cache entry for ref, so later
// runs read the freshly built artifact. Failures are logged, not returned.
func invalidateArtifactCache(ctx context.Context, ref catalog.Reference) {
	lgr := logger.FromContext(ctx)
	version := ref.Version.String()
	err := cache.InvalidateArtifact(paths.ArtifactCacheDir(), settings.DefaultArtifactCacheTTL, string(ref.Kind), ref.Name, version)
	if err != nil {
		lgr.V(1).Info("failed to invalidate artifact cache (non-fatal)", "error", err)
		return
	}
	lgr.V(1).Info("invalidated artifact cache entry", "name", ref.Name, "version", version)
}

// writeBuildCacheEntry records info in the build cache in dir under the
// build's fingerprint, so an unchanged rebuild is a cache hit. It returns
// whether an entry was written; failures are logged.
func writeBuildCacheEntry(ctx context.Context, dir string, build *BuildResult, info catalog.ArtifactInfo) bool {
	lgr := logger.FromContext(ctx)
	entry := &bundler.BuildCacheEntry{
		Fingerprint:     build.BuildFingerprint,
		ArtifactName:    info.Reference.Name,
		ArtifactVersion: info.Reference.Version.String(),
		ArtifactDigest:  info.Digest,
		CreatedAt:       time.Now(),
		InputFiles:      build.InputFileCount,
	}
	if err := bundler.WriteBuildCache(dir, build.BuildFingerprint, entry); err != nil {
		lgr.V(1).Info("failed to write build cache (non-fatal)", "error", err)
		return false
	}
	lgr.V(1).Info("wrote build cache entry", "fingerprint", build.BuildFingerprint)
	return true
}
