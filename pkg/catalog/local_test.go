// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/json"
	"os"
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

func TestLocalCatalog_Tag(t *testing.T) {
	ctx := context.Background()

	t.Run("tags artifact with alias", func(t *testing.T) {
		cat := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		_, err := cat.Store(ctx, ref, []byte("content"), nil, nil, false)
		require.NoError(t, err)

		// Tag as stable
		_, err = cat.Tag(ctx, ref, "stable")
		require.NoError(t, err)

		// The alias should resolve to the same digest as the original
		origTag := "solution/my-solution:1.0.0"
		aliasTag := "solution/my-solution:stable"
		origDesc, err := cat.store.Resolve(ctx, origTag)
		require.NoError(t, err)
		aliasDesc, err := cat.store.Resolve(ctx, aliasTag)
		require.NoError(t, err)
		assert.Equal(t, origDesc.Digest, aliasDesc.Digest)
	})

	t.Run("tags with multiple aliases", func(t *testing.T) {
		cat := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("2.0.0"),
		}
		_, err := cat.Store(ctx, ref, []byte("content-v2"), nil, nil, false)
		require.NoError(t, err)

		// Tag with multiple aliases
		_, err = cat.Tag(ctx, ref, "dev")
		require.NoError(t, err)
		_, err = cat.Tag(ctx, ref, "production")
		require.NoError(t, err)

		// Both should resolve
		devDesc, err := cat.store.Resolve(ctx, "solution/my-solution:dev")
		require.NoError(t, err)
		prodDesc, err := cat.store.Resolve(ctx, "solution/my-solution:production")
		require.NoError(t, err)
		assert.Equal(t, devDesc.Digest, prodDesc.Digest)
	})

	t.Run("moves alias to new version", func(t *testing.T) {
		cat := newTestCatalog(t)

		// Store v1.0.0
		ref1 := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
		}
		_, err := cat.Store(ctx, ref1, []byte("v1-content"), nil, nil, false)
		require.NoError(t, err)
		_, err = cat.Tag(ctx, ref1, "stable")
		require.NoError(t, err)

		// Store v2.0.0
		ref2 := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("2.0.0"),
		}
		_, err = cat.Store(ctx, ref2, []byte("v2-content"), nil, nil, false)
		require.NoError(t, err)

		// Re-tag stable to v2
		oldVersion, err := cat.Tag(ctx, ref2, "stable")
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", oldVersion)

		// Stable should now point to v2's digest
		v2Desc, err := cat.store.Resolve(ctx, "solution/my-solution:2.0.0")
		require.NoError(t, err)
		stableDesc, err := cat.store.Resolve(ctx, "solution/my-solution:stable")
		require.NoError(t, err)
		assert.Equal(t, v2Desc.Digest, stableDesc.Digest)
	})

	t.Run("returns error for non-existent artifact", func(t *testing.T) {
		cat := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "non-existent",
			Version: semver.MustParse("1.0.0"),
		}

		_, err := cat.Tag(ctx, ref, "stable")
		require.Error(t, err)
		assert.True(t, IsNotFound(err))
	})

	t.Run("tags provider artifact", func(t *testing.T) {
		cat := newTestCatalog(t)

		ref := Reference{
			Kind:    ArtifactKindProvider,
			Name:    "echo",
			Version: semver.MustParse("1.0.0"),
		}
		_, err := cat.Store(ctx, ref, []byte("binary"), nil, nil, false)
		require.NoError(t, err)

		_, err = cat.Tag(ctx, ref, "stable")
		require.NoError(t, err)

		// Verify
		aliasDesc, err := cat.store.Resolve(ctx, "provider/echo:stable")
		require.NoError(t, err)
		origDesc, err := cat.store.Resolve(ctx, "provider/echo:1.0.0")
		require.NoError(t, err)
		assert.Equal(t, origDesc.Digest, aliasDesc.Digest)
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
