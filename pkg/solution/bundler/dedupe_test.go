// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateDeduplicatedBundle_BasicPartitioning(t *testing.T) {
	// Create small (<4KB) and large (>=4KB) files
	smallContent := []byte("small file content")
	largeContent := []byte(strings.Repeat("x", 5000))

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.txt"), smallContent, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "large.txt"), largeContent, 0o644))

	files := []FileEntry{
		{RelPath: "small.txt"},
		{RelPath: "large.txt"},
	}

	result, err := CreateDeduplicatedBundle(dir, files, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Manifest should be version 2
	assert.Equal(t, BundleManifestVersion2, result.Manifest.Version)
	assert.Equal(t, 2, len(result.Manifest.Files))
	assert.NotNil(t, result.SmallBlobsTar, "small files should produce a tar")
	assert.Equal(t, 1, len(result.LargeBlobs), "one large file = one large blob")

	// Total size should be sum of both files
	assert.Equal(t, int64(len(smallContent)+len(largeContent)), result.TotalSize)

	// Small file entry should have layer 2 (small tar layer)
	var smallEntry, largeEntry BundleFileEntry
	for _, f := range result.Manifest.Files {
		if f.Path == "small.txt" {
			smallEntry = f
		}
		if f.Path == "large.txt" {
			largeEntry = f
		}
	}
	assert.Equal(t, 2, smallEntry.Layer, "small files go to the small tar layer")
	assert.Equal(t, 3, largeEntry.Layer, "first large blob goes to layer 3")
}

func TestCreateDeduplicatedBundle_Deduplication(t *testing.T) {
	content := []byte(strings.Repeat("y", 5000)) // large, >= 4KB threshold
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), content, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), content, 0o644))

	files := []FileEntry{
		{RelPath: "a.txt"},
		{RelPath: "b.txt"},
	}

	result, err := CreateDeduplicatedBundle(dir, files, nil)
	require.NoError(t, err)

	// Should have only 1 unique large blob despite 2 files
	assert.Equal(t, 1, len(result.LargeBlobs), "duplicate content should be deduplicated")

	// Both manifest entries should point to the same layer
	assert.Equal(t, 2, len(result.Manifest.Files))
	assert.Equal(t, result.Manifest.Files[0].Digest, result.Manifest.Files[1].Digest)
	assert.Equal(t, result.Manifest.Files[0].Layer, result.Manifest.Files[1].Layer)
}

func TestCreateDeduplicatedBundle_AllSmallFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bbb"), 0o644))

	files := []FileEntry{
		{RelPath: "a.txt"},
		{RelPath: "b.txt"},
	}

	result, err := CreateDeduplicatedBundle(dir, files, nil)
	require.NoError(t, err)

	assert.Empty(t, result.LargeBlobs, "all files are small")
	assert.NotNil(t, result.SmallBlobsTar)
	assert.Equal(t, 2, len(result.Manifest.Files))

	// All entries should point to layer 2 (small tar layer)
	for _, f := range result.Manifest.Files {
		assert.Equal(t, 2, f.Layer)
	}
}

func TestCreateDeduplicatedBundle_AllLargeFiles(t *testing.T) {
	dir := t.TempDir()
	large1 := []byte(strings.Repeat("a", 5000))
	large2 := []byte(strings.Repeat("b", 5000))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.bin"), large1, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.bin"), large2, 0o644))

	files := []FileEntry{
		{RelPath: "a.bin"},
		{RelPath: "b.bin"},
	}

	result, err := CreateDeduplicatedBundle(dir, files, nil)
	require.NoError(t, err)

	assert.Nil(t, result.SmallBlobsTar, "no small files => no small tar")
	assert.Equal(t, 2, len(result.LargeBlobs))
	assert.Equal(t, 2, len(result.Manifest.Files))

	// Layers should be 2 and 3 (no small tar layer, so large starts at 2)
	layers := map[int]bool{}
	for _, f := range result.Manifest.Files {
		layers[f.Layer] = true
	}
	assert.True(t, layers[2])
	assert.True(t, layers[3])
}

func TestCreateDeduplicatedBundle_WithPlugins(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("data"), 0o644))

	plugins := []BundlePluginEntry{
		{Name: "myplugin", Kind: "exec", Version: "1.0.0"},
	}

	result, err := CreateDeduplicatedBundle(dir, []FileEntry{{RelPath: "f.txt"}}, plugins)
	require.NoError(t, err)

	assert.Equal(t, 1, len(result.Manifest.Plugins))
	assert.Equal(t, "myplugin", result.Manifest.Plugins[0].Name)
}

func TestCreateDeduplicatedBundle_ExceedsMaxSize(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.bin"), []byte(strings.Repeat("x", 100)), 0o644))

	_, err := CreateDeduplicatedBundle(dir, []FileEntry{{RelPath: "big.bin"}}, nil,
		WithDedupeMaxSize(50))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum size limit")
}

func TestCreateDeduplicatedBundle_CustomThreshold(t *testing.T) {
	dir := t.TempDir()
	content := []byte("small-ish data") // 14 bytes
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), content, 0o644))

	// Default threshold (4KB) → small file → no large blobs
	result1, err := CreateDeduplicatedBundle(dir, []FileEntry{{RelPath: "f.txt"}}, nil)
	require.NoError(t, err)
	assert.Empty(t, result1.LargeBlobs)

	// Threshold = 10 bytes → file is "large"
	result2, err := CreateDeduplicatedBundle(dir, []FileEntry{{RelPath: "f.txt"}}, nil,
		WithDedupeThreshold(10))
	require.NoError(t, err)
	assert.Equal(t, 1, len(result2.LargeBlobs))
}

func TestCreateDeduplicatedBundle_ManifestJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "data.txt"), []byte("hello"), 0o644))

	result, err := CreateDeduplicatedBundle(dir, []FileEntry{{RelPath: "data.txt"}}, nil)
	require.NoError(t, err)

	// ManifestJSON should be valid JSON and roundtrip
	var parsed BundleManifest
	require.NoError(t, json.Unmarshal(result.ManifestJSON, &parsed))
	assert.Equal(t, BundleManifestVersion2, parsed.Version)
	assert.Equal(t, 1, len(parsed.Files))
	assert.Equal(t, "data.txt", parsed.Files[0].Path)
}

func TestCreateDeduplicatedBundle_DigestConsistency(t *testing.T) {
	dir := t.TempDir()
	content := []byte("deterministic content")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), content, 0o644))

	expected := fmt.Sprintf("sha256:%x", sha256.Sum256(content))

	result, err := CreateDeduplicatedBundle(dir, []FileEntry{{RelPath: "f.txt"}}, nil)
	require.NoError(t, err)

	assert.Equal(t, expected, result.Manifest.Files[0].Digest)
}

func TestCreateDeduplicatedBundle_FileReadError(t *testing.T) {
	_, err := CreateDeduplicatedBundle("/nonexistent", []FileEntry{{RelPath: "missing.txt"}}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read")
}

func TestCreateDeduplicatedBundle_WithReadFileFunc(t *testing.T) {
	mockFiles := map[string][]byte{
		"a.txt": []byte(strings.Repeat("a", 5000)),
	}

	readFn := func(path string) ([]byte, error) {
		name := filepath.Base(path)
		if data, ok := mockFiles[name]; ok {
			return data, nil
		}
		return nil, fmt.Errorf("file not found: %s", path)
	}

	result, err := CreateDeduplicatedBundle("/fake", []FileEntry{{RelPath: "a.txt"}}, nil,
		WithDedupeReadFileFunc(readFn))
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.LargeBlobs))
}

func TestExtractDeduplicatedBundle_RoundTrip(t *testing.T) {
	// Create files
	srcDir := t.TempDir()
	smallContent := []byte("hi")
	largeContent := []byte(strings.Repeat("z", 5000))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "small.txt"), smallContent, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "large.bin"), largeContent, 0o644))

	files := []FileEntry{
		{RelPath: "small.txt"},
		{RelPath: "large.bin"},
	}

	result, err := CreateDeduplicatedBundle(srcDir, files, nil)
	require.NoError(t, err)

	// Build a layer map for the fetcher
	layers := map[int][]byte{}
	if result.SmallBlobsTar != nil {
		// Find the small tar layer index
		for _, f := range result.Manifest.Files {
			if f.Path == "small.txt" {
				layers[f.Layer] = result.SmallBlobsTar
				break
			}
		}
	}
	for _, blob := range result.LargeBlobs {
		layers[blob.Layer] = blob.Content
	}

	fetcher := func(layer int) ([]byte, error) {
		data, ok := layers[layer]
		if !ok {
			return nil, fmt.Errorf("layer %d not found", layer)
		}
		return data, nil
	}

	// Extract
	destDir := t.TempDir()
	err = ExtractDeduplicatedBundle(result.Manifest, destDir, fetcher)
	require.NoError(t, err)

	// Verify extracted files match originals
	extracted, err := os.ReadFile(filepath.Join(destDir, "small.txt"))
	require.NoError(t, err)
	assert.Equal(t, smallContent, extracted)

	extracted, err = os.ReadFile(filepath.Join(destDir, "large.bin"))
	require.NoError(t, err)
	assert.Equal(t, largeContent, extracted)
}

func TestExtractDeduplicatedBundle_NilManifest(t *testing.T) {
	err := ExtractDeduplicatedBundle(nil, t.TempDir(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manifest is nil")
}

func TestExtractDeduplicatedBundle_WrongVersion(t *testing.T) {
	m := &BundleManifest{Version: BundleManifestVersion1}
	err := ExtractDeduplicatedBundle(m, t.TempDir(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected manifest version")
}

func TestExtractDeduplicatedBundle_FetchError(t *testing.T) {
	m := &BundleManifest{
		Version: BundleManifestVersion2,
		Files:   []BundleFileEntry{{Path: "f.txt", Layer: 2}},
	}

	fetcher := func(layer int) ([]byte, error) {
		return nil, fmt.Errorf("network error")
	}

	err := ExtractDeduplicatedBundle(m, t.TempDir(), fetcher)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch layer")
}

func TestIsTarData(t *testing.T) {
	// Build a minimal valid tar header
	header := make([]byte, 512)
	copy(header[257:], "ustar")
	assert.True(t, isTarData(header))

	// Too short
	assert.False(t, isTarData([]byte("short")))

	// Wrong magic
	badHeader := make([]byte, 512)
	copy(badHeader[257:], "nope!")
	assert.False(t, isTarData(badHeader))
}

func TestDedupeOptions(t *testing.T) {
	cfg := &dedupeConfig{
		threshold: DefaultDedupeThreshold,
		maxSize:   DefaultMaxBundleSize,
		readFile:  os.ReadFile,
	}

	WithDedupeThreshold(1024)(cfg)
	assert.Equal(t, int64(1024), cfg.threshold)

	WithDedupeMaxSize(2048)(cfg)
	assert.Equal(t, int64(2048), cfg.maxSize)

	customRead := func(path string) ([]byte, error) { return nil, nil }
	WithDedupeReadFileFunc(customRead)(cfg)
	assert.NotNil(t, cfg.readFile)
}

func TestExtractBundleTarFromReader_ErrorOnBadData(t *testing.T) {
	tmpDir := t.TempDir()
	r := bytes.NewReader([]byte("not a tar"))
	_, err := ExtractBundleTarFromReader(r, tmpDir)
	assert.Error(t, err)
}
