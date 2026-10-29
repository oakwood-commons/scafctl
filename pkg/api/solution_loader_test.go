// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/adrg/xdg"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const loaderTestSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: api-loader-test
  version: 1.0.0
spec:
  resolvers: {}
`

// loaderTestInvalidYAML parses but fails strict spec validation.
const loaderTestInvalidYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: api-loader-lenient
  version: 1.0.0
spec:
  resolvers:
    app:
      dependsOn: [doesNotExist]
      resolve:
        with:
          - provider: static
            inputs:
              value: x
`

// loaderTestDir returns a temp dir holding a solution at each relative path.
func loaderTestDir(t *testing.T, paths ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, p := range paths {
		full := filepath.Join(dir, p)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(loaderTestSolutionYAML), 0o600))
	}
	return dir
}

// loaderTestServer serves body and returns its solution URL.
func loaderTestServer(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/solution.yaml"
}

// loopbackContext allows the URL loader to reach an httptest server.
func loopbackContext(ctx context.Context) context.Context {
	return config.WithConfig(ctx, &config.Config{
		HTTPClient: config.HTTPClientConfig{
			AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
		},
	})
}

func TestNewSolutionLoaderFactory_RejectsLocalSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  func(ctx context.Context) context.Context
		opts []load.RouterOption
		c    source.Classification
	}{
		{
			name: "local file",
			c:    source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"},
		},
		{
			name: "absolute local file",
			c:    source.Classification{Kind: source.KindLocalFile, Ref: "/etc/passwd"},
		},
		{
			name: "auto-discovery",
			c:    source.Classification{Kind: source.KindAutoDiscover},
		},
		{
			name: "caller options cannot re-enable local files",
			opts: []load.RouterOption{load.WithLenient(), load.WithFileOptions(load.WithLenient())},
			c:    source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"},
		},
		{
			name: "embedder binary name does not enable discovery",
			ctx: func(ctx context.Context) context.Context {
				return settings.IntoContext(ctx, &settings.Run{BinaryName: "mycli"})
			},
			c: source.Classification{Kind: source.KindAutoDiscover},
		},
		{
			name: "stdin is not enabled",
			c:    source.Classification{Kind: source.KindStdin, Ref: "-"},
		},
		{
			name: "caller options cannot enable stdin",
			opts: []load.RouterOption{load.WithStdin(strings.NewReader(loaderTestSolutionYAML))},
			c:    source.Classification{Kind: source.KindStdin, Ref: "-"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := loaderTestDir(t, "solution.yaml", "mycli/solution.yaml")
			ctx := provider.WithWorkingDirectory(t.Context(), dir)
			if tt.ctx != nil {
				ctx = tt.ctx(ctx)
			}

			got, err := NewSolutionLoaderFactory(nil)(ctx, tt.opts...).Load(ctx, tt.c)
			require.ErrorIs(t, err, load.ErrSourceNotEnabled)
			assert.Nil(t, got)
		})
	}
}

func TestNewSolutionLoaderFactory_URL(t *testing.T) {
	t.Parallel()

	t.Run("uses the secure default policy without app config", func(t *testing.T) {
		t.Parallel()
		url := loaderTestServer(t, loaderTestSolutionYAML)

		_, err := NewSolutionLoaderFactory(nil)(t.Context()).
			Load(t.Context(), source.Classification{Kind: source.KindURL, Ref: url})
		require.Error(t, err)
		assert.NotErrorIs(t, err, load.ErrSourceNotEnabled)
	})

	t.Run("honors the app config HTTP policy", func(t *testing.T) {
		t.Parallel()
		url := loaderTestServer(t, loaderTestSolutionYAML)
		ctx := loopbackContext(t.Context())

		got, err := NewSolutionLoaderFactory(nil)(ctx).
			Load(ctx, source.Classification{Kind: source.KindURL, Ref: url})
		require.NoError(t, err)
		assert.Equal(t, "api-loader-test", got.Solution.Metadata.Name)
	})

	t.Run("is strict by default", func(t *testing.T) {
		t.Parallel()
		url := loaderTestServer(t, loaderTestInvalidYAML)
		ctx := loopbackContext(t.Context())

		_, err := NewSolutionLoaderFactory(nil)(ctx).
			Load(ctx, source.Classification{Kind: source.KindURL, Ref: url})
		require.Error(t, err)
	})

	t.Run("caller options extend the defaults", func(t *testing.T) {
		t.Parallel()
		url := loaderTestServer(t, loaderTestInvalidYAML)
		ctx := loopbackContext(t.Context())

		got, err := NewSolutionLoaderFactory(nil)(ctx, load.WithLenient()).
			Load(ctx, source.Classification{Kind: source.KindURL, Ref: url})
		require.NoError(t, err)
		assert.Equal(t, "api-loader-lenient", got.Solution.Metadata.Name)
	})
}

func TestNewSolutionLoaderFactory_Catalog(t *testing.T) {
	t.Parallel()

	ctxIndex := catalogindex.FromConfig(&config.Config{
		Catalogs: []config.CatalogConfig{{Name: "corp", URL: "oci://reg.example.com/team"}},
	})
	// An unknown alias fails before any fetch, so no catalog is opened.
	qualified := source.Classification{Kind: source.KindCatalogQualified, Ref: "app", Catalog: "corp"}

	t.Run("nil index uses the request context index", func(t *testing.T) {
		t.Parallel()
		_, err := NewSolutionLoaderFactory(nil)(t.Context()).
			Load(t.Context(), source.Classification{Kind: source.KindCatalogQualified, Ref: "app", Catalog: "no-such-alias"})
		require.ErrorIs(t, err, catalogindex.ErrUnknownCatalog)
		assert.NotErrorIs(t, err, load.ErrSourceNotEnabled)
	})

	t.Run("given index overrides the request context index", func(t *testing.T) {
		t.Parallel()
		ctx := catalogindex.WithIndex(t.Context(), ctxIndex)
		idx := catalogindex.FromConfig(&config.Config{})

		_, err := NewSolutionLoaderFactory(idx)(ctx).Load(ctx, qualified)
		require.ErrorIs(t, err, catalogindex.ErrUnknownCatalog)
	})

	t.Run("caller catalog option overrides the given index", func(t *testing.T) {
		t.Parallel()
		fetcher := &recordingFetcher{content: loaderTestSolutionYAML}

		got, err := NewSolutionLoaderFactory(catalogindex.FromConfig(&config.Config{}))(
			t.Context(), load.WithCatalog(ctxIndex, fetcher),
		).Load(t.Context(), qualified)
		require.NoError(t, err)
		assert.Equal(t, "api-loader-test", got.Solution.Metadata.Name)
		require.Len(t, fetcher.refs, 1)
		assert.Equal(t, "app", fetcher.refs[0].Name)
		assert.NotEmpty(t, fetcher.refs[0].Remote)
	})
}

// The API must not build remotes for registries the server has not configured:
// a request naming one would otherwise make the server dial an arbitrary host
// with its own credentials. Not parallel: it points the local catalog at a
// temp dir through XDG_DATA_HOME.
func TestNewSolutionLoaderFactory_RejectsUnconfiguredRegistryWithoutDialing(t *testing.T) {
	t.Cleanup(xdg.Reload) // registered first so it runs after the env is restored
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	xdg.Reload()

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

	for name, idx := range map[string]*catalogindex.Index{
		"nil index":   nil,
		"given index": catalogindex.FromConfig(&config.Config{}),
	} {
		for _, ref := range []string{host + "/org/sol@1.0.0", host + "/org/sol"} {
			t.Run(name+"/"+ref, func(t *testing.T) {
				ctx := loopbackContext(t.Context())
				got, err := NewSolutionLoaderFactory(idx)(ctx).
					Load(ctx, source.Classification{Kind: source.KindRemoteRef, Ref: ref})
				require.Error(t, err)
				assert.Contains(t, err.Error(), "no catalog configured")
				assert.Nil(t, got)
			})
		}
	}
	assert.Zero(t, conns.Load(), "an unconfigured registry must not be contacted")
}

// recordingFetcher serves content for every reference and records each one.
type recordingFetcher struct {
	content string
	refs    []catalog.Reference
}

func (f *recordingFetcher) FetchSolution(_ context.Context, ref catalog.Reference, _ ...catalog.FetchOption) (*catalog.SolutionParts, error) {
	f.refs = append(f.refs, ref)
	return &catalog.SolutionParts{Content: []byte(f.content)}, nil
}

func TestNewSolutionLoaderFactory_NewLoaderPerCall(t *testing.T) {
	t.Parallel()
	factory := NewSolutionLoaderFactory(nil)
	assert.NotSame(t, factory(t.Context()), factory(t.Context()))
}

// stubLoader is a load.Loader that returns a fixed result.
type stubLoader struct{ ms *load.MaterializedSolution }

func (s stubLoader) Load(context.Context, source.Classification) (*load.MaterializedSolution, error) {
	return s.ms, nil
}

func TestHandlerContext_SolutionLoader(t *testing.T) {
	t.Parallel()

	t.Run("nil factory defaults to remote-only loader", func(t *testing.T) {
		t.Parallel()
		hctx := &HandlerContext{}
		dir := loaderTestDir(t, "solution.yaml")
		ctx := provider.WithWorkingDirectory(t.Context(), dir)

		_, err := hctx.SolutionLoader(ctx).
			Load(ctx, source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"})
		require.ErrorIs(t, err, load.ErrSourceNotEnabled)
	})

	t.Run("nil factory uses the handler catalog index", func(t *testing.T) {
		t.Parallel()
		hctx := &HandlerContext{CatalogIndex: catalogindex.FromConfig(&config.Config{})}
		// The request context knows "corp"; the handler index does not.
		ctx := catalogindex.WithIndex(t.Context(), catalogindex.FromConfig(&config.Config{
			Catalogs: []config.CatalogConfig{{Name: "corp", URL: "oci://reg.example.com/team"}},
		}))

		_, err := hctx.SolutionLoader(ctx).
			Load(ctx, source.Classification{Kind: source.KindCatalogQualified, Ref: "app", Catalog: "corp"})
		require.ErrorIs(t, err, catalogindex.ErrUnknownCatalog)
	})

	t.Run("embedder factory is used with caller options", func(t *testing.T) {
		t.Parallel()
		want := &load.MaterializedSolution{}
		var gotOpts []load.RouterOption
		hctx := &HandlerContext{
			NewSolutionLoader: func(_ context.Context, opts ...load.RouterOption) load.Loader {
				gotOpts = opts
				return stubLoader{ms: want}
			},
		}

		got, err := hctx.SolutionLoader(t.Context(), load.WithLenient()).
			Load(t.Context(), source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"})
		require.NoError(t, err)
		assert.Same(t, want, got)
		assert.Len(t, gotOpts, 1)
	})
}

func BenchmarkNewSolutionLoaderFactory(b *testing.B) {
	factory := NewSolutionLoaderFactory(catalogindex.FromConfig(&config.Config{}))
	ctx := b.Context()
	for b.Loop() {
		_ = factory(ctx)
	}
}
