// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// platformManifest pairs a per-platform manifest chain with its platform.
type platformManifest struct {
	platform string
	builder  *ManifestBuilder
}

// IndexBuilder assembles an OCI image index (fat manifest) through a fluent
// chain. Each platform contributes one ManifestBuilder; pushing the index
// pushes every platform manifest (layers, config, manifest) and then the index
// that references them:
//
//	desc, err := NewIndexBuilder(ref).
//		Platform("linux/amd64", amd64Manifest).
//		Platform("darwin/arm64", arm64Manifest).
//		Annotations(metadata).
//		Push(ctx, store)
//
// The index's reference and creation time are stamped onto every platform
// manifest, together with that manifest's platform, so all blobs of the
// artifact share one identity. Index annotations go through the same
// pipeline as ManifestBuilder's.
type IndexBuilder struct {
	ref       Reference
	createdAt time.Time
	platforms []platformManifest
	metadata  map[string]string
	transform AnnotationTransformer
}

// NewIndexBuilder starts a new image index chain for an artifact identified
// by ref.
func NewIndexBuilder(ref Reference) *IndexBuilder {
	return &IndexBuilder{
		ref:       ref,
		createdAt: time.Now().UTC(),
		transform: copyAllAnnotations,
	}
}

// Reference returns the identity the index is built as.
func (b *IndexBuilder) Reference() Reference {
	return b.ref
}

// Platform adds a platform-specific manifest (e.g. "linux/amd64").
// The platform is recorded on the index entry descriptor and validated when
// the index is built. The manifest's reference and creation time are replaced
// by the index's when the index is built; the caller's builder is not mutated.
func (b *IndexBuilder) Platform(platform string, manifest *ManifestBuilder) *IndexBuilder {
	b.platforms = append(b.platforms, platformManifest{platform: platform, builder: manifest})
	return b
}

// Annotations sets the caller's metadata for the index blob. See
// ManifestBuilder.Annotations for the keys that cannot be set here.
func (b *IndexBuilder) Annotations(annotations map[string]string) *IndexBuilder {
	b.metadata = copyAnnotations(annotations)
	return b
}

// AnnotationTransformer configures how the caller's metadata is filtered
// before the identity is stamped. A nil transformer restores the default
// copy-all behavior.
func (b *IndexBuilder) AnnotationTransformer(transformer AnnotationTransformer) *IndexBuilder {
	if transformer == nil {
		b.transform = copyAllAnnotations
	} else {
		b.transform = transformer
	}
	return b
}

// CreatedAt overrides the creation time recorded in the index and in every
// platform manifest. Use it for reproducible digests.
func (b *IndexBuilder) CreatedAt(t time.Time) *IndexBuilder {
	b.createdAt = t.UTC()
	return b
}

// annotations runs the annotation pipeline for the index blob.
func (b *IndexBuilder) annotations() map[string]string {
	anns := b.transform(copyAnnotations(b.metadata))
	anns = contentIdentity(b.ref, b.createdAt, anns)
	return stripDescriptorOnly(anns)
}

// stamped returns a copy of the platform's manifest builder carrying the
// index's identity and creation time plus its own platform.
func (b *IndexBuilder) stamped(p platformManifest) *ManifestBuilder {
	child := *p.builder
	child.ref = b.ref
	child.createdAt = b.createdAt
	child.platform = p.platform
	return &child
}

// entries returns one index entry descriptor per platform, in chain order.
func (b *IndexBuilder) entries() ([]ocispec.Descriptor, error) {
	if len(b.platforms) == 0 {
		return nil, &BuilderError{
			Stage:   BuildStageAssemble,
			Builder: BuilderKindIndex,
			Detail:  "building index: no platforms",
		}
	}

	descs := make([]ocispec.Descriptor, len(b.platforms))
	for i, p := range b.platforms {
		ociPlatform, err := PlatformToOCI(p.platform)
		if err != nil {
			return nil, &BuilderError{
				Stage:    BuildStagePlatform,
				Builder:  BuilderKindIndex,
				Platform: p.platform,
				Detail:   fmt.Sprintf("invalid platform %q", p.platform),
				Cause:    err,
			}
		}
		if p.builder == nil {
			return nil, &BuilderError{
				Stage:    BuildStageAssemble,
				Builder:  BuilderKindIndex,
				Platform: p.platform,
				Detail:   fmt.Sprintf("building index: nil manifest for platform %q", p.platform),
			}
		}

		child := b.stamped(p)
		encoded, err := child.encode()
		if err != nil {
			return nil, &BuilderError{
				Stage:    BuildStageAssemble,
				Builder:  BuilderKindIndex,
				Platform: p.platform,
				Detail:   fmt.Sprintf("platform %s", p.platform),
				Cause:    err,
			}
		}

		desc := encoded.manifest.Descriptor()
		desc.Platform = ociPlatform
		desc.Annotations = encoded.annotations
		descs[i] = desc
	}
	return descs, nil
}

// Index builds the ocispec.Index from the chain without pushing.
func (b *IndexBuilder) Index() (ocispec.Index, error) {
	if err := validateBuildRef(b.ref); err != nil {
		return ocispec.Index{}, &BuilderError{
			Stage:   BuildStageAssemble,
			Builder: BuilderKindIndex,
			Detail:  "building index",
			Cause:   err,
		}
	}

	descs, err := b.entries()
	if err != nil {
		return ocispec.Index{}, err
	}

	return ocispec.Index{
		Versioned:   specs.Versioned{SchemaVersion: 2},
		MediaType:   ocispec.MediaTypeImageIndex,
		Manifests:   descs,
		Annotations: b.annotations(),
	}, nil
}

// Push writes every platform manifest and then the index to pusher and
// returns the index descriptor for tagging. Blobs that already exist are
// treated as success.
func (b *IndexBuilder) Push(ctx context.Context, pusher Pusher) (ocispec.Descriptor, error) {
	index, err := b.Index()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	for _, p := range b.platforms {
		if _, err := b.stamped(p).Push(ctx, pusher); err != nil {
			return ocispec.Descriptor{}, &BuilderError{
				Stage:    BuildStagePush,
				Builder:  BuilderKindIndex,
				Platform: p.platform,
				Detail:   fmt.Sprintf("pushing platform %s", p.platform),
				Cause:    err,
			}
		}
	}

	indexData, err := json.Marshal(index)
	if err != nil {
		return ocispec.Descriptor{}, &BuilderError{
			Stage:   BuildStageEncode,
			Builder: BuilderKindIndex,
			Detail:  "marshalling image index",
			Cause:   err,
		}
	}

	indexDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageIndex,
		Digest:    digest.FromBytes(indexData),
		Size:      int64(len(indexData)),
	}
	if err := pushIfAbsent(ctx, pusher, indexDesc, indexData); err != nil {
		return ocispec.Descriptor{}, &BuilderError{
			Stage:   BuildStagePush,
			Builder: BuilderKindIndex,
			Detail:  "pushing image index",
			Cause:   err,
		}
	}

	return indexDesc, nil
}
