// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// rebuildArtifact fetches every blob behind desc and returns a builder that
// re-pushes the same content under target's identity. Image manifests yield a
// ManifestBuilder; image indexes yield an IndexBuilder whose per-platform
// manifests are rebuilt the same way.
func rebuildArtifact(
	ctx context.Context,
	fetcher content.Fetcher,
	desc ocispec.Descriptor,
	target Reference,
	onProgress func(ocispec.Descriptor),
) (ArtifactBuilder, error) {
	if !target.HasVersion() {
		return nil, errors.New("rebuilding artifact: target reference has no version")
	}

	switch desc.MediaType {
	case ocispec.MediaTypeImageManifest:
		return rebuildManifest(ctx, fetcher, desc, target, onProgress)
	case ocispec.MediaTypeImageIndex:
		return rebuildIndex(ctx, fetcher, desc, target, onProgress)
	default:
		return nil, fmt.Errorf("unsupported media type %q", desc.MediaType)
	}
}

// rebuildManifest downloads the layer blobs of a single manifest and returns a
// chain that re-pushes them under target's identity. The source annotations
// are carried over as metadata; the builder replaces the identity and creation
// time and strips any provenance.
//
// The source config blob is deliberately not reused: it restates the artifact's
// name and version inside the content, so copying it would leave the old
// identity embedded in the renamed artifact. The builder mints a fresh one.
func rebuildManifest(
	ctx context.Context,
	fetcher content.Fetcher,
	desc ocispec.Descriptor,
	target Reference,
	onProgress func(ocispec.Descriptor),
) (*ManifestBuilder, error) {
	var manifest ocispec.Manifest
	if err := fetchJSON(ctx, fetcher, desc, &manifest); err != nil {
		return nil, err
	}

	builder := NewManifestBuilder(target).Annotations(manifest.Annotations)
	for _, layerDesc := range manifest.Layers {
		builder.LayerRef(layerDesc, openLayer(fetcher, layerDesc, onProgress))
	}
	return builder, nil
}

// openLayer returns a reader factory for a layer blob, used by the builder
// only when the destination does not already hold that digest. The size bound
// is checked before any bytes are requested so an absurd advertised size is
// rejected without a transfer.
func openLayer(
	fetcher content.Fetcher,
	desc ocispec.Descriptor,
	onProgress func(ocispec.Descriptor),
) func(context.Context) (io.ReadCloser, error) {
	return func(ctx context.Context) (io.ReadCloser, error) {
		if err := checkLayerSize(desc); err != nil {
			return nil, err
		}

		rc, err := fetcher.Fetch(ctx, desc)
		if err != nil {
			return nil, fmt.Errorf("fetching layer %s: %w", desc.Digest, err)
		}
		reportProgress(onProgress, desc)
		return rc, nil
	}
}

// rebuildIndex rebuilds every platform manifest referenced by an image index
// and returns a chain that re-pushes them under target's identity.
func rebuildIndex(
	ctx context.Context,
	fetcher content.Fetcher,
	desc ocispec.Descriptor,
	target Reference,
	onProgress func(ocispec.Descriptor),
) (*IndexBuilder, error) {
	var index ocispec.Index
	if err := fetchJSON(ctx, fetcher, desc, &index); err != nil {
		return nil, err
	}
	if len(index.Manifests) == 0 {
		return nil, fmt.Errorf("image index %s has no manifests", desc.Digest)
	}

	builder := NewIndexBuilder(target)
	for _, entry := range index.Manifests {
		if entry.MediaType != ocispec.MediaTypeImageManifest {
			return nil, fmt.Errorf("image index entry %s has unsupported media type %q", entry.Digest, entry.MediaType)
		}

		platform := OCIPlatformString(entry.Platform)
		if platform == "" {
			return nil, fmt.Errorf("image index entry %s has no platform", entry.Digest)
		}

		manifestBuilder, err := rebuildManifest(ctx, fetcher, entry, target, onProgress)
		if err != nil {
			return nil, fmt.Errorf("platform %s: %w", platform, err)
		}
		builder.Platform(platform, manifestBuilder)
	}

	return builder.Annotations(index.Annotations), nil
}

// artifactConfig is the scafctl metadata config blob every artifact manifest
// carries. It restates the artifact's identity inside the content itself,
// which is why a rebuilt copy mints a new one instead of reusing the source's.
type artifactConfig struct {
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Platform    string            `json:"platform,omitempty"`
	CreatedAt   string            `json:"createdAt"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// newArtifactConfig marshals the config blob for an artifact built as target.
// Platform is empty for single-platform artifacts. createdAt is supplied by
// the builder so repeated encodings of one chain yield the same digest.
func newArtifactConfig(target Reference, platform string, createdAt time.Time, annotations map[string]string) ([]byte, error) {
	data, err := json.Marshal(artifactConfig{
		Kind:        target.Kind.String(),
		Name:        target.Name,
		Version:     target.Version.String(),
		Platform:    platform,
		CreatedAt:   formatCreated(createdAt),
		Annotations: annotations,
	})
	if err != nil {
		return nil, fmt.Errorf("marshalling config for %q: %w", target, err)
	}
	return data, nil
}

// fetchJSON fetches and decodes a manifest-sized JSON blob, verifying it
// against the descriptor's size and digest.
func fetchJSON(ctx context.Context, fetcher content.Fetcher, desc ocispec.Descriptor, out any) error {
	data, err := content.FetchAll(ctx, fetcher, desc)
	if err != nil {
		return fmt.Errorf("fetching %s %s: %w", desc.MediaType, desc.Digest, err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decoding %s %s: %w", desc.MediaType, desc.Digest, err)
	}
	return nil
}

func reportProgress(onProgress func(ocispec.Descriptor), desc ocispec.Descriptor) {
	if onProgress != nil {
		onProgress(desc)
	}
}
