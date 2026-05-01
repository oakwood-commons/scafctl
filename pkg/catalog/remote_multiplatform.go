// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// Compile-time check that RemoteCatalog implements PlatformAwareCatalog.
var _ PlatformAwareCatalog = (*RemoteCatalog)(nil)

// FetchByPlatform fetches a plugin binary for a specific platform from a
// remote OCI registry. It transparently handles both single-platform
// manifests and multi-platform image indexes.
func (c *RemoteCatalog) FetchByPlatform(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error) {
	data, info, err := c.fetchByPlatformInternal(ctx, ref, platform)
	if err != nil && isOCIAuthError(err) {
		c.logger.V(1).Info("platform fetch rejected by registry, retrying anonymously",
			"kind", ref.Kind, "name", ref.Name, "platform", platform, "error", err.Error())
		c.switchToAnonymous()
		var retryErr error
		data, info, retryErr = c.fetchByPlatformInternal(ctx, ref, platform)
		if retryErr != nil {
			return nil, ArtifactInfo{}, fmt.Errorf("anonymous retry failed (%w) after auth error: %w", retryErr, err)
		}
		return data, info, nil
	}
	return data, info, err
}

func (c *RemoteCatalog) fetchByPlatformInternal(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error) {
	// When no version is specified, resolve to the latest version first.
	if !ref.HasVersion() && !ref.HasDigest() {
		resolved, err := c.resolveWithKind(ctx, ref)
		if err != nil {
			return nil, ArtifactInfo{}, err
		}
		ref = resolved.Reference
	}

	repo, err := c.getRepository(ref)
	if err != nil {
		return nil, ArtifactInfo{}, err
	}

	tag := c.tagForRef(ref)
	topDesc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	topData, err := content.FetchAll(ctx, repo, topDesc)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to fetch manifest: %w", err)
	}

	// If this is an image index, resolve the platform-specific manifest.
	if IsImageIndex(topDesc) {
		var index ocispec.Index
		if err := json.Unmarshal(topData, &index); err != nil {
			return nil, ArtifactInfo{}, fmt.Errorf("failed to unmarshal image index: %w", err)
		}

		platDesc, err := MatchPlatform(&index, platform)
		if err != nil {
			return nil, ArtifactInfo{}, err
		}

		// Fetch the platform-specific manifest.
		platManifestData, err := content.FetchAll(ctx, repo, *platDesc)
		if err != nil {
			return nil, ArtifactInfo{}, fmt.Errorf("failed to fetch platform manifest: %w", err)
		}

		return c.extractLayerContent(ctx, repo, platManifestData, ref, platform)
	}

	// Single-platform manifest — extract content directly.
	return c.extractLayerContent(ctx, repo, topData, ref, "")
}

// extractLayerContent unmarshals a manifest and fetches the first content layer.
func (c *RemoteCatalog) extractLayerContent(
	ctx context.Context,
	repo content.Fetcher,
	manifestData []byte,
	ref Reference,
	platform string,
) ([]byte, ArtifactInfo, error) {
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	if len(manifest.Layers) == 0 {
		return nil, ArtifactInfo{}, fmt.Errorf("manifest has no content layers")
	}

	contentData, err := content.FetchAll(ctx, repo, manifest.Layers[0])
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to fetch content layer: %w", err)
	}

	createdAt := time.Now()
	if created, ok := manifest.Annotations[AnnotationCreated]; ok {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			createdAt = t
		}
	}

	annotations := manifest.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}
	if platform != "" {
		annotations[AnnotationPlatform] = platform
	}

	info := ArtifactInfo{
		Reference:   ref,
		Digest:      manifest.Layers[0].Digest.String(),
		CreatedAt:   createdAt,
		Size:        int64(len(contentData)),
		Annotations: annotations,
		Catalog:     c.name,
	}

	return contentData, info, nil
}

// ListPlatforms returns the platforms available for a multi-platform artifact.
// Returns nil if the artifact is single-platform.
func (c *RemoteCatalog) ListPlatforms(ctx context.Context, ref Reference) ([]string, error) {
	platforms, err := c.listPlatformsInternal(ctx, ref)
	if err != nil && isOCIAuthError(err) {
		c.logger.V(1).Info("list platforms rejected by registry, retrying anonymously",
			"kind", ref.Kind, "name", ref.Name, "error", err.Error())
		c.switchToAnonymous()
		var retryErr error
		platforms, retryErr = c.listPlatformsInternal(ctx, ref)
		if retryErr != nil {
			return nil, fmt.Errorf("anonymous retry failed (%w) after auth error: %w", retryErr, err)
		}
		return platforms, nil
	}
	return platforms, err
}

func (c *RemoteCatalog) listPlatformsInternal(ctx context.Context, ref Reference) ([]string, error) {
	if !ref.HasVersion() && !ref.HasDigest() {
		resolved, err := c.resolveWithKind(ctx, ref)
		if err != nil {
			return nil, err
		}
		ref = resolved.Reference
	}

	repo, err := c.getRepository(ref)
	if err != nil {
		return nil, err
	}

	tag := c.tagForRef(ref)
	desc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return nil, &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	if !IsImageIndex(desc) {
		return nil, nil
	}

	data, err := content.FetchAll(ctx, repo, desc)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image index: %w", err)
	}

	var index ocispec.Index
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("failed to unmarshal image index: %w", err)
	}

	return IndexPlatforms(&index), nil
}
