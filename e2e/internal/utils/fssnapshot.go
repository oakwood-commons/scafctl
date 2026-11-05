// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/onsi/gomega"
)

// SnapshotTree returns a map of slash-relative path to sha256 for every
// regular file under root. Directories are recorded with an empty hash and a
// trailing slash so an empty directory appearing is also a diff. Specs compare
// snapshots before and after a command to prove it had no filesystem side
// effects.
func SnapshotTree(root string) map[string]string {
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			snap[rel+"/"] = ""
			return nil
		}
		data, readErr := os.ReadFile(path) //nolint:gosec // test-controlled path under a temp dir.
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(data)
		snap[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return snap
}
