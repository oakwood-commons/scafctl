// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginKindFromCacheKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cacheKey string
		wantName string
		wantKind solution.PluginKind
	}{
		{"github", "github", solution.PluginKindProvider},
		{"exec", "exec", solution.PluginKindProvider},
		{"auth-handler-github", "github", solution.PluginKindAuthHandler},
		{"auth-handler-entra", "entra", solution.PluginKindAuthHandler},
		{"auth-handler-gcp", "gcp", solution.PluginKindAuthHandler},
	}

	for _, tt := range tests {
		t.Run(tt.cacheKey, func(t *testing.T) {
			t.Parallel()
			name, kind := PluginKindFromCacheKey(tt.cacheKey)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantKind, kind)
		})
	}
}

func TestBuildUpdateConstraint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		currentVer string
		target     UpdateTarget
		want       string
	}{
		{"latest returns empty", "1.2.3", UpdateTargetLatest, ""},
		{"minor for 1.x", "1.2.3", UpdateTargetMinor, ">=1.2.3, <2.0.0"},
		{"minor for 2.x", "2.0.0", UpdateTargetMinor, ">=2.0.0, <3.0.0"},
		{"minor for 0.x constrains to same minor", "0.4.2", UpdateTargetMinor, ">=0.4.2, <0.5.0"},
		{"patch for 1.2.x", "1.2.3", UpdateTargetPatch, ">=1.2.3, <1.3.0"},
		{"patch for 0.4.x", "0.4.0", UpdateTargetPatch, ">=0.4.0, <0.5.0"},
		{"invalid version returns empty", "not-semver", UpdateTargetMinor, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildUpdateConstraint(tt.currentVer, tt.target)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPlanUpdates_RequiresNamesOrAll(t *testing.T) {
	t.Parallel()

	cache := NewCache(t.TempDir())
	_, err := PlanUpdates(t.Context(), cache, nil, UpdateOptions{
		Names: nil,
		All:   false,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no plugin names specified")
}

func TestPlanUpdates_PluginNotInCache(t *testing.T) {
	t.Parallel()

	cache := NewCache(t.TempDir())
	// Use a nil catalogFetcher — it should return an error for explicit names not in cache.
	_, err := PlanUpdates(t.Context(), cache, nil, UpdateOptions{
		Names: []string{"nonexistent"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found in cache")
}

func TestPlanUpdates_NilFetcher_ReportsFailed(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	seedUpdaterCache(t, cacheDir, "exec", "1.0.0")

	cache := NewCache(cacheDir)

	// Nil catalogFetcher with a cached plugin — should report it as failed, not panic.
	plan, err := PlanUpdates(t.Context(), cache, nil, UpdateOptions{
		All:    true,
		Target: UpdateTargetLatest,
	})
	require.NoError(t, err)
	assert.Empty(t, plan.Updates)
	require.Len(t, plan.Failed, 1)
	assert.Equal(t, "exec", plan.Failed[0].Name)
	assert.Contains(t, plan.Failed[0].Error, "no remote catalogs configured")
}

// mockCatalogForUpdater implements catalog.Catalog for updater tests.
type mockCatalogForUpdater struct {
	resolveFunc func(ctx context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error)
}

func (m *mockCatalogForUpdater) Name() string { return "mock" }

func (m *mockCatalogForUpdater) Resolve(ctx context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, ref)
	}
	return catalog.ArtifactInfo{}, fmt.Errorf("not found")
}

func (m *mockCatalogForUpdater) Store(context.Context, catalog.Reference, []byte, []byte, map[string]string, bool) (catalog.ArtifactInfo, error) {
	return catalog.ArtifactInfo{}, nil
}

func (m *mockCatalogForUpdater) Fetch(context.Context, catalog.Reference) ([]byte, catalog.ArtifactInfo, error) {
	return nil, catalog.ArtifactInfo{}, nil
}

func (m *mockCatalogForUpdater) FetchWithBundle(context.Context, catalog.Reference) ([]byte, []byte, catalog.ArtifactInfo, error) {
	return nil, nil, catalog.ArtifactInfo{}, nil
}

func (m *mockCatalogForUpdater) List(context.Context, catalog.ArtifactKind, string) ([]catalog.ArtifactInfo, error) {
	return nil, nil
}

func (m *mockCatalogForUpdater) Exists(context.Context, catalog.Reference) (bool, error) {
	return false, nil
}

func (m *mockCatalogForUpdater) Delete(context.Context, catalog.Reference) error { return nil }

func seedUpdaterCache(t *testing.T, cacheDir, name, version string) {
	t.Helper()
	platform := CurrentPlatform()
	platformKey := PlatformCacheKey(platform)
	dir := filepath.Join(cacheDir, name, version, platformKey)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	binName := name
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, binName), []byte("bin-"+version), 0o755))
}

func TestPlanUpdates_AllWithCachedPlugins_UpdateAvailable(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	seedUpdaterCache(t, cacheDir, "github", "1.0.0")

	mock := &mockCatalogForUpdater{
		resolveFunc: func(_ context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error) {
			v := semver.MustParse("2.0.0")
			return catalog.ArtifactInfo{
				Reference: catalog.Reference{Name: ref.Name, Version: v},
				Catalog:   "mock",
			}, nil
		},
	}

	cache := NewCache(cacheDir)
	fetcher := catalog.NewPluginFetcher(mock, logr.Discard())

	plan, err := PlanUpdates(t.Context(), cache, fetcher, UpdateOptions{
		All:    true,
		Target: UpdateTargetLatest,
	})
	require.NoError(t, err)
	require.Len(t, plan.Updates, 1)
	assert.Equal(t, "github", plan.Updates[0].Name)
	assert.Equal(t, "1.0.0", plan.Updates[0].OldVersion)
	assert.Equal(t, "2.0.0", plan.Updates[0].NewVersion)
	assert.Equal(t, string(solution.PluginKindProvider), plan.Updates[0].Kind)
}

func TestPlanUpdates_AllWithCachedPlugins_UpToDate(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	seedUpdaterCache(t, cacheDir, "exec", "1.5.0")

	mock := &mockCatalogForUpdater{
		resolveFunc: func(_ context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error) {
			// Return same version as cached.
			v := semver.MustParse("1.5.0")
			return catalog.ArtifactInfo{
				Reference: catalog.Reference{Name: ref.Name, Version: v},
				Catalog:   "mock",
			}, nil
		},
	}

	cache := NewCache(cacheDir)
	fetcher := catalog.NewPluginFetcher(mock, logr.Discard())

	plan, err := PlanUpdates(t.Context(), cache, fetcher, UpdateOptions{
		All:    true,
		Target: UpdateTargetLatest,
	})
	require.NoError(t, err)
	assert.Empty(t, plan.Updates)
	assert.Contains(t, plan.UpToDate, "exec")
}

func TestPlanUpdates_CatalogResolutionFails(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	seedUpdaterCache(t, cacheDir, "github", "1.0.0")

	mock := &mockCatalogForUpdater{
		resolveFunc: func(_ context.Context, _ catalog.Reference) (catalog.ArtifactInfo, error) {
			return catalog.ArtifactInfo{}, fmt.Errorf("network timeout")
		},
	}

	cache := NewCache(cacheDir)
	fetcher := catalog.NewPluginFetcher(mock, logr.Discard())

	plan, err := PlanUpdates(t.Context(), cache, fetcher, UpdateOptions{
		Names:  []string{"github"},
		Target: UpdateTargetLatest,
	})
	require.NoError(t, err)
	assert.Empty(t, plan.Updates)
	require.Len(t, plan.Failed, 1)
	assert.Equal(t, "github", plan.Failed[0].Name)
	assert.Contains(t, plan.Failed[0].Error, "network timeout")
}

func TestPlanUpdates_PlatformFilter(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	// Seed for the current platform — when we filter by a different platform, it shouldn't find it.
	seedUpdaterCache(t, cacheDir, "github", "1.0.0")

	cache := NewCache(cacheDir)
	fetcher := catalog.NewPluginFetcher(&mockCatalogForUpdater{}, logr.Discard())

	// Use a platform that doesn't match our cached binaries.
	plan, err := PlanUpdates(t.Context(), cache, fetcher, UpdateOptions{
		All:      true,
		Target:   UpdateTargetLatest,
		Platform: "fakeos/fakearch",
	})
	require.NoError(t, err)
	// No plugins for fakeos/fakearch → nothing to update.
	assert.Empty(t, plan.Updates)
	assert.Empty(t, plan.UpToDate)
}

func TestPlanUpdates_AuthHandlerCacheKey(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	seedUpdaterCache(t, cacheDir, "auth-handler-github", "0.3.0")

	mock := &mockCatalogForUpdater{
		resolveFunc: func(_ context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error) {
			// Confirm the name is stripped (bare name "github", not "auth-handler-github").
			if ref.Name != "github" {
				return catalog.ArtifactInfo{}, fmt.Errorf("unexpected name: %s", ref.Name)
			}
			v := semver.MustParse("0.4.0")
			return catalog.ArtifactInfo{
				Reference: catalog.Reference{Name: ref.Name, Version: v},
				Catalog:   "mock",
			}, nil
		},
	}

	cache := NewCache(cacheDir)
	fetcher := catalog.NewPluginFetcher(mock, logr.Discard())

	plan, err := PlanUpdates(t.Context(), cache, fetcher, UpdateOptions{
		Names:  []string{"auth-handler-github"},
		Target: UpdateTargetLatest,
	})
	require.NoError(t, err)
	require.Len(t, plan.Updates, 1)
	assert.Equal(t, "auth-handler-github", plan.Updates[0].Name)
	assert.Equal(t, string(solution.PluginKindAuthHandler), plan.Updates[0].Kind)
}

func TestPlanUpdates_ResolvedVersionNotNewer(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	seedUpdaterCache(t, cacheDir, "exec", "2.0.0")

	mock := &mockCatalogForUpdater{
		resolveFunc: func(_ context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error) {
			// Return an older version (shouldn't happen but tests the branch).
			v := semver.MustParse("1.5.0")
			return catalog.ArtifactInfo{
				Reference: catalog.Reference{Name: ref.Name, Version: v},
				Catalog:   "mock",
			}, nil
		},
	}

	cache := NewCache(cacheDir)
	fetcher := catalog.NewPluginFetcher(mock, logr.Discard())

	plan, err := PlanUpdates(t.Context(), cache, fetcher, UpdateOptions{
		Names:  []string{"exec"},
		Target: UpdateTargetLatest,
	})
	require.NoError(t, err)
	assert.Empty(t, plan.Updates)
	assert.Contains(t, plan.UpToDate, "exec")
}

func TestPlanUpdates_EmptyResolvedVersion(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	seedUpdaterCache(t, cacheDir, "github", "1.0.0")

	mock := &mockCatalogForUpdater{
		resolveFunc: func(_ context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error) {
			// No version in the resolved info.
			return catalog.ArtifactInfo{
				Reference: catalog.Reference{Name: ref.Name},
				Catalog:   "mock",
			}, nil
		},
	}

	cache := NewCache(cacheDir)
	fetcher := catalog.NewPluginFetcher(mock, logr.Discard())

	plan, err := PlanUpdates(t.Context(), cache, fetcher, UpdateOptions{
		Names:  []string{"github"},
		Target: UpdateTargetLatest,
	})
	require.NoError(t, err)
	assert.Empty(t, plan.Updates)
	assert.Contains(t, plan.UpToDate, "github")
}

func TestPlanUpdates_DefaultTarget(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	seedUpdaterCache(t, cacheDir, "github", "1.0.0")

	mock := &mockCatalogForUpdater{
		resolveFunc: func(_ context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error) {
			v := semver.MustParse("2.0.0")
			return catalog.ArtifactInfo{
				Reference: catalog.Reference{Name: ref.Name, Version: v},
				Catalog:   "mock",
			}, nil
		},
	}

	cache := NewCache(cacheDir)
	fetcher := catalog.NewPluginFetcher(mock, logr.Discard())

	// Empty target should default to "latest".
	plan, err := PlanUpdates(t.Context(), cache, fetcher, UpdateOptions{
		Names: []string{"github"},
	})
	require.NoError(t, err)
	require.Len(t, plan.Updates, 1)
	assert.Equal(t, "2.0.0", plan.Updates[0].NewVersion)
}
