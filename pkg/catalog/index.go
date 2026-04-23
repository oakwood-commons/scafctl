// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
)

// catalogIndexRepository is the well-known repository name for the catalog
// index artifact. Catalog maintainers push a JSON manifest of all available
// artifacts to this location so that unauthenticated users can discover
// packages without needing the GHCR Packages API.
const catalogIndexRepository = "catalog-index"

// catalogIndexTag is the OCI tag used for the current catalog index.
const catalogIndexTag = "latest"

// catalogIndexMediaType is the OCI media type for the catalog index layer.
const catalogIndexMediaType = "application/vnd.scafctl.catalog-index.v1+json"

// Index is the JSON payload stored inside the catalog-index artifact.
// It contains a list of all discoverable artifacts in the catalog.
type Index struct {
	// Artifacts is the list of discoverable artifacts.
	Artifacts []DiscoveredArtifact `json:"artifacts"`
}

// FetchIndex pulls the well-known catalog-index artifact and returns
// the discovered artifacts. Returns an error if the index does not exist or
// cannot be parsed.
func (c *RemoteCatalog) FetchIndex(ctx context.Context) ([]DiscoveredArtifact, error) {
	repoPath := c.buildIndexRepositoryPath()

	repo, err := remote.NewRepository(repoPath)
	if err != nil {
		return nil, fmt.Errorf("creating index repository: %w", err)
	}
	repo.Client = c.client

	// Resolve the manifest descriptor.
	manifestDesc, err := repo.Resolve(ctx, catalogIndexTag)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog index: %w", err)
	}

	// Fetch and parse the OCI manifest.
	manifestData, err := content.FetchAll(ctx, repo, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("fetching catalog index manifest: %w", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("parsing catalog index manifest: %w", err)
	}

	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("catalog index has no layers")
	}

	// Fetch the first layer (the JSON index).
	layerData, err := content.FetchAll(ctx, repo, manifest.Layers[0])
	if err != nil {
		return nil, fmt.Errorf("fetching catalog index layer: %w", err)
	}

	var index Index
	if err := json.Unmarshal(layerData, &index); err != nil {
		return nil, fmt.Errorf("parsing catalog index: %w", err)
	}

	c.logger.V(1).Info("loaded catalog index",
		"artifacts", len(index.Artifacts))

	return index.Artifacts, nil
}

// buildIndexRepositoryPath returns the OCI repository path for the catalog
// index artifact (e.g. "ghcr.io/oakwood-commons/catalog-index").
func (c *RemoteCatalog) buildIndexRepositoryPath() string {
	parts := []string{c.registry}
	if c.repository != "" {
		parts = append(parts, c.repository)
	}
	parts = append(parts, catalogIndexRepository)
	return strings.Join(parts, "/")
}

// PushIndex pushes a catalog index artifact containing the given artifacts
// to the well-known catalog-index repository. This enables unauthenticated
// users to discover available packages without needing the registry's
// enumeration API.
func (c *RemoteCatalog) PushIndex(ctx context.Context, artifacts []DiscoveredArtifact) error {
	repoPath := c.buildIndexRepositoryPath()

	repo, err := remote.NewRepository(repoPath)
	if err != nil {
		return fmt.Errorf("creating index repository: %w", err)
	}
	repo.Client = c.client

	// Marshal the index payload.
	indexData, err := json.Marshal(Index{Artifacts: artifacts})
	if err != nil {
		return fmt.Errorf("marshaling catalog index: %w", err)
	}

	// Push the index layer.
	layerDesc := ocispec.Descriptor{
		MediaType: catalogIndexMediaType,
		Digest:    digest.FromBytes(indexData),
		Size:      int64(len(indexData)),
	}
	if err := repo.Push(ctx, layerDesc, bytes.NewReader(indexData)); err != nil {
		return fmt.Errorf("pushing index layer: %w", err)
	}

	// Push an empty config (required by OCI manifest).
	emptyConfig := []byte("{}")
	configDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageConfig,
		Digest:    digest.FromBytes(emptyConfig),
		Size:      int64(len(emptyConfig)),
	}
	if err := repo.Push(ctx, configDesc, bytes.NewReader(emptyConfig)); err != nil {
		return fmt.Errorf("pushing index config: %w", err)
	}

	// Pack and tag the manifest.
	manifestDesc, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, catalogIndexMediaType, oras.PackManifestOptions{
		Layers:           []ocispec.Descriptor{layerDesc},
		ConfigDescriptor: &configDesc,
	})
	if err != nil {
		return fmt.Errorf("packing index manifest: %w", err)
	}

	if err := repo.Tag(ctx, manifestDesc, catalogIndexTag); err != nil {
		return fmt.Errorf("tagging index manifest: %w", err)
	}

	c.logger.V(1).Info("pushed catalog index",
		"artifacts", len(artifacts),
		"digest", manifestDesc.Digest.String())

	return nil
}
