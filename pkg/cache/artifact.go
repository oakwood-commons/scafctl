// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// ArtifactCache is a disk-based TTL cache for catalog artifacts.
// It stores artifact content (and optional bundle data) in a structured
// directory layout: {dir}/{kind}/{safe(name@version)}/
//
// Each cache entry contains:
//   - content      - the primary artifact bytes (e.g., solution YAML)
//   - bundle.tar.gz - the bundle tar (if any)
//   - meta.json     - creation timestamp and content digest
type ArtifactCache struct {
	dir string
	ttl time.Duration
}

// artifactCacheMeta holds metadata written alongside cached artifact files.
type artifactCacheMeta struct {
	CreatedAt time.Time `json:"createdAt"`
	Digest    string    `json:"digest,omitempty"`
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

// Get retrieves cached content and bundle data for the given artifact.
// Returns (nil, nil, false, nil) on cache miss or expiry.
// Returns (nil, nil, false, err) on read errors.
func (c *ArtifactCache) Get(kind, name, version string) (content, bundleData []byte, ok bool, err error) {
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

	// Read bundle (optional — missing is not an error)
	bundleBytes, err := os.ReadFile(filepath.Join(dir, "bundle.tar.gz"))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, false, fmt.Errorf("artifact cache: reading bundle: %w", err)
	}

	return contentBytes, bundleBytes, true, nil
}

// Put stores artifact content and optional bundle data in the cache.
// digest is the content digest returned by the catalog (e.g., "sha256:abc123...").
// bundleData may be nil when the artifact has no bundle layer.
func (c *ArtifactCache) Put(kind, name, version, digest string, content, bundleData []byte) error {
	dir := c.entryDir(kind, name, version)

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("artifact cache: creating entry directory: %w", err)
	}

	// Write content
	if err := os.WriteFile(filepath.Join(dir, "content"), content, 0o600); err != nil {
		return fmt.Errorf("artifact cache: writing content: %w", err)
	}

	// Write bundle (skip if no bundle)
	bundlePath := filepath.Join(dir, "bundle.tar.gz")
	if len(bundleData) > 0 {
		if err := os.WriteFile(bundlePath, bundleData, 0o600); err != nil {
			return fmt.Errorf("artifact cache: writing bundle: %w", err)
		}
	} else {
		// Remove stale bundle from a previous put, if any
		_ = os.Remove(bundlePath)
	}

	// Write meta
	meta := artifactCacheMeta{
		CreatedAt: time.Now(),
		Digest:    digest,
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
