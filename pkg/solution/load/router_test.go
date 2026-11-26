// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

// staticFetcher returns a fetcher that serves validSolutionYAML and records
// each ref.
func staticFetcher(fetched *[]catalog.Reference) fetchFunc {
	return func(_ context.Context, ref catalog.Reference) (*catalog.SolutionParts, error) {
		*fetched = append(*fetched, ref)
		return &catalog.SolutionParts{Content: []byte(validSolutionYAML)}, nil
	}
}

func TestRouter_Load_Dispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		c           func(dir, srvURL string) source.Classification
		wantFetched bool
	}{
		{
			name: "local file",
			c: func(string, string) source.Classification {
				return source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"}
			},
		},
		{
			name: "auto-discover",
			c: func(string, string) source.Classification {
				return source.Classification{Kind: source.KindAutoDiscover}
			},
		},
		{
			name: "stdin",
			c: func(string, string) source.Classification {
				return source.Classification{Kind: source.KindStdin, Ref: "-"}
			},
		},
		{
			name: "url",
			c: func(_, srvURL string) source.Classification {
				return source.Classification{Kind: source.KindURL, Ref: srvURL}
			},
		},
		{
			name: "remote ref",
			c: func(string, string) source.Classification {
				return source.Classification{Kind: source.KindRemoteRef, Ref: "ghcr.io/org/app@1.0.0"}
			},
			wantFetched: true,
		},
		{
			name: "catalog qualified",
			c: func(string, string) source.Classification {
				return source.Classification{Kind: source.KindCatalogQualified, Ref: "app@1.0.0", Catalog: "official"}
			},
			wantFetched: true,
		},
		{
			name: "bare name",
			c: func(string, string) source.Classification {
				return source.Classification{Kind: source.KindBareName, Ref: "app@1.0.0"}
			},
			wantFetched: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{"solution.yaml": validSolutionYAML})
			ctx := provider.WithWorkingDirectory(t.Context(), dir)
			srv := serve(t, http.StatusOK, validSolutionYAML)

			var fetched []catalog.Reference
			r := NewRouter("scafctl",
				WithStdin(strings.NewReader(validSolutionYAML)),
				WithURL(newTestClient(t, true)),
				WithCatalog(testCatalogIndex(), staticFetcher(&fetched)),
			)

			got, err := r.Load(ctx, tt.c(dir, srv.URL))
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, "test-solution", got.Solution.Metadata.Name)
			if tt.wantFetched {
				require.Len(t, fetched, 1)
				assert.Equal(t, "app", fetched[0].Name)
			} else {
				assert.Empty(t, fetched)
			}
		})
	}
}

func TestRouter_Load_NotEnabled(t *testing.T) {
	t.Parallel()

	routers := map[string]*Router{
		"no options":  NewRouter("scafctl"),
		"nil inputs":  NewRouter("scafctl", WithStdin(nil), WithCatalog(testCatalogIndex(), nil)),
		"file option": NewRouter("scafctl", WithFileOptions(WithLenient())),
	}
	kinds := []source.Kind{
		source.KindStdin,
		source.KindURL,
		source.KindRemoteRef,
		source.KindCatalogQualified,
		source.KindBareName,
	}

	for name, r := range routers {
		for _, kind := range kinds {
			t.Run(name+"/"+kind.String(), func(t *testing.T) {
				t.Parallel()
				got, err := r.Load(t.Context(), source.Classification{Kind: kind, Ref: "ref"})
				require.ErrorIs(t, err, ErrSourceNotEnabled)
				assert.NotErrorIs(t, err, ErrUnsupportedKind)
				assert.Contains(t, err.Error(), kind.String())
				assert.Contains(t, err.Error(), `"ref"`)
				assert.Nil(t, got)
			})
		}
	}
}

func TestRouter_Load_WithoutLocal(t *testing.T) {
	t.Parallel()

	newRouter := func(t *testing.T, fetched *[]catalog.Reference, opts ...RouterOption) *Router {
		t.Helper()
		return NewRouter("scafctl", append([]RouterOption{
			WithURL(newTestClient(t, true)),
			WithCatalog(testCatalogIndex(), staticFetcher(fetched)),
		}, opts...)...)
	}

	for _, c := range []source.Classification{
		{Kind: source.KindLocalFile, Ref: "solution.yaml"},
		{Kind: source.KindAutoDiscover},
		{Kind: source.KindStdin, Ref: "-"},
	} {
		t.Run(c.Kind.String()+" is not enabled", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{"solution.yaml": validSolutionYAML})
			ctx := provider.WithWorkingDirectory(t.Context(), dir)

			var fetched []catalog.Reference
			got, err := newRouter(t, &fetched, WithStdin(strings.NewReader(validSolutionYAML)), WithoutLocal()).Load(ctx, c)
			require.ErrorIs(t, err, ErrSourceNotEnabled)
			assert.Contains(t, err.Error(), c.Kind.String())
			assert.Nil(t, got)
			assert.Empty(t, fetched)
		})
	}

	t.Run("holds when given before other options", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{"solution.yaml": validSolutionYAML})
		ctx := provider.WithWorkingDirectory(t.Context(), dir)

		r := NewRouter("scafctl", WithoutLocal(), WithFileOptions(WithLenient()), WithLenient(),
			WithStdin(strings.NewReader(validSolutionYAML)))
		_, err := r.Load(ctx, source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"})
		require.ErrorIs(t, err, ErrSourceNotEnabled)
		_, err = r.Load(ctx, source.Classification{Kind: source.KindStdin, Ref: "-"})
		require.ErrorIs(t, err, ErrSourceNotEnabled)
	})

	t.Run("remote sources still load", func(t *testing.T) {
		t.Parallel()
		srv := serve(t, http.StatusOK, validSolutionYAML)

		var fetched []catalog.Reference
		r := newRouter(t, &fetched, WithoutLocal())
		for _, c := range []source.Classification{
			{Kind: source.KindURL, Ref: srv.URL},
			{Kind: source.KindRemoteRef, Ref: "ghcr.io/org/app@1.0.0"},
			{Kind: source.KindBareName, Ref: "app@1.0.0"},
		} {
			got, err := r.Load(t.Context(), c)
			require.NoError(t, err, c.Kind)
			assert.Equal(t, "test-solution", got.Solution.Metadata.Name, c.Kind)
		}
		assert.Len(t, fetched, 2)
	})
}

func TestRouter_Load_UnknownKind(t *testing.T) {
	t.Parallel()

	r := NewRouter("scafctl", WithStdin(strings.NewReader("")), WithURL(nil))
	for _, kind := range []source.Kind{source.KindUnknown, source.Kind(99)} {
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()
			got, err := r.Load(t.Context(), source.Classification{Kind: kind, Ref: "ref"})
			require.ErrorIs(t, err, ErrUnsupportedKind)
			assert.Nil(t, got)
		})
	}
}

func TestRouter_Load_Lenient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		c    func(srvURL string) source.Classification
	}{
		{
			name: "local file",
			c: func(string) source.Classification {
				return source.Classification{Kind: source.KindLocalFile, Ref: "solution.json"}
			},
		},
		{
			name: "auto-discover",
			c: func(string) source.Classification {
				return source.Classification{Kind: source.KindAutoDiscover}
			},
		},
		{
			name: "stdin",
			c: func(string) source.Classification {
				return source.Classification{Kind: source.KindStdin, Ref: "-"}
			},
		},
		{
			name: "url",
			c: func(srvURL string) source.Classification {
				return source.Classification{Kind: source.KindURL, Ref: srvURL}
			},
		},
		{
			name: "catalog",
			c: func(string) source.Classification {
				return source.Classification{Kind: source.KindBareName, Ref: "app@1.0.0"}
			},
		},
	}

	for _, tt := range tests {
		for _, lenient := range []bool{false, true} {
			name := tt.name + "/strict"
			if lenient {
				name = tt.name + "/lenient"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				writeTree(t, dir, map[string]string{"solution.json": undefinedDepJSON})
				ctx := provider.WithWorkingDirectory(t.Context(), dir)
				srv := serve(t, http.StatusOK, undefinedDepJSON)

				opts := []RouterOption{
					WithStdin(strings.NewReader(undefinedDepJSON)),
					WithURL(newTestClient(t, true)),
					WithCatalog(nil, fetchFunc(func(context.Context, catalog.Reference) (*catalog.SolutionParts, error) {
						return &catalog.SolutionParts{Content: []byte(undefinedDepJSON)}, nil
					})),
				}
				if lenient {
					opts = append(opts, WithLenient())
				}

				got, err := NewRouter("scafctl", opts...).Load(ctx, tt.c(srv.URL))
				if !lenient {
					require.Error(t, err)
					assert.Nil(t, got)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, "lenient", got.Solution.Metadata.Name)
			})
		}
	}
}

func TestRouter_Load_NoCache(t *testing.T) {
	t.Parallel()

	const remote = cachedRemote
	c := source.Classification{Kind: source.KindRemoteRef, Ref: remote + "/app@1.0.0"}

	t.Run("default serves the cached copy", func(t *testing.T) {
		t.Parallel()
		got, err := NewRouter("scafctl", WithCatalog(nil, cachedRemoteFetcher(t))).Load(t.Context(), c)
		require.NoError(t, err)
		assert.Equal(t, "test-solution", got.Solution.Metadata.Name)
	})

	t.Run("no-cache reaches the catalog loader", func(t *testing.T) {
		t.Parallel()
		got, err := NewRouter("scafctl", WithCatalog(nil, cachedRemoteFetcher(t)), WithNoCache()).Load(t.Context(), c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `no catalog configured for remote "`+remote+`"`)
		assert.Nil(t, got)
	})

	t.Run("no-cache leaves local files alone", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{"solution.yaml": validSolutionYAML})
		ctx := provider.WithWorkingDirectory(t.Context(), dir)

		got, err := NewRouter("scafctl", WithNoCache()).
			Load(ctx, source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"})
		require.NoError(t, err)
		assert.Equal(t, "test-solution", got.Solution.Metadata.Name)
	})
}

func TestRouter_Load_FileOptionsReachDiscovery(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"solution.yaml": "not: [valid"})
	ctx := provider.WithWorkingDirectory(t.Context(), dir)
	path := filepath.Join(dir, "solution.yaml")

	r := NewRouter("scafctl", WithFileOptions(WithReadFile(readFileMap(map[string]string{path: validSolutionYAML}))))

	for _, c := range []source.Classification{
		{Kind: source.KindLocalFile, Ref: "solution.yaml"},
		{Kind: source.KindAutoDiscover},
	} {
		got, err := r.Load(ctx, c)
		require.NoError(t, err, c.Kind)
		assert.Equal(t, "test-solution", got.Solution.Metadata.Name)
		assert.Equal(t, path, got.Solution.GetPath())
	}
}

func TestRouter_Load_DiscoverOptions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"scafctl/solution.yaml": validSolutionYAML, "solution.yaml": validSolutionYAML})
	ctx := provider.WithWorkingDirectory(t.Context(), dir)

	got, err := NewRouter("scafctl", WithDiscoverOptions(WithDiscoveryRisk(DiscoveryRiskHigh))).
		Load(ctx, source.Classification{Kind: source.KindAutoDiscover})
	require.ErrorIs(t, err, ErrAmbiguousSolution)
	assert.Nil(t, got)
}

func TestRouter_Load_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"scafctl/solution.yaml": "not: [valid",
		".mycli/mycli.yaml":     validSolutionYAML,
	})
	ctx := provider.WithWorkingDirectory(t.Context(), dir)

	got, err := NewRouter("mycli").Load(ctx, source.Classification{Kind: source.KindAutoDiscover})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, ".mycli", "mycli.yaml"), got.Solution.GetPath())
	assert.Empty(t, got.OtherMatches)
}

func TestCLIRouterOptions(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		r := NewRouter("scafctl", append(CLIRouterOptions(nil, false, settings.DiscoveryModeDefault),
			WithCatalog(testCatalogIndex(), staticFetcher(new([]catalog.Reference))))...)
		assert.Nil(t, r.stdin, "nil stdin leaves stdin disabled")
		require.NotNil(t, r.catalog)
		assert.False(t, r.catalog.noCache)
		require.NotNil(t, r.discover)
		assert.Equal(t, settings.DiscoveryModeDefault, r.discover.mode)
	})

	t.Run("stdin, no-cache, and discovery mode", func(t *testing.T) {
		t.Parallel()
		r := NewRouter("scafctl", append(CLIRouterOptions(strings.NewReader(validSolutionYAML), true, settings.DiscoveryModeAction),
			WithCatalog(testCatalogIndex(), staticFetcher(new([]catalog.Reference))))...)
		require.NotNil(t, r.stdin)
		require.NotNil(t, r.catalog)
		assert.True(t, r.catalog.noCache)
		require.NotNil(t, r.discover)
		assert.Equal(t, settings.DiscoveryModeAction, r.discover.mode)

		got, err := r.Load(t.Context(), source.Classification{Kind: source.KindStdin, Ref: "-"})
		require.NoError(t, err)
		assert.Equal(t, "test-solution", got.Solution.Metadata.Name)
	})
}

func TestNewRouterFromContext(t *testing.T) {
	t.Parallel()

	t.Run("loads a local file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{"solution.yaml": validSolutionYAML})
		ctx := provider.WithWorkingDirectory(t.Context(), dir)

		got, err := NewRouterFromContext(ctx, logr.Discard()).
			Load(ctx, source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"})
		require.NoError(t, err)
		assert.Equal(t, "test-solution", got.Solution.Metadata.Name)
	})

	t.Run("URLs use the secure default policy without app config", func(t *testing.T) {
		t.Parallel()
		srv := serve(t, http.StatusOK, validSolutionYAML)

		_, err := NewRouterFromContext(t.Context(), logr.Discard()).
			Load(t.Context(), source.Classification{Kind: source.KindURL, Ref: srv.URL + "/solution.yaml"})
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrSourceNotEnabled)
	})

	t.Run("URLs honor the app config HTTP policy", func(t *testing.T) {
		t.Parallel()
		srv := serve(t, http.StatusOK, validSolutionYAML)
		disableCache := false
		ctx := config.WithConfig(t.Context(), &config.Config{
			HTTPClient: config.HTTPClientConfig{
				EnableCache:         &disableCache,
				AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
			},
		})

		got, err := NewRouterFromContext(ctx, logr.Discard()).
			Load(ctx, source.Classification{Kind: source.KindURL, Ref: srv.URL + "/solution.yaml"})
		require.NoError(t, err)
		assert.Equal(t, "test-solution", got.Solution.Metadata.Name)
	})

	t.Run("URLs reuse pooled client and TCP connection across routers", func(t *testing.T) {
		t.Parallel()
		var newConns atomic.Int32
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(validSolutionYAML))
		}))
		srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
			if state == http.StateNew {
				newConns.Add(1)
			}
		}
		srv.Start()
		t.Cleanup(srv.Close)

		disableCache := false
		ctx := config.WithConfig(t.Context(), &config.Config{
			HTTPClient: config.HTTPClientConfig{
				EnableCache:         &disableCache,
				AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
			},
		})

		r1 := NewRouterFromContext(ctx, logr.Discard())
		r2 := NewRouterFromContext(ctx, logr.Discard())
		require.NotNil(t, r1.url)
		require.NotNil(t, r2.url)
		assert.Same(t, r1.url.client, r2.url.client, "routers sharing one app config must share one HTTP client")

		for range 3 {
			got, err := NewRouterFromContext(ctx, logr.Discard()).
				Load(ctx, source.Classification{Kind: source.KindURL, Ref: srv.URL + "/solution.yaml"})
			require.NoError(t, err)
			assert.Equal(t, "test-solution", got.Solution.Metadata.Name)
		}
		assert.Equal(t, int32(1), newConns.Load(), "repeated router loads must reuse a single TCP connection")
	})

	t.Run("WithNoCache bypasses HTTP response cache on URL loads", func(t *testing.T) {
		t.Parallel()
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n := hits.Add(1)
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: sol-" + strconv.Itoa(int(n)) + "\n  version: 1.0.0\nspec:\n  resolvers: {}\n"))
		}))
		t.Cleanup(srv.Close)

		enableCache := true
		ctx := config.WithConfig(t.Context(), &config.Config{
			HTTPClient: config.HTTPClientConfig{
				EnableCache:         &enableCache,
				CacheType:           "memory",
				CacheDir:            t.TempDir(),
				AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
			},
		})

		ref := source.Classification{Kind: source.KindURL, Ref: srv.URL + "/solution.yaml"}

		first, err := NewRouterFromContext(ctx, logr.Discard()).Load(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, "sol-1", first.Solution.Metadata.Name)

		cached, err := NewRouterFromContext(ctx, logr.Discard()).Load(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, "sol-1", cached.Solution.Metadata.Name, "default app client honors response cache")

		fresh, err := NewRouterFromContext(ctx, logr.Discard(), WithNoCache()).Load(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, "sol-2", fresh.Solution.Metadata.Name, "WithNoCache must bypass HTTP response cache")
	})

	t.Run("explicit WithURL overrides context URL client", func(t *testing.T) {
		t.Parallel()
		custom := newTestClient(t, false)
		t.Cleanup(func() { _ = custom.Close() })
		r := NewRouterFromContext(t.Context(), logr.Discard(), WithURL(custom))
		require.NotNil(t, r.url)
		assert.Same(t, custom, r.url.client)

		// Option order is last-wins in both directions, and WithNoCache works regardless of order.
		disableCache := false
		appCfg := &config.Config{HTTPClient: config.HTTPClientConfig{EnableCache: &disableCache}}
		rAppWins := NewRouter("scafctl", WithURL(custom), withAppConfigURL(appCfg))
		assert.Same(t, httpc.AppClientFromConfig(appCfg), rAppWins.url.client)

		rNoCacheFirst := NewRouter("scafctl", WithNoCache(), withAppConfigURL(appCfg))
		assert.Same(t, httpc.FetchClientFromConfig(appCfg), rNoCacheFirst.url.client)

		rNilWins := NewRouter("scafctl", withAppConfigURL(appCfg), WithURL(nil))
		assert.Same(t, httpc.FetchClientFromConfig(nil), rNilWins.url.client)
	})

	t.Run("catalog references are enabled", func(t *testing.T) {
		t.Parallel()
		// An unknown alias fails before any fetch, so the real catalog
		// directory is never opened.
		_, err := NewRouterFromContext(t.Context(), logr.Discard()).
			Load(t.Context(), source.Classification{Kind: source.KindCatalogQualified, Ref: "app", Catalog: "no-such-alias"})
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrSourceNotEnabled)
	})

	t.Run("stdin is disabled by default", func(t *testing.T) {
		t.Parallel()
		_, err := NewRouterFromContext(t.Context(), logr.Discard()).
			Load(t.Context(), source.Classification{Kind: source.KindStdin, Ref: "-"})
		require.ErrorIs(t, err, ErrSourceNotEnabled)
	})

	t.Run("caller options extend the defaults", func(t *testing.T) {
		t.Parallel()
		got, err := NewRouterFromContext(t.Context(), logr.Discard(), WithStdin(strings.NewReader(validSolutionYAML))).
			Load(t.Context(), source.Classification{Kind: source.KindStdin, Ref: "-"})
		require.NoError(t, err)
		assert.Equal(t, "test-solution", got.Solution.Metadata.Name)
	})

	t.Run("embedder binary name drives discovery", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{
			"scafctl/solution.yaml": "not: [valid",
			".mycli/mycli.yaml":     validSolutionYAML,
		})
		ctx := settings.IntoContext(provider.WithWorkingDirectory(t.Context(), dir), &settings.Run{BinaryName: "mycli"})

		got, err := NewRouterFromContext(ctx, logr.Discard()).
			Load(ctx, source.Classification{Kind: source.KindAutoDiscover})
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(dir, ".mycli", "mycli.yaml"), got.Solution.GetPath())
	})

	t.Run("custom action file names from settings", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{
			"solution.yaml": "not: [valid",
			"deploy.yaml":   validSolutionYAML,
		})
		ctx := settings.IntoContext(provider.WithWorkingDirectory(t.Context(), dir),
			&settings.Run{BinaryName: "mycli", ActionDiscoveryFileNames: []string{"deploy.yaml"}})

		got, err := NewRouterFromContext(ctx, logr.Discard(),
			WithDiscoverOptions(WithDiscoveryMode(settings.DiscoveryModeAction))).
			Load(ctx, source.Classification{Kind: source.KindAutoDiscover})
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(dir, "deploy.yaml"), got.Solution.GetPath())
		require.NotNil(t, got.Discovery)
		assert.True(t, got.Discovery.IsActionFile)
	})
}

func BenchmarkNewRouterFromContext(b *testing.B) {
	ctx := b.Context()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = NewRouterFromContext(ctx, logr.Discard())
	}
}

func BenchmarkRouter_Load(b *testing.B) {
	dir := b.TempDir()
	writeTree(b, dir, map[string]string{"solution.yaml": validSolutionYAML})
	ctx := provider.WithWorkingDirectory(b.Context(), dir)
	r := NewRouter("scafctl")
	c := source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"}

	for b.Loop() {
		if _, err := r.Load(ctx, c); err != nil {
			b.Fatal(err)
		}
	}
}
