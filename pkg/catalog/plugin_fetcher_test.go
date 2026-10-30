// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePluginSource returns a fixed result (or error) and counts calls.
type fakePluginSource struct {
	name  string
	info  ArtifactInfo
	err   error
	calls int
}

func (s *fakePluginSource) Name() string { return s.name }

func (s *fakePluginSource) FetchByPlatform(_ context.Context, _ Reference, _ string) ([]byte, ArtifactInfo, error) {
	s.calls++
	if s.err != nil {
		return nil, ArtifactInfo{}, s.err
	}
	return []byte(s.name), s.info, nil
}

func (s *fakePluginSource) ResolveContentDigest(_ context.Context, _ Reference, _, _ string) (ContentDigestInfo, error) {
	s.calls++
	if s.err != nil {
		return ContentDigestInfo{}, s.err
	}
	return ContentDigestInfo{ArtifactInfo: s.info, ContentDigest: s.name}, nil
}

func (s *fakePluginSource) Find(_ context.Context, _ ArtifactQuery) (ArtifactInfo, error) {
	s.calls++
	if s.err != nil {
		return ArtifactInfo{}, s.err
	}
	return s.info, nil
}

func notFoundSource(name string) *fakePluginSource {
	return &fakePluginSource{name: name, err: &ArtifactNotFoundError{Reference: Reference{Name: "p"}, Catalog: name}}
}

func okSource(name string) *fakePluginSource {
	return &fakePluginSource{name: name, info: ArtifactInfo{Catalog: name}}
}

// chainOps exercises every chain operation and returns the serving catalog.
var chainOps = map[string]func(c *pluginSourceChain) (string, error){
	"FetchByPlatform": func(c *pluginSourceChain) (string, error) {
		_, info, err := c.FetchByPlatform(context.Background(), testRef("p", "1.0.0"), "linux/amd64")
		return info.Catalog, err
	},
	"ResolveContentDigest": func(c *pluginSourceChain) (string, error) {
		info, err := c.ResolveContentDigest(context.Background(), testRef("p", "1.0.0"), "linux/amd64", MediaTypeProviderBinary)
		return info.Catalog, err
	},
	"Find": func(c *pluginSourceChain) (string, error) {
		info, err := c.Find(context.Background(), ArtifactQuery{Name: "p", Kind: ArtifactKindProvider})
		return info.Catalog, err
	},
}

func TestPluginSourceChain(t *testing.T) {
	t.Parallel()

	for opName, op := range chainOps {
		t.Run(opName, func(t *testing.T) {
			t.Parallel()

			t.Run("not-found falls through", func(t *testing.T) {
				t.Parallel()
				first, second := notFoundSource("a"), okSource("b")
				got, err := op(newPluginSourceChain(logr.Discard(), nil, first, second))
				require.NoError(t, err)
				assert.Equal(t, "b", got)
				assert.Equal(t, 1, first.calls)
			})

			t.Run("other error halts", func(t *testing.T) {
				t.Parallel()
				first := &fakePluginSource{name: "a", err: errors.New("corrupt store")}
				second := okSource("b")
				_, err := op(newPluginSourceChain(logr.Discard(), nil, first, second))
				require.ErrorContains(t, err, "corrupt store")
				assert.NotErrorIs(t, err, ErrArtifactNotFound)
				assert.Zero(t, second.calls, "lower-priority source must not be tried")
			})

			t.Run("unreachable halts and is reported", func(t *testing.T) {
				t.Parallel()
				first := &fakePluginSource{name: "a", err: &UnreachableError{Catalog: "a", Cause: errors.New("dial tcp")}}
				second := okSource("b")
				_, err := op(newPluginSourceChain(logr.Discard(), nil, first, second))
				_, unreachable := IsCatalogUnreachable(err)
				assert.True(t, unreachable, "want unreachable, got %v", err)
				assert.Zero(t, second.calls)
			})

			t.Run("platform not found halts unchanged", func(t *testing.T) {
				t.Parallel()
				first := &fakePluginSource{name: "a", err: &PlatformNotFoundError{Platform: "linux/amd64"}}
				second := okSource("b")
				_, err := op(newPluginSourceChain(logr.Discard(), nil, first, second))
				var pnf *PlatformNotFoundError
				require.ErrorAs(t, err, &pnf)
				assert.Zero(t, second.calls)
			})

			t.Run("all not-found wraps ErrArtifactNotFound", func(t *testing.T) {
				t.Parallel()
				_, err := op(newPluginSourceChain(logr.Discard(), nil, notFoundSource("a"), notFoundSource("b")))
				require.ErrorIs(t, err, ErrArtifactNotFound)
			})

			t.Run("empty chain is not-found", func(t *testing.T) {
				t.Parallel()
				_, err := op(newPluginSourceChain(logr.Discard(), nil))
				require.ErrorIs(t, err, ErrArtifactNotFound)
			})
		})
	}
}

func TestNewPluginFetcher_DuplicateName(t *testing.T) {
	t.Parallel()
	local := newTestLocalCatalog(t)

	_, err := NewPluginFetcher(logr.Discard(), local, okSource(local.Name()))
	require.ErrorContains(t, err, "duplicate plugin source")

	_, err = NewPluginFetcher(logr.Discard(), nil, okSource("a"), okSource("a"))
	require.ErrorContains(t, err, "duplicate plugin source")
}

func TestPluginFetcher_Catalogs(t *testing.T) {
	t.Parallel()
	local := newTestLocalCatalog(t)
	mock := NewMockPluginSource("mock")
	wrapped := newAllowlistPluginSource(NewMockPluginSource("wrapped"), nil)

	f, err := NewPluginFetcher(logr.Discard(), local, okSource("not-a-catalog"), mock, wrapped)
	require.NoError(t, err)

	cats := f.Catalogs()
	require.Len(t, cats, 3, "sources that are not catalogs are omitted")
	assert.Same(t, local, cats[0], "localPlugin is unwrapped to its LocalCatalog")
	assert.Same(t, mock, cats[1])
	assert.Same(t, wrapped.inner, cats[2], "decorators are unwrapped")
}

func TestNewLocalPlugin_Nil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, newLocalPlugin(nil))

	f, err := NewPluginFetcher(logr.Discard(), nil, okSource("a"))
	require.NoError(t, err)
	assert.Len(t, f.byCatalog, 1)
}

func TestPluginFetcher_UnknownCatalog(t *testing.T) {
	t.Parallel()
	f, err := NewPluginFetcher(logr.Discard(), nil, okSource("a"))
	require.NoError(t, err)
	ctx := t.Context()
	scope := WithCatalog("nope")

	_, err = f.Find(ctx, ArtifactQuery{Name: "p", Kind: ArtifactKindProvider}, scope)
	require.ErrorContains(t, err, `catalog "nope" not configured`)
	_, err = f.ResolvePlugin(ctx, "p", ArtifactKindProvider, "", scope)
	require.ErrorContains(t, err, `catalog "nope" not configured`)
	_, _, err = f.FetchPlugin(ctx, "p", ArtifactKindProvider, "1.0.0", "linux/amd64", scope)
	require.ErrorContains(t, err, `catalog "nope" not configured`)
	_, err = f.ResolveContentDigest(ctx, "p", ArtifactKindProvider, "1.0.0", "linux/amd64", MediaTypeProviderBinary, scope)
	require.ErrorContains(t, err, `catalog "nope" not configured`)
}

func TestPluginFetcher_InvalidVersion(t *testing.T) {
	t.Parallel()
	f, err := NewPluginFetcher(logr.Discard(), nil, okSource("a"))
	require.NoError(t, err)
	ctx := t.Context()

	_, _, err = f.FetchPlugin(ctx, "p", ArtifactKindProvider, "not a version", "linux/amd64")
	require.ErrorContains(t, err, "invalid plugin reference")
	_, err = f.ResolveContentDigest(ctx, "p", ArtifactKindProvider, "not a version", "linux/amd64", MediaTypeProviderBinary)
	require.ErrorContains(t, err, "invalid plugin reference")
}

// pluginV2Fixture is a local catalog holding a built 1.0.0 plus a pulled copy
// of remote 2.0.0, in front of a remote serving 2.0.0 and 3.0.0.
type pluginV2Fixture struct {
	fetcher *PluginFetcher
	remote  *CachedRemoteCatalog
	local   *LocalCatalog
}

func newPluginV2Fixture(t *testing.T) pluginV2Fixture {
	t.Helper()
	cr := newCachedPlatformFixture(t)
	_, err := cr.cat.local.StoreMultiPlatform(t.Context(), testRef(cachedPlatformName, "1.0.0"), []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("linux-built")},
	}, nil, false)
	require.NoError(t, err)

	f, err := NewPluginFetcher(logr.Discard(), cr.cat.local, cr.cat)
	require.NoError(t, err)
	return pluginV2Fixture{fetcher: f, remote: cr.cat, local: cr.cat.local}
}

func TestPluginFetcher_ResolvePlugin(t *testing.T) {
	t.Parallel()
	fx := newPluginV2Fixture(t)
	ctx := t.Context()

	tests := []struct {
		name        string
		version     string
		opts        []Option
		wantVersion string
		wantCatalog string
	}{
		{name: "latest prefers local build", version: "", wantVersion: "1.0.0", wantCatalog: fx.local.Name()},
		{name: "constraint matching build stays local", version: ">=1.0.0", wantVersion: "1.0.0", wantCatalog: fx.local.Name()},
		{name: "constraint missing locally falls to remote", version: "^2.0.0", wantVersion: "2.0.0", wantCatalog: fx.remote.Name()},
		{name: "pulled copy is not a local build", version: "2.0.0", wantVersion: "2.0.0", wantCatalog: fx.remote.Name()},
		{name: "scoped to remote", version: "", opts: []Option{WithCatalog(fx.remote.Name())}, wantVersion: "3.0.0", wantCatalog: fx.remote.Name()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info, err := fx.fetcher.ResolvePlugin(ctx, cachedPlatformName, ArtifactKindProvider, tt.version, tt.opts...)
			require.NoError(t, err)
			require.NotNil(t, info.Reference.Version)
			assert.Equal(t, tt.wantVersion, info.Reference.Version.String())
			assert.Equal(t, tt.wantCatalog, info.Catalog)
		})
	}

	t.Run("scoped to local ignores pulled copies", func(t *testing.T) {
		t.Parallel()
		_, err := fx.fetcher.ResolvePlugin(ctx, cachedPlatformName, ArtifactKindProvider, "2.0.0", WithCatalog(fx.local.Name()))
		require.ErrorIs(t, err, ErrArtifactNotFound)
	})

	t.Run("invalid constraint halts", func(t *testing.T) {
		t.Parallel()
		_, err := fx.fetcher.ResolvePlugin(ctx, cachedPlatformName, ArtifactKindProvider, "not a version")
		require.ErrorIs(t, err, ErrInvalidQuery)
	})

	t.Run("Find delegates", func(t *testing.T) {
		t.Parallel()
		info, err := fx.fetcher.Find(ctx, ArtifactQuery{Name: cachedPlatformName, Kind: ArtifactKindProvider, Version: "3.0.0"})
		require.NoError(t, err)
		assert.Equal(t, fx.remote.Name(), info.Catalog)

		_, err = fx.fetcher.Find(ctx, ArtifactQuery{Name: "missing", Kind: ArtifactKindProvider})
		require.ErrorIs(t, err, ErrArtifactNotFound)
	})
}

func TestPluginFetcher_FetchPlugin(t *testing.T) {
	t.Parallel()
	fx := newPluginV2Fixture(t)
	ctx := t.Context()

	tests := []struct {
		name        string
		version     string
		platform    string
		wantData    string
		wantCatalog string
	}{
		{name: "local build", version: "1.0.0", platform: "linux/amd64", wantData: "linux-built", wantCatalog: fx.local.Name()},
		{name: "pulled copy served via remote", version: "2.0.0", platform: "linux/amd64", wantData: "linux-2.0.0", wantCatalog: fx.remote.Name()},
		{name: "remote only", version: "3.0.0", platform: "darwin/arm64", wantData: "darwin-3.0.0", wantCatalog: fx.remote.Name()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, info, err := fx.fetcher.FetchPlugin(ctx, cachedPlatformName, ArtifactKindProvider, tt.version, tt.platform)
			require.NoError(t, err)
			assert.Equal(t, tt.wantData, string(data))
			assert.Equal(t, tt.wantCatalog, info.Catalog)
		})
	}

	t.Run("platform missing from local build halts", func(t *testing.T) {
		t.Parallel()
		_, _, err := fx.fetcher.FetchPlugin(ctx, cachedPlatformName, ArtifactKindProvider, "1.0.0", "darwin/arm64")
		assert.True(t, IsPlatformNotFound(err), "want platform-not-found, got %v", err)
	})

	t.Run("missing everywhere is not-found", func(t *testing.T) {
		t.Parallel()
		_, _, err := fx.fetcher.FetchPlugin(ctx, cachedPlatformName, ArtifactKindProvider, "9.9.9", "linux/amd64")
		require.ErrorIs(t, err, ErrArtifactNotFound)
	})
}

func TestPluginFetcher_ResolveContentDigest(t *testing.T) {
	t.Parallel()
	fx := newPluginV2Fixture(t)
	ctx := t.Context()

	info, err := fx.fetcher.ResolveContentDigest(ctx, cachedPlatformName, ArtifactKindProvider, "3.0.0", "linux/amd64", MediaTypeProviderBinary)
	require.NoError(t, err)
	assert.NotEmpty(t, info.ContentDigest)
	assert.Equal(t, fx.remote.Name(), info.Catalog)

	info, err = fx.fetcher.ResolveContentDigest(ctx, cachedPlatformName, ArtifactKindProvider, "1.0.0", "linux/amd64", MediaTypeProviderBinary)
	require.NoError(t, err)
	assert.Equal(t, fx.local.Name(), info.Catalog)

	_, err = fx.fetcher.ResolveContentDigest(ctx, cachedPlatformName, ArtifactKindProvider, "9.9.9", "linux/amd64", MediaTypeProviderBinary)
	require.ErrorIs(t, err, ErrArtifactNotFound)
}
