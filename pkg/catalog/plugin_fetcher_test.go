// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePluginSource returns a fixed result (or error) and counts calls.
type fakePluginSource struct {
	mu        sync.Mutex
	name      string
	info      ArtifactInfo
	platforms []string
	err       error
	calls     int
}

func (s *fakePluginSource) Name() string { return s.name }

func (s *fakePluginSource) FetchByPlatform(_ context.Context, _ Reference, _ string) ([]byte, ArtifactInfo, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.err != nil {
		return nil, ArtifactInfo{}, s.err
	}
	return []byte(s.name), s.info, nil
}

func (s *fakePluginSource) ListPlatforms(_ context.Context, _ Reference) ([]string, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return s.platforms, nil
}

func (s *fakePluginSource) ResolveContentDigest(_ context.Context, _ Reference, _, _ string) (ContentDigestInfo, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.err != nil {
		return ContentDigestInfo{}, s.err
	}
	return ContentDigestInfo{ArtifactInfo: s.info, ContentDigest: s.name}, nil
}

func (s *fakePluginSource) Find(_ context.Context, _ ArtifactQuery) (ArtifactInfo, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.err != nil {
		return ArtifactInfo{}, s.err
	}
	return s.info, nil
}

func notFoundSource(name string) *fakePluginSource {
	return &fakePluginSource{name: name, err: &ArtifactNotFoundError{Reference: Reference{Name: "p"}, Catalog: name}}
}

func okSource(name string) *fakePluginSource {
	return &fakePluginSource{name: name, info: ArtifactInfo{Catalog: name}, platforms: []string{name}}
}

// chainOpsFor builds the 4-operation dispatch table targeting name@version.
func chainOpsFor(name, version string) map[string]func(c *pluginSourceChain) (string, error) {
	return map[string]func(c *pluginSourceChain) (string, error){
		"FetchByPlatform": func(c *pluginSourceChain) (string, error) {
			_, info, err := c.FetchByPlatform(context.Background(), testRef(name, version), "linux/amd64")
			return info.Catalog, err
		},
		"ListPlatforms": func(c *pluginSourceChain) (string, error) {
			platforms, err := c.ListPlatforms(context.Background(), testRef(name, version))
			if len(platforms) > 0 {
				return platforms[0], err
			}
			return "", err
		},
		"ResolveContentDigest": func(c *pluginSourceChain) (string, error) {
			info, err := c.ResolveContentDigest(context.Background(), testRef(name, version), "linux/amd64", MediaTypeProviderBinary)
			return info.Catalog, err
		},
		"Find": func(c *pluginSourceChain) (string, error) {
			info, err := c.Find(context.Background(), ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Version: version})
			return info.Catalog, err
		},
	}
}

// chainOps exercises every chain operation and returns the serving catalog.
var chainOps = chainOpsFor("p", "1.0.0")

func TestPluginSourceChain(t *testing.T) {
	t.Parallel()

	for opName, op := range chainOps {
		t.Run(opName, func(t *testing.T) {
			t.Parallel()

			t.Run("corrupt local build halts", func(t *testing.T) {
				t.Parallel()
				local := newTestLocalCatalog(t)
				storeCorruptPlugin(t, local, "p", "1.0.0")
				remote := okSource("remote")

				_, err := op(newPluginSourceChain(logr.Discard(), newLocalPlugin(local), remote))
				require.Error(t, err)
				assert.NotErrorIs(t, err, ErrArtifactNotFound)
				assert.Zero(t, remote.calls, "a corrupt local build must not fall through to a same-named remote plugin")
			})

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
	wrapped := newAllowlistPluginSource(newKindFilterPluginSource(NewMockPluginSource("wrapped"), []ArtifactKind{ArtifactKindProvider}), nil)

	f, err := NewPluginFetcher(logr.Discard(), local, okSource("not-a-catalog"), mock, wrapped)
	require.NoError(t, err)

	cats := f.Catalogs()
	require.Len(t, cats, 3, "sources that are not catalogs are omitted")
	assert.Same(t, local, cats[0], "localPlugin is unwrapped to its LocalCatalog")
	assert.Same(t, mock, cats[1])
	assert.Equal(t, "wrapped", cats[2].Name(), "nested decorators are unwrapped")
}

func TestPluginSourceChain_BestEffortAndStrictModes(t *testing.T) {
	t.Parallel()

	for opName, op := range chainOps {
		t.Run(opName, func(t *testing.T) {
			t.Parallel()

			t.Run("best-effort warns and falls through on unreachable remote", func(t *testing.T) {
				t.Parallel()
				var warnings []string
				private := &fakePluginSource{
					name: "corp",
					err:  &UnreachableError{Catalog: "corp", Cause: errors.New("dial tcp 10.0.0.1:443: i/o timeout")},
				}
				official := okSource("official")

				chain := newPluginSourceChainWithMode(logr.Discard(), ChainModeBestEffort, func(msg string) {
					warnings = append(warnings, msg)
				}, nil, private, official)

				got, err := op(chain)
				require.NoError(t, err)
				assert.Equal(t, "official", got)
				assert.Equal(t, 1, private.calls)
				assert.Equal(t, 1, official.calls)
				require.Equal(t, []string{`catalog "corp" unavailable, trying next catalog...`}, warnings)

				// Subsequent lookup against the same chain deduplicates the warning.
				got2, err2 := op(chain)
				require.NoError(t, err2)
				assert.Equal(t, "official", got2)
				assert.Len(t, warnings, 1, "warning must be deduplicated per catalog")
			})

			t.Run("best-effort warns and falls through on credential rejection", func(t *testing.T) {
				t.Parallel()
				var warnings []string
				private := &fakePluginSource{
					name: "corp",
					err:  errCredentialsRejected,
				}
				official := okSource("official")

				chain := newPluginSourceChainWithMode(logr.Discard(), ChainModeBestEffort, func(msg string) {
					warnings = append(warnings, msg)
				}, nil, private, official)

				got, err := op(chain)
				require.NoError(t, err)
				assert.Equal(t, "official", got)
				require.Equal(t, []string{`catalog "corp" unauthenticated, trying next catalog...`}, warnings)
			})

			t.Run("best-effort surfaces unreachable cause when all subsequent catalogs report not-found", func(t *testing.T) {
				t.Parallel()
				var warnings []string
				private := &fakePluginSource{
					name: "corp",
					err:  &UnreachableError{Catalog: "corp", Cause: errors.New("no route to host")},
				}
				official := notFoundSource("official")

				chain := newPluginSourceChainWithMode(logr.Discard(), ChainModeBestEffort, func(msg string) {
					warnings = append(warnings, msg)
				}, nil, private, official)

				_, err := op(chain)
				require.Error(t, err)
				_, unreachable := IsCatalogUnreachable(err)
				assert.True(t, unreachable, "expected unreachable cause preserved, got %v", err)
				assert.NotErrorIs(t, err, ErrArtifactNotFound)
				assert.Equal(t, 1, official.calls, "official must still have been checked")
				assert.Len(t, warnings, 1)
			})

			t.Run("best-effort surfaces auth failure when all subsequent catalogs report not-found", func(t *testing.T) {
				t.Parallel()
				private := &fakePluginSource{
					name: "corp",
					err:  errCredentialsRejected,
				}
				official := notFoundSource("official")

				chain := newPluginSourceChainWithMode(logr.Discard(), ChainModeBestEffort, nil, nil, private, official)

				_, err := op(chain)
				require.Error(t, err)
				assert.ErrorIs(t, err, errCredentialsRejected)
				assert.NotErrorIs(t, err, ErrArtifactNotFound)
				assert.Equal(t, 1, official.calls)
			})

			t.Run("best-effort still halts on corrupt local build", func(t *testing.T) {
				t.Parallel()
				local := newTestLocalCatalog(t)
				storeCorruptPlugin(t, local, "p", "1.0.0")
				official := okSource("official")

				chain := newPluginSourceChainWithMode(logr.Discard(), ChainModeBestEffort, nil, newLocalPlugin(local), official)
				_, err := op(chain)
				require.Error(t, err)
				assert.Zero(t, official.calls, "corrupt local build must halt even in best-effort mode")
			})

			t.Run("best-effort still halts on wrapped corrupt local build", func(t *testing.T) {
				t.Parallel()
				local := newTestLocalCatalog(t)
				storeCorruptPlugin(t, local, "corrupt-plugin", "2.0.0")
				wrappedLocal := newAllowlistPluginSource(newLocalPlugin(local), []string{"corrupt-plugin"})
				official := okSource("official")

				chain := newPluginSourceChainWithMode(logr.Discard(), ChainModeBestEffort, nil, nil, wrappedLocal, official)
				_, err := chainOpsFor("corrupt-plugin", "2.0.0")[opName](chain)
				require.Error(t, err)
				assert.Zero(t, official.calls, "wrapped corrupt local build must halt even in best-effort mode")
			})

			t.Run("best-effort still halts on platform not found", func(t *testing.T) {
				t.Parallel()
				first := &fakePluginSource{name: "corp", err: &PlatformNotFoundError{Platform: "linux/amd64"}}
				official := okSource("official")

				chain := newPluginSourceChainWithMode(logr.Discard(), ChainModeBestEffort, nil, nil, first, official)
				_, err := op(chain)
				assert.True(t, IsPlatformNotFound(err))
				assert.Zero(t, official.calls)
			})

			t.Run("best-effort halts on ambiguous artifact without warning", func(t *testing.T) {
				t.Parallel()
				var warnings []string
				first := &fakePluginSource{name: "corp", err: fmt.Errorf("conflict: %w", ErrAmbiguousArtifact)}
				official := okSource("official")

				chain := newPluginSourceChainWithMode(logr.Discard(), ChainModeBestEffort, func(msg string) {
					warnings = append(warnings, msg)
				}, nil, first, official)
				_, err := op(chain)
				require.ErrorIs(t, err, ErrAmbiguousArtifact)
				assert.Zero(t, official.calls)
				assert.Empty(t, warnings)
			})
		})
	}

	t.Run("best-effort halts when caller context is canceled", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		var warnings []string
		first := &fakePluginSource{name: "corp", err: context.Canceled}
		official := okSource("official")
		chain := newPluginSourceChainWithMode(logr.Discard(), ChainModeBestEffort, func(msg string) {
			warnings = append(warnings, msg)
		}, nil, first, official)

		_, err := chain.Find(ctx, ArtifactQuery{Name: "p", Kind: ArtifactKindProvider})
		require.ErrorIs(t, err, context.Canceled)
		assert.Zero(t, official.calls)
		assert.Empty(t, warnings)
	})

	t.Run("concurrent fallback warnings across distinct catalogs serialize onWarn safely", func(t *testing.T) {
		t.Parallel()
		var warnings []string
		c1 := &fakePluginSource{name: "corp-1", err: &UnreachableError{Catalog: "corp-1", Cause: errors.New("down")}}
		c2 := &fakePluginSource{name: "corp-2", err: errCredentialsRejected}
		c3 := &fakePluginSource{name: "corp-3", err: &UnreachableError{Catalog: "corp-3", Cause: errors.New("down")}}
		official := okSource("official")

		// Unsynchronized slice append in onWarn will trigger -race if emitMu does not serialize callbacks.
		chain := newPluginSourceChainWithMode(logr.Discard(), ChainModeBestEffort, func(msg string) {
			warnings = append(warnings, msg)
		}, nil, c1, c2, c3, official)

		var wg sync.WaitGroup
		for i := range 8 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				_, findErr := chain.Find(context.Background(), ArtifactQuery{
					Name: fmt.Sprintf("plugin-%d", idx),
					Kind: ArtifactKindProvider,
				})
				assert.NoError(t, findErr)
			}(i)
		}
		wg.Wait()
		assert.Len(t, warnings, 3, "each failing catalog must emit exactly one warning")
	})
}

func TestPluginFetcher_StrictVsBestEffortAndWithCatalog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	newBrokenCorp := func() *fakePluginSource {
		return &fakePluginSource{
			name: "corp",
			err:  errCredentialsRejected,
		}
	}
	newOfficial := func(pluginName string) *fakePluginSource {
		return &fakePluginSource{
			name:      "official",
			info:      ArtifactInfo{Catalog: "official", Reference: testRef(pluginName, "1.2.0")},
			platforms: []string{"linux/amd64"},
		}
	}

	// Unpinned lookup resolves brand-new plugin name from official despite 401 on corp.
	corp := newBrokenCorp()
	official := newOfficial("brand-new-official-plugin")
	bestEffortFetcher, err := NewPluginFetcher(logr.Discard(), nil, corp, official)
	require.NoError(t, err)

	info, err := bestEffortFetcher.ResolvePlugin(ctx, "brand-new-official-plugin", ArtifactKindProvider, "")
	require.NoError(t, err)
	assert.Equal(t, "official", info.Catalog)

	// Explicit WithCatalog("corp") on the SAME fetcher fails closed with zero fallback.
	_, err = bestEffortFetcher.ResolvePlugin(ctx, "brand-new-official-plugin", ArtifactKindProvider, "", WithCatalog("corp"))
	require.ErrorIs(t, err, errCredentialsRejected)

	// Strict fetcher (--strict / serve mode) fails closed on unpinned lookup too.
	strictCorp := newBrokenCorp()
	strictOfficial := newOfficial("brand-new-official-plugin")
	strictFetcher, err := NewPluginFetcherWithOptions(
		logr.Discard(),
		nil,
		[]PluginFetcherOption{WithFetcherChainMode(ChainModeStrict)},
		strictCorp,
		strictOfficial,
	)
	require.NoError(t, err)

	_, err = strictFetcher.ResolvePlugin(ctx, "brand-new-official-plugin", ArtifactKindProvider, "")
	require.ErrorIs(t, err, errCredentialsRejected)
	assert.Zero(t, strictOfficial.calls, "strict mode must not fall through to official")
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
	require.ErrorIs(t, err, ErrCatalogNotConfigured)
	_, err = f.ResolvePlugin(ctx, "p", ArtifactKindProvider, "", scope)
	require.ErrorContains(t, err, `catalog "nope" not configured`)
	_, _, err = f.FetchPlugin(ctx, "p", ArtifactKindProvider, "1.0.0", "linux/amd64", scope)
	require.ErrorContains(t, err, `catalog "nope" not configured`)
	_, err = f.ListPlatforms(ctx, "p", ArtifactKindProvider, "1.0.0", scope)
	require.ErrorContains(t, err, `catalog "nope" not configured`)
	require.ErrorIs(t, err, ErrCatalogNotConfigured)
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
	_, err = f.ListPlatforms(ctx, "p", ArtifactKindProvider, "not a version")
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

	t.Run("Find with remote selector skips local builds", func(t *testing.T) {
		t.Parallel()
		query := ArtifactQuery{
			Name:   cachedPlatformName,
			Kind:   ArtifactKindProvider,
			Source: SourceSelector{Kind: SourceRemote, Remote: fx.remote.canonicalID()},
		}
		info, err := fx.fetcher.Find(ctx, query)
		require.NoError(t, err)
		assert.Equal(t, fx.remote.Name(), info.Catalog)
		require.NotNil(t, info.Reference.Version)
		assert.Equal(t, "3.0.0", info.Reference.Version.String())

		_, err = fx.fetcher.Find(ctx, query, WithCatalog(fx.local.Name()))
		require.ErrorIs(t, err, ErrArtifactNotFound)
	})
}

func TestLocalPlugin_RemoteRefIsNotFound(t *testing.T) {
	t.Parallel()
	fx := newPluginV2Fixture(t)
	ctx := t.Context()
	lp := newLocalPlugin(fx.local)

	ref := testRef(cachedPlatformName, "1.0.0")
	_, _, err := lp.FetchByPlatform(ctx, ref, "linux/amd64")
	require.NoError(t, err, "a ref without a remote is served from the build")

	ref.Remote = fx.remote.canonicalID()
	_, _, err = lp.FetchByPlatform(ctx, ref, "linux/amd64")
	require.ErrorIs(t, err, ErrArtifactNotFound)
	_, err = lp.ListPlatforms(ctx, ref)
	require.ErrorIs(t, err, ErrArtifactNotFound)
	_, err = lp.ResolveContentDigest(ctx, ref, "linux/amd64", MediaTypeProviderBinary)
	require.ErrorIs(t, err, ErrArtifactNotFound)
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

func TestPluginFetcher_ListPlatforms(t *testing.T) {
	t.Parallel()
	fx := newPluginV2Fixture(t)
	ctx := t.Context()

	platforms, err := fx.fetcher.ListPlatforms(ctx, cachedPlatformName, ArtifactKindProvider, "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, []string{"linux/amd64"}, platforms)

	platforms, err = fx.fetcher.ListPlatforms(ctx, cachedPlatformName, ArtifactKindProvider, "2.0.0", WithCatalog(fx.remote.Name()))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"linux/amd64", "darwin/arm64"}, platforms)

	_, err = fx.fetcher.ListPlatforms(ctx, cachedPlatformName, ArtifactKindProvider, "2.0.0", WithCatalog(fx.local.Name()))
	require.ErrorIs(t, err, ErrArtifactNotFound, "pulled copy is not served as a local build")

	_, err = fx.fetcher.ListPlatforms(ctx, cachedPlatformName, ArtifactKindProvider, "9.9.9")
	require.ErrorIs(t, err, ErrArtifactNotFound)
}

func BenchmarkPluginFetcher_ListPlatforms(b *testing.B) {
	f, err := NewPluginFetcher(logr.Discard(), nil, okSource("a"))
	require.NoError(b, err)
	ctx := b.Context()
	scope := WithCatalog("a")

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := f.ListPlatforms(ctx, "p", ArtifactKindProvider, "1.0.0", scope); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPluginSourceChain_BestEffortFallback(b *testing.B) {
	unreach := &fakePluginSource{
		name: "corp",
		err:  &UnreachableError{Catalog: "corp", Cause: errors.New("unreachable")},
	}
	official := okSource("official")
	chain := newPluginSourceChainWithMode(logr.Discard(), ChainModeBestEffort, func(_ string) {}, nil, unreach, official)
	ctx := b.Context()
	query := ArtifactQuery{Name: "p", Kind: ArtifactKindProvider}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := chain.Find(ctx, query); err != nil {
			b.Fatal(err)
		}
	}
}
