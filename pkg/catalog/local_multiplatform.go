// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/errdef"
)

// StoreMultiPlatform stores a set of platform-specific plugin binaries as an
// OCI image index (fat manifest). Each entry in platformBinaries maps a
// platform string (e.g. "linux/amd64") to the raw binary data. The resulting
// image index is tagged under the normal kind/name:version scheme.
//
// This replaces any existing single-platform artifact at the same reference.
func (c *LocalCatalog) StoreMultiPlatform(ctx context.Context, ref Reference, platformBinaries []PlatformBinary, annotations map[string]string, force bool) (ArtifactInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(platformBinaries) == 0 {
		return ArtifactInfo{}, fmt.Errorf("at least one platform binary is required")
	}

	// Check existence (unless force)
	if !force && c.existsLocked(ctx, ref) {
		return ArtifactInfo{}, &ArtifactExistsError{Reference: ref, Catalog: LocalCatalogName}
	}

	now := time.Now().UTC()

	// Merge annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[AnnotationArtifactType] = ref.Kind.String()
	annotations[AnnotationArtifactName] = ref.Name
	if ref.Version != nil {
		annotations[AnnotationVersion] = ref.Version.String()
	}
	annotations[AnnotationCreated] = now.Format(time.RFC3339)

	// Build a per-platform manifest for each binary and collect descriptors.
	var manifestDescs []ocispec.Descriptor

	for _, pb := range platformBinaries {
		ociPlatform, err := PlatformToOCI(pb.Platform)
		if err != nil {
			return ArtifactInfo{}, fmt.Errorf("invalid platform %q: %w", pb.Platform, err)
		}

		// Push content layer
		contentDesc, err := c.pushBlobLocked(ctx, MediaTypeForKind(ref.Kind), pb.Data)
		if err != nil {
			return ArtifactInfo{}, fmt.Errorf("pushing content for %s: %w", pb.Platform, err)
		}

		// Push config blob
		configData, err := json.Marshal(map[string]any{
			"kind":      ref.Kind.String(),
			"name":      ref.Name,
			"version":   ref.Version.String(),
			"platform":  pb.Platform,
			"createdAt": now.Format(time.RFC3339),
		})
		if err != nil {
			return ArtifactInfo{}, fmt.Errorf("marshalling config for %s: %w", pb.Platform, err)
		}

		configDesc, err := c.pushBlobLocked(ctx, ConfigMediaTypeForKind(ref.Kind), configData)
		if err != nil {
			return ArtifactInfo{}, fmt.Errorf("pushing config for %s: %w", pb.Platform, err)
		}

		// Build per-platform manifest
		platAnnotations := copyAnnotations(annotations)
		platAnnotations[AnnotationPlatform] = pb.Platform

		manifest := ocispec.Manifest{
			Versioned:   specs.Versioned{SchemaVersion: 2},
			MediaType:   ocispec.MediaTypeImageManifest,
			Config:      configDesc,
			Layers:      []ocispec.Descriptor{contentDesc},
			Annotations: platAnnotations,
		}

		manifestData, err := json.Marshal(manifest)
		if err != nil {
			return ArtifactInfo{}, fmt.Errorf("marshalling manifest for %s: %w", pb.Platform, err)
		}

		manifestDesc, err := c.pushBlobLocked(ctx, ocispec.MediaTypeImageManifest, manifestData)
		if err != nil {
			return ArtifactInfo{}, fmt.Errorf("pushing manifest for %s: %w", pb.Platform, err)
		}

		manifestDesc.Platform = ociPlatform
		manifestDesc.Annotations = platAnnotations
		manifestDescs = append(manifestDescs, manifestDesc)
	}

	// Build the image index
	index := ocispec.Index{
		Versioned:   specs.Versioned{SchemaVersion: 2},
		MediaType:   ocispec.MediaTypeImageIndex,
		Manifests:   manifestDescs,
		Annotations: annotations,
	}

	indexData, err := json.Marshal(index)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("marshalling image index: %w", err)
	}

	indexDesc, err := c.pushBlobLocked(ctx, ocispec.MediaTypeImageIndex, indexData)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("pushing image index: %w", err)
	}

	// Tag the index under the standard schema
	tag := c.tagForRef(ref)
	indexDesc.Annotations = annotations
	if err := c.store.Tag(ctx, indexDesc, tag); err != nil {
		return ArtifactInfo{}, fmt.Errorf("tagging image index: %w", err)
	}

	c.logger.V(1).Info("stored multi-platform artifact",
		"name", ref.Name,
		"version", ref.Version.String(),
		"platforms", len(platformBinaries),
		"digest", indexDesc.Digest.String())

	return ArtifactInfo{
		Reference:   ref,
		Digest:      indexDesc.Digest.String(),
		CreatedAt:   now,
		Size:        indexDesc.Size,
		Annotations: annotations,
		Catalog:     LocalCatalogName,
	}, nil
}

// FetchByPlatform fetches a plugin binary for the given platform from an
// artifact that may be stored as either a single-platform manifest or a
// multi-platform image index. It transparently handles both cases.
func (c *LocalCatalog) FetchByPlatform(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info, err := c.resolveLocked(ctx, ref)
	if err != nil {
		return nil, ArtifactInfo{}, err
	}

	tag := c.tagForRef(info.Reference)
	desc, err := c.store.Resolve(ctx, tag)
	if err != nil {
		return nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	}

	// If it's an image index, resolve the platform-specific manifest.
	if IsImageIndex(desc) {
		return c.fetchFromIndex(ctx, desc, platform, info)
	}

	// Single-platform manifest — return the content directly.
	return c.fetchFromManifest(ctx, desc, info)
}

// fetchFromIndex resolves a platform-specific manifest from an image index
// and returns the binary content.
func (c *LocalCatalog) fetchFromIndex(ctx context.Context, indexDesc ocispec.Descriptor, platform string, info ArtifactInfo) ([]byte, ArtifactInfo, error) {
	indexData, err := c.fetchBlob(ctx, indexDesc)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("fetching image index: %w", err)
	}

	var index ocispec.Index
	if err := json.Unmarshal(indexData, &index); err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("unmarshalling image index: %w", err)
	}

	platDesc, err := MatchPlatform(&index, platform)
	if err != nil {
		return nil, ArtifactInfo{}, err
	}

	content, _, err := c.fetchFromManifest(ctx, *platDesc, info)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("fetching platform %s manifest: %w", platform, err)
	}

	// Enrich info with platform annotation
	if info.Annotations == nil {
		info.Annotations = make(map[string]string)
	}
	info.Annotations[AnnotationPlatform] = platform

	return content, info, nil
}

// fetchFromManifest fetches the first layer content from a manifest descriptor.
func (c *LocalCatalog) fetchFromManifest(ctx context.Context, manifestDesc ocispec.Descriptor, info ArtifactInfo) ([]byte, ArtifactInfo, error) {
	manifestData, err := c.fetchBlob(ctx, manifestDesc)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("fetching manifest: %w", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("unmarshalling manifest: %w", err)
	}

	if len(manifest.Layers) == 0 {
		return nil, ArtifactInfo{}, fmt.Errorf("manifest has no content layers")
	}

	content, err := c.fetchBlob(ctx, manifest.Layers[0])
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("fetching content layer: %w", err)
	}

	return content, info, nil
}

// ListPlatforms returns the platforms available for a multi-platform artifact.
// If the artifact is single-platform, returns nil.
func (c *LocalCatalog) ListPlatforms(ctx context.Context, ref Reference) ([]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info, err := c.resolveLocked(ctx, ref)
	if err != nil {
		return nil, err
	}

	tag := c.tagForRef(info.Reference)
	desc, err := c.store.Resolve(ctx, tag)
	if err != nil {
		return nil, &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	}

	if !IsImageIndex(desc) {
		return nil, nil
	}

	indexData, err := c.fetchBlob(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf("fetching image index: %w", err)
	}

	var index ocispec.Index
	if err := json.Unmarshal(indexData, &index); err != nil {
		return nil, fmt.Errorf("unmarshalling image index: %w", err)
	}

	return IndexPlatforms(&index), nil
}

// pushBlobLocked pushes a blob to the OCI store (caller must hold the lock).
func (c *LocalCatalog) pushBlobLocked(ctx context.Context, mediaType string, content []byte) (ocispec.Descriptor, error) {
	desc := ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(content),
		Size:      int64(len(content)),
	}

	if err := c.store.Push(ctx, desc, bytes.NewReader(content)); err != nil {
		if !isAlreadyExists(err) {
			return ocispec.Descriptor{}, err
		}
	}

	return desc, nil
}

// isAlreadyExists checks if the error is an "already exists" error from the oras store.
func isAlreadyExists(err error) bool {
	return err != nil && errors.Is(err, errdef.ErrAlreadyExists)
}

// copyAnnotations creates a shallow copy of an annotations map.
func copyAnnotations(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
