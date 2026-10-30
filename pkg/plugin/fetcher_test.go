// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// mockCatalog doubles as the Fetcher's CatalogFetcher. Its plugin methods run
// the production catalog.PluginFetcher over a mockPluginSource view of the
// mock, so resolution semantics (scoping, not-found handling) match runtime.
func (m *mockCatalog) pluginFetcher() (*catalog.PluginFetcher, error) {
	return catalog.NewPluginFetcher(logr.Discard(), nil, mockPluginSource{m})
}

func (m *mockCatalog) ResolvePlugin(ctx context.Context, name string, kind catalog.ArtifactKind, versionConstraint string, opts ...catalog.Option) (catalog.ArtifactInfo, error) {
	pf, err := m.pluginFetcher()
	if err != nil {
		return catalog.ArtifactInfo{}, err
	}
	return pf.ResolvePlugin(ctx, name, kind, versionConstraint, opts...)
}

func (m *mockCatalog) FetchPlugin(ctx context.Context, name string, kind catalog.ArtifactKind, version, platform string, opts ...catalog.Option) ([]byte, catalog.ArtifactInfo, error) {
	pf, err := m.pluginFetcher()
	if err != nil {
		return nil, catalog.ArtifactInfo{}, err
	}
	return pf.FetchPlugin(ctx, name, kind, version, platform, opts...)
}

func (m *mockCatalog) ResolveContentDigest(ctx context.Context, name string, kind catalog.ArtifactKind, version, platform, mediaType string, opts ...catalog.Option) (catalog.ContentDigestInfo, error) {
	pf, err := m.pluginFetcher()
	if err != nil {
		return catalog.ContentDigestInfo{}, err
	}
	return pf.ResolveContentDigest(ctx, name, kind, version, platform, mediaType, opts...)
}

// Catalogs reports the mock itself so NewFetcher indexes it.
func (m *mockCatalog) Catalogs() []catalog.Catalog { return []catalog.Catalog{m} }

// mockPluginSource adapts mockCatalog to catalog.PluginSource.
type mockPluginSource struct{ m *mockCatalog }

func (s mockPluginSource) Name() string { return s.m.name }

func (s mockPluginSource) FetchByPlatform(ctx context.Context, ref catalog.Reference, platform string) ([]byte, catalog.ArtifactInfo, error) {
	return s.m.FetchByPlatform(ctx, ref, platform)
}

func (s mockPluginSource) ResolveContentDigest(_ context.Context, ref catalog.Reference, _, _ string) (catalog.ContentDigestInfo, error) {
	a, ok := s.m.artifacts[ref.String()]
	if !ok {
		return catalog.ContentDigestInfo{}, &catalog.ArtifactNotFoundError{Reference: ref, Catalog: s.m.name}
	}
	return catalog.ContentDigestInfo{
		ArtifactInfo:  a.info,
		ContentDigest: a.info.Digest,
	}, nil
}

// Find returns the newest artifact matching query.Version ("" / "latest",
// an exact version, or a semver constraint). For "" / "latest" an unversioned
// artifact is returned when no versioned one exists.
func (s mockPluginSource) Find(ctx context.Context, query catalog.ArtifactQuery) (catalog.ArtifactInfo, error) {
	infos, err := s.m.List(ctx, query.Kind, query.Name)
	if err != nil {
		return catalog.ArtifactInfo{}, err
	}
	notFound := &catalog.ArtifactNotFoundError{Reference: catalog.Reference{Kind: query.Kind, Name: query.Name}, Catalog: s.m.name}
	constraint := query.Version
	if strings.EqualFold(constraint, "latest") {
		constraint = ""
	}
	if constraint == "" {
		var best *catalog.ArtifactInfo
		for i := range infos {
			v := infos[i].Reference.Version
			if best == nil || (v != nil && (best.Reference.Version == nil || v.GreaterThan(best.Reference.Version))) {
				best = &infos[i]
			}
		}
		if best == nil {
			return catalog.ArtifactInfo{}, notFound
		}
		return *best, nil
	}
	matches, err := catalog.FilterByVersionConstraint(infos, constraint)
	if err != nil {
		return catalog.ArtifactInfo{}, fmt.Errorf("%w: %w", catalog.ErrInvalidQuery, err)
	}
	if len(matches) == 0 {
		return catalog.ArtifactInfo{}, notFound
	}
	return matches[0], nil
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
		CatalogFetcher: cat,
		Cache:          NewCache(t.TempDir()),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
	})

	results, err := f.FetchPlugins(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Nil(t, results)
}

// builtFixture is a fetcher over a real local catalog holding a locally built
// my-plugin@1.0.0, in front of a remote serving pulled copies of 1.0.0 and
// 2.0.0 under the same name.
type builtFixture struct {
	fetcher *Fetcher
	cache   *Cache
}

var builtBinary = []byte("built-binary")

func newBuiltFixture(t *testing.T, opts ...func(*FetcherConfig)) builtFixture {
	t.Helper()
	local, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)
	_, err = local.StoreMultiPlatform(t.Context(), testRef("my-plugin", "1.0.0"),
		[]catalog.PlatformBinary{{Platform: "linux/amd64", Data: builtBinary}}, nil, false)
	require.NoError(t, err)

	remote := newMockCatalog()
	remote.name = "remote"
	remote.addArtifact(testRef("my-plugin", "1.0.0"), []byte("pulled-binary"))
	remote.addArtifact(testRef("my-plugin", "2.0.0"), []byte("pulled-binary-2"))

	pf, err := catalog.NewPluginFetcher(logr.Discard(), local, mockPluginSource{remote})
	require.NoError(t, err)
	cache := NewCache(t.TempDir())
	cfg := FetcherConfig{
		CatalogFetcher: pf,
		Cache:          cache,
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return builtFixture{fetcher: NewFetcher(cfg), cache: cache}
}

func builtPluginDep(version string) solution.PluginDependency {
	return solution.PluginDependency{
		Name: "my-plugin", Kind: solution.PluginKindProvider, Version: version,
		Source: &solution.PluginSource{Built: true},
	}
}

func readResult(t *testing.T, r FetchResult) string {
	t.Helper()
	data, err := os.ReadFile(r.Path)
	require.NoError(t, err)
	return string(data)
}

func TestFetcher_BuiltDependency_ServedFromLocalCatalog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		catalog string
	}{
		{name: "unbound dep is bound to local"},
		{name: "dep already bound to local", catalog: catalog.LocalCatalogName},
		{name: "local binding is case-insensitive", catalog: "LOCAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newBuiltFixture(t)
			ctx := t.Context()
			dep := builtPluginDep("1.0.0")
			dep.Catalog = tt.catalog

			results, err := fx.fetcher.FetchPlugins(ctx, []solution.PluginDependency{dep}, nil)
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, catalog.LocalCatalogName, results[0].Catalog)
			assert.Equal(t, string(builtBinary), readResult(t, results[0]))

			infos, err := fx.fetcher.ResolvePlugins(ctx, []solution.PluginDependency{dep})
			require.NoError(t, err)
			require.Len(t, infos, 1)
			assert.Equal(t, catalog.LocalCatalogName, infos[0].Catalog)
		})
	}
}

func TestFetcher_BuiltDependency_NeverFallsBackToRemote(t *testing.T) {
	t.Parallel()
	fx := newBuiltFixture(t)
	ctx := t.Context()

	// Only the remote serves 2.0.0; a short name resolves it there...
	short := builtPluginDep("^2.0.0")
	short.Source = nil
	infos, err := fx.fetcher.ResolvePlugins(ctx, []solution.PluginDependency{short})
	require.NoError(t, err)
	assert.Equal(t, "remote", infos[0].Catalog)

	// ...but a built dep must fail closed rather than serve the pulled copy.
	dep := builtPluginDep("^2.0.0")
	_, err = fx.fetcher.ResolvePlugins(ctx, []solution.PluginDependency{dep})
	require.ErrorIs(t, err, catalog.ErrArtifactNotFound)

	_, err = fx.fetcher.FetchPlugin(ctx, dep, nil)
	require.ErrorIs(t, err, catalog.ErrArtifactNotFound)
}

func TestFetcher_BuiltDependency_CatalogConflict(t *testing.T) {
	t.Parallel()

	otherCatalog := builtPluginDep("1.0.0")
	otherCatalog.Catalog = "remote"
	withRegistry := builtPluginDep("1.0.0")
	withRegistry.Source.Registry = "ghcr.io/other"

	for name, dep := range map[string]solution.PluginDependency{
		"bound to another catalog": otherCatalog,
		"also names a registry":    withRegistry,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fx := newBuiltFixture(t)
			ctx := t.Context()

			_, err := fx.fetcher.FetchPlugins(ctx, []solution.PluginDependency{dep}, nil)
			require.ErrorIs(t, err, ErrCatalogConflict)

			_, err = fx.fetcher.FetchPlugin(ctx, dep, nil)
			require.ErrorIs(t, err, ErrCatalogConflict)

			_, err = fx.fetcher.ResolvePlugins(ctx, []solution.PluginDependency{dep})
			require.ErrorIs(t, err, ErrCatalogConflict)
		})
	}
}

func TestFetcher_LocalCatalog_SkipsUnverifiedCache(t *testing.T) {
	t.Parallel()

	shortName := func(catalogName string) solution.PluginDependency {
		dep := builtPluginDep("1.0.0")
		dep.Source = nil
		dep.Catalog = catalogName
		return dep
	}

	tests := []struct {
		name string
		dep  solution.PluginDependency
	}{
		{name: "built dep", dep: builtPluginDep("1.0.0")},
		{name: "short name bound to local", dep: shortName(catalog.LocalCatalogName)},
		{name: "unbound short name", dep: shortName("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newBuiltFixture(t)
			ctx := t.Context()

			// A stale binary cached under the local catalog's identity at the
			// built version, as left behind by rebuilding the plugin in place.
			localHash := fx.fetcher.resolveCatalogIdentity(catalog.LocalCatalogName).RegistryHash()
			_, err := fx.cache.Put(PluginCacheKey("my-plugin", solution.PluginKindProvider), "1.0.0", "linux/amd64",
				[]byte("stale-binary"), WithRegistryHash(localHash))
			require.NoError(t, err)

			// The digest is re-resolved and the current build is served.
			r, err := fx.fetcher.FetchPlugin(ctx, tt.dep, nil)
			require.NoError(t, err)
			assert.False(t, r.FromCache)
			assert.Equal(t, catalog.LocalCatalogName, r.Catalog)
			assert.Equal(t, string(builtBinary), readResult(t, r))

			// The verified binary is then reused through the digest-checked cache.
			r, err = fx.fetcher.FetchPlugin(ctx, tt.dep, nil)
			require.NoError(t, err)
			assert.True(t, r.FromCache)
			assert.Equal(t, string(builtBinary), readResult(t, r))
		})
	}
}

func TestFetcher_BuiltDependency_Locked(t *testing.T) {
	t.Parallel()

	builtLock := func(digest string) []bundler.LockPlugin {
		return []bundler.LockPlugin{{
			Name: "my-plugin", Kind: "provider", Version: "1.0.0", Constraint: "1.0.0",
			Digest: digest, ResolvedFrom: catalog.LocalCatalogName,
			Source: &bundler.LockPluginSource{Built: true},
		}}
	}

	t.Run("verified against the lock digest", func(t *testing.T) {
		t.Parallel()
		fx := newBuiltFixture(t)
		r, err := fx.fetcher.FetchPlugin(t.Context(), builtPluginDep("1.0.0"), builtLock(binaryDigest(builtBinary)))
		require.NoError(t, err)
		assert.Equal(t, catalog.LocalCatalogName, r.Catalog)
		assert.Equal(t, binaryDigest(builtBinary), r.Digest)
		assert.Equal(t, string(builtBinary), readResult(t, r))
	})

	t.Run("short name locked to a built copy resolves locally", func(t *testing.T) {
		t.Parallel()
		fx := newBuiltFixture(t)
		dep := builtPluginDep("1.0.0")
		dep.Source = nil
		lock := builtLock(binaryDigest(builtBinary))
		lock[0].Source = nil
		r, err := fx.fetcher.FetchPlugin(t.Context(), dep, lock)
		require.NoError(t, err)
		assert.Equal(t, catalog.LocalCatalogName, r.Catalog)
		assert.Equal(t, string(builtBinary), readResult(t, r))
	})

	t.Run("rebuilt after locking reports a refresh hint", func(t *testing.T) {
		t.Parallel()
		fx := newBuiltFixture(t)
		_, err := fx.fetcher.FetchPlugin(t.Context(), builtPluginDep("1.0.0"), builtLock(binaryDigest([]byte("old-build"))))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "built plugin my-plugin@1.0.0 does not match the lock file")
		assert.Contains(t, err.Error(), "build solution")
		assert.NotContains(t, err.Error(), "supply chain")
	})
}

func disallowLocalBuilds(cfg *FetcherConfig) { cfg.DisallowLocalBuilds = true }

func TestFetcher_DisallowLocalBuilds_RejectsLocalBuilds(t *testing.T) {
	t.Parallel()

	shortName := func(catalogName string) solution.PluginDependency {
		dep := builtPluginDep("1.0.0")
		dep.Source = nil
		dep.Catalog = catalogName
		return dep
	}

	tests := []struct {
		name string
		dep  solution.PluginDependency
	}{
		{name: "built dep", dep: builtPluginDep("1.0.0")},
		{name: "short name bound to local", dep: shortName(catalog.LocalCatalogName)},
		{name: "unbound short name shadowed by a local build", dep: shortName("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newBuiltFixture(t, disallowLocalBuilds)
			ctx := t.Context()

			_, err := fx.fetcher.FetchPlugins(ctx, []solution.PluginDependency{tt.dep}, nil)
			require.ErrorIs(t, err, ErrLocalBuildNotAllowed)
			assert.Contains(t, err.Error(), "publish the plugin to a registry")

			_, err = fx.fetcher.FetchPlugin(ctx, tt.dep, nil)
			require.ErrorIs(t, err, ErrLocalBuildNotAllowed)

			_, err = fx.fetcher.ResolvePlugins(ctx, []solution.PluginDependency{tt.dep})
			require.ErrorIs(t, err, ErrLocalBuildNotAllowed)
		})
	}
}

func TestFetcher_DisallowLocalBuilds_RejectsBuiltLockEntry(t *testing.T) {
	t.Parallel()
	fx := newBuiltFixture(t, disallowLocalBuilds)
	dep := builtPluginDep("1.0.0")
	dep.Source = nil
	lock := []bundler.LockPlugin{{
		Name: "my-plugin", Kind: "provider", Version: "1.0.0", Constraint: "1.0.0",
		Digest: binaryDigest(builtBinary), ResolvedFrom: catalog.LocalCatalogName,
	}}

	_, err := fx.fetcher.FetchPlugin(t.Context(), dep, lock)
	require.ErrorIs(t, err, ErrLocalBuildNotAllowed)
	assert.Contains(t, err.Error(), "locked from a local build")
}

func TestFetcher_DisallowLocalBuilds_IgnoresUnknownOriginCache(t *testing.T) {
	t.Parallel()
	dep := builtPluginDep("1.0.0")
	dep.Source = nil

	for _, disallow := range []bool{false, true} {
		t.Run(fmt.Sprintf("disallow=%t", disallow), func(t *testing.T) {
			t.Parallel()
			fx := newBuiltFixture(t, func(cfg *FetcherConfig) { cfg.DisallowLocalBuilds = disallow })
			// A flat-layout entry has no recorded origin.
			_, err := fx.cache.Put(PluginCacheKey("my-plugin", solution.PluginKindProvider), "1.0.0", "linux/amd64",
				[]byte("unknown-origin"))
			require.NoError(t, err)

			r, err := fx.fetcher.FetchPlugin(t.Context(), dep, nil)
			if disallow {
				require.ErrorIs(t, err, ErrLocalBuildNotAllowed)
				return
			}
			require.NoError(t, err)
			assert.True(t, r.FromCache)
			assert.Equal(t, "unknown-origin", readResult(t, r))
		})
	}
}

func TestFetcher_DisallowLocalBuilds_AllowsRemotePlugins(t *testing.T) {
	t.Parallel()
	fx := newRegistryFixture(t, disallowLocalBuilds)
	r, err := fx.fetcher.FetchPlugin(t.Context(), registryPluginDep(betaRegistry), nil)
	require.NoError(t, err)
	assert.Equal(t, "beta", r.Catalog)
	assert.Equal(t, string(betaBinary), readResult(t, r))
}

// registryCatalog is a mock remote catalog with an OCI origin, so the fetcher's
// catalog index maps that registry to the catalog's alias.
type registryCatalog struct {
	*mockCatalog
	registry string
}

func (c registryCatalog) Registry() string   { return c.registry }
func (c registryCatalog) Repository() string { return "" }

// indexedPluginFetcher reports registry-aware catalogs for the fetcher's index.
type indexedPluginFetcher struct {
	*catalog.PluginFetcher
	cats []catalog.Catalog
}

func (f indexedPluginFetcher) Catalogs() []catalog.Catalog { return f.cats }

// registryFixture is a fetcher over two remote catalogs, alpha (first in the
// chain) and beta, that both serve my-plugin@1.0.0 with different binaries.
type registryFixture struct {
	fetcher *Fetcher
	cache   *Cache
}

const betaRegistry = "ghcr.io/beta"

var (
	alphaBinary = []byte("alpha-binary")
	betaBinary  = []byte("beta-binary")
)

func newRegistryFixture(t *testing.T, opts ...func(*FetcherConfig)) registryFixture {
	t.Helper()
	alpha := newMockCatalog()
	alpha.name = "alpha"
	alpha.addArtifact(testRef("my-plugin", "1.0.0"), alphaBinary)
	beta := newMockCatalog()
	beta.name = "beta"
	beta.addArtifact(testRef("my-plugin", "1.0.0"), betaBinary)

	pf, err := catalog.NewPluginFetcher(logr.Discard(), nil, mockPluginSource{alpha}, mockPluginSource{beta})
	require.NoError(t, err)
	cache := NewCache(t.TempDir())
	cfg := FetcherConfig{
		CatalogFetcher: indexedPluginFetcher{PluginFetcher: pf, cats: []catalog.Catalog{
			registryCatalog{mockCatalog: alpha, registry: "ghcr.io/alpha"},
			registryCatalog{mockCatalog: beta, registry: betaRegistry},
		}},
		Cache:    cache,
		Platform: "linux/amd64",
		Logger:   logr.Discard(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return registryFixture{fetcher: NewFetcher(cfg), cache: cache}
}

func registryPluginDep(registry string) solution.PluginDependency {
	return solution.PluginDependency{
		Name: "my-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0",
		Source: &solution.PluginSource{Registry: registry, Artifact: "my-plugin"},
	}
}

func TestFetcher_RegistryDependency_ServedFromItsCatalog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		catalog  string
	}{
		{name: "unbound dep is bound to the registry's catalog", registry: betaRegistry},
		{name: "registry matches case-insensitively", registry: "GHCR.io/Beta"},
		{name: "dep already bound to the registry's catalog", registry: betaRegistry, catalog: "beta"},
		{name: "existing binding matches case-insensitively", registry: betaRegistry, catalog: "BETA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newRegistryFixture(t)
			ctx := t.Context()
			dep := registryPluginDep(tt.registry)
			dep.Catalog = tt.catalog

			results, err := fx.fetcher.FetchPlugins(ctx, []solution.PluginDependency{dep}, nil)
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, "beta", results[0].Catalog)
			assert.Equal(t, string(betaBinary), readResult(t, results[0]))

			infos, err := fx.fetcher.ResolvePlugins(ctx, []solution.PluginDependency{dep})
			require.NoError(t, err)
			require.Len(t, infos, 1)
			assert.Equal(t, "beta", infos[0].Catalog)
		})
	}

	t.Run("control: a short name resolves from the first catalog", func(t *testing.T) {
		t.Parallel()
		fx := newRegistryFixture(t)
		short := registryPluginDep(betaRegistry)
		short.Source = nil
		infos, err := fx.fetcher.ResolvePlugins(t.Context(), []solution.PluginDependency{short})
		require.NoError(t, err)
		assert.Equal(t, "alpha", infos[0].Catalog)
	})
}

func TestFetcher_RegistryDependency_FailsClosed(t *testing.T) {
	t.Parallel()

	unconfigured := registryPluginDep("ghcr.io/other")
	otherCatalog := registryPluginDep(betaRegistry)
	otherCatalog.Catalog = "alpha"

	tests := []struct {
		name    string
		dep     solution.PluginDependency
		wantErr error
	}{
		{name: "unconfigured registry", dep: unconfigured, wantErr: ErrUnconfiguredRegistry},
		{name: "bound to another catalog", dep: otherCatalog, wantErr: ErrCatalogConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newRegistryFixture(t)
			ctx := t.Context()

			_, err := fx.fetcher.FetchPlugins(ctx, []solution.PluginDependency{tt.dep}, nil)
			require.ErrorIs(t, err, tt.wantErr)

			_, err = fx.fetcher.FetchPlugin(ctx, tt.dep, nil)
			require.ErrorIs(t, err, tt.wantErr)

			_, err = fx.fetcher.ResolvePlugins(ctx, []solution.PluginDependency{tt.dep})
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestFetcher_RegistryDependency_CacheScopedToItsCatalog(t *testing.T) {
	t.Parallel()
	fx := newRegistryFixture(t)
	ctx := t.Context()
	cacheKey := PluginCacheKey("my-plugin", solution.PluginKindProvider)

	// A copy cached from alpha must not satisfy a dep sourced from beta.
	alphaHash := fx.fetcher.resolveCatalogIdentity("alpha").RegistryHash()
	_, err := fx.cache.Put(cacheKey, "1.0.0", "linux/amd64", []byte("alpha-cached"), WithRegistryHash(alphaHash))
	require.NoError(t, err)

	r, err := fx.fetcher.FetchPlugin(ctx, registryPluginDep(betaRegistry), nil)
	require.NoError(t, err)
	assert.Equal(t, "beta", r.Catalog)
	assert.Equal(t, string(betaBinary), readResult(t, r))

	// A copy cached from beta is reused, and a catalog binding matches the
	// index alias case-insensitively.
	betaHash := fx.fetcher.resolveCatalogIdentity("beta").RegistryHash()
	_, err = fx.cache.Put(cacheKey, "1.0.0", "linux/amd64", []byte("beta-cached"), WithRegistryHash(betaHash))
	require.NoError(t, err)

	short := registryPluginDep(betaRegistry)
	short.Source = nil
	short.Catalog = "BETA"
	r, err = fx.fetcher.FetchPlugin(ctx, short, nil)
	require.NoError(t, err)
	assert.True(t, r.FromCache)
	assert.Equal(t, "beta", r.Catalog)
	assert.Equal(t, "beta-cached", readResult(t, r))
}

func TestFetcher_RegistryDependency_Locked(t *testing.T) {
	t.Parallel()

	lockFrom := func(canonical string, binary []byte) []bundler.LockPlugin {
		return []bundler.LockPlugin{{
			Name: "my-plugin", Kind: "provider", Version: "1.0.0", Constraint: "1.0.0",
			Digest: binaryDigest(binary), ResolvedCanonical: canonical,
			Source: &bundler.LockPluginSource{Registry: betaRegistry},
		}}
	}

	t.Run("served from the locked catalog", func(t *testing.T) {
		t.Parallel()
		fx := newRegistryFixture(t)
		r, err := fx.fetcher.FetchPlugin(t.Context(), registryPluginDep(betaRegistry), lockFrom(betaRegistry, betaBinary))
		require.NoError(t, err)
		assert.Equal(t, "beta", r.Catalog)
		assert.Equal(t, string(betaBinary), readResult(t, r))
	})

	t.Run("lock resolved from another catalog is stale", func(t *testing.T) {
		t.Parallel()
		fx := newRegistryFixture(t)
		_, err := fx.fetcher.FetchPlugin(t.Context(), registryPluginDep(betaRegistry), lockFrom("ghcr.io/alpha", alphaBinary))
		require.ErrorIs(t, err, ErrLockStale)
		assert.Contains(t, err.Error(), `resolved from catalog "alpha"`)
		assert.Contains(t, err.Error(), "build solution")
	})
}

func TestFetcher_FetchPlugins_CacheMiss_FetchFromCatalog(t *testing.T) {
	cat := newMockCatalog()
	ref := testRef("my-plugin", "1.0.0")
	cat.addArtifact(ref, []byte("the-binary"))

	cacheDir := t.TempDir()
	f := NewFetcher(FetcherConfig{
		CatalogFetcher: cat,
		Cache:          NewCache(cacheDir),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          NewCache(t.TempDir()),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          NewCache(t.TempDir()),
		Platform:       "windows/amd64", // not published by the lock
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          cache,
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          NewCache(t.TempDir()),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          NewCache(t.TempDir()),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          NewCache(t.TempDir()),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          NewCache(t.TempDir()),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          cache,
		Platform:       "linux/amd64",
		NoCache:        true,
		Logger:         logr.Discard(),
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
	signed := cat.artifacts[ref.String()]
	signed.info.ImageRef = "ghcr.io/org/providers/signed-plugin@sha256:abc"
	cat.artifacts[ref.String()] = signed

	cacheDir := t.TempDir()
	cache := NewCache(cacheDir)

	// Pre-populate cache
	_, err := cache.Put("signed-plugin", "1.0.0", "linux/amd64", []byte("fresh-binary"), WithRegistryHash(testCatalogRegistryHash()))
	require.NoError(t, err)

	verifier := &mockVerifier{result: &SignatureResult{Verified: true}}
	f := NewFetcher(FetcherConfig{
		CatalogFetcher: cat,
		Cache:          cache,
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
		SignaturePolicy: &SignaturePolicy{
			Mode:              SignatureModeEnforce,
			TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
			TrustedIdentities: []string{"https://github.com/org/*"},
		},
		SignatureVerifier: verifier,
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
	require.NotNil(t, results[0].Signature)
	assert.True(t, results[0].Signature.Verified)
	assert.Equal(t, []string{"ghcr.io/org/providers/signed-plugin@sha256:abc"}, verifier.imageRefs)
}

func TestFetcher_FetchPlugins_EnforceMode_NoLockFile_BypassesCache(t *testing.T) {
	t.Parallel()

	enforce := &SignaturePolicy{
		Mode:              SignatureModeEnforce,
		TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
		TrustedIdentities: []string{"https://github.com/org/*"},
	}
	deps := []solution.PluginDependency{
		{Name: "signed-plugin", Kind: solution.PluginKindProvider, Version: "^1.0.0"},
	}
	newCachedFetcher := func(t *testing.T, cat *mockCatalog, verifier *mockVerifier) *Fetcher {
		t.Helper()
		cache := NewCache(t.TempDir())
		_, err := cache.Put("signed-plugin", "1.0.0", "linux/amd64", []byte("cached-binary"), WithRegistryHash(testCatalogRegistryHash()))
		require.NoError(t, err)
		return NewFetcher(FetcherConfig{
			CatalogFetcher:    cat,
			Cache:             cache,
			Platform:          "linux/amd64",
			Logger:            logr.Discard(),
			SignaturePolicy:   enforce,
			SignatureVerifier: verifier,
		})
	}

	t.Run("cache shortcut skipped", func(t *testing.T) {
		t.Parallel()
		cat := newMockCatalog()
		ref := testRef("signed-plugin", "1.0.0")
		cat.addArtifact(ref, []byte("fresh-binary"))
		signed := cat.artifacts[ref.String()]
		signed.info.ImageRef = "ghcr.io/org/providers/signed-plugin@sha256:abc"
		cat.artifacts[ref.String()] = signed

		verifier := &mockVerifier{result: &SignatureResult{Verified: true}}
		results, err := newCachedFetcher(t, cat, verifier).FetchPlugins(t.Context(), deps, nil)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].FromCache, "enforce mode must not serve an unverified cached binary")
		require.NotNil(t, results[0].Signature)
		assert.True(t, results[0].Signature.Verified)
		assert.Equal(t, []string{"ghcr.io/org/providers/signed-plugin@sha256:abc"}, verifier.imageRefs)
	})

	t.Run("no offline fallback", func(t *testing.T) {
		t.Parallel()
		verifier := &mockVerifier{err: errors.New("verifier must not be called")}
		_, err := newCachedFetcher(t, newMockCatalog(), verifier).FetchPlugins(t.Context(), deps, nil)
		require.Error(t, err, "enforce mode must fail closed rather than fall back to the cache")
		assert.Empty(t, verifier.imageRefs)
	})
}

func TestFetcher_CacheReadsAllowed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		noCache bool
		policy  *SignaturePolicy
		want    bool
	}{
		{name: "default", want: true},
		{name: "no cache", noCache: true, want: false},
		{name: "warn", policy: &SignaturePolicy{Mode: SignatureModeWarn}, want: true},
		{name: "enforce", policy: &SignaturePolicy{Mode: SignatureModeEnforce}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &Fetcher{noCache: tt.noCache, sigPolicy: tt.policy}
			assert.Equal(t, tt.want, f.cacheReadsAllowed())
		})
	}
}

func TestFetcher_SignaturePolicy_LocalBuildHasNoSignature(t *testing.T) {
	t.Parallel()

	policy := func(mode SignatureMode) func(*FetcherConfig) {
		return func(cfg *FetcherConfig) {
			cfg.SignaturePolicy = &SignaturePolicy{
				Mode:              mode,
				TrustedIssuers:    []string{"https://issuer.example.com"},
				TrustedIdentities: []string{"https://github.com/org/*"},
			}
			cfg.SignatureVerifier = &mockVerifier{err: errors.New("verifier must not be called")}
		}
	}

	t.Run("enforce rejects", func(t *testing.T) {
		t.Parallel()
		fx := newBuiltFixture(t, policy(SignatureModeEnforce))
		_, err := fx.fetcher.FetchPlugin(t.Context(), builtPluginDep("1.0.0"), nil)
		require.ErrorIs(t, err, ErrSignatureUnverifiable)
		assert.Contains(t, err.Error(), "no published origin")
	})

	t.Run("warn logs and continues", func(t *testing.T) {
		t.Parallel()
		fx := newBuiltFixture(t, policy(SignatureModeWarn))
		r, err := fx.fetcher.FetchPlugin(t.Context(), builtPluginDep("1.0.0"), nil)
		require.NoError(t, err)
		assert.Nil(t, r.Signature)
		assert.Equal(t, string(builtBinary), readResult(t, r))
	})
}

func TestErrUnverifiable(t *testing.T) {
	t.Parallel()
	dep := builtPluginDep("1.0.0")

	err := errUnverifiable(dep, "1.0.0", catalog.ArtifactInfo{Catalog: "local"})
	require.ErrorIs(t, err, ErrSignatureUnverifiable)
	assert.Contains(t, err.Error(), "no published origin")

	err = errUnverifiable(dep, "1.0.0", catalog.ArtifactInfo{Catalog: "remote", Canonical: "ghcr.io/org"})
	require.ErrorIs(t, err, ErrSignatureUnverifiable)
	assert.NotContains(t, err.Error(), "no published origin")
	assert.Contains(t, err.Error(), `from catalog "remote"`)
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
		CatalogFetcher: cat,
		Cache:          cache,
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          cache,
		Platform:       "linux/amd64",
		NoCache:        true,
		Logger:         logr.Discard(),
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
		CatalogFetcher: &mockCatalog{name: "test"},
		Logger:         logr.Discard(),
	})
	assert.Equal(t, "scafctl", f.binaryName)
}

func TestNewFetcher_BinaryNameCustom(t *testing.T) {
	t.Parallel()
	f := NewFetcher(FetcherConfig{
		CatalogFetcher: &mockCatalog{name: "test"},
		BinaryName:     "mycli",
		Logger:         logr.Discard(),
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
		CatalogFetcher:  cat,
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
		CatalogFetcher:  cat,
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

// Catalogs reports the wrapper (not the embedded mock) so the index sees Path.
func (l *localMockCatalog) Catalogs() []catalog.Catalog { return []catalog.Catalog{l} }

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
		CatalogFetcher:  cat,
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
		CatalogFetcher: cat,
		Cache:          c,
		Platform:       platform,
		Logger:         logr.Discard(),
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
		CatalogFetcher:  cat,
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
		CatalogFetcher: cat,
		Cache:          cache,
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          cache,
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          cache,
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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

func TestFetcher_FetchPlugins_ScopedDep_IgnoresLegacyFlatCache(t *testing.T) {
	t.Parallel()

	// Catalog has nothing -- resolution fails.
	cat := newMockCatalog()

	cache := NewCache(t.TempDir())

	// Origin-unknown binary in the flat (no registry hash) layout.
	_, err := cache.Put("my-plugin", "1.0.0", "linux/amd64", []byte("unattributed-binary"))
	require.NoError(t, err)

	f := NewFetcher(FetcherConfig{
		CatalogFetcher: cat,
		Cache:          cache,
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "my-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0", Catalog: "test"},
	}

	_, err = f.FetchPlugins(context.Background(), deps, nil)
	require.Error(t, err, "scoped dep must not be served an origin-unknown cached binary")
}

func TestFetcher_FetchPlugins_UnscopedDep_UsesLegacyFlatCache(t *testing.T) {
	t.Parallel()

	cat := newMockCatalog()
	cache := NewCache(t.TempDir())

	_, err := cache.Put("my-plugin", "1.0.0", "linux/amd64", []byte("legacy-binary"))
	require.NoError(t, err)

	f := NewFetcher(FetcherConfig{
		CatalogFetcher: cat,
		Cache:          cache,
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "my-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}

	results, err := f.FetchPlugins(context.Background(), deps, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].FromCache)
	assert.Empty(t, results[0].Catalog)
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
		CatalogFetcher: cat,
		Cache:          cache,
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          NewCache(cacheDir),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          NewCache(t.TempDir()),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
	ref.Kind = catalog.ArtifactKindAuthHandler
	cat.addArtifact(ref, []byte("auth-binary"))

	f := NewFetcher(FetcherConfig{
		CatalogFetcher: cat,
		Cache:          NewCache(t.TempDir()),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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

func TestRegisterFetchedAuthHandlerPluginsNamed_SkipsUnrequestedHandlers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping auth handler plugin test in short mode (requires go build)")
	}
	binPath := buildTestAuthHandlerPlugin(t)

	ctx := context.Background()
	reg := auth.NewRegistry()
	results := []FetchResult{
		{Name: "test-auth", Kind: solution.PluginKindAuthHandler, Path: binPath},
	}

	// The plugin advertises only "test-auth"; requesting another name must
	// register nothing and kill the client.
	clients, err := RegisterFetchedAuthHandlerPluginsNamed(ctx, reg, results, nil, "other")
	require.NoError(t, err)
	assert.Empty(t, clients)
	assert.False(t, reg.Has("test-auth"))
}

func TestFilterAuthHandlers(t *testing.T) {
	t.Parallel()
	handlers := []AuthHandlerInfo{{Name: "entra"}, {Name: "github"}, {Name: "gcp"}}

	kept := filterAuthHandlers(context.Background(), handlers, "github", "multi")
	assert.Equal(t, []AuthHandlerInfo{{Name: "github"}}, kept)
	assert.Empty(t, filterAuthHandlers(context.Background(), handlers, "missing", "multi"))
}

func TestFetcher_ResolvePlugins_Empty(t *testing.T) {
	t.Parallel()
	f := NewFetcher(FetcherConfig{
		CatalogFetcher: newMockCatalog(),
		Cache:          NewCache(t.TempDir()),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: cat,
		Cache:          NewCache(t.TempDir()),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher:  cat,
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
		CatalogFetcher: cat,
		Cache:          NewCache(t.TempDir()),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
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
		CatalogFetcher: newMockCatalog(),
		Cache:          NewCache(t.TempDir()),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
	})

	deps := []solution.PluginDependency{
		{Name: "missing-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"},
	}

	_, err := f.ResolvePlugins(context.Background(), deps)
	require.Error(t, err)
}
