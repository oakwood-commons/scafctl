// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests cover the fs helpers: the function type aliases (StatFunc,
// ReadFileFunc) compile and accept real functions, and WriteFileAtomic writes
// content atomically.

func TestStatFunc_AssignableFromOsStat(t *testing.T) {
	var fn StatFunc = os.Stat
	assert.NotNil(t, fn)
}

func TestReadFileFunc_AssignableFromOsReadFile(t *testing.T) {
	var fn ReadFileFunc = os.ReadFile
	assert.NotNil(t, fn)
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.yaml")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o640))

	require.NoError(t, WriteFileAtomic(path, []byte("replaced content"), 0o640))

	got, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Equal(t, "replaced content", string(got))

	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), fi.Mode().Perm(), "mode preserved")

	// No leftover temp files in the directory.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temp files left behind")
}

func TestWriteFileAtomic_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.yaml")

	require.NoError(t, WriteFileAtomic(path, []byte("brand new"), 0o644))

	got, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Equal(t, "brand new", string(got))
}

// TestWriteFileAtomic_RenameFailurePreservesDestination verifies that on
// non-Windows platforms a failed rename returns the error without deleting the
// existing destination. The remove+retry fallback is gated to Windows, so a
// POSIX rename failure must never destroy the good on-disk data.
//
// An empty directory is used as the destination: renaming a file over a
// directory fails on POSIX, so this exercises the rename-failure branch. Crucially,
// os.Remove would succeed on an empty directory, so the ungated fallback would
// delete it and retry (turning the directory into a file and returning success).
// The gate must instead return the error and leave the directory intact.
func TestWriteFileAtomic_RenameFailurePreservesDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fallback deletes the destination on Windows; this test asserts POSIX behavior")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "dest")
	require.NoError(t, os.Mkdir(path, 0o755))

	err := WriteFileAtomic(path, []byte("replaced content"), 0o640)
	require.Error(t, err, "rename over a directory should fail without falling back to remove+retry")

	// The destination must be untouched: still a directory, not deleted and
	// replaced by the temp file via a Windows-only fallback that must not run here.
	fi, statErr := os.Stat(path)
	require.NoError(t, statErr, "destination must still exist after a failed rename")
	assert.True(t, fi.IsDir(), "destination must remain a directory, not be replaced by the write")
}
