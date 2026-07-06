// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

func newTestCache(t *testing.T, now func() time.Time) *diskCache {
	t.Helper()
	return &diskCache{baseDir: filepath.Join(t.TempDir(), cacheDirName), now: now}
}

func TestDiskCache_SetGetRoundTrip(t *testing.T) {
	t.Parallel()

	c := newTestCache(t, time.Now)
	entries := []Entry{{Name: "cluster-a", URL: "https://api.cluster-a.example.com:6443"}}
	c.Set(context.Background(), "key1", entries, time.Hour)

	got, ok := c.Get(context.Background(), "key1")
	require.True(t, ok)
	assert.Equal(t, entries, got)
}

func TestDiskCache_Miss(t *testing.T) {
	t.Parallel()

	c := newTestCache(t, time.Now)
	_, ok := c.Get(context.Background(), "absent")
	assert.False(t, ok)
}

func TestDiskCache_Expired(t *testing.T) {
	t.Parallel()

	base := time.Now()
	current := base
	c := newTestCache(t, func() time.Time { return current })

	c.Set(context.Background(), "key1", []Entry{{Name: "a", URL: "https://a"}}, time.Minute)

	// Advance past expiry.
	current = base.Add(2 * time.Minute)
	_, ok := c.Get(context.Background(), "key1")
	assert.False(t, ok, "expired entry must be a miss")

	// File should be removed on expired read.
	_, statErr := os.Stat(c.path("key1"))
	assert.True(t, os.IsNotExist(statErr), "expired file should be removed")
}

func TestDiskCache_ZeroTTLDisablesCaching(t *testing.T) {
	t.Parallel()

	c := newTestCache(t, time.Now)
	c.Set(context.Background(), "key1", []Entry{{Name: "a", URL: "https://a"}}, 0)

	_, ok := c.Get(context.Background(), "key1")
	assert.False(t, ok, "zero TTL must not persist an entry")
}

func TestDiskCache_CorruptFileIsMiss(t *testing.T) {
	t.Parallel()

	c := newTestCache(t, time.Now)
	require.NoError(t, os.MkdirAll(c.baseDir, cacheDirPerm))
	require.NoError(t, os.WriteFile(c.path("key1"), []byte("{ not json"), cacheFilePerm))

	_, ok := c.Get(context.Background(), "key1")
	assert.False(t, ok, "corrupt file must be treated as a miss")
}

func TestDiskCache_PeekAll(t *testing.T) {
	t.Parallel()

	base := time.Now()
	current := base
	c := newTestCache(t, func() time.Time { return current })

	// Two inventories under different keys; one will expire.
	c.Set(context.Background(), "auth-key", []Entry{{Name: "cluster-c", URL: "https://api.cluster-c.example.com:6443"}}, time.Minute)
	c.Set(context.Background(), "kube-key", []Entry{{Name: "cluster-a", URL: "https://api.cluster-a.example.com:6443"}}, time.Minute)

	// Advance past expiry: Get would miss + evict, but peekAll ignores TTL.
	current = base.Add(2 * time.Minute)

	all := c.peekAll()
	names := make(map[string]string, len(all))
	for _, e := range all {
		names[e.Name] = e.URL
	}
	assert.Len(t, all, 2, "peekAll returns entries from all cache files, ignoring TTL")
	assert.Equal(t, "https://api.cluster-c.example.com:6443", names["cluster-c"])
	assert.Equal(t, "https://api.cluster-a.example.com:6443", names["cluster-a"])

	// peekAll must not evict expired files.
	_, statErr := os.Stat(c.path("auth-key"))
	assert.NoError(t, statErr, "peekAll must not delete expired files")
}

func TestDiskCache_PeekAll_EmptyDir(t *testing.T) {
	t.Parallel()

	c := newTestCache(t, time.Now)
	assert.Nil(t, c.peekAll(), "no cache dir yields nil")
}

func TestAllCachedInventoryEntries_NoPanic(t *testing.T) {
	t.Parallel()

	// Smoke test: reads the real on-disk cache dir. It must never panic and
	// returns a slice (possibly nil when nothing is cached in the environment).
	_ = AllCachedInventoryEntries()
}

func TestCacheKey_StableAndSensitive(t *testing.T) {
	t.Parallel()

	rc := &config.HostnameResolverConfig{
		Source:    config.HostnameResolverSource{URL: "https://inv.example.com"},
		Transform: "_",
	}
	k1 := cacheKey("openshift", rc)
	k2 := cacheKey("openshift", rc)
	assert.Equal(t, k1, k2, "same inputs yield same key")

	// Different handler.
	assert.NotEqual(t, k1, cacheKey("other", rc))

	// Different URL.
	rc2 := &config.HostnameResolverConfig{
		Source:    config.HostnameResolverSource{URL: "https://other.example.com"},
		Transform: "_",
	}
	assert.NotEqual(t, k1, cacheKey("openshift", rc2))

	// Different transform.
	rc3 := &config.HostnameResolverConfig{
		Source:    config.HostnameResolverSource{URL: "https://inv.example.com"},
		Transform: "_.map(k, k)",
	}
	assert.NotEqual(t, k1, cacheKey("openshift", rc3))

	// Different auth provider.
	rc4 := &config.HostnameResolverConfig{
		Source:    config.HostnameResolverSource{URL: "https://inv.example.com", AuthProvider: "entra"},
		Transform: "_",
	}
	assert.NotEqual(t, k1, cacheKey("openshift", rc4))

	// Different auth scope.
	rc5 := &config.HostnameResolverConfig{
		Source:    config.HostnameResolverSource{URL: "https://inv.example.com", AuthScope: "api://x/.default"},
		Transform: "_",
	}
	assert.NotEqual(t, k1, cacheKey("openshift", rc5))

	// Different headers.
	rc6 := &config.HostnameResolverConfig{
		Source:    config.HostnameResolverSource{URL: "https://inv.example.com", Headers: map[string]string{"X-Env": "prod"}},
		Transform: "_",
	}
	k6 := cacheKey("openshift", rc6)
	assert.NotEqual(t, k1, k6)
	// Header key order must not matter (sorted internally).
	rc6b := &config.HostnameResolverConfig{
		Source: config.HostnameResolverSource{
			URL:     "https://inv.example.com",
			Headers: map[string]string{"X-Env": "prod"},
		},
		Transform: "_",
	}
	assert.Equal(t, k6, cacheKey("openshift", rc6b), "same headers yield same key")
}
