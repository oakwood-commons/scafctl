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
	"time"
)

const (
	// BundleManifestPath is the path within the tar archive for the bundle manifest.
	BundleManifestPath = ".scafctl/bundle-manifest.json"

	// DefaultMaxBundleSize is the default maximum total size of bundled files (50 MB).
	DefaultMaxBundleSize int64 = 50 * 1024 * 1024
)

// BundleManifest describes the contents of a bundle tar archive.
type BundleManifest struct {
	// Version is the manifest format version.
	Version int `json:"version"`
	// Root is the bundle root directory (always ".").
	Root string `json:"root"`
	// Files lists all bundled files with their paths, sizes, and content digests.
	Files []BundleFileEntry `json:"files"`
	// Plugins lists plugin dependencies (informational, recorded from bundle.plugins).
	Plugins []BundlePluginEntry `json:"plugins,omitempty"`
}

// BundleFileEntry describes a single file in the bundle.
type BundleFileEntry struct {
	// Path is the file's path relative to the bundle root.
	Path string `json:"path"`
	// Size is the file size in bytes.
	Size int64 `json:"size"`
	// Digest is the SHA-256 content digest.
	Digest string `json:"digest"`
	// Layer is the OCI layer index for this file (version 2 only).
	// In version 1 manifests, this is omitted (all files are in the tar layer).
	Layer int `json:"layer,omitempty"`
}

// BundlePluginEntry describes a plugin dependency in the bundle manifest.
type BundlePluginEntry struct {
	// Name is the plugin's catalog reference.
	Name string `json:"name"`
	// Kind is the plugin type (provider, auth-handler).
	Kind string `json:"kind"`
	// Version is the semver constraint.
	Version string `json:"version"`
}

// TarOption configures tar creation behavior.
type TarOption func(*tarConfig)

type tarConfig struct {
	maxSize  int64
	readFile func(string) ([]byte, error)
}

// WithMaxBundleSize sets the maximum total size for the bundle.
func WithMaxBundleSize(size int64) TarOption {
	return func(c *tarConfig) {
		c.maxSize = size
	}
}

// WithTarReadFileFunc overrides the file reading function for testing.
func WithTarReadFileFunc(fn func(string) ([]byte, error)) TarOption {
	return func(c *tarConfig) {
		c.readFile = fn
	}
}

// CreateBundleTar creates a tar archive containing the discovered files
// and a bundle manifest. Returns the tar bytes and the manifest.
func CreateBundleTar(bundleRoot string, files []FileEntry, plugins []BundlePluginEntry, opts ...TarOption) ([]byte, *BundleManifest, error) {
	cfg := &tarConfig{
		maxSize:  DefaultMaxBundleSize,
		readFile: os.ReadFile,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	manifest := &BundleManifest{
		Version: 1,
		Root:    ".",
		Files:   make([]BundleFileEntry, 0, len(files)),
		Plugins: plugins,
	}

	// First pass: read all files, compute digests, check total size
	type fileData struct {
		relPath string
		content []byte
		digest  string
	}

	var totalSize int64
	fileContents := make([]fileData, 0, len(files))

	for _, f := range files {
		absPath := filepath.Join(bundleRoot, f.RelPath)
		content, err := cfg.readFile(absPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read %s: %w", f.RelPath, err)
		}

		totalSize += int64(len(content))
		if totalSize > cfg.maxSize {
			return nil, nil, fmt.Errorf("bundle exceeds maximum size limit (%d bytes, limit %d bytes)", totalSize, cfg.maxSize)
		}

		digest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))

		manifest.Files = append(manifest.Files, BundleFileEntry{
			Path:   filepath.ToSlash(f.RelPath),
			Size:   int64(len(content)),
			Digest: digest,
		})

		fileContents = append(fileContents, fileData{
			relPath: f.RelPath,
			content: content,
			digest:  digest,
		})
	}

	// Create tar archive
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Write the bundle manifest first
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal bundle manifest: %w", err)
	}

	if err := writeToTar(tw, BundleManifestPath, manifestJSON); err != nil {
		return nil, nil, fmt.Errorf("failed to write bundle manifest to tar: %w", err)
	}

	// Write all files
	for _, fd := range fileContents {
		entryPath := filepath.ToSlash(fd.relPath)
		if err := writeToTar(tw, entryPath, fd.content); err != nil {
			return nil, nil, fmt.Errorf("failed to write %s to tar: %w", fd.relPath, err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, nil, fmt.Errorf("failed to close tar writer: %w", err)
	}

	return buf.Bytes(), manifest, nil
}

// writeToTar writes a single file entry to a tar writer.
func writeToTar(tw *tar.Writer, name string, content []byte) error {
	header := &tar.Header{
		Name:    name,
		Size:    int64(len(content)),
		Mode:    0o644,
		ModTime: time.Now().UTC(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	if _, err := io.Copy(tw, bytes.NewReader(content)); err != nil {
		return err
	}
	return nil
}

// newTarWriter creates a new tar.Writer wrapping the given writer.
func newTarWriter(w io.Writer) *tar.Writer {
	return tar.NewWriter(w)
}

// writeToTarWriter is an alias for writeToTar, exported for use by dedupe.
func writeToTarWriter(tw *tar.Writer, name string, content []byte) error {
	return writeToTar(tw, name, content)
}

// ExtractBundleTar extracts a bundle tar archive to a destination directory.
// Returns the bundle manifest.
func ExtractBundleTar(tarData []byte, destDir string) (*BundleManifest, error) {
	return extractBundleTarRecursive(tarData, destDir, nil)
}

// extractBundleTarRecursive extracts a bundle tar archive, detecting and recursively
// extracting nested tar archives from sub-solution bundles.
// visitedDigests tracks tar content digests to detect circular nested bundles.
func extractBundleTarRecursive(tarData []byte, destDir string, visitedDigests map[string]bool) (*BundleManifest, error) {
	if visitedDigests == nil {
		visitedDigests = make(map[string]bool)
	}

	// Detect circular nested tar references via content digest
	tarDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(tarData))
	if visitedDigests[tarDigest] {
		return nil, fmt.Errorf("circular nested bundle detected (digest %s)", tarDigest)
	}
	visitedDigests[tarDigest] = true

	tr := tar.NewReader(bytes.NewReader(tarData))

	var manifest *BundleManifest
	// Collect nested tar entries for deferred extraction after the main pass.
	type nestedTar struct {
		destDir string
		content []byte
	}
	var nestedTars []nestedTar

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar entry: %w", err)
		}

		// Security: validate path
		cleanName := filepath.Clean(header.Name)
		if filepath.IsAbs(cleanName) || strings.HasPrefix(cleanName, "..") {
			return nil, fmt.Errorf("tar contains path traversal: %s", header.Name)
		}

		destPath := filepath.Join(destDir, cleanName)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", cleanName, err)
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return nil, fmt.Errorf("failed to create directory for %s: %w", cleanName, err)
			}

			content, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("failed to read %s from tar: %w", cleanName, err)
			}

			if err := os.WriteFile(destPath, content, os.FileMode(uint32(header.Mode)&0o777)); err != nil { //nolint:gosec // mode is masked to safe permission bits
				return nil, fmt.Errorf("failed to write %s: %w", cleanName, err)
			}

			// Parse manifest if this is the manifest file
			if filepath.ToSlash(cleanName) == BundleManifestPath {
				manifest = &BundleManifest{}
				if err := json.Unmarshal(content, manifest); err != nil {
					return nil, fmt.Errorf("failed to parse bundle manifest: %w", err)
				}
			}

			// Detect nested tar archives (sub-solution bundles)
			if isNestedBundleTar(cleanName, content) {
				nestedTars = append(nestedTars, nestedTar{
					destDir: filepath.Dir(destPath),
					content: content,
				})
			}
		}
	}

	if manifest == nil {
		return nil, fmt.Errorf("bundle tar does not contain a manifest at %s", BundleManifestPath)
	}

	// Extract nested tar archives
	for _, nt := range nestedTars {
		nestedManifest, err := extractBundleTarRecursive(nt.content, nt.destDir, visitedDigests)
		if err != nil {
			// Log but don't fail — nested bundles are best-effort
			continue
		}
		// Merge nested manifest files into the parent manifest for visibility
		if nestedManifest != nil {
			relDir, relErr := filepath.Rel(destDir, nt.destDir)
			if relErr != nil {
				relDir = nt.destDir
			}
			for _, f := range nestedManifest.Files {
				nestedPath := filepath.ToSlash(filepath.Join(relDir, f.Path))
				manifest.Files = append(manifest.Files, BundleFileEntry{
					Path:   nestedPath,
					Size:   f.Size,
					Digest: f.Digest,
				})
			}
		}
	}

	return manifest, nil
}

// isNestedBundleTar checks whether a tar entry contains a nested bundle tar archive.
// A nested bundle tar is detected by checking if the file name ends with .bundle.tar
// or if the content starts with a valid tar header containing a bundle manifest path.
func isNestedBundleTar(name string, content []byte) bool {
	// Check by file extension convention
	slashName := filepath.ToSlash(name)
	if strings.HasSuffix(slashName, ".bundle.tar") {
		return true
	}

	// Check if the content is a tar archive containing a bundle manifest
	if len(content) < 512 { // minimum tar header size
		return false
	}

	tr := tar.NewReader(bytes.NewReader(content))
	for i := 0; i < 5; i++ { // only check first few entries
		header, err := tr.Next()
		if err != nil {
			return false
		}
		if filepath.ToSlash(filepath.Clean(header.Name)) == BundleManifestPath {
			return true
		}
	}
	return false
}
