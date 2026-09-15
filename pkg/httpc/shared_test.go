// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

// resetSharedClients clears the shared client map so a test starts from a known
// state and does not inherit clients built by another test.
func resetSharedClients(t *testing.T) {
	t.Helper()
	sharedClientsMu.Lock()
	defer sharedClientsMu.Unlock()
	sharedClients = make(map[string]*Client, 1)
}

func ctxWithHTTPConfig(cfg config.HTTPClientConfig) context.Context {
	return config.WithConfig(context.Background(), &config.Config{HTTPClient: cfg})
}

func TestFetchClient_ReusesClientForSamePolicy(t *testing.T) {
	resetSharedClients(t)

	ctx := ctxWithHTTPConfig(config.HTTPClientConfig{
		AllowedPrivateCIDRs: []string{"10.0.0.0/8"},
	})

	first := FetchClient(ctx)
	second := FetchClient(ctx)

	require.NotNil(t, first)
	assert.Same(t, first, second,
		"the same policy must reuse one client, or every fetch pays a fresh handshake")
}

// Clients are keyed on the configuration verbatim, so two allowlists that are
// equivalent but differently ordered may get separate clients. That is an
// accepted cost (bounded by maxCachedClients) rather than paying normalization
// on every call. What must NOT happen is the reverse -- see
// TestFetchClient_DoesNotShareAcrossPolicies.
func TestFetchClient_IdenticalConfigShares(t *testing.T) {
	resetSharedClients(t)

	a := FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
		AllowedPrivateCIDRs: []string{"10.0.0.0/8", "192.168.1.5"},
	}))
	b := FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
		AllowedPrivateCIDRs: []string{"10.0.0.0/8", "192.168.1.5"},
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
		AllowedPrivateCIDRs: []string{"10.0.0.0/8"},
	}))
	// Empty is a distinct policy from absent: it overrides AllowPrivateIPs.
	empty := FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
		AllowPrivateIPs:     &allow,
		AllowedPrivateCIDRs: []string{},
	}))
	proxied := FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
		TrustProxyResolution: &trust,
	}))

	distinct := []*Client{permissive, restrictive, narrow, empty, proxied}
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
			AllowedPrivateCIDRs: []string{"127.0.0.0/8", "::1/128"},
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

func TestFetchClient_BoundsCachedClients(t *testing.T) {
	resetSharedClients(t)

	// Well past the bound, so the map must stop growing.
	for i := range maxCachedClients * 3 {
		cidr := "10." + string(rune('0'+i%10)) + ".0.0/16"
		_ = FetchClient(ctxWithHTTPConfig(config.HTTPClientConfig{
			AllowedPrivateCIDRs: []string{"10.0.0.0/8", cidr},
		}))
	}

	sharedClientsMu.Lock()
	size := len(sharedClients)
	sharedClientsMu.Unlock()

	assert.LessOrEqual(t, size, maxCachedClients,
		"the shared map must not grow without bound")
}

func TestFetchClient_ConcurrentCallersShareOneClient(t *testing.T) {
	resetSharedClients(t)

	ctx := ctxWithHTTPConfig(config.HTTPClientConfig{
		AllowedPrivateCIDRs: []string{"10.0.0.0/8"},
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
		"nil config":     configKey(nil),
		"zero":           configKey(&config.Config{}),
		"allow true":     configKey(&config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allow}}),
		"allow false":    configKey(&config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &deny}}),
		"empty list":     configKey(&config.Config{HTTPClient: config.HTTPClientConfig{AllowedPrivateCIDRs: []string{}}}),
		"populated list": configKey(&config.Config{HTTPClient: config.HTTPClientConfig{AllowedPrivateCIDRs: []string{"10.0.0.0/8"}}}),
	}

	seen := make(map[string]string, len(keys))
	for name, key := range keys {
		if other, dup := seen[key]; dup {
			t.Errorf("%q and %q produced the same key %q but are different policies", name, other, key)
		}
		seen[key] = name
	}
}

func BenchmarkFetchClient(b *testing.B) {
	allow := true
	ctx := config.WithConfig(context.Background(), &config.Config{
		HTTPClient: config.HTTPClientConfig{
			AllowPrivateIPs:     &allow,
			AllowedPrivateCIDRs: []string{"10.0.0.0/8", "192.168.0.0/16"},
		},
	})

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = FetchClient(ctx)
	}
}
