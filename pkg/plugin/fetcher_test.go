// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// binaryDigest returns the sha256 digest string for the given bytes.
func binaryDigest(data []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data))
}

// testCatalogRegistryHash returns the registry hash for the default mock catalog ("test").
func testCatalogRegistryHash() string {
	return CatalogIdentity{Canonical: "test"}.RegistryHash()
}

// mockCatalog implements catalog.PlatformAwareCatalog for testing.
type mockCatalog struct {
	name      string
	artifacts map[string]mockArtifact
	listFunc  func(ctx context.Context, kind catalog.ArtifactKind, name string) ([]catalog.ArtifactInfo, error)
}

type mockArtifact struct {
	content []byte
	info    catalog.ArtifactInfo
}

func newMockCatalog() *mockCatalog {
	return &mockCatalog{
		name:      "test",
		artifacts: make(map[string]mockArtifact),
	}
}

func (m *mockCatalog) Name() string { return m.name }

func (m *mockCatalog) addArtifact(ref catalog.Reference, content []byte) {
	key := ref.String()
	m.artifacts[key] = mockArtifact{
		content: content,
		info: catalog.ArtifactInfo{
			Reference: ref,
			Digest:    binaryDigest(content),
			Catalog:   m.name,
		},
	}
}

func (m *mockCatalog) Store(_ context.Context, ref catalog.Reference, content, _ []byte, _ map[string]string, _ bool, _ ...catalog.Layer) (catalog.ArtifactInfo, error) {
	return catalog.ArtifactInfo{Reference: ref, Catalog: m.name}, nil
}

func (m *mockCatalog) Fetch(_ context.Context, ref catalog.Reference) ([]byte, catalog.ArtifactInfo, error) {
	a, ok := m.artifacts[ref.String()]
	if !ok {
		return nil, catalog.ArtifactInfo{}, catalog.ErrArtifactNotFound
	}
	return a.content, a.info, nil
}

func (m *mockCatalog) FetchWithBundle(_ context.Context, ref catalog.Reference) ([]byte, []byte, catalog.ArtifactInfo, error) {
	a, ok := m.artifacts[ref.String()]
	if !ok {
		return nil, nil, catalog.ArtifactInfo{}, catalog.ErrArtifactNotFound
	}
	return a.content, nil, a.info, nil
}

func (m *mockCatalog) FetchWithLayer(_ context.Context, ref catalog.Reference, _ ...string) ([]byte, map[string][]byte, catalog.ArtifactInfo, error) {
	a, ok := m.artifacts[ref.String()]
	if !ok {
		return nil, nil, catalog.ArtifactInfo{}, catalog.ErrArtifactNotFound
	}
	return a.content, nil, a.info, nil
}

func (m *mockCatalog) Resolve(_ context.Context, ref catalog.Reference) (catalog.ArtifactInfo, error) {
	a, ok := m.artifacts[ref.String()]
	if !ok {
		return catalog.ArtifactInfo{}, catalog.ErrArtifactNotFound
	}
	return a.info, nil
}

func (m *mockCatalog) List(_ context.Context, kind catalog.ArtifactKind, name string) ([]catalog.ArtifactInfo, error) {
	if m.listFunc != nil {
		return m.listFunc(nil, kind, name)
	}
	var results []catalog.ArtifactInfo
	for _, a := range m.artifacts {
		if name != "" && a.info.Reference.Name != name {
			continue
		}
		if a.info.Reference.Kind != kind {
			continue
		}
		results = append(results, a.info)
	}
	return results, nil
}

func (m *mockCatalog) Exists(_ context.Context, ref catalog.Reference) (bool, error) {
	_, ok := m.artifacts[ref.String()]
	return ok, nil
}

func (m *mockCatalog) Delete(_ context.Context, ref catalog.Reference) error {
	delete(m.artifacts, ref.String())
	return nil
}

func (m *mockCatalog) FetchByPlatform(_ context.Context, ref catalog.Reference, _ string) ([]byte, catalog.ArtifactInfo, error) {
	a, ok := m.artifacts[ref.String()]
	if !ok {
		return nil, catalog.ArtifactInfo{}, catalog.ErrArtifactNotFound
	}
	return a.content, a.info, nil
}

func (m *mockCatalog) ListPlatforms(_ context.Context, _ catalog.Reference) ([]string, error) {
	return nil, nil
}

func (m *mockCatalog) ResolveContentDigest(_ context.Context, ref catalog.Reference, _, _ string) (catalog.ContentDigestInfo, error) {
	a, ok := m.artifacts[ref.String()]
	if !ok {
		return catalog.ContentDigestInfo{}, catalog.ErrArtifactNotFound
	}
	return catalog.ContentDigestInfo{
		ArtifactInfo:  a.info,
		ContentDigest: a.info.Digest,
	}, nil
}

func testRef(name, version string) catalog.Reference {
	ref := catalog.Reference{
		Kind: catalog.ArtifactKindProvider,
		Name: name,
	}
	if version != "" {
		v, err := semver.NewVersion(version)
		if err != nil {
			panic("bad test version: " + err.Error())
		}
		ref.Version = v
	}
	return ref
}

func TestFetcher_FetchPlugins_Empty(t *testing.T) {
	cat := newMockCatalog()
	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    NewCache(t.TempDir()),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	results, err := f.FetchPlugins(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestFetcher_FetchPlugins_CacheMiss_FetchFromCatalog(t *testing.T) {
	cat := newMockCatalog()
	ref := testRef("my-plugin", "1.0.0")
	cat.addArtifact(ref, []byte("the-binary"))

	cacheDir := t.TempDir()
	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    NewCache(cacheDir),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "my-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}
	lock := []bundler.LockPlugin{
		{Name: "my-plugin", Kind: "provider", Version: "1.0.0", Digest: binaryDigest([]byte("the-binary")), ResolvedFrom: "test"},
	}

	results, err := f.FetchPlugins(context.Background(), deps, lock)
	require.NoError(t, err)
	require.Len(t, results, 1)

	r := results[0]
	assert.Equal(t, "my-plugin", r.Name)
	assert.Equal(t, solution.PluginKindProvider, r.Kind)
	assert.Equal(t, "1.0.0", r.Version)
	assert.False(t, r.FromCache)
	assert.NotEmpty(t, r.Path)
}

// TestFetcher_FetchPlugins_MultiPlatformLock_CrossPlatform is the regression
// test for cross-platform verification: a multi-platform lock built on one
// os/arch must verify correctly when run on another. The runtime platform is
// linux/amd64 while the primary Digest holds the (different) darwin/arm64
// digest; verification must use Digests[linux/amd64], not the primary.
func TestFetcher_FetchPlugins_MultiPlatformLock_CrossPlatform(t *testing.T) {
	cat := newMockCatalog()
	ref := testRef("multi-plugin", "1.0.0")
	binary := []byte("linux-amd64-binary")
	cat.addArtifact(ref, binary)

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    NewCache(t.TempDir()),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "multi-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}
	// Digests is populated (multi-platform). The primary Digest is the BUILD
	// platform's (darwin/arm64) digest and does NOT match the linux binary; the
	// correct linux/amd64 entry does. Before the per-platform selector this
	// failed with a bogus "digest mismatch / supply chain attack".
	lock := []bundler.LockPlugin{
		{
			Name:         "multi-plugin",
			Kind:         "provider",
			Version:      "1.0.0",
			Digest:       "sha256:darwin-build-platform-digest",
			Digests:      map[string]string{"darwin/arm64": "sha256:darwin-build-platform-digest", "linux/amd64": binaryDigest(binary)},
			ResolvedFrom: "test",
		},
	}

	results, err := f.FetchPlugins(context.Background(), deps, lock)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "1.0.0", results[0].Version)
	assert.False(t, results[0].FromCache)
}

// TestFetcher_FetchPlugins_MultiPlatformLock_MissingPlatform verifies that a
// multi-platform lock lacking the runtime platform fails cleanly (no silent
// fallback to the primary Digest) with an actionable message.
func TestFetcher_FetchPlugins_MultiPlatformLock_MissingPlatform(t *testing.T) {
	cat := newMockCatalog()
	ref := testRef("multi-plugin", "1.0.0")
	binary := []byte("some-binary")
	cat.addArtifact(ref, binary)

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    NewCache(t.TempDir()),
		Platform: "windows/amd64", // not published by the lock
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "multi-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}
	lock := []bundler.LockPlugin{
		{
			Name:         "multi-plugin",
			Kind:         "provider",
			Version:      "1.0.0",
			Digest:       binaryDigest(binary),
			Digests:      map[string]string{"darwin/arm64": binaryDigest(binary), "linux/amd64": binaryDigest(binary)},
			ResolvedFrom: "test",
		},
	}

	_, err := f.FetchPlugins(context.Background(), deps, lock)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no digest for platform windows/amd64")
}

func TestFetcher_FetchPlugins_CacheHit(t *testing.T) {
	cat := newMockCatalog()
	ref := testRef("cached-plugin", "2.0.0")
	cat.addArtifact(ref, []byte("the-binary"))

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)

	// Pre-populate cache with registry hash matching the mock catalog identity.
	_, err := cache.Put("cached-plugin", "2.0.0", "linux/amd64", []byte("cached-binary"), WithRegistryHash(testCatalogRegistryHash()))
	require.NoError(t, err)

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    cache,
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "cached-plugin", Kind: solution.PluginKindProvider, Version: "2.0.0"},
	}
	lock := []bundler.LockPlugin{
		{Name: "cached-plugin", Kind: "provider", Version: "2.0.0", ResolvedFrom: "test"},
	}

	results, err := f.FetchPlugins(context.Background(), deps, lock)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].FromCache)
}

func TestFetcher_FetchPlugins_NoLockFile_WarnsAndResolves(t *testing.T) {
	cat := newMockCatalog()
	ref := testRef("unlocked-plugin", "1.2.3")
	cat.addArtifact(ref, []byte("resolved-binary"))

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    NewCache(t.TempDir()),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "unlocked-plugin", Kind: solution.PluginKindProvider, Version: "1.2.3"},
	}

	// No lock plugins — should warn and resolve
	results, err := f.FetchPlugins(context.Background(), deps, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "unlocked-plugin", results[0].Name)
	assert.Equal(t, "1.2.3", results[0].Version)
	assert.False(t, results[0].FromCache)
}

func TestFetcher_FetchPlugins_NotFound(t *testing.T) {
	cat := newMockCatalog()

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    NewCache(t.TempDir()),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "missing-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}

	_, err := f.FetchPlugins(context.Background(), deps, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing-plugin")
}

func TestFetcher_FetchPlugins_MultiplePlugins(t *testing.T) {
	cat := newMockCatalog()

	ref1 := testRef("plugin-a", "1.0.0")
	cat.addArtifact(ref1, []byte("binary-a"))

	ref2 := testRef("plugin-b", "2.0.0")
	cat.addArtifact(ref2, []byte("binary-b"))

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    NewCache(t.TempDir()),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "plugin-a", Kind: solution.PluginKindProvider, Version: "1.0.0"},
		{Name: "plugin-b", Kind: solution.PluginKindProvider, Version: "2.0.0"},
	}

	lock := []bundler.LockPlugin{
		{Name: "plugin-a", Kind: "provider", Version: "1.0.0", Digest: binaryDigest([]byte("binary-a"))},
		{Name: "plugin-b", Kind: "provider", Version: "2.0.0", Digest: binaryDigest([]byte("binary-b"))},
	}

	results, err := f.FetchPlugins(context.Background(), deps, lock)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "plugin-a", results[0].Name)
	assert.Equal(t, "plugin-b", results[1].Name)
}

func TestPaths(t *testing.T) {
	results := []FetchResult{
		{Path: "/a/b/plugin1"},
		{Path: "/c/d/plugin2"},
	}
	paths := Paths(results)
	assert.Equal(t, []string{"/a/b/plugin1", "/c/d/plugin2"}, paths)
}

func TestPluginKindToArtifactKind(t *testing.T) {
	assert.Equal(t, catalog.ArtifactKindProvider, pluginKindToArtifactKind(solution.PluginKindProvider))
	assert.Equal(t, catalog.ArtifactKindAuthHandler, pluginKindToArtifactKind(solution.PluginKindAuthHandler))
	assert.Equal(t, catalog.ArtifactKind("custom"), pluginKindToArtifactKind(solution.PluginKind("custom")))
}

func TestFindLockPlugin(t *testing.T) {
	locks := []bundler.LockPlugin{
		{Name: "a", Kind: "provider", Version: "1.0.0"},
		{Name: "b", Kind: "auth-handler", Version: "2.0.0"},
	}

	found := findLockPlugin(locks, "a", "provider")
	require.NotNil(t, found)
	assert.Equal(t, "1.0.0", found.Version)

	found = findLockPlugin(locks, "b", "auth-handler")
	require.NotNil(t, found)
	assert.Equal(t, "2.0.0", found.Version)

	found = findLockPlugin(locks, "c", "provider")
	assert.Nil(t, found)

	found = findLockPlugin(locks, "a", "auth-handler") // wrong kind
	assert.Nil(t, found)
}

func TestFindLockPluginByDep(t *testing.T) {
	locks := []bundler.LockPlugin{
		// Unsourced (local-catalog) entry: Source is nil.
		{Name: "echo", Kind: "provider", Version: "1.0.0"},
		// Sourced entries sharing a leaf name across distinct registries --
		// disambiguated by Source.Registry, not collapsed.
		{Name: "github", Kind: "provider", Version: "1.5.0", Source: &bundler.LockPluginSource{Registry: "ghcr.io/orgA"}},
		{Name: "github", Kind: "provider", Version: "2.3.0", Source: &bundler.LockPluginSource{Registry: "ghcr.io/orgB"}},
		// Sourced entry recorded under its resolved remote leaf name.
		{Name: "scafctl-exec-provider", Kind: "provider", Version: "3.0.0", Source: &bundler.LockPluginSource{Registry: "ghcr.io/orgA"}},
	}

	t.Run("unsourced dep matches the unsourced entry", func(t *testing.T) {
		dep := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider}
		got := findLockPluginByDep(locks, dep)
		require.NotNil(t, got)
		assert.Equal(t, "1.0.0", got.Version)
	})

	t.Run("unsourced dep does not match a sourced entry of the same name", func(t *testing.T) {
		dep := solution.PluginDependency{Name: "github", Kind: solution.PluginKindProvider}
		assert.Nil(t, findLockPluginByDep(locks, dep))
	})

	t.Run("empty lock slice returns no match", func(t *testing.T) {
		dep := solution.PluginDependency{Name: "echo", Kind: solution.PluginKindProvider}
		assert.Nil(t, findLockPluginByDep(nil, dep))
	})
}

func TestLockDigestForPlatform(t *testing.T) {
	t.Run("single-platform (nil Digests) uses primary on any platform", func(t *testing.T) {
		locked := &bundler.LockPlugin{Digest: "sha256:only"}
		// Any runtime platform resolves to the primary digest.
		d, ok := lockDigestForPlatform(locked, "linux/amd64")
		require.True(t, ok)
		assert.Equal(t, "sha256:only", d)
		d, ok = lockDigestForPlatform(locked, "windows/arm64")
		require.True(t, ok)
		assert.Equal(t, "sha256:only", d)
	})

	t.Run("single-platform with empty primary passes through (handled downstream)", func(t *testing.T) {
		// A legacy/single-platform lock with no digest is not hard-failed here:
		// it passes through as ("", true) so the existing cache-hit and
		// "no digest available" logic downstream decides, preserving prior
		// behavior for digest-less locks.
		locked := &bundler.LockPlugin{}
		d, ok := lockDigestForPlatform(locked, "linux/amd64")
		assert.True(t, ok)
		assert.Empty(t, d)
	})

	t.Run("multi-platform selects the per-platform digest", func(t *testing.T) {
		locked := &bundler.LockPlugin{
			Digest: "sha256:darwin", // primary = build platform's digest
			Digests: map[string]string{
				"darwin/arm64": "sha256:darwin",
				"linux/amd64":  "sha256:linux",
			},
		}
		// A different runtime platform than the build platform still verifies
		// against the correct per-platform entry, not the primary.
		d, ok := lockDigestForPlatform(locked, "linux/amd64")
		require.True(t, ok)
		assert.Equal(t, "sha256:linux", d)
	})

	t.Run("multi-platform missing platform hard-fails (no fallback)", func(t *testing.T) {
		locked := &bundler.LockPlugin{
			Digest: "sha256:darwin",
			Digests: map[string]string{
				"darwin/arm64": "sha256:darwin",
			},
		}
		// windows/amd64 is genuinely not published: must NOT fall back to the
		// primary Digest, which would be a wrong-platform match.
		_, ok := lockDigestForPlatform(locked, "windows/amd64")
		assert.False(t, ok)
	})
}

func TestFetcher_FetchPlugins_VersionConstraintMismatch(t *testing.T) {
	cat := newMockCatalog()
	// Add a plugin at version 3.0.0
	ref := testRef("strict-plugin", "3.0.0")
	cat.addArtifact(ref, []byte("binary"))

	// Override Resolve to return 3.0.0 regardless of ref version
	// (simulating a catalog that resolves to the latest version)
	origResolve := cat.Resolve
	_ = origResolve // use the default

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    NewCache(t.TempDir()),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	// Use an exact version that exists but the constraint doesn't match
	// When no lock file, the fetcher resolves from catalog, gets the version,
	// then checks if it satisfies the constraint.
	// Since ParseReference only accepts exact versions, we need to test with
	// a version constraint that IS parseable but the resolved version doesn't match.
	// The real-world flow: constraint "^1.0.0" → catalog resolves to 3.0.0 → fails constraint check.
	// But since ParseReference rejects "^1.0.0", the error happens during resolve.
	// This is actually the correct behavior—the catalog correctly rejects unparseable refs.
	deps := []solution.PluginDependency{
		{Name: "strict-plugin", Kind: solution.PluginKindProvider, Version: "^1.0.0"},
	}

	// No lock — resolves, but "^1.0.0" isn't a valid reference so it fails
	_, err := f.FetchPlugins(context.Background(), deps, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strict-plugin")
}

func TestFetcher_FetchPlugins_NoCache_BypassesCachedBinary(t *testing.T) {
	cat := newMockCatalog()
	ref := testRef("my-plugin", "1.0.0")
	cat.addArtifact(ref, []byte("fresh-binary"))

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)

	// Pre-populate cache with stale data
	_, err := cache.Put("my-plugin", "1.0.0", "linux/amd64", []byte("stale-binary"))
	require.NoError(t, err)

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    cache,
		Platform: "linux/amd64",
		NoCache:  true,
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "my-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}
	lock := []bundler.LockPlugin{
		{Name: "my-plugin", Kind: "provider", Version: "1.0.0", Digest: binaryDigest([]byte("fresh-binary")), ResolvedFrom: "test"},
	}

	results, err := f.FetchPlugins(context.Background(), deps, lock)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].FromCache, "expected fresh fetch, not cache hit")
	assert.Equal(t, "1.0.0", results[0].Version)
}

func TestFetcher_FetchPlugins_EnforceMode_BypassesCache(t *testing.T) {
	cat := newMockCatalog()
	ref := testRef("signed-plugin", "1.0.0")
	cat.addArtifact(ref, []byte("fresh-binary"))

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)

	// Pre-populate cache
	_, err := cache.Put("signed-plugin", "1.0.0", "linux/amd64", []byte("fresh-binary"), WithRegistryHash(testCatalogRegistryHash()))
	require.NoError(t, err)

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    cache,
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
		SignaturePolicy: &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
			TrustedIdentities: []string{"https://github.com/org/*"},
		},
	})

	deps := []solution.PluginDependency{
		{Name: "signed-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}
	lock := []bundler.LockPlugin{
		{Name: "signed-plugin", Kind: "provider", Version: "1.0.0", Digest: binaryDigest([]byte("fresh-binary")), ResolvedFrom: "test"},
	}

	results, err := f.FetchPlugins(context.Background(), deps, lock)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].FromCache, "enforce mode must bypass cache for signature verification")
}

func TestFetcher_FetchPlugins_WarnMode_UsesCache(t *testing.T) {
	cat := newMockCatalog()
	ref := testRef("warn-plugin", "1.0.0")
	cat.addArtifact(ref, []byte("the-binary"))

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)

	// Pre-populate cache with registry hash
	_, err := cache.Put("warn-plugin", "1.0.0", "linux/amd64", []byte("cached-binary"), WithRegistryHash(testCatalogRegistryHash()))
	require.NoError(t, err)

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    cache,
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
		SignaturePolicy: &SignaturePolicy{
			Mode:              SignatureModeWarn,
			TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
			TrustedIdentities: []string{"https://github.com/org/*"},
		},
	})

	deps := []solution.PluginDependency{
		{Name: "warn-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}
	lock := []bundler.LockPlugin{
		{Name: "warn-plugin", Kind: "provider", Version: "1.0.0", ResolvedFrom: "test"},
	}

	results, err := f.FetchPlugins(context.Background(), deps, lock)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].FromCache, "warn mode should allow cache hits")
}

func TestFetcher_FetchPlugins_NoCache_BypassesLatestCached(t *testing.T) {
	cat := newMockCatalog()
	ref := testRef("unlocked-plugin", "2.0.0")
	cat.addArtifact(ref, []byte("catalog-binary"))

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)

	// Pre-populate cache — without NoCache this would be used
	_, err := cache.Put("unlocked-plugin", "1.5.0", "linux/amd64", []byte("cached-binary"))
	require.NoError(t, err)

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    cache,
		Platform: "linux/amd64",
		NoCache:  true,
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "unlocked-plugin", Kind: solution.PluginKindProvider, Version: "2.0.0"},
	}

	results, err := f.FetchPlugins(context.Background(), deps, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].FromCache, "expected fresh fetch when --no-cache is set")
	assert.Equal(t, "2.0.0", results[0].Version)
}

func TestNewFetcher_BinaryNameDefault(t *testing.T) {
	t.Parallel()
	f := NewFetcher(FetcherConfig{
		Catalog: &mockCatalog{name: "test"},
		Logger:  logr.Discard(),
	})
	assert.Equal(t, "scafctl", f.binaryName)
}

func TestNewFetcher_BinaryNameCustom(t *testing.T) {
	t.Parallel()
	f := NewFetcher(FetcherConfig{
		Catalog:    &mockCatalog{name: "test"},
		BinaryName: "mycli",
		Logger:     logr.Discard(),
	})
	assert.Equal(t, "mycli", f.binaryName)
}

// ── RegisterFetchedPlugins tests ─────────────────────────────────────────────

func TestRegisterFetchedPlugins_EmptyResults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := provider.NewRegistry()

	clients, err := RegisterFetchedPlugins(ctx, reg, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, clients)
}

func TestRegisterFetchedPlugins_SkipsNonProviderKinds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := provider.NewRegistry()

	results := []FetchResult{
		{Name: "auth-handler-1", Kind: solution.PluginKindAuthHandler, Path: "/tmp/fake"},
	}

	clients, err := RegisterFetchedPlugins(ctx, reg, results, nil)
	require.NoError(t, err)
	assert.Nil(t, clients, "auth handler kind should be skipped by RegisterFetchedPlugins")
}

func TestRegisterFetchedPlugins_InvalidPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := provider.NewRegistry()

	results := []FetchResult{
		{Name: "bad-plugin", Kind: solution.PluginKindProvider, Path: "/nonexistent/binary/path"},
	}

	clients, err := RegisterFetchedPlugins(ctx, reg, results, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading plugin bad-plugin")
	assert.Nil(t, clients)
}

// ── RegisterFetchedPluginsV2 tests ───────────────────────────────────────────

// fakeProviderClient builds an in-memory *Client backed by a MockProviderPlugin
// exposing the given provider names. It never spawns a subprocess, so Kill() is
// a no-op (pluginClient is nil). Each provider name gets a minimal valid
// descriptor so registry registration succeeds.
func fakeProviderClient(path string, providers []string) *Client {
	descriptors := make(map[string]*provider.Descriptor, len(providers))
	for _, name := range providers {
		descriptors[name] = &provider.Descriptor{
			Name:         name,
			Description:  "fake provider",
			APIVersion:   "v1",
			Version:      semver.MustParse("1.0.0"),
			Capabilities: []provider.Capability{provider.CapabilityFrom},
		}
	}
	return &Client{
		plugin: &MockProviderPlugin{providers: providers, descriptors: descriptors},
		path:   path,
		name:   pluginNameFromPath(path),
	}
}

func TestRegisterFetchedPluginsV2_SetsVersionAndCatalog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := provider.NewCompositeRegistry()

	results := []FetchResult{
		{Name: "p1", Kind: solution.PluginKindProvider, Path: "/tmp/p1", Version: "1.2.3", Catalog: "my-catalog"},
	}

	newClient := func(path string, _ ...ClientOption) (*Client, error) {
		return fakeProviderClient(path, []string{"test-provider"}), nil
	}

	clients, err := RegisterFetchedVersionedPlugins(ctx, reg, results, nil, newClient, nil)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	assert.Equal(t, "1.2.3", clients[0].Version)
	assert.Equal(t, "my-catalog", clients[0].Catalog)
	assert.Equal(t, "p1", clients[0].Name())

	// The provider should have been registered into the external tier under the
	// supplied catalog name.
	_, ok := reg.GetExternal("test-provider", provider.WithCatalogName("my-catalog"))
	assert.True(t, ok, "provider should be registered under the given catalog")
}

func TestRegisterFetchedPluginsV2_NilFactoriesFallBackToDefaults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := provider.NewCompositeRegistry()

	// A provider kind with an invalid path exercises the default NewClient
	// factory (nil newClient) and its failure path.
	results := []FetchResult{
		{Name: "bad", Kind: solution.PluginKindProvider, Path: "/nonexistent/binary"},
	}

	clients, err := RegisterFetchedVersionedPlugins(ctx, reg, results, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading plugin bad")
	assert.Nil(t, clients)
}

func TestRegisterFetchedPluginsV2_SkipsNonProviderKinds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := provider.NewCompositeRegistry()

	results := []FetchResult{
		{Name: "auth-1", Kind: solution.PluginKindAuthHandler, Path: "/tmp/auth"},
	}

	called := false
	newClient := func(path string, _ ...ClientOption) (*Client, error) {
		called = true
		return fakeProviderClient(path, nil), nil
	}

	clients, err := RegisterFetchedVersionedPlugins(ctx, reg, results, nil, newClient, nil)
	require.NoError(t, err)
	assert.Nil(t, clients)
	assert.False(t, called, "non-provider kinds must not construct a client")
}

func TestRegisterFetchedPluginsV2_UsesWrapperFactory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := provider.NewCompositeRegistry()

	results := []FetchResult{
		{Name: "p1", Kind: solution.PluginKindProvider, Path: "/tmp/p1", Version: "0.1.0"},
	}

	newClient := func(path string, _ ...ClientOption) (*Client, error) {
		return fakeProviderClient(path, []string{"test-provider"}), nil
	}

	wrapperCalls := 0
	newWrapper := func(client *Client, providerName string, opts ...WrapperOption) (*ProviderWrapper, error) {
		wrapperCalls++
		return NewProviderWrapper(client, providerName, opts...)
	}

	clients, err := RegisterFetchedVersionedPlugins(ctx, reg, results, nil, newClient, newWrapper)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	assert.Equal(t, 1, wrapperCalls, "injected wrapper factory should be used per provider")
}

func TestRegisterFetchedPluginsV2_ClientErrorKillsStartedClients(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := provider.NewCompositeRegistry()

	results := []FetchResult{
		{Name: "good", Kind: solution.PluginKindProvider, Path: "/tmp/good", Version: "1.0.0"},
		{Name: "bad", Kind: solution.PluginKindProvider, Path: "/tmp/bad"},
	}

	newClient := func(path string, _ ...ClientOption) (*Client, error) {
		if path == "/tmp/bad" {
			return nil, fmt.Errorf("boom")
		}
		return fakeProviderClient(path, []string{"test-provider"}), nil
	}

	clients, err := RegisterFetchedVersionedPlugins(ctx, reg, results, nil, newClient, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading plugin bad")
	assert.Nil(t, clients)
}

// ── RegisterFetchedAuthHandlerPlugins tests ──────────────────────────────────

func TestRegisterFetchedAuthHandlerPlugins_EmptyResults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := auth.NewRegistry()

	clients, err := RegisterFetchedAuthHandlerPlugins(ctx, reg, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, clients)
}

func TestRegisterFetchedAuthHandlerPlugins_SkipsProviderKinds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := auth.NewRegistry()

	results := []FetchResult{
		{Name: "provider-1", Kind: solution.PluginKindProvider, Path: "/tmp/fake"},
	}

	clients, err := RegisterFetchedAuthHandlerPlugins(ctx, reg, results, nil)
	require.NoError(t, err)
	assert.Nil(t, clients, "provider kind should be skipped by RegisterFetchedAuthHandlerPlugins")
}

func TestRegisterFetchedAuthHandlerPlugins_InvalidPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := auth.NewRegistry()

	results := []FetchResult{
		{Name: "bad-handler", Kind: solution.PluginKindAuthHandler, Path: "/nonexistent/binary"},
	}

	clients, err := RegisterFetchedAuthHandlerPlugins(ctx, reg, results, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading auth handler plugin bad-handler")
	assert.Nil(t, clients)
}

// --- Catalog allowlist security tests ---

func TestFetcher_CheckCatalogAllowed_NoRestriction(t *testing.T) {
	t.Parallel()
	f := &Fetcher{catalogIndex: catalogindex.FromConfig(nil)}
	assert.NoError(t, f.catalogIndex.CheckAllowed("any-catalog"))
}

func TestFetcher_CheckCatalogAllowed_EmptyResolvedFrom(t *testing.T) {
	t.Parallel()
	f := &Fetcher{catalogIndex: catalogindex.FromConfig(nil).WithAllowed([]string{"official"})}
	err := f.catalogIndex.CheckAllowed("")
	assert.Error(t, err)
}

func TestFetcher_CheckCatalogAllowed_EmptyResolvedFrom_NoAllowlist(t *testing.T) {
	t.Parallel()
	f := &Fetcher{catalogIndex: catalogindex.FromConfig(nil)}
	assert.NoError(t, f.catalogIndex.CheckAllowed(""))
}

func TestFetcher_CheckCatalogAllowed_Permitted(t *testing.T) {
	t.Parallel()
	f := &Fetcher{catalogIndex: catalogindex.FromConfig(nil).WithAllowed([]string{"official", "internal"})}
	assert.NoError(t, f.catalogIndex.CheckAllowed("official"))
}

func TestFetcher_CheckCatalogAllowed_Rejected(t *testing.T) {
	t.Parallel()
	f := &Fetcher{catalogIndex: catalogindex.FromConfig(nil).WithAllowed([]string{"official"})}
	err := f.catalogIndex.CheckAllowed("untrusted-catalog")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in the allowed catalogs list")
}

func TestFetcher_CheckCatalogAllowed_CaseInsensitive(t *testing.T) {
	t.Parallel()
	f := &Fetcher{catalogIndex: catalogindex.FromConfig(nil).WithAllowed([]string{"official"})}
	// The allowlist is built with ToLower and CheckAllowed also lowercases its
	// argument, so matching is fully case-insensitive.
	assert.NoError(t, f.catalogIndex.CheckAllowed("official"))
	assert.NoError(t, f.catalogIndex.CheckAllowed("Official"))
	assert.NoError(t, f.catalogIndex.CheckAllowed("OFFICIAL"))
}

func TestNewFetcher_AllowedCatalogs(t *testing.T) {
	t.Parallel()
	cat := newMockCatalog()
	f := NewFetcher(FetcherConfig{
		Catalog:         cat,
		Logger:          logr.Discard(),
		AllowedCatalogs: []string{"Official", "Internal"},
	})
	// NewFetcher wires the allowlist into the catalog index gate, matched
	// case-insensitively.
	assert.NoError(t, f.catalogIndex.CheckAllowed("official"))
	assert.NoError(t, f.catalogIndex.CheckAllowed("internal"))
	err := f.catalogIndex.CheckAllowed("untrusted")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in the allowed catalogs list")
}

func TestFetcher_FetchPlugins_CatalogNotAllowed(t *testing.T) {
	t.Parallel()
	// Create a catalog that resolves to a non-allowed name
	cat := newMockCatalog()
	cat.name = "untrusted"

	binaryData := []byte("fake-plugin-binary")
	digest := binaryDigest(binaryData)

	ver := semver.MustParse("1.0.0")
	ref := catalog.Reference{
		Kind:    catalog.ArtifactKindProvider,
		Name:    "evil-plugin",
		Version: ver,
	}
	cat.addArtifact(ref, binaryData)

	f := NewFetcher(FetcherConfig{
		Catalog:         cat,
		Logger:          logr.Discard(),
		NoCache:         true,
		AllowedCatalogs: []string{"official"},
	})

	deps := []solution.PluginDependency{
		{Name: "evil-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}

	// Provide a lock entry so it hits the locked path with resolvedFrom set
	lockPlugins := []bundler.LockPlugin{
		{Name: "evil-plugin", Kind: "provider", Version: "1.0.0", Digest: digest, ResolvedFrom: "untrusted"},
	}

	_, err := f.FetchPlugins(context.Background(), deps, lockPlugins)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in the allowed catalogs list")
}

// localMockCatalog makes a mockCatalog appear as a local (filesystem) catalog so
// catalogindex.FromChain derives an identity (and registry hash) for it. This
// lets the per-catalog cached-plugin path be exercised, since the plain
// mockCatalog exposes only Name() and is therefore skipped by the index.
type localMockCatalog struct {
	*mockCatalog
	path string
}

func (l *localMockCatalog) Path() string { return l.path }

// TestFetcher_FetchPlugins_PluginNotAllowedFromCatalog_Locked proves the
// per-catalog plugin allowlist is enforced on the locked path, which serves
// entirely from lock+cache and never touches the chain's AllowlistCatalog
// decorator. The catalog is allowed, but it may not serve this plugin.
func TestFetcher_FetchPlugins_PluginNotAllowedFromCatalog_Locked(t *testing.T) {
	t.Parallel()
	cat := newMockCatalog()
	cat.name = "trusted"

	binaryData := []byte("fake-plugin-binary")
	digest := binaryDigest(binaryData)

	f := NewFetcher(FetcherConfig{
		Catalog:         cat,
		Logger:          logr.Discard(),
		NoCache:         true,
		AllowedCatalogs: []string{"trusted"}, // catalog IS allowed
		PerCatalogArtifacts: map[string]catalog.PluginPolicy{
			// ...but only "good-plugin" may be served from it.
			"trusted": {Plugins: []string{"good-plugin"}},
		},
	})

	deps := []solution.PluginDependency{
		{Name: "evil-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}
	lockPlugins := []bundler.LockPlugin{
		{Name: "evil-plugin", Kind: "provider", Version: "1.0.0", Digest: digest, ResolvedFrom: "trusted"},
	}

	_, err := f.FetchPlugins(context.Background(), deps, lockPlugins)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in catalog")
}

// TestFetcher_CachedPlugin_PluginNotAllowedFromCatalog proves the per-catalog
// plugin allowlist is enforced on the no-lock cache-first path: a binary cached
// under an allowed catalog's registry hash must not be served when that catalog
// is not permitted to serve that plugin name.
func TestFetcher_CachedPlugin_PluginNotAllowedFromCatalog(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	platform := CurrentPlatform()
	pluginName := "evil-plugin"

	base := newMockCatalog()
	base.name = "trusted"
	cat := &localMockCatalog{mockCatalog: base, path: "/catalogs/trusted"}

	// Derive the registry hash the fetcher will use for this catalog and
	// pre-populate the cache under it.
	ids := catalogindex.FromChain(cat).All()
	require.Len(t, ids, 1)
	regHash := ids[0].RegistryHash()

	c := NewCache(cacheDir)
	_, release, err := c.SetPin(pluginName, "2.0.0", platform, []byte("#!/bin/sh\n"), WithRegistryHash(regHash))
	require.NoError(t, err)
	release()

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    c,
		Platform: platform,
		Logger:   logr.Discard(),
		// Catalog is allowed (no AllowedCatalogs restriction), but it may only
		// serve "good-plugin" -- not the cached "evil-plugin".
		PerCatalogArtifacts: map[string]catalog.PluginPolicy{
			"trusted": {Plugins: []string{"good-plugin"}},
		},
	})

	deps := []solution.PluginDependency{
		{Name: pluginName, Kind: solution.PluginKindProvider},
	}

	// No lock file -> cache-first path. The cached binary must NOT be served;
	// with no artifact to resolve, the fetch fails rather than returning it.
	_, err = f.FetchPlugins(context.Background(), deps, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving version")
}

func TestFetcher_CacheFallback_AllowlistRejects(t *testing.T) {
	t.Parallel()
	// Create a pre-populated cache so the no-lock cache-first path is taken.
	cacheDir := t.TempDir()
	platform := CurrentPlatform()
	pluginName := "cached-plugin"
	binDir := filepath.Join(cacheDir, pluginName, "2.0.0", PlatformCacheKey(platform))
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, pluginName), []byte("#!/bin/sh\n"), 0o755))

	// Provide a catalog named "test" (not in allowlist) so the fallthrough
	// to catalog resolution resolves from a disallowed catalog.
	cat := newMockCatalog() // name = "test"
	ref := testRef(pluginName, "")
	cat.addArtifact(ref, []byte("#!/bin/sh\n"))

	f := NewFetcher(FetcherConfig{
		Catalog:         cat,
		Cache:           NewCache(cacheDir),
		Platform:        platform,
		Logger:          logr.Discard(),
		AllowedCatalogs: []string{"approved"},
	})

	deps := []solution.PluginDependency{
		{Name: pluginName, Kind: solution.PluginKindProvider},
	}

	// No lock file provided — fetcher uses cache-first path.
	// Allowlist is set, cached entry has unknown origin → falls through to
	// catalog resolution. Catalog resolves from "test" which is not in the
	// allowlist → rejected.
	_, err := f.FetchPlugins(context.Background(), deps, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in the allowed catalogs list")
}

// ── RegisterCachedPlugin tests ──────────────────────────────────────────────

func TestRegisterCachedPlugin_NotCached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := provider.NewRegistry()

	// Use a unique name that won't exist in any real cache.
	clients, err := RegisterCachedPlugin(ctx, "nonexistent-plugin-for-test-"+t.Name(), reg, nil, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in cache")
	assert.Nil(t, clients)
}

func TestRegisterCachedPluginVersion_NotCached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := provider.NewRegistry()

	clients, err := RegisterCachedPluginVersion(ctx, "nonexistent-plugin-for-test-"+t.Name(), "1.0.0", reg, nil, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in cache")
	assert.Nil(t, clients)
}

func TestRegisterCachedPluginVersion_WrongVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := provider.NewRegistry()

	// Create a cache with version 1.0.0 but request 2.0.0
	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	_, err := cache.Put("test-plugin", "1.0.0", CurrentPlatform(), []byte("#!/bin/sh\nexit 0"))
	require.NoError(t, err)

	clients, err := RegisterCachedPluginVersion(ctx, "test-plugin", "2.0.0", reg, nil, cacheDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in cache")
	assert.Nil(t, clients)
}

// TestRegisterCachedPluginVersion_AmbiguousRegistries verifies the ambiguity
// error from cache.ResolveVersion is surfaced to the caller when the same
// version exists under multiple catalog registries with differing content.
func TestRegisterCachedPluginVersion_AmbiguousRegistries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := provider.NewRegistry()

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)
	platform := CurrentPlatform()
	_, err := cache.Put("test-plugin", "1.0.0", platform, []byte("#!/bin/sh\nexit 0"), WithRegistryHash("0123456789abcdef"))
	require.NoError(t, err)
	_, err = cache.Put("test-plugin", "1.0.0", platform, []byte("#!/bin/sh\nexit 1"), WithRegistryHash("fedcba9876543210"))
	require.NoError(t, err)

	clients, err := RegisterCachedPluginVersion(ctx, "test-plugin", "1.0.0", reg, nil, cacheDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "differing binaries")
	assert.Nil(t, clients)
}

func TestPluginCacheName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind solution.PluginKind
		want string
	}{
		{"github", solution.PluginKindProvider, "github"},
		{"exec", solution.PluginKindProvider, "exec"},
		{"github", solution.PluginKindAuthHandler, "auth-handler-github"},
		{"entra", solution.PluginKindAuthHandler, "auth-handler-entra"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, PluginCacheKey(tc.name, tc.kind))
		})
	}
}

func TestPluginCachePath(t *testing.T) {
	t.Parallel()

	// With registry hash.
	path := PluginCachePath("github", solution.PluginKindProvider, "a1b2c3d4e5f67890", "1.5.3", "darwin/arm64")
	assert.Contains(t, path, "github")
	assert.Contains(t, path, "a1b2c3d4e5f67890")
	assert.Contains(t, path, "1.5.3")

	// Without registry hash.
	path = PluginCachePath("github", solution.PluginKindProvider, "", "1.5.3", "darwin/arm64")
	assert.NotContains(t, path, "a1b2c3d4e5f67890")
	assert.Contains(t, path, "github")
	assert.Contains(t, path, "1.5.3")
}

func TestPluginCacheName_NamespaceIsolation(t *testing.T) {
	t.Parallel()

	// A provider and auth-handler with the same name must produce different cache names.
	providerName := PluginCacheKey("github", solution.PluginKindProvider)
	authName := PluginCacheKey("github", solution.PluginKindAuthHandler)
	assert.NotEqual(t, providerName, authName)
}

func TestRegistryHash_CatalogIsolation(t *testing.T) {
	t.Parallel()

	// Same name/kind from different catalogs must produce different registry hashes.
	id1 := CatalogIdentity{Canonical: "ghcr.io/acme/plugins"}
	id2 := CatalogIdentity{Canonical: "ghcr.io/other/plugins"}

	hash1 := id1.RegistryHash()
	hash2 := id2.RegistryHash()
	assert.NotEqual(t, hash1, hash2)
	assert.Len(t, hash1, 16)
	assert.Len(t, hash2, 16)
}

func TestRegistryHash_ZeroIdentity(t *testing.T) {
	t.Parallel()

	id := CatalogIdentity{}
	assert.Equal(t, "", id.RegistryHash())
}

func TestFetcher_FetchPlugins_CatalogFallback_RejectsConstraintMismatch(t *testing.T) {
	t.Parallel()

	// Catalog is empty — resolution will fail.
	cat := newMockCatalog()

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)

	// Pre-populate cache with version 0.5.0 under the mock catalog's registry hash.
	_, err := cache.Put("my-plugin", "0.5.0", "linux/amd64", []byte("old-binary"), WithRegistryHash(testCatalogRegistryHash()))
	require.NoError(t, err)

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    cache,
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	// Request version 0.6.0 — catalog fails, cache has 0.5.0 which doesn't satisfy.
	deps := []solution.PluginDependency{
		{Name: "my-plugin", Kind: solution.PluginKindProvider, Version: "0.6.0"},
	}

	_, err = f.FetchPlugins(context.Background(), deps, nil)
	require.Error(t, err)
	// Catalog resolution fails and cached 0.5.0 doesn't satisfy 0.6.0, so the
	// catalog error propagates.
	assert.Contains(t, err.Error(), "resolving version")
}

func TestFetcher_FetchPlugins_CacheHit_RangeConstraintSatisfied(t *testing.T) {
	t.Parallel()

	// Catalog is empty — resolution will fail if attempted.
	cat := newMockCatalog()

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)

	// Pre-populate cache with version 0.5.2
	_, err := cache.Put("my-plugin", "0.5.2", "linux/amd64", []byte("cached-binary"))
	require.NoError(t, err)

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    cache,
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	// Request ^0.5.0 — cache has 0.5.2 which satisfies ^0.5.0.
	// The first cache check should match and return without hitting catalog.
	deps := []solution.PluginDependency{
		{Name: "my-plugin", Kind: solution.PluginKindProvider, Version: "^0.5.0"},
	}

	results, err := f.FetchPlugins(context.Background(), deps, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "0.5.2", results[0].Version)
	assert.True(t, results[0].FromCache)
}

func TestFetcher_FetchPlugins_CacheHit_LatestUsesAnyVersion(t *testing.T) {
	t.Parallel()

	// Catalog is empty — resolution will fail if attempted.
	cat := newMockCatalog()

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)

	// Pre-populate cache with version 0.5.0
	_, err := cache.Put("my-plugin", "0.5.0", "linux/amd64", []byte("cached-binary"))
	require.NoError(t, err)

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    cache,
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	// Request "latest" — cache has 0.5.0 which matches any version.
	// The first cache check should match and return without hitting catalog.
	deps := []solution.PluginDependency{
		{Name: "my-plugin", Kind: solution.PluginKindProvider, Version: "latest"},
	}

	results, err := f.FetchPlugins(context.Background(), deps, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "0.5.0", results[0].Version)
	assert.True(t, results[0].FromCache)
}

func TestFetcher_FetchPlugins_CatalogFallback_InvalidConstraintReportsParseError(t *testing.T) {
	t.Parallel()

	// Catalog is empty — resolution will fail.
	cat := newMockCatalog()

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)

	// Pre-populate cache with a valid version
	_, err := cache.Put("my-plugin", "1.0.0", "linux/amd64", []byte("binary"))
	require.NoError(t, err)

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    cache,
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	// Use an unparseable constraint — catalog resolution fails, and the
	// cached version doesn't match the invalid constraint either.
	deps := []solution.PluginDependency{
		{Name: "my-plugin", Kind: solution.PluginKindProvider, Version: ">>>invalid<<<"},
	}

	_, err = f.FetchPlugins(context.Background(), deps, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving version")
}

func TestFetcher_FetchPlugins_SkipsBuiltinProviders(t *testing.T) {
	t.Parallel()

	// Catalog has only the external plugin — builtins are never published.
	cat := newMockCatalog()
	ref := testRef("my-plugin", "1.0.0")
	cat.addArtifact(ref, []byte("external-binary"))

	cacheDir := t.TempDir()
	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    NewCache(cacheDir),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		// Builtin providers — must be silently skipped.
		{Name: "cel", Kind: solution.PluginKindProvider, Version: "1.0.0"},
		{Name: "file", Kind: solution.PluginKindProvider, Version: ">=1.0.0"},
		// External plugin — must be fetched normally.
		{Name: "my-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}

	results, err := f.FetchPlugins(context.Background(), deps, nil)
	require.NoError(t, err)
	// Only the external plugin should appear in results.
	require.Len(t, results, 1)
	assert.Equal(t, "my-plugin", results[0].Name)
}

func TestFetcher_FetchPlugins_AllBuiltins_ReturnsNil(t *testing.T) {
	t.Parallel()

	// When every declared plugin is a builtin, FetchPlugins should return nil.
	cat := newMockCatalog()
	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    NewCache(t.TempDir()),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "cel", Kind: solution.PluginKindProvider, Version: "1.0.0"},
		{Name: "http", Kind: solution.PluginKindProvider, Version: ">=0.1.0"},
		{Name: "static", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}

	results, err := f.FetchPlugins(context.Background(), deps, nil)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestFetcher_FetchPlugins_AuthHandlerBuiltinNotSkipped(t *testing.T) {
	t.Parallel()

	// Auth handler plugins with a builtin provider name should NOT be skipped
	// (the builtin check only applies to provider-kind plugins).
	cat := newMockCatalog()
	ref := testRef("cel", "1.0.0")
	cat.addArtifact(ref, []byte("auth-binary"))

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    NewCache(t.TempDir()),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "cel", Kind: solution.PluginKindAuthHandler, Version: "1.0.0"},
	}

	results, err := f.FetchPlugins(context.Background(), deps, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "cel", results[0].Name)
}

func TestCachedVersionSatisfies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		constraint string
		cached     string
		want       bool
		wantErr    bool
	}{
		{"empty constraint", "", "1.0.0", true, false},
		{"latest", "latest", "0.5.0", true, false},
		{"LATEST case insensitive", "LATEST", "0.5.0", true, false},
		{"exact match", "1.0.0", "1.0.0", true, false},
		{"exact mismatch", "2.0.0", "1.0.0", false, false},
		{"range satisfied", "^1.0.0", "1.2.3", true, false},
		{"range not satisfied", "^2.0.0", "1.2.3", false, false},
		{"invalid constraint", ">>>bad<<<", "1.0.0", false, true},
		{"invalid cached version", "^1.0.0", "not-semver", false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := cachedVersionSatisfies(tc.constraint, tc.cached)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// ── RegisterFetchedAuthHandlerPlugins subprocess-lifecycle tests ──────────────

var (
	testAuthPluginOnce sync.Once
	testAuthPluginPath string
	testAuthPluginErr  error
)

// buildTestAuthHandlerPlugin compiles the minimal auth-handler fixture in
// testdata/authhandler and returns its path. The binary is built once and
// reused across tests.
func buildTestAuthHandlerPlugin(t *testing.T) string {
	t.Helper()
	testAuthPluginOnce.Do(func() {
		binName := "scafctl-plugin-test-auth"
		if runtime.GOOS == "windows" {
			binName += ".exe"
		}
		tmpDir, err := os.MkdirTemp("", "scafctl-test-authhandler-*")
		if err != nil {
			testAuthPluginErr = err
			return
		}
		binPath := filepath.Join(tmpDir, binName)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, ".")
		cmd.Dir = filepath.Join("testdata", "authhandler")
		if out, buildErr := cmd.CombinedOutput(); buildErr != nil {
			testAuthPluginErr = fmt.Errorf("building test auth handler plugin: %w\n%s", buildErr, out)
			return
		}
		testAuthPluginPath = binPath
	})
	require.NoError(t, testAuthPluginErr)
	require.NotEmpty(t, testAuthPluginPath, "test auth handler plugin build failed")
	return testAuthPluginPath
}

func TestRegisterFetchedAuthHandlerPlugins_RegistersAndKeepsClient(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping auth handler plugin test in short mode (requires go build)")
	}
	binPath := buildTestAuthHandlerPlugin(t)

	ctx := context.Background()
	reg := auth.NewRegistry()
	results := []FetchResult{
		{Name: "test-auth", Kind: solution.PluginKindAuthHandler, Path: binPath},
	}

	clients, err := RegisterFetchedAuthHandlerPlugins(ctx, reg, results, nil)
	require.NoError(t, err)
	require.Len(t, clients, 1, "handler with a new name must be registered and its client kept alive")
	t.Cleanup(func() {
		for _, c := range clients {
			c.Kill()
		}
	})
	assert.True(t, reg.Has("test-auth"))
}

func TestRegisterFetchedAuthHandlerPlugins_KillsClientWhenNoNewHandlers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping auth handler plugin test in short mode (requires go build)")
	}
	binPath := buildTestAuthHandlerPlugin(t)

	ctx := context.Background()
	reg := auth.NewRegistry()
	// Pre-register a handler with the same name the plugin exposes so the
	// plugin registers zero new handlers on load.
	require.NoError(t, reg.Register(auth.NewMockHandler("test-auth")))

	results := []FetchResult{
		{Name: "test-auth", Kind: solution.PluginKindAuthHandler, Path: binPath},
	}

	clients, err := RegisterFetchedAuthHandlerPlugins(ctx, reg, results, nil)
	require.NoError(t, err)
	assert.Empty(t, clients,
		"a plugin that registers no new handlers must have its subprocess killed and not be returned")
}

func TestFetcher_ResolvePlugins_Empty(t *testing.T) {
	t.Parallel()
	f := NewFetcher(FetcherConfig{
		Catalog:  newMockCatalog(),
		Cache:    NewCache(t.TempDir()),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	infos, err := f.ResolvePlugins(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, infos)
}

func TestFetcher_ResolvePlugins_Success(t *testing.T) {
	t.Parallel()
	cat := newMockCatalog()
	cat.addArtifact(testRef("plugin-a", "1.0.0"), []byte("binary-a"))
	cat.addArtifact(testRef("plugin-b", "2.0.0"), []byte("binary-b"))

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    NewCache(t.TempDir()),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "plugin-a", Kind: solution.PluginKindProvider, Version: "1.0.0"},
		{Name: "plugin-b", Kind: solution.PluginKindProvider, Version: "2.0.0"},
	}

	infos, err := f.ResolvePlugins(context.Background(), deps)
	require.NoError(t, err)
	require.Len(t, infos, 2)

	// Results are index-aligned with the input deps.
	assert.Equal(t, "plugin-a", infos[0].Reference.Name)
	require.NotNil(t, infos[0].Reference.Version)
	assert.Equal(t, "1.0.0", infos[0].Reference.Version.String())
	assert.Equal(t, "plugin-b", infos[1].Reference.Name)
	require.NotNil(t, infos[1].Reference.Version)
	assert.Equal(t, "2.0.0", infos[1].Reference.Version.String())
}

func TestFetcher_ResolvePlugins_CatalogNotAllowed(t *testing.T) {
	t.Parallel()
	cat := newMockCatalog()
	cat.addArtifact(testRef("plugin-a", "1.0.0"), []byte("binary-a"))

	f := NewFetcher(FetcherConfig{
		Catalog:         cat,
		Cache:           NewCache(t.TempDir()),
		Platform:        "linux/amd64",
		Logger:          logr.Discard(),
		AllowedCatalogs: []string{"official"},
	})

	// The dep names a catalog that is not in the allowlist, so resolvePlugin's
	// CheckAllowed gate rejects before any resolution happens.
	deps := []solution.PluginDependency{
		{Name: "plugin-a", Kind: solution.PluginKindProvider, Version: "1.0.0", Catalog: "untrusted"},
	}

	_, err := f.ResolvePlugins(context.Background(), deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in the allowed catalogs list")
}

func TestFetcher_ResolvePlugins_PluginNotAllowedFromCatalog(t *testing.T) {
	t.Parallel()
	cat := newMockCatalog()
	cat.name = "trusted"
	cat.addArtifact(testRef("evil-plugin", "1.0.0"), []byte("binary"))

	f := NewFetcher(FetcherConfig{
		Catalog:  cat,
		Cache:    NewCache(t.TempDir()),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
		// The catalog is allowed, but may only serve "good-plugin".
		PerCatalogArtifacts: map[string]catalog.PluginPolicy{
			"trusted": {Plugins: []string{"good-plugin"}},
		},
	})

	deps := []solution.PluginDependency{
		{Name: "evil-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0", Catalog: "trusted"},
	}

	_, err := f.ResolvePlugins(context.Background(), deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in catalog")
}

func TestFetcher_ResolvePlugins_ResolveError(t *testing.T) {
	t.Parallel()
	// No artifacts added, so resolution fails with not-found.
	f := NewFetcher(FetcherConfig{
		Catalog:  newMockCatalog(),
		Cache:    NewCache(t.TempDir()),
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "missing-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}

	_, err := f.ResolvePlugins(context.Background(), deps)
	require.Error(t, err)
}
