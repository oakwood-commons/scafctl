// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateBundleTar_BasicFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "templates", "main.tf.tmpl"), []byte("terraform content"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "child.yaml"), []byte("child content"), 0o644))

	files := []FileEntry{
		{RelPath: "templates/main.tf.tmpl", Source: StaticAnalysis},
		{RelPath: "child.yaml", Source: ExplicitInclude},
	}

	tarData, manifest, err := CreateBundleTar(tmpDir, files, nil)
	require.NoError(t, err)
	require.NotNil(t, manifest)

	// Verify manifest
	assert.Equal(t, 1, manifest.Version)
	assert.Equal(t, ".", manifest.Root)
	assert.Len(t, manifest.Files, 2)
	assert.Equal(t, "templates/main.tf.tmpl", manifest.Files[0].Path)
	assert.Equal(t, int64(17), manifest.Files[0].Size)
	assert.NotEmpty(t, manifest.Files[0].Digest)
	assert.Equal(t, "child.yaml", manifest.Files[1].Path)
	assert.Equal(t, int64(13), manifest.Files[1].Size)

	// Verify tar contents
	files_in_tar := listTarEntries(t, tarData)
	assert.Contains(t, files_in_tar, BundleManifestPath)
	assert.Contains(t, files_in_tar, "templates/main.tf.tmpl")
	assert.Contains(t, files_in_tar, "child.yaml")
}

func TestCreateBundleTar_WithPlugins(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("data"), 0o644))

	files := []FileEntry{
		{RelPath: "file.txt", Source: StaticAnalysis},
	}
	plugins := []BundlePluginEntry{
		{Name: "aws-provider", Kind: "provider", Version: "^1.5.0"},
		{Name: "vault-auth", Kind: "auth-handler", Version: "~1.2.0"},
	}

	_, manifest, err := CreateBundleTar(tmpDir, files, plugins)
	require.NoError(t, err)

	assert.Len(t, manifest.Plugins, 2)
	assert.Equal(t, "aws-provider", manifest.Plugins[0].Name)
	assert.Equal(t, "provider", manifest.Plugins[0].Kind)
	assert.Equal(t, "^1.5.0", manifest.Plugins[0].Version)
}

func TestCreateBundleTar_ExceedsMaxSize(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file that's bigger than our limit
	bigData := make([]byte, 1024)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "big.bin"), bigData, 0o644))

	files := []FileEntry{
		{RelPath: "big.bin", Source: StaticAnalysis},
	}

	_, _, err := CreateBundleTar(tmpDir, files, nil, WithMaxBundleSize(100))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bundle exceeds maximum size limit")
}

func TestCreateBundleTar_EmptyFileList(t *testing.T) {
	tarData, manifest, err := CreateBundleTar(t.TempDir(), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, manifest.Files)
	assert.NotEmpty(t, tarData, "tar should at least contain the manifest")
}

func TestExtractBundleTar_RoundTrip(t *testing.T) {
	srcDir := t.TempDir()

	// Create source files
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "templates", "main.tf.tmpl"), []byte("terraform content"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "child.yaml"), []byte("child content"), 0o644))

	files := []FileEntry{
		{RelPath: "templates/main.tf.tmpl", Source: StaticAnalysis},
		{RelPath: "child.yaml", Source: ExplicitInclude},
	}

	// Create tar
	tarData, _, err := CreateBundleTar(srcDir, files, nil)
	require.NoError(t, err)

	// Extract tar
	destDir := t.TempDir()
	manifest, err := ExtractBundleTar(tarData, destDir)
	require.NoError(t, err)
	require.NotNil(t, manifest)

	// Verify extracted files
	content, err := os.ReadFile(filepath.Join(destDir, "templates", "main.tf.tmpl"))
	require.NoError(t, err)
	assert.Equal(t, "terraform content", string(content))

	content, err = os.ReadFile(filepath.Join(destDir, "child.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "child content", string(content))

	// Verify manifest was extracted and parsed
	assert.Equal(t, 1, manifest.Version)
	assert.Len(t, manifest.Files, 2)
}

func TestExtractBundleTar_PathTraversal(t *testing.T) {
	// Create a malicious tar with path traversal
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Try to write a file outside the dest dir
	header := &tar.Header{
		Name: "../../etc/passwd",
		Size: 4,
		Mode: 0o644,
	}
	require.NoError(t, tw.WriteHeader(header))
	_, err := tw.Write([]byte("evil"))
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	_, err = ExtractBundleTar(buf.Bytes(), t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestExtractBundleTar_NoManifest(t *testing.T) {
	// Create tar without manifest
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	header := &tar.Header{
		Name: "file.txt",
		Size: 4,
		Mode: 0o644,
	}
	require.NoError(t, tw.WriteHeader(header))
	_, err := tw.Write([]byte("data"))
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	_, err = ExtractBundleTar(buf.Bytes(), t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not contain a manifest")
}

func TestCreateBundleTar_DigestConsistency(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("hello"), 0o644))

	files := []FileEntry{
		{RelPath: "file.txt", Source: StaticAnalysis},
	}

	_, manifest1, err := CreateBundleTar(tmpDir, files, nil)
	require.NoError(t, err)

	_, manifest2, err := CreateBundleTar(tmpDir, files, nil)
	require.NoError(t, err)

	assert.Equal(t, manifest1.Files[0].Digest, manifest2.Files[0].Digest, "digests should be consistent")
}

func listTarEntries(t *testing.T, tarData []byte) []string {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(tarData))
	var entries []string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		entries = append(entries, header.Name)
	}
	return entries
}
