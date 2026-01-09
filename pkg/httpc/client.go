package httpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/oakwood-commons/scafctl/pkg/metrics"
	"ivan.dev/httpcache"
)

// ErrCacheSizeLimitExceeded is returned when attempting to cache data exceeds size limit
var ErrCacheSizeLimitExceeded = errors.New("cache: size limit exceeded")

// RequestHook is a function that processes a request before it's sent
type RequestHook func(*http.Request) error

// ResponseHook is a function that processes a response after it's received
type ResponseHook func(*http.Response) error

// CacheStats represents cache hit and miss statistics with computed hit rate
type CacheStats struct {
	Hits    uint64
	Misses  uint64
	HitRate float64 // Computed as Hits / (Hits + Misses)
}

// CacheType defines the type of cache to use
type CacheType string

const (
	// CacheTypeMemory uses in-memory caching
	CacheTypeMemory CacheType = "memory"
	// CacheTypeFilesystem uses filesystem-based caching
	CacheTypeFilesystem CacheType = "filesystem"
)

// ClientConfig holds the configuration for the HTTP client
type ClientConfig struct {
	// Timeout is the maximum time to wait for a request to complete
	Timeout time.Duration
	// RetryMax is the maximum number of retries
	RetryMax int
	// RetryWaitMin is the minimum time to wait between retries
	RetryWaitMin time.Duration
	// RetryWaitMax is the maximum time to wait between retries
	RetryWaitMax time.Duration
	// EnableCache enables HTTP caching
	EnableCache bool
	// CacheType specifies the type of cache to use (memory or filesystem)
	CacheType CacheType
	// CacheDir is the directory to use for filesystem cache (only used when CacheType is filesystem)
	// Defaults to ~/.scafctl/http-cache
	CacheDir string
	// CacheTTL is the time-to-live for cached responses
	CacheTTL time.Duration
	// CacheKeyPrefix is a prefix added to all cache keys to prevent collisions
	CacheKeyPrefix string
	// MaxCacheFileSize is the maximum size in bytes for a single cached file (0 = no limit)
	// Only applies to filesystem cache
	MaxCacheFileSize int64
	// MemoryCacheSize is the maximum number of entries in the memory cache (default: 1000)
	MemoryCacheSize int
	// Logger is the logger to use for the client
	Logger logr.Logger
	// CheckRetry is a custom retry policy function
	CheckRetry retryablehttp.CheckRetry
	// Backoff is a custom backoff policy function
	Backoff retryablehttp.Backoff
	// ErrorHandler is called if retries are exhausted
	ErrorHandler retryablehttp.ErrorHandler
	// RequestHooks are functions called before each request
	RequestHooks []RequestHook
	// ResponseHooks are functions called after each response
	ResponseHooks []ResponseHook
	// EnableCircuitBreaker enables circuit breaker pattern
	EnableCircuitBreaker bool
	// CircuitBreakerConfig holds circuit breaker configuration
	CircuitBreakerConfig *CircuitBreakerConfig
	// EnableCompression enables automatic gzip compression for requests/responses
	EnableCompression bool
}

// DefaultConfig returns a ClientConfig with sensible defaults
func DefaultConfig() *ClientConfig {
	return &ClientConfig{
		Timeout:              30 * time.Second,
		RetryMax:             3,
		RetryWaitMin:         1 * time.Second,
		RetryWaitMax:         30 * time.Second,
		EnableCache:          true,
		CacheType:            CacheTypeFilesystem,
		CacheDir:             "~/.scafctl/http-cache",
		CacheTTL:             10 * time.Minute,
		CacheKeyPrefix:       "scafctl:",
		MaxCacheFileSize:     10 * 1024 * 1024, // 10MB default
		MemoryCacheSize:      1000,
		Logger:               logr.Discard(),
		EnableCircuitBreaker: false,
		CircuitBreakerConfig: DefaultCircuitBreakerConfig(),
		EnableCompression:    true,
	}
}

// Client is an HTTP client with retry, timeout, and caching capabilities.
//
// Thread-Safety: Client is safe for concurrent use by multiple goroutines.
// All methods can be called concurrently. The underlying retryable HTTP client,
// cache implementations, and circuit breaker are all thread-safe.
// Multiple goroutines can share a single Client instance without additional synchronization.
type Client struct {
	retryClient    *retryablehttp.Client
	httpClient     *http.Client
	config         *ClientConfig
	cache          httpcache.Cache // Store reference to cache for clearing
	circuitBreaker *circuitBreaker
}

// NewClient creates a new HTTP client with the provided configuration
func NewClient(config *ClientConfig) *Client {
	if config == nil {
		config = DefaultConfig()
	}

	// Create the base retryable HTTP client
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = config.RetryMax
	retryClient.RetryWaitMin = config.RetryWaitMin
	retryClient.RetryWaitMax = config.RetryWaitMax

	if config.CheckRetry != nil {
		retryClient.CheckRetry = wrapCheckRetryWithMetrics(config.CheckRetry)
	} else {
		retryClient.CheckRetry = wrapCheckRetryWithMetrics(retryablehttp.DefaultRetryPolicy)
	}

	if config.Backoff != nil {
		retryClient.Backoff = config.Backoff
	}

	if config.ErrorHandler != nil {
		retryClient.ErrorHandler = config.ErrorHandler
	}

	// Set up logging
	if config.Logger.GetSink() != nil {
		retryClient.Logger = &retryableLogger{logger: config.Logger}
	}

	// Configure the HTTP client with caching if enabled
	var httpClient *http.Client
	var cache httpcache.Cache

	if config.EnableCache {
		// Create cache based on type
		switch config.CacheType {
		case CacheTypeFilesystem:
			cacheDir := config.CacheDir
			if cacheDir == "" {
				cacheDir = "~/.scafctl/http-cache"
			}
			fileCacheConfig := &FileCacheConfig{
				Dir:       cacheDir,
				TTL:       config.CacheTTL,
				KeyPrefix: config.CacheKeyPrefix,
				MaxSize:   config.MaxCacheFileSize,
				Logger:    config.Logger,
			}
			fileCache, err := NewFileCache(fileCacheConfig)
			if err != nil {
				// Fall back to memory cache if filesystem cache fails
				if config.Logger.GetSink() != nil {
					config.Logger.Error(err, "Failed to create filesystem cache, falling back to memory cache")
				}
				cacheSize := config.MemoryCacheSize
				if cacheSize <= 0 {
					cacheSize = 1000
				}
				memCache := httpcache.MemoryCache(cacheSize, config.CacheTTL)
				cache = newMetricsMemoryCache(memCache)
			} else {
				cache = fileCache
			}
		case CacheTypeMemory:
			fallthrough
		default:
			cacheSize := config.MemoryCacheSize
			if cacheSize <= 0 {
				cacheSize = 1000
			}
			memCache := httpcache.MemoryCache(cacheSize, config.CacheTTL)
			cache = newMetricsMemoryCache(memCache)
		}

		// Get base transport and wrap with metrics transport
		baseTransport := retryClient.HTTPClient.Transport
		if baseTransport == nil {
			baseTransport = http.DefaultTransport
		}

		// Wrap base transport with metrics transport
		metricsTransport := newMetricsTransport(baseTransport)

		// Wrap with compression if enabled
		var finalTransport http.RoundTripper = metricsTransport
		if config.EnableCompression {
			finalTransport = newCompressionTransport(finalTransport)
		}

		// Create a cached transport that wraps the final transport
		cachedTransport := httpcache.NewCacheTransport(
			finalTransport,
			cache,
			httpcache.WithTTL(config.CacheTTL),
		)

		// Set the cached transport on the retryable client
		retryClient.HTTPClient.Transport = cachedTransport
		retryClient.HTTPClient.Timeout = config.Timeout

		httpClient = retryClient.StandardClient()
	} else {
		// Use standard client without caching
		stdClient := retryClient.StandardClient()
		stdClient.Timeout = config.Timeout

		// Add compression if enabled
		if config.EnableCompression {
			baseTransport := stdClient.Transport
			if baseTransport == nil {
				baseTransport = http.DefaultTransport
			}
			stdClient.Transport = newCompressionTransport(baseTransport)
		}

		httpClient = stdClient
	}

	// Initialize circuit breaker if enabled
	var cb *circuitBreaker
	if config.EnableCircuitBreaker {
		cb = newCircuitBreaker(config.CircuitBreakerConfig)
	}

	return &Client{
		retryClient:    retryClient,
		httpClient:     httpClient,
		config:         config,
		cache:          cache,
		circuitBreaker: cb,
	}
}

// Do executes an HTTP request with retry logic, hooks, and circuit breaker support
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	// Validate request has a URL
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("request or request URL is nil")
	}

	// Check circuit breaker if enabled
	if c.circuitBreaker != nil {
		host := req.URL.Hostname()
		if err := c.circuitBreaker.allow(host); err != nil {
			return nil, err
		}
	}

	// Run request hooks
	for _, hook := range c.config.RequestHooks {
		if err := hook(req); err != nil {
			return nil, fmt.Errorf("request hook failed: %w", err)
		}
	}

	// Track concurrent requests
	metrics.HTTPClientConcurrentRequests.Inc()
	defer metrics.HTTPClientConcurrentRequests.Dec()

	// Convert to retryable request for retry logic
	retryReq, err := retryablehttp.FromRequest(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create retryable request: %w", err)
	}

	resp, err := c.retryClient.Do(retryReq)

	// Record circuit breaker result
	if c.circuitBreaker != nil {
		host := req.URL.Hostname()
		if err != nil || (resp != nil && resp.StatusCode >= 500) {
			c.circuitBreaker.recordFailure(host)
		} else if resp != nil {
			c.circuitBreaker.recordSuccess(host)
		}
	}

	// Run response hooks if we got a response
	if resp != nil {
		for _, hook := range c.config.ResponseHooks {
			if hookErr := hook(resp); hookErr != nil {
				// Close response body before returning hook error
				if resp.Body != nil {
					resp.Body.Close()
				}
				return nil, fmt.Errorf("response hook failed: %w", hookErr)
			}
		}
	}

	return resp, err
}

// Get performs a GET request
func (c *Client) Get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET request: %w", err)
	}

	return c.Do(req)
}

// Post performs a POST request
func (c *Client) Post(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create POST request: %w", err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return c.Do(req)
}

// Put performs a PUT request
func (c *Client) Put(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create PUT request: %w", err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return c.Do(req)
}

// Delete performs a DELETE request
func (c *Client) Delete(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DELETE request: %w", err)
	}

	return c.Do(req)
}

// StandardClient returns the underlying standard HTTP client (useful for external libraries)
func (c *Client) StandardClient() *http.Client {
	return c.httpClient
}

// RetryableClient returns the underlying retryable HTTP client
func (c *Client) RetryableClient() *retryablehttp.Client {
	return c.retryClient
}

// ClearCache clears all cached entries
// For filesystem cache, this also removes files from disk
func (c *Client) ClearCache() error {
	if c.cache == nil {
		return nil // No cache configured
	}

	// If it's a FileCache, use the Clear method
	if fc, ok := c.cache.(*FileCache); ok {
		return fc.Clear()
	}

	// For other cache types, we can't clear them directly
	// as they don't expose a Clear method
	return fmt.Errorf("cache clearing not supported for this cache type")
}

// CleanExpiredCache removes expired cache entries
// Only supported for filesystem cache
func (c *Client) CleanExpiredCache() error {
	if c.cache == nil {
		return nil // No cache configured
	}

	// Only FileCache supports cleaning expired entries
	if fc, ok := c.cache.(*FileCache); ok {
		return fc.CleanExpired()
	}

	return fmt.Errorf("cache cleanup not supported for this cache type")
}

// DeleteCacheEntry removes a specific entry from the cache by URL
func (c *Client) DeleteCacheEntry(ctx context.Context, url string) error {
	if c.cache == nil {
		return nil // No cache configured
	}

	// Use the URL as the cache key
	return c.cache.Del(ctx, url)
}

// WarmCache pre-populates the cache with the specified URLs
// This is useful for frequently accessed resources
func (c *Client) WarmCache(ctx context.Context, urls []string) error {
	if c.cache == nil {
		return fmt.Errorf("cache not enabled")
	}

	var errs []error
	for _, url := range urls {
		resp, err := c.Get(ctx, url)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to warm cache for %s: %w", url, err))
			continue
		}
		// Close the response body - the data is already cached
		if resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body) // Ensure body is fully read; ignore copy errors
			resp.Body.Close()
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("encountered %d errors while warming cache: %v", len(errs), errs)
	}

	return nil
}

// CacheStats returns cache hit and miss statistics with computed hit rate
// Returns nil if cache stats are not available
func (c *Client) CacheStats() *CacheStats {
	if c.cache == nil {
		return nil
	}

	var hits, misses uint64
	var ok bool

	// Check if cache supports stats
	if fc, isFileCache := c.cache.(*FileCache); isFileCache {
		hits, misses = fc.Stats()
		ok = true
	} else if mc, isMemCache := c.cache.(*metricsMemoryCache); isMemCache {
		hits, misses = mc.Stats()
		ok = true
	}

	if !ok {
		return nil
	}

	// Calculate hit rate
	total := hits + misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	return &CacheStats{
		Hits:    hits,
		Misses:  misses,
		HitRate: hitRate,
	}
}

// Close gracefully shuts down the client and cleans up resources
// For filesystem cache, this performs a cleanup of expired entries
func (c *Client) Close() error {
	if c.cache == nil {
		return nil
	}

	// If it's a FileCache, close it
	if fc, ok := c.cache.(*FileCache); ok {
		return fc.Close()
	}

	return nil
}

// wrapCheckRetryWithMetrics wraps a CheckRetry function to track retry metrics
func wrapCheckRetryWithMetrics(original retryablehttp.CheckRetry) retryablehttp.CheckRetry {
	return func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		shouldRetry := false
		var checkErr error

		if original != nil {
			shouldRetry, checkErr = original(ctx, resp, err)
		} else {
			shouldRetry, checkErr = retryablehttp.DefaultRetryPolicy(ctx, resp, err)
		}

		if shouldRetry {
			method := "unknown"
			host := "unknown"
			pathTemplate := "/"
			// Extract request info from response if available
			if resp != nil && resp.Request != nil {
				method = resp.Request.Method
				host, pathTemplate = extractMetricLabels(resp.Request.URL)
			}
			metrics.HTTPClientRetriesTotal.WithLabelValues(method, host, pathTemplate).Inc()
		}

		return shouldRetry, checkErr
	}
}

// retryableLogger adapts logr.Logger to retryablehttp.LeveledLogger interface
type retryableLogger struct {
	logger logr.Logger
}

func (l *retryableLogger) Error(msg string, keysAndValues ...interface{}) {
	l.logger.Error(nil, msg, keysAndValues...)
}

func (l *retryableLogger) Info(msg string, keysAndValues ...interface{}) {
	l.logger.Info(msg, keysAndValues...)
}

func (l *retryableLogger) Debug(msg string, keysAndValues ...interface{}) {
	l.logger.V(1).Info(msg, keysAndValues...)
}

func (l *retryableLogger) Warn(msg string, keysAndValues ...interface{}) {
	l.logger.V(0).Info(msg, keysAndValues...)
}
