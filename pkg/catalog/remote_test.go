// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	scafctlauth "github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	orasauth "oras.land/oras-go/v2/registry/remote/auth"
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

	t.Run("with auth handler only", func(t *testing.T) {
		t.Parallel()
		handler := scafctlauth.NewMockHandler("test-handler")
		handler.GetTokenResult = &scafctlauth.Token{AccessToken: "test-token"}
		cat, err := NewRemoteCatalog(RemoteCatalogConfig{
			Name:        "test",
			Registry:    "ghcr.io",
			Repository:  "org/repo",
			AuthHandler: handler,
			AuthScope:   "repo:read",
			Logger:      logr.Discard(),
		})
		require.NoError(t, err)
		assert.NotNil(t, cat)
		assert.NotNil(t, cat.client.Credential)
	})

	t.Run("with credential store and auth handler", func(t *testing.T) {
		t.Parallel()
		handler := scafctlauth.NewMockHandler("test-handler")
		credStore := &CredentialStore{}
		cat, err := NewRemoteCatalog(RemoteCatalogConfig{
			Name:            "test",
			Registry:        "ghcr.io",
			Repository:      "org/repo",
			CredentialStore: credStore,
			AuthHandler:     handler,
			Logger:          logr.Discard(),
		})
		require.NoError(t, err)
		assert.NotNil(t, cat)
		assert.NotNil(t, cat.client.Credential)
	})

	t.Run("with credential store only", func(t *testing.T) {
		t.Parallel()
		credStore := &CredentialStore{}
		cat, err := NewRemoteCatalog(RemoteCatalogConfig{
			Name:            "test",
			Registry:        "ghcr.io",
			Repository:      "org/repo",
			CredentialStore: credStore,
			Logger:          logr.Discard(),
		})
		require.NoError(t, err)
		assert.NotNil(t, cat)
		assert.NotNil(t, cat.client.Credential)
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

func TestRemoteCatalog_RepositoryPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		registry   string
		repository string
		ref        Reference
		expected   string
	}{
		{
			name:       "solution with repository",
			registry:   "ghcr.io",
			repository: "myorg/scafctl",
			ref:        Reference{Kind: ArtifactKindSolution, Name: "my-sol"},
			expected:   "ghcr.io/myorg/scafctl/solutions/my-sol",
		},
		{
			name:       "provider without repository",
			registry:   "ghcr.io",
			repository: "",
			ref:        Reference{Kind: ArtifactKindProvider, Name: "my-prov"},
			expected:   "ghcr.io/providers/my-prov",
		},
		{
			name:       "kindless ref",
			registry:   "ghcr.io",
			repository: "myorg",
			ref:        Reference{Name: "starter-kit"},
			expected:   "ghcr.io/myorg/starter-kit",
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
			assert.Equal(t, tc.expected, cat.RepositoryPath(tc.ref))
		})
	}
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
			name:     "no version or digest returns sentinel",
			ref:      Reference{Kind: ArtifactKindSolution, Name: "sol"},
			expected: "__unresolved__",
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

func TestTagInfo_sorting(t *testing.T) {
	t.Parallel()

	tags := []TagInfo{
		{Tag: "stable", IsSemver: false},
		{Tag: "1.0.0", IsSemver: true, Version: "1.0.0"},
		{Tag: "latest", IsSemver: false},
		{Tag: "2.0.0", IsSemver: true, Version: "2.0.0"},
		{Tag: "1.5.0", IsSemver: true, Version: "1.5.0"},
	}

	// Apply the same sort as ListTags
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].IsSemver != tags[j].IsSemver {
			return tags[i].IsSemver
		}
		if tags[i].IsSemver {
			vi, _ := semver.NewVersion(tags[i].Version)
			vj, _ := semver.NewVersion(tags[j].Version)
			return vi.GreaterThan(vj)
		}
		return tags[i].Tag < tags[j].Tag
	})

	// Semver first (descending), then aliases (alphabetical)
	assert.Equal(t, "2.0.0", tags[0].Tag)
	assert.Equal(t, "1.5.0", tags[1].Tag)
	assert.Equal(t, "1.0.0", tags[2].Tag)
	assert.Equal(t, "latest", tags[3].Tag)
	assert.Equal(t, "stable", tags[4].Tag)
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

func TestRemoteCatalog_parseRepositoryPath(t *testing.T) {
	t.Parallel()

	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "test",
		Registry:   "ghcr.io",
		Repository: "myorg/scafctl",
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		path     string
		wantOK   bool
		wantKind ArtifactKind
		wantName string
	}{
		{
			name:     "solution",
			path:     "myorg/scafctl/solutions/my-app",
			wantOK:   true,
			wantKind: ArtifactKindSolution,
			wantName: "my-app",
		},
		{
			name:     "provider",
			path:     "myorg/scafctl/providers/terraform",
			wantOK:   true,
			wantKind: ArtifactKindProvider,
			wantName: "terraform",
		},
		{
			name:     "auth handler",
			path:     "myorg/scafctl/auth-handlers/github",
			wantOK:   true,
			wantKind: ArtifactKindAuthHandler,
			wantName: "github",
		},
		{
			name:   "wrong prefix",
			path:   "other/repo/solutions/my-app",
			wantOK: false,
		},
		{
			name:   "no kind segment",
			path:   "myorg/scafctl/my-app",
			wantOK: false,
		},
		{
			name:   "unknown kind",
			path:   "myorg/scafctl/widgets/foo",
			wantOK: false,
		},
		{
			name:   "empty name after kind",
			path:   "myorg/scafctl/solutions/",
			wantOK: false,
		},
		{
			name:   "unrelated repo",
			path:   "completely/different/path",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, ok := cat.parseRepositoryPath(tc.path)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantKind, d.Kind)
				assert.Equal(t, tc.wantName, d.Name)
			}
		})
	}
}

func TestRemoteCatalog_parseRepositoryPath_EmptyRepository(t *testing.T) {
	t.Parallel()

	// When repository is empty, paths are just "kind-plural/name"
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:     "test",
		Registry: "localhost:5000",
		Logger:   logr.Discard(),
	})
	require.NoError(t, err)

	d, ok := cat.parseRepositoryPath("solutions/my-app")
	assert.True(t, ok)
	assert.Equal(t, ArtifactKindSolution, d.Kind)
	assert.Equal(t, "my-app", d.Name)

	_, ok = cat.parseRepositoryPath("just-name")
	assert.False(t, ok)
}

func BenchmarkRemoteCatalog_parseRepositoryPath(b *testing.B) {
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:       "bench",
		Registry:   "ghcr.io",
		Repository: "myorg/scafctl",
		Logger:     logr.Discard(),
	})
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cat.parseRepositoryPath("myorg/scafctl/solutions/my-app")
	}
}

func TestListRepositories_ContextTimeout(t *testing.T) {
	t.Parallel()

	// Use an already-cancelled context so the timeout fires immediately
	// rather than waiting for the full DefaultHTTPTimeout.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cat := &RemoteCatalog{
		name:     "slow-registry",
		registry: "localhost:0",
		insecure: true,
		logger:   logr.Discard(),
		client:   &orasauth.Client{},
		enumerator: newOCICatalogEnumerator(enumeratorConfig{
			registry: "localhost:0",
			client:   &orasauth.Client{},
			logger:   logr.Discard(),
		}),
	}

	_, err := cat.ListRepositories(ctx)
	require.Error(t, err)
	assert.True(t, IsEnumerationNotSupported(err), "timeout should be reported as enumeration not supported, got: %v", err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestListRepositories_EmptyAutoFallsBackToIndex(t *testing.T) {
	t.Parallel()

	// When API enumeration returns zero repos with "auto" strategy,
	// ListRepositories should try FetchIndex as a fallback.
	// Since there's no real registry, FetchIndex fails and we get an empty
	// result (not an error).
	cat := &RemoteCatalog{
		name:              "test-fallback",
		registry:          "localhost:0",
		repository:        "myorg",
		discoveryStrategy: config.DiscoveryStrategyAuto,
		insecure:          true,
		logger:            logr.Discard(),
		client:            &orasauth.Client{},
		enumerator:        &mockEnumerator{repos: []string{}},
	}

	result, err := cat.ListRepositories(t.Context())
	require.NoError(t, err)
	assert.Empty(t, result, "should return empty when both enumeration and index fallback yield nothing")
}

func TestListRepositories_EmptyAPINoFallback(t *testing.T) {
	t.Parallel()

	// With "api" strategy, empty enumeration should NOT fall back to index.
	cat := &RemoteCatalog{
		name:              "test-api-only",
		registry:          "localhost:0",
		repository:        "myorg",
		discoveryStrategy: config.DiscoveryStrategyAPI,
		insecure:          true,
		logger:            logr.Discard(),
		client:            &orasauth.Client{},
		enumerator:        &mockEnumerator{repos: []string{}},
	}

	result, err := cat.ListRepositories(t.Context())
	require.NoError(t, err)
	assert.Empty(t, result, "api-only should return empty without fallback")
}

// mockEnumerator returns a fixed set of repository paths.
type mockEnumerator struct {
	repos []string
	err   error
}

func (m *mockEnumerator) enumerate(_ context.Context) ([]string, error) {
	return m.repos, m.err
}

func TestListAllArtifacts_SearchFilter(t *testing.T) {
	t.Parallel()

	cat := &RemoteCatalog{
		name:       "test",
		registry:   "registry.example.com",
		repository: "myorg",
		logger:     logr.Discard(),
		client:     &orasauth.Client{},
		enumerator: &mockEnumerator{
			repos: []string{
				"myorg/solutions/starter-kit",
				"myorg/solutions/hello-world",
				"myorg/solutions/foo",
				"myorg/providers/terraform",
			},
		},
	}

	t.Run("no search returns all", func(t *testing.T) {
		t.Parallel()
		// listAllArtifacts will fail on tag fetch (no real registry),
		// but the discovered list will be filtered. We test the filter
		// by checking that it attempts all repos (errors are logged, not returned).
		infos, err := cat.listAllArtifacts(t.Context(), "")
		require.NoError(t, err)
		// All tag fetches fail silently, so we get empty results.
		assert.Empty(t, infos)
	})

	t.Run("search pattern filters before tag fetch", func(t *testing.T) {
		t.Parallel()
		ctx := WithSearchPattern(t.Context(), "starter*")
		infos, err := cat.listAllArtifacts(ctx, "")
		require.NoError(t, err)
		// Tags fail silently, but we can verify the method doesn't error.
		assert.Empty(t, infos)
	})

	t.Run("kind filter applied", func(t *testing.T) {
		t.Parallel()
		infos, err := cat.listAllArtifacts(t.Context(), ArtifactKindProvider)
		require.NoError(t, err)
		assert.Empty(t, infos)
	})

	t.Run("search with no matches returns nil", func(t *testing.T) {
		t.Parallel()
		ctx := WithSearchPattern(t.Context(), "nonexistent*")
		infos, err := cat.listAllArtifacts(ctx, "")
		require.NoError(t, err)
		assert.Nil(t, infos)
	})
}

func TestListAllArtifacts_ConcurrencyRespected(t *testing.T) {
	t.Parallel()

	// Create enough repos to exercise the worker pool.
	repos := make([]string, 20)
	for i := range repos {
		repos[i] = fmt.Sprintf("myorg/solutions/app-%d", i)
	}

	cat := &RemoteCatalog{
		name:       "test",
		registry:   "registry.example.com",
		repository: "myorg",
		logger:     logr.Discard(),
		client:     &orasauth.Client{},
		enumerator: &mockEnumerator{repos: repos},
	}

	// With 20 repos and no real registry, all tag fetches fail silently.
	// The test verifies no deadlock/panic with concurrent goroutines.
	infos, err := cat.listAllArtifacts(t.Context(), "")
	require.NoError(t, err)
	assert.Empty(t, infos) // all tag fetches fail
}

// mockClientUpdatableEnumerator implements both registryEnumerator and clientUpdatable.
type mockClientUpdatableEnumerator struct {
	repos  []string
	client *orasauth.Client
}

func (m *mockClientUpdatableEnumerator) enumerate(_ context.Context) ([]string, error) {
	return m.repos, nil
}

func (m *mockClientUpdatableEnumerator) setClient(client *orasauth.Client) {
	m.client = client
}

func TestSetClient_PropagatesClient(t *testing.T) {
	t.Parallel()

	originalClient := &orasauth.Client{}
	newClient := &orasauth.Client{}
	mockEnum := &mockClientUpdatableEnumerator{client: originalClient}

	cat := &RemoteCatalog{
		name:       "test",
		registry:   "registry.example.com",
		repository: "myorg",
		logger:     logr.Discard(),
		client:     originalClient,
		enumerator: mockEnum,
	}

	cat.SetClient(newClient)

	assert.Same(t, newClient, cat.client, "catalog client should be updated")
	assert.Same(t, newClient, mockEnum.client, "enumerator client should be propagated")
}

func TestSetClient_NonUpdatableEnumerator(t *testing.T) {
	t.Parallel()

	originalClient := &orasauth.Client{}
	newClient := &orasauth.Client{}

	cat := &RemoteCatalog{
		name:       "test",
		registry:   "registry.example.com",
		repository: "myorg",
		logger:     logr.Discard(),
		client:     originalClient,
		enumerator: &mockEnumerator{repos: nil},
	}

	// Should not panic when enumerator doesn't implement clientUpdatable.
	cat.SetClient(newClient)

	assert.Same(t, newClient, cat.client, "catalog client should be updated")
}

func TestLatestVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		versions []string
		expected string
	}{
		{
			name:     "multiple versions",
			versions: []string{"0.1.0", "1.0.0", "0.9.0", "1.2.0", "1.1.0"},
			expected: "1.2.0",
		},
		{
			name:     "single version",
			versions: []string{"2.0.0"},
			expected: "2.0.0",
		},
		{
			name:     "empty",
			versions: nil,
			expected: "",
		},
		{
			name:     "prerelease lower than release",
			versions: []string{"1.0.0-rc.1", "1.0.0"},
			expected: "1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var versions []*semver.Version
			for _, v := range tt.versions {
				versions = append(versions, semver.MustParse(v))
			}
			got := latestVersion(versions)
			if tt.expected == "" {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, tt.expected, got.String())
			}
		})
	}
}

func TestIsOCIAuthError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"generic error", fmt.Errorf("network timeout"), false},
		{"401 status", fmt.Errorf("response status code 401: Unauthorized"), true},
		{"403 status", fmt.Errorf("response status code 403: denied"), true},
		{"unauthorized keyword", fmt.Errorf("unauthorized access to resource"), true},
		{"denied keyword", fmt.Errorf("token exchange denied"), true},
		{"nested 403", fmt.Errorf("failed to fetch: %w", fmt.Errorf("GET /token: response status code 403: denied: denied")), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isOCIAuthError(tt.err))
		})
	}
}

func TestSwitchToAnonymous(t *testing.T) {
	t.Parallel()

	cat := &RemoteCatalog{
		name:     "test",
		registry: "ghcr.io",
		logger:   logr.Discard(),
		client:   &orasauth.Client{},
	}

	assert.False(t, cat.HasStaleCredentials(), "should start without stale credentials")

	originalClient := cat.client
	cat.switchToAnonymous()

	assert.True(t, cat.HasStaleCredentials(), "should report stale credentials after switch")
	assert.NotSame(t, originalClient, cat.client, "client should be replaced")
}

func TestAnonymousClient_Insecure(t *testing.T) {
	t.Parallel()

	cat := &RemoteCatalog{
		name:     "test",
		registry: "localhost:5000",
		logger:   logr.Discard(),
		client:   &orasauth.Client{},
		insecure: true,
	}

	anonClient := cat.anonymousClient()
	assert.NotNil(t, anonClient)
	// The anonymous client should have a custom transport for insecure mode.
	assert.NotNil(t, anonClient.Client)
}

func TestAnonymousClient_Secure(t *testing.T) {
	t.Parallel()

	cat := &RemoteCatalog{
		name:     "test",
		registry: "ghcr.io",
		logger:   logr.Discard(),
		client:   &orasauth.Client{},
		insecure: false,
	}

	anonClient := cat.anonymousClient()
	assert.NotNil(t, anonClient)
}

func TestAuthHandlerUsed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		handler  string
		expected string
	}{
		{name: "github handler", handler: "github", expected: "github"},
		{name: "empty handler", handler: "", expected: ""},
		{name: "custom handler", handler: "entra", expected: "entra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cat := &RemoteCatalog{
				name:            "test",
				registry:        "ghcr.io",
				logger:          logr.Discard(),
				client:          &orasauth.Client{},
				authHandlerUsed: tt.handler,
			}
			assert.Equal(t, tt.expected, cat.AuthHandlerUsed())
		})
	}
}

func TestCredentialSource(t *testing.T) {
	t.Parallel()

	t.Run("returns set value", func(t *testing.T) {
		t.Parallel()
		cat := &RemoteCatalog{
			logger: logr.Discard(),
			client: &orasauth.Client{},
		}
		cat.credentialSource.Store("docker credential helper (desktop)")
		assert.Equal(t, "docker credential helper (desktop)", cat.CredentialSource())
	})

	t.Run("returns empty when unset", func(t *testing.T) {
		t.Parallel()
		cat := &RemoteCatalog{
			logger: logr.Discard(),
			client: &orasauth.Client{},
		}
		assert.Empty(t, cat.CredentialSource())
	})
}

func TestDiscoveredArtifact_ToAnnotations(t *testing.T) {
	t.Parallel()

	t.Run("populates all non-empty fields", func(t *testing.T) {
		t.Parallel()
		d := DiscoveredArtifact{
			Name:        "my-app",
			DisplayName: "My Application",
			Description: "A test app",
			Category:    "deployment",
			Tags:        []string{"go", "cloud"},
		}
		ann := d.ToAnnotations()
		assert.Equal(t, "My Application", ann[AnnotationDisplayName])
		assert.Equal(t, "A test app", ann[AnnotationDescription])
		assert.Equal(t, "deployment", ann[AnnotationCategory])
		assert.Equal(t, "go,cloud", ann[AnnotationTags])
	})

	t.Run("omits empty fields", func(t *testing.T) {
		t.Parallel()
		d := DiscoveredArtifact{
			Name:        "bare",
			DisplayName: "Bare App",
		}
		ann := d.ToAnnotations()
		assert.Equal(t, "Bare App", ann[AnnotationDisplayName])
		assert.NotContains(t, ann, AnnotationDescription)
		assert.NotContains(t, ann, AnnotationCategory)
		assert.NotContains(t, ann, AnnotationTags)
	})

	t.Run("returns empty map when no metadata", func(t *testing.T) {
		t.Parallel()
		d := DiscoveredArtifact{Name: "empty"}
		ann := d.ToAnnotations()
		assert.Empty(t, ann)
	})
}

func TestIsOCIServerError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"generic error", fmt.Errorf("network timeout"), false},
		{"401 is not server error", fmt.Errorf("response status code 401"), false},
		{"500 status", fmt.Errorf("response status code 500"), true},
		{"INTERNAL_SERVER_ERROR keyword", fmt.Errorf("INTERNAL_SERVER_ERROR"), true},
		{"nested 500", fmt.Errorf("failed: %w", fmt.Errorf("response status code 500: error")), true},
		{"port 5000 is not server error", fmt.Errorf("connect to localhost:5000 failed"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isOCIServerError(tt.err))
		})
	}
}

func TestWrapWithCredentialHint(t *testing.T) {
	t.Parallel()

	t.Run("nil error returns nil", func(t *testing.T) {
		t.Parallel()
		cat := &RemoteCatalog{logger: logr.Discard()}
		assert.NoError(t, cat.wrapWithCredentialHint(nil))
	})

	t.Run("non-500 error passes through", func(t *testing.T) {
		t.Parallel()
		cat := &RemoteCatalog{logger: logr.Discard()}
		cat.credentialSource.Store("docker config static auth")
		original := fmt.Errorf("network timeout")
		result := cat.wrapWithCredentialHint(original)
		assert.Equal(t, original, result)
	})

	t.Run("500 with anonymous creds passes through", func(t *testing.T) {
		t.Parallel()
		cat := &RemoteCatalog{logger: logr.Discard()}
		// no credential source stored → anonymous
		original := fmt.Errorf("response status code 500")
		result := cat.wrapWithCredentialHint(original)
		assert.Equal(t, original, result)
	})

	t.Run("500 with credentials adds hint", func(t *testing.T) {
		t.Parallel()
		cat := &RemoteCatalog{logger: logr.Discard()}
		cat.credentialSource.Store("docker config static auth")
		original := fmt.Errorf("response status code 500")
		result := cat.wrapWithCredentialHint(original)
		assert.ErrorIs(t, result, original)
		assert.Contains(t, result.Error(), "hint:")
		assert.Contains(t, result.Error(), "docker config static auth")
		assert.Contains(t, result.Error(), "expired")
	})
}

func TestAuthHandlerPrecedenceOverDockerConfig(t *testing.T) {
	t.Parallel()

	// Set up a mock auth handler that returns a valid token.
	handler := scafctlauth.NewMockHandler("entra")
	handler.GetTokenResult = &scafctlauth.Token{AccessToken: "fresh-entra-token"}
	handler.StatusResult = &scafctlauth.Status{
		Authenticated: true,
		Claims:        &scafctlauth.Claims{Username: "testuser"},
	}

	// Set up a credential store with static Docker config creds for the same host.
	credStore := &CredentialStore{
		config: &dockerConfig{
			Auths: map[string]dockerAuthEntry{
				"myregistry.azurecr.io": {
					Username: "stale-user",
					Password: "stale-password",
				},
			},
		},
		logger: logr.Discard(),
	}

	// Create catalog with both credential store and auth handler.
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:            "test",
		Registry:        "myregistry.azurecr.io",
		Repository:      "org/repo",
		CredentialStore: credStore,
		AuthHandler:     handler,
		Logger:          logr.Discard(),
	})
	require.NoError(t, err)
	require.NotNil(t, cat.client.Credential)

	// Call the credential function and verify the auth handler is used, not Docker config.
	cred, err := cat.client.Credential(context.Background(), "myregistry.azurecr.io")
	require.NoError(t, err)
	assert.Equal(t, "fresh-entra-token", cred.Password, "auth handler token should take precedence over Docker config")
	assert.Equal(t, "entra auth handler token", cat.CredentialSource(), "credential source should indicate auth handler")
}

func TestAuthHandlerFallsBackToCredentialStore(t *testing.T) {
	t.Parallel()

	// Set up a mock auth handler that fails to produce a token.
	handler := scafctlauth.NewMockHandler("entra")
	handler.GetTokenErr = fmt.Errorf("token expired")

	// Set up a credential store with valid static Docker config creds.
	credStore := &CredentialStore{
		config: &dockerConfig{
			Auths: map[string]dockerAuthEntry{
				"myregistry.azurecr.io": {
					Username: "docker-user",
					Password: "docker-password",
				},
			},
		},
		logger: logr.Discard(),
	}

	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:            "test",
		Registry:        "myregistry.azurecr.io",
		Repository:      "org/repo",
		CredentialStore: credStore,
		AuthHandler:     handler,
		Logger:          logr.Discard(),
	})
	require.NoError(t, err)

	// When auth handler fails, credential store should be used.
	cred, err := cat.client.Credential(context.Background(), "myregistry.azurecr.io")
	require.NoError(t, err)
	assert.Equal(t, "docker-user", cred.Username, "should fall back to Docker config when auth handler fails")
	assert.Equal(t, "docker-password", cred.Password)
	assert.Equal(t, "docker config static auth", cat.CredentialSource())
}
