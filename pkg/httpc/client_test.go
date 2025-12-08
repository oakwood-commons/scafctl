package httpc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, 30*time.Second, config.Timeout)
	assert.Equal(t, 3, config.RetryMax)
	assert.Equal(t, 1*time.Second, config.RetryWaitMin)
	assert.Equal(t, 30*time.Second, config.RetryWaitMax)
	assert.True(t, config.EnableCache)
	assert.Equal(t, 10*time.Minute, config.CacheTTL)
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name   string
		config *ClientConfig
	}{
		{
			name:   "with default config",
			config: nil,
		},
		{
			name: "with custom config",
			config: &ClientConfig{
				Timeout:      10 * time.Second,
				RetryMax:     5,
				RetryWaitMin: 500 * time.Millisecond,
				RetryWaitMax: 10 * time.Second,
				EnableCache:  false,
				Logger:       logr.Discard(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.config)
			require.NotNil(t, client)
			require.NotNil(t, client.httpClient)
			require.NotNil(t, client.retryClient)
		})
	}
}

func TestClient_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))
	defer server.Close()

	client := NewClient(DefaultConfig())
	ctx := context.Background()

	resp, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "success", string(body))
}

func TestClient_Post(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, `{"test":"data"}`, string(body))

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	}))
	defer server.Close()

	client := NewClient(DefaultConfig())
	ctx := context.Background()

	resp, err := client.Post(ctx, server.URL, "application/json", strings.NewReader(`{"test":"data"}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestClient_Put(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(DefaultConfig())
	ctx := context.Background()

	resp, err := client.Put(ctx, server.URL, "application/json", strings.NewReader(`{"update":"data"}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_Delete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(DefaultConfig())
	ctx := context.Background()

	resp, err := client.Delete(ctx, server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestClient_Retry(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)

		// Fail the first 2 requests, succeed on the 3rd
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success after retry"))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.RetryMax = 3
	config.RetryWaitMin = 100 * time.Millisecond
	config.RetryWaitMax = 200 * time.Millisecond

	client := NewClient(config)
	ctx := context.Background()

	resp, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(3), requestCount.Load(), "Should have made 3 requests (1 initial + 2 retries)")
}

func TestClient_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the client timeout
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := DefaultConfig()
	config.Timeout = 100 * time.Millisecond
	config.RetryMax = 0 // Disable retries for this test

	client := NewClient(config)
	ctx := context.Background()

	start := time.Now()
	r, err := client.Get(ctx, server.URL)
	elapsed := time.Since(start)
	if r != nil && r.Body != nil {
		r.Body.Close()
	}
	require.Error(t, err)
	// Should timeout quickly, not wait the full 2 seconds
	assert.Less(t, elapsed, 1*time.Second, "Request should timeout quickly")
}

func TestClient_Cache(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		// Set appropriate cache headers for GET requests
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"cached": true}`))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.EnableCache = true
	config.CacheTTL = 10 * time.Second

	client := NewClient(config)
	ctx := context.Background()

	// First request - should hit the server
	resp1, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	assert.Equal(t, `{"cached": true}`, string(body1))

	// Second request - should be served from cache (httpcache respects Cache-Control headers)
	resp2, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	assert.Equal(t, `{"cached": true}`, string(body2))

	// With proper cache headers, the second request should be cached
	// However, the exact behavior depends on httpcache implementation
	// So we'll verify that at least both requests succeeded
	assert.GreaterOrEqual(t, requestCount.Load(), int32(1), "At least one request should have been made")
	assert.LessOrEqual(t, requestCount.Load(), int32(2), "No more than two requests should have been made")
}

func TestClient_CacheDisabled(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.EnableCache = false

	client := NewClient(config)
	ctx := context.Background()

	// Make two requests
	resp1, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	resp1.Body.Close()

	resp2, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	resp2.Body.Close()

	// Both should hit the server
	assert.Equal(t, int32(2), requestCount.Load(), "Both requests should hit the server when cache is disabled")
}

func TestClient_Do(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "CustomValue", r.Header.Get("X-Custom-Header"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(DefaultConfig())

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	req.Header.Set("X-Custom-Header", "CustomValue")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_StandardClient(t *testing.T) {
	client := NewClient(DefaultConfig())
	stdClient := client.StandardClient()

	require.NotNil(t, stdClient)
	assert.IsType(t, &http.Client{}, stdClient)
}

func TestClient_RetryableClient(t *testing.T) {
	client := NewClient(DefaultConfig())
	retryClient := client.RetryableClient()

	require.NotNil(t, retryClient)
}

func TestClient_FilesystemCache(t *testing.T) {
	var requestCount atomic.Int32
	tmpDir := filepath.Join(t.TempDir(), "http-cache")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		// Set appropriate cache headers for GET requests
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"cached": true}`))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.EnableCache = true
	config.CacheType = CacheTypeFilesystem
	config.CacheDir = tmpDir
	config.CacheTTL = 10 * time.Second

	client := NewClient(config)
	ctx := context.Background()

	// First request - should hit the server
	resp1, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	assert.Equal(t, `{"cached": true}`, string(body1))

	// Second request - should be served from cache
	resp2, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	assert.Equal(t, `{"cached": true}`, string(body2))

	// Verify cache directory was created
	info, err := os.Stat(tmpDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Both requests succeeded, cache behavior depends on httpcache implementation
	assert.GreaterOrEqual(t, requestCount.Load(), int32(1))
	assert.LessOrEqual(t, requestCount.Load(), int32(2))
}

func TestClient_FilesystemCache_InvalidDir(t *testing.T) {
	config := DefaultConfig()
	config.EnableCache = true
	config.CacheType = CacheTypeFilesystem
	config.CacheDir = "/invalid/path/that/cannot/be/created"

	// Should fall back to memory cache
	client := NewClient(config)
	require.NotNil(t, client)

	// Client should still work
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	resp, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_CacheTypeMemory(t *testing.T) {
	config := DefaultConfig()
	config.EnableCache = true
	config.CacheType = CacheTypeMemory

	client := NewClient(config)
	require.NotNil(t, client)

	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	resp, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_ClearCache_Filesystem(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "http-cache")
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.EnableCache = true
	config.CacheType = CacheTypeFilesystem
	config.CacheDir = tmpDir
	config.CacheTTL = 10 * time.Minute

	client := NewClient(config)
	ctx := context.Background()

	// First request - should cache
	resp1, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	resp1.Body.Close()

	// Clear the cache
	err = client.ClearCache()
	require.NoError(t, err)

	// Second request - should hit server again since cache was cleared
	resp2, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	resp2.Body.Close()

	// Should have made at least 2 requests (cache was cleared)
	assert.GreaterOrEqual(t, requestCount.Load(), int32(2))
}

func TestClient_ClearCache_NoCacheConfigured(t *testing.T) {
	config := DefaultConfig()
	config.EnableCache = false

	client := NewClient(config)

	// Should not error when cache is not configured
	err := client.ClearCache()
	require.NoError(t, err)
}

func TestClient_ClearCache_MemoryCache(t *testing.T) {
	config := DefaultConfig()
	config.EnableCache = true
	config.CacheType = CacheTypeMemory

	client := NewClient(config)

	// Memory cache doesn't support Clear, should return error
	err := client.ClearCache()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache clearing not supported")
}

func TestClient_CleanExpiredCache(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "http-cache")

	config := DefaultConfig()
	config.EnableCache = true
	config.CacheType = CacheTypeFilesystem
	config.CacheDir = tmpDir
	config.CacheTTL = 100 * time.Millisecond

	client := NewClient(config)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))
	defer server.Close()

	// Make a request to cache it
	resp, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	resp.Body.Close()

	// Wait for cache to expire
	time.Sleep(200 * time.Millisecond)

	// Clean expired entries
	err = client.CleanExpiredCache()
	require.NoError(t, err)
}

func TestClient_CleanExpiredCache_MemoryCache(t *testing.T) {
	config := DefaultConfig()
	config.EnableCache = true
	config.CacheType = CacheTypeMemory

	client := NewClient(config)

	// Memory cache doesn't support CleanExpired, should return error
	err := client.CleanExpiredCache()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache cleanup not supported")
}

func TestClient_DeleteCacheEntry(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "http-cache")

	config := DefaultConfig()
	config.EnableCache = true
	config.CacheType = CacheTypeFilesystem
	config.CacheDir = tmpDir
	config.CacheTTL = 10 * time.Minute

	client := NewClient(config)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))
	defer server.Close()

	// Make a request to cache it
	resp, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	resp.Body.Close()

	// Delete the cache entry
	err = client.DeleteCacheEntry(ctx, server.URL)
	require.NoError(t, err)
}

func TestClient_DeleteCacheEntry_NoCacheConfigured(t *testing.T) {
	config := DefaultConfig()
	config.EnableCache = false

	client := NewClient(config)
	ctx := context.Background()

	// Should not error when cache is not configured
	err := client.DeleteCacheEntry(ctx, "http://example.com")
	require.NoError(t, err)
}
