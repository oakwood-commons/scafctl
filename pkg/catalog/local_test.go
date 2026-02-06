package catalog

import (
	"context"
	"os"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
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

		info, err := catalog.Store(ctx, ref, content, annotations, false)
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
		_, err := catalog.Store(ctx, ref, content, nil, false)
		require.NoError(t, err)

		// Store again without force should fail
		_, err = catalog.Store(ctx, ref, content, nil, false)
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
		_, err := catalog.Store(ctx, ref, content1, nil, false)
		require.NoError(t, err)

		// Store again with force should succeed
		info, err := catalog.Store(ctx, ref, content2, nil, true)
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

		_, err := catalog.Store(ctx, ref, content, nil, false)
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
		_, err := catalog.Store(ctx, ref, []byte("v1.0.0"), nil, false)
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
			_, err := catalog.Store(ctx, ref, []byte("version "+v), nil, false)
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
			_, err := catalog.Store(ctx, ref, []byte("content"), nil, false)
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
		_, err := catalog.Store(ctx, solRef, []byte("solution"), nil, false)
		require.NoError(t, err)

		// Store plugin
		pluginRef := Reference{
			Kind:    ArtifactKindPlugin,
			Name:    "my-plugin",
			Version: semver.MustParse("1.0.0"),
		}
		_, err = catalog.Store(ctx, pluginRef, []byte("plugin"), nil, false)
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
			_, err := catalog.Store(ctx, ref, []byte("content"), nil, false)
			require.NoError(t, err)
		}

		// Store another solution
		otherRef := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "other-solution",
			Version: semver.MustParse("1.0.0"),
		}
		_, err := catalog.Store(ctx, otherRef, []byte("content"), nil, false)
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
		_, err := catalog.Store(ctx, ref, []byte("content"), nil, false)
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
		_, err := catalog.Store(ctx, ref, []byte("content"), nil, false)
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
