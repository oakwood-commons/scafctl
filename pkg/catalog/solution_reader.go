// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/json"
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// ReadSolutionArtifact reads a solution's document and bundle from any OCI
// content source, given the descriptor of its manifest. It lets a caller
// inspect an artifact staged in a scratch store (e.g. an in-memory store)
// exactly as a consumer will read it back from the catalog, before the
// artifact is committed anywhere persistent. The bundle is nil when the
// artifact has none; a deduplicated bundle is reassembled into a single tar.
func ReadSolutionArtifact(ctx context.Context, fetcher content.Fetcher, manifestDesc ocispec.Descriptor) (doc, bundle []byte, err error) {
	fetchBlob := func(desc ocispec.Descriptor) ([]byte, error) {
		return content.FetchAll(ctx, fetcher, desc)
	}
	data, err := fetchBlob(manifestDesc)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}
	return readSolutionLayers(manifest, fetchBlob)
}

// readSolutionLayers reads the primary content layer (always the first layer)
// and the bundle (if any) of a solution manifest. The bundle is located by
// media type, never by index, matching LocalCatalog.FetchWithLayer: a plain
// tar is probed first, then a deduplicated bundle manifest, which is
// reassembled into a tar.
func readSolutionLayers(manifest ocispec.Manifest, fetchBlob func(ocispec.Descriptor) ([]byte, error)) (doc, bundle []byte, err error) {
	if len(manifest.Layers) == 0 {
		return nil, nil, fmt.Errorf("manifest has no content layers")
	}
	doc, err = fetchBlob(manifest.Layers[0])
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch content: %w", err)
	}
	if desc, ok := findLayerByMediaType(manifest, MediaTypeSolutionBundle); ok {
		bundle, err = fetchBlob(desc)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch bundle: %w", err)
		}
		return doc, bundle, nil
	}
	if _, ok := findLayerByMediaType(manifest, MediaTypeSolutionBundleManifest); ok {
		bundle, err = reassembleDedupBundle(manifest, fetchBlob)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to reassemble dedup bundle: %w", err)
		}
	}
	return doc, bundle, nil
}
