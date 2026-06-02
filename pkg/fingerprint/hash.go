// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// HashFiles computes a deterministic SHA-256 hash over the contents of all
// files matching the given glob patterns, resolved relative to baseDir.
// Files are sorted lexicographically before hashing for determinism.
// The hash includes both file paths and contents so that renames are detected.
// Returns empty string and nil error if patterns is empty.
func HashFiles(baseDir string, patterns []string) (string, error) {
	if len(patterns) == 0 {
		return "", nil
	}

	files, err := ExpandGlobs(baseDir, patterns)
	if err != nil {
		return "", err
	}

	return hashFileList(baseDir, files)
}

// hashFileList computes a SHA-256 hash over the given sorted file list.
func hashFileList(baseDir string, files []string) (string, error) {
	h := sha256.New()

	for _, relPath := range files {
		absPath := filepath.Join(baseDir, relPath)

		info, err := os.Stat(absPath)
		if err != nil {
			return "", fmt.Errorf("stat %q: %w", relPath, err)
		}

		// Include file path and content length in hash so the stream is
		// unambiguous -- without length, file additions/removals or content
		// that mimics a "file:" marker could produce hash collisions.
		fmt.Fprintf(h, "file:%s\nlen:%d\n", relPath, info.Size())

		f, err := os.Open(absPath)
		if err != nil {
			return "", fmt.Errorf("reading %q: %w", relPath, err)
		}

		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return "", fmt.Errorf("hashing %q: %w", relPath, err)
		}
		f.Close()
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashInputs computes a deterministic SHA-256 hash over the resolved action
// inputs map. Uses json.Marshal which sorts map keys lexicographically.
// Returns empty string and nil error if inputs is nil or empty.
func HashInputs(inputs map[string]any) (string, error) {
	if len(inputs) == 0 {
		return "", nil
	}

	data, err := json.Marshal(inputs)
	if err != nil {
		return "", fmt.Errorf("serializing inputs: %w", err)
	}

	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}
