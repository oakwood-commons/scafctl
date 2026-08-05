// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fs

import (
	"os"
	"path/filepath"
)

// StatFunc defines a function type that takes a file path as input and returns
// the file's os.FileInfo and an error if the operation fails. It is typically
// used to abstract file stat operations for testing or customization.
type StatFunc func(path string) (os.FileInfo, error)

// ReadFileFunc defines a function type for reading the contents of a file.
// It takes a filename as input and returns the file's contents as a byte slice,
// along with an error if the operation fails.
type ReadFileFunc func(filename string) ([]byte, error)

// WriteFileAtomic writes data to path atomically by writing to a temporary file
// in the same directory and renaming it over path. os.Rename is atomic on the
// same filesystem, so a crash mid-write cannot leave the destination file
// truncated or partially written. mode sets the final file's permission bits.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*") //nolint:gosec // temp file created in the target's own directory
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we return before a successful rename; a no-op once
	// the temp file has been renamed away.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil { //nolint:gosec // path resolved from user-provided reference
		return err
	}
	return nil
}
