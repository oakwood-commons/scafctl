// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/oakwood-commons/scafctl/pkg/catalog/mediatypes"
)

// MediaTypeSolutionBundle is the media type whose layer is persisted at the
// legacy top-level "bundle.tar.gz" path within a cache entry rather than under
// the entry's layers/ directory. It aliases the canonical value in the
// import-free mediatypes leaf package (the single source of truth), so it
// cannot drift and pkg/cache still avoids a dependency on pkg/catalog itself.
const MediaTypeSolutionBundle = mediatypes.SolutionBundle

// bundleFileName is the legacy top-level filename used for the bundle layer.
const bundleFileName = "bundle.tar.gz"

// ArtifactCache is a disk-based TTL cache for catalog artifacts.
// It stores artifact content (and optional auxiliary layers) in a structured
// directory layout: {dir}/{kind}/{safe(name@version)}/
//
// Each cache entry contains:
//   - content        - the primary artifact bytes (e.g., solution YAML)
//   - bundle.tar.gz  - the bundle layer, when present (MediaTypeSolutionBundle)
//   - layers/        - a directory of any other auxiliary layer files, one per
//     media type (e.g. the lock layer); absent when there are none
//   - meta.json      - creation timestamp, content digest, and the list of
//     stored layer media types
type ArtifactCache struct {
	dir string
	ttl time.Duration
}

// artifactCacheMeta holds metadata written alongside cached artifact files.
type artifactCacheMeta struct {
	CreatedAt time.Time `json:"createdAt"`
	Digest    string    `json:"digest,omitempty"`
	// Layers lists the media types of the auxiliary layers stored for the
	// entry. The bundle media type is persisted at the top-level bundle.tar.gz
	// path; every other media type is persisted at layers/{sanitize(mediaType)}.
	Layers []string `json:"layers,omitempty"`
}

// NewArtifactCache creates a new ArtifactCache rooted at dir with the given TTL.
// A zero TTL means entries never expire.
func NewArtifactCache(dir string, ttl time.Duration) *ArtifactCache {
	return &ArtifactCache{dir: dir, ttl: ttl}
}

// Dir returns the root directory of the cache.
func (c *ArtifactCache) Dir() string {
	return c.dir
}

// TTL returns the configured TTL for cache entries.
func (c *ArtifactCache) TTL() time.Duration {
	return c.ttl
}

// Get retrieves cached content and any stored auxiliary layers for the given
// artifact. The returned layers map is keyed by the media type supplied to Put
// (e.g. a bundle or lock media type) and is nil when the entry has no auxiliary
// layers. Returns (nil, nil, false, nil) on cache miss or expiry, and
// (nil, nil, false, err) on read errors.
//
// If the entry's meta lists a layer whose file is missing on disk, the entry is
// treated as corrupt: it is removed and reported as a miss rather than served
// with an incomplete layer set.
func (c *ArtifactCache) Get(kind, name, version string) (content []byte, layers map[string][]byte, ok bool, err error) {
	dir := c.entryDir(kind, name, version)

	// Read and validate meta
	meta, found, err := c.readMeta(dir)
	if err != nil {
		return nil, nil, false, err
	}
	if !found {
		return nil, nil, false, nil
	}

	// Check TTL
	if c.ttl > 0 && time.Since(meta.CreatedAt) > c.ttl {
		_ = os.RemoveAll(dir) // remove stale entry
		return nil, nil, false, nil
	}

	// Read content
	contentBytes, err := os.ReadFile(filepath.Join(dir, "content"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, false, nil
		}
		return nil, nil, false, fmt.Errorf("artifact cache: reading content: %w", err)
	}

	// Read auxiliary layers listed in meta. The bundle media type lives at the
	// top-level bundle.tar.gz path; all others live under layers/.
	var layerMap map[string][]byte
	for _, mt := range meta.Layers {
		layerBytes, rerr := os.ReadFile(c.layerPath(dir, mt))
		if rerr != nil {
			if os.IsNotExist(rerr) {
				// A listed layer is missing — the entry is inconsistent. Drop it
				// and report a miss so the caller re-fetches a complete artifact.
				_ = os.RemoveAll(dir)
				return nil, nil, false, nil
			}
			return nil, nil, false, fmt.Errorf("artifact cache: reading layer %s: %w", mt, rerr)
		}
		if layerMap == nil {
			layerMap = make(map[string][]byte, len(meta.Layers))
		}
		layerMap[mt] = layerBytes
	}

	return contentBytes, layerMap, true, nil
}

// Put stores artifact content and optional auxiliary layers in the cache.
// digest is the content digest returned by the catalog (e.g., "sha256:abc123...").
// layers maps a media type to its bytes (e.g. a bundle tar and/or a lock layer);
// nil or empty-byte entries are skipped. Any auxiliary layers from a previous
// Put of the same entry are cleared, so the on-disk layer set always matches the
// supplied map.
func (c *ArtifactCache) Put(kind, name, version, digest string, content []byte, layers map[string][]byte) error {
	dir := c.entryDir(kind, name, version)

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("artifact cache: creating entry directory: %w", err)
	}

	// Write content
	if err := os.WriteFile(filepath.Join(dir, "content"), content, 0o600); err != nil {
		return fmt.Errorf("artifact cache: writing content: %w", err)
	}

	// Reset stale layers from a previous put: the bundle at its legacy top-level
	// path and any other layers under layers/. Whatever the current put supplies
	// is rewritten below, so the on-disk layer set always matches the map.
	if err := os.Remove(filepath.Join(dir, bundleFileName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("artifact cache: clearing bundle: %w", err)
	}
	layersDir := filepath.Join(dir, "layers")
	if err := os.RemoveAll(layersDir); err != nil {
		return fmt.Errorf("artifact cache: clearing layers: %w", err)
	}

	// Write each non-empty layer. Iterate in sorted media-type order for
	// deterministic output and stable collision detection.
	var stored []string
	mediaTypes := make([]string, 0, len(layers))
	for mt := range layers {
		mediaTypes = append(mediaTypes, mt)
	}
	sort.Strings(mediaTypes)

	seen := make(map[string]string, len(mediaTypes))
	layersCreated := false
	for _, mt := range mediaTypes {
		data := layers[mt]
		if len(data) == 0 {
			continue
		}

		// The bundle goes to its dedicated top-level path; everything else goes
		// under layers/{sanitize(mediaType)}, guarding against filename
		// collisions between distinct media types.
		if mt != MediaTypeSolutionBundle {
			fileName := sanitizeArtifactCacheKey(mt)
			if prev, dup := seen[fileName]; dup {
				return fmt.Errorf("artifact cache: layer media types %q and %q collide on cache filename %q", prev, mt, fileName)
			}
			seen[fileName] = mt
			if !layersCreated {
				if err := os.MkdirAll(layersDir, 0o750); err != nil {
					return fmt.Errorf("artifact cache: creating layers directory: %w", err)
				}
				layersCreated = true
			}
		}

		if err := os.WriteFile(c.layerPath(dir, mt), data, 0o600); err != nil {
			return fmt.Errorf("artifact cache: writing layer %s: %w", mt, err)
		}
		stored = append(stored, mt)
	}

	// Write meta
	meta := artifactCacheMeta{
		CreatedAt: time.Now(),
		Digest:    digest,
		Layers:    stored,
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("artifact cache: marshaling meta: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), metaBytes, 0o600); err != nil {
		return fmt.Errorf("artifact cache: writing meta: %w", err)
	}

	return nil
}

// Invalidate removes a cached entry for the given artifact.
// Returns nil if the entry does not exist.
func (c *ArtifactCache) Invalidate(kind, name, version string) error {
	dir := c.entryDir(kind, name, version)
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("artifact cache: invalidating entry: %w", err)
	}
	return nil
}

// InvalidateArtifact is a convenience function that creates a default
// ArtifactCache and invalidates the entry for the given kind, name, and
// optional version. The cacheDir and ttl are forwarded to NewArtifactCache.
//
// This encapsulates the common pattern:
//
//	cache := NewArtifactCache(dir, ttl)
//	cache.Invalidate(kind, name, version)
func InvalidateArtifact(cacheDir string, ttl time.Duration, kind, name, version string) error {
	c := NewArtifactCache(cacheDir, ttl)
	return c.Invalidate(kind, name, version)
}

// entryDir returns the directory path for a given cache entry.
// Path: {c.dir}/{safeKind}/{safeNameVersion}
func (c *ArtifactCache) entryDir(kind, name, version string) string {
	safeKind := sanitizeArtifactCacheKey(kind)
	nameVersion := name
	if version != "" {
		nameVersion = name + "@" + version
	}
	safeNameVersion := sanitizeArtifactCacheKey(nameVersion)
	return filepath.Join(c.dir, safeKind, safeNameVersion)
}

// layerPath returns the on-disk path for a layer of the given media type within
// a cache entry directory. The bundle media type maps to the legacy top-level
// bundle.tar.gz file; every other media type maps to layers/{sanitize(mt)}.
func (c *ArtifactCache) layerPath(dir, mediaType string) string {
	if mediaType == MediaTypeSolutionBundle {
		return filepath.Join(dir, bundleFileName)
	}
	return filepath.Join(dir, "layers", sanitizeArtifactCacheKey(mediaType))
}

// readMeta reads and parses meta.json from a cache entry directory.
// Returns (meta, true, nil) on success, (zero, false, nil) if not found,
// (zero, false, err) on other errors.
func (c *ArtifactCache) readMeta(dir string) (artifactCacheMeta, bool, error) {
	metaPath := filepath.Join(dir, "meta.json")
	data, err := os.ReadFile(metaPath)
	if os.IsNotExist(err) {
		return artifactCacheMeta{}, false, nil
	}
	if err != nil {
		return artifactCacheMeta{}, false, fmt.Errorf("artifact cache: reading meta: %w", err)
	}

	var meta artifactCacheMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		// Corrupt meta — treat as cache miss and clean up
		_ = os.RemoveAll(dir)
		return artifactCacheMeta{}, false, nil //nolint:nilerr // Intentionally ignoring unmarshal error for corrupt meta
	}

	return meta, true, nil
}

// sanitizeArtifactCacheKey replaces characters unsafe for directory names.
// Preserves letters, digits, '-', '.', '_', and '@'.
func sanitizeArtifactCacheKey(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '.' || r == '_' || r == '@' {
			return r
		}
		return '_'
	}, s)
}
