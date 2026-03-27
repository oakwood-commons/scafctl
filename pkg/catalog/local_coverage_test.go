// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalCatalog_FetchWithBundle_NoBundleLayer(t *testing.T) {
	cat := newTestCatalog(t)
	ctx := context.Background()

	ref := testRef("sol-no-bundle", "1.0.0")
	ref.Kind = ArtifactKindSolution

	// Store without bundle data
	_, err := cat.Store(ctx, ref, []byte("solution content"), nil, nil, false)
	require.NoError(t, err)

	// FetchWithBundle should return nil for bundleData
	content, bundleData, info, err := cat.FetchWithBundle(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, []byte("solution content"), content)
	assert.Nil(t, bundleData)
	assert.Equal(t, ref.Name, info.Reference.Name)
}

func TestLocalCatalog_FetchWithBundle_WithBundle(t *testing.T) {
	cat := newTestCatalog(t)
	ctx := context.Background()

	ref := testRef("sol-with-bundle", "1.0.0")
	ref.Kind = ArtifactKindSolution

	bundleContent := []byte("bundled tar data")
	_, err := cat.Store(ctx, ref, []byte("solution content"), bundleContent, nil, false)
	require.NoError(t, err)

	content, bundleData, info, err := cat.FetchWithBundle(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, []byte("solution content"), content)
	assert.Equal(t, bundleContent, bundleData)
	assert.Equal(t, ref.Name, info.Reference.Name)
}

func TestLocalCatalog_FetchWithBundle_NotFound(t *testing.T) {
	cat := newTestCatalog(t)
	ctx := context.Background()

	ref := testRef("nonexistent", "1.0.0")
	ref.Kind = ArtifactKindSolution

	_, _, _, err := cat.FetchWithBundle(ctx, ref) //nolint:dogsled // only testing error path
	require.Error(t, err)
	assert.True(t, IsNotFound(err))
}

func TestWriteTarEntry(t *testing.T) {
	t.Parallel()

	t.Run("writes valid tar entry", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)

		err := writeTarEntry(tw, "test.txt", []byte("hello world"))
		require.NoError(t, err)
		require.NoError(t, tw.Close())

		// Verify we can read it back
		content, err := extractFileFromTar(buf.Bytes(), "test.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("hello world"), content)
	})

	t.Run("writes multiple entries", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)

		require.NoError(t, writeTarEntry(tw, "a.txt", []byte("aaa")))
		require.NoError(t, writeTarEntry(tw, "b.txt", []byte("bbb")))
		require.NoError(t, tw.Close())

		contentA, err := extractFileFromTar(buf.Bytes(), "a.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("aaa"), contentA)

		contentB, err := extractFileFromTar(buf.Bytes(), "b.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("bbb"), contentB)
	})
}

func TestExtractFileFromTar(t *testing.T) {
	t.Parallel()

	t.Run("extracts existing file", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		require.NoError(t, writeTarEntry(tw, "dir/file.txt", []byte("content")))
		require.NoError(t, writeTarEntry(tw, "other.txt", []byte("other")))
		require.NoError(t, tw.Close())

		content, err := extractFileFromTar(buf.Bytes(), "dir/file.txt")
		require.NoError(t, err)
		assert.Equal(t, []byte("content"), content)
	})

	t.Run("returns error for missing file", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		require.NoError(t, writeTarEntry(tw, "existing.txt", []byte("data")))
		require.NoError(t, tw.Close())

		_, err := extractFileFromTar(buf.Bytes(), "missing.txt")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found in tar")
	})

	t.Run("handles empty tar", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		require.NoError(t, tw.Close())

		_, err := extractFileFromTar(buf.Bytes(), "any.txt")
		require.Error(t, err)
	})
}

func TestLocalCatalog_StoreForce(t *testing.T) {
	cat := newTestCatalog(t)
	ctx := context.Background()

	ref := testRef("force-test", "1.0.0")
	ref.Kind = ArtifactKindSolution

	_, err := cat.Store(ctx, ref, []byte("v1"), nil, nil, false)
	require.NoError(t, err)

	// Without force should fail
	_, err = cat.Store(ctx, ref, []byte("v2"), nil, nil, false)
	require.Error(t, err)
	assert.True(t, IsExists(err))

	// With force should succeed
	_, err = cat.Store(ctx, ref, []byte("v2"), nil, nil, true)
	require.NoError(t, err)

	// Verify updated content
	content, _, err := cat.Fetch(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), content)
}

func TestLocalCatalog_StoreWithAnnotations(t *testing.T) {
	cat := newTestCatalog(t)
	ctx := context.Background()

	ref := testRef("annotated", "1.0.0")
	ref.Kind = ArtifactKindSolution

	annotations := map[string]string{
		"custom-key": "custom-value",
		"author":     "test",
	}

	info, err := cat.Store(ctx, ref, []byte("content"), nil, annotations, false)
	require.NoError(t, err)
	assert.Equal(t, "custom-value", info.Annotations["custom-key"])
	assert.Equal(t, "test", info.Annotations["author"])
	// Built-in annotations should also be present
	assert.Equal(t, "solution", info.Annotations[AnnotationArtifactType])
	assert.Equal(t, "annotated", info.Annotations[AnnotationArtifactName])
}

func TestLocalCatalog_ConcurrentAccess(t *testing.T) {
	cat := newTestCatalog(t)
	ctx := context.Background()

	// Store a base artifact
	ref := testRef("concurrent", "1.0.0")
	ref.Kind = ArtifactKindSolution
	_, err := cat.Store(ctx, ref, []byte("content"), nil, nil, false)
	require.NoError(t, err)

	// Concurrent reads should not panic.
	// Collect errors via a channel and assert in the main goroutine to avoid
	// calling testing.T from multiple goroutines (which is not safe).
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, _, fetchErr := cat.Fetch(ctx, ref)
			errs <- fetchErr
		}()
	}
	for i := 0; i < 10; i++ {
		require.NoError(t, <-errs)
	}
}

func TestLocalCatalog_Prune_NoOrphans(t *testing.T) {
	cat := newTestCatalog(t)
	ctx := context.Background()

	ref := testRef("kept", "1.0.0")
	ref.Kind = ArtifactKindSolution
	_, err := cat.Store(ctx, ref, []byte("content"), nil, nil, false)
	require.NoError(t, err)

	result, err := cat.Prune(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, result.RemovedManifests)
	assert.Equal(t, 0, result.RemovedBlobs)
}

func TestNewLocalCatalogAt_InvalidPath(t *testing.T) {
	// Path to a file (not a directory) to trigger an error scenario
	tmpFile := t.TempDir() + "/testfile"
	require.NoError(t, os.WriteFile(tmpFile, []byte("x"), 0o600))

	// Try to create catalog in a subdirectory of a file (impossible)
	_, err := NewLocalCatalogAt(tmpFile+"/subdir", logr.Discard())
	require.Error(t, err)
}

// Benchmarks

func BenchmarkLocalCatalog_Store(b *testing.B) {
	tmpDir := b.TempDir()
	cat, err := NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(b, err)
	ctx := context.Background()
	content := []byte("benchmark content")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ref := testRef("bench", fmt.Sprintf("1.0.%d", i))
		ref.Kind = ArtifactKindSolution
		_, _ = cat.Store(ctx, ref, content, nil, nil, false)
	}
}

func BenchmarkLocalCatalog_Fetch(b *testing.B) {
	tmpDir := b.TempDir()
	cat, err := NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(b, err)
	ctx := context.Background()

	ref := testRef("bench", "1.0.0")
	ref.Kind = ArtifactKindSolution
	_, err = cat.Store(ctx, ref, []byte("benchmark content"), nil, nil, false)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = cat.Fetch(ctx, ref)
	}
}

func TestLocalCatalog_StoreDedup_FetchWithBundle_RoundTrip(t *testing.T) {
	t.Parallel()

	cat := newTestCatalog(t)
	ctx := context.Background()

	ref := testRef("dedup-roundtrip", "1.0.0")
	ref.Kind = ArtifactKindSolution

	solutionYAML := []byte("apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: test\n")

	// Build a small tar with one file (simulating what CreateDeduplicatedBundle does)
	var smallTarBuf bytes.Buffer
	tw := tar.NewWriter(&smallTarBuf)
	fileContent := []byte("hello from bundle")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "data/info.txt",
		Size: int64(len(fileContent)),
		Mode: 0o644,
	}))
	_, err := tw.Write(fileContent)
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	// Build a manifest JSON pointing to layer 2 (small tar)
	manifestJSON := []byte(fmt.Sprintf(`{
		"version": 2,
		"root": ".",
		"files": [
			{"path": "data/info.txt", "size": %d, "digest": "sha256:abc", "layer": 2}
		]
	}`, len(fileContent)))

	_, err = cat.StoreDedup(ctx, ref, solutionYAML, manifestJSON, smallTarBuf.Bytes(), nil, nil, false)
	require.NoError(t, err)

	// FetchWithBundle should reassemble and return correct content
	content, bundleData, info, err := cat.FetchWithBundle(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, solutionYAML, content)
	assert.Equal(t, ref.Name, info.Reference.Name)
	require.NotNil(t, bundleData, "bundle data should not be nil")

	// Extract the file from the reassembled tar and verify its content
	extracted, err := extractFileFromTar(bundleData, "data/info.txt")
	require.NoError(t, err)
	assert.Equal(t, fileContent, extracted, "extracted bundle file should match original content, not solution YAML")
}

func BenchmarkLocalCatalog_ReassembleDedup(b *testing.B) {
	tmpDir := b.TempDir()
	cat, err := NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(b, err)
	ctx := context.Background()

	ref := testRef("bench-dedup", "1.0.0")
	ref.Kind = ArtifactKindSolution

	var smallTarBuf bytes.Buffer
	tw := tar.NewWriter(&smallTarBuf)
	require.NoError(b, tw.WriteHeader(&tar.Header{Name: "data/f.txt", Size: 5, Mode: 0o644}))
	_, _ = tw.Write([]byte("hello"))
	require.NoError(b, tw.Close())

	manifestJSON := []byte(`{"version":2,"root":".","files":[{"path":"data/f.txt","size":5,"digest":"sha256:abc","layer":2}]}`)
	_, err = cat.StoreDedup(ctx, ref, []byte("sol"), manifestJSON, smallTarBuf.Bytes(), nil, nil, false)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = cat.FetchWithBundle(ctx, ref)
	}
}
