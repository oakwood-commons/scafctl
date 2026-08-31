// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"errors"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPluginResolver implements PluginResolver for testing.
type mockPluginResolver struct {
	plugins map[string]catalog.ArtifactInfo
}

func (m *mockPluginResolver) ResolvePlugin(_ context.Context, name string, kind catalog.ArtifactKind, _ string) (catalog.ArtifactInfo, error) {
	key := name + ":" + string(kind)
	info, ok := m.plugins[key]
	if !ok {
		return catalog.ArtifactInfo{}, &catalog.ArtifactNotFoundError{
			Reference: catalog.Reference{Kind: kind, Name: name},
			Catalog:   "mock",
		}
	}
	return info, nil
}

func TestVendorPlugins_ResolvesFromCatalog(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"azure-provider:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "azure-provider",
					Version: semver.MustParse("1.2.3"),
				},
				Digest:    "sha256:abc123",
				Catalog:   "test-catalog",
				Canonical: "ghcr.io/org/plugins",
			},
		},
	}

	plugins := []solution.PluginDependency{
		{
			Name:    "azure-provider",
			Kind:    solution.PluginKindProvider,
			Version: "^1.0.0",
		},
	}

	result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
		PluginResolver: resolver,
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	assert.Equal(t, "azure-provider", result.ResolvedPlugins[0].Name)
	assert.Equal(t, "provider", result.ResolvedPlugins[0].Kind)
	assert.Equal(t, "1.2.3", result.ResolvedPlugins[0].Version)
	assert.Equal(t, "^1.0.0", result.ResolvedPlugins[0].Constraint)
	assert.Equal(t, "sha256:abc123", result.ResolvedPlugins[0].Digest)
	assert.Equal(t, "test-catalog", result.ResolvedPlugins[0].ResolvedFrom)
	// The stable canonical identity is recorded alongside the mutable alias.
	assert.Equal(t, "ghcr.io/org/plugins", result.ResolvedPlugins[0].ResolvedCanonical)
}

func TestVendorPlugins_EmptyCanonicalWhenUnpinnable(t *testing.T) {
	ctx := testContext()

	// A resolver that returns no canonical identity (e.g. a local catalog,
	// whose filesystem path is machine-specific and not portable).
	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"local-provider:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "local-provider",
					Version: semver.MustParse("1.0.0"),
				},
				Digest:  "sha256:localdigest",
				Catalog: "local",
			},
		},
	}

	plugins := []solution.PluginDependency{
		{
			Name:    "local-provider",
			Kind:    solution.PluginKindProvider,
			Version: "^1.0.0",
		},
	}

	result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
		PluginResolver: resolver,
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	assert.Equal(t, "local", result.ResolvedPlugins[0].ResolvedFrom)
	assert.Empty(t, result.ResolvedPlugins[0].ResolvedCanonical)
}

func TestVendorPlugins_ReplaysFromLockFile(t *testing.T) {
	ctx := testContext()

	// Resolver should NOT be called if lock entry is valid
	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{},
	}

	plugins := []solution.PluginDependency{
		{
			Name:    "azure-provider",
			Kind:    solution.PluginKindProvider,
			Version: "^1.0.0",
		},
	}

	existingLock := &LockFile{
		Version: 1,
		Plugins: []LockPlugin{
			{
				Name:         "azure-provider",
				Kind:         "provider",
				Version:      "1.2.3",
				Constraint:   ">=1.0.0",
				Digest:       "sha256:abc123",
				ResolvedFrom: "cached-catalog",
			},
		},
	}

	result, err := VendorPlugins(ctx, plugins, existingLock, VendorPluginsOptions{
		PluginResolver: resolver,
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	// Should come from the lock file, not the resolver
	assert.Equal(t, "cached-catalog", result.ResolvedPlugins[0].ResolvedFrom)
	assert.Equal(t, "1.2.3", result.ResolvedPlugins[0].Version)
	// The pinned version is replayed, but the requested constraint is refreshed
	// to the current spec value (was ">=1.0.0" in the lock).
	assert.Equal(t, "^1.0.0", result.ResolvedPlugins[0].Constraint)
}

func TestVendorPlugins_StaleLockEntryReResolves(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"azure-provider:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "azure-provider",
					Version: semver.MustParse("2.0.0"),
				},
				Digest:  "sha256:newdigest",
				Catalog: "fresh-catalog",
			},
		},
	}

	plugins := []solution.PluginDependency{
		{
			Name:    "azure-provider",
			Kind:    solution.PluginKindProvider,
			Version: "^2.0.0",
		},
	}

	// Lock file has version 1.2.3 which doesn't satisfy ^2.0.0
	existingLock := &LockFile{
		Version: 1,
		Plugins: []LockPlugin{
			{
				Name:         "azure-provider",
				Kind:         "provider",
				Version:      "1.2.3",
				Digest:       "sha256:olddigest",
				ResolvedFrom: "old-catalog",
			},
		},
	}

	result, err := VendorPlugins(ctx, plugins, existingLock, VendorPluginsOptions{
		PluginResolver: resolver,
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	// Should be re-resolved from catalog, not the stale lock
	assert.Equal(t, "fresh-catalog", result.ResolvedPlugins[0].ResolvedFrom)
	assert.Equal(t, "2.0.0", result.ResolvedPlugins[0].Version)
	assert.Equal(t, "sha256:newdigest", result.ResolvedPlugins[0].Digest)
}

func TestVendorPlugins_NilResolver(t *testing.T) {
	ctx := testContext()

	plugins := []solution.PluginDependency{
		{
			Name:    "azure-provider",
			Kind:    solution.PluginKindProvider,
			Version: "^1.0.0",
		},
	}

	result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
		PluginResolver: nil,
	})
	require.NoError(t, err)
	assert.Empty(t, result.ResolvedPlugins)
}

func TestVendorPlugins_MultiplePlugins(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"azure-provider:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "azure-provider",
					Version: semver.MustParse("1.5.0"),
				},
				Digest:  "sha256:prov123",
				Catalog: "catalog-a",
			},
			"entra-auth:auth-handler": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindAuthHandler,
					Name:    "entra-auth",
					Version: semver.MustParse("2.1.0"),
				},
				Digest:  "sha256:auth456",
				Catalog: "catalog-b",
			},
		},
	}

	plugins := []solution.PluginDependency{
		{
			Name:    "azure-provider",
			Kind:    solution.PluginKindProvider,
			Version: "^1.0.0",
		},
		{
			Name:    "entra-auth",
			Kind:    solution.PluginKindAuthHandler,
			Version: "^2.0.0",
		},
	}

	result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
		PluginResolver: resolver,
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 2)

	assert.Equal(t, "azure-provider", result.ResolvedPlugins[0].Name)
	assert.Equal(t, "provider", result.ResolvedPlugins[0].Kind)
	assert.Equal(t, "entra-auth", result.ResolvedPlugins[1].Name)
	assert.Equal(t, "auth-handler", result.ResolvedPlugins[1].Kind)
}

func TestVendorPlugins_VersionConstraintViolation(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"azure-provider:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "azure-provider",
					Version: semver.MustParse("3.0.0"),
				},
				Digest:  "sha256:wrongversion",
				Catalog: "catalog",
			},
		},
	}

	plugins := []solution.PluginDependency{
		{
			Name:    "azure-provider",
			Kind:    solution.PluginKindProvider,
			Version: "^1.0.0", // Wants 1.x, but catalog has 3.0.0
		},
	}

	_, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
		PluginResolver: resolver,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not satisfy constraint")
}

func TestVendorPlugins_WithSignatureVerification(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"signed-plugin:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "signed-plugin",
					Version: semver.MustParse("2.0.0"),
				},
				Digest:   "sha256:def456",
				ImageRef: "ghcr.io/org/plugins/providers/signed-plugin@sha256:manifest123",
				Catalog:  "remote-catalog",
			},
		},
	}

	plugins := []solution.PluginDependency{
		{Name: "signed-plugin", Kind: solution.PluginKindProvider, Version: "^2.0.0"},
	}

	t.Run("records signature metadata", func(t *testing.T) {
		result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
			PluginResolver: resolver,
			VerifySignature: func(_ context.Context, imageRef string) (*LockPluginSignature, error) {
				assert.Equal(t, "ghcr.io/org/plugins/providers/signed-plugin@sha256:manifest123", imageRef)
				return &LockPluginSignature{
					Issuer:   "https://token.actions.githubusercontent.com",
					Identity: "https://github.com/org/plugin/.github/workflows/release.yaml@refs/tags/v2.0.0",
					SignedAt: "2025-06-01T10:00:00Z",
				}, nil
			},
		})
		require.NoError(t, err)
		require.Len(t, result.ResolvedPlugins, 1)
		require.NotNil(t, result.ResolvedPlugins[0].Signature)
		assert.Equal(t, "https://token.actions.githubusercontent.com", result.ResolvedPlugins[0].Signature.Issuer)
		assert.Equal(t, "2025-06-01T10:00:00Z", result.ResolvedPlugins[0].Signature.SignedAt)
	})

	t.Run("nil signature on warn-mode failure", func(t *testing.T) {
		result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
			PluginResolver: resolver,
			VerifySignature: func(_ context.Context, _ string) (*LockPluginSignature, error) {
				// Warn mode: caller returns nil (no error to propagate)
				return nil, nil
			},
		})
		require.NoError(t, err)
		require.Len(t, result.ResolvedPlugins, 1)
		assert.Nil(t, result.ResolvedPlugins[0].Signature)
	})

	t.Run("error on enforce-mode failure", func(t *testing.T) {
		_, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
			PluginResolver: resolver,
			VerifySignature: func(_ context.Context, _ string) (*LockPluginSignature, error) {
				return nil, assert.AnError
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "signature verification failed during lock")
	})
}

// fakePlatformDigestCatalog is a minimal platformDigestCatalog for unit-testing
// resolvePlatformDigests without a real OCI catalog.
type fakePlatformDigestCatalog struct {
	platforms    []string
	platformsErr error
	digestByPlat map[string]string
	fetchErr     map[string]error
}

func (f *fakePlatformDigestCatalog) ListPlatforms(_ context.Context, _ catalog.Reference) ([]string, error) {
	return f.platforms, f.platformsErr
}

func (f *fakePlatformDigestCatalog) FetchByPlatform(_ context.Context, _ catalog.Reference, platform string) ([]byte, catalog.ArtifactInfo, error) {
	if err := f.fetchErr[platform]; err != nil {
		return nil, catalog.ArtifactInfo{}, err
	}
	return []byte("binary"), catalog.ArtifactInfo{Digest: f.digestByPlat[platform]}, nil
}

func TestResolvePlatformDigests_MultiPlatform(t *testing.T) {
	cat := &fakePlatformDigestCatalog{
		platforms: []string{"linux/amd64", "darwin/arm64"},
		digestByPlat: map[string]string{
			"linux/amd64":  "sha256:aaa",
			"darwin/arm64": "sha256:bbb",
		},
	}

	digests, primary, err := resolvePlatformDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"linux/amd64":  "sha256:aaa",
		"darwin/arm64": "sha256:bbb",
	}, digests)
	assert.Equal(t, "sha256:aaa", primary, "primary is the build-platform digest")
}

func TestResolvePlatformDigests_SinglePlatformNilMap(t *testing.T) {
	// ListPlatforms returns empty (single-platform artifact): the map is nil
	// (the invariant marker) and the sole digest is returned as primary.
	cat := &fakePlatformDigestCatalog{
		platforms:    nil,
		digestByPlat: map[string]string{"linux/amd64": "sha256:only"},
	}

	digests, primary, err := resolvePlatformDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64")
	require.NoError(t, err)
	assert.Nil(t, digests, "single-platform must leave Digests nil")
	assert.Equal(t, "sha256:only", primary)
}

func TestResolvePlatformDigests_ListPlatformsError(t *testing.T) {
	cat := &fakePlatformDigestCatalog{platformsErr: errors.New("boom")}

	_, _, err := resolvePlatformDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing platforms")
}

func TestResolvePlatformDigests_FetchError(t *testing.T) {
	cat := &fakePlatformDigestCatalog{
		platforms:    []string{"linux/amd64", "darwin/arm64"},
		digestByPlat: map[string]string{"linux/amd64": "sha256:aaa"},
		fetchErr:     map[string]error{"darwin/arm64": errors.New("network")},
	}

	_, _, err := resolvePlatformDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "darwin/arm64")
}

func TestResolvePlatformDigests_EmptyDigest(t *testing.T) {
	cat := &fakePlatformDigestCatalog{
		platforms:    []string{"linux/amd64"},
		digestByPlat: map[string]string{"linux/amd64": ""}, // catalog returned blank
	}

	_, _, err := resolvePlatformDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty digest")
}

func TestVendorPlugins_ResolvesPerPlatformDigests(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"multi-plat:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "multi-plat",
					Version: semver.MustParse("1.0.0"),
				},
				Digest:  "sha256:index-digest",
				Catalog: "remote",
			},
		},
	}

	fakeCat := &fakePlatformDigestCatalog{
		platforms: []string{"linux/amd64", "darwin/arm64", "linux/arm64"},
		digestByPlat: map[string]string{
			"linux/amd64":  "sha256:aaa",
			"darwin/arm64": "sha256:bbb",
			"linux/arm64":  "sha256:ccc",
		},
	}

	plugins := []solution.PluginDependency{
		{Name: "multi-plat", Kind: solution.PluginKindProvider, Version: "^1.0.0"},
	}

	result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
		PluginResolver:  resolver,
		PlatformCatalog: fakeCat,
		Platform:        "darwin/arm64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	lp := result.ResolvedPlugins[0]
	// Primary digest is the build platform's content digest.
	assert.Equal(t, "sha256:bbb", lp.Digest)
	// All platforms are recorded.
	assert.Equal(t, map[string]string{
		"linux/amd64":  "sha256:aaa",
		"darwin/arm64": "sha256:bbb",
		"linux/arm64":  "sha256:ccc",
	}, lp.Digests)
}

func TestVendorPlugins_SinglePlatformDigests(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"single-plat:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "single-plat",
					Version: semver.MustParse("1.0.0"),
				},
				Digest:  "sha256:manifest-digest",
				Catalog: "remote",
			},
		},
	}

	// ListPlatforms returns empty → single-platform fallback
	fakeCat := &fakePlatformDigestCatalog{
		platforms:    nil,
		digestByPlat: map[string]string{"linux/amd64": "sha256:only-one"},
	}

	plugins := []solution.PluginDependency{
		{Name: "single-plat", Kind: solution.PluginKindProvider, Version: "^1.0.0"},
	}

	result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
		PluginResolver:  resolver,
		PlatformCatalog: fakeCat,
		Platform:        "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	lp := result.ResolvedPlugins[0]
	assert.Equal(t, "sha256:only-one", lp.Digest)
	// Single-platform: Digests stays nil (the invariant marker); the sole
	// digest lives in the primary Digest above.
	assert.Nil(t, lp.Digests, "single-platform must leave Digests nil")
}

func TestVendorPlugins_PlatformDigestFailureFallsBack(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"fallback-plat:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "fallback-plat",
					Version: semver.MustParse("1.0.0"),
				},
				Digest:  "sha256:manifest-level",
				Catalog: "remote",
			},
		},
	}

	// resolvePlatformDigests will fail → graceful fallback to manifest digest
	fakeCat := &fakePlatformDigestCatalog{
		platformsErr: errors.New("registry unavailable"),
	}

	plugins := []solution.PluginDependency{
		{Name: "fallback-plat", Kind: solution.PluginKindProvider, Version: "^1.0.0"},
	}

	result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
		PluginResolver:  resolver,
		PlatformCatalog: fakeCat,
		Platform:        "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	lp := result.ResolvedPlugins[0]
	// Falls back to the manifest-level digest; no per-platform map.
	assert.Equal(t, "sha256:manifest-level", lp.Digest)
	assert.Nil(t, lp.Digests)
}

func TestVendorPlugins_NoPlatformCatalog_NoDigests(t *testing.T) {
	ctx := testContext()

	resolver := &mockPluginResolver{
		plugins: map[string]catalog.ArtifactInfo{
			"no-plat:provider": {
				Reference: catalog.Reference{
					Kind:    catalog.ArtifactKindProvider,
					Name:    "no-plat",
					Version: semver.MustParse("1.0.0"),
				},
				Digest:  "sha256:plain-digest",
				Catalog: "local",
			},
		},
	}

	plugins := []solution.PluginDependency{
		{Name: "no-plat", Kind: solution.PluginKindProvider, Version: "^1.0.0"},
	}

	// No PlatformCatalog set
	result, err := VendorPlugins(ctx, plugins, nil, VendorPluginsOptions{
		PluginResolver: resolver,
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)

	lp := result.ResolvedPlugins[0]
	assert.Equal(t, "sha256:plain-digest", lp.Digest)
	assert.Nil(t, lp.Digests)
}

// fakeSourcedCatalog is a minimal SourcedCatalog for unit-testing
// VendorPluginsFQN and resolveContentDigests without a real OCI registry.
type fakeSourcedCatalog struct {
	// resolveInfo is the base metadata returned by Resolve. When the reference
	// passed to Resolve carries a version (an exact pin or a range-selected
	// version), that version is echoed back on the returned Reference so the
	// resolved version reflects what the selector chose; ImageRef and other
	// fields come from resolveInfo. With a nil reference version, resolveInfo is
	// returned verbatim (the "latest" path).
	resolveInfo  catalog.ArtifactInfo
	resolveErr   error
	resolveCalls int
	resolveRef   catalog.Reference

	// forcedResolveVersion, when set, overrides the echoed version regardless of
	// the requested reference. It simulates a misbehaving catalog that returns a
	// version outside the selected constraint, exercising the post-condition
	// guard.
	forcedResolveVersion *semver.Version

	// listInfos/listErr back the range-constraint path (List). listCalls counts
	// invocations so tests can assert the fast paths (exact/latest) never list.
	listInfos []catalog.ArtifactInfo
	listErr   error
	listCalls int

	platforms    []string
	platformsErr error
	digestByPlat map[string]string
	digestErr    map[string]error
}

func (f *fakeSourcedCatalog) Resolve(_ context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error) {
	f.resolveCalls++
	f.resolveRef = ref
	if f.resolveErr != nil {
		return catalog.ArtifactInfo{}, f.resolveErr
	}
	info := f.resolveInfo
	switch {
	case f.forcedResolveVersion != nil:
		info.Reference = catalog.Reference{Kind: ref.Kind, Name: ref.Name, Version: f.forcedResolveVersion}
	case ref.Version != nil:
		info.Reference = catalog.Reference{Kind: ref.Kind, Name: ref.Name, Version: ref.Version}
	}
	return info, nil
}

func (f *fakeSourcedCatalog) List(_ context.Context, _ catalog.ArtifactKind, _ string) ([]catalog.ArtifactInfo, error) {
	f.listCalls++
	return f.listInfos, f.listErr
}

func (f *fakeSourcedCatalog) ListPlatforms(_ context.Context, _ catalog.Reference) ([]string, error) {
	return f.platforms, f.platformsErr
}

func (f *fakeSourcedCatalog) ResolveContentDigest(_ context.Context, _ catalog.Reference, platform, _ string) (catalog.ContentDigestInfo, error) {
	if err := f.digestErr[platform]; err != nil {
		return catalog.ContentDigestInfo{}, err
	}
	return catalog.ContentDigestInfo{ContentDigest: f.digestByPlat[platform]}, nil
}

// versionInfos builds a []catalog.ArtifactInfo (one per version) for seeding a
// fakeSourcedCatalog's List response.
func versionInfos(kind catalog.ArtifactKind, name string, versions ...string) []catalog.ArtifactInfo { //nolint:unparam // test helper uses constant kind for clarity
	infos := make([]catalog.ArtifactInfo, 0, len(versions))
	for _, v := range versions {
		infos = append(infos, catalog.ArtifactInfo{
			Reference: catalog.Reference{Kind: kind, Name: name, Version: semver.MustParse(v)},
		})
	}
	return infos
}

// newTestAliasResolver builds a catalog index mapping a single catalog alias to
// a registry origin (via its OCI URL).
func newTestAliasResolver(alias, url string) *catalogindex.Index { //nolint:unparam // test helper uses constant alias for clarity
	return catalogindex.FromConfig(&config.Config{
		Catalogs: []config.CatalogConfig{{Name: alias, URL: url}},
	})
}

func TestResolveContentDigests_MultiPlatform(t *testing.T) {
	cat := &fakeSourcedCatalog{
		platforms: []string{"linux/amd64", "darwin/arm64"},
		digestByPlat: map[string]string{
			"linux/amd64":  "sha256:aaa",
			"darwin/arm64": "sha256:bbb",
		},
	}

	digests, primary, err := resolveContentDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64", catalog.MediaTypeProviderBinary)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"linux/amd64":  "sha256:aaa",
		"darwin/arm64": "sha256:bbb",
	}, digests)
	assert.Equal(t, "sha256:aaa", primary, "primary is the build-platform digest")
}

func TestResolveContentDigests_SinglePlatformNilMap(t *testing.T) {
	// ListPlatforms returns empty (single-platform artifact): the map is nil
	// (the invariant marker) and the sole digest is returned as primary.
	cat := &fakeSourcedCatalog{
		platforms:    nil,
		digestByPlat: map[string]string{"linux/amd64": "sha256:only"},
	}

	digests, primary, err := resolveContentDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64", catalog.MediaTypeProviderBinary)
	require.NoError(t, err)
	assert.Nil(t, digests, "single-platform must leave Digests nil")
	assert.Equal(t, "sha256:only", primary)
}

func TestResolveContentDigests_ListPlatformsError(t *testing.T) {
	cat := &fakeSourcedCatalog{platformsErr: errors.New("boom")}

	_, _, err := resolveContentDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64", catalog.MediaTypeProviderBinary)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing platforms")
}

func TestResolveContentDigests_ResolveError(t *testing.T) {
	cat := &fakeSourcedCatalog{
		platforms:    []string{"linux/amd64", "darwin/arm64"},
		digestByPlat: map[string]string{"linux/amd64": "sha256:aaa"},
		digestErr:    map[string]error{"darwin/arm64": errors.New("network")},
	}

	_, _, err := resolveContentDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64", catalog.MediaTypeProviderBinary)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "darwin/arm64")
}

func TestResolveContentDigests_EmptyDigest(t *testing.T) {
	cat := &fakeSourcedCatalog{
		platforms:    []string{"linux/amd64"},
		digestByPlat: map[string]string{"linux/amd64": ""},
	}

	_, _, err := resolveContentDigests(context.Background(), cat, catalog.Reference{}, "linux/amd64", catalog.MediaTypeProviderBinary)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty digest")
}

func TestVendorPluginsFQN_ResolvesFromSourcedCatalog(t *testing.T) {
	ctx := testContext()

	cat := &fakeSourcedCatalog{
		resolveInfo: catalog.ArtifactInfo{
			ImageRef: "ghcr.io/myorg/exec@sha256:aaa",
		},
		// Range constraint lists versions; the selector must pick the highest
		// that satisfies ^1.0.0 (1.2.3, not 1.1.0).
		listInfos: versionInfos(catalog.ArtifactKindProvider, "exec", "1.0.0", "1.1.0", "1.2.3"),
		platforms: []string{"linux/amd64", "darwin/arm64"},
		digestByPlat: map[string]string{
			"linux/amd64":  "sha256:aaa",
			"darwin/arm64": "sha256:bbb",
		},
	}

	// Alias "myorg" maps origin "ghcr.io/myorg" to the sourced catalog.
	plugins := []solution.PluginDependency{
		{
			Name:    "exec", // solution-local alias, NOT stored in the lock
			Kind:    solution.PluginKindProvider,
			Version: "^1.0.0",
			Source:  &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"},
		},
	}

	result, err := VendorPluginsFQN(ctx, plugins, nil, VendorPluginFQNOptions{
		SourcedCatalogs:      map[string]SourcedCatalog{"myorg": cat},
		CatalogAliasResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		Platform:             "darwin/arm64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)
	assert.Equal(t, 1, cat.listCalls, "range constraint must list versions")

	lp := result.ResolvedPlugins[0]
	// Identity is (canonical, leaf, kind): Name is the leaf, not the alias.
	assert.Equal(t, "exec", lp.Name)
	assert.Equal(t, "provider", lp.Kind)
	assert.Equal(t, "ghcr.io/myorg", lp.ResolvedCanonical)
	assert.Equal(t, "myorg", lp.ResolvedFrom)
	assert.Equal(t, "1.2.3", lp.Version)
	assert.Equal(t, "^1.0.0", lp.Constraint)
	// Primary digest is the build platform's content digest.
	assert.Equal(t, "sha256:bbb", lp.Digest)
	assert.Equal(t, map[string]string{
		"linux/amd64":  "sha256:aaa",
		"darwin/arm64": "sha256:bbb",
	}, lp.Digests)
}

func TestVendorPluginsFQN_NilCatalogsSkips(t *testing.T) {
	ctx := testContext()

	result, err := VendorPluginsFQN(ctx, []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "^1.0.0", Source: &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"}},
	}, nil, VendorPluginFQNOptions{})
	require.NoError(t, err)
	assert.Empty(t, result.ResolvedPlugins)
}

func TestVendorPluginsFQN_MissingPlatform(t *testing.T) {
	ctx := testContext()

	_, err := VendorPluginsFQN(ctx, []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "^1.0.0", Source: &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"}},
	}, nil, VendorPluginFQNOptions{
		SourcedCatalogs: map[string]SourcedCatalog{"myorg": &fakeSourcedCatalog{}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target platform")
}

func TestVendorPluginsFQN_UnconfiguredOrigin(t *testing.T) {
	ctx := testContext()

	_, err := VendorPluginsFQN(ctx, []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "^1.0.0", Source: &solution.PluginSource{Registry: "ghcr.io/other", Artifact: "exec"}},
	}, nil, VendorPluginFQNOptions{
		SourcedCatalogs:      map[string]SourcedCatalog{"myorg": &fakeSourcedCatalog{}},
		CatalogAliasResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		Platform:             "linux/amd64",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match any configured catalog")
}

func TestVendorPluginsFQN_NoCatalogForAlias(t *testing.T) {
	ctx := testContext()

	// The origin resolves to alias "myorg", but no catalog is registered for it.
	_, err := VendorPluginsFQN(ctx, []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "^1.0.0", Source: &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"}},
	}, nil, VendorPluginFQNOptions{
		SourcedCatalogs:      map[string]SourcedCatalog{"different": &fakeSourcedCatalog{}},
		CatalogAliasResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		Platform:             "linux/amd64",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no catalog available for alias")
}

func TestVendorPluginsFQN_ReplaysFromLock(t *testing.T) {
	ctx := testContext()

	// A catalog that errors if Resolve is called, proving replay short-circuits.
	cat := &fakeSourcedCatalog{resolveErr: errors.New("should not be called")}

	const registry = "ghcr.io/myorg"
	existingLock := &LockFile{
		Version: LockFileVersion,
		Plugins: []LockPlugin{
			{
				Name:              "exec", // leaf identity
				Kind:              "provider",
				Version:           "1.2.3",
				Constraint:        ">=1.0.0",
				Digest:            "sha256:locked",
				ResolvedCanonical: registry,
				ResolvedFrom:      "myorg",
				Source: &LockPluginSource{
					Registry: registry,
				},
			},
		},
	}

	plugins := []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "^1.0.0", Source: &solution.PluginSource{Registry: registry, Artifact: "exec"}},
	}

	result, err := VendorPluginsFQN(ctx, plugins, existingLock, VendorPluginFQNOptions{
		SourcedCatalogs:      map[string]SourcedCatalog{"myorg": cat},
		CatalogAliasResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		Platform:             "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)
	assert.Equal(t, 0, cat.resolveCalls)

	lp := result.ResolvedPlugins[0]
	assert.Equal(t, "1.2.3", lp.Version)
	assert.Equal(t, "sha256:locked", lp.Digest)
	// Constraint refreshed to the current spec value.
	assert.Equal(t, "^1.0.0", lp.Constraint)
}

func TestVendorPluginsFQN_StaleLockReResolves(t *testing.T) {
	ctx := testContext()

	cat := &fakeSourcedCatalog{
		listInfos:    versionInfos(catalog.ArtifactKindProvider, "exec", "1.0.0", "2.0.0"),
		platforms:    nil,
		digestByPlat: map[string]string{"linux/amd64": "sha256:fresh"},
	}

	// Lock pins 1.0.0, but the constraint now demands ^2.0.0.
	existingLock := &LockFile{
		Version: LockFileVersion,
		Plugins: []LockPlugin{
			{Name: "exec", Kind: "provider", Version: "1.0.0", ResolvedCanonical: "ghcr.io/myorg", ResolvedFrom: "myorg", Source: &LockPluginSource{Registry: "ghcr.io/myorg"}},
		},
	}

	plugins := []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "^2.0.0", Source: &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"}},
	}

	result, err := VendorPluginsFQN(ctx, plugins, existingLock, VendorPluginFQNOptions{
		SourcedCatalogs:      map[string]SourcedCatalog{"myorg": cat},
		CatalogAliasResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		Platform:             "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)
	assert.Equal(t, 1, cat.resolveCalls)
	assert.Equal(t, "2.0.0", result.ResolvedPlugins[0].Version)
	assert.Equal(t, "sha256:fresh", result.ResolvedPlugins[0].Digest)
}

// TestVendorPluginsFQN_ConstraintViolation exercises the post-condition guard:
// the selector picks a satisfying version, but a misbehaving catalog resolves a
// version outside the constraint. Vendoring must reject it rather than pin it.
func TestVendorPluginsFQN_ConstraintViolation(t *testing.T) {
	ctx := testContext()

	cat := &fakeSourcedCatalog{
		// A satisfying version exists and is selected...
		listInfos: versionInfos(catalog.ArtifactKindProvider, "exec", "1.5.0"),
		// ...but Resolve returns 2.0.0, which violates ^1.0.0.
		forcedResolveVersion: semver.MustParse("2.0.0"),
	}

	plugins := []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "^1.0.0", Source: &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"}},
	}

	_, err := VendorPluginsFQN(ctx, plugins, nil, VendorPluginFQNOptions{
		SourcedCatalogs:      map[string]SourcedCatalog{"myorg": cat},
		CatalogAliasResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		Platform:             "linux/amd64",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not satisfy constraint")
}

func TestVendorPluginsFQN_SignatureVerification(t *testing.T) {
	ctx := testContext()

	cat := &fakeSourcedCatalog{
		resolveInfo: catalog.ArtifactInfo{
			ImageRef: "ghcr.io/myorg/exec@sha256:aaa",
		},
		listInfos:    versionInfos(catalog.ArtifactKindProvider, "exec", "1.0.0"),
		platforms:    nil,
		digestByPlat: map[string]string{"linux/amd64": "sha256:aaa"},
	}

	plugins := []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "^1.0.0", Source: &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"}},
	}

	t.Run("failure aborts vendoring", func(t *testing.T) {
		_, err := VendorPluginsFQN(ctx, plugins, nil, VendorPluginFQNOptions{
			SourcedCatalogs:      map[string]SourcedCatalog{"myorg": cat},
			CatalogAliasResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
			Platform:             "linux/amd64",
			VerifySignature: func(_ context.Context, _ string) (*LockPluginSignature, error) {
				return nil, errors.New("untrusted signer")
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "signature verification failed during lock")
	})

	t.Run("metadata recorded on success", func(t *testing.T) {
		result, err := VendorPluginsFQN(ctx, plugins, nil, VendorPluginFQNOptions{
			SourcedCatalogs:      map[string]SourcedCatalog{"myorg": cat},
			CatalogAliasResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
			Platform:             "linux/amd64",
			VerifySignature: func(_ context.Context, _ string) (*LockPluginSignature, error) {
				return &LockPluginSignature{Issuer: "https://token.actions.githubusercontent.com"}, nil
			},
		})
		require.NoError(t, err)
		require.Len(t, result.ResolvedPlugins, 1)
		require.NotNil(t, result.ResolvedPlugins[0].Signature)
		assert.Equal(t, "https://token.actions.githubusercontent.com", result.ResolvedPlugins[0].Signature.Issuer)
	})
}

// TestVendorPluginsFQN_ExactVersionPinsTag verifies that an exact version pins
// that tag directly -- no version listing, and the requested version is what
// gets resolved even when newer versions exist in the catalog.
func TestVendorPluginsFQN_ExactVersionPinsTag(t *testing.T) {
	ctx := testContext()

	cat := &fakeSourcedCatalog{
		resolveInfo: catalog.ArtifactInfo{ImageRef: "ghcr.io/myorg/exec@sha256:pinned"},
		// List would offer a newer version, but an exact pin must not consult it.
		listInfos:    versionInfos(catalog.ArtifactKindProvider, "exec", "1.2.3", "9.9.9"),
		platforms:    nil,
		digestByPlat: map[string]string{"linux/amd64": "sha256:pinned"},
	}

	plugins := []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "1.2.3", Source: &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"}},
	}

	result, err := VendorPluginsFQN(ctx, plugins, nil, VendorPluginFQNOptions{
		SourcedCatalogs:      map[string]SourcedCatalog{"myorg": cat},
		CatalogAliasResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		Platform:             "linux/amd64",
	})
	require.NoError(t, err)
	require.Len(t, result.ResolvedPlugins, 1)
	assert.Equal(t, 0, cat.listCalls, "exact version must not list versions")
	assert.Equal(t, "1.2.3", result.ResolvedPlugins[0].Version)
	// The reference passed to Resolve carried the exact pinned version.
	require.NotNil(t, cat.resolveRef.Version)
	assert.Equal(t, "1.2.3", cat.resolveRef.Version.String())
}

// TestVendorPluginsFQN_LatestResolvesNewest verifies that an empty or "latest"
// constraint leaves the reference version nil (Resolve picks the newest) and
// never lists versions.
func TestVendorPluginsFQN_LatestResolvesNewest(t *testing.T) {
	ctx := testContext()

	for _, constraint := range []string{"", "latest", "LATEST"} {
		t.Run("constraint="+constraint, func(t *testing.T) {
			cat := &fakeSourcedCatalog{
				resolveInfo: catalog.ArtifactInfo{
					Reference: catalog.Reference{
						Kind:    catalog.ArtifactKindProvider,
						Name:    "exec",
						Version: semver.MustParse("3.1.0"),
					},
				},
				platforms:    nil,
				digestByPlat: map[string]string{"linux/amd64": "sha256:newest"},
			}

			plugins := []solution.PluginDependency{
				{Name: "exec", Kind: solution.PluginKindProvider, Version: constraint, Source: &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"}},
			}

			result, err := VendorPluginsFQN(ctx, plugins, nil, VendorPluginFQNOptions{
				SourcedCatalogs:      map[string]SourcedCatalog{"myorg": cat},
				CatalogAliasResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
				Platform:             "linux/amd64",
			})
			require.NoError(t, err)
			require.Len(t, result.ResolvedPlugins, 1)
			assert.Equal(t, 0, cat.listCalls, "latest must not list versions")
			assert.Nil(t, cat.resolveRef.Version, "latest must resolve with a nil version")
			assert.Equal(t, "3.1.0", result.ResolvedPlugins[0].Version)
		})
	}
}

// TestVendorPluginsFQN_RangeNoSatisfyingVersion verifies a clear error when no
// published version satisfies a range constraint.
func TestVendorPluginsFQN_RangeNoSatisfyingVersion(t *testing.T) {
	ctx := testContext()

	cat := &fakeSourcedCatalog{
		listInfos: versionInfos(catalog.ArtifactKindProvider, "exec", "1.0.0", "1.5.0"),
	}

	plugins := []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "^2.0.0", Source: &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"}},
	}

	_, err := VendorPluginsFQN(ctx, plugins, nil, VendorPluginFQNOptions{
		SourcedCatalogs:      map[string]SourcedCatalog{"myorg": cat},
		CatalogAliasResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		Platform:             "linux/amd64",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no published version of")
	assert.Contains(t, err.Error(), "satisfies constraint")
	assert.Equal(t, 0, cat.resolveCalls, "must not resolve when nothing satisfies")
}

// TestVendorPluginsFQN_ListErrorPropagates verifies that a version-listing
// failure aborts vendoring with context.
func TestVendorPluginsFQN_ListErrorPropagates(t *testing.T) {
	ctx := testContext()

	cat := &fakeSourcedCatalog{listErr: errors.New("registry unreachable")}

	plugins := []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider, Version: "~1.2", Source: &solution.PluginSource{Registry: "ghcr.io/myorg", Artifact: "exec"}},
	}

	_, err := VendorPluginsFQN(ctx, plugins, nil, VendorPluginFQNOptions{
		SourcedCatalogs:      map[string]SourcedCatalog{"myorg": cat},
		CatalogAliasResolver: newTestAliasResolver("myorg", "oci://ghcr.io/myorg"),
		Platform:             "linux/amd64",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing versions")
	assert.Contains(t, err.Error(), "registry unreachable")
}
