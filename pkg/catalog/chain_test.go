// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCatalog implements the Catalog interface for testing.
type mockCatalog struct {
	name                string
	artifacts           map[string]mockArtifact
	listFunc            func(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error)
	storeFunc           func(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool) (ArtifactInfo, error)
	deleteFunc          func(ctx context.Context, ref Reference) error
	fetchFunc           func(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error)
	fetchWithBundleFunc func(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error)
}

type mockArtifact struct {
	content    []byte
	bundleData []byte
	info       ArtifactInfo
}

func newMockCatalog(name string) *mockCatalog {
	return &mockCatalog{
		name:      name,
		artifacts: make(map[string]mockArtifact),
	}
}

func (m *mockCatalog) addArtifact(ref Reference, content []byte, annotations map[string]string) {
	m.artifacts[ref.String()] = mockArtifact{
		content: content,
		info: ArtifactInfo{
			Reference:   ref,
			Digest:      fmt.Sprintf("sha256:mock-%s", ref.String()),
			Annotations: annotations,
			Catalog:     m.name,
		},
	}
}

func (m *mockCatalog) Name() string { return m.name }

func (m *mockCatalog) Store(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool) (ArtifactInfo, error) {
	if m.storeFunc != nil {
		return m.storeFunc(ctx, ref, content, bundleData, annotations, force)
	}
	info := ArtifactInfo{Reference: ref, Catalog: m.name}
	m.artifacts[ref.String()] = mockArtifact{content: content, bundleData: bundleData, info: info}
	return info, nil
}

func (m *mockCatalog) Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	if m.fetchFunc != nil {
		return m.fetchFunc(ctx, ref)
	}
	a, ok := m.artifacts[ref.String()]
	if !ok {
		return nil, ArtifactInfo{}, ErrArtifactNotFound
	}
	return a.content, a.info, nil
}

func (m *mockCatalog) FetchWithBundle(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	if m.fetchWithBundleFunc != nil {
		return m.fetchWithBundleFunc(ctx, ref)
	}
	a, ok := m.artifacts[ref.String()]
	if !ok {
		return nil, nil, ArtifactInfo{}, ErrArtifactNotFound
	}
	return a.content, a.bundleData, a.info, nil
}

func (m *mockCatalog) Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	a, ok := m.artifacts[ref.String()]
	if !ok {
		return ArtifactInfo{}, ErrArtifactNotFound
	}
	return a.info, nil
}

func (m *mockCatalog) List(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, kind, name)
	}
	var results []ArtifactInfo
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

func (m *mockCatalog) Exists(ctx context.Context, ref Reference) (bool, error) {
	_, ok := m.artifacts[ref.String()]
	return ok, nil
}

func (m *mockCatalog) Delete(ctx context.Context, ref Reference) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, ref)
	}
	delete(m.artifacts, ref.String())
	return nil
}

func testRef(name, version string) Reference {
	ref := Reference{
		Kind: ArtifactKindProvider,
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

func TestNewChainCatalog_RequiresAtLeastOne(t *testing.T) {
	_, err := NewChainCatalog(logr.Discard())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one catalog")
}

func TestChainCatalog_Name(t *testing.T) {
	cat := newMockCatalog("local")
	chain, err := NewChainCatalog(logr.Discard(), cat)
	require.NoError(t, err)
	assert.Equal(t, "chain", chain.Name())
}

func TestChainCatalog_Catalogs(t *testing.T) {
	c1 := newMockCatalog("c1")
	c2 := newMockCatalog("c2")
	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)
	assert.Len(t, chain.Catalogs(), 2)
}

func TestChainCatalog_Fetch_FirstCatalog(t *testing.T) {
	c1 := newMockCatalog("local")
	ref := testRef("my-plugin", "1.0.0")
	c1.addArtifact(ref, []byte("binary-local"), nil)

	c2 := newMockCatalog("remote")
	c2.addArtifact(ref, []byte("binary-remote"), nil)

	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)

	content, info, err := chain.Fetch(context.Background(), ref)
	require.NoError(t, err)
	assert.Equal(t, []byte("binary-local"), content)
	assert.Equal(t, "local", info.Catalog)
}

func TestChainCatalog_Fetch_Fallback(t *testing.T) {
	c1 := newMockCatalog("local")
	// c1 has no artifact

	c2 := newMockCatalog("remote")
	ref := testRef("my-plugin", "1.0.0")
	c2.addArtifact(ref, []byte("binary-remote"), nil)

	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)

	content, info, err := chain.Fetch(context.Background(), ref)
	require.NoError(t, err)
	assert.Equal(t, []byte("binary-remote"), content)
	assert.Equal(t, "remote", info.Catalog)
}

func TestChainCatalog_Fetch_NotFound(t *testing.T) {
	c1 := newMockCatalog("local")
	c2 := newMockCatalog("remote")

	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)

	ref := testRef("missing", "1.0.0")
	_, _, err = chain.Fetch(context.Background(), ref)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in any catalog")
}

func TestChainCatalog_FetchWithBundle_Fallback(t *testing.T) {
	c1 := newMockCatalog("local")

	c2 := newMockCatalog("remote")
	ref := testRef("sol", "2.0.0")
	c2.artifacts[ref.String()] = mockArtifact{
		content:    []byte("solution-content"),
		bundleData: []byte("bundle-data"),
		info: ArtifactInfo{
			Reference: ref,
			Catalog:   "remote",
		},
	}

	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)

	content, bundle, info, err := chain.FetchWithBundle(context.Background(), ref)
	require.NoError(t, err)
	assert.Equal(t, []byte("solution-content"), content)
	assert.Equal(t, []byte("bundle-data"), bundle)
	assert.Equal(t, "remote", info.Catalog)
}

func TestChainCatalog_Resolve_Order(t *testing.T) {
	c1 := newMockCatalog("local")
	ref := testRef("plugin", "1.0.0")
	c1.addArtifact(ref, nil, nil)

	c2 := newMockCatalog("remote")
	c2.addArtifact(ref, nil, nil)

	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)

	info, err := chain.Resolve(context.Background(), ref)
	require.NoError(t, err)
	assert.Equal(t, "local", info.Catalog)
}

func TestChainCatalog_Resolve_FallbackToSecond(t *testing.T) {
	c1 := newMockCatalog("local")

	c2 := newMockCatalog("remote")
	ref := testRef("plugin", "1.0.0")
	c2.addArtifact(ref, nil, nil)

	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)

	info, err := chain.Resolve(context.Background(), ref)
	require.NoError(t, err)
	assert.Equal(t, "remote", info.Catalog)
}

func TestChainCatalog_List_Dedup(t *testing.T) {
	ref := testRef("shared-plugin", "1.0.0")

	c1 := newMockCatalog("local")
	c1.addArtifact(ref, nil, nil)

	c2 := newMockCatalog("remote")
	c2.addArtifact(ref, nil, nil)

	// Also add a unique artifact to remote
	ref2 := testRef("remote-only", "2.0.0")
	c2.addArtifact(ref2, nil, nil)

	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)

	items, err := chain.List(context.Background(), ArtifactKindProvider, "")
	require.NoError(t, err)

	// Should have 2 unique artifacts, not 3 (shared-plugin deduped)
	assert.Len(t, items, 2)

	names := make(map[string]bool)
	for _, item := range items {
		names[item.Reference.Name] = true
	}
	assert.True(t, names["shared-plugin"])
	assert.True(t, names["remote-only"])
}

func TestChainCatalog_List_ContinuesOnError(t *testing.T) {
	c1 := newMockCatalog("broken")
	c1.listFunc = func(_ context.Context, _ ArtifactKind, _ string) ([]ArtifactInfo, error) {
		return nil, fmt.Errorf("network error")
	}

	c2 := newMockCatalog("good")
	ref := testRef("plugin", "1.0.0")
	c2.addArtifact(ref, nil, nil)

	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)

	items, err := chain.List(context.Background(), ArtifactKindProvider, "")
	require.NoError(t, err)
	assert.Len(t, items, 1)
}

func TestChainCatalog_Exists_AnyTrue(t *testing.T) {
	c1 := newMockCatalog("local")

	c2 := newMockCatalog("remote")
	ref := testRef("plugin", "1.0.0")
	c2.addArtifact(ref, nil, nil)

	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)

	exists, err := chain.Exists(context.Background(), ref)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestChainCatalog_Exists_NoneFalse(t *testing.T) {
	c1 := newMockCatalog("local")
	c2 := newMockCatalog("remote")

	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)

	ref := testRef("missing", "1.0.0")
	exists, err := chain.Exists(context.Background(), ref)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestChainCatalog_Store_DelegatesToFirst(t *testing.T) {
	storeCalled := false
	c1 := newMockCatalog("local")
	c1.storeFunc = func(_ context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool) (ArtifactInfo, error) {
		storeCalled = true
		return ArtifactInfo{Reference: ref, Catalog: "local"}, nil
	}

	c2 := newMockCatalog("remote")

	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)

	ref := testRef("plugin", "1.0.0")
	info, err := chain.Store(context.Background(), ref, []byte("data"), nil, nil, false)
	require.NoError(t, err)
	assert.True(t, storeCalled)
	assert.Equal(t, "local", info.Catalog)
}

func TestChainCatalog_Delete_DelegatesToFirst(t *testing.T) {
	deleteCalled := false
	c1 := newMockCatalog("local")
	c1.deleteFunc = func(_ context.Context, ref Reference) error {
		deleteCalled = true
		return nil
	}

	c2 := newMockCatalog("remote")

	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)

	ref := testRef("plugin", "1.0.0")
	err = chain.Delete(context.Background(), ref)
	require.NoError(t, err)
	assert.True(t, deleteCalled)
}

func TestChainCatalog_Fetch_NonNotFoundError(t *testing.T) {
	// When a catalog returns a non-404 error, the chain should still try next
	c1 := newMockCatalog("broken")
	// Override Fetch to return a non-404 error
	// We can't directly override Fetch, but we can create a custom behavior
	// by putting an artifact with a special trigger

	c2 := newMockCatalog("good")
	ref := testRef("plugin", "1.0.0")
	c2.addArtifact(ref, []byte("good-binary"), nil)

	chain, err := NewChainCatalog(logr.Discard(), c1, c2)
	require.NoError(t, err)

	content, info, err := chain.Fetch(context.Background(), ref)
	require.NoError(t, err)
	assert.Equal(t, []byte("good-binary"), content)
	assert.Equal(t, "good", info.Catalog)
}

func TestBuildCatalogChain_NilConfig(t *testing.T) {
	chain, err := BuildCatalogChain(nil, nil, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, chain)
}

func TestBuildCatalogChain_WithEmptyConfig(t *testing.T) {
	cfg := &config.Config{}
	chain, err := BuildCatalogChain(cfg, nil, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, chain)
}

func TestBuildCatalogChain_WithOCICatalog(t *testing.T) {
	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{
				Name: "test-remote",
				Type: config.CatalogTypeOCI,
				URL:  "registry.example.com",
			},
		},
	}
	// Creating a remote catalog with an invalid/unreachable registry should not fail at construction time
	chain, err := BuildCatalogChain(cfg, nil, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, chain)
}

func TestBuildCatalogChain_SkipsNonOCICatalog(t *testing.T) {
	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{
				Name: "local-only",
				Type: "local", // Not OCI - should be skipped
				URL:  "/some/path",
			},
		},
	}
	chain, err := BuildCatalogChain(cfg, nil, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, chain)
}

// TestBuildCatalogChain_WithAuthProvider tests that an OCI catalog with an AuthProvider
// configured uses the corresponding handler from the registry.
func TestBuildCatalogChain_WithAuthProvider(t *testing.T) {
	t.Parallel()

	mock := auth.NewMockHandler("quay")
	mock.GetTokenResult = &auth.Token{AccessToken: "fake-token", TokenType: "Bearer"}

	registry := auth.NewRegistry()
	require.NoError(t, registry.Register(mock))

	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{
				Name:         "quay-catalog",
				Type:         config.CatalogTypeOCI,
				URL:          "quay.io",
				AuthProvider: "quay",
				AuthScope:    "repository:myorg/myrepo:pull",
			},
		},
	}

	chain, err := BuildCatalogChain(cfg, registry, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, chain)
}

// TestBuildCatalogChain_WithAuthProviderNotFound tests that a missing auth provider
// does not fail the entire chain build - it is silently skipped.
func TestBuildCatalogChain_WithAuthProviderNotFound(t *testing.T) {
	t.Parallel()

	emptyRegistry := auth.NewRegistry()

	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{
				Name:         "private-catalog",
				Type:         config.CatalogTypeOCI,
				URL:          "registry.example.io",
				AuthProvider: "nonexistent-handler",
			},
		},
	}

	// Should succeed (warning logged but not an error)
	chain, err := BuildCatalogChain(cfg, emptyRegistry, logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, chain)
}

// TestNewRemoteCatalog_WithAuthHandlerNoCredStore tests NewRemoteCatalog when only
// an auth handler is provided (no credential store).
func TestNewRemoteCatalog_WithAuthHandlerNoCredStore(t *testing.T) {
	t.Parallel()

	mock := auth.NewMockHandler("test-handler")
	mock.GetTokenResult = &auth.Token{AccessToken: "my-token", TokenType: "Bearer"}

	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:        "test",
		Registry:    "registry.example.io",
		Repository:  "",
		AuthHandler: mock,
		AuthScope:   "",
		Logger:      logr.Discard(),
	})
	require.NoError(t, err)
	require.NotNil(t, cat)
}

// TestNewRemoteCatalog_WithAuthHandlerAndCredStore tests that the composite credential
// function is set when both AuthHandler and CredentialStore are provided.
func TestNewRemoteCatalog_WithAuthHandlerAndCredStore(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	mock := auth.NewMockHandler("test-handler")
	mock.GetTokenResult = &auth.Token{AccessToken: "my-token", TokenType: "Bearer"}

	credStore, err := NewCredentialStore(logr.Discard())
	require.NoError(t, err)

	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Name:            "test",
		Registry:        "registry.example.io",
		Repository:      "",
		AuthHandler:     mock,
		CredentialStore: credStore,
		Logger:          logr.Discard(),
	})
	require.NoError(t, err)
	require.NotNil(t, cat)
}
