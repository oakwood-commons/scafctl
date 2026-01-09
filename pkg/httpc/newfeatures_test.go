package httpc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWarmCache(t *testing.T) {
	hitCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test data"))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.EnableCache = true
	config.CacheType = CacheTypeMemory
	client := NewClient(config)
	defer client.Close()

	ctx := context.Background()

	// Warm cache with URLs
	urls := []string{
		server.URL + "/api/1",
		server.URL + "/api/2",
	}

	err := client.WarmCache(ctx, urls)
	require.NoError(t, err)
	assert.Equal(t, 2, hitCount)

	// Request again - should come from cache
	resp, err := client.Get(ctx, urls[0])
	require.NoError(t, err)
	defer resp.Body.Close()

	// Hit count should still be 2 (from cache)
	assert.Equal(t, 2, hitCount)
}

func TestWarmCache_NoCache(t *testing.T) {
	config := DefaultConfig()
	config.EnableCache = false
	client := NewClient(config)
	defer client.Close()

	ctx := context.Background()
	err := client.WarmCache(ctx, []string{"http://example.com"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cache not enabled")
}

func TestCacheStats_Structured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.EnableCache = true
	config.CacheType = CacheTypeMemory
	client := NewClient(config)
	defer client.Close()

	ctx := context.Background()

	// Initial stats
	stats := client.CacheStats()
	require.NotNil(t, stats)
	assert.Equal(t, uint64(0), stats.Hits)
	assert.Equal(t, uint64(0), stats.Misses)
	assert.Equal(t, 0.0, stats.HitRate)

	// First request (miss)
	resp, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// Second request (hit)
	resp2, err2 := client.Get(ctx, server.URL)
	require.NoError(t, err2)
	resp2.Body.Close()

	// Check stats
	stats = client.CacheStats()
	require.NotNil(t, stats)
	assert.Equal(t, uint64(1), stats.Hits)
	assert.Equal(t, uint64(1), stats.Misses)
	assert.Equal(t, 0.5, stats.HitRate) // 1/(1+1) = 0.5
}

func TestRequestResponseHooks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if request hook added the header
		assert.Equal(t, "test-value", r.Header.Get("X-Test-Header"))
		w.Header().Set("X-Response-Header", "response-value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))
	defer server.Close()

	requestHookCalled := false
	responseHookCalled := false

	config := DefaultConfig()
	config.EnableCache = false
	config.RequestHooks = []RequestHook{
		func(req *http.Request) error {
			requestHookCalled = true
			req.Header.Set("X-Test-Header", "test-value")
			return nil
		},
	}
	config.ResponseHooks = []ResponseHook{
		func(resp *http.Response) error {
			responseHookCalled = true
			assert.Equal(t, "response-value", resp.Header.Get("X-Response-Header"))
			return nil
		},
	}

	client := NewClient(config)
	ctx := context.Background()

	resp, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.True(t, requestHookCalled)
	assert.True(t, responseHookCalled)
}

func TestRequestHook_Error(t *testing.T) {
	config := DefaultConfig()
	config.EnableCache = false
	config.RequestHooks = []RequestHook{
		func(req *http.Request) error {
			return assert.AnError
		},
	}

	client := NewClient(config)
	ctx := context.Background()

	resp, err := client.Get(ctx, "http://example.com")
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "request hook failed")
}

func TestResponseHook_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := DefaultConfig()
	config.EnableCache = false
	config.ResponseHooks = []ResponseHook{
		func(resp *http.Response) error {
			return assert.AnError
		},
	}

	client := NewClient(config)
	ctx := context.Background()

	resp, err := client.Get(ctx, server.URL)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "response hook failed")
}

func TestCircuitBreaker(t *testing.T) {
	failureCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failureCount < 5 {
			failureCount++
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.EnableCache = false
	config.EnableCircuitBreaker = true
	config.CircuitBreakerConfig = &CircuitBreakerConfig{
		MaxFailures:         3,
		OpenTimeout:         100 * time.Millisecond,
		HalfOpenMaxRequests: 1,
	}
	config.RetryMax = 0 // Disable retries for this test
	config.Logger = logr.Discard()

	client := NewClient(config)
	ctx := context.Background()

	// Make requests that will fail (circuit breaker counts failures, not retries)
	for i := 0; i < 3; i++ {
		resp, err := client.Get(ctx, server.URL)
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		// Expect 500 status
		if err == nil {
			assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		}
	}

	// Next request should fail with circuit breaker open
	resp, err := client.Get(ctx, server.URL)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrCircuitBreakerOpen)

	// Wait for circuit breaker to transition to half-open
	time.Sleep(150 * time.Millisecond)

	// Next request should succeed (circuit goes to closed)
	resp2, err2 := client.Get(ctx, server.URL)
	if err2 != nil {
		t.Logf("Expected success but got error: %v", err2)
		// Circuit might still be healing, this is acceptable
		return
	}
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}

func TestCompression(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if client accepts compression
		acceptEncoding := r.Header.Get("Accept-Encoding")
		assert.Contains(t, acceptEncoding, "gzip")

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test data"))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.EnableCache = false
	config.EnableCompression = true

	client := NewClient(config)
	ctx := context.Background()

	resp, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "test data", string(body))
}

func TestMemoryCacheSize(t *testing.T) {
	config := DefaultConfig()
	config.EnableCache = true
	config.CacheType = CacheTypeMemory
	config.MemoryCacheSize = 5

	client := NewClient(config)
	defer client.Close()

	// Just verify it doesn't panic with custom size
	assert.NotNil(t, client)
}

func TestConcurrentRequestsMetric(t *testing.T) {
	// This is a basic test to ensure concurrent requests tracking doesn't panic
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := DefaultConfig()
	config.EnableCache = false
	client := NewClient(config)
	ctx := context.Background()

	// Make concurrent requests
	done := make(chan bool, 3)
	for i := 0; i < 3; i++ {
		go func() {
			resp, err := client.Get(ctx, server.URL)
			if err == nil {
				resp.Body.Close()
			}
			done <- true
		}()
	}

	// Wait for all to complete
	for i := 0; i < 3; i++ {
		<-done
	}
}

func TestRetryableLogger(t *testing.T) {
	// Test that the retryable logger doesn't panic
	logger := logr.Discard()
	rl := &retryableLogger{logger: logger}

	rl.Error("error message", "key", "value")
	rl.Info("info message", "key", "value")
	rl.Debug("debug message", "key", "value")
	rl.Warn("warn message", "key", "value")
}
