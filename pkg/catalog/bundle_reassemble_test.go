// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"archive/tar"
	"bytes"
	"fmt"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildTarBlob creates a tar archive containing the given file entries.
func buildTarBlob(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range entries {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name,
			Size: int64(len(content)),
			Mode: 0o644,
		}))
		_, err := tw.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

// makeDesc creates a descriptor with a digest computed from data.
func makeDesc(mediaType string, data []byte) ocispec.Descriptor {
	return ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}
}

func TestReassembleDedupBundle_RawBlob(t *testing.T) {
	t.Parallel()

	fileContent := []byte("hello world")

	manifestJSON := []byte(fmt.Sprintf(`{
		"version": 2,
		"root": ".",
		"files": [
			{"path": "data/readme.txt", "size": %d, "digest": "sha256:abc", "layer": 2}
		]
	}`, len(fileContent)))

	// Layer 0 = solution YAML (not used by reassemble)
	solutionData := []byte("apiVersion: scafctl.io/v1")
	// Layer 1 = bundle manifest
	// Layer 2 = raw blob
	ociManifest := ocispec.Manifest{
		Layers: []ocispec.Descriptor{
			makeDesc(MediaTypeSolutionContent, solutionData),
			makeDesc(MediaTypeSolutionBundleManifest, manifestJSON),
			makeDesc(MediaTypeSolutionBundleBlob, fileContent),
		},
	}

	blobs := map[digest.Digest][]byte{
		ociManifest.Layers[0].Digest: solutionData,
		ociManifest.Layers[1].Digest: manifestJSON,
		ociManifest.Layers[2].Digest: fileContent,
	}

	fetchBlob := func(desc ocispec.Descriptor) ([]byte, error) {
		data, ok := blobs[desc.Digest]
		if !ok {
			return nil, fmt.Errorf("blob not found: %s", desc.Digest)
		}
		return data, nil
	}

	result, err := reassembleDedupBundle(ociManifest, fetchBlob)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the file is in the reassembled tar
	extracted, err := extractFileFromTar(result, "data/readme.txt")
	require.NoError(t, err)
	assert.Equal(t, fileContent, extracted)

	// Verify v1 manifest is embedded
	manifestData, err := extractFileFromTar(result, ".scafctl/bundle-manifest.json")
	require.NoError(t, err)
	assert.Contains(t, string(manifestData), `"version": 1`)
}

func TestReassembleDedupBundle_SmallTar(t *testing.T) {
	t.Parallel()

	file1 := []byte("file one content")
	file2 := []byte("file two content")

	// Build a small tar layer containing both files
	smallTar := buildTarBlob(t, map[string][]byte{
		"config/a.yaml": file1,
		"config/b.yaml": file2,
	})

	manifestJSON := []byte(fmt.Sprintf(`{
		"version": 2,
		"root": ".",
		"files": [
			{"path": "config/a.yaml", "size": %d, "digest": "sha256:aaa", "layer": 2},
			{"path": "config/b.yaml", "size": %d, "digest": "sha256:bbb", "layer": 2}
		]
	}`, len(file1), len(file2)))

	solutionData := []byte("solution")
	ociManifest := ocispec.Manifest{
		Layers: []ocispec.Descriptor{
			makeDesc(MediaTypeSolutionContent, solutionData),
			makeDesc(MediaTypeSolutionBundleManifest, manifestJSON),
			makeDesc(MediaTypeSolutionBundleSmallTar, smallTar),
		},
	}

	blobs := map[digest.Digest][]byte{
		ociManifest.Layers[0].Digest: solutionData,
		ociManifest.Layers[1].Digest: manifestJSON,
		ociManifest.Layers[2].Digest: smallTar,
	}

	fetchBlob := func(desc ocispec.Descriptor) ([]byte, error) {
		data, ok := blobs[desc.Digest]
		if !ok {
			return nil, fmt.Errorf("blob not found: %s", desc.Digest)
		}
		return data, nil
	}

	result, err := reassembleDedupBundle(ociManifest, fetchBlob)
	require.NoError(t, err)

	extracted1, err := extractFileFromTar(result, "config/a.yaml")
	require.NoError(t, err)
	assert.Equal(t, file1, extracted1)

	extracted2, err := extractFileFromTar(result, "config/b.yaml")
	require.NoError(t, err)
	assert.Equal(t, file2, extracted2)
}

func TestReassembleDedupBundle_InsufficientLayers(t *testing.T) {
	t.Parallel()

	ociManifest := ocispec.Manifest{
		Layers: []ocispec.Descriptor{
			makeDesc(MediaTypeSolutionContent, []byte("sol")),
		},
	}

	fetchBlob := func(_ ocispec.Descriptor) ([]byte, error) {
		return nil, fmt.Errorf("should not be called")
	}

	_, err := reassembleDedupBundle(ociManifest, fetchBlob)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient layers")
}

func TestReassembleDedupBundle_FetchError(t *testing.T) {
	t.Parallel()

	solutionData := []byte("solution")
	manifestJSON := []byte(`{"version":2,"root":".","files":[{"path":"f.txt","size":5,"digest":"sha256:x","layer":2}]}`)

	ociManifest := ocispec.Manifest{
		Layers: []ocispec.Descriptor{
			makeDesc(MediaTypeSolutionContent, solutionData),
			makeDesc(MediaTypeSolutionBundleManifest, manifestJSON),
			makeDesc(MediaTypeSolutionBundleBlob, []byte("data")),
		},
	}

	callCount := 0
	fetchBlob := func(desc ocispec.Descriptor) ([]byte, error) {
		callCount++
		if callCount == 1 {
			// Return manifest on first call
			return manifestJSON, nil
		}
		return nil, fmt.Errorf("network error")
	}

	_, err := reassembleDedupBundle(ociManifest, fetchBlob)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch layer")
}

func TestReassembleDedupBundle_LayerOutOfRange(t *testing.T) {
	t.Parallel()

	manifestJSON := []byte(`{"version":2,"root":".","files":[{"path":"f.txt","size":5,"digest":"sha256:x","layer":99}]}`)
	solutionData := []byte("solution")

	ociManifest := ocispec.Manifest{
		Layers: []ocispec.Descriptor{
			makeDesc(MediaTypeSolutionContent, solutionData),
			makeDesc(MediaTypeSolutionBundleManifest, manifestJSON),
		},
	}

	blobs := map[digest.Digest][]byte{
		ociManifest.Layers[1].Digest: manifestJSON,
	}

	fetchBlob := func(desc ocispec.Descriptor) ([]byte, error) {
		data, ok := blobs[desc.Digest]
		if !ok {
			return nil, fmt.Errorf("blob not found")
		}
		return data, nil
	}

	_, err := reassembleDedupBundle(ociManifest, fetchBlob)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "layer index 99 out of range")
}

func BenchmarkReassembleDedupBundle(b *testing.B) {
	fileContent := []byte("benchmark file content here")
	manifestJSON := []byte(fmt.Sprintf(`{"version":2,"root":".","files":[{"path":"data/f.txt","size":%d,"digest":"sha256:abc","layer":2}]}`, len(fileContent)))
	solutionData := []byte("solution")

	ociManifest := ocispec.Manifest{
		Layers: []ocispec.Descriptor{
			makeDesc(MediaTypeSolutionContent, solutionData),
			makeDesc(MediaTypeSolutionBundleManifest, manifestJSON),
			makeDesc(MediaTypeSolutionBundleBlob, fileContent),
		},
	}

	blobs := map[digest.Digest][]byte{
		ociManifest.Layers[1].Digest: manifestJSON,
		ociManifest.Layers[2].Digest: fileContent,
	}

	fetchBlob := func(desc ocispec.Descriptor) ([]byte, error) {
		return blobs[desc.Digest], nil
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = reassembleDedupBundle(ociManifest, fetchBlob)
	}
}
