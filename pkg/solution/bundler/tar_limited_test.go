// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// limitedTestTar builds a bundle tar holding files plus a manifest and
// returns the tar bytes and the total number of content bytes it carries
// (manifest included), which is what the extraction budget counts.
func limitedTestTar(t testing.TB, files map[string][]byte) ([]byte, int64) {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)

	manifest := &BundleManifest{Version: 1, Root: "."}
	for _, name := range names {
		manifest.Files = append(manifest.Files, BundleFileEntry{Path: name, Size: int64(len(files[name]))})
	}
	manifestJSON, err := json.Marshal(manifest)
	require.NoError(t, err)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, writeToTar(tw, BundleManifestPath, manifestJSON))
	total := int64(len(manifestJSON))
	for _, name := range names {
		require.NoError(t, writeToTar(tw, name, files[name]))
		total += int64(len(files[name]))
	}
	require.NoError(t, tw.Close())
	return buf.Bytes(), total
}

func TestExtractBundleTarLimited_UnderLimit(t *testing.T) {
	t.Parallel()
	data, total := limitedTestTar(t, map[string][]byte{
		"templates/a.tmpl": []byte("hello"),
		"static/b.txt":     []byte("world"),
	})

	dest := t.TempDir()
	manifest, err := ExtractBundleTarLimited(data, dest, total)
	require.NoError(t, err, "a bundle exactly at the limit must extract")
	require.NotNil(t, manifest)
	assert.Len(t, manifest.Files, 2)

	got, err := os.ReadFile(filepath.Join(dest, "templates", "a.tmpl"))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(got))
}

func TestExtractBundleTarLimited_ExceedsLimit(t *testing.T) {
	t.Parallel()
	data, total := limitedTestTar(t, map[string][]byte{
		"big.bin": bytes.Repeat([]byte("x"), 1024),
	})

	_, err := ExtractBundleTarLimited(data, t.TempDir(), total-1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBundleTooLarge)
}

func TestExtractBundleTarLimited_NonPositiveLimit(t *testing.T) {
	t.Parallel()
	data, _ := limitedTestTar(t, map[string][]byte{"a.txt": []byte("a")})

	for _, limit := range []int64{0, -1} {
		_, err := ExtractBundleTarLimited(data, t.TempDir(), limit)
		require.Error(t, err, "limit %d", limit)
		assert.NotErrorIs(t, err, ErrBundleTooLarge)
	}
}

func TestExtractBundleTarLimited_NestedCountsTowardLimit(t *testing.T) {
	t.Parallel()
	inner, _ := limitedTestTar(t, map[string][]byte{"inner.tmpl": []byte("inner content")})
	outer, outerTotal := limitedTestTar(t, map[string][]byte{"sub/child.bundle.tar": inner})

	// The unlimited extractor accepts the same archive.
	_, err := ExtractBundleTar(outer, t.TempDir())
	require.NoError(t, err)

	// Budget covers the outer entries but not the nested bundle's contents.
	// Nested failures are otherwise best-effort, so the size error must
	// still propagate.
	_, err = ExtractBundleTarLimited(outer, t.TempDir(), outerTotal+1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBundleTooLarge)
}

func TestExtractBundleTarLimited_PathTraversal(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, writeToTar(tw, "../../etc/passwd", []byte("evil")))
	require.NoError(t, tw.Close())

	_, err := ExtractBundleTarLimited(buf.Bytes(), t.TempDir(), DefaultMaxBundleSize)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func BenchmarkExtractBundleTarLimited(b *testing.B) {
	data, _ := limitedTestTar(b, map[string][]byte{
		"templates/a.tmpl": bytes.Repeat([]byte("a"), 4096),
		"templates/b.tmpl": bytes.Repeat([]byte("b"), 4096),
	})
	for b.Loop() {
		if _, err := ExtractBundleTarLimited(data, b.TempDir(), DefaultMaxBundleSize); err != nil {
			b.Fatal(err)
		}
	}
}
