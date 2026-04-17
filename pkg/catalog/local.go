// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"
)

const (
	// LocalCatalogName is the name of the built-in local catalog.
	LocalCatalogName = "local"
)

// LocalCatalog implements Catalog using a local OCI layout store.
type LocalCatalog struct {
	store  *oci.Store
	path   string
	logger logr.Logger
	mu     sync.RWMutex
}

// NewLocalCatalog creates a catalog at the XDG data path.
// The path is determined by paths.CatalogDir().
func NewLocalCatalog(logger logr.Logger) (*LocalCatalog, error) {
	catalogPath := paths.CatalogDir()
	return NewLocalCatalogAt(catalogPath, logger)
}

// NewLocalCatalogAt creates a catalog at a custom path.
// Use this for testing or custom installations.
func NewLocalCatalogAt(path string, logger logr.Logger) (*LocalCatalog, error) {
	// Ensure directory exists
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create catalog directory: %w", err)
	}

	// Create OCI store
	store, err := oci.New(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCI store: %w", err)
	}

	return &LocalCatalog{
		store:  store,
		path:   path,
		logger: logger.WithName("catalog").WithValues("catalog", LocalCatalogName),
	}, nil
}

// Name returns "local".
func (c *LocalCatalog) Name() string {
	return LocalCatalogName
}

// Path returns the catalog directory path.
func (c *LocalCatalog) Path() string {
	return c.path
}

// Store saves an artifact to the catalog.
// For solutions with bundled files, bundleData contains the tar archive.
// If bundleData is nil, only the primary content layer is stored.
func (c *LocalCatalog) Store(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool) (ArtifactInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if artifact already exists (unless force is set)
	if !force {
		if c.existsLocked(ctx, ref) {
			return ArtifactInfo{}, &ArtifactExistsError{Reference: ref, Catalog: LocalCatalogName}
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

	// Default origin to "built" unless the caller set one (e.g., cacheArtifact
	// passes "auto-cached from <catalog>").
	if annotations[AnnotationOrigin] == "" {
		annotations[AnnotationOrigin] = "built"
	}

	// Create content layer
	contentDesc, err := c.pushBlob(ctx, MediaTypeForKind(ref.Kind), content)
	if err != nil {
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

	configDesc, err := c.pushBlob(ctx, ConfigMediaTypeForKind(ref.Kind), configData)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to push config blob: %w", err)
	}

	// Create manifest layers
	layers := []ocispec.Descriptor{contentDesc}

	// Add bundle layer if present
	if len(bundleData) > 0 {
		bundleDesc, err := c.pushBlob(ctx, MediaTypeSolutionBundle, bundleData)
		if err != nil {
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

	manifestDesc, err := c.pushBlob(ctx, ocispec.MediaTypeImageManifest, manifestData)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to push manifest: %w", err)
	}

	// Tag the manifest
	tag := c.tagForRef(ref)
	manifestDesc.Annotations = annotations
	if err := c.store.Tag(ctx, manifestDesc, tag); err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to tag manifest: %w", err)
	}

	c.logger.V(1).Info("stored artifact",
		"name", ref.Name,
		"version", ref.Version.String(),
		"digest", manifestDesc.Digest.String(),
		"size", manifestDesc.Size)

	return ArtifactInfo{
		Reference:   ref,
		Digest:      manifestDesc.Digest.String(),
		CreatedAt:   now,
		Size:        int64(len(content)),
		Annotations: annotations,
		Catalog:     LocalCatalogName,
	}, nil
}

// Fetch retrieves an artifact from the catalog.
func (c *LocalCatalog) Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Resolve to get the manifest descriptor
	info, err := c.resolveLocked(ctx, ref)
	if err != nil {
		return nil, ArtifactInfo{}, err
	}

	// Fetch the manifest using the resolved digest directly rather than
	// reconstructing a tag. This handles catalog entries whose OCI tag does
	// not match the canonical kind/name:version format (e.g., entries pulled
	// from a remote registry before the kind was set).
	manifest, err := c.fetchManifestByDigest(ctx, info.Digest)
	if err != nil {
		return nil, ArtifactInfo{}, err
	}

	if len(manifest.Layers) == 0 {
		return nil, ArtifactInfo{}, fmt.Errorf("manifest has no content layers")
	}

	// Fetch content from first layer
	content, err := c.fetchBlob(ctx, manifest.Layers[0])
	if err != nil {
		return nil, ArtifactInfo{}, fmt.Errorf("failed to fetch content: %w", err)
	}

	return content, info, nil
}

// FetchWithBundle retrieves an artifact's primary content and bundle layer.
// If the artifact has no bundle layer, bundleData is nil.
func (c *LocalCatalog) FetchWithBundle(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Resolve to get the manifest descriptor
	info, err := c.resolveLocked(ctx, ref)
	if err != nil {
		return nil, nil, ArtifactInfo{}, err
	}

	// Fetch the manifest by digest (see Fetch for rationale).
	manifest, err := c.fetchManifestByDigest(ctx, info.Digest)
	if err != nil {
		return nil, nil, ArtifactInfo{}, err
	}

	if len(manifest.Layers) == 0 {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("manifest has no content layers")
	}

	// Fetch content from first layer
	content, err := c.fetchBlob(ctx, manifest.Layers[0])
	if err != nil {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to fetch content: %w", err)
	}

	// Fetch bundle from second layer if present
	var bundleData []byte
	if len(manifest.Layers) > 1 {
		switch manifest.Layers[1].MediaType {
		case MediaTypeSolutionBundle:
			// Version 1: single tar layer
			bundleData, err = c.fetchBlob(ctx, manifest.Layers[1])
			if err != nil {
				return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to fetch bundle: %w", err)
			}
		case MediaTypeSolutionBundleManifest:
			// Version 2: deduplicated — reassemble into a v1-compatible tar
			// for backward-compatible consumers that use FetchWithBundle.
			bundleData, err = c.reassembleDedup(ctx, manifest)
			if err != nil {
				return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to reassemble dedup bundle: %w", err)
			}
		}
	}

	return content, bundleData, info, nil
}

// reassembleDedup fetches all layers of a v2 deduplicated bundle and
// reassembles them into a single v1-compatible tar with embedded manifest.
func (c *LocalCatalog) reassembleDedup(ctx context.Context, ociManifest ocispec.Manifest) ([]byte, error) {
	fetchBlob := func(desc ocispec.Descriptor) ([]byte, error) {
		return c.fetchBlob(ctx, desc)
	}
	return reassembleDedupBundle(ociManifest, fetchBlob)
}

// writeTarEntry writes a single entry to a tar writer.
func writeTarEntry(tw *tar.Writer, name string, content []byte) error {
	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Size: int64(len(content)),
		Mode: 0o644,
	}); err != nil {
		return fmt.Errorf("failed to write tar header for %s: %w", name, err)
	}
	if _, err := tw.Write(content); err != nil {
		return fmt.Errorf("failed to write tar content for %s: %w", name, err)
	}
	return nil
}

// isTarMediaType returns true if the media type indicates a tar layer.
func isTarMediaType(mediaType string) bool {
	return mediaType == MediaTypeSolutionBundle || mediaType == MediaTypeSolutionBundleSmallTar
}

// extractFileFromTar finds and returns a single file's content from a tar archive.
func extractFileFromTar(tarData []byte, filePath string) ([]byte, error) {
	tr := tar.NewReader(bytes.NewReader(tarData))
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("file %s not found in tar", filePath)
		}
		if err != nil {
			return nil, err
		}
		if filepath.ToSlash(filepath.Clean(header.Name)) == filepath.ToSlash(filepath.Clean(filePath)) {
			return io.ReadAll(tr)
		}
	}
}

// Resolve finds the best matching version for a reference.
func (c *LocalCatalog) Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.resolveLocked(ctx, ref)
}

func (c *LocalCatalog) resolveLocked(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	// If version is specified, look for exact match
	if ref.HasVersion() || ref.HasDigest() {
		tag := c.tagForRef(ref)
		desc, err := c.store.Resolve(ctx, tag)
		if err == nil {
			annotations, err := c.getManifestAnnotations(ctx, desc)
			if err != nil {
				return ArtifactInfo{}, err
			}

			return c.infoFromAnnotations(ref, desc, annotations), nil
		}

		// Tag lookup failed. This can happen when an artifact was pulled
		// from a remote registry with an empty or incorrect kind, producing
		// a tag that doesn't match the canonical format (e.g., "/name:1.0.0"
		// instead of "solution/name:1.0.0"). Fall back to annotation-based
		// listing which handles mismatched tags.
		artifacts, err := c.listLocked(ctx, ref.Kind, ref.Name)
		if err != nil {
			return ArtifactInfo{}, err
		}
		for _, a := range artifacts {
			if ref.HasVersion() && a.Reference.Version != nil && a.Reference.Version.Equal(ref.Version) {
				return a, nil
			}
			if ref.HasDigest() && a.Digest == ref.Digest {
				return a, nil
			}
		}
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	}

	// No version specified - find highest semver
	artifacts, err := c.listLocked(ctx, ref.Kind, ref.Name)
	if err != nil {
		return ArtifactInfo{}, err
	}

	if len(artifacts) == 0 {
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	}

	// Sort by version descending and return highest
	sort.Slice(artifacts, func(i, j int) bool {
		vi := artifacts[i].Reference.Version
		vj := artifacts[j].Reference.Version
		if vi == nil {
			return false
		}
		if vj == nil {
			return true
		}
		return vi.GreaterThan(vj)
	})

	return artifacts[0], nil
}

// List returns all artifacts matching the criteria.
func (c *LocalCatalog) List(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.listLocked(ctx, kind, name)
}

func (c *LocalCatalog) listLocked(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error) {
	var results []ArtifactInfo

	// Iterate through all tags
	err := c.store.Tags(ctx, "", func(tags []string) error {
		for _, tag := range tags {
			desc, err := c.store.Resolve(ctx, tag)
			if err != nil {
				c.logger.V(2).Info("failed to resolve tag", "tag", tag, "error", err)
				continue
			}

			annotations, err := c.getManifestAnnotations(ctx, desc)
			if err != nil {
				c.logger.V(2).Info("failed to get annotations", "tag", tag, "error", err)
				continue
			}

			// Filter by kind
			artifactType := annotations[AnnotationArtifactType]
			if kind != "" && artifactType != kind.String() {
				continue
			}

			// Filter by name
			artifactName := annotations[AnnotationArtifactName]
			if name != "" && artifactName != name {
				continue
			}

			// Parse reference
			ref, err := c.refFromAnnotations(annotations)
			if err != nil {
				c.logger.V(2).Info("failed to parse reference from annotations", "tag", tag, "error", err)
				continue
			}

			info := c.infoFromAnnotations(ref, desc, annotations)
			// Extract the tag label from the OCI tag.
			// Digest-pinned references use '@' and must preserve the full
			// digest value (e.g., "sha256:..."), while version/alias tags use ':'.
			if idx := strings.LastIndex(tag, "@"); idx >= 0 {
				info.Tag = tag[idx+1:]
			} else if idx := strings.LastIndex(tag, ":"); idx >= 0 {
				info.Tag = tag[idx+1:]
			}
			results = append(results, info)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tags: %w", err)
	}

	return results, nil
}

// Exists checks if an artifact exists in the catalog.
func (c *LocalCatalog) Exists(ctx context.Context, ref Reference) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.existsLocked(ctx, ref), nil
}

func (c *LocalCatalog) existsLocked(ctx context.Context, ref Reference) bool {
	tag := c.tagForRef(ref)
	_, err := c.store.Resolve(ctx, tag)
	if err == nil {
		return true
	}

	// Tag lookup failed -- fall back to annotation-based listing to handle
	// artifacts whose OCI tag doesn't match the canonical format.
	artifacts, listErr := c.listLocked(ctx, ref.Kind, ref.Name)
	if listErr != nil {
		return false
	}
	for _, a := range artifacts {
		if ref.HasVersion() && a.Reference.Version != nil && a.Reference.Version.Equal(ref.Version) {
			return true
		}
		if ref.HasDigest() && a.Digest == ref.Digest {
			return true
		}
		if !ref.HasVersion() && !ref.HasDigest() {
			return true
		}
	}
	return false
}

// Delete removes an artifact from the catalog.
func (c *LocalCatalog) Delete(ctx context.Context, ref Reference) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Try canonical tag first.
	tag := c.tagForRef(ref)
	desc, err := c.store.Resolve(ctx, tag)
	if err != nil {
		// Fall back to resolving via annotations for mismatched tags.
		tag, desc, err = c.findTagByAnnotations(ctx, ref)
		if err != nil {
			return &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
		}
	}

	// Delete the tag (blobs are orphaned but not deleted - would need GC)
	if err := c.store.Untag(ctx, tag); err != nil {
		return fmt.Errorf("failed to delete artifact: %w", err)
	}

	c.logger.V(1).Info("deleted artifact",
		"name", ref.Name,
		"version", ref.Version.String(),
		"digest", desc.Digest.String())

	return nil
}

// Helper methods

func (c *LocalCatalog) tagForRef(ref Reference) string {
	// Format: kind/name:version or kind/name@digest
	if ref.HasDigest() {
		return fmt.Sprintf("%s/%s@%s", ref.Kind, ref.Name, ref.Digest)
	}
	if ref.HasVersion() {
		return fmt.Sprintf("%s/%s:%s", ref.Kind, ref.Name, ref.Version.String())
	}
	return fmt.Sprintf("%s/%s", ref.Kind, ref.Name)
}

func (c *LocalCatalog) pushBlob(ctx context.Context, mediaType string, content []byte) (ocispec.Descriptor, error) {
	desc := ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(content),
		Size:      int64(len(content)),
	}

	if err := c.store.Push(ctx, desc, bytes.NewReader(content)); err != nil {
		// Check if blob already exists (expected for content-addressable storage)
		if !errors.Is(err, errdef.ErrAlreadyExists) {
			return ocispec.Descriptor{}, err
		}
	}

	return desc, nil
}

func (c *LocalCatalog) fetchBlob(ctx context.Context, desc ocispec.Descriptor) ([]byte, error) {
	rc, err := c.store.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(rc); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// findTagByAnnotations searches all OCI tags for an artifact matching the
// given reference by annotation metadata. Returns the actual stored tag and
// its descriptor. This handles mismatched tags (e.g., "/name:1.0.0" instead
// of "solution/name:1.0.0").
func (c *LocalCatalog) findTagByAnnotations(ctx context.Context, ref Reference) (string, ocispec.Descriptor, error) {
	var foundTag string
	var foundDesc ocispec.Descriptor

	err := c.store.Tags(ctx, "", func(tags []string) error {
		for _, tag := range tags {
			desc, err := c.store.Resolve(ctx, tag)
			if err != nil {
				continue
			}
			annotations, err := c.getManifestAnnotations(ctx, desc)
			if err != nil {
				continue
			}

			artifactName := annotations[AnnotationArtifactName]
			artifactKind := ArtifactKind(annotations[AnnotationArtifactType])

			if artifactName != ref.Name {
				continue
			}
			if ref.Kind != "" && artifactKind != ref.Kind {
				continue
			}
			if ref.HasDigest() && desc.Digest.String() != ref.Digest {
				continue
			}
			if ref.HasVersion() {
				versionStr := annotations[AnnotationVersion]
				if versionStr != ref.Version.String() {
					continue
				}
			}

			foundTag = tag
			foundDesc = desc
			return errStopIteration
		}
		return nil
	})

	if err != nil && !errors.Is(err, errStopIteration) {
		return "", ocispec.Descriptor{}, err
	}
	if foundTag == "" {
		return "", ocispec.Descriptor{}, fmt.Errorf("not found")
	}
	return foundTag, foundDesc, nil
}

// errStopIteration is a sentinel used to break out of tag iteration early.
var errStopIteration = fmt.Errorf("stop iteration")

// fetchManifestByDigest fetches and unmarshals an OCI manifest using its
// content digest. This avoids the need to reconstruct a tag which may not
// match the actual stored tag (e.g., when the artifact was pulled from a
// remote registry with an empty kind).
func (c *LocalCatalog) fetchManifestByDigest(ctx context.Context, dgst string) (ocispec.Manifest, error) {
	parsedDigest, err := digest.Parse(dgst)
	if err != nil {
		return ocispec.Manifest{}, fmt.Errorf("invalid digest %q: %w", dgst, err)
	}

	desc := ocispec.Descriptor{
		Digest:    parsedDigest,
		MediaType: ocispec.MediaTypeImageManifest,
	}

	data, err := c.fetchBlob(ctx, desc)
	if err != nil {
		return ocispec.Manifest{}, fmt.Errorf("failed to fetch manifest: %w", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ocispec.Manifest{}, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	return manifest, nil
}

func (c *LocalCatalog) getManifestAnnotations(ctx context.Context, desc ocispec.Descriptor) (map[string]string, error) {
	data, err := c.fetchBlob(ctx, desc)
	if err != nil {
		return nil, err
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	return manifest.Annotations, nil
}

func (c *LocalCatalog) refFromAnnotations(annotations map[string]string) (Reference, error) {
	kind := ArtifactKind(annotations[AnnotationArtifactType])
	name := annotations[AnnotationArtifactName]
	versionStr := annotations[AnnotationVersion]

	if name == "" {
		return Reference{}, fmt.Errorf("missing artifact name annotation")
	}

	ref := Reference{
		Kind: kind,
		Name: name,
	}

	if versionStr != "" {
		v, err := semver.NewVersion(versionStr)
		if err != nil {
			return Reference{}, fmt.Errorf("invalid version annotation: %w", err)
		}
		ref.Version = v
	}

	return ref, nil
}

func (c *LocalCatalog) infoFromAnnotations(ref Reference, desc ocispec.Descriptor, annotations map[string]string) ArtifactInfo {
	var createdAt time.Time
	if created := annotations[AnnotationCreated]; created != "" {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			createdAt = t
		}
	}

	// Merge descriptor-level annotations (from OCI index.json) into the
	// manifest-level annotations. Descriptor annotations take precedence
	// because they contain local-only metadata like AnnotationOrigin that
	// is set during Tag() without modifying the manifest blob.
	merged := make(map[string]string, len(annotations)+len(desc.Annotations))
	for k, v := range annotations {
		merged[k] = v
	}
	for k, v := range desc.Annotations {
		merged[k] = v
	}

	return ArtifactInfo{
		Reference:   ref,
		Digest:      desc.Digest.String(),
		CreatedAt:   createdAt,
		Size:        desc.Size,
		Annotations: merged,
		Catalog:     LocalCatalogName,
	}
}

// Tag creates an alias tag for an existing artifact.
// The source reference must have a version or digest to resolve.
// The alias is a freeform string (e.g., "stable", "production").
// Returns the previous version string if the alias already existed (empty otherwise).
func (c *LocalCatalog) Tag(ctx context.Context, ref Reference, alias string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Resolve the source artifact to get its descriptor
	tag := c.tagForRef(ref)
	desc, err := c.store.Resolve(ctx, tag)
	if err != nil {
		return "", &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	}

	// Build alias tag in the same format: kind/name:alias
	aliasTag := fmt.Sprintf("%s/%s:%s", ref.Kind, ref.Name, alias)

	// Check if alias already exists and points to a different artifact
	var oldVersion string
	if oldDesc, resolveErr := c.store.Resolve(ctx, aliasTag); resolveErr == nil {
		if oldDesc.Digest != desc.Digest {
			// Find the version of the old artifact
			if annotations, annErr := c.getManifestAnnotations(ctx, oldDesc); annErr == nil {
				oldVersion = annotations[AnnotationVersion]
			}
		}
	}

	if err := c.store.Tag(ctx, desc, aliasTag); err != nil {
		return "", fmt.Errorf("failed to tag artifact: %w", err)
	}

	c.logger.V(1).Info("tagged artifact",
		"name", ref.Name,
		"source", tag,
		"alias", alias)

	return oldVersion, nil
}

// PruneResult contains statistics from a prune operation.
type PruneResult struct {
	// RemovedManifests is the number of orphaned manifests removed
	RemovedManifests int `json:"removedManifests" yaml:"removedManifests"`
	// RemovedBlobs is the number of orphaned blobs removed
	RemovedBlobs int `json:"removedBlobs" yaml:"removedBlobs"`
	// ReclaimedBytes is the total bytes freed
	ReclaimedBytes int64 `json:"reclaimedBytes" yaml:"reclaimedBytes"`
}

// Prune removes orphaned blobs and manifests from the catalog.
// Orphaned content is any blob or manifest not referenced by a tagged artifact.
func (c *LocalCatalog) Prune(ctx context.Context) (PruneResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := PruneResult{}

	// Step 1: Collect all digests referenced by tags
	referencedDigests := make(map[string]bool)

	err := c.store.Tags(ctx, "", func(tags []string) error {
		for _, tag := range tags {
			desc, err := c.store.Resolve(ctx, tag)
			if err != nil {
				c.logger.V(2).Info("failed to resolve tag during prune", "tag", tag, "error", err)
				continue
			}
			// Mark the manifest as referenced
			referencedDigests[desc.Digest.String()] = true

			// Also mark all blobs referenced by this manifest
			if err := c.markReferencedBlobs(ctx, desc, referencedDigests); err != nil {
				c.logger.V(2).Info("failed to collect blob references", "digest", desc.Digest.String(), "error", err)
			}
		}
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("failed to enumerate tags: %w", err)
	}

	// Step 2: Read the index.json to find all manifests
	indexPath := filepath.Join(c.path, "index.json")
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No index = nothing to prune
			return result, nil
		}
		return result, fmt.Errorf("failed to read index.json: %w", err)
	}

	var index ocispec.Index
	if err := json.Unmarshal(indexData, &index); err != nil {
		return result, fmt.Errorf("failed to parse index.json: %w", err)
	}

	// Step 3: Filter manifests to keep only referenced ones
	var keptManifests []ocispec.Descriptor
	for _, manifest := range index.Manifests {
		if referencedDigests[manifest.Digest.String()] {
			keptManifests = append(keptManifests, manifest)
		} else {
			result.RemovedManifests++
			result.ReclaimedBytes += manifest.Size
			c.logger.V(1).Info("pruning orphaned manifest",
				"digest", manifest.Digest.String(),
				"name", manifest.Annotations[AnnotationArtifactName],
				"version", manifest.Annotations[AnnotationVersion])
		}
	}

	// Step 4: Write updated index if we removed anything
	if result.RemovedManifests > 0 {
		index.Manifests = keptManifests
		updatedIndex, err := json.MarshalIndent(index, "", "  ")
		if err != nil {
			return result, fmt.Errorf("failed to marshal updated index: %w", err)
		}
		if err := os.WriteFile(indexPath, updatedIndex, 0o600); err != nil {
			return result, fmt.Errorf("failed to write updated index: %w", err)
		}
	}

	// Step 5: Clean up orphaned blobs in blobs/sha256/
	blobsDir := filepath.Join(c.path, "blobs", "sha256")
	entries, err := os.ReadDir(blobsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, fmt.Errorf("failed to read blobs directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		digest := "sha256:" + entry.Name()
		if !referencedDigests[digest] {
			blobPath := filepath.Join(blobsDir, entry.Name())
			info, err := entry.Info()
			if err == nil {
				result.ReclaimedBytes += info.Size()
			}
			if err := os.Remove(blobPath); err != nil {
				c.logger.V(2).Info("failed to remove orphaned blob", "digest", digest, "error", err)
				continue
			}
			result.RemovedBlobs++
			c.logger.V(1).Info("pruned orphaned blob", "digest", digest)
		}
	}

	return result, nil
}

// SaveResult contains information about the save operation.
type SaveResult struct {
	// Reference is the artifact that was saved.
	Reference Reference `json:"reference" yaml:"reference"`
	// OutputPath is the path to the created archive.
	OutputPath string `json:"outputPath" yaml:"outputPath"`
	// Size is the size of the archive in bytes.
	Size int64 `json:"size" yaml:"size"`
	// Digest is the manifest digest.
	Digest string `json:"digest" yaml:"digest"`
}

// Save exports an artifact to an OCI Image Layout tar archive.
// If version is empty, exports the latest version.
func (c *LocalCatalog) Save(ctx context.Context, name, version, outputPath string) (SaveResult, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Build reference (kind is inferred from catalog lookup)
	ref := Reference{
		Kind: ArtifactKindSolution, // Default to solution
		Name: name,
	}
	if version != "" {
		v, err := semver.NewVersion(version)
		if err != nil {
			return SaveResult{}, fmt.Errorf("invalid version %q: %w", version, err)
		}
		ref.Version = v
	}

	// Resolve the artifact (finds latest if no version specified)
	info, err := c.resolveLocked(ctx, ref)
	if err != nil {
		return SaveResult{}, err
	}
	ref = info.Reference // Update with resolved version

	// Get manifest descriptor
	tag := c.tagForRef(ref)
	manifestDesc, err := c.store.Resolve(ctx, tag)
	if err != nil {
		return SaveResult{}, &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	}

	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return SaveResult{}, fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	// Create a tar writer
	tw := NewOCITarWriter(outFile)
	defer tw.Close()

	// Write OCI layout file
	if err := tw.WriteOCILayout(); err != nil {
		return SaveResult{}, fmt.Errorf("failed to write oci-layout: %w", err)
	}

	// Collect all blobs to export (manifest + config + layers)
	blobs := make(map[string]ocispec.Descriptor)
	if err := c.collectBlobsForExport(ctx, manifestDesc, blobs); err != nil {
		return SaveResult{}, fmt.Errorf("failed to collect blobs: %w", err)
	}

	// Write all blobs
	for _, desc := range blobs {
		data, err := c.fetchBlob(ctx, desc)
		if err != nil {
			return SaveResult{}, fmt.Errorf("failed to fetch blob %s: %w", desc.Digest, err)
		}
		if err := tw.WriteBlob(desc.Digest.String(), data); err != nil {
			return SaveResult{}, fmt.Errorf("failed to write blob %s: %w", desc.Digest, err)
		}
	}

	// Write index.json with the manifest
	index := ocispec.Index{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{
			{
				MediaType:   manifestDesc.MediaType,
				Digest:      manifestDesc.Digest,
				Size:        manifestDesc.Size,
				Annotations: info.Annotations,
			},
		},
	}
	if err := tw.WriteIndex(index); err != nil {
		return SaveResult{}, fmt.Errorf("failed to write index: %w", err)
	}

	// Get file size
	fileInfo, err := outFile.Stat()
	if err != nil {
		return SaveResult{}, fmt.Errorf("failed to stat output file: %w", err)
	}

	c.logger.V(1).Info("saved artifact to archive",
		"name", ref.Name,
		"version", ref.Version.String(),
		"output", outputPath,
		"size", fileInfo.Size())

	return SaveResult{
		Reference:  ref,
		OutputPath: outputPath,
		Size:       fileInfo.Size(),
		Digest:     manifestDesc.Digest.String(),
	}, nil
}

// collectBlobsForExport recursively collects all blobs referenced by a
// manifest or image index.
func (c *LocalCatalog) collectBlobsForExport(ctx context.Context, desc ocispec.Descriptor, blobs map[string]ocispec.Descriptor) error {
	// Add this blob
	blobs[desc.Digest.String()] = desc

	// If it's an image index, recursively collect all referenced manifests
	if IsImageIndex(desc) {
		data, err := c.fetchBlob(ctx, desc)
		if err != nil {
			return err
		}

		var index ocispec.Index
		if err := json.Unmarshal(data, &index); err != nil {
			return err
		}

		for _, manifestDesc := range index.Manifests {
			if err := c.collectBlobsForExport(ctx, manifestDesc, blobs); err != nil {
				return err
			}
		}
		return nil
	}

	// If it's a manifest, also collect config and layers
	if desc.MediaType == ocispec.MediaTypeImageManifest {
		data, err := c.fetchBlob(ctx, desc)
		if err != nil {
			return err
		}

		var manifest ocispec.Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return err
		}

		// Add config
		blobs[manifest.Config.Digest.String()] = manifest.Config

		// Add layers
		for _, layer := range manifest.Layers {
			blobs[layer.Digest.String()] = layer
		}
	}

	return nil
}

// LoadResult contains information about the load operation.
type LoadResult struct {
	// Reference is the artifact that was loaded.
	Reference Reference `json:"reference" yaml:"reference"`
	// Digest is the manifest digest.
	Digest string `json:"digest" yaml:"digest"`
	// Size is the artifact content size in bytes.
	Size int64 `json:"size" yaml:"size"`
	// CreatedAt is when the artifact was originally created.
	CreatedAt time.Time `json:"createdAt" yaml:"createdAt"`
}

// Load imports an artifact from an OCI Image Layout tar archive.
// Returns ErrArtifactExists if artifact already exists and force is false.
func (c *LocalCatalog) Load(ctx context.Context, inputPath string, force bool) (LoadResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Open and parse the tar archive
	file, err := os.Open(inputPath)
	if err != nil {
		return LoadResult{}, fmt.Errorf("failed to open archive: %w", err)
	}
	defer file.Close()

	// Read the archive
	reader, err := NewOCITarReader(file)
	if err != nil {
		return LoadResult{}, fmt.Errorf("failed to read archive: %w", err)
	}

	// Validate OCI layout
	if !reader.HasValidLayout() {
		return LoadResult{}, fmt.Errorf("invalid archive: missing oci-layout file")
	}

	// Get the index
	index, err := reader.Index()
	if err != nil {
		return LoadResult{}, fmt.Errorf("failed to read index: %w", err)
	}

	if len(index.Manifests) == 0 {
		return LoadResult{}, fmt.Errorf("archive contains no artifacts")
	}

	// Get first manifest descriptor
	manifestDesc := index.Manifests[0]

	// Extract artifact info from annotations
	annotations := manifestDesc.Annotations
	if annotations == nil {
		return LoadResult{}, fmt.Errorf("manifest has no annotations")
	}

	ref, err := c.refFromAnnotations(annotations)
	if err != nil {
		return LoadResult{}, fmt.Errorf("failed to parse artifact reference: %w", err)
	}

	// Check if artifact already exists
	if c.existsLocked(ctx, ref) && !force {
		return LoadResult{}, &ArtifactExistsError{Reference: ref, Catalog: LocalCatalogName}
	}

	// Push all blobs to the store
	for digestStr, data := range reader.Blobs() {
		d, err := digest.Parse(digestStr)
		if err != nil {
			return LoadResult{}, fmt.Errorf("invalid digest %q: %w", digestStr, err)
		}

		desc := ocispec.Descriptor{
			Digest: d,
			Size:   int64(len(data)),
		}

		if err := c.store.Push(ctx, desc, bytes.NewReader(data)); err != nil {
			if !errors.Is(err, errdef.ErrAlreadyExists) {
				return LoadResult{}, fmt.Errorf("failed to push blob %s: %w", digestStr, err)
			}
		}
	}

	// Tag the manifest
	tag := c.tagForRef(ref)
	if err := c.store.Tag(ctx, manifestDesc, tag); err != nil {
		return LoadResult{}, fmt.Errorf("failed to tag artifact: %w", err)
	}

	// Parse created time from annotations
	var createdAt time.Time
	if created := annotations[AnnotationCreated]; created != "" {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			createdAt = t
		}
	}

	c.logger.V(1).Info("loaded artifact from archive",
		"name", ref.Name,
		"version", ref.Version.String(),
		"digest", manifestDesc.Digest.String(),
		"input", inputPath)

	return LoadResult{
		Reference: ref,
		Digest:    manifestDesc.Digest.String(),
		Size:      manifestDesc.Size,
		CreatedAt: createdAt,
	}, nil
}

// markReferencedBlobs recursively marks all blobs referenced by a manifest
// or image index.
func (c *LocalCatalog) markReferencedBlobs(ctx context.Context, desc ocispec.Descriptor, refs map[string]bool) error {
	// Fetch the manifest/index content
	rc, err := c.store.Fetch(ctx, desc)
	if err != nil {
		return err
	}
	defer rc.Close()

	manifestData, err := io.ReadAll(rc)
	if err != nil {
		return err
	}

	// Mark the blob itself
	refs[desc.Digest.String()] = true

	// Check if this is an image index (multi-platform artifact)
	if IsImageIndex(desc) {
		var index ocispec.Index
		if err := json.Unmarshal(manifestData, &index); err != nil {
			return nil // not a valid index, just mark the blob
		}
		// Recursively mark all referenced manifests in the index
		for _, manifestDesc := range index.Manifests {
			if err := c.markReferencedBlobs(ctx, manifestDesc, refs); err != nil {
				c.logger.V(2).Info("failed to mark blobs for index manifest",
					"digest", manifestDesc.Digest.String(), "error", err)
			}
		}
		return nil
	}

	// Parse as OCI manifest to get config and layers
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		// Not a valid manifest, just mark the blob
		return nil
	}

	// Mark config blob
	if manifest.Config.Digest != "" {
		refs[manifest.Config.Digest.String()] = true
	}

	// Mark layer blobs
	for _, layer := range manifest.Layers {
		refs[layer.Digest.String()] = true
	}

	return nil
}

// Ensure LocalCatalog implements Catalog.
var _ Catalog = (*LocalCatalog)(nil)

// StoreDedup saves a solution with content-addressable deduplicated layers.
// manifestJSON is the bundle manifest (v2), smallTar is grouped small files,
// and blobLayers are individual large file blobs with their media types.
func (c *LocalCatalog) StoreDedup(ctx context.Context, ref Reference, solutionYAML, manifestJSON, smallTar []byte, blobLayers [][]byte, annotations map[string]string, force bool) (ArtifactInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !force {
		if c.existsLocked(ctx, ref) {
			return ArtifactInfo{}, &ArtifactExistsError{Reference: ref, Catalog: LocalCatalogName}
		}
	}

	now := time.Now().UTC()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[AnnotationArtifactType] = ref.Kind.String()
	annotations[AnnotationArtifactName] = ref.Name
	if ref.Version != nil {
		annotations[AnnotationVersion] = ref.Version.String()
	}
	annotations[AnnotationCreated] = now.Format(time.RFC3339)

	// Layer 0: solution YAML
	contentDesc, err := c.pushBlob(ctx, MediaTypeSolutionContent, solutionYAML)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to push solution content: %w", err)
	}

	// Config blob
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

	configDesc, err := c.pushBlob(ctx, ConfigMediaTypeForKind(ref.Kind), configData)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to push config: %w", err)
	}

	layers := []ocispec.Descriptor{contentDesc}

	// Layer 1: bundle manifest JSON
	manifestDesc, err := c.pushBlob(ctx, MediaTypeSolutionBundleManifest, manifestJSON)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to push bundle manifest: %w", err)
	}
	layers = append(layers, manifestDesc)

	// Layer 2 (optional): small files tar
	if len(smallTar) > 0 {
		smallDesc, err := c.pushBlob(ctx, MediaTypeSolutionBundleSmallTar, smallTar)
		if err != nil {
			return ArtifactInfo{}, fmt.Errorf("failed to push small files tar: %w", err)
		}
		layers = append(layers, smallDesc)
	}

	// Layer 3+: individual file blobs (content-addressable — duplicates are automatically deduped by digest)
	for _, blob := range blobLayers {
		blobDesc, err := c.pushBlob(ctx, MediaTypeSolutionBundleBlob, blob)
		if err != nil {
			return ArtifactInfo{}, fmt.Errorf("failed to push bundle blob: %w", err)
		}
		layers = append(layers, blobDesc)
	}

	// Create OCI manifest
	ociManifest := ocispec.Manifest{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		MediaType:   ocispec.MediaTypeImageManifest,
		Config:      configDesc,
		Layers:      layers,
		Annotations: annotations,
	}

	ociManifestData, err := json.Marshal(ociManifest)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to marshal manifest: %w", err)
	}

	ociManifestDesc, err := c.pushBlob(ctx, ocispec.MediaTypeImageManifest, ociManifestData)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to push OCI manifest: %w", err)
	}

	tag := c.tagForRef(ref)
	ociManifestDesc.Annotations = annotations
	if err := c.store.Tag(ctx, ociManifestDesc, tag); err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to tag manifest: %w", err)
	}

	return ArtifactInfo{
		Reference:   ref,
		Digest:      ociManifestDesc.Digest.String(),
		CreatedAt:   now,
		Size:        int64(len(solutionYAML)),
		Annotations: annotations,
		Catalog:     LocalCatalogName,
	}, nil
}

// FetchDedup retrieves a deduplicated (v2) bundle by fetching all layers.
// Returns the solution YAML, bundle manifest JSON, and a layer fetcher
// that can retrieve individual layers by index.
func (c *LocalCatalog) FetchDedup(ctx context.Context, ref Reference) (solutionYAML, manifestJSON []byte, layerFetcher func(layer int) ([]byte, error), info ArtifactInfo, err error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info, err = c.resolveLocked(ctx, ref)
	if err != nil {
		return nil, nil, nil, ArtifactInfo{}, err
	}

	tag := c.tagForRef(info.Reference)
	manifestDesc, err := c.store.Resolve(ctx, tag)
	if err != nil {
		return nil, nil, nil, ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	}

	manifestData, err := c.fetchBlob(ctx, manifestDesc)
	if err != nil {
		return nil, nil, nil, ArtifactInfo{}, fmt.Errorf("failed to fetch manifest: %w", err)
	}

	var ociManifest ocispec.Manifest
	if err := json.Unmarshal(manifestData, &ociManifest); err != nil {
		return nil, nil, nil, ArtifactInfo{}, fmt.Errorf("failed to unmarshal OCI manifest: %w", err)
	}

	if len(ociManifest.Layers) < 2 {
		return nil, nil, nil, ArtifactInfo{}, fmt.Errorf("manifest has insufficient layers for dedup bundle")
	}

	// Layer 0: solution YAML
	solutionYAML, err = c.fetchBlob(ctx, ociManifest.Layers[0])
	if err != nil {
		return nil, nil, nil, ArtifactInfo{}, fmt.Errorf("failed to fetch solution YAML: %w", err)
	}

	// Layer 1: bundle manifest
	manifestJSON, err = c.fetchBlob(ctx, ociManifest.Layers[1])
	if err != nil {
		return nil, nil, nil, ArtifactInfo{}, fmt.Errorf("failed to fetch bundle manifest: %w", err)
	}

	// Capture layers for the fetcher closure
	allLayers := ociManifest.Layers
	store := c

	layerFetcher = func(layer int) ([]byte, error) {
		if layer < 0 || layer >= len(allLayers) {
			return nil, fmt.Errorf("layer index %d out of range (0-%d)", layer, len(allLayers)-1)
		}
		return store.fetchBlob(ctx, allLayers[layer])
	}

	return solutionYAML, manifestJSON, layerFetcher, info, nil
}
