// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fs

import (
	"os"
	"path/filepath"
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
