// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCatalog(t *testing.T) *LocalCatalog {
	t.Helper()
	tmpDir := t.TempDir()
	catalog, err := NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)
	return catalog
}

func TestNewLocalCatalogAt(t *testing.T) {
	t.Run("creates directory if not exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := tmpDir + "/nested/catalog"

		catalog, err := NewLocalCatalogAt(path, logr.Discard())
		require.NoError(t, err)
		assert.NotNil(t, catalog)
		assert.Equal(t, path, catalog.Path())

		// Verify directory was created
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("returns local as name", func(t *testing.T) {
		catalog := newTestCatalog(t)
		assert.Equal(t, LocalCatalogName, catalog.Name())
	})
}

func TestLocalCatalog_Store(t *testing.T) {
	ctx := context.Background()

	t.Run("stores artifact successfully", func(t *testing.T) {
		catalog := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		content := []byte("name: my-solution\nversion: 1.0.0")
		annotations := map[string]string{
			"description": "Test solution",
		}

		info, err := catalog.Store(ctx, ref, content, nil, annotations, false)
		require.NoError(t, err)
		assert.Equal(t, ref.Name, info.Reference.Name)
		assert.Equal(t, ref.Version.String(), info.Reference.Version.String())
		assert.NotEmpty(t, info.Digest)
		assert.NotZero(t, info.CreatedAt)
		assert.Equal(t, LocalCatalogName, info.Catalog)
	})

	t.Run("fails when artifact exists", func(t *testing.T) {
		catalog := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		content := []byte("content")

		// Store first time
		_, err := catalog.Store(ctx, ref, content, nil, nil, false)
		require.NoError(t, err)

		// Store again without force should fail
		_, err = catalog.Store(ctx, ref, content, nil, nil, false)
		require.Error(t, err)
		assert.True(t, IsExists(err))
	})

	t.Run("overwrites with force", func(t *testing.T) {
		catalog := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		content1 := []byte("content v1")
		content2 := []byte("content v2")

		// Store first time
		_, err := catalog.Store(ctx, ref, content1, nil, nil, false)
		require.NoError(t, err)

		// Store again with force should succeed
		info, err := catalog.Store(ctx, ref, content2, nil, nil, true)
		require.NoError(t, err)
		assert.NotEmpty(t, info.Digest)

		// Verify new content
		fetched, _, err := catalog.Fetch(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, content2, fetched)
	})
}

func TestLocalCatalog_Fetch(t *testing.T) {
	ctx := context.Background()

	t.Run("fetches stored artifact", func(t *testing.T) {
		catalog := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		content := []byte("name: my-solution")

		_, err := catalog.Store(ctx, ref, content, nil, nil, false)
		require.NoError(t, err)

		fetched, info, err := catalog.Fetch(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, content, fetched)
		assert.Equal(t, ref.Name, info.Reference.Name)
	})

	t.Run("returns error for non-existent artifact", func(t *testing.T) {
		catalog := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "non-existent",
			Version: semver.MustParse("1.0.0"),
		}

		_, _, err := catalog.Fetch(ctx, ref)
		require.Error(t, err)
		assert.True(t, IsNotFound(err))
	})
}

func TestLocalCatalog_Resolve(t *testing.T) {
	ctx := context.Background()

	t.Run("resolves exact version", func(t *testing.T) {
		catalog := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		_, err := catalog.Store(ctx, ref, []byte("v1.0.0"), nil, nil, false)
		require.NoError(t, err)

		info, err := catalog.Resolve(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", info.Reference.Version.String())
	})

	t.Run("resolves highest version when not specified", func(t *testing.T) {
		catalog := newTestCatalog(t)

		// Store multiple versions
		versions := []string{"1.0.0", "2.0.0", "1.5.0"}
		for _, v := range versions {
			ref := Reference{
				Kind:    ArtifactKindSolution,
				Name:    "my-solution",
				Version: semver.MustParse(v),
			}
			_, err := catalog.Store(ctx, ref, []byte("version "+v), nil, nil, false)
			require.NoError(t, err)
		}

		// Resolve without version should get highest
		ref := Reference{
			Kind: ArtifactKindSolution,
			Name: "my-solution",
		}
		info, err := catalog.Resolve(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, "2.0.0", info.Reference.Version.String())
	})

	t.Run("returns error when no versions exist", func(t *testing.T) {
		catalog := newTestCatalog(t)

		ref := Reference{
			Kind: ArtifactKindSolution,
			Name: "non-existent",
		}

		_, err := catalog.Resolve(ctx, ref)
		require.Error(t, err)
		assert.True(t, IsNotFound(err))
	})

	t.Run("excludes pre-release versions by default", func(t *testing.T) {
		cat := newTestCatalog(t)

		// Store stable and pre-release versions
		for _, v := range []string{"1.0.0", "2.0.0-beta.1", "1.5.0"} {
			ref := Reference{
				Kind:    ArtifactKindSolution,
				Name:    "my-solution",
				Version: semver.MustParse(v),
			}
			_, err := cat.Store(ctx, ref, []byte("version "+v), nil, nil, false)
			require.NoError(t, err)
		}

		// Resolve without version should skip pre-release and get 1.5.0
		ref := Reference{
			Kind: ArtifactKindSolution,
			Name: "my-solution",
		}
		info, err := cat.Resolve(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, "1.5.0", info.Reference.Version.String())
	})

	t.Run("includes pre-release versions when context flag is set", func(t *testing.T) {
		cat := newTestCatalog(t)

		for _, v := range []string{"1.0.0", "2.0.0-beta.1", "1.5.0"} {
			ref := Reference{
				Kind:    ArtifactKindSolution,
				Name:    "my-solution",
				Version: semver.MustParse(v),
			}
			_, err := cat.Store(ctx, ref, []byte("version "+v), nil, nil, false)
			require.NoError(t, err)
		}

		// With pre-release included, should get 2.0.0-beta.1
		preReleaseCtx := WithIncludePreRelease(ctx)
		ref := Reference{
			Kind: ArtifactKindSolution,
			Name: "my-solution",
		}
		info, err := cat.Resolve(preReleaseCtx, ref)
		require.NoError(t, err)
		assert.Equal(t, "2.0.0-beta.1", info.Reference.Version.String())
	})

	t.Run("falls back to pre-release when no stable versions exist", func(t *testing.T) {
		cat := newTestCatalog(t)

		for _, v := range []string{"1.0.0-alpha.1", "2.0.0-beta.1"} {
			ref := Reference{
				Kind:    ArtifactKindSolution,
				Name:    "my-solution",
				Version: semver.MustParse(v),
			}
			_, err := cat.Store(ctx, ref, []byte("version "+v), nil, nil, false)
			require.NoError(t, err)
		}

		// No stable versions - should fall back to highest pre-release
		ref := Reference{
			Kind: ArtifactKindSolution,
			Name: "my-solution",
		}
		info, err := cat.Resolve(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, "2.0.0-beta.1", info.Reference.Version.String())
	})
}

func TestLocalCatalog_List(t *testing.T) {
	ctx := context.Background()

	t.Run("lists all artifacts", func(t *testing.T) {
		catalog := newTestCatalog(t)

		// Store some solutions
		for _, name := range []string{"sol-a", "sol-b"} {
			ref := Reference{
				Kind:    ArtifactKindSolution,
				Name:    name,
				Version: semver.MustParse("1.0.0"),
			}
			_, err := catalog.Store(ctx, ref, []byte("content"), nil, nil, false)
			require.NoError(t, err)
		}

		artifacts, err := catalog.List(ctx, "", "")
		require.NoError(t, err)
		assert.Len(t, artifacts, 2)
	})

	t.Run("filters by kind", func(t *testing.T) {
		catalog := newTestCatalog(t)

		// Store solution
		solRef := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		_, err := catalog.Store(ctx, solRef, []byte("solution"), nil, nil, false)
		require.NoError(t, err)

		// Store provider
		providerRef := Reference{
			Kind:    ArtifactKindProvider,
			Name:    "my-provider",
			Version: semver.MustParse("1.0.0"),
		}
		_, err = catalog.Store(ctx, providerRef, []byte("provider"), nil, nil, false)
		require.NoError(t, err)

		// List only solutions
		solutions, err := catalog.List(ctx, ArtifactKindSolution, "")
		require.NoError(t, err)
		assert.Len(t, solutions, 1)
		assert.Equal(t, "my-solution", solutions[0].Reference.Name)
	})

	t.Run("filters by name", func(t *testing.T) {
		catalog := newTestCatalog(t)

		// Store multiple versions
		for _, v := range []string{"1.0.0", "2.0.0"} {
			ref := Reference{
				Kind:    ArtifactKindSolution,
				Name:    "my-solution",
				Version: semver.MustParse(v),
			}
			_, err := catalog.Store(ctx, ref, []byte("content"), nil, nil, false)
			require.NoError(t, err)
		}

		// Store another solution
		otherRef := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "other-solution",
			Version: semver.MustParse("1.0.0"),
		}
		_, err := catalog.Store(ctx, otherRef, []byte("content"), nil, nil, false)
		require.NoError(t, err)

		// List only my-solution
		artifacts, err := catalog.List(ctx, "", "my-solution")
		require.NoError(t, err)
		assert.Len(t, artifacts, 2)
		for _, a := range artifacts {
			assert.Equal(t, "my-solution", a.Reference.Name)
		}
	})
}

func TestLocalCatalog_Exists(t *testing.T) {
	ctx := context.Background()

	t.Run("returns true for existing artifact", func(t *testing.T) {
		catalog := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		_, err := catalog.Store(ctx, ref, []byte("content"), nil, nil, false)
		require.NoError(t, err)

		exists, err := catalog.Exists(ctx, ref)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("returns false for non-existent artifact", func(t *testing.T) {
		catalog := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "non-existent",
			Version: semver.MustParse("1.0.0"),
		}

		exists, err := catalog.Exists(ctx, ref)
		require.NoError(t, err)
		assert.False(t, exists)
	})
}

func TestLocalCatalog_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("deletes existing artifact", func(t *testing.T) {
		catalog := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		_, err := catalog.Store(ctx, ref, []byte("content"), nil, nil, false)
		require.NoError(t, err)

		err = catalog.Delete(ctx, ref)
		require.NoError(t, err)

		// Verify deleted
		exists, err := catalog.Exists(ctx, ref)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("returns error for non-existent artifact", func(t *testing.T) {
		catalog := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "non-existent",
			Version: semver.MustParse("1.0.0"),
		}

		err := catalog.Delete(ctx, ref)
		require.Error(t, err)
		assert.True(t, IsNotFound(err))
	})
}

func TestLocalCatalog_Save(t *testing.T) {
	ctx := context.Background()

	t.Run("saves artifact to tar archive", func(t *testing.T) {
		catalog := newTestCatalog(t)

		// Store an artifact first
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		content := []byte("name: my-solution\nversion: 1.0.0")
		_, err := catalog.Store(ctx, ref, content, nil, nil, false)
		require.NoError(t, err)

		// Save to tar
		outputPath := t.TempDir() + "/output.tar"
		result, err := catalog.Save(ctx, "my-solution", "", outputPath)
		require.NoError(t, err)

		assert.Equal(t, "my-solution", result.Reference.Name)
		assert.Equal(t, "1.0.0", result.Reference.Version.String())
		assert.Equal(t, outputPath, result.OutputPath)
		assert.Greater(t, result.Size, int64(0))
		assert.NotEmpty(t, result.Digest)

		// Verify file exists
		info, err := os.Stat(outputPath)
		require.NoError(t, err)
		assert.Greater(t, info.Size(), int64(0))
	})

	t.Run("saves specific version", func(t *testing.T) {
		catalog := newTestCatalog(t)

		// Store multiple versions
		for _, ver := range []string{"1.0.0", "2.0.0"} {
			ref := Reference{
				Kind:    ArtifactKindSolution,
				Name:    "my-solution",
				Version: semver.MustParse(ver),
			}
			_, err := catalog.Store(ctx, ref, []byte("version: "+ver), nil, nil, false)
			require.NoError(t, err)
		}

		// Save specific version
		outputPath := t.TempDir() + "/output.tar"
		result, err := catalog.Save(ctx, "my-solution", "1.0.0", outputPath)
		require.NoError(t, err)

		assert.Equal(t, "1.0.0", result.Reference.Version.String())
	})

	t.Run("returns error for non-existent artifact", func(t *testing.T) {
		catalog := newTestCatalog(t)

		outputPath := t.TempDir() + "/output.tar"
		_, err := catalog.Save(ctx, "non-existent", "", outputPath)
		require.Error(t, err)
		assert.True(t, IsNotFound(err))
	})
}

func TestLocalCatalog_Load(t *testing.T) {
	ctx := context.Background()

	t.Run("loads artifact from tar archive", func(t *testing.T) {
		// Create source catalog and store artifact
		srcCatalog := newTestCatalog(t)
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		content := []byte("name: my-solution\nversion: 1.0.0")
		_, err := srcCatalog.Store(ctx, ref, content, nil, nil, false)
		require.NoError(t, err)

		// Save to tar
		tarPath := t.TempDir() + "/artifact.tar"
		_, err = srcCatalog.Save(ctx, "my-solution", "", tarPath)
		require.NoError(t, err)

		// Create destination catalog and load
		dstCatalog := newTestCatalog(t)
		result, err := dstCatalog.Load(ctx, tarPath, false)
		require.NoError(t, err)

		assert.Equal(t, "my-solution", result.Reference.Name)
		assert.Equal(t, "1.0.0", result.Reference.Version.String())
		assert.NotEmpty(t, result.Digest)

		// Verify artifact exists in destination
		exists, err := dstCatalog.Exists(ctx, ref)
		require.NoError(t, err)
		assert.True(t, exists)

		// Verify content matches
		fetchedContent, _, err := dstCatalog.Fetch(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, content, fetchedContent)
	})

	t.Run("returns error when artifact already exists", func(t *testing.T) {
		srcCatalog := newTestCatalog(t)
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		_, err := srcCatalog.Store(ctx, ref, []byte("content"), nil, nil, false)
		require.NoError(t, err)

		tarPath := t.TempDir() + "/artifact.tar"
		_, err = srcCatalog.Save(ctx, "my-solution", "", tarPath)
		require.NoError(t, err)

		// Load into same catalog should fail
		_, err = srcCatalog.Load(ctx, tarPath, false)
		require.Error(t, err)
		assert.True(t, IsExists(err))
	})

	t.Run("overwrites with force", func(t *testing.T) {
		srcCatalog := newTestCatalog(t)
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		_, err := srcCatalog.Store(ctx, ref, []byte("content"), nil, nil, false)
		require.NoError(t, err)

		tarPath := t.TempDir() + "/artifact.tar"
		_, err = srcCatalog.Save(ctx, "my-solution", "", tarPath)
		require.NoError(t, err)

		// Load with force should succeed
		result, err := srcCatalog.Load(ctx, tarPath, true)
		require.NoError(t, err)
		assert.Equal(t, "my-solution", result.Reference.Name)
	})

	t.Run("returns error for invalid archive", func(t *testing.T) {
		catalog := newTestCatalog(t)

		// Create an invalid tar file
		invalidPath := t.TempDir() + "/invalid.tar"
		err := os.WriteFile(invalidPath, []byte("not a tar file"), 0o644)
		require.NoError(t, err)

		_, err = catalog.Load(ctx, invalidPath, false)
		require.Error(t, err)
	})
}

func TestLocalCatalog_SaveLoad_RoundTrip(t *testing.T) {
	ctx := context.Background()

	t.Run("full round-trip preserves content", func(t *testing.T) {
		// Create source catalog
		srcCatalog := newTestCatalog(t)

		// Store an artifact
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "round-trip-test",
			Version: semver.MustParse("2.5.0"),
		}
		originalContent := []byte("name: round-trip-test\nversion: 2.5.0\nkey: value")
		annotations := map[string]string{
			"custom-key": "custom-value",
		}
		_, err := srcCatalog.Store(ctx, ref, originalContent, nil, annotations, false)
		require.NoError(t, err)

		// Save to tar
		tarPath := t.TempDir() + "/round-trip.tar"
		saveResult, err := srcCatalog.Save(ctx, "round-trip-test", "2.5.0", tarPath)
		require.NoError(t, err)

		// Create destination catalog
		dstCatalog := newTestCatalog(t)

		// Load from tar
		loadResult, err := dstCatalog.Load(ctx, tarPath, false)
		require.NoError(t, err)

		// Verify metadata matches
		assert.Equal(t, saveResult.Reference.Name, loadResult.Reference.Name)
		assert.Equal(t, saveResult.Reference.Version.String(), loadResult.Reference.Version.String())
		assert.Equal(t, saveResult.Digest, loadResult.Digest)

		// Verify content matches
		fetchedContent, info, err := dstCatalog.Fetch(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, originalContent, fetchedContent)

		// Verify annotations are preserved
		assert.Equal(t, "custom-value", info.Annotations["custom-key"])
	})
}

func makeBundleTarForTest(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	manifest := map[string]any{
		"version": 1,
		"root":    ".",
		"files":   make([]map[string]any, 0, len(files)),
	}

	for relPath, content := range files {
		data := []byte(content)
		manifest["files"] = append(manifest["files"].([]map[string]any), map[string]any{
			"path":   filepath.ToSlash(relPath),
			"size":   len(data),
			"digest": digest.FromBytes(data).String(),
		})

		header := &tar.Header{
			Name: filepath.ToSlash(relPath),
			Mode: 0o644,
			Size: int64(len(data)),
		}
		require.NoError(t, tw.WriteHeader(header))
		_, err := tw.Write(data)
		require.NoError(t, err)
	}

	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)

	manifestHeader := &tar.Header{
		Name: ".scafctl/bundle-manifest.json",
		Mode: 0o644,
		Size: int64(len(manifestJSON)),
	}
	require.NoError(t, tw.WriteHeader(manifestHeader))
	_, err = tw.Write(manifestJSON)
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	return buf.Bytes()
}

func TestLocalCatalog_FetchWithBundle_PreservesBundledFiles_and_LockFile(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)

	expectedFiles := map[string]string{
		"config/app.yaml": "name: demo\nversion: 1.0.0\n",
		"scripts/run.sh":  "#!/bin/sh\necho hello\n",
	}
	bundleData := makeBundleTarForTest(t, expectedFiles)
	lockData := []byte("lockfile content")
	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "bundle-roundtrip",
		Version: semver.MustParse("1.2.3"),
	}
	content := []byte("name: bundle-roundtrip\nversion: 1.2.3\n")
	Layers := []Layer{
		{
			MediaType: MediaTypeSolutionBundle,
			Data:      bundleData,
		},
		{
			MediaType: MediaTypeSolutionLock,
			Data:      lockData,
		},
	}
	_, err := cat.Store(ctx, ref, content, bundleData, nil, false, Layers...)
	require.NoError(t, err)

	fetchedContent, fetchedLayers, info, err := cat.FetchWithLayer(ctx, ref, MediaTypeSolutionBundle, MediaTypeSolutionLock)
	require.NoError(t, err)
	assert.Equal(t, content, fetchedContent)
	assert.Len(t, fetchedLayers, 2)
	assert.NotEmpty(t, fetchedLayers[MediaTypeSolutionBundle])
	assert.Equal(t, lockData, fetchedLayers[MediaTypeSolutionLock])
	assert.Equal(t, ref.Name, info.Reference.Name)
	assert.Equal(t, ref.Version.String(), info.Reference.Version.String())

	tr := tar.NewReader(bytes.NewReader(fetchedLayers[MediaTypeSolutionBundle]))
	actualFiles := map[string]string{}
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if header.Name == ".scafctl/bundle-manifest.json" {
			continue
		}
		data, err := io.ReadAll(tr)
		require.NoError(t, err)
		actualFiles[header.Name] = string(data)
	}
	assert.Equal(t, expectedFiles, actualFiles)
}

func TestIsTarMediaType(t *testing.T) {
	assert.True(t, isTarMediaType(MediaTypeSolutionBundle))
	assert.True(t, isTarMediaType(MediaTypeSolutionBundleSmallTar))
	assert.False(t, isTarMediaType("application/json"))
	assert.False(t, isTarMediaType(""))
}

func TestNewLocalCatalog(t *testing.T) {
	// Override XDG path to use a temp dir
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	cat, err := NewLocalCatalog(logr.Discard())
	require.NoError(t, err)
	assert.NotNil(t, cat)
}

func TestLocalCatalog_TagForRef(t *testing.T) {
	cat := newTestCatalog(t)
	semVer := semver.MustParse("1.0.0")

	tests := []struct {
		name string
		ref  Reference
		want string
	}{
		{
			name: "name only",
			ref:  Reference{Kind: ArtifactKindSolution, Name: "my-sol"},
			want: "solution/my-sol",
		},
		{
			name: "with version",
			ref:  Reference{Kind: ArtifactKindSolution, Name: "my-sol", Version: semVer},
			want: "solution/my-sol:1.0.0",
		},
		{
			name: "version wins over digest",
			ref:  Reference{Kind: ArtifactKindSolution, Name: "my-sol", Version: semVer, Digest: "sha256:abc123"},
			want: "solution/my-sol:1.0.0",
		},
		{
			name: "with digest",
			ref:  Reference{Kind: ArtifactKindSolution, Name: "my-sol", Digest: "sha256:abc123"},
			want: "solution/my-sol@sha256:abc123",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cat.tagForRef(tt.ref)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLocalCatalog_Prune_Empty(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)

	result, err := cat.Prune(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, result.RemovedManifests)
	assert.Equal(t, int64(0), result.ReclaimedBytes)
}

func TestLocalCatalog_Prune_WithArtifacts(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)

	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "prune-sol",
		Version: semver.MustParse("1.0.0"),
	}
	_, err := cat.Store(ctx, ref, []byte("content"), nil, nil, false)
	require.NoError(t, err)

	// Prune with live artifacts should not remove anything
	result, err := cat.Prune(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, result.RemovedManifests)
}

func TestLocalCatalog_Prune_OrphanedBlob(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)

	// Store a solution so the catalog has a valid index.json
	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "keep-sol",
		Version: semver.MustParse("1.0.0"),
	}
	_, err := cat.Store(ctx, ref, []byte("content"), nil, nil, false)
	require.NoError(t, err)

	// Create an orphaned blob (not referenced by any manifest)
	blobsDir := cat.path + "/blobs/sha256"
	orphanPath := blobsDir + "/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	require.NoError(t, os.WriteFile(orphanPath, []byte("orphan"), 0o600))

	result, err := cat.Prune(ctx)
	require.NoError(t, err)
	// The orphaned blob should have been removed
	assert.Equal(t, 1, result.RemovedBlobs)
	assert.Greater(t, result.ReclaimedBytes, int64(0))
}

func TestLocalCatalog_Prune_OrphanedManifest(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)

	// Store a solution so catalog is initialized
	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "real-sol",
		Version: semver.MustParse("1.0.0"),
	}
	_, err := cat.Store(ctx, ref, []byte("content"), nil, nil, false)
	require.NoError(t, err)

	// Inject a fake orphaned manifest entry into index.json
	indexPath := cat.path + "/index.json"
	data, err := os.ReadFile(indexPath)
	require.NoError(t, err)

	var index ocispec.Index
	require.NoError(t, json.Unmarshal(data, &index))

	// Add a fake manifest entry that has no corresponding tag
	fakeDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	index.Manifests = append(index.Manifests, ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.Digest(fakeDigest),
		Size:      42,
	})
	updatedData, err := json.MarshalIndent(index, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(indexPath, updatedData, 0o600))

	result, err := cat.Prune(ctx)
	require.NoError(t, err)
	// The orphaned manifest entry should have been removed from index.json
	assert.Equal(t, 1, result.RemovedManifests)
}

func TestLocalCatalog_Fetch_MismatchedTag(t *testing.T) {
	// Simulates the embedder scenario where an artifact was pulled from a
	// remote registry with an empty Kind, producing a tag like "/name:1.0.0"
	// instead of "solution/name:1.0.0". Both Fetch and FetchWithBundle must
	// still resolve via annotations.
	ctx := context.Background()
	cat := newTestCatalog(t)

	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "starter-kit",
		Version: semver.MustParse("1.0.0"),
	}
	content := []byte("name: starter-kit\nversion: 1.0.0")
	bundle := []byte("fake-bundle-tar")

	// Store normally (creates tag "solution/starter-kit:1.0.0").
	_, err := cat.Store(ctx, ref, content, bundle, nil, false)
	require.NoError(t, err)

	// Store the embedder artifact with correct kind so annotations are right,
	// then manually re-tag with a wrong tag to simulate the CopyTo bug.
	embedRef := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "embedder-solution",
		Version: semver.MustParse("2.0.0"),
	}
	_, err = cat.Store(ctx, embedRef, []byte("name: embedder-solution"), nil, nil, false)
	require.NoError(t, err)

	// Re-tag with wrong format (empty kind prefix) and remove the correct tag.
	correctTag := "solution/embedder-solution:2.0.0"
	wrongTag := "/embedder-solution:2.0.0"
	desc, err := cat.store.Resolve(ctx, correctTag)
	require.NoError(t, err)
	require.NoError(t, cat.store.Tag(ctx, desc, wrongTag))
	require.NoError(t, cat.store.Untag(ctx, correctTag))

	// Fetch with the correct kind — the canonical tag "solution/embedder-solution:2.0.0"
	// won't exist, but the annotation-based fallback should find it.
	lookupRef := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "embedder-solution",
		Version: semver.MustParse("2.0.0"),
	}

	t.Run("Fetch resolves artifact with mismatched tag", func(t *testing.T) {
		fetched, info, err := cat.Fetch(ctx, lookupRef)
		require.NoError(t, err)
		assert.Equal(t, []byte("name: embedder-solution"), fetched)
		assert.Equal(t, "embedder-solution", info.Reference.Name)
	})

	t.Run("FetchWithBundle resolves artifact with mismatched tag", func(t *testing.T) {
		fetched, bundleData, info, err := cat.FetchWithBundle(ctx, lookupRef)
		require.NoError(t, err)
		assert.Equal(t, []byte("name: embedder-solution"), fetched)
		assert.Nil(t, bundleData) // no bundle stored
		assert.Equal(t, "embedder-solution", info.Reference.Name)
	})

	t.Run("Resolve falls back to listing for mismatched tag", func(t *testing.T) {
		info, err := cat.Resolve(ctx, lookupRef)
		require.NoError(t, err)
		assert.Equal(t, "embedder-solution", info.Reference.Name)
		assert.True(t, info.Reference.Version.Equal(semver.MustParse("2.0.0")))
	})

	// Verify normal path still works
	t.Run("Fetch still works for correctly tagged artifacts", func(t *testing.T) {
		fetched, info, err := cat.Fetch(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, content, fetched)
		assert.Equal(t, "starter-kit", info.Reference.Name)
	})
}

func TestLocalCatalog_Fetch_BareName_MismatchedTag(t *testing.T) {
	// Bare name (no version) should resolve via listing regardless of tag format.
	ctx := context.Background()
	cat := newTestCatalog(t)

	// Store with correct kind, then re-tag with wrong format
	correctRef := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "my-app",
		Version: semver.MustParse("3.0.0"),
	}
	_, err := cat.Store(ctx, correctRef, []byte("name: my-app"), nil, nil, false)
	require.NoError(t, err)

	// Re-tag with wrong format (empty kind prefix)
	correctTag := "solution/my-app:3.0.0"
	wrongTag := "/my-app:3.0.0"
	desc, err := cat.store.Resolve(ctx, correctTag)
	require.NoError(t, err)
	require.NoError(t, cat.store.Tag(ctx, desc, wrongTag))
	require.NoError(t, cat.store.Untag(ctx, correctTag))

	// Fetch by bare name with correct kind
	bareRef := Reference{
		Kind: ArtifactKindSolution,
		Name: "my-app",
	}
	fetched, info, err := cat.Fetch(ctx, bareRef)
	require.NoError(t, err)
	assert.Equal(t, []byte("name: my-app"), fetched)
	assert.Equal(t, "my-app", info.Reference.Name)
}

func TestLocalCatalog_Exists_MismatchedTag(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)

	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "exists-sol",
		Version: semver.MustParse("1.0.0"),
	}
	_, err := cat.Store(ctx, ref, []byte("data"), nil, nil, false)
	require.NoError(t, err)

	// Re-tag with wrong format
	desc, err := cat.store.Resolve(ctx, "solution/exists-sol:1.0.0")
	require.NoError(t, err)
	require.NoError(t, cat.store.Tag(ctx, desc, "/exists-sol:1.0.0"))
	require.NoError(t, cat.store.Untag(ctx, "solution/exists-sol:1.0.0"))

	exists, err := cat.Exists(ctx, ref)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestLocalCatalog_Delete_MismatchedTag(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)

	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "delete-sol",
		Version: semver.MustParse("1.0.0"),
	}
	_, err := cat.Store(ctx, ref, []byte("data"), nil, nil, false)
	require.NoError(t, err)

	// Re-tag with wrong format
	desc, err := cat.store.Resolve(ctx, "solution/delete-sol:1.0.0")
	require.NoError(t, err)
	require.NoError(t, cat.store.Tag(ctx, desc, "/delete-sol:1.0.0"))
	require.NoError(t, cat.store.Untag(ctx, "solution/delete-sol:1.0.0"))

	// Delete should still work via annotation fallback
	err = cat.Delete(ctx, ref)
	require.NoError(t, err)

	// Verify it's gone
	exists, err := cat.Exists(ctx, ref)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestLocalCatalog_Store_SetsBuiltOrigin(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cat := newTestCatalog(t)

	ref := Reference{Kind: ArtifactKindSolution, Name: "origin-test", Version: semver.MustParse("1.0.0")}
	info, err := cat.Store(ctx, ref, []byte("name: origin-test"), nil, nil, false)
	require.NoError(t, err)
	assert.Equal(t, "built", info.Annotations[AnnotationOrigin])

	// Resolve should also return the origin annotation.
	resolved, err := cat.Resolve(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, "built", resolved.Annotations[AnnotationOrigin])
}

func TestLocalCatalog_Store_PreservesCallerOrigin(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cat := newTestCatalog(t)

	ref := Reference{Kind: ArtifactKindSolution, Name: "cached-sol", Version: semver.MustParse("1.0.0")}
	annotations := map[string]string{
		AnnotationOrigin: "auto-cached from my-registry",
	}
	info, err := cat.Store(ctx, ref, []byte("name: cached-sol"), nil, annotations, false)
	require.NoError(t, err)
	assert.Equal(t, "auto-cached from my-registry", info.Annotations[AnnotationOrigin])
}

func TestLocalCatalog_List_IncludesOrigin(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cat := newTestCatalog(t)

	ref := Reference{Kind: ArtifactKindSolution, Name: "list-origin", Version: semver.MustParse("1.0.0")}
	_, err := cat.Store(ctx, ref, []byte("name: list-origin"), nil, nil, false)
	require.NoError(t, err)

	artifacts, err := cat.List(ctx, ArtifactKindSolution, "list-origin")
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, "built", artifacts[0].Annotations[AnnotationOrigin])
}

func TestLocalCatalog_List_ReferenceProvenance(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cat := newTestCatalog(t)

	built := Reference{Kind: ArtifactKindSolution, Name: "list-provenance", Version: semver.MustParse("1.0.0")}
	_, err := cat.Store(ctx, built, []byte("name: list-provenance"), nil, nil, false)
	require.NoError(t, err)
	pulled := Reference{Kind: ArtifactKindSolution, Name: "list-provenance", Version: semver.MustParse("2.0.0")}
	_, err = cat.Store(ctx, pulled, []byte("name: list-provenance"), nil,
		map[string]string{AnnotationSourceCanonical: "ghcr.io/org"}, false)
	require.NoError(t, err)

	artifacts, err := cat.List(ctx, ArtifactKindSolution, "list-provenance")
	require.NoError(t, err)
	require.Len(t, artifacts, 2)

	byVersion := make(map[string]Reference, len(artifacts))
	for _, a := range artifacts {
		byVersion[a.Reference.Version.String()] = a.Reference
	}
	assert.True(t, byVersion["1.0.0"].Built)
	assert.Empty(t, byVersion["1.0.0"].Remote)
	assert.False(t, byVersion["2.0.0"].Built, "a pulled copy must not be reported as built")
	assert.Equal(t, "ghcr.io/org", byVersion["2.0.0"].Remote)
}

func TestStore_ExcludesSourceCanonicalFromManifestBlob(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)

	ref := Reference{Kind: ArtifactKindSolution, Name: "canonical-test", Version: semver.MustParse("1.0.0")}
	annotations := map[string]string{
		AnnotationSourceCanonical: "ghcr.io/org/plugins",
	}

	info, err := cat.Store(ctx, ref, []byte("name: canonical-test"), nil, annotations, false)
	require.NoError(t, err)

	// The annotation should be accessible via the descriptor (index-level).
	assert.Equal(t, "ghcr.io/org/plugins", info.Annotations[AnnotationSourceCanonical])

	// Fetch the manifest blob directly and verify AnnotationSourceCanonical is NOT inside it.
	d, err := digest.Parse(info.Digest)
	require.NoError(t, err)

	desc := ocispec.Descriptor{Digest: d, MediaType: ocispec.MediaTypeImageManifest}
	blob, err := cat.fetchBlob(ctx, desc)
	require.NoError(t, err)

	var manifest ocispec.Manifest
	require.NoError(t, json.Unmarshal(blob, &manifest))

	_, found := manifest.Annotations[AnnotationSourceCanonical]
	assert.False(t, found, "AnnotationSourceCanonical must not appear in the manifest blob")
}

// newStoreInternalBuilder returns a fresh ManifestBuilder for ref carrying
// body as its single content layer, suitable for exercising storeInternal.
func newStoreInternalBuilder(ref Reference, body string) *ManifestBuilder {
	return NewManifestBuilder(ref).
		Layers(Layer{MediaType: MediaTypeSolutionContent, Data: []byte(body)})
}

func TestLocalCatalog_tagExists(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)

	_, err := cat.storeInternal(ctx, newStoreInternalBuilder(solutionRef("demo", "1.0.0"), "x"), false)
	require.NoError(t, err)

	t.Run("present tag", func(t *testing.T) {
		exists, err := cat.tagExists(ctx, "solution/demo:1.0.0")
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("absent tag is not an error", func(t *testing.T) {
		exists, err := cat.tagExists(ctx, "solution/missing:1.0.0")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("other resolve failures are surfaced", func(t *testing.T) {
		exists, err := cat.tagExists(ctx, "")
		require.Error(t, err, "a failure other than not-found must not read as a free tag")
		assert.False(t, exists)
		assert.Contains(t, err.Error(), "resolving tag")
	})
}

func TestLocalCatalog_StoreInternal(t *testing.T) {
	ctx := context.Background()

	t.Run("single-platform manifest is pushed and tagged", func(t *testing.T) {
		cat := newTestCatalog(t)

		ref := Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0")}
		builder := newStoreInternalBuilder(ref, "name: demo")

		desc, err := cat.storeInternal(ctx, builder, false)
		require.NoError(t, err)
		assert.Equal(t, ocispec.MediaTypeImageManifest, desc.MediaType)

		resolved, err := cat.store.Resolve(ctx, "solution/demo:1.0.0")
		require.NoError(t, err)
		assert.Equal(t, desc.Digest, resolved.Digest)
	})

	t.Run("multi-platform index is pushed and tagged with remote repo path", func(t *testing.T) {
		cat := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindProvider,
			Name:    "demo",
			Version: semver.MustParse("2.0.0"),
			Remote:  "example.com",
		}
		builder := NewIndexBuilder(ref).
			Platform("linux/amd64", platformManifestBuilder([]byte("amd64"))).
			Platform("darwin/arm64", platformManifestBuilder([]byte("arm64")))

		desc, err := cat.storeInternal(ctx, builder, false)
		require.NoError(t, err)
		assert.Equal(t, ocispec.MediaTypeImageIndex, desc.MediaType)
		assert.Equal(t, "example.com", desc.Annotations[AnnotationSourceCanonical])

		resolved, err := cat.store.Resolve(ctx, "example.com/providers/demo:2.0.0")
		require.NoError(t, err)
		assert.Equal(t, desc.Digest, resolved.Digest)
	})

	t.Run("nil builder is rejected", func(t *testing.T) {
		cat := newTestCatalog(t)

		_, err := cat.storeInternal(ctx, nil, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil builder")
	})

	t.Run("reference without version or digest is rejected before push", func(t *testing.T) {
		cat := newTestCatalog(t)

		builder := newStoreInternalBuilder(Reference{Kind: ArtifactKindSolution, Name: "demo"}, "x")

		_, err := cat.storeInternal(ctx, builder, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no version or digest")
	})

	t.Run("reference without kind or name is rejected before push", func(t *testing.T) {
		cat := newTestCatalog(t)

		builder := newStoreInternalBuilder(Reference{Name: "demo", Version: semver.MustParse("1.0.0")}, "x")

		_, err := cat.storeInternal(ctx, builder, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "kind and name")
	})

	t.Run("existing artifact is rejected without force", func(t *testing.T) {
		cat := newTestCatalog(t)

		ref := Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0")}

		_, err := cat.storeInternal(ctx, newStoreInternalBuilder(ref, "name: demo"), false)
		require.NoError(t, err)

		_, err = cat.storeInternal(ctx, newStoreInternalBuilder(ref, "name: demo v2"), false)
		require.Error(t, err)
		var existsErr *ArtifactExistsError
		require.ErrorAs(t, err, &existsErr)
		assert.True(t, IsExists(err))
	})

	t.Run("force overwrites an existing artifact", func(t *testing.T) {
		cat := newTestCatalog(t)

		ref := Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0")}

		first, err := cat.storeInternal(ctx, newStoreInternalBuilder(ref, "name: demo"), false)
		require.NoError(t, err)

		second, err := cat.storeInternal(ctx, newStoreInternalBuilder(ref, "name: demo v2"), true)
		require.NoError(t, err)
		assert.NotEqual(t, first.Digest, second.Digest)

		resolved, err := cat.store.Resolve(ctx, "solution/demo:1.0.0")
		require.NoError(t, err)
		assert.Equal(t, second.Digest, resolved.Digest)
	})

	t.Run("builder push failure is wrapped", func(t *testing.T) {
		cat := newTestCatalog(t)

		ref := Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0")}
		// An IndexBuilder with no platforms fails to build, surfacing a Push error.
		builder := NewIndexBuilder(ref)

		_, err := cat.storeInternal(ctx, builder, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pushing artifact")
	})

	t.Run("stored artifact carries reference annotations", func(t *testing.T) {
		cat := newTestCatalog(t)

		ref := Reference{Kind: ArtifactKindProvider, Name: "demo", Version: semver.MustParse("1.0.0")}
		desc, err := cat.storeInternal(ctx, newStoreInternalBuilder(ref, "name: demo"), false)
		require.NoError(t, err)

		tagged, err := cat.store.Resolve(ctx, "provider/demo:1.0.0")
		require.NoError(t, err)
		assert.Equal(t, tagged.Annotations, desc.Annotations,
			"storeInternal returns the descriptor as tagged")

		assert.Equal(t, "demo", tagged.Annotations[AnnotationArtifactName])
		assert.Equal(t, ArtifactKindProvider.String(), tagged.Annotations[AnnotationArtifactType])
		assert.Equal(t, "1.0.0", tagged.Annotations[AnnotationVersion])
		_, hasCanonical := tagged.Annotations[AnnotationSourceCanonical]
		assert.False(t, hasCanonical, "built artifact must not carry AnnotationSourceCanonical")
	})

	t.Run("remote reference carries AnnotationSourceCanonical only on the descriptor", func(t *testing.T) {
		cat := newTestCatalog(t)

		ref := Reference{Kind: ArtifactKindProvider, Name: "demo", Version: semver.MustParse("1.0.0"), Remote: "example.com"}
		_, err := cat.storeInternal(ctx, newStoreInternalBuilder(ref, "name: demo"), false)
		require.NoError(t, err)

		tagged, err := cat.store.Resolve(ctx, "example.com/providers/demo:1.0.0")
		require.NoError(t, err)
		assert.Equal(t, "example.com", tagged.Annotations[AnnotationSourceCanonical])

		blob, err := cat.fetchBlob(ctx, tagged)
		require.NoError(t, err)
		var manifest ocispec.Manifest
		require.NoError(t, json.Unmarshal(blob, &manifest))
		assert.NotContains(t, manifest.Annotations, AnnotationSourceCanonical,
			"provenance must not enter the manifest blob")
	})
}

func putSolutionLocal(t *testing.T, cat *LocalCatalog, ref Reference, _ bool) {
	t.Helper()
	_, err := cat.storeArtifact(context.Background(), remoteSolutionBuilder(ref), false)
	require.NoError(t, err)
}

func TestLocalCatalog_Find(t *testing.T) {
	ctx := context.Background()
	built := func(name, version string) Reference {
		return Reference{Kind: ArtifactKindSolution, Name: name, Version: semver.MustParse(version), Built: true}
	}
	remote := func(name, version, origin string) Reference {
		return Reference{Kind: ArtifactKindSolution, Name: name, Version: semver.MustParse(version), Remote: origin}
	}

	t.Run("latest built version wins when source pinned", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("demo", "1.0.0"), false)
		putSolutionLocal(t, cat, built("demo", "2.0.0"), false)

		got, err := cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Source: SourceSelector{Kind: SourceBuilt}})
		require.NoError(t, err)
		assert.Equal(t, "2.0.0", got.Reference.Version.String())
		assert.True(t, got.Reference.Built)
	})

	t.Run("single origin different versions picks latest without a selector", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("demo", "1.0.0", "ghcr.io/a"), false)
		putSolutionLocal(t, cat, remote("demo", "2.0.0", "ghcr.io/a"), false)

		got, err := cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution})
		require.NoError(t, err)
		assert.Equal(t, "2.0.0", got.Reference.Version.String())
		assert.Equal(t, "ghcr.io/a", got.Reference.Remote)
		assert.False(t, got.Reference.Built)
	})

	t.Run("built and remote same version is ambiguous", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("demo", "1.0.0"), false)
		putSolutionLocal(t, cat, remote("demo", "1.0.0", "ghcr.io/a"), false)

		_, err := cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Version: "1.0.0"})
		var ambErr *AmbiguousArtifactError
		require.ErrorAs(t, err, &ambErr)
	})

	t.Run("two remotes same version resolves when narrowed", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("demo", "1.0.0", "ghcr.io/a"), false)
		putSolutionLocal(t, cat, remote("demo", "1.0.0", "ghcr.io/b"), false)

		_, err := cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Version: "1.0.0"})
		var ambErr *AmbiguousArtifactError
		require.ErrorAs(t, err, &ambErr)

		got, err := cat.Find(ctx, ArtifactQuery{
			Name: "demo", Kind: ArtifactKindSolution, Version: "1.0.0",
			Source: SourceSelector{Kind: SourceRemote, Remote: "ghcr.io/b"},
		})
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/b", got.Reference.Remote)
	})

	t.Run("source built excludes remote artifact", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("demo", "1.0.0", "ghcr.io/a"), false)

		_, err := cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Source: SourceSelector{Kind: SourceBuilt}})
		var nfErr *ArtifactNotFoundError
		require.ErrorAs(t, err, &nfErr)
	})

	t.Run("not found", func(t *testing.T) {
		cat := newTestCatalog(t)
		_, err := cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution})
		var nfErr *ArtifactNotFoundError
		require.ErrorAs(t, err, &nfErr)
	})

	t.Run("invalid query without kind", func(t *testing.T) {
		cat := newTestCatalog(t)
		_, err := cat.Find(ctx, ArtifactQuery{Name: "demo"})
		var iqErr *InvalidQueryError
		require.ErrorAs(t, err, &iqErr)
	})

	t.Run("latest prefers stable over a newer pre-release", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("demo", "1.0.0", "ghcr.io/a"), false)
		putSolutionLocal(t, cat, remote("demo", "2.0.0-rc.1", "ghcr.io/a"), false)

		got, err := cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution})
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", got.Reference.Version.String())

		got, err = cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Version: "latest"})
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", got.Reference.Version.String())
	})

	t.Run("latest falls back to pre-release when no stable exists", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("demo", "2.0.0-rc.1", "ghcr.io/a"), false)
		putSolutionLocal(t, cat, remote("demo", "2.0.0-rc.2", "ghcr.io/a"), false)

		got, err := cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution})
		require.NoError(t, err)
		assert.Equal(t, "2.0.0-rc.2", got.Reference.Version.String())
	})

	t.Run("include pre-release context picks the newest pre-release", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("demo", "1.0.0", "ghcr.io/a"), false)
		putSolutionLocal(t, cat, remote("demo", "2.0.0-rc.1", "ghcr.io/a"), false)

		got, err := cat.Find(WithIncludePreRelease(ctx), ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution})
		require.NoError(t, err)
		assert.Equal(t, "2.0.0-rc.1", got.Reference.Version.String())
	})

	t.Run("exact pre-release version is not filtered", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("demo", "1.0.0", "ghcr.io/a"), false)
		putSolutionLocal(t, cat, remote("demo", "2.0.0-rc.1", "ghcr.io/a"), false)

		got, err := cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Version: "2.0.0-rc.1"})
		require.NoError(t, err)
		assert.Equal(t, "2.0.0-rc.1", got.Reference.Version.String())
	})

	t.Run("built stable and remote pre-release is ambiguous", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("demo", "1.0.0"), false)
		putSolutionLocal(t, cat, remote("demo", "2.0.0-rc.1", "ghcr.io/a"), false)

		_, err := cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution})
		var ambErr *AmbiguousArtifactError
		require.ErrorAs(t, err, &ambErr)
		assert.Len(t, ambErr.Matches, 2, "ambiguity is reported against every match, not the stable subset")
	})

	t.Run("built pre-release and remote stable is ambiguous", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("demo", "2.0.0-rc.1"), false)
		putSolutionLocal(t, cat, remote("demo", "1.0.0", "ghcr.io/a"), false)

		_, err := cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution})
		var ambErr *AmbiguousArtifactError
		require.ErrorAs(t, err, &ambErr)
		assert.Equal(t, "2.0.0-rc.1", ambErr.Matches[0].Reference.Version.String(), "matches are ordered newest first")
	})

	t.Run("remotes where one has only pre-releases are ambiguous", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("demo", "1.0.0", "ghcr.io/a"), false)
		putSolutionLocal(t, cat, remote("demo", "2.0.0-rc.1", "ghcr.io/b"), false)

		_, err := cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Version: "latest"})
		var ambErr *AmbiguousArtifactError
		require.ErrorAs(t, err, &ambErr)
	})

	t.Run("source selector applies stable-first within the selected origin", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("demo", "2.0.0-rc.1"), false)
		putSolutionLocal(t, cat, remote("demo", "1.0.0", "ghcr.io/a"), false)

		got, err := cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Source: SourceSelector{Kind: SourceBuilt}})
		require.NoError(t, err)
		assert.True(t, got.Reference.Built)
		assert.Equal(t, "2.0.0-rc.1", got.Reference.Version.String())

		got, err = cat.Find(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Source: SourceSelector{Kind: SourceRemote, Remote: "ghcr.io/a"}})
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/a", got.Reference.Remote)
		assert.Equal(t, "1.0.0", got.Reference.Version.String())
	})
}

func TestLocalCatalog_FindStrict(t *testing.T) {
	ctx := context.Background()
	built := func(name, version string) Reference {
		return Reference{Kind: ArtifactKindSolution, Name: name, Version: semver.MustParse(version), Built: true}
	}
	remote := func(name, version, origin string) Reference {
		return Reference{Kind: ArtifactKindSolution, Name: name, Version: semver.MustParse(version), Remote: origin}
	}

	t.Run("returns a single match", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("demo", "1.0.0"), false)

		got, err := cat.FindStrict(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution})
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", got.Reference.Version.String())
		assert.True(t, got.Reference.Built)
	})

	t.Run("rejects multiple matches even when source is pinned", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("demo", "1.0.0"), false)
		putSolutionLocal(t, cat, built("demo", "2.0.0"), false)

		_, err := cat.FindStrict(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Source: SourceSelector{Kind: SourceBuilt}})
		var ambErr *AmbiguousArtifactError
		require.ErrorAs(t, err, &ambErr)
		assert.Len(t, ambErr.Matches, 2)
	})

	t.Run("does not filter pre-releases", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("demo", "1.0.0", "ghcr.io/a"), false)
		putSolutionLocal(t, cat, remote("demo", "2.0.0-rc.1", "ghcr.io/a"), false)

		_, err := cat.FindStrict(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution})
		var ambErr *AmbiguousArtifactError
		require.ErrorAs(t, err, &ambErr)
		assert.Len(t, ambErr.Matches, 2)
	})

	t.Run("rejects multiple origins", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("demo", "1.0.0"), false)
		putSolutionLocal(t, cat, remote("demo", "1.0.0", "ghcr.io/a"), false)

		_, err := cat.FindStrict(ctx, ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Version: "1.0.0"})
		var ambErr *AmbiguousArtifactError
		require.ErrorAs(t, err, &ambErr)
		assert.Len(t, ambErr.Matches, 2)
	})
}

func TestLocalCatalog_resolveLocked(t *testing.T) {
	ctx := context.Background()
	built := func(version string) Reference {
		return Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse(version), Built: true}
	}
	remote := func(version, origin string) Reference {
		return Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse(version), Remote: origin}
	}
	anyOrigin := func(version string) Reference {
		return Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse(version)}
	}
	// retag moves an artifact to a tag that no longer matches its annotations.
	retag := func(t *testing.T, cat *LocalCatalog, from, to string) {
		t.Helper()
		desc, err := cat.store.Resolve(ctx, from)
		require.NoError(t, err)
		require.NoError(t, cat.store.Tag(ctx, desc, to))
		require.NoError(t, cat.store.Untag(ctx, from))
	}
	requireNotFound := func(t *testing.T, err error) {
		t.Helper()
		var nfErr *ArtifactNotFoundError
		require.ErrorAs(t, err, &nfErr)
	}

	t.Run("full identity built ref resolves by tag", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)

		got, err := cat.Resolve(ctx, built("1.0.0"))
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", got.Reference.Version.String())
		assert.True(t, got.Reference.Built)
		assert.Equal(t, "1.0.0", got.Tag)
	})

	t.Run("full identity remote ref resolves by tag", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("1.0.0", "ghcr.io/a"), false)

		got, err := cat.Resolve(ctx, remote("1.0.0", "ghcr.io/a"))
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/a", got.Reference.Remote)
		assert.False(t, got.Reference.Built)
	})

	t.Run("remote ref does not return the built copy", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)

		_, err := cat.Resolve(ctx, remote("1.0.0", "ghcr.io/a"))
		requireNotFound(t, err)
	})

	t.Run("built ref does not return the remote copy", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("1.0.0", "ghcr.io/a"), false)

		_, err := cat.Resolve(ctx, built("1.0.0"))
		requireNotFound(t, err)
	})

	t.Run("different remote origin is not found", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("1.0.0", "ghcr.io/a"), false)

		_, err := cat.Resolve(ctx, remote("1.0.0", "ghcr.io/b"))
		requireNotFound(t, err)
	})

	t.Run("origin-less ref resolves a built-only artifact", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)

		got, err := cat.Resolve(ctx, anyOrigin("1.0.0"))
		require.NoError(t, err)
		assert.True(t, got.Reference.Built)
	})

	t.Run("origin-less ref resolves a remote-only artifact", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("1.0.0", "ghcr.io/a"), false)

		got, err := cat.Resolve(ctx, anyOrigin("1.0.0"))
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/a", got.Reference.Remote)
	})

	t.Run("origin-less ref is ambiguous across built and remote copies", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)
		putSolutionLocal(t, cat, remote("1.0.0", "ghcr.io/a"), false)

		_, err := cat.Resolve(ctx, anyOrigin("1.0.0"))
		var ambErr *AmbiguousArtifactError
		require.ErrorAs(t, err, &ambErr)

		// An explicit origin disambiguates.
		got, err := cat.Resolve(ctx, built("1.0.0"))
		require.NoError(t, err)
		assert.True(t, got.Reference.Built)
	})

	t.Run("full identity tag miss is not found without scanning", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)
		retag(t, cat, "solution/demo:1.0.0", "/demo:1.0.0")

		_, err := cat.Resolve(ctx, built("1.0.0"))
		requireNotFound(t, err)

		// The same artifact is still reachable when the origin is left open.
		got, err := cat.Resolve(ctx, anyOrigin("1.0.0"))
		require.NoError(t, err)
		assert.Equal(t, "demo", got.Reference.Name)
	})

	t.Run("bare name resolves latest version", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)
		putSolutionLocal(t, cat, built("2.0.0"), false)

		got, err := cat.Resolve(ctx, Reference{Kind: ArtifactKindSolution, Name: "demo", Built: true})
		require.NoError(t, err)
		assert.Equal(t, "2.0.0", got.Reference.Version.String())
	})

	t.Run("latest across a built release and a remote pre-release is ambiguous", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)
		putSolutionLocal(t, cat, remote("2.0.0-rc.1", "ghcr.io/a"), false)

		_, err := cat.Resolve(ctx, Reference{Kind: ArtifactKindSolution, Name: "demo"})
		var ambErr *AmbiguousArtifactError
		require.ErrorAs(t, err, &ambErr)

		got, err := cat.Resolve(ctx, Reference{Kind: ArtifactKindSolution, Name: "demo", Built: true})
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", got.Reference.Version.String())
		assert.True(t, got.Reference.Built)
	})

	t.Run("exact pre-release version resolves", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("2.0.0-rc.1"), false)

		got, err := cat.Resolve(ctx, built("2.0.0-rc.1"))
		require.NoError(t, err)
		assert.Equal(t, "2.0.0-rc.1", got.Reference.Version.String())
	})

	t.Run("digest-only ref resolves by annotation search", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)
		stored, err := cat.Resolve(ctx, built("1.0.0"))
		require.NoError(t, err)

		got, err := cat.Resolve(ctx, Reference{Kind: ArtifactKindSolution, Name: "demo", Digest: stored.Digest})
		require.NoError(t, err)
		assert.Equal(t, stored.Digest, got.Digest)
		assert.Equal(t, "1.0.0", got.Reference.Version.String())
	})

	t.Run("unknown digest is not found", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)

		_, err := cat.Resolve(ctx, Reference{Kind: ArtifactKindSolution, Name: "demo", Digest: digest.FromString("absent").String()})
		requireNotFound(t, err)
	})

	t.Run("version and matching digest resolves", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)
		stored, err := cat.Resolve(ctx, built("1.0.0"))
		require.NoError(t, err)

		ref := built("1.0.0")
		ref.Digest = stored.Digest
		got, err := cat.Resolve(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, stored.Digest, got.Digest)
	})

	t.Run("version and mismatched digest is not found", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)

		ref := built("1.0.0")
		ref.Digest = digest.FromString("absent").String()
		_, err := cat.Resolve(ctx, ref)
		requireNotFound(t, err)
	})

	t.Run("version and digest resolves despite a mismatched tag", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)
		stored, err := cat.Resolve(ctx, built("1.0.0"))
		require.NoError(t, err)
		retag(t, cat, "solution/demo:1.0.0", "/demo:1.0.0")

		ref := built("1.0.0")
		ref.Digest = stored.Digest
		got, err := cat.Resolve(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, stored.Digest, got.Digest)
	})

	t.Run("ref without kind is an invalid query", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)

		_, err := cat.Resolve(ctx, Reference{Name: "demo", Version: semver.MustParse("1.0.0"), Built: true})
		var qErr *InvalidQueryError
		require.ErrorAs(t, err, &qErr)
	})

	t.Run("miss reports the caller's reference", func(t *testing.T) {
		cat := newTestCatalog(t)

		ref := Reference{Kind: ArtifactKindSolution, Name: "demo"}
		_, err := cat.Resolve(ctx, ref)
		var nfErr *ArtifactNotFoundError
		require.ErrorAs(t, err, &nfErr)
		assert.Equal(t, ref, nfErr.Reference)
		assert.Equal(t, LocalCatalogName, nfErr.Catalog)
	})

	t.Run("origin-less resolve matches find on the equivalent query", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("1.0.0", "ghcr.io/a"), false)
		putSolutionLocal(t, cat, remote("2.0.0", "ghcr.io/a"), false)

		ref := Reference{Kind: ArtifactKindSolution, Name: "demo"}
		resolved, err := cat.Resolve(ctx, ref)
		require.NoError(t, err)
		found, err := cat.Find(ctx, queryFromRef(ref))
		require.NoError(t, err)
		assert.Equal(t, found.Digest, resolved.Digest)
		assert.Equal(t, found.Tag, resolved.Tag)
	})
}

func TestLocalCatalog_existsLocked(t *testing.T) {
	ctx := context.Background()
	built := func(version string) Reference {
		return Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse(version), Built: true}
	}
	remote := func(version, origin string) Reference {
		return Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse(version), Remote: origin}
	}
	anyOrigin := func(version string) Reference {
		return Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse(version)}
	}
	exists := func(t *testing.T, cat *LocalCatalog, ref Reference) bool {
		t.Helper()
		got, err := cat.Exists(ctx, ref)
		require.NoError(t, err)
		return got
	}

	t.Run("full identity is an exact tag lookup", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)
		putSolutionLocal(t, cat, remote("1.0.0", "ghcr.io/a"), false)

		assert.True(t, exists(t, cat, built("1.0.0")))
		assert.True(t, exists(t, cat, remote("1.0.0", "ghcr.io/a")))
		assert.False(t, exists(t, cat, built("2.0.0")), "other version")
		assert.False(t, exists(t, cat, remote("1.0.0", "ghcr.io/b")), "other origin")
	})

	t.Run("a built ref is not satisfied by a remote copy", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, remote("1.0.0", "ghcr.io/a"), false)

		assert.False(t, exists(t, cat, built("1.0.0")))
	})

	t.Run("a remote ref is not satisfied by the built copy", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)

		assert.False(t, exists(t, cat, remote("1.0.0", "ghcr.io/a")))
	})

	t.Run("full identity miss does not scan annotations", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)
		desc, err := cat.store.Resolve(ctx, "solution/demo:1.0.0")
		require.NoError(t, err)
		require.NoError(t, cat.store.Tag(ctx, desc, "/demo:1.0.0"))
		require.NoError(t, cat.store.Untag(ctx, "solution/demo:1.0.0"))

		assert.False(t, exists(t, cat, built("1.0.0")), "tag is the contract for a full identity")
		assert.True(t, exists(t, cat, anyOrigin("1.0.0")), "an open origin is a search and finds it by annotation")
	})

	t.Run("open origin matches any origin", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)
		putSolutionLocal(t, cat, remote("2.0.0", "ghcr.io/a"), false)

		assert.True(t, exists(t, cat, anyOrigin("1.0.0")), "built copy")
		assert.True(t, exists(t, cat, anyOrigin("2.0.0")), "remote copy")
		assert.False(t, exists(t, cat, anyOrigin("3.0.0")))
	})

	t.Run("ambiguous match still exists", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)
		putSolutionLocal(t, cat, remote("1.0.0", "ghcr.io/a"), false)

		assert.True(t, exists(t, cat, anyOrigin("1.0.0")))
	})

	t.Run("open version matches any version", func(t *testing.T) {
		cat := newTestCatalog(t)
		versionless := Reference{Kind: ArtifactKindSolution, Name: "demo"}
		assert.False(t, exists(t, cat, versionless), "empty catalog")

		putSolutionLocal(t, cat, built("1.0.0"), false)
		assert.True(t, exists(t, cat, versionless))
		assert.True(t, exists(t, cat, Reference{Kind: ArtifactKindSolution, Name: "demo", Built: true}))
		assert.False(t, exists(t, cat, Reference{Kind: ArtifactKindSolution, Name: "demo", Remote: "ghcr.io/a"}))
		assert.False(t, exists(t, cat, Reference{Kind: ArtifactKindProvider, Name: "demo"}), "other kind")
	})

	t.Run("digest is checked against the stored artifact", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)
		stored, err := cat.Resolve(ctx, built("1.0.0"))
		require.NoError(t, err)

		withDigest := built("1.0.0")
		withDigest.Digest = stored.Digest
		assert.True(t, exists(t, cat, withDigest))

		wrongDigest := built("1.0.0")
		wrongDigest.Digest = "sha256:" + strings.Repeat("0", 64)
		assert.False(t, exists(t, cat, wrongDigest), "the version tag exists but holds a different digest")
	})

	t.Run("invalid query is an error not a miss", func(t *testing.T) {
		cat := newTestCatalog(t)

		_, err := cat.Exists(ctx, Reference{Name: "demo"})
		var invalid *InvalidQueryError
		require.ErrorAs(t, err, &invalid)
	})

	t.Run("deleted artifact no longer exists", func(t *testing.T) {
		cat := newTestCatalog(t)
		putSolutionLocal(t, cat, built("1.0.0"), false)
		require.NoError(t, cat.Delete(ctx, built("1.0.0")))

		assert.False(t, exists(t, cat, built("1.0.0")))
	})
}

func TestLocalCatalog_Store_RejectsInvalidRefOnGuard(t *testing.T) {
	cat := newTestCatalog(t)

	_, err := cat.Store(context.Background(), Reference{Name: "demo", Version: semver.MustParse("1.0.0")}, []byte("x"), nil, nil, false)
	var invalid *InvalidQueryError
	require.ErrorAs(t, err, &invalid)
}

// An origin-less write is stored as built, so its existence guard must check
// the built tag only: a pulled copy of the same name@version lives under its
// own tag and must not block the build, while a prior build still must.
func TestLocalCatalog_StoreOriginlessIgnoresPulledCopy(t *testing.T) {
	ctx := context.Background()
	const pulledOrigin = "ghcr.io/org"

	tests := []struct {
		name  string
		kind  ArtifactKind
		store func(cat *LocalCatalog, ref Reference) (ArtifactInfo, error)
	}{
		{
			name: "Store",
			kind: ArtifactKindProvider,
			store: func(cat *LocalCatalog, ref Reference) (ArtifactInfo, error) {
				return cat.Store(ctx, ref, []byte("x"), nil, nil, false)
			},
		},
		{
			name: "StoreDedup",
			kind: ArtifactKindSolution,
			store: func(cat *LocalCatalog, ref Reference) (ArtifactInfo, error) {
				return cat.StoreDedup(ctx, ref, []byte("sol"), []byte("{}"), nil, nil, nil, false)
			},
		},
		{
			name: "StoreMultiPlatform",
			kind: ArtifactKindProvider,
			store: func(cat *LocalCatalog, ref Reference) (ArtifactInfo, error) {
				return cat.StoreMultiPlatform(ctx, ref, []PlatformBinary{{Platform: "linux/amd64", Data: []byte("bin")}}, nil, false)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat := newTestCatalog(t)
			pulled := Reference{Kind: tt.kind, Name: "foo", Version: semver.MustParse("1.0.0"), Remote: pulledOrigin}
			_, err := tt.store(cat, pulled)
			require.NoError(t, err)

			originless := Reference{Kind: tt.kind, Name: "foo", Version: semver.MustParse("1.0.0")}
			info, err := tt.store(cat, originless)
			require.NoError(t, err, "a pulled copy must not block an origin-less (built) write")
			assert.True(t, info.Reference.Built)

			builtExists, err := cat.Exists(ctx, Reference{Kind: tt.kind, Name: "foo", Version: semver.MustParse("1.0.0"), Built: true})
			require.NoError(t, err)
			assert.True(t, builtExists)
			pulledExists, err := cat.Exists(ctx, pulled)
			require.NoError(t, err)
			assert.True(t, pulledExists, "the pulled copy must be kept")

			_, err = tt.store(cat, originless)
			var existsErr *ArtifactExistsError
			require.ErrorAs(t, err, &existsErr, "a prior build must still block the write")
		})
	}
}
