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
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"
)

const (
	// LocalCatalogName is the name of the built-in local catalog.
	LocalCatalogName = "local"
)

// LocalCatalog implements Catalog using a local OCI layout store.
//
// TODO: concurrency across instances and processes. mu only serializes one
// instance, but caching fetched remote solutions now writes the shared OCI
// index.json, so concurrent run/build processes (or two LocalCatalog
// instances on the same path) can lose tags. Follow up with:
//   - a per-path singleton so all in-process users of a path share one store
//     and lock (NewLocalCatalog/NewLocalCatalogAt return the same instance);
//   - a cross-process file lock around index.json writes (and Prune);
//   - IngestVerbatim no longer holding the write lock for the whole network
//     copy: stage the blobs first, then take the lock only to tag.
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
// Optional extraLayers are pushed and appended last (empty layers skipped);
// they are located on read by media type, not index.
func (c *LocalCatalog) Store(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool, extraLayers ...Layer) (ArtifactInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ref = writeRef(ref)

	// Check if artifact already exists (unless force is set)
	if !force {
		exists, err := c.existsLocked(ctx, ref)
		if err != nil {
			return ArtifactInfo{}, err
		}
		if exists {
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
		annotations[AnnotationOrigin] = originBuilt
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

	// Append optional extra layers (e.g. solution lock) last so they never
	// disturb the bundle layer's positional contract; located by media type.
	layers, err = c.appendExtraLayers(ctx, layers, extraLayers)
	if err != nil {
		return ArtifactInfo{}, err
	}

	// Create manifest — exclude descriptor-only annotations (see
	// descriptorOnlyAnnotations) from the blob so they don't affect the
	// content digest. They are attached to the descriptor (index-level) below.
	manifest := ocispec.Manifest{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		MediaType:   ocispec.MediaTypeImageManifest,
		Config:      configDesc,
		Layers:      layers,
		Annotations: stripDescriptorOnly(annotations),
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to marshal manifest: %w", err)
	}

	manifestDesc, err := c.pushBlob(ctx, ocispec.MediaTypeImageManifest, manifestData)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("failed to push manifest: %w", err)
	}

	// Tag the manifest — full annotations on the descriptor (index-level only)
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

	content, bundleData, err := readSolutionLayers(manifest, func(desc ocispec.Descriptor) ([]byte, error) {
		return c.fetchBlob(ctx, desc)
	})
	if err != nil {
		return nil, nil, ArtifactInfo{}, err
	}
	return content, bundleData, info, nil
}

// FetchWithLayer retrieves an artifact's primary content together with one or
// more auxiliary layers (each located by media type) in one manifest lookup.
// The returned map is keyed by the requested media type; absent layers are
// omitted from the map (not an error).
func (c *LocalCatalog) FetchWithLayer(ctx context.Context, ref Reference, mediaTypes ...string) ([]byte, map[string][]byte, ArtifactInfo, error) {
	for _, mt := range mediaTypes {
		if !isAuxLayerMediaType(mt) {
			return nil, nil, ArtifactInfo{}, fmt.Errorf("FetchWithLayer: %q is not a fetchable auxiliary layer", mt)
		}
	}

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
	contentData, err := c.fetchBlob(ctx, manifest.Layers[0])
	if err != nil {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to fetch content: %w", err)
	}

	// Fetch each requested auxiliary layer (located by media type, never index).
	layers := make(map[string][]byte, len(mediaTypes))
	for _, mt := range mediaTypes {
		var layerData []byte
		switch mt {
		case MediaTypeSolutionBundle:
			// Probe v1 (plain tar) first, then v2 deduplicated.
			if desc, ok := findLayerByMediaType(manifest, MediaTypeSolutionBundle); ok {
				layerData, err = c.fetchBlob(ctx, desc)
			} else if _, ok := findLayerByMediaType(manifest, MediaTypeSolutionBundleManifest); ok {
				layerData, err = c.reassembleDedup(ctx, manifest)
			}
		default:
			if desc, ok := findLayerByMediaType(manifest, mt); ok {
				layerData, err = c.fetchBlob(ctx, desc)
			}
		}
		if err != nil {
			return nil, nil, ArtifactInfo{}, fmt.Errorf("failed to fetch %s layer: %w", mt, err)
		}
		if len(layerData) > 0 {
			layers[mt] = layerData
		}
	}

	return contentData, layers, info, nil
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

// resolveLocked resolves ref to one artifact. Caller must hold the read lock.
//
// The OCI tag is a pure function of the artifact's identity
// (kind, name, version, origin), so a ref that carries all of it is a lookup:
// the tag either exists or the artifact is absent, and nothing is scanned.
// A ref that leaves any of it open (no version, or no origin) is a search and
// is answered from annotation identity through findLocked, which owns
// latest-version, pre-release and cross-origin ambiguity semantics.
//
// Stored tags are keyed by version, so a digest cannot derive a tag. A ref that
// carries a digest is therefore a search too: find filters on the digest from
// annotation identity and returns the tag that actually holds it.
func (c *LocalCatalog) resolveLocked(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	if !hasFullIdentity(ref) {
		return c.resolveByQuery(ctx, ref)
	}

	tag := c.tagForRef(ref)
	desc, err := c.store.Resolve(ctx, tag)
	if err != nil {
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	}
	return c.infoForTag(ctx, tag, desc)
}

// hasFullIdentity reports whether ref carries every coordinate the OCI tag is
// derived from (kind, name, version, explicit origin) and no digest, so a tag
// lookup alone answers for it. Stored tags are keyed by version, so a digest
// cannot derive a tag and a ref carrying one is always a search.
func hasFullIdentity(ref Reference) bool {
	return !ref.HasDigest() && ref.HasVersion() && canGenerateTag(ref) == nil
}

// resolveByQuery answers an under-specified ref by annotation identity,
// reporting a miss against the caller's original ref.
func (c *LocalCatalog) resolveByQuery(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	info, err := c.findLocked(ctx, queryFromRef(ref), false)
	var notFound *ArtifactNotFoundError
	if errors.As(err, &notFound) {
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	}
	return info, err
}

// infoForTag builds the ArtifactInfo for the artifact stored under tag,
// recovering its identity (including origin) from annotations rather than
// from the caller's ref or the tag text.
func (c *LocalCatalog) infoForTag(ctx context.Context, tag string, desc ocispec.Descriptor) (ArtifactInfo, error) {
	manifestAnns, err := c.getManifestAnnotations(ctx, desc)
	if err != nil {
		return ArtifactInfo{}, err
	}
	// Descriptor annotations take precedence, matching infoFromAnnotations.
	ref, err := c.refFromAnnotations(mergeAnnotations(manifestAnns, desc.Annotations))
	if err != nil {
		return ArtifactInfo{}, err
	}
	info := c.infoFromAnnotations(ref, desc, manifestAnns)
	info.Tag = tagLabel(tag)
	return info, nil
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

			// Parse reference. Origin (AnnotationSourceCanonical) lives only on
			// the descriptor, so merge it in as infoForTag does.
			ref, err := c.refFromAnnotations(mergeAnnotations(annotations, desc.Annotations))
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

// Find resolves a single artifact by its annotation identity, mirroring
// LocalOciCatalog.Find. It returns one ArtifactInfo, or AmbiguousArtifactError
// when a query without an explicit source selector matches artifacts spanning
// multiple provenances or remote origins.
func (c *LocalCatalog) Find(ctx context.Context, query ArtifactQuery) (ArtifactInfo, error) {
	return c.find(ctx, query, false)
}

// FindStrict resolves a single artifact by annotation identity and rejects
// any query that matches more than one artifact.
func (c *LocalCatalog) FindStrict(ctx context.Context, query ArtifactQuery) (ArtifactInfo, error) {
	return c.find(ctx, query, true)
}

func (c *LocalCatalog) find(ctx context.Context, query ArtifactQuery, strict bool) (ArtifactInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.findLocked(ctx, query, strict)
}

// findLocked is find without locking. Caller must hold the read lock.
func (c *LocalCatalog) findLocked(ctx context.Context, query ArtifactQuery, strict bool) (ArtifactInfo, error) {
	if err := query.Validate(); err != nil {
		return ArtifactInfo{}, err
	}

	artifacts, err := c.findByAnnotation(ctx, query)
	if err != nil {
		return ArtifactInfo{}, err
	}
	// An open version means "latest": prefer stable releases over pre-releases
	// unless the context opts in. Strict lookups consider every match.
	if !strict && wantsLatest(query) {
		// Without a source selector, origin ambiguity is judged on every match
		// first: the stable-first preference picks a version within one origin
		// and must not silently pick the origin by filtering another one out.
		if query.Source.Kind == SourceAny {
			if err := checkAmbiguity(query, artifacts); err != nil {
				sortByVersionDesc(artifacts)
				return ArtifactInfo{}, err
			}
		}
		artifacts = latestCandidates(ctx, artifacts)
	}
	switch len(artifacts) {
	case 0:
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: Reference{Kind: query.Kind, Name: query.Name}, Catalog: LocalCatalogName}
	case 1:
		return artifacts[0], nil
	}

	// Several matches: order newest first so "latest wins" is a head pick.
	sortByVersionDesc(artifacts)

	if strict {
		return ArtifactInfo{}, &AmbiguousArtifactError{Query: query, Matches: artifacts}
	}

	// An explicit source selector already pins the origin, so every match shares
	// one provenance and the latest version is the answer.
	if query.Source.Kind == SourceBuilt || query.Source.Kind == SourceRemote {
		return artifacts[0], nil
	}

	// No source selector: matches may span provenances or origins, so comparing
	// versions across origins is meaningless without an ambiguity check.
	if err := checkAmbiguity(query, artifacts); err != nil {
		return ArtifactInfo{}, err
	}
	return artifacts[0], nil
}

// findByAnnotation resolves artifacts matching query by their annotation
// identity, reflecting origin (pulled vs built). Caller must hold the read lock.
func (c *LocalCatalog) findByAnnotation(ctx context.Context, query ArtifactQuery) ([]ArtifactInfo, error) {
	tagged, err := c.scanByAnnotation(ctx, query)
	if err != nil {
		return nil, err
	}
	artifacts := make([]ArtifactInfo, 0, len(tagged))
	for _, t := range tagged {
		artifacts = append(artifacts, t.info)
	}
	return artifacts, nil
}

// taggedArtifact is an annotation-search match together with the OCI tag and
// descriptor it is stored under. ArtifactInfo.Tag carries only the tag label,
// so callers that must act on the stored tag (such as Delete) need this.
type taggedArtifact struct {
	tag  string
	desc ocispec.Descriptor
	info ArtifactInfo
}

// scanByAnnotation is the single annotation-identity scan behind
// findByAnnotation and Delete. Caller must hold a lock.
func (c *LocalCatalog) scanByAnnotation(ctx context.Context, query ArtifactQuery) ([]taggedArtifact, error) {
	var artifacts []taggedArtifact
	err := c.store.Tags(ctx, "", func(tags []string) error {
		for _, tag := range tags {
			desc, err := c.store.Resolve(ctx, tag)
			if err != nil {
				continue
			}

			// Identity, including origin, comes from the artifact's own
			// annotations, not from the query, so each match carries its real
			// version and provenance even when the query left them open.
			info, err := c.infoForTag(ctx, tag, desc)
			if err != nil {
				continue
			}
			if !query.matches(info) {
				continue
			}
			artifacts = append(artifacts, taggedArtifact{tag: tag, desc: desc, info: info})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return artifacts, nil
}

// tagLabel extracts the label component from an OCI tag: the part after "@"
// (digest) or ":" (version/alias).
func tagLabel(tag string) string {
	if idx := strings.LastIndex(tag, "@"); idx >= 0 {
		return tag[idx+1:]
	}
	if idx := strings.LastIndex(tag, ":"); idx >= 0 {
		return tag[idx+1:]
	}
	return tag
}

// Exists checks if an artifact exists in the catalog.
//
// Pass a full identity (kind, name, version and an explicit origin: Built or
// Remote) for an exact tag lookup. A ref that leaves the version or origin
// open, or carries a digest, is answered by annotation identity and matches
// an artifact from any origin.
func (c *LocalCatalog) Exists(ctx context.Context, ref Reference) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.existsLocked(ctx, ref)
}

// existsLocked is Exists without locking. Caller must hold a lock.
//
// A ref with full identity is answered by its tag alone, so a miss is a
// definitive no and nothing is scanned. Anything else is a search over
// annotation identity, where any match counts, including an ambiguous one.
// An invalid query is reported as an error rather than as "not found".
func (c *LocalCatalog) existsLocked(ctx context.Context, ref Reference) (bool, error) {
	if hasFullIdentity(ref) {
		tag := c.tagForRef(ref)
		_, err := c.store.Resolve(ctx, tag)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, errdef.ErrNotFound):
			return false, nil
		default:
			return false, fmt.Errorf("checking tag %q: %w", tag, err)
		}
	}

	query := queryFromRef(ref)
	if err := query.Validate(); err != nil {
		return false, err
	}
	matches, err := c.findByAnnotation(ctx, query)
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

// Delete removes an artifact from the catalog.
//
// Like Resolve and Exists, a ref with full identity (kind, name, version and
// an explicit origin) is a tag lookup: a miss means the artifact is absent.
// Any other ref is a search over annotation identity, which also finds
// artifacts stored under a mismatched tag. A search matching more than one
// artifact (for example the same version built and pulled) is ambiguous and
// deletes nothing.
func (c *LocalCatalog) Delete(ctx context.Context, ref Reference) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	tag, desc, err := c.locateForDelete(ctx, ref)
	if err != nil {
		return err
	}

	// Delete the tag (blobs are orphaned but not deleted - would need GC)
	if err := c.store.Untag(ctx, tag); err != nil {
		return fmt.Errorf("failed to delete artifact: %w", err)
	}
	c.logger.V(1).Info("deleted artifact",
		"name", ref.Name,
		"version", ref.VersionOrDigest(),
		"digest", desc.Digest.String())

	return nil
}

// locateForDelete returns the stored tag and descriptor ref designates.
// Caller must hold the write lock.
func (c *LocalCatalog) locateForDelete(ctx context.Context, ref Reference) (string, ocispec.Descriptor, error) {
	notFound := &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}

	if hasFullIdentity(ref) {
		tag := c.tagForRef(ref)
		desc, err := c.store.Resolve(ctx, tag)
		if err != nil {
			return "", ocispec.Descriptor{}, notFound
		}
		return tag, desc, nil
	}

	query := queryFromRef(ref)
	if err := query.Validate(); err != nil {
		return "", ocispec.Descriptor{}, err
	}
	matches, err := c.scanByAnnotation(ctx, query)
	if err != nil {
		return "", ocispec.Descriptor{}, err
	}
	switch len(matches) {
	case 0:
		return "", ocispec.Descriptor{}, notFound
	case 1:
		return matches[0].tag, matches[0].desc, nil
	}

	infos := make([]ArtifactInfo, 0, len(matches))
	for _, m := range matches {
		infos = append(infos, m.info)
	}
	sortByVersionDesc(infos)
	return "", ocispec.Descriptor{}, &AmbiguousArtifactError{Query: query, Matches: infos}
}

// Helper methods

func (c *LocalCatalog) tagForRef(ref Reference) string {
	// Format: kind/name:version or kind/name@digest
	return buildLocalTag(ref)
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

// appendExtraLayers pushes each non-empty extra layer and appends its
// descriptor to layers, preserving order. Empty layers are skipped so callers
// can pass optional layers (e.g. a solution lock) unconditionally.
func (c *LocalCatalog) appendExtraLayers(ctx context.Context, layers []ocispec.Descriptor, extra []Layer) ([]ocispec.Descriptor, error) {
	for _, l := range extra {
		if len(l.Data) == 0 {
			continue
		}
		desc, err := c.pushBlob(ctx, l.MediaType, l.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to push %s layer: %w", l.MediaType, err)
		}
		layers = append(layers, desc)
	}
	return layers, nil
}

// findLayerByMediaType returns the first layer descriptor matching mediaType,
// and whether one was found. Extra layers (e.g. the solution lock) are located
// this way rather than by a fixed index.
func findLayerByMediaType(m ocispec.Manifest, mediaType string) (ocispec.Descriptor, bool) {
	for _, l := range m.Layers {
		if l.MediaType == mediaType {
			return l, true
		}
	}
	return ocispec.Descriptor{}, false
}

// ResolveContentDigest returns the content-layer digest and artifact metadata
// for the local catalog. Unlike Fetch/FetchByPlatform, this reads only the
// manifest JSON — it never loads the (potentially large) binary blob.
//
// For a multi-platform image index it selects the platform-specific manifest;
// for a single-platform manifest the platform argument is ignored. The
// mediaType selects which layer to read the digest from.
func (c *LocalCatalog) ResolveContentDigest(ctx context.Context, ref Reference, platform, mediaType string) (ContentDigestInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info, err := c.resolveLocked(ctx, ref)
	if err != nil {
		return ContentDigestInfo{}, err
	}

	tag := c.tagForRef(info.Reference)
	desc, err := c.store.Resolve(ctx, tag)
	if err != nil {
		return ContentDigestInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: LocalCatalogName}
	}

	// For a multi-platform image index, select the platform-specific manifest.
	manifestDesc := desc
	if IsImageIndex(desc) {
		indexData, err := c.fetchBlob(ctx, desc)
		if err != nil {
			return ContentDigestInfo{}, fmt.Errorf("fetching image index: %w", err)
		}

		var index ocispec.Index
		if err := json.Unmarshal(indexData, &index); err != nil {
			return ContentDigestInfo{}, fmt.Errorf("unmarshalling image index: %w", err)
		}

		platDesc, err := MatchPlatform(&index, platform)
		if err != nil {
			return ContentDigestInfo{}, err
		}
		manifestDesc = *platDesc
	}

	manifestData, err := c.fetchBlob(ctx, manifestDesc)
	if err != nil {
		return ContentDigestInfo{}, fmt.Errorf("fetching manifest: %w", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return ContentDigestInfo{}, fmt.Errorf("unmarshalling manifest: %w", err)
	}

	layer, ok := findLayerByMediaType(manifest, mediaType)
	if !ok {
		return ContentDigestInfo{}, fmt.Errorf("manifest has no layer with media type %s", mediaType)
	}

	info.Size = layer.Size
	return ContentDigestInfo{
		ArtifactInfo:  info,
		ContentDigest: layer.Digest.String(),
	}, nil
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

	// Provenance is binary: a canonical source marks a remote cache, its absence
	// marks an artifact built here.
	remote := annotations[AnnotationSourceCanonical]
	ref.Remote = remote
	ref.Built = remote == ""

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
		Canonical:   merged[AnnotationSourceCanonical],
	}
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
	if !force {
		exists, err := c.existsLocked(ctx, ref)
		if err != nil {
			return LoadResult{}, err
		}
		if exists {
			return LoadResult{}, &ArtifactExistsError{Reference: ref, Catalog: LocalCatalogName}
		}
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
// Optional extraLayers (e.g. the solution lock) are appended AFTER the file
// blobs so the deduplicated layers' positional indices remain valid.
func (c *LocalCatalog) StoreDedup(ctx context.Context, ref Reference, solutionYAML, manifestJSON, smallTar []byte, blobLayers [][]byte, annotations map[string]string, force bool, extraLayers ...Layer) (ArtifactInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ref = writeRef(ref)

	if !force {
		exists, err := c.existsLocked(ctx, ref)
		if err != nil {
			return ArtifactInfo{}, err
		}
		if exists {
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

	// Append optional extra layers (e.g. solution lock) AFTER the file blobs
	// so the dedup manifest's f.Layer indices stay valid; located by media type.
	layers, err = c.appendExtraLayers(ctx, layers, extraLayers)
	if err != nil {
		return ArtifactInfo{}, err
	}

	// Create OCI manifest — exclude descriptor-only annotations from the blob.
	ociManifest := ocispec.Manifest{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		MediaType:   ocispec.MediaTypeImageManifest,
		Config:      configDesc,
		Layers:      layers,
		Annotations: stripDescriptorOnly(annotations),
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

// IngestVerbatim copies an artifact from src into the local catalog unchanged
// and tags it under the local tag derived from localRef.
func (c *LocalCatalog) IngestVerbatim(
	ctx context.Context,
	src oras.ReadOnlyTarget,
	srcTag string,
	localRef Reference,
	copyOpts oras.CopyOptions,
) (ArtifactInfo, error) {
	tagger, err := NewLocalTagBuilder(localRef)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("tagging local reference %q: %w", localRef, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	desc, err := oras.Copy(ctx, src, srcTag, c.store, tagger.Tag(), copyOpts)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("copying artifact %q: %w", localRef, err)
	}

	// The manifest blob is copied byte for byte — that is what preserves the
	// digest — so the scafctl identity and provenance are layered onto the root
	// descriptor that oras.Copy returned. Descriptor annotations live in
	// index.json, not in the manifest, so this does not touch the content.
	tagged, err := tagger.Build(ctx, desc, c.store)
	if err != nil {
		return ArtifactInfo{}, err
	}

	return c.artifactInfo(tagger.Tag(), tagged, tagged.Annotations), nil
}

func (c *LocalCatalog) artifactInfo(tag string, desc ocispec.Descriptor, annotations map[string]string) ArtifactInfo {
	remote := annotations[AnnotationSourceCanonical]
	ref := Reference{
		Kind:   ArtifactKind(annotations[AnnotationArtifactType]),
		Name:   annotations[AnnotationArtifactName],
		Digest: desc.Digest.String(),
		Remote: remote,
		Built:  remote == "",
	}
	if v, err := semver.NewVersion(annotations[AnnotationVersion]); err == nil {
		ref.Version = v
	}

	return ArtifactInfo{
		Reference:   ref,
		Tag:         tag,
		Digest:      desc.Digest.String(),
		Size:        desc.Size,
		Annotations: annotations,
		Catalog:     LocalCatalogName,
	}
}

// ExportVerbatim streams an artifact from the local catalog to a destination target unchanged,
// acquiring a read lock for the duration of the transfer.
func (c *LocalCatalog) ExportVerbatim(
	ctx context.Context,
	srcTag string,
	dst oras.Target,
	dstTag string,
	copyOpts oras.CopyOptions,
) (ocispec.Descriptor, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	desc, err := oras.Copy(ctx, c.store, srcTag, dst, dstTag, copyOpts)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("publishing artifact %q: %w", srcTag, err)
	}
	return desc, nil
}

// ExportRebuilt re-pushes an artifact under target's identity so the name and
// version embedded in the manifest match what the destination publishes.
//
// Layers are re-pushed through LayerRef, which asks the destination whether it
// already holds each digest, so a rename shares every unchanged blob with the
// artifacts already in the repository and uploads only what is genuinely new.
func (c *LocalCatalog) ExportRebuilt(
	ctx context.Context,
	srcTag string,
	dst TargetWriter,
	dstTag string,
	target Reference,
	onProgress func(ocispec.Descriptor),
) (ocispec.Descriptor, error) {
	// A rebuilt artifact is content the destination never published, under a
	// name that may not exist there, so it carries no remote provenance.
	target.Remote = ""
	target.Built = true

	c.mu.RLock()
	defer c.mu.RUnlock()

	srcDesc, err := c.store.Resolve(ctx, srcTag)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("resolving local artifact %q: %w", srcTag, err)
	}

	builder, err := rebuildArtifact(ctx, c.store, srcDesc, target, onProgress)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("rebuilding artifact %q as %q: %w", srcTag, target, err)
	}

	desc, err := builder.Push(ctx, dst)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("pushing artifact %q: %w", target, err)
	}

	return NewTagBuilder(dstTag).Build(ctx, desc, dst)
}

// tagExists reports whether tag resolves in the local store. Only a
// not-found result means absent; any other failure is returned so a broken
// store is never mistaken for a free tag.
func (c *LocalCatalog) tagExists(ctx context.Context, tag string) (bool, error) {
	_, err := c.store.Resolve(ctx, tag)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, errdef.ErrNotFound):
		return false, nil
	default:
		return false, fmt.Errorf("resolving tag %q: %w", tag, err)
	}
}

// storeInternal pushes builder into the local store and tags it. The tag and
// the descriptor provenance are both derived from builder.Reference(), the
// same identity the builder stamps into the content. It returns the tagged
// descriptor, annotations included.
func (c *LocalCatalog) storeInternal(ctx context.Context, builder ArtifactBuilder, force bool) (ocispec.Descriptor, error) {
	return c.storeInternalWith(ctx, builder, force, nil)
}

// storeInternalWith is storeInternal with extra descriptor-only annotations
// (for example AnnotationOrigin) recorded on the tag. Identity and provenance
// derived from the builder's reference are stamped after them and always win.
func (c *LocalCatalog) storeInternalWith(ctx context.Context, builder ArtifactBuilder, force bool, descAnnotations map[string]string) (ocispec.Descriptor, error) {
	if builder == nil {
		return ocispec.Descriptor{}, &BuilderError{
			Stage:  BuildStageAssemble,
			Detail: "storing artifact: nil builder",
		}
	}

	ref := builder.Reference()
	tagger, err := NewLocalTagBuilder(ref)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	tagger.Annotations(descAnnotations)

	c.mu.Lock()
	defer c.mu.Unlock()

	if !force {
		exists, err := c.tagExists(ctx, tagger.Tag())
		if err != nil {
			return ocispec.Descriptor{}, err
		}
		if exists {
			return ocispec.Descriptor{}, &ArtifactExistsError{Reference: ref, Catalog: LocalCatalogName}
		}
	}
	desc, err := builder.Push(ctx, c.store)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("pushing artifact %q: %w", ref, err)
	}
	return tagger.Build(ctx, desc, c.store)
}

// storeArtifact stores an already-assembled artifact (a ManifestBuilder or an
// IndexBuilder) under the builder's reference, delegating to storeInternal. It
// complements Store, which stores raw content bytes, for callers that already
// hold a builder — e.g. a pulled-and-rebuilt artifact.
func (c *LocalCatalog) storeArtifact(ctx context.Context, builder ArtifactBuilder, force bool) (ArtifactInfo, error) {
	return c.storeArtifactWith(ctx, builder, force, nil)
}

func (c *LocalCatalog) storeArtifactWith(ctx context.Context, builder ArtifactBuilder, force bool, descAnnotations map[string]string) (ArtifactInfo, error) {
	desc, err := c.storeInternalWith(ctx, builder, force, descAnnotations)
	if err != nil {
		return ArtifactInfo{}, err
	}
	tag, err := tagForLocalRef(builder.Reference())
	if err != nil {
		return ArtifactInfo{}, err
	}
	return c.artifactInfo(tag, desc, desc.Annotations), nil
}

// originBuilt is the AnnotationOrigin value recorded for artifacts built into
// the local catalog.
const originBuilt = "built"

// StoreBuilt stores an artifact built on this machine under its built tag
// (<kind>/<name>:<version>). The existence check (unless force) and the
// overwrite only ever touch the built copy, so a pulled copy of the same
// name@version is never consulted or replaced. A reference that carries a
// remote origin is rejected: built content has no remote provenance.
func (c *LocalCatalog) StoreBuilt(ctx context.Context, b ArtifactBuilder, force bool) (ArtifactInfo, error) {
	if b == nil {
		return ArtifactInfo{}, &BuilderError{
			Stage:  BuildStageAssemble,
			Detail: "storing built artifact: nil builder",
		}
	}
	ref := b.Reference()
	if ref.Remote != "" {
		return ArtifactInfo{}, &InvalidReferenceError{Input: ref.String(), Message: "built artifact cannot carry a remote origin"}
	}
	return c.storeArtifactWith(ctx, b, force, map[string]string{AnnotationOrigin: originBuilt})
}
