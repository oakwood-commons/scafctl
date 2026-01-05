package httpc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/oakwood-commons/scafctl/pkg/metrics"
	"ivan.dev/httpcache"
)

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
	// Logger is the logger to use for the client
	Logger logr.Logger
	// CheckRetry is a custom retry policy function
	CheckRetry retryablehttp.CheckRetry
	// Backoff is a custom backoff policy function
	Backoff retryablehttp.Backoff
	// ErrorHandler is called if retries are exhausted
	ErrorHandler retryablehttp.ErrorHandler
}

// DefaultConfig returns a ClientConfig with sensible defaults
func DefaultConfig() *ClientConfig {
	return &ClientConfig{
		Timeout:      30 * time.Second,
		RetryMax:     3,
		RetryWaitMin: 1 * time.Second,
		RetryWaitMax: 30 * time.Second,
		EnableCache:  true,
		CacheType:    CacheTypeFilesystem,
		CacheDir:     "~/.scafctl/http-cache",
		CacheTTL:     10 * time.Minute,
		Logger:       logr.Discard(),
	}
}

// Client is an HTTP client with retry, timeout, and caching capabilities
type Client struct {
	retryClient *retryablehttp.Client
	httpClient  *http.Client
	config      *ClientConfig
	cache       httpcache.Cache // Store reference to cache for clearing
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
		retryClient.CheckRetry = config.CheckRetry
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
		var err error

		// Create cache based on type
		switch config.CacheType {
		case CacheTypeFilesystem:
			cacheDir := config.CacheDir
			if cacheDir == "" {
				cacheDir = "~/.scafctl/http-cache"
			}
			cache, err = NewFileCache(cacheDir, config.CacheTTL)
			if err != nil {
				// Fall back to memory cache if filesystem cache fails
				if config.Logger.GetSink() != nil {
					config.Logger.Error(err, "Failed to create filesystem cache, falling back to memory cache")
				}
				cache = httpcache.MemoryCache(1000, config.CacheTTL)
			}
		case CacheTypeMemory:
			fallthrough
		default:
			cache = httpcache.MemoryCache(1000, config.CacheTTL)
		}

		// Create a cached transport that wraps the retryable client's transport
		baseTransport := retryClient.HTTPClient.Transport
		if baseTransport == nil {
			baseTransport = http.DefaultTransport
		}

		cachedTransport := httpcache.NewCacheTransport(
			baseTransport,
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
		httpClient = stdClient
	}

	return &Client{
		retryClient: retryClient,
		httpClient:  httpClient,
		config:      config,
		cache:       cache,
	}
}

// Do executes an HTTP request with retry logic
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	start := time.Now()
	// Convert to retryable request for retry logic

	retryReq, err := retryablehttp.FromRequest(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create retryable request: %w", err)
	}

	resp, err := c.retryClient.Do(retryReq)
	duration := time.Since(start).Seconds()

	if resp != nil {
		metrics.HTTPClientCallsTimeHistogram.WithLabelValues(req.URL.String(), strconv.Itoa(resp.StatusCode)).Observe(duration)
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
