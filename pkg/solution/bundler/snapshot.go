// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// SnapshotFile is one file to bundle, read once.
type SnapshotFile struct {
	// RelPath is the slash-separated path relative to the bundle root.
	RelPath string
	// Content is the file's bytes.
	Content []byte
	// Digest is the SHA-256 content digest ("sha256:<hex>").
	Digest string
}

// FileSnapshot holds the content of the files to bundle. Each file is read
// once, so the build fingerprint and the archive are computed from the same
// bytes.
type FileSnapshot struct {
	// Files are in the order given to ReadFileSnapshot, one per path.
	Files []SnapshotFile
	// TotalSize is the sum of the file sizes, in bytes.
	TotalSize int64
}

// entries returns the snapshot's files; nil for a nil snapshot.
func (s *FileSnapshot) entries() []SnapshotFile {
	if s == nil {
		return nil
	}
	return s.Files
}

// SnapshotOption configures ReadFileSnapshot.
type SnapshotOption func(*snapshotConfig)

type snapshotConfig struct {
	maxSize  int64
	contents map[string][]byte
	readFile func(string) ([]byte, error)
}

// WithSnapshotMaxSize sets the maximum total size of the files.
func WithSnapshotMaxSize(size int64) SnapshotOption {
	return func(c *snapshotConfig) {
		c.maxSize = size
	}
}

// WithSnapshotContents supplies content already in memory, keyed by
// slash-separated path relative to the bundle root. These files are not read
// from disk.
func WithSnapshotContents(contents map[string][]byte) SnapshotOption {
	return func(c *snapshotConfig) {
		c.contents = contents
	}
}

// WithSnapshotReadFileFunc overrides the file reading function for testing.
func WithSnapshotReadFileFunc(fn func(string) ([]byte, error)) SnapshotOption {
	return func(c *snapshotConfig) {
		c.readFile = fn
	}
}

// ReadFileSnapshot reads each file under bundleRoot once and records its
// digest. Duplicate paths are read once. It fails when a file cannot be read
// or the total size exceeds the limit (DefaultMaxBundleSize by default).
func ReadFileSnapshot(bundleRoot string, files []FileEntry, opts ...SnapshotOption) (*FileSnapshot, error) {
	cfg := &snapshotConfig{
		maxSize:  DefaultMaxBundleSize,
		readFile: os.ReadFile,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	snap := &FileSnapshot{Files: make([]SnapshotFile, 0, len(files))}
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		relPath := filepath.ToSlash(f.RelPath)
		if seen[relPath] {
			continue
		}
		seen[relPath] = true

		content, ok := cfg.contents[relPath]
		if !ok {
			var err error
			content, err = cfg.readFile(filepath.Join(bundleRoot, filepath.FromSlash(relPath)))
			if err != nil {
				return nil, fmt.Errorf("failed to read %s: %w", f.RelPath, err)
			}
		}

		snap.TotalSize += int64(len(content))
		if snap.TotalSize > cfg.maxSize {
			return nil, fmt.Errorf("bundle exceeds maximum size limit (%d bytes, limit %d bytes)", snap.TotalSize, cfg.maxSize)
		}

		snap.Files = append(snap.Files, SnapshotFile{
			RelPath: relPath,
			Content: content,
			Digest:  fmt.Sprintf("sha256:%x", sha256.Sum256(content)),
		})
	}
	return snap, nil
}
