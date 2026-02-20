// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

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
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/metrics"
	"github.com/oakwood-commons/scafctl/pkg/settings"
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
	// OnUnauthorized is called when a 401 Unauthorized response is received.
	// Return the new full Authorization header value (e.g. "Bearer <new-token>") to
	// inject a single transparent retry with the refreshed token.
	// Return an empty string (or an error) to pass the 401 response through as-is.
	OnUnauthorized func(ctx context.Context) (authorizationHeader string, err error)
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
		Timeout:              settings.DefaultHTTPTimeout,
		RetryMax:             settings.DefaultHTTPRetryMax,
		RetryWaitMin:         settings.DefaultHTTPRetryWaitMinimum,
		RetryWaitMax:         settings.DefaultHTTPRetryWaitMaximum,
		EnableCache:          true,
		CacheType:            CacheTypeFilesystem,
		CacheDir:             settings.DefaultHTTPCacheDir(),
		CacheTTL:             settings.DefaultHTTPCacheTTL,
		CacheKeyPrefix:       settings.DefaultHTTPCacheKeyPrefix,
		MaxCacheFileSize:     settings.DefaultMaxCacheFileSize,
		MemoryCacheSize:      settings.DefaultMemoryCacheSize,
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
	} else {
		// Silence retryablehttp's default stdlib logger, which prints [DEBUG]/[ERROR]
		// directly to stderr when no logr sink is configured.
		retryClient.Logger = nil
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
		// Use standard client without caching.
		// Set the timeout on retryClient.HTTPClient so it applies to every individual
		// request attempt made by retryablehttp (not just to the top-level wrapper).
		retryClient.HTTPClient.Timeout = config.Timeout

		// Add compression to the underlying transport if enabled.
		if config.EnableCompression {
			baseTransport := retryClient.HTTPClient.Transport
			if baseTransport == nil {
				baseTransport = http.DefaultTransport
			}
			retryClient.HTTPClient.Transport = newCompressionTransport(baseTransport)
		}

		// StandardClient wraps retryablehttp as an http.RoundTripper; also set
		// Timeout here for callers who obtain it via StandardClient().
		stdClient := retryClient.StandardClient()
		stdClient.Timeout = config.Timeout

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

	// Handle 401 Unauthorized with optional token refresh (single retry).
	// This runs after the retryablehttp layer has already exhausted its own retries.
	if err == nil && resp != nil && resp.StatusCode == http.StatusUnauthorized && c.config.OnUnauthorized != nil {
		reqCtx := req.Context()
		newAuthHeader, hookErr := c.config.OnUnauthorized(reqCtx)
		if hookErr == nil && newAuthHeader != "" {
			// Drain and discard the 401 response body before re-using the connection.
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()

			// Clone the original request and inject the refreshed credential.
			retryReq := req.Clone(reqCtx)
			retryReq.Header.Set("Authorization", newAuthHeader)
			// Replay the request body if the caller provided a GetBody func.
			if req.GetBody != nil {
				retryReq.Body, _ = req.GetBody()
			}

			// Execute once through the raw httpClient (bypass retryable layer for this single auth retry).
			resp, err = c.httpClient.Do(retryReq) //nolint:gosec // request cloned from caller-supplied req

			// Run response hooks on the retried response.
			if err == nil && resp != nil {
				for _, hook := range c.config.ResponseHooks {
					if hookErr2 := hook(resp); hookErr2 != nil {
						if resp.Body != nil {
							resp.Body.Close()
						}
						return nil, fmt.Errorf("response hook failed after auth retry: %w", hookErr2)
					}
				}
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

// NewClientFromAppConfig creates a new HTTP client using the application configuration.
// The cfg parameter can be nil, in which case defaults are used.
// Duration strings must be pre-validated (config.Validate should be called first).
func NewClientFromAppConfig(cfg *config.HTTPClientConfig, logger logr.Logger) *Client {
	clientCfg := DefaultConfig()
	clientCfg.Logger = logger

	if cfg == nil {
		return NewClient(clientCfg)
	}

	// Apply timeout (already validated)
	if cfg.Timeout != "" {
		clientCfg.Timeout, _ = time.ParseDuration(cfg.Timeout)
	}

	// Apply retry settings
	if cfg.RetryMax > 0 {
		clientCfg.RetryMax = cfg.RetryMax
	}
	if cfg.RetryWaitMin != "" {
		clientCfg.RetryWaitMin, _ = time.ParseDuration(cfg.RetryWaitMin)
	}
	if cfg.RetryWaitMax != "" {
		clientCfg.RetryWaitMax, _ = time.ParseDuration(cfg.RetryWaitMax)
	}

	// Apply cache settings
	if cfg.EnableCache != nil {
		clientCfg.EnableCache = *cfg.EnableCache
	}
	if cfg.CacheType != "" {
		clientCfg.CacheType = CacheType(cfg.CacheType)
	}
	if cfg.CacheDir != "" {
		clientCfg.CacheDir = cfg.CacheDir
	}
	if cfg.CacheTTL != "" {
		clientCfg.CacheTTL, _ = time.ParseDuration(cfg.CacheTTL)
	}
	if cfg.CacheKeyPrefix != "" {
		clientCfg.CacheKeyPrefix = cfg.CacheKeyPrefix
	}
	if cfg.MaxCacheFileSize > 0 {
		clientCfg.MaxCacheFileSize = cfg.MaxCacheFileSize
	}
	if cfg.MemoryCacheSize > 0 {
		clientCfg.MemoryCacheSize = cfg.MemoryCacheSize
	}

	// Apply circuit breaker settings
	if cfg.EnableCircuitBreaker != nil {
		clientCfg.EnableCircuitBreaker = *cfg.EnableCircuitBreaker
	}
	if cfg.CircuitBreakerMaxFailures > 0 || cfg.CircuitBreakerOpenTimeout != "" || cfg.CircuitBreakerHalfOpenMaxRequests > 0 {
		clientCfg.CircuitBreakerConfig = DefaultCircuitBreakerConfig()
		if cfg.CircuitBreakerMaxFailures > 0 {
			clientCfg.CircuitBreakerConfig.MaxFailures = cfg.CircuitBreakerMaxFailures
		}
		if cfg.CircuitBreakerOpenTimeout != "" {
			clientCfg.CircuitBreakerConfig.OpenTimeout, _ = time.ParseDuration(cfg.CircuitBreakerOpenTimeout)
		}
		if cfg.CircuitBreakerHalfOpenMaxRequests > 0 {
			clientCfg.CircuitBreakerConfig.HalfOpenMaxRequests = cfg.CircuitBreakerHalfOpenMaxRequests
		}
	}

	// Apply compression setting
	if cfg.EnableCompression != nil {
		clientCfg.EnableCompression = *cfg.EnableCompression
	}

	return NewClient(clientCfg)
}

// MergeHTTPClientConfig merges a per-catalog config with the global config.
// Per-catalog values override global values when set.
// Returns the global config if perCatalog is nil.
func MergeHTTPClientConfig(global, perCatalog *config.HTTPClientConfig) *config.HTTPClientConfig {
	if perCatalog == nil {
		return global
	}
	if global == nil {
		return perCatalog
	}

	// Start with a copy of global
	merged := *global

	// Override with per-catalog values if set
	if perCatalog.Timeout != "" {
		merged.Timeout = perCatalog.Timeout
	}
	if perCatalog.RetryMax > 0 {
		merged.RetryMax = perCatalog.RetryMax
	}
	if perCatalog.RetryWaitMin != "" {
		merged.RetryWaitMin = perCatalog.RetryWaitMin
	}
	if perCatalog.RetryWaitMax != "" {
		merged.RetryWaitMax = perCatalog.RetryWaitMax
	}
	if perCatalog.EnableCache != nil {
		merged.EnableCache = perCatalog.EnableCache
	}
	if perCatalog.CacheType != "" {
		merged.CacheType = perCatalog.CacheType
	}
	if perCatalog.CacheDir != "" {
		merged.CacheDir = perCatalog.CacheDir
	}
	if perCatalog.CacheTTL != "" {
		merged.CacheTTL = perCatalog.CacheTTL
	}
	if perCatalog.CacheKeyPrefix != "" {
		merged.CacheKeyPrefix = perCatalog.CacheKeyPrefix
	}
	if perCatalog.MaxCacheFileSize > 0 {
		merged.MaxCacheFileSize = perCatalog.MaxCacheFileSize
	}
	if perCatalog.MemoryCacheSize > 0 {
		merged.MemoryCacheSize = perCatalog.MemoryCacheSize
	}
	if perCatalog.EnableCircuitBreaker != nil {
		merged.EnableCircuitBreaker = perCatalog.EnableCircuitBreaker
	}
	if perCatalog.CircuitBreakerMaxFailures > 0 {
		merged.CircuitBreakerMaxFailures = perCatalog.CircuitBreakerMaxFailures
	}
	if perCatalog.CircuitBreakerOpenTimeout != "" {
		merged.CircuitBreakerOpenTimeout = perCatalog.CircuitBreakerOpenTimeout
	}
	if perCatalog.CircuitBreakerHalfOpenMaxRequests > 0 {
		merged.CircuitBreakerHalfOpenMaxRequests = perCatalog.CircuitBreakerHalfOpenMaxRequests
	}
	if perCatalog.EnableCompression != nil {
		merged.EnableCompression = perCatalog.EnableCompression
	}

	return &merged
}
