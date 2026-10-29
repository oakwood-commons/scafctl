// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

// countingBuild returns a build func that counts its calls and yields f or err.
func countingBuild(calls *atomic.Int32, f SolutionFetcher, err error) func() (SolutionFetcher, error) {
	return func() (SolutionFetcher, error) {
		calls.Add(1)
		return f, err
	}
}

func TestLazyFetcher_BuildsOnlyWhenACatalogRefIsLoaded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"solution.yaml": validSolutionYAML})
	ctx := provider.WithWorkingDirectory(t.Context(), dir)

	var calls atomic.Int32
	var fetched []catalog.Reference
	lazy := newLazyFetcher(countingBuild(&calls, staticFetcher(&fetched), nil))
	r := NewRouter("scafctl", WithCatalog(testCatalogIndex(), lazy))

	_, err := r.Load(ctx, source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"})
	require.NoError(t, err)
	_, err = r.Load(ctx, source.Classification{Kind: source.KindAutoDiscover})
	require.NoError(t, err)
	assert.Zero(t, calls.Load(), "non-catalog loads must not build the fetcher")

	for range 2 {
		_, err = r.Load(ctx, source.Classification{Kind: source.KindBareName, Ref: "app@1.0.0"})
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), calls.Load())
	assert.Len(t, fetched, 2)
}

func TestLazyFetcher_ConcurrentFetchesBuildOnce(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	parts := &catalog.SolutionParts{Content: []byte(validSolutionYAML)}
	inner := fetchFunc(func(context.Context, catalog.Reference) (*catalog.SolutionParts, error) {
		return parts, nil
	})
	lazy := newLazyFetcher(countingBuild(&calls, inner, nil))

	const workers = 8
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			got, err := lazy.FetchSolution(t.Context(), catalog.Reference{Name: "app"})
			assert.NoError(t, err)
			assert.Same(t, parts, got)
		})
	}
	wg.Wait()
	assert.Equal(t, int32(1), calls.Load())
}

func TestLazyFetcher_BuildErrorIsWrappedAndCached(t *testing.T) {
	t.Parallel()

	errBuild := errors.New("local catalog unavailable")
	var calls atomic.Int32
	lazy := newLazyFetcher(countingBuild(&calls, nil, errBuild))

	for range 2 {
		got, err := lazy.FetchSolution(t.Context(), catalog.Reference{Name: "app"})
		require.ErrorIs(t, err, errBuild)
		assert.Contains(t, err.Error(), "catalog not available")
		assert.Nil(t, got)
	}
	assert.Equal(t, int32(1), calls.Load())
}

func TestSolutionFetcherFromContext_IsLazy(t *testing.T) {
	t.Parallel()

	for name, opts := range map[string][]FetcherOption{
		"default":           nil,
		"on-demand remotes": {WithOnDemandRemotes()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := SolutionFetcherFromContext(t.Context(), logr.Discard(), opts...)
			require.NotNil(t, f)
			lazy, ok := f.(*lazyFetcher)
			require.True(t, ok)
			assert.NotNil(t, lazy.build)
		})
	}
}

func TestWithOnDemandRemotes(t *testing.T) {
	t.Parallel()

	var cfg fetcherConfig
	assert.False(t, cfg.onDemandRemotes)
	WithOnDemandRemotes()(&cfg)
	assert.True(t, cfg.onDemandRemotes)
}

func TestNewSolutionFetcher(t *testing.T) {
	t.Parallel()

	for name, cfg := range map[string]*config.Config{
		"no config": nil,
		"configured oci remote": {Catalogs: []config.CatalogConfig{
			{Name: "corp", Type: config.CatalogTypeOCI, URL: "oci://reg.example.com/team"},
		}},
	} {
		t.Run("fetches from the local catalog/"+name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if cfg != nil {
				ctx = config.WithConfig(ctx, cfg)
			}
			f, err := newSolutionFetcher(ctx, logr.Discard(), t.TempDir(), fetcherConfig{})
			require.NoError(t, err)

			got, err := NewCatalogLoader(nil, f).Load(ctx,
				source.Classification{Kind: source.KindCatalogQualified, Ref: "app@1.0.0", Catalog: config.CatalogNameLocal})
			require.ErrorIs(t, err, catalog.ErrArtifactNotFound)
			assert.Nil(t, got)
		})
	}

	t.Run("unconfigured remote is rejected without dialing by default", func(t *testing.T) {
		t.Parallel()
		var conns atomic.Int32
		srv := httptest.NewUnstartedServer(http.NotFoundHandler())
		srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
			if state == http.StateNew {
				conns.Add(1)
			}
		}
		srv.Start()
		t.Cleanup(srv.Close)
		host := srv.Listener.Addr().String()

		f, err := newSolutionFetcher(t.Context(), logr.Discard(), t.TempDir(), fetcherConfig{})
		require.NoError(t, err)

		for _, ref := range []catalog.Reference{
			{Name: "sol", Version: semver.MustParse("1.0.0"), Remote: host + "/org"},
			{Name: "sol", Remote: host + "/org"},
		} {
			got, err := f.FetchSolution(t.Context(), ref)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no catalog configured")
			assert.Nil(t, got)
		}
		assert.Zero(t, conns.Load(), "an unconfigured registry must not be contacted")
	})

	t.Run("unconfigured remote goes through the remote factory when enabled", func(t *testing.T) {
		t.Parallel()
		f, err := newSolutionFetcher(t.Context(), logr.Discard(), t.TempDir(), fetcherConfig{onDemandRemotes: true})
		require.NoError(t, err)

		// An origin with no registry makes the factory fail before any network
		// access, proving it was consulted instead of the "no catalog
		// configured" error a fetcher without a factory returns.
		got, err := f.FetchSolution(t.Context(), catalog.Reference{Name: "app", Remote: "/team"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no registry")
		assert.NotContains(t, err.Error(), "no catalog configured")
		assert.Nil(t, got)
	})

	t.Run("local catalog open failure", func(t *testing.T) {
		t.Parallel()
		blocker := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(blocker, nil, 0o600))

		f, err := newSolutionFetcher(t.Context(), logr.Discard(), filepath.Join(blocker, "catalog"), fetcherConfig{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "opening local catalog")
		assert.True(t, f == nil, "error must not return a typed-nil fetcher")
	})
}

func TestDefaultRemoteID(t *testing.T) {
	t.Parallel()

	catalogs := []config.CatalogConfig{
		{Name: "corp", URL: "oci://reg.example.com/team"},
		{Name: "files", Path: "/tmp/catalog"},
	}

	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{name: "no config"},
		{name: "unset", cfg: &config.Config{Catalogs: catalogs}},
		{
			name: "local",
			cfg:  &config.Config{Catalogs: catalogs, Settings: config.Settings{DefaultCatalog: config.CatalogNameLocal}},
		},
		{
			name: "remote alias",
			cfg:  &config.Config{Catalogs: catalogs, Settings: config.Settings{DefaultCatalog: "Corp"}},
			want: "reg.example.com/team",
		},
		{
			name: "filesystem alias",
			cfg:  &config.Config{Catalogs: catalogs, Settings: config.Settings{DefaultCatalog: "files"}},
		},
		{
			name: "unknown alias",
			cfg:  &config.Config{Catalogs: catalogs, Settings: config.Settings{DefaultCatalog: "nope"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if tt.cfg != nil {
				ctx = config.WithConfig(ctx, tt.cfg)
			}
			assert.Equal(t, tt.want, defaultRemoteID(ctx))
		})
	}
}

func BenchmarkSolutionFetcherFromContext(b *testing.B) {
	ctx := b.Context()
	for b.Loop() {
		_ = SolutionFetcherFromContext(ctx, logr.Discard())
	}
}
