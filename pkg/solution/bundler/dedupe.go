// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// BundleManifestVersion1 is the original tar-based bundle format.
	BundleManifestVersion1 = 1

	// BundleManifestVersion2 is the content-addressable deduplicated format.
	BundleManifestVersion2 = 2

	// DefaultDedupeThreshold is the minimum file size for individual layer extraction.
	// Files smaller than this are grouped into a single tar layer.
	DefaultDedupeThreshold int64 = 4 * 1024 // 4 KB
)

// DedupeOption configures deduplication behavior.
type DedupeOption func(*dedupeConfig)

type dedupeConfig struct {
	threshold int64
	maxSize   int64
	readFile  func(string) ([]byte, error)
}

// WithDedupeThreshold sets the minimum file size for individual blob layers.
// Files smaller than this threshold are grouped into a combined tar layer.
func WithDedupeThreshold(size int64) DedupeOption {
	return func(c *dedupeConfig) {
		c.threshold = size
	}
}

// WithDedupeMaxSize sets the maximum total size of all bundled files.
func WithDedupeMaxSize(size int64) DedupeOption {
	return func(c *dedupeConfig) {
		c.maxSize = size
	}
}

// WithDedupeReadFileFunc overrides the file reading function for testing.
func WithDedupeReadFileFunc(fn func(string) ([]byte, error)) DedupeOption {
	return func(c *dedupeConfig) {
		c.readFile = fn
	}
}

// DedupeFileBlob represents a single file prepared for content-addressable storage.
type DedupeFileBlob struct {
	// RelPath is the file path relative to the bundle root.
	RelPath string
	// Content is the file's raw bytes.
	Content []byte
	// Digest is the SHA-256 content digest.
	Digest string
	// Size is the file size in bytes.
	Size int64
	// Layer is the 0-based layer index in the OCI manifest (set after partitioning).
	Layer int
}

// DedupeResult contains the output of content-addressable deduplication.
type DedupeResult struct {
	// Manifest is the bundle manifest (version 2).
	Manifest *BundleManifest
	// ManifestJSON is the serialized manifest.
	ManifestJSON []byte
	// LargeBlobs are individual file blobs stored as separate OCI layers.
	LargeBlobs []DedupeFileBlob
	// SmallBlobsTar is a tar archive of files below the dedup threshold.
	// May be nil if there are no small files.
	SmallBlobsTar []byte
	// TotalSize is the sum of all file sizes.
	TotalSize int64
}

// CreateDeduplicatedBundle prepares files for content-addressable OCI storage.
// Large files (>= threshold) become individual layers; small files are tarred together.
// Returns a DedupeResult containing the manifest and all blobs to push.
func CreateDeduplicatedBundle(bundleRoot string, files []FileEntry, plugins []BundlePluginEntry, opts ...DedupeOption) (*DedupeResult, error) {
	cfg := &dedupeConfig{
		threshold: DefaultDedupeThreshold,
		maxSize:   DefaultMaxBundleSize,
		readFile:  os.ReadFile,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Read all files and compute digests
	var totalSize int64
	blobs := make([]DedupeFileBlob, 0, len(files))

	for _, f := range files {
		absPath := filepath.Join(bundleRoot, f.RelPath)
		content, err := cfg.readFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", f.RelPath, err)
		}

		totalSize += int64(len(content))
		if totalSize > cfg.maxSize {
			return nil, fmt.Errorf("bundle exceeds maximum size limit (%d bytes, limit %d bytes)", totalSize, cfg.maxSize)
		}

		digest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
		blobs = append(blobs, DedupeFileBlob{
			RelPath: filepath.ToSlash(f.RelPath),
			Content: content,
			Digest:  digest,
			Size:    int64(len(content)),
		})
	}

	// Partition into large (individual layers) and small (tarred together)
	var largeBlobs []DedupeFileBlob
	var smallBlobs []DedupeFileBlob

	for i := range blobs {
		if blobs[i].Size >= cfg.threshold {
			largeBlobs = append(largeBlobs, blobs[i])
		} else {
			smallBlobs = append(smallBlobs, blobs[i])
		}
	}

	// Deduplicate large blobs by digest — if multiple files have the same content,
	// store only one blob but record all paths in the manifest.
	type dedupeGroup struct {
		blob  DedupeFileBlob
		paths []string
	}
	digestGroups := make(map[string]*dedupeGroup)
	var uniqueLargeBlobs []DedupeFileBlob

	for _, b := range largeBlobs {
		if group, exists := digestGroups[b.Digest]; exists {
			group.paths = append(group.paths, b.RelPath)
		} else {
			digestGroups[b.Digest] = &dedupeGroup{
				blob:  b,
				paths: []string{b.RelPath},
			}
			uniqueLargeBlobs = append(uniqueLargeBlobs, b)
		}
	}

	// Assign layer indices:
	// Layer 0: solution YAML (handled by caller)
	// Layer 1: bundle manifest JSON
	// Layer 2: small files tar (if any)
	// Layer 3+: individual large file blobs
	nextLayer := 2
	smallTarLayer := -1

	if len(smallBlobs) > 0 {
		smallTarLayer = nextLayer
		nextLayer++
	}

	// Map digest -> layer index for large blobs
	digestToLayer := make(map[string]int)
	for i := range uniqueLargeBlobs {
		uniqueLargeBlobs[i].Layer = nextLayer
		digestToLayer[uniqueLargeBlobs[i].Digest] = nextLayer
		nextLayer++
	}

	// Build manifest entries
	manifest := &BundleManifest{
		Version: BundleManifestVersion2,
		Root:    ".",
		Files:   make([]BundleFileEntry, 0, len(blobs)),
		Plugins: plugins,
	}

	// Add small file entries (all share the small-tar layer)
	for _, b := range smallBlobs {
		manifest.Files = append(manifest.Files, BundleFileEntry{
			Path:   b.RelPath,
			Size:   b.Size,
			Digest: b.Digest,
			Layer:  smallTarLayer,
		})
	}

	// Add large file entries. Files with duplicate digests point to the same layer.
	for _, b := range largeBlobs {
		layer := digestToLayer[b.Digest]
		manifest.Files = append(manifest.Files, BundleFileEntry{
			Path:   b.RelPath,
			Size:   b.Size,
			Digest: b.Digest,
			Layer:  layer,
		})
	}

	// Serialize manifest
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bundle manifest: %w", err)
	}

	// Create small files tar
	var smallTarData []byte
	if len(smallBlobs) > 0 {
		smallTarData, err = createSmallFilesTar(smallBlobs)
		if err != nil {
			return nil, fmt.Errorf("failed to create small files tar: %w", err)
		}
	}

	return &DedupeResult{
		Manifest:      manifest,
		ManifestJSON:  manifestJSON,
		LargeBlobs:    uniqueLargeBlobs,
		SmallBlobsTar: smallTarData,
		TotalSize:     totalSize,
	}, nil
}

// createSmallFilesTar creates a tar archive containing all small files.
func createSmallFilesTar(blobs []DedupeFileBlob) ([]byte, error) {
	var buf bytes.Buffer
	tw := newTarWriter(&buf)

	for _, b := range blobs {
		if err := writeToTarWriter(tw, b.RelPath, b.Content); err != nil {
			return nil, fmt.Errorf("failed to write %s to small files tar: %w", b.RelPath, err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close small files tar: %w", err)
	}

	return buf.Bytes(), nil
}

// ExtractDeduplicatedBundle extracts a version 2 bundle from individual OCI layers.
// layerFetcher returns the raw bytes of a layer by its 0-based index.
func ExtractDeduplicatedBundle(manifest *BundleManifest, destDir string, layerFetcher func(layer int) ([]byte, error)) error {
	if manifest == nil {
		return fmt.Errorf("manifest is nil")
	}
	if manifest.Version != BundleManifestVersion2 {
		return fmt.Errorf("expected manifest version %d, got %d", BundleManifestVersion2, manifest.Version)
	}

	// Group files by layer
	layerFiles := make(map[int][]BundleFileEntry)
	for _, f := range manifest.Files {
		layerFiles[f.Layer] = append(layerFiles[f.Layer], f)
	}

	// Extract each layer
	for layer, entries := range layerFiles {
		data, err := layerFetcher(layer)
		if err != nil {
			return fmt.Errorf("failed to fetch layer %d: %w", layer, err)
		}

		// Determine if this is a tar layer (small files) or a raw blob (large file)
		if isTarData(data) {
			// Extract plain tar — small files tar has no manifest inside
			if err := extractPlainTar(data, destDir); err != nil {
				return fmt.Errorf("failed to extract tar layer %d: %w", layer, err)
			}
		} else {
			// Raw blob — write to each path that references this layer+digest
			for _, entry := range entries {
				destPath := filepath.Join(destDir, filepath.FromSlash(entry.Path))
				if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
					return fmt.Errorf("failed to create directory for %s: %w", entry.Path, err)
				}
				if err := os.WriteFile(destPath, data, 0o600); err != nil {
					return fmt.Errorf("failed to write %s: %w", entry.Path, err)
				}
			}
		}
	}

	return nil
}

// isTarData performs a simple check to detect if data looks like a tar archive.
// Tar files start with a file name in the first 100 bytes, then have specific
// header bytes. We check for the "ustar" magic at offset 257.
func isTarData(data []byte) bool {
	if len(data) < 263 {
		return false
	}
	// ustar magic at offset 257
	magic := string(data[257:262])
	return magic == "ustar"
}

// ExtractBundleTarFromReader extracts a tar archive from a reader to a destination directory.
func ExtractBundleTarFromReader(r io.Reader, destDir string) (*BundleManifest, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read tar data: %w", err)
	}
	return ExtractBundleTar(data, destDir)
}

// extractPlainTar extracts a tar archive that contains only file entries (no manifest).
// Used for dedup v2 small-file tar layers.
func extractPlainTar(data []byte, destDir string) error {
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		cleanName := filepath.Clean(header.Name)
		if filepath.IsAbs(cleanName) || strings.HasPrefix(cleanName, "..") {
			return fmt.Errorf("tar contains path traversal: %s", header.Name)
		}

		destPath := filepath.Join(destDir, cleanName)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", cleanName, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return fmt.Errorf("failed to create directory for %s: %w", cleanName, err)
			}
			content, err := io.ReadAll(tr)
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", cleanName, err)
			}
			if err := os.WriteFile(destPath, content, 0o600); err != nil {
				return fmt.Errorf("failed to write %s: %w", cleanName, err)
			}
		}
	}
	return nil
}
