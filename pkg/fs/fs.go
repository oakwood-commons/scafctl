// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fs

import (
	"os"
	"path/filepath"
	"runtime"
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
//
// On Windows, renaming over an existing destination is not permitted, so a
// failed rename falls back to removing the destination and retrying.
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
	// Flush the file's contents to stable storage before the rename so a crash
	// or power loss right after WriteFileAtomic returns cannot leave the renamed
	// destination pointing at unwritten data (matches the atomic-write pattern in
	// pkg/cache/diskcache and pkg/catalog).
	if err := tmp.Sync(); err != nil {
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
		// On POSIX, rename-over-existing is atomic and expected to succeed, so a
		// failure here is a genuine error (permissions, cross-device link, ...).
		// Removing the destination and retrying would destroy the existing file for
		// no benefit, so only fall back on Windows, where rename-over-existing is
		// not permitted.
		if runtime.GOOS != "windows" {
			return err
		}
		// Windows does not allow rename-over-existing. Remove the destination and
		// retry so the write is still atomic-enough on that platform. Only do this
		// for a regular-file destination: if path is a directory, os.Remove would
		// delete it and drop a file in its place -- surprising data loss -- so
		// surface the original rename error instead. Return the final failure
		// (remove/retry error) rather than the original rename error, so a genuine
		// remove/retry problem is not masked by the expected rename-over-existing
		// failure.
		if fi, statErr := os.Stat(path); statErr == nil && !fi.IsDir() {
			if rmErr := os.Remove(path); rmErr != nil {
				return rmErr
			}
			if retryErr := os.Rename(tmpName, path); retryErr != nil { //nolint:gosec // path resolved from user-provided reference
				return retryErr
			}
			return nil
		}
		return err
	}
	return nil
}
