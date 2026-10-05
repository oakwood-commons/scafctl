// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// ArtifactBuilder is implemented by both ManifestBuilder (single artifact)
// and IndexBuilder (multi-platform). Push writes every blob the artifact
// needs and returns the root descriptor (manifest or index) for tagging.
// Reference is the identity the artifact is built as; storing it derives the
// tag from the same reference, so content and tag can never disagree.
type ArtifactBuilder interface {
	Push(ctx context.Context, pusher Pusher) (ocispec.Descriptor, error)
	Reference() Reference
}

var (
	_ ArtifactBuilder = (*ManifestBuilder)(nil)
	_ ArtifactBuilder = (*IndexBuilder)(nil)
)

// AnnotationTransformer prepares annotations without mutating the caller's
// map. Builders and taggers apply it to caller-supplied annotations before
// stamping the keys they own; see DropAnnotations for the stock transformer.
type AnnotationTransformer func(map[string]string) map[string]string

func copyAllAnnotations(annotations map[string]string) map[string]string {
	return copyAnnotations(annotations)
}

// ManifestBuilder assembles an OCI image manifest through a fluent chain. The
// artifact's identity comes from ref; the config blob is generated from it:
//
//	desc, err := NewManifestBuilder(ref).
//		Layers(content, bundle).
//		Annotations(metadata).
//		Push(ctx, store)
//
// Manifest annotations are produced by a fixed pipeline:
//
//  1. a copy of the caller's metadata (Annotations),
//  2. passed through the transformer (AnnotationTransformer),
//  3. stamped with the identity derived from ref (type, name, version,
//     created), which overrides the same keys in the metadata,
//  4. stripped of descriptor-only provenance keys, which no input can override.
//
// The config blob embeds the same result, so the config, the manifest, and
// the tag derived from ref can never disagree.
//
// Descriptors are content-addressed (digest + size of the data), so Manifest
// is pure and can be inspected without a store. Push writes blobs in OCI
// order: layers, then config, then the manifest that references them.
type ManifestBuilder struct {
	ref       Reference
	platform  string
	createdAt time.Time
	layers    []layerEntry
	metadata  map[string]string
	transform AnnotationTransformer
}

// layerEntry is one layer of the manifest under construction. Layers added
// through Layers hold their content in memory; layers added through LayerRef
// carry only a descriptor and an opener, so their content is read from the
// source only when the destination turns out not to have the blob already.
type layerEntry struct {
	desc ocispec.Descriptor
	data []byte
	open func(context.Context) (io.ReadCloser, error)
}

// push writes the layer to pusher, skipping the transfer entirely when the
// destination already stores the blob. Layers are content-addressed, so an
// identical digest is identical content and re-sending it is pure waste.
func (e layerEntry) push(ctx context.Context, pusher Pusher) error {
	if e.open == nil {
		return pushIfAbsent(ctx, pusher, e.desc, e.data)
	}

	exists, err := blobExists(ctx, pusher, e.desc)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	rc, err := e.open(ctx)
	if err != nil {
		return err
	}
	defer rc.Close()

	if err := pusher.Push(ctx, e.desc, rc); err != nil && !isAlreadyExists(err) {
		return err
	}
	return nil
}

// BlobChecker reports whether a blob is already stored. oras stores implement
// it; a Pusher that does not simply has every blob transferred and relies on
// the store to deduplicate on write.
type BlobChecker interface {
	Exists(ctx context.Context, desc ocispec.Descriptor) (bool, error)
}

func blobExists(ctx context.Context, pusher Pusher, desc ocispec.Descriptor) (bool, error) {
	checker, ok := pusher.(BlobChecker)
	if !ok {
		return false, nil
	}

	exists, err := checker.Exists(ctx, desc)
	if err != nil {
		return false, fmt.Errorf("checking for blob %s: %w", desc.Digest, err)
	}
	return exists, nil
}

// NewManifestBuilder starts a new manifest chain for an artifact identified
// by ref. The ref must carry a valid kind, a name, and a version; this is
// checked when the manifest is built. The creation time is fixed now so every
// Manifest and Push call on the chain produces the same digest.
func NewManifestBuilder(ref Reference) *ManifestBuilder {
	return &ManifestBuilder{
		ref:       ref,
		createdAt: time.Now().UTC(),
		transform: copyAllAnnotations,
	}
}

// Reference returns the identity the manifest is built as.
func (b *ManifestBuilder) Reference() Reference {
	return b.ref
}

// Layers appends in-memory layers to the manifest, preserving order.
func (b *ManifestBuilder) Layers(layers ...Layer) *ManifestBuilder {
	for _, layer := range layers {
		b.layers = append(b.layers, layerEntry{desc: layer.Descriptor(), data: layer.Data})
	}
	return b
}

// LayerRef appends a layer the builder does not hold in memory, identified by
// the descriptor it already has. open is called only when the destination is
// missing the blob, so copying content that is already stored transfers
// nothing. The pushed content is verified against desc by the store.
func (b *ManifestBuilder) LayerRef(desc ocispec.Descriptor, open func(context.Context) (io.ReadCloser, error)) *ManifestBuilder {
	b.layers = append(b.layers, layerEntry{desc: desc, open: open})
	return b
}

// Annotations sets the caller's metadata, the first input of the annotation
// pipeline. Identity keys derived from the reference and descriptor-only
// provenance keys are overridden or removed regardless of what is set here.
func (b *ManifestBuilder) Annotations(annotations map[string]string) *ManifestBuilder {
	b.metadata = copyAnnotations(annotations)
	return b
}

// AnnotationTransformer configures how the caller's metadata is filtered
// before the identity is stamped. A nil transformer restores the default
// copy-all behavior.
func (b *ManifestBuilder) AnnotationTransformer(transformer AnnotationTransformer) *ManifestBuilder {
	if transformer == nil {
		b.transform = copyAllAnnotations
	} else {
		b.transform = transformer
	}
	return b
}

// CreatedAt overrides the creation time recorded in the annotations and the
// config blob. Use it for reproducible digests.
func (b *ManifestBuilder) CreatedAt(t time.Time) *ManifestBuilder {
	b.createdAt = t.UTC()
	return b
}

// annotations runs the annotation pipeline. The result is embedded in both
// the manifest and the config blob.
func (b *ManifestBuilder) annotations() map[string]string {
	anns := b.transform(copyAnnotations(b.metadata))
	anns = contentIdentity(b.ref, b.createdAt, anns)
	if b.platform != "" {
		anns[AnnotationPlatform] = b.platform
	}
	return stripDescriptorOnly(anns)
}

// Manifest builds the ocispec.Manifest from the chain without pushing.
func (b *ManifestBuilder) Manifest() (ocispec.Manifest, error) {
	manifest, _, err := b.assemble()
	return manifest, err
}

// assemble builds the manifest together with the config blob it references.
func (b *ManifestBuilder) assemble() (ocispec.Manifest, Layer, error) {
	if err := validateBuildRef(b.ref); err != nil {
		return ocispec.Manifest{}, Layer{}, &BuilderError{
			Stage:   BuildStageAssemble,
			Builder: BuilderKindManifest,
			Detail:  "building manifest",
			Cause:   err,
		}
	}
	if len(b.layers) == 0 {
		return ocispec.Manifest{}, Layer{}, &BuilderError{
			Stage:   BuildStageAssemble,
			Builder: BuilderKindManifest,
			Detail:  "building manifest: no layers",
		}
	}

	annotations := b.annotations()
	configData, err := newArtifactConfig(b.ref, b.platform, b.createdAt, annotations)
	if err != nil {
		return ocispec.Manifest{}, Layer{}, &BuilderError{
			Stage:   BuildStageEncode,
			Builder: BuilderKindManifest,
			Detail:  "encoding config",
			Cause:   err,
		}
	}
	config := Layer{MediaType: ConfigMediaTypeForKind(b.ref.Kind), Data: configData}

	layers := make([]ocispec.Descriptor, len(b.layers))
	for i := range b.layers {
		layers[i] = b.layers[i].desc
	}

	return ocispec.Manifest{
		Versioned:   specs.Versioned{SchemaVersion: 2},
		MediaType:   ocispec.MediaTypeImageManifest,
		Config:      config.Descriptor(),
		Layers:      layers,
		Annotations: annotations,
	}, config, nil
}

// encodedManifest is a manifest serialized for pushing. The config and the
// manifest are both content-addressed blobs; annotations is the manifest's
// annotation set, which an index repeats on its entry for the manifest.
type encodedManifest struct {
	config      Layer
	manifest    Layer
	annotations map[string]string
}

// encode assembles the manifest and serializes it.
func (b *ManifestBuilder) encode() (encodedManifest, error) {
	manifest, config, err := b.assemble()
	if err != nil {
		return encodedManifest{}, err
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return encodedManifest{}, &BuilderError{
			Stage:   BuildStageEncode,
			Builder: BuilderKindManifest,
			Detail:  "marshalling manifest",
			Cause:   err,
		}
	}

	return encodedManifest{
		config:      config,
		manifest:    Layer{MediaType: ocispec.MediaTypeImageManifest, Data: data},
		annotations: manifest.Annotations,
	}, nil
}

// Push writes the layers, config, and manifest to pusher (in that order) and
// returns the manifest descriptor for tagging. Blobs that already exist are
// treated as success.
func (b *ManifestBuilder) Push(ctx context.Context, pusher Pusher) (ocispec.Descriptor, error) {
	encoded, err := b.encode()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	for i := range b.layers {
		if err := b.layers[i].push(ctx, pusher); err != nil {
			return ocispec.Descriptor{}, &BuilderError{
				Stage:   BuildStagePush,
				Builder: BuilderKindManifest,
				Detail:  fmt.Sprintf("pushing layer %d (%s)", i, b.layers[i].desc.MediaType),
				Cause:   err,
			}
		}
	}

	if err := pushIfAbsent(ctx, pusher, encoded.config.Descriptor(), encoded.config.Data); err != nil {
		return ocispec.Descriptor{}, &BuilderError{
			Stage:   BuildStagePush,
			Builder: BuilderKindManifest,
			Detail:  "pushing config",
			Cause:   err,
		}
	}

	manifestDesc := encoded.manifest.Descriptor()
	if err := pushIfAbsent(ctx, pusher, manifestDesc, encoded.manifest.Data); err != nil {
		return ocispec.Descriptor{}, &BuilderError{
			Stage:   BuildStagePush,
			Builder: BuilderKindManifest,
			Detail:  "pushing manifest",
			Cause:   err,
		}
	}

	return manifestDesc, nil
}

// validateBuildRef checks the identity a built artifact needs. Content is
// always built as a concrete name@version; a digest cannot identify content
// that has not been encoded yet.
func validateBuildRef(ref Reference) error {
	if !ref.Kind.IsValid() || ref.Name == "" || !ref.HasVersion() {
		return &InvalidReferenceError{
			Input:   ref.String(),
			Message: "a built artifact needs a valid kind, a name, and a version",
		}
	}
	return nil
}

func pushIfAbsent(ctx context.Context, pusher Pusher, desc ocispec.Descriptor, data []byte) error {
	if err := pusher.Push(ctx, desc, bytes.NewReader(data)); err != nil && !isAlreadyExists(err) {
		return err
	}
	return nil
}
