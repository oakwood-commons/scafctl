// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// RemoteCatalog implements Catalog interface for OCI registries.
type RemoteCatalog struct {
	name       string
	registry   string
	repository string
	client     *auth.Client
	logger     logr.Logger
}

// RemoteCatalogConfig holds configuration for creating a remote catalog.
type RemoteCatalogConfig struct {
	// Name is the catalog identifier (e.g., "company-registry")
	Name string

	// RegistryURL is the registry address (e.g., "ghcr.io", "registry.example.com")
	Registry string

	// Repository is the base repository path (e.g., "myorg/scafctl")
	Repository string

	// CredentialStore provides authentication credentials
	CredentialStore *CredentialStore

	// Insecure allows HTTP connections (for testing)
	Insecure bool

	// Logger for logging operations
	Logger logr.Logger
}

// NewRemoteCatalog creates a remote catalog client.
func NewRemoteCatalog(cfg RemoteCatalogConfig) (*RemoteCatalog, error) {
	if cfg.Name == "" {
		cfg.Name = cfg.Registry
	}

	// Create auth client with retry
	client := &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
	}

	if cfg.CredentialStore != nil {
		client.Credential = cfg.CredentialStore.CredentialFunc()
	}

	// Set insecure for local development/testing
	if cfg.Insecure {
		client.Client = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec // Opt-in via --insecure flag for local dev/testing only
				},
			},
		}
	}

	return &RemoteCatalog{
		name:       cfg.Name,
		registry:   cfg.Registry,
		repository: cfg.Repository,
		client:     client,
		logger:     cfg.Logger.WithName("remote-catalog").WithValues("catalog", cfg.Name),
	}, nil
}

// Name returns the catalog identifier.
func (c *RemoteCatalog) Name() string {
	return c.name
}

// Registry returns the registry address.
func (c *RemoteCatalog) Registry() string {
	return c.registry
}

// Repository returns the base repository path.
func (c *RemoteCatalog) Repository() string {
	return c.repository
}

// getRepository creates a remote.Repository for an artifact.
func (c *RemoteCatalog) getRepository(ref Reference) (*remote.Repository, error) {
	// Build full repository path: registry/repository/kind/name
	repoPath := c.buildRepositoryPath(ref)

	repo, err := remote.NewRepository(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	repo.Client = c.client

	return repo, nil
}

// buildRepositoryPath builds the full repository path for an artifact.
func (c *RemoteCatalog) buildRepositoryPath(ref Reference) string {
	// Format: registry/repository/kind/name
	// e.g., ghcr.io/myorg/scafctl/solutions/my-solution
	parts := []string{c.registry}
	if c.repository != "" {
		parts = append(parts, c.repository)
	}
	parts = append(parts, ref.Kind.Plural(), ref.Name)

	return strings.Join(parts, "/")
}

// Store saves an artifact to the remote catalog.
// For solutions with bundled files, bundleData contains the tar archive.
// If bundleData is nil, only the primary content layer is stored.
func (c *RemoteCatalog) Store(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool) (ArtifactInfo, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return ArtifactInfo{}, err
	}

	// Check if artifact already exists (unless force is set)
	if !force {
		exists, err := c.Exists(ctx, ref)
		if err != nil {
			return ArtifactInfo{}, fmt.Errorf("failed to check existence: %w", err)
		}
		if exists {
			return ArtifactInfo{}, &ArtifactExistsError{Reference: ref, Catalog: c.name}
		}
	}

	// Create the OCI artifact
	now := time.Now().UTC()

	// Merge annotations with required fields
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[AnnotationArtifactType] = ref.Kind.String()
	annotations[AnnotationArtifactName] = ref.Name
	if ref.Version != nil {
		annotations[AnnotationVersion] = ref.Version.String()
	}
	annotations[AnnotationCreated] = now.Format(time.RFC3339)

	// Create content layer
	contentDesc := ocispec.Descriptor{
		MediaType: MediaTypeForKind(ref.Kind),
		Digest:    digest.FromBytes(content),
		Size:      int64(len(content)),
	}

	if err := repo.Push(ctx, contentDesc, bytes.NewReader(content)); err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to push content blob: %w", err)
	}

	// Create config blob with metadata
	configData, err := json.Marshal(map[string]any{
		"kind":        ref.Kind.String(),
		"name":        ref.Name,
		"version":     ref.Version.String(),
		"createdAt":   now.Format(time.RFC3339),
		"annotations": annotations,
	})
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to marshal config: %w", err)
	}

	configDesc := ocispec.Descriptor{
		MediaType: ConfigMediaTypeForKind(ref.Kind),
		Digest:    digest.FromBytes(configData),
		Size:      int64(len(configData)),
	}

	if err := repo.Push(ctx, configDesc, bytes.NewReader(configData)); err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to push config blob: %w", err)
	}

	// Create manifest layers
	layers := []ocispec.Descriptor{contentDesc}

	// Add bundle layer if present
	if len(bundleData) > 0 {
		bundleDesc := ocispec.Descriptor{
			MediaType: MediaTypeSolutionBundle,
			Digest:    digest.FromBytes(bundleData),
			Size:      int64(len(bundleData)),
		}

		if err := repo.Push(ctx, bundleDesc, bytes.NewReader(bundleData)); err != nil {
			return ArtifactInfo{}, fmt.Errorf("failed to push bundle blob: %w", err)
		}

		layers = append(layers, bundleDesc)
	}

	// Create manifest
	manifest := ocispec.Manifest{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		MediaType:   ocispec.MediaTypeImageManifest,
		Config:      configDesc,
		Layers:      layers,
		Annotations: annotations,
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to marshal manifest: %w", err)
	}

	manifestDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifestData),
		Size:      int64(len(manifestData)),
	}

	if err := repo.Push(ctx, manifestDesc, bytes.NewReader(manifestData)); err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to push manifest: %w", err)
	}

	// Tag the manifest with version
	tag := c.tagForRef(ref)
	if err := repo.Tag(ctx, manifestDesc, tag); err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to tag manifest: %w", err)
	}

	c.logger.V(1).Info("stored artifact",
		"name", ref.Name,
		"version", ref.Version.String(),
		"digest", manifestDesc.Digest.String())

	return ArtifactInfo{
		Reference:   ref,
		Digest:      manifestDesc.Digest.String(),
		CreatedAt:   now,
		Size:        int64(len(content)),
		Annotations: annotations,
		Catalog:     c.name,
	}, nil
}

// Fetch retrieves an artifact from the remote catalog.
func (c *RemoteCatalog) Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return nil, ArtifactInfo{}, err
	}

	// Resolve to get the manifest descriptor
	tag := c.tagForRef(ref)
	manifestDesc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	// Fetch manifest
	manifestReader, err := repo.Fetch(ctx, manifestDesc)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer manifestReader.Close()

	manifestData, err := io.ReadAll(manifestReader)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	if len(manifest.Layers) == 0 {
		return nil, ArtifactInfo{}, fmt.Errorf("manifest has no content layers")
	}

	// Fetch content layer
	contentDesc := manifest.Layers[0]
	contentReader, err := repo.Fetch(ctx, contentDesc)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to fetch content: %w", err)
	}
	defer contentReader.Close()

	contentData, err := io.ReadAll(contentReader)
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to read content: %w", err)
	}

	// Parse annotations for metadata
	createdAt := time.Now()
	if created, ok := manifest.Annotations[AnnotationCreated]; ok {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			createdAt = t
		}
	}

	info := ArtifactInfo{
		Reference:   ref,
		Digest:      manifestDesc.Digest.String(),
		CreatedAt:   createdAt,
		Size:        int64(len(contentData)),
		Annotations: manifest.Annotations,
		Catalog:     c.name,
	}

	return contentData, info, nil
}

// FetchWithBundle retrieves an artifact's primary content and bundle layer.
// If the artifact has no bundle layer, bundleData is nil.
func (c *RemoteCatalog) FetchWithBundle(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return nil, nil, ArtifactInfo{}, err
	}

	// Resolve to get the manifest descriptor
	tag := c.tagForRef(ref)
	manifestDesc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return nil, nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	// Fetch manifest
	manifestReader, err := repo.Fetch(ctx, manifestDesc)
	if err != nil {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer manifestReader.Close()

	manifestData, err := io.ReadAll(manifestReader)
	if err != nil {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	if len(manifest.Layers) == 0 {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("manifest has no content layers")
	}

	// Fetch content layer
	contentDesc := manifest.Layers[0]
	contentReader, err := repo.Fetch(ctx, contentDesc)
	if err != nil {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to fetch content: %w", err)
	}
	defer contentReader.Close()

	contentData, err := io.ReadAll(contentReader)
	if err != nil {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to read content: %w", err)
	}

	// Fetch bundle layer if present
	var bundleData []byte
	if len(manifest.Layers) > 1 && manifest.Layers[1].MediaType == MediaTypeSolutionBundle {
		bundleReader, err := repo.Fetch(ctx, manifest.Layers[1])
		if err != nil {
			return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to fetch bundle: %w", err)
		}
		defer bundleReader.Close()

		bundleData, err = io.ReadAll(bundleReader)
		if err != nil {
			return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to read bundle: %w", err)
		}
	}

	// Parse annotations for metadata
	createdAt := time.Now()
	if created, ok := manifest.Annotations[AnnotationCreated]; ok {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			createdAt = t
		}
	}

	info := ArtifactInfo{
		Reference:   ref,
		Digest:      manifestDesc.Digest.String(),
		CreatedAt:   createdAt,
		Size:        int64(len(contentData)),
		Annotations: manifest.Annotations,
		Catalog:     c.name,
	}

	return contentData, bundleData, info, nil
}

// Resolve finds the best matching version for a reference.
func (c *RemoteCatalog) Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	// If version is specified, resolve directly
	if ref.HasVersion() || ref.HasDigest() {
		repo, err := c.getRepository(ref)
		if err != nil {
			return ArtifactInfo{}, err
		}

		tag := c.tagForRef(ref)
		desc, err := repo.Resolve(ctx, tag)
		if err != nil {
			return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
		}

		return ArtifactInfo{
			Reference: ref,
			Digest:    desc.Digest.String(),
			Size:      desc.Size,
			Catalog:   c.name,
		}, nil
	}

	// No version specified - find the latest
	versions, err := c.listVersions(ctx, ref)
	if err != nil {
		return ArtifactInfo{}, err
	}

	if len(versions) == 0 {
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	// Sort versions descending
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].GreaterThan(versions[j])
	})

	// Return highest version
	ref.Version = versions[0]
	return c.Resolve(ctx, ref)
}

// listVersions lists all versions of an artifact.
func (c *RemoteCatalog) listVersions(ctx context.Context, ref Reference) ([]*semver.Version, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return nil, err
	}

	var versions []*semver.Version

	err = repo.Tags(ctx, "", func(tags []string) error {
		for _, tag := range tags {
			if v, err := semver.NewVersion(tag); err == nil {
				versions = append(versions, v)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tags: %w", err)
	}

	return versions, nil
}

// List returns all artifacts matching the criteria.
func (c *RemoteCatalog) List(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error) {
	// If name is specified, list versions of that artifact
	if name != "" {
		ref := Reference{Kind: kind, Name: name}
		versions, err := c.listVersions(ctx, ref)
		if err != nil {
			return nil, err
		}

		var infos []ArtifactInfo
		for _, v := range versions {
			infos = append(infos, ArtifactInfo{
				Reference: Reference{Kind: kind, Name: name, Version: v},
				Catalog:   c.name,
			})
		}
		return infos, nil
	}

	// Listing all artifacts in a registry is not well-supported by OCI spec
	// This would require registry-specific catalog API
	c.logger.V(1).Info("listing all artifacts not supported for remote catalogs")
	return nil, nil
}

// Exists checks if an artifact exists in the catalog.
func (c *RemoteCatalog) Exists(ctx context.Context, ref Reference) (bool, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return false, err
	}

	tag := c.tagForRef(ref)
	//nolint:errcheck // Resolve error means artifact doesn't exist
	_, err = repo.Resolve(ctx, tag)
	return err == nil, nil
}

// Delete removes an artifact from the catalog.
func (c *RemoteCatalog) Delete(ctx context.Context, ref Reference) error {
	repo, err := c.getRepository(ref)
	if err != nil {
		return err
	}

	// Resolve to get the manifest descriptor
	tag := c.tagForRef(ref)
	desc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	// Delete the manifest (this may not be supported by all registries)
	if err := repo.Delete(ctx, desc); err != nil {
		return fmt.Errorf("failed to delete artifact: %w", err)
	}

	c.logger.V(1).Info("deleted artifact",
		"name", ref.Name,
		"version", ref.Version.String())

	return nil
}

// Tag creates an alias tag for an existing artifact in the remote registry.
func (c *RemoteCatalog) Tag(ctx context.Context, ref Reference, alias string) error {
	repo, err := c.getRepository(ref)
	if err != nil {
		return err
	}

	// Resolve the source artifact
	tag := c.tagForRef(ref)
	desc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return &ArtifactNotFoundError{Reference: ref, Catalog: c.name}
	}

	// Tag with alias
	if err := repo.Tag(ctx, desc, alias); err != nil {
		return fmt.Errorf("failed to tag artifact: %w", err)
	}

	c.logger.V(1).Info("tagged artifact",
		"name", ref.Name,
		"source", tag,
		"alias", alias)

	return nil
}

// tagForRef returns the tag string for a reference.
func (c *RemoteCatalog) tagForRef(ref Reference) string {
	if ref.HasDigest() {
		return ref.Digest
	}
	if ref.HasVersion() {
		return ref.Version.String()
	}
	return "latest"
}

// CopyOptions configures a copy operation between catalogs.
type CopyOptions struct {
	// TargetName overrides the artifact name in the target catalog
	TargetName string

	// Force overwrites existing artifacts
	Force bool

	// OnProgress reports copy progress
	OnProgress func(desc ocispec.Descriptor)
}

// CopyTo copies an artifact from this remote catalog to a local catalog.
func (c *RemoteCatalog) CopyTo(ctx context.Context, ref Reference, target *LocalCatalog, opts CopyOptions) (ArtifactInfo, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return ArtifactInfo{}, err
	}

	tag := c.tagForRef(ref)

	// Configure copy options
	copyOpts := oras.DefaultCopyOptions
	if opts.OnProgress != nil {
		copyOpts.OnCopySkipped = func(_ context.Context, desc ocispec.Descriptor) error {
			opts.OnProgress(desc)
			return nil
		}
		copyOpts.PostCopy = func(_ context.Context, desc ocispec.Descriptor) error {
			opts.OnProgress(desc)
			return nil
		}
	}

	// Copy from remote to local store
	desc, err := oras.Copy(ctx, repo, tag, target.store, tag, copyOpts)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to copy artifact: %w", err)
	}

	// Determine target reference
	targetRef := ref
	if opts.TargetName != "" {
		targetRef.Name = opts.TargetName
	}

	// Tag in local store
	targetTag := target.tagForRef(targetRef)
	if err := target.store.Tag(ctx, desc, targetTag); err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to tag artifact: %w", err)
	}

	c.logger.V(1).Info("copied artifact from remote to local",
		"name", ref.Name,
		"version", ref.Version.String(),
		"digest", desc.Digest.String())

	return ArtifactInfo{
		Reference: targetRef,
		Digest:    desc.Digest.String(),
		Size:      desc.Size,
		Catalog:   LocalCatalogName,
	}, nil
}

// CopyFrom copies an artifact from a local catalog to this remote catalog.
func (c *RemoteCatalog) CopyFrom(ctx context.Context, source *LocalCatalog, ref Reference, opts CopyOptions) (ArtifactInfo, error) {
	repo, err := c.getRepository(ref)
	if err != nil {
		return ArtifactInfo{}, err
	}

	tag := source.tagForRef(ref)

	// Check if artifact exists in source
	desc, err := source.store.Resolve(ctx, tag)
	if err != nil {
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	}

	// Check if target already exists (unless force)
	if !opts.Force {
		exists, _ := c.Exists(ctx, ref)
		if exists {
			return ArtifactInfo{}, &ArtifactExistsError{Reference: ref, Catalog: c.name}
		}
	}

	// Configure copy options
	copyOpts := oras.DefaultCopyOptions
	if opts.OnProgress != nil {
		copyOpts.OnCopySkipped = func(_ context.Context, d ocispec.Descriptor) error {
			opts.OnProgress(d)
			return nil
		}
		copyOpts.PostCopy = func(_ context.Context, d ocispec.Descriptor) error {
			opts.OnProgress(d)
			return nil
		}
	}

	// Determine target tag
	targetRef := ref
	if opts.TargetName != "" {
		targetRef.Name = opts.TargetName
	}
	targetTag := c.tagForRef(targetRef)

	// Copy from local to remote
	desc, err = oras.Copy(ctx, source.store, tag, repo, targetTag, copyOpts)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to copy artifact: %w", err)
	}

	c.logger.V(1).Info("copied artifact from local to remote",
		"name", ref.Name,
		"version", ref.Version.String(),
		"digest", desc.Digest.String(),
		"targetCatalog", c.name)

	return ArtifactInfo{
		Reference: targetRef,
		Digest:    desc.Digest.String(),
		Size:      desc.Size,
		Catalog:   c.name,
	}, nil
}

// Ensure RemoteCatalog implements Catalog interface.
var _ Catalog = (*RemoteCatalog)(nil)

// Ensure content.Storage is satisfied for oras.Copy (compile-time check).
var _ content.Storage = (*remote.Repository)(nil)
