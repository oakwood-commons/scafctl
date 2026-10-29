// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type bundleManifest struct {
	Version int    `json:"version"`
	Root    string `json:"root"`
	Files   []struct {
		Path   string `json:"path"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
		Layer  int    `json:"layer"`
	} `json:"files"`
	Plugins []struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Version string `json:"version"`
	} `json:"plugins,omitempty"`
}

// reassembleDedupBundle fetches all layers of a v2 deduplicated bundle and
// reassembles them into a single v1-compatible tar with embedded manifest.
// fetchBlob is called to retrieve layer data by descriptor.
func reassembleDedupBundle(ociManifest ocispec.Manifest, fetchBlob func(ocispec.Descriptor) ([]byte, error)) ([]byte, error) {
	bundleLayerDescriptor, ok := findLayerByMediaType(ociManifest, MediaTypeSolutionBundleManifest)
	if !ok {
		return nil, fmt.Errorf("bundle manifest layer not found in OCI manifest")
	}
	// Read the bundle manifest from the found layer
	manifestJSON, err := fetchBlob(bundleLayerDescriptor)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch bundle manifest: %w", err)
	}

	var bundleManifest bundleManifest
	if err := json.Unmarshal(manifestJSON, &bundleManifest); err != nil {
		return nil, fmt.Errorf("failed to parse bundle manifest: %w", err)
	}

	return buildV1Tar(bundleManifest, ociManifest, fetchBlob)
}

// buildV1Tar builds a v1-compatible tar containing the embedded bundle
// manifest followed by every file referenced by the v2 bundle manifest.
// Layer data is fetched via fetchBlob and cached per layer index.
func buildV1Tar(bundleManifest bundleManifest, ociManifest ocispec.Manifest, fetchBlob func(ocispec.Descriptor) ([]byte, error)) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Write the manifest as a v1-compatible manifest (downgrade version)
	v1Manifest := bundleManifest
	v1Manifest.Version = 1
	// Deep-copy Files to avoid mutating bundleManifest via shared slice backing array
	v1Manifest.Files = make([]struct {
		Path   string `json:"path"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
		Layer  int    `json:"layer"`
	}, len(bundleManifest.Files))
	copy(v1Manifest.Files, bundleManifest.Files)
	// Zero out layer fields for v1
	for i := range v1Manifest.Files {
		v1Manifest.Files[i].Layer = 0
	}
	v1ManifestJSON, err := json.MarshalIndent(v1Manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal v1 manifest: %w", err)
	}
	if err := writeTarEntry(tw, ".scafctl/bundle-manifest.json", v1ManifestJSON); err != nil {
		return nil, err
	}

	// Cache fetched layers to avoid re-fetching for duplicate layer references
	layerCache := make(map[int][]byte)

	for _, f := range bundleManifest.Files {
		layerData, ok := layerCache[f.Layer]
		if !ok {
			if f.Layer < 0 || f.Layer >= len(ociManifest.Layers) {
				return nil, fmt.Errorf("layer index %d out of range", f.Layer)
			}
			data, err := fetchBlob(ociManifest.Layers[f.Layer])
			if err != nil {
				return nil, fmt.Errorf("failed to fetch layer %d: %w", f.Layer, err)
			}
			layerCache[f.Layer] = data
			layerData = data
		}

		// If the layer is a tar (small files), extract just this file from it
		if isTarMediaType(ociManifest.Layers[f.Layer].MediaType) {
			fileContent, err := extractFileFromTar(layerData, f.Path)
			if err != nil {
				return nil, fmt.Errorf("failed to extract %s from tar layer: %w", f.Path, err)
			}
			if err := writeTarEntry(tw, f.Path, fileContent); err != nil {
				return nil, err
			}
		} else {
			// Raw blob — write directly
			if err := writeTarEntry(tw, f.Path, layerData); err != nil {
				return nil, err
			}
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar: %w", err)
	}

	return buf.Bytes(), nil
}
