// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRemoteCatalog(t *testing.T) {
	t.Parallel()

	t.Run("defaults name to registry when empty", func(t *testing.T) {
		t.Parallel()
		cat, err := NewRemoteCatalog(RemoteCatalogConfig{
			Registry:   "ghcr.io",
			Repository: "myorg/scafctl",
			Logger:     logr.Discard(),
		})
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io", cat.Name())
	})

	t.Run("uses explicit name", func(t *testing.T) {
		t.Parallel()
		cat, err := NewRemoteCatalog(RemoteCatalogConfig{
			Name:       "company-registry",
			Registry:   "ghcr.io",
			Repository: "myorg/scafctl",
			Logger:     logr.Discard(),
		})
		require.NoError(t, err)
		assert.Equal(t, "company-registry", cat.Name())
	})

	t.Run("insecure flag configures transport", func(t *testing.T) {
		t.Parallel()
		cat, err := NewRemoteCatalog(RemoteCatalogConfig{
			Name:       "test",
			Registry:   "localhost:5000",
			Repository: "test",
			Insecure:   true,
			Logger:     logr.Discard(),
		})
		require.NoError(t, err)
		assert.NotNil(t, cat)
		assert.Equal(t, "localhost:5000", cat.Registry())
	})
}

func TestRemoteCatalog_Name(t *testing.T) {
	t.Parallel()
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "my-catalog",
		Registry:   "ghcr.io",
		Repository: "org/repo",
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)
	assert.Equal(t, "my-catalog", cat.Name())
}

func TestRemoteCatalog_Registry(t *testing.T) {
	t.Parallel()
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "test",
		Registry:   "ghcr.io",
		Repository: "org/repo",
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)
	assert.Equal(t, "ghcr.io", cat.Registry())
}

func TestRemoteCatalog_Repository(t *testing.T) {
	t.Parallel()
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "test",
		Registry:   "ghcr.io",
		Repository: "org/repo",
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)
	assert.Equal(t, "org/repo", cat.Repository())
}

func TestRemoteCatalog_buildRepositoryPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		registry   string
		repository string
		ref        Reference
		expected   string
	}{
		{
			name:       "full path with repository",
			registry:   "ghcr.io",
			repository: "myorg/scafctl",
			ref:        Reference{Kind: ArtifactKindSolution, Name: "my-solution"},
			expected:   "ghcr.io/myorg/scafctl/solutions/my-solution",
		},
		{
			name:       "path without repository",
			registry:   "ghcr.io",
			repository: "",
			ref:        Reference{Kind: ArtifactKindSolution, Name: "my-solution"},
			expected:   "ghcr.io/solutions/my-solution",
		},
		{
			name:       "provider artifact",
			registry:   "registry.example.com",
			repository: "team/artifacts",
			ref:        Reference{Kind: ArtifactKindProvider, Name: "my-provider"},
			expected:   "registry.example.com/team/artifacts/providers/my-provider",
		},
		{
			name:       "auth-handler artifact",
			registry:   "ghcr.io",
			repository: "org/repo",
			ref:        Reference{Kind: ArtifactKindAuthHandler, Name: "my-auth"},
			expected:   "ghcr.io/org/repo/auth-handlers/my-auth",
		},
		{
			name:       "kindless Docker-style with repository",
			registry:   "ghcr.io",
			repository: "myorg",
			ref:        Reference{Name: "starter-kit"},
			expected:   "ghcr.io/myorg/starter-kit",
		},
		{
			name:       "kindless Docker-style without repository",
			registry:   "ghcr.io",
			repository: "",
			ref:        Reference{Name: "starter-kit"},
			expected:   "ghcr.io/starter-kit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cat, err := NewRemoteCatalog(RemoteCatalogConfig{
				Name:       "test",
				Registry:   tc.registry,
				Repository: tc.repository,
				Logger:     logr.Discard(),
			})
			require.NoError(t, err)
			assert.Equal(t, tc.expected, cat.buildRepositoryPath(tc.ref))
		})
	}
}

func TestRemoteCatalog_tagForRef(t *testing.T) {
	t.Parallel()

	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "test",
		Registry:   "ghcr.io",
		Repository: "org/repo",
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		ref      Reference
		expected string
	}{
		{
			name:     "with version",
			ref:      Reference{Kind: ArtifactKindSolution, Name: "sol", Version: semver.MustParse("1.2.3")},
			expected: "1.2.3",
		},
		{
			name:     "with digest",
			ref:      Reference{Kind: ArtifactKindSolution, Name: "sol", Digest: "sha256:abc123"},
			expected: "sha256:abc123",
		},
		{
			name:     "digest takes precedence over version",
			ref:      Reference{Kind: ArtifactKindSolution, Name: "sol", Version: semver.MustParse("1.0.0"), Digest: "sha256:abc123"},
			expected: "sha256:abc123",
		},
		{
			name:     "no version or digest returns latest",
			ref:      Reference{Kind: ArtifactKindSolution, Name: "sol"},
			expected: "latest",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, cat.tagForRef(tc.ref))
		})
	}
}

func TestRemoteCatalog_getRepository(t *testing.T) {
	t.Parallel()

	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "test",
		Registry:   "ghcr.io",
		Repository: "org/repo",
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)

	ref := Reference{Kind: ArtifactKindSolution, Name: "my-solution"}
	repo, err := cat.getRepository(ref)
	require.NoError(t, err)
	require.NotNil(t, repo)
}

// Benchmarks

func BenchmarkNewRemoteCatalog(b *testing.B) {
	cfg := RemoteCatalogConfig{
		Name:       "bench",
		Registry:   "ghcr.io",
		Repository: "org/repo",
		Logger:     logr.Discard(),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := NewRemoteCatalog(cfg)
		require.NoError(b, err)
	}
}

func BenchmarkRemoteCatalog_buildRepositoryPath(b *testing.B) {
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "bench",
		Registry:   "ghcr.io",
		Repository: "org/repo",
		Logger:     logr.Discard(),
	})
	require.NoError(b, err)
	ref := Reference{Kind: ArtifactKindSolution, Name: "my-solution"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cat.buildRepositoryPath(ref)
	}
}

func BenchmarkRemoteCatalog_tagForRef(b *testing.B) {
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "bench",
		Registry:   "ghcr.io",
		Repository: "org/repo",
		Logger:     logr.Discard(),
	})
	require.NoError(b, err)
	ref := Reference{Kind: ArtifactKindSolution, Name: "sol", Version: semver.MustParse("1.2.3")}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cat.tagForRef(ref)
	}
}
