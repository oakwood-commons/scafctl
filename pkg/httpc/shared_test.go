// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

// resetSharedClients closes and clears the default shared client pool so a test
// starts from a known state and does not inherit clients built by another test.
func resetSharedClients(t *testing.T) {
	t.Helper()
	require.NoError(t, defaultPool.Close())
}

func ctxWithHTTPConfig(cfg config.HTTPClientConfig) context.Context {
	return config.WithConfig(context.Background(), &config.Config{HTTPClient: cfg})
}

func TestClientPool_Lifecycle(t *testing.T) {
	t.Parallel()

	t.Run("nil pool is safe", func(t *testing.T) {
		t.Parallel()
		var p *ClientPool
		assert.Zero(t, p.Len())
		require.NoError(t, p.Close())
	})

	t.Run("zero value and non-positive capacity default to DefaultClientPoolCap", func(t *testing.T) {
		t.Parallel()
		p := NewClientPool(0)
		assert.Equal(t, DefaultClientPoolCap, p.capacity)

		var zero ClientPool
		c := zero.GetOrBuild("k", func() *Client { return NewClient(&ClientConfig{EnableCache: false}) })
		require.NotNil(t, c)
		assert.Equal(t, 1, zero.Len())
		assert.Nil(t, zero.GetOrBuild("nil", func() *Client { return nil }))
		assert.Equal(t, 1, zero.Len())
		require.NoError(t, zero.Close())
		assert.Zero(t, zero.Len())

		// Reusable after Close.
		c2 := zero.GetOrBuild("k", func() *Client { return NewClient(&ClientConfig{EnableCache: false}) })
		require.NotNil(t, c2)
		assert.NotSame(t, c, c2)
		require.NoError(t, zero.Close())
	})
}

func TestFetchClient_ReusesClientForSamePolicy(t *testing.T) {
	resetSharedClients(t)

	ctx := ctxWithHTTPConfig(config.HTTPClientConfig{
		AllowedPrivateCIDRs: config.PrivateCIDRList("10.0.0.0/8"),
	})

	first := FetchClient(ctx)
	second := FetchClient(ctx)

	require.NotNil(t, first)
	assert.Same(t, first, second,
		"the same policy must reuse one client, or every fetch pays a fresh handshake")
}

// Clients are keyed on the configuration verbatim, so two allowlists that are
// equivalent but differently ordered may get separate clients. That is an
// accepted cost (bounded by DefaultClientPoolCap) rather than paying
// normalization on every call. What must NOT happen is the reverse -- see
// TestFetchClient_DoesNotShareAcrossPolicies.
func TestFetchClient_IdenticalConfigShares(t *testing.T) {
	resetSharedClients(t)

	a := FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
		AllowedPrivateCIDRs: config.PrivateCIDRList("10.0.0.0/8", "192.168.1.5"),
	}))
	b := FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
		AllowedPrivateCIDRs: config.PrivateCIDRList("10.0.0.0/8", "192.168.1.5"),
	}))

	assert.Same(t, a, b, "identical allowlists should share one client")
}

// The security-relevant half: a client's connections were opened under its own
// policy, so a different policy must never be handed that client.
func TestFetchClient_DoesNotShareAcrossPolicies(t *testing.T) {
	resetSharedClients(t)

	allow := true
	trust := true

	permissive := FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
		AllowPrivateIPs: &allow,
	}))
	restrictive := FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{}))
	narrow := FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
		AllowedPrivateCIDRs: config.PrivateCIDRList("10.0.0.0/8"),
	}))
	// Empty is a distinct policy from absent: it overrides AllowPrivateIPs.
	empty := FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
		AllowPrivateIPs:     &allow,
		AllowedPrivateCIDRs: config.PrivateCIDRList(),
	}))
	proxied := FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
		TrustProxyResolution: &trust,
	}))
	trustedProxy := FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
		TrustedProxy: &trust,
	}))

	distinct := []*Client{permissive, restrictive, narrow, empty, proxied, trustedProxy}
	for i := range distinct {
		for j := i + 1; j < len(distinct); j++ {
			assert.NotSame(t, distinct[i], distinct[j],
				"clients %d and %d have different policies and must not be shared", i, j)
		}
	}
}

// A missing configuration fails closed, and must not be served the client built
// for some other configuration.
func TestFetchClient_MissingConfigGetsItsOwnClient(t *testing.T) {
	resetSharedClients(t)

	allow := true
	withCfg := FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{AllowPrivateIPs: &allow}))
	noCfg := FetchClient(context.Background())

	require.NotNil(t, noCfg)
	assert.NotSame(t, withCfg, noCfg)
	assert.Same(t, noCfg, FetchClient(context.Background()),
		"the no-config case should still be reused")
	assert.Same(t, noCfg, FetchClientFromConfig(nil),
		"FetchClientFromConfig(nil) shares the no-config client")
}

// The shared client must actually work, and must still enforce its policy.
func TestFetchClient_EnforcesPolicy(t *testing.T) {
	resetSharedClients(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Run("reaches an allowed address", func(t *testing.T) {
		ctx := ctxWithHTTPConfig(config.HTTPClientConfig{
			AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
		})
		resp, err := FetchClient(ctx).Get(ctx, srv.URL)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("refuses a blocked address", func(t *testing.T) {
		ctx := ctxWithHTTPConfig(config.HTTPClientConfig{})
		resp, err := FetchClient(ctx).Get(ctx, srv.URL) //nolint:bodyclose // refused, no body
		if resp != nil && resp.Body != nil {
			defer resp.Body.Close()
		}
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrBlockedByPolicy)
	})
}

func TestFetchClient_HonorsAppSettingsAndDisablesCache(t *testing.T) {
	resetSharedClients(t)

	var newConns atomic.Int32
	var hits atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hit-" + strconv.Itoa(int(n))))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	enableCache := true
	ctx := ctxWithHTTPConfig(config.HTTPClientConfig{
		EnableCache:         &enableCache,
		CacheDir:            t.TempDir(),
		Timeout:             "5s",
		AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
	})

	for i := range 3 {
		resp, err := FetchClient(ctx).Get(ctx, srv.URL+"/item")
		require.NoError(t, err)
		body, readErr := io.ReadAll(resp.Body)
		require.NoError(t, readErr)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "hit-"+strconv.Itoa(i+1), string(body))
	}

	assert.Equal(t, int32(3), hits.Load(), "FetchClient must force EnableCache=false even when app config enables caching")
	assert.Equal(t, int32(1), newConns.Load(), "repeated FetchClient requests to one host must reuse a single connection")
}

func TestAppClient_HonorsCacheAndReusesConnection(t *testing.T) {
	resetSharedClients(t)

	t.Run("distinct from FetchClient and caches when enabled", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("cached-payload"))
		}))
		defer srv.Close()

		enableCache := true
		cfg := &config.Config{
			HTTPClient: config.HTTPClientConfig{
				EnableCache:         &enableCache,
				CacheType:           "memory",
				CacheDir:            t.TempDir(),
				AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
			},
		}
		ctx := config.WithConfig(t.Context(), cfg)

		appClient := AppClient(ctx)
		fetchClient := FetchClient(ctx)
		assert.NotSame(t, appClient, fetchClient, "app and fetch shapes must not share a client")
		assert.Same(t, appClient, AppClientFromConfig(cfg))

		for range 2 {
			resp, err := appClient.Get(ctx, srv.URL+"/cached")
			require.NoError(t, err)
			body, readErr := io.ReadAll(resp.Body)
			require.NoError(t, readErr)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, "cached-payload", string(body))
		}
		assert.Equal(t, int32(1), hits.Load(), "AppClient must honor EnableCache=true")
	})

	t.Run("reuses TCP connection across uncached requests", func(t *testing.T) {
		var newConns atomic.Int32
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
			if state == http.StateNew {
				newConns.Add(1)
			}
		}
		srv.Start()
		defer srv.Close()

		disableCache := false
		ctx := ctxWithHTTPConfig(config.HTTPClientConfig{
			EnableCache:         &disableCache,
			AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
		})

		for range 3 {
			resp, err := AppClient(ctx).Get(ctx, srv.URL+"/uncached")
			require.NoError(t, err)
			_, readErr := io.ReadAll(resp.Body)
			require.NoError(t, readErr)
			require.NoError(t, resp.Body.Close())
		}
		assert.Equal(t, int32(1), newConns.Load(), "AppClient must reuse one TCP connection across requests")
	})
}

func TestFetchClient_BoundsCachedClients(t *testing.T) {
	resetSharedClients(t)

	// Well past the bound, so the pool must stop growing.
	for i := range DefaultClientPoolCap * 3 {
		cidr := "10." + strconv.Itoa(i%250) + ".0.0/16"
		_ = FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
			AllowedPrivateCIDRs: config.PrivateCIDRList("10.0.0.0/8", cidr),
		}))
	}

	assert.LessOrEqual(t, defaultPool.Len(), DefaultClientPoolCap,
		"the shared pool must not grow without bound")
}

// TestFetchClient_EvictedClientStillServicesBorrower pins the eviction
// contract: the pool owns bookkeeping, not the client's lifetime. A borrower
// that already holds a client when a different policy trips the bound loses
// nothing -- Close at eviction is a cleanup hint that reaps currently-idle
// connections only, so the borrower's next request dials fresh and completes
// normally.
func TestFetchClient_EvictedClientStillServicesBorrower(t *testing.T) {
	resetSharedClients(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	borrowed := make([]*Client, 0, DefaultClientPoolCap)
	for i := range DefaultClientPoolCap {
		borrowed = append(borrowed, FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
			AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128", "10."+strconv.Itoa(i)+".0.0/16"),
		})))
	}

	_ = FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
		AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128", "10.200.0.0/16"),
	}))

	assert.LessOrEqual(t, defaultPool.Len(), DefaultClientPoolCap, "the eviction must have kept the pool at its bound")

	for i, c := range borrowed {
		resp, err := c.Get(t.Context(), srv.URL)
		require.NoError(t, err, "borrowed client %d must still service requests after eviction+Close", i)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Body)
		status := resp.StatusCode
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, http.StatusOK, status,
			"borrowed client %d's post-eviction request must complete normally", i)
	}
}

func TestFetchClient_ConcurrentCallersShareOneClient(t *testing.T) {
	resetSharedClients(t)

	ctx := ctxWithHTTPConfig(config.HTTPClientConfig{
		AllowedPrivateCIDRs: config.PrivateCIDRList("10.0.0.0/8"),
	})

	const goroutines = 32
	var wg sync.WaitGroup
	seen := make([]*Client, goroutines)

	wg.Add(goroutines)
	for i := range goroutines {
		go func() {
			defer wg.Done()
			seen[i] = FetchClient(ctx)
		}()
	}
	wg.Wait()

	for i := 1; i < goroutines; i++ {
		assert.Same(t, seen[0], seen[i], "every caller should get the same client")
	}
}

func TestConfigKey(t *testing.T) {
	t.Parallel()

	allow := true
	deny := false

	// Absent, empty, and populated allowlists are three different policies.
	keys := map[string]string{
		"nil config":     ConfigKey(nil),
		"zero":           ConfigKey(&config.Config{}),
		"allow true":     ConfigKey(&config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allow}}),
		"allow false":    ConfigKey(&config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &deny}}),
		"empty list":     ConfigKey(&config.Config{HTTPClient: config.HTTPClientConfig{AllowedPrivateCIDRs: config.PrivateCIDRList()}}),
		"populated list": ConfigKey(&config.Config{HTTPClient: config.HTTPClientConfig{AllowedPrivateCIDRs: config.PrivateCIDRList("10.0.0.0/8")}}),
	}

	seen := make(map[string]string, len(keys))
	for name, key := range keys {
		if other, dup := seen[key]; dup {
			t.Errorf("%q and %q produced the same key %q but are different policies", name, other, key)
		}
		seen[key] = name
	}

	assert.Equal(t, "nilhttp", HTTPConfigKey(nil))
	assert.NotEqual(t, HTTPConfigKey(&config.HTTPClientConfig{}), HTTPConfigKey(&config.HTTPClientConfig{AllowedPrivateCIDRs: config.PrivateCIDRList()}))
}

func TestConfigKey_EmbedderAndCacheDirIsolation(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{
			CacheDir: "/tmp/shared-cache",
		},
	}
	keyDefault := configKeyForApp("scafctl", "/cache/scafctl", cfg)
	keyEmbedder := configKeyForApp("mycli", "/cache/mycli", cfg)
	assert.NotEqual(t, keyDefault, keyEmbedder, "distinct binary names must never share a ConfigKey")

	// When CacheDir is unset on HTTPClientConfig, changing the default XDG HTTP
	// cache directory must also yield a distinct key.
	keyCacheA := configKeyForApp("scafctl", "/tmp/xdg-a/scafctl/http-cache", &config.Config{})
	keyCacheB := configKeyForApp("scafctl", "/tmp/xdg-b/scafctl/http-cache", &config.Config{})
	assert.NotEqual(t, keyCacheA, keyCacheB)
}

func BenchmarkFetchClient(b *testing.B) {
	allow := true
	ctx := config.WithConfig(context.Background(), &config.Config{
		HTTPClient: config.HTTPClientConfig{
			AllowPrivateIPs:     &allow,
			AllowedPrivateCIDRs: config.PrivateCIDRList("10.0.0.0/8", "192.168.0.0/16"),
		},
	})

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = FetchClient(ctx)
	}
}

func BenchmarkAppClient(b *testing.B) {
	allow := true
	disableCache := false
	ctx := config.WithConfig(context.Background(), &config.Config{
		HTTPClient: config.HTTPClientConfig{
			EnableCache:         &disableCache,
			CacheDir:            b.TempDir(),
			AllowPrivateIPs:     &allow,
			AllowedPrivateCIDRs: config.PrivateCIDRList("10.0.0.0/8", "192.168.0.0/16"),
		},
	})

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = AppClient(ctx)
	}
}
