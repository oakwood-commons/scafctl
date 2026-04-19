// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"context"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCatalog implements catalog.Catalog for testing NextPatchVersion.
type mockCatalog struct {
	artifacts []catalog.ArtifactInfo
	listErr   error
}

func (m *mockCatalog) Name() string { return "mock" }
func (m *mockCatalog) Store(_ context.Context, _ catalog.Reference, _, _ []byte, _ map[string]string, _ bool) (catalog.ArtifactInfo, error) {
	return catalog.ArtifactInfo{}, nil
}

func (m *mockCatalog) Fetch(_ context.Context, _ catalog.Reference) ([]byte, catalog.ArtifactInfo, error) {
	return nil, catalog.ArtifactInfo{}, nil
}

func (m *mockCatalog) FetchWithBundle(_ context.Context, _ catalog.Reference) ([]byte, []byte, catalog.ArtifactInfo, error) {
	return nil, nil, catalog.ArtifactInfo{}, nil
}

func (m *mockCatalog) Resolve(_ context.Context, _ catalog.Reference) (catalog.ArtifactInfo, error) {
	return catalog.ArtifactInfo{}, nil
}

func (m *mockCatalog) List(_ context.Context, _ catalog.ArtifactKind, _ string) ([]catalog.ArtifactInfo, error) {
	return m.artifacts, m.listErr
}

func (m *mockCatalog) Exists(_ context.Context, _ catalog.Reference) (bool, error) {
	return false, nil
}
func (m *mockCatalog) Delete(_ context.Context, _ catalog.Reference) error { return nil }

func TestResolveArtifactName(t *testing.T) {
	t.Run("explicit name takes priority", func(t *testing.T) {
		name, err := ResolveArtifactName("my-app", "from-metadata", "/path/to/file.yaml")
		require.NoError(t, err)
		assert.Equal(t, "my-app", name)
	})

	t.Run("falls back to metadata name", func(t *testing.T) {
		name, err := ResolveArtifactName("", "from-metadata", "/path/to/file.yaml")
		require.NoError(t, err)
		assert.Equal(t, "from-metadata", name)
	})

	t.Run("falls back to filename", func(t *testing.T) {
		name, err := ResolveArtifactName("", "", "/path/to/my-solution.yaml")
		require.NoError(t, err)
		assert.Equal(t, "my-solution", name)
	})

	t.Run("filename with nested path", func(t *testing.T) {
		name, err := ResolveArtifactName("", "", "solutions/gcp-basic.yml")
		require.NoError(t, err)
		assert.Equal(t, "gcp-basic", name)
	})

	t.Run("invalid name format", func(t *testing.T) {
		_, err := ResolveArtifactName("INVALID_NAME", "", "file.yaml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid name")
	})

	t.Run("all empty falls back to filename base", func(t *testing.T) {
		// "file" is a valid catalog name
		name, err := ResolveArtifactName("", "", "file.yaml")
		require.NoError(t, err)
		assert.Equal(t, "file", name)
	})
}

func TestResolveArtifactVersion(t *testing.T) {
	v1 := semver.MustParse("1.0.0")
	v2 := semver.MustParse("2.0.0")

	t.Run("explicit version takes priority", func(t *testing.T) {
		v, overrides, err := ResolveArtifactVersion("2.0.0", v1)
		require.NoError(t, err)
		assert.Equal(t, v2, v)
		assert.True(t, overrides, "should indicate it overrides metadata version")
	})

	t.Run("explicit version same as metadata", func(t *testing.T) {
		v, overrides, err := ResolveArtifactVersion("1.0.0", v1)
		require.NoError(t, err)
		assert.Equal(t, v1, v)
		assert.False(t, overrides, "should not indicate override when versions match")
	})

	t.Run("explicit version no metadata", func(t *testing.T) {
		v, overrides, err := ResolveArtifactVersion("1.0.0", nil)
		require.NoError(t, err)
		assert.Equal(t, v1, v)
		assert.False(t, overrides)
	})

	t.Run("falls back to metadata version", func(t *testing.T) {
		v, overrides, err := ResolveArtifactVersion("", v1)
		require.NoError(t, err)
		assert.Equal(t, v1, v)
		assert.False(t, overrides)
	})

	t.Run("invalid explicit version", func(t *testing.T) {
		_, _, err := ResolveArtifactVersion("not-a-version", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid version")
	})

	t.Run("no version available", func(t *testing.T) {
		_, _, err := ResolveArtifactVersion("", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no version")
	})
}

func BenchmarkResolveArtifactName(b *testing.B) {
	for b.Loop() {
		_, _ = ResolveArtifactName("", "my-solution", "/path/to/file.yaml")
	}
}

func BenchmarkResolveArtifactVersion(b *testing.B) {
	metadata := semver.MustParse("1.0.0")
	for b.Loop() {
		_, _, _ = ResolveArtifactVersion("2.0.0", metadata)
	}
}

func TestNextPatchVersion(t *testing.T) {
	ctx := context.Background()

	t.Run("no existing versions returns 0.1.0", func(t *testing.T) {
		cat := &mockCatalog{}
		v, err := NextPatchVersion(ctx, cat, catalog.ArtifactKindSolution, "my-app")
		require.NoError(t, err)
		assert.Equal(t, "0.1.0", v.String())
	})

	t.Run("increments patch from highest version", func(t *testing.T) {
		cat := &mockCatalog{
			artifacts: []catalog.ArtifactInfo{
				{Reference: catalog.Reference{Version: semver.MustParse("1.0.0")}, CreatedAt: time.Now()},
				{Reference: catalog.Reference{Version: semver.MustParse("1.2.3")}, CreatedAt: time.Now()},
				{Reference: catalog.Reference{Version: semver.MustParse("1.1.0")}, CreatedAt: time.Now()},
			},
		}
		v, err := NextPatchVersion(ctx, cat, catalog.ArtifactKindSolution, "my-app")
		require.NoError(t, err)
		assert.Equal(t, "1.2.4", v.String())
	})

	t.Run("single version increments patch", func(t *testing.T) {
		cat := &mockCatalog{
			artifacts: []catalog.ArtifactInfo{
				{Reference: catalog.Reference{Version: semver.MustParse("0.1.0")}, CreatedAt: time.Now()},
			},
		}
		v, err := NextPatchVersion(ctx, cat, catalog.ArtifactKindSolution, "my-app")
		require.NoError(t, err)
		assert.Equal(t, "0.1.1", v.String())
	})

	t.Run("skips artifacts without version", func(t *testing.T) {
		cat := &mockCatalog{
			artifacts: []catalog.ArtifactInfo{
				{Reference: catalog.Reference{Name: "my-app"}, CreatedAt: time.Now()},                                     // no version (alias tag)
				{Reference: catalog.Reference{Name: "my-app", Version: semver.MustParse("2.0.0")}, CreatedAt: time.Now()}, // has version
			},
		}
		v, err := NextPatchVersion(ctx, cat, catalog.ArtifactKindSolution, "my-app")
		require.NoError(t, err)
		assert.Equal(t, "2.0.1", v.String())
	})

	t.Run("catalog list error", func(t *testing.T) {
		cat := &mockCatalog{listErr: assert.AnError}
		_, err := NextPatchVersion(ctx, cat, catalog.ArtifactKindSolution, "my-app")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "listing catalog versions")
	})
}

func BenchmarkNextPatchVersion(b *testing.B) {
	ctx := context.Background()
	cat := &mockCatalog{
		artifacts: []catalog.ArtifactInfo{
			{Reference: catalog.Reference{Version: semver.MustParse("1.0.0")}},
			{Reference: catalog.Reference{Version: semver.MustParse("1.2.3")}},
			{Reference: catalog.Reference{Version: semver.MustParse("1.1.0")}},
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = NextPatchVersion(ctx, cat, catalog.ArtifactKindSolution, "my-app")
	}
}
