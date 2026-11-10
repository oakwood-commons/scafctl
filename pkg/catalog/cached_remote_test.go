// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"net/http/httptest"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cachedTestName = "cached-prov"

type cachedRemoteFixture struct {
	cat     *CachedRemoteCatalog
	ts      *httptest.Server
	digests map[string]string
}

// newCachedRemoteFixture seeds a remote with 1.0.0 and 1.5.0, caches only
// 1.0.0 locally, and stores a local build of 2.0.0 that must never be served
// as a copy of the remote.
func newCachedRemoteFixture(t *testing.T) cachedRemoteFixture {
	t.Helper()
	ctx := t.Context()

	remote, ts := newTestRemoteCatalog(t)
	t.Cleanup(ts.Close)
	local := newTestLocalCatalog(t)

	digests := map[string]string{}
	for _, v := range []string{"1.0.0", "1.5.0"} {
		info, err := remote.Store(ctx, testRef(cachedTestName, v), []byte("v"+v), nil, nil, false)
		require.NoError(t, err)
		digests[v] = info.Digest
	}
	_, err := remote.CopyToLocal(ctx, testRef(cachedTestName, "1.0.0"), local, CopyOptions{})
	require.NoError(t, err)
	_, err = local.Store(ctx, testRef(cachedTestName, "2.0.0"), []byte("built"), nil, nil, false)
	require.NoError(t, err)

	cat, err := NewCachedRemoteCatalog(local, remote)
	require.NoError(t, err)

	return cachedRemoteFixture{
		cat:     cat,
		ts:      ts,
		digests: digests,
	}
}

func TestNewCachedRemoteCatalog_NilArgs(t *testing.T) {
	t.Parallel()
	remote, ts := newTestRemoteCatalog(t)
	t.Cleanup(ts.Close)
	local := newTestLocalCatalog(t)

	_, err := NewCachedRemoteCatalog(nil, remote)
	require.ErrorContains(t, err, "nil local catalog")

	_, err = NewCachedRemoteCatalog(local, nil)
	require.ErrorContains(t, err, "nil remote catalog")
}

func assertServedBy(t *testing.T, c *CachedRemoteCatalog, info ArtifactInfo, wantVersion string) {
	t.Helper()
	require.NotNil(t, info.Reference.Version)
	assert.Equal(t, wantVersion, info.Reference.Version.String())
	assert.Equal(t, c.Name(), info.Catalog)
}

func TestCachedRemoteCatalog_Find_RemoteReachable(t *testing.T) {
	t.Parallel()
	f := newCachedRemoteFixture(t)
	ctx := t.Context()

	tests := []struct {
		name        string
		query       ArtifactQuery
		wantVersion string
		wantErr     error
	}{
		{
			name:        "open version asks remote for newest",
			query:       ArtifactQuery{Name: cachedTestName, Kind: ArtifactKindProvider},
			wantVersion: "1.5.0",
		},
		{
			name:        "constraint asks remote",
			query:       ArtifactQuery{Name: cachedTestName, Kind: ArtifactKindProvider, Version: "^1.0.0"},
			wantVersion: "1.5.0",
		},
		{
			name:        "pinned version served from cache",
			query:       ArtifactQuery{Name: cachedTestName, Kind: ArtifactKindProvider, Version: "1.0.0"},
			wantVersion: "1.0.0",
		},
		{
			name:        "pinned version not cached comes from remote",
			query:       ArtifactQuery{Name: cachedTestName, Kind: ArtifactKindProvider, Version: "1.5.0"},
			wantVersion: "1.5.0",
		},
		{
			name:    "local build is not a copy of the remote",
			query:   ArtifactQuery{Name: cachedTestName, Kind: ArtifactKindProvider, Version: "2.0.0"},
			wantErr: ErrArtifactNotFound,
		},
		{
			name:    "built source is not-found",
			query:   ArtifactQuery{Name: cachedTestName, Kind: ArtifactKindProvider, Source: SourceSelector{Kind: SourceBuilt}},
			wantErr: ErrArtifactNotFound,
		},
		{
			name:    "other remote is not-found",
			query:   ArtifactQuery{Name: cachedTestName, Kind: ArtifactKindProvider, Source: SourceSelector{Kind: SourceRemote, Remote: "other.example.com/repo"}},
			wantErr: ErrArtifactNotFound,
		},
		{
			name:    "missing kind is invalid",
			query:   ArtifactQuery{Name: cachedTestName},
			wantErr: ErrInvalidQuery,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info, err := f.cat.Find(ctx, tt.query)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assertServedBy(t, f.cat, info, tt.wantVersion)
		})
	}
}

func TestCachedRemoteCatalog_Find_RemoteUnreachable(t *testing.T) {
	t.Parallel()
	f := newCachedRemoteFixture(t)
	f.ts.Close()
	ctx := t.Context()

	tests := []struct {
		name            string
		query           ArtifactQuery
		wantVersion     string
		wantUnreachable bool
	}{
		{
			name:        "open version falls back to newest cached copy",
			query:       ArtifactQuery{Name: cachedTestName, Kind: ArtifactKindProvider},
			wantVersion: "1.0.0",
		},
		{
			name:        "constraint falls back to cache",
			query:       ArtifactQuery{Name: cachedTestName, Kind: ArtifactKindProvider, Version: "^1.0.0"},
			wantVersion: "1.0.0",
		},
		{
			name:        "pinned version served from cache",
			query:       ArtifactQuery{Name: cachedTestName, Kind: ArtifactKindProvider, Version: "1.0.0"},
			wantVersion: "1.0.0",
		},
		{
			name:        "pinned digest served from cache",
			query:       ArtifactQuery{Name: cachedTestName, Kind: ArtifactKindProvider, Digest: f.digests["1.0.0"]},
			wantVersion: "1.0.0",
		},
		{
			name:            "pinned version not cached reports unreachable",
			query:           ArtifactQuery{Name: cachedTestName, Kind: ArtifactKindProvider, Version: "1.5.0"},
			wantUnreachable: true,
		},
		{
			name:            "constraint matching only a local build reports unreachable",
			query:           ArtifactQuery{Name: cachedTestName, Kind: ArtifactKindProvider, Version: ">=2.0.0"},
			wantUnreachable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info, err := f.cat.Find(ctx, tt.query)
			if tt.wantUnreachable {
				_, unreachable := IsCatalogUnreachable(err)
				assert.True(t, unreachable, "want unreachable error, got %v", err)
				return
			}
			require.NoError(t, err)
			assertServedBy(t, f.cat, info, tt.wantVersion)
			assert.Equal(t, f.cat.canonicalID(), info.Canonical)
		})
	}
}

func TestCachedRemoteCatalog_Resolve(t *testing.T) {
	t.Parallel()

	t.Run("reachable", func(t *testing.T) {
		t.Parallel()
		f := newCachedRemoteFixture(t)
		ctx := t.Context()

		info, err := f.cat.Resolve(ctx, testRef(cachedTestName, ""))
		require.NoError(t, err)
		assertServedBy(t, f.cat, info, "1.5.0")

		info, err = f.cat.Resolve(ctx, testRef(cachedTestName, "1.0.0"))
		require.NoError(t, err)
		assertServedBy(t, f.cat, info, "1.0.0")

		info, err = f.cat.Resolve(ctx, testRef(cachedTestName, "1.5.0"))
		require.NoError(t, err)
		assertServedBy(t, f.cat, info, "1.5.0")
	})

	t.Run("unreachable", func(t *testing.T) {
		t.Parallel()
		f := newCachedRemoteFixture(t)
		f.ts.Close()
		ctx := t.Context()

		info, err := f.cat.Resolve(ctx, testRef(cachedTestName, ""))
		require.NoError(t, err)
		assertServedBy(t, f.cat, info, "1.0.0")

		info, err = f.cat.Resolve(ctx, testRef(cachedTestName, "1.0.0"))
		require.NoError(t, err)
		assertServedBy(t, f.cat, info, "1.0.0")

		digestRef := Reference{Kind: ArtifactKindProvider, Name: cachedTestName, Digest: f.digests["1.0.0"]}
		info, err = f.cat.Resolve(ctx, digestRef)
		require.NoError(t, err)
		assertServedBy(t, f.cat, info, "1.0.0")

		for _, ref := range []Reference{
			testRef(cachedTestName, "1.5.0"),
			testRef(cachedTestName, "2.0.0"),
		} {
			_, err = f.cat.Resolve(ctx, ref)
			_, unreachable := IsCatalogUnreachable(err)
			assert.True(t, unreachable, "%s: want unreachable error, got %v", ref.String(), err)
		}
	})
}

func TestIsPinnedQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query ArtifactQuery
		want  bool
	}{
		{name: "open", query: ArtifactQuery{}, want: false},
		{name: "latest", query: ArtifactQuery{Version: "latest"}, want: false},
		{name: "exact version", query: ArtifactQuery{Version: "1.2.3"}, want: true},
		{name: "constraint", query: ArtifactQuery{Version: "^1.2.0"}, want: false},
		{name: "digest", query: ArtifactQuery{Digest: "sha256:abc"}, want: true},
		{name: "digest with constraint", query: ArtifactQuery{Version: "~1.2", Digest: "sha256:abc"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isPinnedQuery(tt.query))
		})
	}
}

const cachedPlatformName = "cached-multi"

// newCachedPlatformFixture publishes multi-platform 2.0.0 and 3.0.0 (linux
// and darwin) to a remote and caches only 2.0.0 locally.
func newCachedPlatformFixture(t *testing.T) cachedRemoteFixture {
	t.Helper()
	remote, ts := newTestRemoteCatalog(t)
	t.Cleanup(ts.Close)
	local := newTestLocalCatalog(t)

	for _, v := range []string{"2.0.0", "3.0.0"} {
		storeMultiPlatformInFake(t, remote, testRef(cachedPlatformName, v), []PlatformBinary{
			{Platform: "linux/amd64", Data: []byte("linux-" + v)},
			{Platform: "darwin/arm64", Data: []byte("darwin-" + v)},
		})
	}
	_, err := remote.CopyToLocal(t.Context(), testRef(cachedPlatformName, "2.0.0"), local, CopyOptions{})
	require.NoError(t, err)

	cat, err := NewCachedRemoteCatalog(local, remote)
	require.NoError(t, err)

	return cachedRemoteFixture{
		cat: cat,
		ts:  ts,
	}
}

func TestCachedRemoteCatalog_FetchByPlatform_RemoteReachable(t *testing.T) {
	t.Parallel()
	f := newCachedPlatformFixture(t)
	ctx := t.Context()

	tests := []struct {
		name     string
		ref      Reference
		platform string
		wantData string
	}{
		{name: "pinned cached", ref: testRef(cachedPlatformName, "2.0.0"), platform: "linux/amd64", wantData: "linux-2.0.0"},
		{name: "pinned not cached comes from remote", ref: testRef(cachedPlatformName, "3.0.0"), platform: "darwin/arm64", wantData: "darwin-3.0.0"},
		{name: "unpinned resolves remote newest", ref: testRef(cachedPlatformName, ""), platform: "linux/amd64", wantData: "linux-3.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, info, err := f.cat.FetchByPlatform(ctx, tt.ref, tt.platform)
			require.NoError(t, err)
			assert.Equal(t, tt.wantData, string(data))
			assert.Equal(t, f.cat.Name(), info.Catalog)
		})
	}
}

func TestCachedRemoteCatalog_FetchByPlatform_DoesNotWriteCache(t *testing.T) {
	t.Parallel()
	f := newCachedPlatformFixture(t)
	ctx := t.Context()
	ref := testRef(cachedPlatformName, "3.0.0")

	_, _, err := f.cat.FetchByPlatform(ctx, ref, "linux/amd64")
	require.NoError(t, err)

	_, _, err = f.cat.local.FetchByPlatform(ctx, f.cat.localRef(ref), "linux/amd64")
	require.ErrorIs(t, err, ErrArtifactNotFound)
}

func TestCachedRemoteCatalog_FetchByPlatform_RemoteUnreachable(t *testing.T) {
	t.Parallel()
	f := newCachedPlatformFixture(t)
	f.ts.Close()
	ctx := t.Context()

	t.Run("pinned served from cache", func(t *testing.T) {
		t.Parallel()
		data, info, err := f.cat.FetchByPlatform(ctx, testRef(cachedPlatformName, "2.0.0"), "darwin/arm64")
		require.NoError(t, err)
		assert.Equal(t, "darwin-2.0.0", string(data))
		assert.Equal(t, f.cat.Name(), info.Catalog)
		assert.Equal(t, f.cat.canonicalID(), info.Canonical)
	})

	t.Run("cached copy without platform is platform-not-found", func(t *testing.T) {
		t.Parallel()
		_, _, err := f.cat.FetchByPlatform(ctx, testRef(cachedPlatformName, "2.0.0"), "windows/amd64")
		assert.True(t, IsPlatformNotFound(err), "want platform-not-found, got %v", err)
	})

	t.Run("pinned not cached fails", func(t *testing.T) {
		t.Parallel()
		_, _, err := f.cat.FetchByPlatform(ctx, testRef(cachedPlatformName, "3.0.0"), "linux/amd64")
		require.Error(t, err)
	})

	t.Run("unpinned skips cache and reports unreachable", func(t *testing.T) {
		t.Parallel()
		_, _, err := f.cat.FetchByPlatform(ctx, testRef(cachedPlatformName, ""), "linux/amd64")
		_, unreachable := IsCatalogUnreachable(err)
		assert.True(t, unreachable, "want unreachable error, got %v", err)
	})
}

func TestCachedRemoteCatalog_ResolveContentDigest_RemoteReachable(t *testing.T) {
	t.Parallel()
	f := newCachedPlatformFixture(t)
	ctx := t.Context()

	tests := []struct {
		name     string
		ref      Reference
		platform string
		wantData string
	}{
		{name: "pinned cached", ref: testRef(cachedPlatformName, "2.0.0"), platform: "linux/amd64", wantData: "linux-2.0.0"},
		{name: "pinned not cached comes from remote", ref: testRef(cachedPlatformName, "3.0.0"), platform: "darwin/arm64", wantData: "darwin-3.0.0"},
		{name: "unpinned resolves remote newest", ref: testRef(cachedPlatformName, ""), platform: "linux/amd64", wantData: "linux-3.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := f.cat.ResolveContentDigest(ctx, tt.ref, tt.platform, MediaTypeProviderBinary)
			require.NoError(t, err)
			assert.Equal(t, digest.FromString(tt.wantData).String(), got.ContentDigest)
			assert.Equal(t, f.cat.Name(), got.Catalog)
		})
	}
}

func TestCachedRemoteCatalog_ResolveContentDigest_RemoteUnreachable(t *testing.T) {
	t.Parallel()
	f := newCachedPlatformFixture(t)
	f.ts.Close()
	ctx := t.Context()

	t.Run("pinned served from cache", func(t *testing.T) {
		t.Parallel()
		got, err := f.cat.ResolveContentDigest(ctx, testRef(cachedPlatformName, "2.0.0"), "darwin/arm64", MediaTypeProviderBinary)
		require.NoError(t, err)
		assert.Equal(t, digest.FromString("darwin-2.0.0").String(), got.ContentDigest)
		assert.Equal(t, f.cat.Name(), got.Catalog)
		assert.Equal(t, f.cat.canonicalID(), got.Canonical)
	})

	t.Run("cached copy without platform is platform-not-found", func(t *testing.T) {
		t.Parallel()
		_, err := f.cat.ResolveContentDigest(ctx, testRef(cachedPlatformName, "2.0.0"), "windows/amd64", MediaTypeProviderBinary)
		assert.True(t, IsPlatformNotFound(err), "want platform-not-found, got %v", err)
	})

	t.Run("pinned not cached fails", func(t *testing.T) {
		t.Parallel()
		_, err := f.cat.ResolveContentDigest(ctx, testRef(cachedPlatformName, "3.0.0"), "linux/amd64", MediaTypeProviderBinary)
		require.Error(t, err)
	})

	t.Run("unpinned skips cache and reports unreachable", func(t *testing.T) {
		t.Parallel()
		_, err := f.cat.ResolveContentDigest(ctx, testRef(cachedPlatformName, ""), "linux/amd64", MediaTypeProviderBinary)
		_, unreachable := IsCatalogUnreachable(err)
		assert.True(t, unreachable, "want unreachable error, got %v", err)
	})
}

// A cached copy must carry the same ImageRef the remote reports, so its
// signature is verified against the registry exactly as for a live fetch.
func TestCachedRemoteCatalog_FetchByPlatform_CachedCopyImageRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  func(*testing.T) cachedRemoteFixture
		ref      Reference
		platform string
	}{
		{name: "multi-platform index", fixture: newCachedPlatformFixture, ref: testRef(cachedPlatformName, "2.0.0"), platform: "darwin/arm64"},
		{name: "single-platform manifest", fixture: newCachedRemoteFixture, ref: testRef(cachedTestName, "1.0.0"), platform: "linux/amd64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := tt.fixture(t)
			ctx := t.Context()

			_, live, err := f.cat.RemoteCatalog.FetchByPlatform(ctx, tt.ref, tt.platform)
			require.NoError(t, err)
			require.NotEmpty(t, live.ImageRef)

			// With the remote gone, the copy can only come from the cache.
			f.ts.Close()
			_, cached, err := f.cat.FetchByPlatform(ctx, tt.ref, tt.platform)
			require.NoError(t, err)
			assert.Equal(t, live.ImageRef, cached.ImageRef)
			assert.Equal(t, live.ManifestDigest, cached.ManifestDigest)
			assert.Equal(t, live.Digest, cached.Digest)
		})
	}
}

func TestCachedRemoteCatalog_FromCachedFetch_NoManifestDigest(t *testing.T) {
	t.Parallel()
	f := newCachedRemoteFixture(t)
	info := f.cat.fromCachedFetch(testRef(cachedTestName, "1.0.0"), ArtifactInfo{})
	assert.Empty(t, info.ImageRef, "no manifest digest leaves nothing to verify, which enforcement rejects")
	assert.Equal(t, f.cat.canonicalID(), info.Canonical)
}
