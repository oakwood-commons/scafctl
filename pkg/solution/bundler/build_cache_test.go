// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeBuildFingerprint_Deterministic(t *testing.T) {
	dir := t.TempDir()

	// Create a test file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0o600))

	content := []byte("solution content")
	files := []FileEntry{{RelPath: "file1.txt", Source: StaticAnalysis}}
	plugins := []BundlePluginEntry{{Name: "my-plugin", Kind: "provider", Version: "1.0.0"}}

	fp1, err := ComputeBuildFingerprint(content, dir, files, plugins, "sha256:lockdigest")
	require.NoError(t, err)

	fp2, err := ComputeBuildFingerprint(content, dir, files, plugins, "sha256:lockdigest")
	require.NoError(t, err)

	assert.Equal(t, fp1, fp2, "fingerprint should be deterministic")
	assert.True(t, len(fp1) > 10, "fingerprint should be non-trivial")
}

func TestComputeBuildFingerprint_ChangesOnFileModification(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0o600))

	content := []byte("solution content")
	files := []FileEntry{{RelPath: "file1.txt", Source: StaticAnalysis}}

	fp1, err := ComputeBuildFingerprint(content, dir, files, nil, "")
	require.NoError(t, err)

	// Modify the file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("modified"), 0o600))

	fp2, err := ComputeBuildFingerprint(content, dir, files, nil, "")
	require.NoError(t, err)

	assert.NotEqual(t, fp1, fp2, "fingerprint should change when file content changes")
}

func TestComputeBuildFingerprint_ChangesOnSolutionModification(t *testing.T) {
	dir := t.TempDir()

	files := []FileEntry{}

	fp1, err := ComputeBuildFingerprint([]byte("version 1"), dir, files, nil, "")
	require.NoError(t, err)

	fp2, err := ComputeBuildFingerprint([]byte("version 2"), dir, files, nil, "")
	require.NoError(t, err)

	assert.NotEqual(t, fp1, fp2, "fingerprint should change when solution content changes")
}

func TestComputeBuildFingerprint_ChangesOnPluginChange(t *testing.T) {
	dir := t.TempDir()

	content := []byte("solution")
	plugins1 := []BundlePluginEntry{{Name: "my-plugin", Kind: "provider", Version: "1.0.0"}}
	plugins2 := []BundlePluginEntry{{Name: "my-plugin", Kind: "provider", Version: "2.0.0"}}

	fp1, err := ComputeBuildFingerprint(content, dir, nil, plugins1, "")
	require.NoError(t, err)

	fp2, err := ComputeBuildFingerprint(content, dir, nil, plugins2, "")
	require.NoError(t, err)

	assert.NotEqual(t, fp1, fp2, "fingerprint should change when plugin version changes")
}

func TestComputeBuildFingerprint_ChangesOnLockChange(t *testing.T) {
	dir := t.TempDir()

	content := []byte("solution")

	fp1, err := ComputeBuildFingerprint(content, dir, nil, nil, "sha256:aaa")
	require.NoError(t, err)

	fp2, err := ComputeBuildFingerprint(content, dir, nil, nil, "sha256:bbb")
	require.NoError(t, err)

	assert.NotEqual(t, fp1, fp2, "fingerprint should change when lock digest changes")
}

func TestComputeBuildFingerprint_SortedFiles(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bbb"), 0o600))

	content := []byte("solution")

	// Different order, same files
	files1 := []FileEntry{
		{RelPath: "a.txt", Source: StaticAnalysis},
		{RelPath: "b.txt", Source: StaticAnalysis},
	}
	files2 := []FileEntry{
		{RelPath: "b.txt", Source: StaticAnalysis},
		{RelPath: "a.txt", Source: StaticAnalysis},
	}

	fp1, err := ComputeBuildFingerprint(content, dir, files1, nil, "")
	require.NoError(t, err)

	fp2, err := ComputeBuildFingerprint(content, dir, files2, nil, "")
	require.NoError(t, err)

	assert.Equal(t, fp1, fp2, "fingerprint should be order-independent for files")
}

func TestWriteAndCheckBuildCache(t *testing.T) {
	dir := t.TempDir()

	entry := &BuildCacheEntry{
		ArtifactName:    "my-solution",
		ArtifactVersion: "1.0.0",
		ArtifactDigest:  "sha256:digest123",
		CreatedAt:       time.Now(),
		InputFiles:      5,
	}

	fingerprint := "sha256:abcdef0123456789"

	// Write cache entry
	err := WriteBuildCache(dir, fingerprint, entry)
	require.NoError(t, err)

	// Check cache hit
	cached, hit := CheckBuildCache(dir, fingerprint)
	require.True(t, hit)
	assert.Equal(t, "my-solution", cached.ArtifactName)
	assert.Equal(t, "1.0.0", cached.ArtifactVersion)
	assert.Equal(t, "sha256:digest123", cached.ArtifactDigest)
	assert.Equal(t, fingerprint, cached.Fingerprint)
	assert.Equal(t, 5, cached.InputFiles)
}

func TestCheckBuildCache_Miss(t *testing.T) {
	dir := t.TempDir()

	_, hit := CheckBuildCache(dir, "sha256:nonexistent")
	assert.False(t, hit)
}

func TestCleanBuildCache(t *testing.T) {
	dir := t.TempDir()

	// Write some entries
	err := WriteBuildCache(dir, "sha256:aaa", &BuildCacheEntry{ArtifactName: "a", CreatedAt: time.Now()})
	require.NoError(t, err)
	err = WriteBuildCache(dir, "sha256:bbb", &BuildCacheEntry{ArtifactName: "b", CreatedAt: time.Now()})
	require.NoError(t, err)

	assert.Equal(t, 2, CountBuildCacheEntries(dir))

	// Clean
	err = CleanBuildCache(dir)
	require.NoError(t, err)
	assert.Equal(t, 0, CountBuildCacheEntries(dir))
}

func TestCleanBuildCache_NonexistentDir(t *testing.T) {
	err := CleanBuildCache(filepath.Join(t.TempDir(), "nonexistent"))
	assert.NoError(t, err)
}

func TestCountBuildCacheEntries(t *testing.T) {
	dir := t.TempDir()

	assert.Equal(t, 0, CountBuildCacheEntries(dir))

	err := WriteBuildCache(dir, "sha256:aaa", &BuildCacheEntry{ArtifactName: "a", CreatedAt: time.Now()})
	require.NoError(t, err)

	assert.Equal(t, 1, CountBuildCacheEntries(dir))
}
