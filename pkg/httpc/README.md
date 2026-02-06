# HTTP Client Package

A robust HTTP client for Go with built-in support for retries, timeouts, and caching.

## Features

- **Automatic Retries**: Uses `github.com/hashicorp/go-retryablehttp` for intelligent retry logic
- **HTTP Caching**: Leverages `ivan.dev/httpcache` for efficient response caching (memory or filesystem)
- **Cache Warming**: Pre-populate cache with frequently accessed URLs
- **Circuit Breaker**: Prevent cascading failures with configurable circuit breaker per host
- **Request/Response Hooks**: Middleware-style processing for requests and responses
- **Automatic Compression**: Built-in gzip/deflate support for requests and responses
- **Configurable Timeouts**: Set custom timeouts for all requests
- **Flexible Retry Policies**: Customize retry behavior with custom policies
- **Structured Logging**: Integrates with `logr.Logger` for consistent logging
- **Context Support**: Full support for context-based cancellation and timeouts
- **Comprehensive Metrics**: Prometheus metrics for requests, cache, errors, retries, sizes, and concurrent requests
- **Thread-Safe**: All operations are safe for concurrent use by multiple goroutines

## Thread Safety

The HTTP client is **fully thread-safe** and designed for concurrent use:

- **Client**: Multiple goroutines can safely share a single `Client` instance. All methods (`Get`, `Post`, `Put`, `Delete`, `Do`) can be called concurrently without additional synchronization.
- **FileCache**: Concurrent cache operations are safe within a single process. Uses atomic operations for statistics and atomic file operations (write-then-rename) for data integrity.
- **MemoryCache**: Fully thread-safe with atomic statistics tracking and concurrent-safe underlying cache implementation.
- **Circuit Breaker**: All state transitions are protected by mutexes and safe for concurrent access.

**Note**: FileCache is safe for concurrent goroutines within a single process, but if multiple processes access the same cache directory, filesystem race conditions may occur.

## Installation

```bash
go get github.com/oakwood-commons/scafctl/pkg/httpc
```

## Quick Start

### Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "io"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

func main() {
    // Create a client with default settings
    client := httpc.NewClient(nil)
    
    // Make a GET request
    ctx := context.Background()
    resp, err := client.Get(ctx, "https://api.github.com/zen")
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()
    
    body, _ := io.ReadAll(resp.Body)
    fmt.Println(string(body))
}
```

### Custom Configuration

```go
config := &httpc.ClientConfig{
    Timeout:      10 * time.Second,
    RetryMax:     5,
    RetryWaitMin: 500 * time.Millisecond,
    RetryWaitMax: 10 * time.Second,
    EnableCache:  true,
    CacheTTL:     15 * time.Minute,
    Logger:       yourLogger, // logr.Logger instance
}

client := httpc.NewClient(config)
```

### From Application Config

The HTTP client can be initialized directly from the application configuration file (`~/.scafctl/config.yaml`):

```go
import (
    "github.com/oakwood-commons/scafctl/pkg/config"
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

// Load application config
mgr := config.NewManager("")
cfg, err := mgr.Load()
if err != nil {
    // handle error
}

// Create HTTP client from config
client := httpc.NewClientFromAppConfig(&cfg.HTTPClient, logger)
```

**Config file example** (`~/.scafctl/config.yaml`):

```yaml
version: 1

httpClient:
  timeout: "60s"
  retryMax: 5
  retryWaitMin: "2s"
  retryWaitMax: "60s"
  enableCache: true
  cacheType: "filesystem"
  cacheDir: "~/.scafctl/http-cache"
  cacheTTL: "30m"
  enableCircuitBreaker: true
  circuitBreakerMaxFailures: 3
  circuitBreakerOpenTimeout: "1m"
  circuitBreakerHalfOpenMaxRequests: 2
  enableCompression: true
```

All config values can be overridden via environment variables with the `SCAFCTL_` prefix:

```bash
export SCAFCTL_HTTPCLIENT_TIMEOUT=120s
export SCAFCTL_HTTPCLIENT_RETRYMAX=10
```

### Per-Catalog Configuration

For catalogs that require different HTTP settings, you can use `MergeHTTPClientConfig`:

```go
// Get global config
globalHTTPConfig := &cfg.HTTPClient

// Get per-catalog config (may be nil)
catalog, _ := cfg.GetCatalog("slow-api")
perCatalogConfig := catalog.HTTPClient

// Merge: per-catalog values override global values
mergedConfig := httpc.MergeHTTPClientConfig(globalHTTPConfig, perCatalogConfig)

// Create client with merged config
client := httpc.NewClientFromAppConfig(mergedConfig, logger)
```

**Config file example with per-catalog overrides**:

```yaml
version: 1

httpClient:
  timeout: "30s"
  retryMax: 3

catalogs:
  - name: slow-api
    type: http
    url: https://slow-api.example.com
    httpClient:
      timeout: "120s"  # Override for this catalog only
      retryMax: 10
```

## Configuration Options

### ClientConfig Fields

| Field | Type | Default | Description |
| ----- | ---- | ------- | ----------- |
| `Timeout` | `time.Duration` | `30s` | Maximum time to wait for a request |
| `RetryMax` | `int` | `3` | Maximum number of retries |
| `RetryWaitMin` | `time.Duration` | `1s` | Minimum wait time between retries |
| `RetryWaitMax` | `time.Duration` | `30s` | Maximum wait time between retries |
| `EnableCache` | `bool` | `true` | Enable HTTP response caching |
| `CacheType` | `CacheType` | `filesystem` | Type of cache: `memory` or `filesystem` |
| `CacheDir` | `string` | `~/.scafctl/http-cache` | Directory for filesystem cache (only used when CacheType is `filesystem`) |
| `CacheTTL` | `time.Duration` | `10m` | Time-to-live for cached responses |
| `CacheKeyPrefix` | `string` | `scafctl:` | Prefix for cache keys to prevent collisions |
| `MaxCacheFileSize` | `int64` | `10MB` | Maximum size for a single cached file (filesystem cache only) |
| `MemoryCacheSize` | `int` | `1000` | Maximum number of entries in memory cache |
| `EnableCircuitBreaker` | `bool` | `false` | Enable circuit breaker pattern for failure protection |
| `CircuitBreakerConfig` | `*CircuitBreakerConfig` | See below | Circuit breaker configuration |
| `EnableCompression` | `bool` | `true` | Enable automatic gzip/deflate compression |
| `RequestHooks` | `[]RequestHook` | `nil` | Functions called before each request |
| `ResponseHooks` | `[]ResponseHook` | `nil` | Functions called after each response |
| `Logger` | `logr.Logger` | Discard | Logger for client operations |
| `CheckRetry` | `retryablehttp.CheckRetry` | `nil` | Custom retry policy function |
| `Backoff` | `retryablehttp.Backoff` | `nil` | Custom backoff policy function |
| `ErrorHandler` | `retryablehttp.ErrorHandler` | `nil` | Called if retries are exhausted |

### CircuitBreakerConfig Fields

| Field | Type | Default | Description |
| ----- | ---- | ------- | ----------- |
| `MaxFailures` | `int` | `5` | Number of consecutive failures before opening circuit |
| `OpenTimeout` | `time.Duration` | `30s` | Time to wait before transitioning from Open to HalfOpen |
| `HalfOpenMaxRequests` | `int` | `1` | Number of successful requests in HalfOpen before closing |

## API Methods

### GET Request

```go
resp, err := client.Get(ctx, "https://api.example.com/data")
```

### POST Request

```go
body := strings.NewReader(`{"key": "value"}`)
resp, err := client.Post(ctx, "https://api.example.com/data", "application/json", body)
```

### PUT Request

```go
body := strings.NewReader(`{"key": "updated"}`)
resp, err := client.Put(ctx, "https://api.example.com/data/1", "application/json", body)
```

### DELETE Request

```go
resp, err := client.Delete(ctx, "https://api.example.com/data/1")
```

### Custom Request

```go
req, _ := http.NewRequestWithContext(ctx, "PATCH", "https://api.example.com/data/1", body)
req.Header.Set("Authorization", "Bearer token")
resp, err := client.Do(req)
```

## Advanced Usage

### Custom Retry Policy

Only retry on specific HTTP status codes:

```go
config := httpc.DefaultConfig()
config.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
    // Always retry on connection errors
    if err != nil {
        return true, nil
    }
    
    // Only retry on rate limits and service unavailable
    if resp.StatusCode == 429 || resp.StatusCode == 503 {
        return true, nil
    }
    
    return false, nil
}

client := httpc.NewClient(config)
```

### Custom Backoff Strategy

```go
config := httpc.DefaultConfig()
config.Backoff = func(min, max time.Duration, attemptNum int, resp *http.Response) time.Duration {
    // Exponential backoff
    return min * time.Duration(math.Pow(2, float64(attemptNum)))
}

client := httpc.NewClient(config)
```

### Disable Caching

```go
config := httpc.DefaultConfig()
config.EnableCache = false

client := httpc.NewClient(config)
```

### Access Underlying Clients

```go
// Get the standard HTTP client (useful for external libraries)
stdClient := client.StandardClient()

// Get the retryable HTTP client (for advanced configuration)
retryClient := client.RetryableClient()
```

### Circuit Breaker

Enable circuit breaker to prevent cascading failures:

```go
config := httpc.DefaultConfig()
config.EnableCircuitBreaker = true
config.CircuitBreakerConfig = &httpc.CircuitBreakerConfig{
    MaxFailures:         5,        // Open circuit after 5 consecutive failures
    OpenTimeout:         30 * time.Second, // Wait 30s before attempting half-open
    HalfOpenMaxRequests: 2,        // Require 2 successes to close circuit
}

client := httpc.NewClient(config)
```

When the circuit is open, requests will immediately fail with `httpc.ErrCircuitBreakerOpen`.

### Request and Response Hooks

Add middleware-style processing to requests and responses:

```go
config := httpc.DefaultConfig()

// Add authentication header to all requests
config.RequestHooks = []httpc.RequestHook{
    func(req *http.Request) error {
        req.Header.Set("Authorization", "Bearer "+getToken())
        return nil
    },
}

// Log response headers
config.ResponseHooks = []httpc.ResponseHook{
    func(resp *http.Response) error {
        log.Printf("Response status: %d", resp.StatusCode)
        return nil
    },
}

client := httpc.NewClient(config)
```

Hooks are executed in order. If a hook returns an error, the request/response chain is aborted.

### Compression

Compression is enabled by default. Disable it if needed:

```go
config := httpc.DefaultConfig()
config.EnableCompression = false  // Disable automatic compression

client := httpc.NewClient(config)
```

When enabled, the client:

- Automatically adds `Accept-Encoding: gzip, deflate` headers
- Decompresses gzip responses transparently
- Handles both compressed and uncompressed responses

### Cache Management

The client provides methods for managing the cache:

```go
// Warm cache with frequently accessed URLs
urls := []string{
    "https://api.example.com/config",
    "https://api.example.com/metadata",
}
err := client.WarmCache(ctx, urls)

// Clear all cached entries (filesystem cache only)
err := client.ClearCache()

// Clean expired cache entries (filesystem cache only)
err := client.CleanExpiredCache()

// Delete a specific cache entry by URL
err := client.DeleteCacheEntry(ctx, "https://api.example.com/data")

// Get cache statistics with hit rate
stats := client.CacheStats()
if stats != nil {
    fmt.Printf("Hits: %d, Misses: %d, Hit Rate: %.2f%%\n", 
        stats.Hits, stats.Misses, stats.HitRate*100)
}
```

**Note:** `ClearCache()` and `CleanExpiredCache()` are only supported for filesystem cache. Memory cache doesn't expose these operations due to the underlying implementation.

## Caching Behavior

The client supports two types of caching:

### Memory Cache

Uses an in-memory LFU (Least Frequently Used) cache:

- Fast and efficient
- Configurable size (default: 1000 entries)
- Cache is lost when the application restarts

```go
config := httpc.DefaultConfig()
config.EnableCache = true
config.CacheType = httpc.CacheTypeMemory
config.MemoryCacheSize = 5000 // Custom size
client := httpc.NewClient(config)
```

### Filesystem Cache

Stores cache entries on disk for persistence across restarts:

- Cache survives application restarts
- Configurable cache directory
- Defaults to `~/.scafctl/http-cache`

```go
config := httpc.DefaultConfig()
config.EnableCache = true
config.CacheType = httpc.CacheTypeFilesystem
config.CacheDir = "~/.myapp/http-cache" // Optional, uses default if not set
client := httpc.NewClient(config)
```

### Cache Behavior

Both cache types:

- Respect HTTP cache headers (`Cache-Control`, `Expires`, etc.)
- Apply the configured TTL to cached entries
- Only cache GET requests by default
- Use SHA-256 hashing for cache keys

### Cache Headers

The cache respects standard HTTP caching headers:

- **Cache-Control**: Directives like `max-age`, `no-cache`, `no-store`, `must-revalidate`
- **Expires**: Explicit expiration date/time
- **ETag**: Entity tags for conditional requests
- **Last-Modified**: Timestamp for conditional requests

The cache will honor these headers in combination with the configured TTL. For example:

- If `Cache-Control: no-cache` is present, the response won't be cached
- If `Cache-Control: max-age=3600` is present, it will be cached for 1 hour (or the configured TTL, whichever is shorter)
- If no cache headers are present, the configured TTL is used

## Prometheus Metrics

The HTTP client automatically collects comprehensive Prometheus metrics for monitoring and observability.

### Available Metrics

| Metric | Type | Labels | Description |
| ------ | ---- | ------ | ----------- |
| `scafctl_http_client_duration_seconds` | Histogram | method, host, path_template, status_code | Request duration in seconds |
| `scafctl_http_client_requests_total` | Counter | method, host, path_template, status_code | Total number of requests |
| `scafctl_http_client_errors_total` | Counter | method, host, path_template, error_type | Total number of errors by type |
| `scafctl_http_client_retries_total` | Counter | method, host, path_template | Total number of retry attempts |
| `scafctl_http_client_cache_hits_total` | Gauge | - | Total cache hits |
| `scafctl_http_client_cache_misses_total` | Gauge | - | Total cache misses |
| `scafctl_http_client_request_size_bytes` | Histogram | method, host, path_template | Request body size in bytes |
| `scafctl_http_client_response_size_bytes` | Histogram | method, host, path_template | Response body size in bytes |
| `scafctl_http_client_cache_size_bytes` | Gauge | - | Total size of cached data |
| `scafctl_http_client_concurrent_requests` | Gauge | - | Current number of concurrent requests |
| `scafctl_http_client_circuit_breaker_state` | Gauge | host | Circuit breaker state (0=closed, 1=open, 2=half-open) |

### Metric Labels

- **`method`**: HTTP method (GET, POST, PUT, DELETE, etc.)
- **`host`**: Hostname with non-standard ports (e.g., `api.github.com`, `localhost:8080`)
- **`path_template`**: Parameterized path with dynamic segments replaced (e.g., `/api/users/{id}`)
- **`status_code`**: HTTP response status code or "error" for failed requests
- **`error_type`**: Categorized error type (see Error Categorization below)

Paths are automatically parameterized using Tier 1 patterns to maintain bounded cardinality. See [Label Cardinality](#label-cardinality) for details.

### Error Categorization

Errors are automatically categorized into the following types for the `error_type` label:

- `client_error` - HTTP 4xx responses
- `server_error` - HTTP 5xx responses
- `context_canceled` - Request canceled via context
- `context_timeout` - Request timeout (context deadline exceeded)
- `network_timeout` - Network-level timeout
- `connection_refused` - Connection refused by server
- `dns_error` - DNS resolution failure
- `unknown` - Other errors

### Metrics Collection

Metrics are collected automatically for all requests:

```go
import (
    "github.com/oakwood-commons/scafctl/pkg/httpc"
    "github.com/oakwood-commons/scafctl/pkg/metrics"
)

func main() {
    // Register metrics (typically done once at application startup)
    metrics.RegisterMetrics()
    
    // Create client - metrics are collected automatically
    client := httpc.NewClient(nil)
    
    // All requests will have metrics recorded
    resp, err := client.Get(ctx, "https://api.example.com/data")
}
```

### Cache Statistics

Cache hit/miss statistics are updated in real-time and can be queried:

```go
hits, misses, ok := client.CacheStats()
if ok {
    hitRate := float64(hits) / float64(hits + misses) * 100
    fmt.Printf("Cache hit rate: %.2f%%\n", hitRate)
}
```

Both FileCache and MemoryCache support statistics tracking with thread-safe atomic operations.

## Retry Behavior

The client automatically retries requests based on:

- Connection errors
- 5xx server errors (500-599)
- 429 Too Many Requests

Retry behavior can be customized with the `CheckRetry` configuration option.

### Default Retry Strategy

- Maximum retries: 3
- Minimum wait: 1 second
- Maximum wait: 30 seconds
- Exponential backoff with jitter

**Note:** Retry attempts are tracked in the `http_client_retries_total` metric.

## Testing

```bash
go test ./pkg/httpc/...
```

## Complete Examples

### Example 1: Basic GET Request

```go
package main

import (
    "context"
    "fmt"
    "io"
    "log"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
    "github.com/go-logr/logr"
)

func main() {
    // Create a client with default settings
    client := httpc.NewClient(nil)
    
    // Make a GET request
    ctx := context.Background()
    resp, err := client.Get(ctx, "https://api.github.com/zen")
    if err != nil {
        log.Fatalf("failed to get zen message: %v", err)
    }
    defer resp.Body.Close()
    
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        log.Fatalf("failed to read response body: %v", err)
    }
    
    fmt.Println("Response:", string(body))
}
```

### Example 2: Custom Configuration

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
    "github.com/go-logr/logr"
)

func main() {
    config := &httpc.ClientConfig{
        Timeout:      10 * time.Second,
        RetryMax:     5,
        RetryWaitMin: 500 * time.Millisecond,
        RetryWaitMax: 10 * time.Second,
        EnableCache:  true,
        CacheTTL:     15 * time.Minute,
        Logger:       logr.Discard(),
    }
    
    client := httpc.NewClient(config)
    
    ctx := context.Background()
    resp, err := client.Get(ctx, "https://httpbin.org/get")
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    
    fmt.Println("Status:", resp.StatusCode)
}
```

### Example 3: Automatic Retry Behavior

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

func main() {
    config := httpc.DefaultConfig()
    config.RetryMax = 3
    config.RetryWaitMin = 1 * time.Second
    config.RetryWaitMax = 5 * time.Second
    
    client := httpc.NewClient(config)
    
    ctx := context.Background()
    // This will automatically retry on failures
    resp, err := client.Get(ctx, "https://httpbin.org/status/500")
    if err != nil {
        // After retries are exhausted
        fmt.Println("Request failed after retries:", err)
        return
    }
    defer resp.Body.Close()
    
    fmt.Println("Status:", resp.StatusCode)
}
```

### Example 4: Caching Behavior

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

func main() {
    config := httpc.DefaultConfig()
    config.EnableCache = true
    config.CacheTTL = 5 * time.Minute
    
    client := httpc.NewClient(config)
    
    ctx := context.Background()
    
    // First request - hits the server
    resp1, err := client.Get(ctx, "https://httpbin.org/cache/60")
    if err != nil {
        log.Fatal(err)
    }
    resp1.Body.Close()
    fmt.Println("First request completed")
    
    // Second request - may be served from cache
    resp2, err := client.Get(ctx, "https://httpbin.org/cache/60")
    if err != nil {
        log.Fatal(err)
    }
    resp2.Body.Close()
    fmt.Println("Second request completed (potentially from cache)")
}
```

### Example 5: Custom Retry Policy

```go
package main

import (
    "context"
    "fmt"
    "net/http"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

func main() {
    config := httpc.DefaultConfig()
    
    // Custom retry policy that only retries on specific status codes
    config.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
        // Always retry on connection errors
        if err != nil {
            return true, nil
        }
        
        // Only retry on 429 (Too Many Requests) and 503 (Service Unavailable)
        if resp.StatusCode == 429 || resp.StatusCode == 503 {
            return true, nil
        }
        
        return false, nil
    }
    
    client := httpc.NewClient(config)
    
    ctx := context.Background()
    resp, err := client.Get(ctx, "https://httpbin.org/status/503")
    if err != nil {
        fmt.Println("Request failed:", err)
        return
    }
    defer resp.Body.Close()
    
    fmt.Println("Status:", resp.StatusCode)
}
```

### Example 6: Disable Caching

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

func main() {
    config := httpc.DefaultConfig()
    config.EnableCache = false
    
    client := httpc.NewClient(config)
    
    ctx := context.Background()
    resp, err := client.Get(ctx, "https://httpbin.org/get")
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    
    fmt.Println("Status:", resp.StatusCode)
}
```

### Example 7: POST Request

```go
package main

import (
    "context"
    "fmt"
    "log"
    "strings"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

func main() {
    client := httpc.NewClient(nil)
    
    ctx := context.Background()
    body := strings.NewReader(`{"key": "value"}`)
    resp, err := client.Post(ctx, "https://httpbin.org/post", "application/json", body)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    
    fmt.Println("Status:", resp.StatusCode)
}
```

### Example 8: Prometheus Metrics Integration

```go
package main

import (
    "context"
    "fmt"
    "net/http"
    "time"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
    "github.com/oakwood-commons/scafctl/pkg/metrics"
)

func main() {
    // Register Prometheus metrics
    metrics.RegisterMetrics()
    
    // Start metrics server
    go func() {
        http.ListenAndServe(":9090", nil)
    }()
    
    // Create HTTP client - all requests will be tracked
    config := httpc.DefaultConfig()
    config.EnableCache = true
    client := httpc.NewClient(config)
    
    ctx := context.Background()
    
    // Make some requests - metrics are collected automatically
    urls := []string{
        "https://httpbin.org/get",
        "https://httpbin.org/delay/2",
        "https://httpbin.org/status/500",
    }
    
    for _, url := range urls {
        resp, err := client.Get(ctx, url)
        if err != nil {
            fmt.Printf("Request failed: %v\n", err)
            continue
        }
        resp.Body.Close()
        fmt.Printf("Completed: %s (status: %d)\n", url, resp.StatusCode)
    }
    
    // Check cache statistics
    stats := client.CacheStats()
    if stats != nil {
        fmt.Printf("\nCache Statistics:\n")
        fmt.Printf("  Hits: %d\n", stats.Hits)
        fmt.Printf("  Misses: %d\n", stats.Misses)
        fmt.Printf("  Hit Rate: %.2f%%\n", stats.HitRate*100)
    }
    
    fmt.Println("\nMetrics available at http://localhost:9090/metrics")
    time.Sleep(1 * time.Minute)
}
```

### Example: Authentication with Bearer Token

```go
package main

import (
    "context"
    "fmt"
    "net/http"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

func main() {
    config := httpc.DefaultConfig()
    
    // Add authentication to all requests
    config.RequestHooks = []httpc.RequestHook{
        func(req *http.Request) error {
            token := "your-api-token-here"
            req.Header.Set("Authorization", "Bearer "+token)
            return nil
        },
    }
    
    client := httpc.NewClient(config)
    ctx := context.Background()
    
    resp, err := client.Get(ctx, "https://api.github.com/user")
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()
    
    fmt.Println("Authenticated request status:", resp.StatusCode)
}
```

### Example: Rate Limiting with Response Hooks

```go
package main

import (
    "context"
    "fmt"
    "net/http"
    "strconv"
    "time"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

func main() {
    config := httpc.DefaultConfig()
    
    // Track rate limit headers
    config.ResponseHooks = []httpc.ResponseHook{
        func(resp *http.Response) error {
            if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
                if count, err := strconv.Atoi(remaining); err == nil && count < 10 {
                    fmt.Printf("Warning: Only %d requests remaining\n", count)
                }
            }
            
            if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
                fmt.Printf("Rate limit resets at: %s\n", reset)
            }
            
            return nil
        },
    }
    
    client := httpc.NewClient(config)
    ctx := context.Background()
    
    resp, err := client.Get(ctx, "https://api.github.com/rate_limit")
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()
}
```

### Example: Custom Error Handling

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "net/http"
    
    "github.com/hashicorp/go-retryablehttp"
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

func main() {
    config := httpc.DefaultConfig()
    
    // Custom retry policy: only retry on 503 Service Unavailable
    config.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
        if err != nil {
            return true, nil // Always retry on connection errors
        }
        
        if resp.StatusCode == 503 {
            return true, nil // Retry on service unavailable
        }
        
        return false, nil // Don't retry on other errors
    }
    
    // Custom error handler called when retries are exhausted
    config.ErrorHandler = func(resp *http.Response, err error, numTries int) (*http.Response, error) {
        if err != nil {
            return resp, fmt.Errorf("request failed after %d attempts: %w", numTries, err)
        }
        if resp != nil && resp.StatusCode >= 500 {
            return resp, fmt.Errorf("server error after %d attempts: status %d", numTries, resp.StatusCode)
        }
        return resp, nil
    }
    
    client := httpc.NewClient(config)
    ctx := context.Background()
    
    resp, err := client.Get(ctx, "https://httpbin.org/status/503")
    if err != nil {
        fmt.Println("Error:", err)
        return
    }
    defer resp.Body.Close()
}
```

### Example: Timeout and Context Cancellation

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

func main() {
    config := httpc.DefaultConfig()
    config.Timeout = 5 * time.Second // Overall client timeout
    
    client := httpc.NewClient(config)
    
    // Create a context with a 2-second timeout (stricter than client timeout)
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    
    // This request will timeout after 2 seconds due to context
    resp, err := client.Get(ctx, "https://httpbin.org/delay/10")
    if err != nil {
        fmt.Println("Request cancelled:", err)
        return
    }
    defer resp.Body.Close()
}
```

### Example: Concurrent Requests with Shared Client

```go
package main

import (
    "context"
    "fmt"
    "sync"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

func main() {
    // Single client instance shared across goroutines
    client := httpc.NewClient(httpc.DefaultConfig())
    ctx := context.Background()
    
    urls := []string{
        "https://api.github.com/users/golang",
        "https://api.github.com/users/kubernetes",
        "https://api.github.com/users/docker",
    }
    
    var wg sync.WaitGroup
    for _, url := range urls {
        wg.Add(1)
        go func(u string) {
            defer wg.Done()
            
            resp, err := client.Get(ctx, u)
            if err != nil {
                fmt.Printf("Failed %s: %v\n", u, err)
                return
            }
            defer resp.Body.Close()
            
            fmt.Printf("Success %s: %d\n", u, resp.StatusCode)
        }(url)
    }
    
    wg.Wait()
    
    // Check cache stats after concurrent requests
    if stats := client.CacheStats(); stats != nil {
        fmt.Printf("\nCache hit rate: %.2f%%\n", stats.HitRate*100)
    }
}
```

## Performance Considerations

### Metrics Overhead

The Prometheus metrics collection adds minimal overhead:

- Duration tracking: ~10-50 nanoseconds per request
- Counter increments: ~5-10 nanoseconds per operation
- Atomic cache operations: ~10-20 nanoseconds per cache access

Total overhead is typically less than 0.1% of request time for most workloads.

### Label Cardinality

The client uses **parameterized URLs** to maintain bounded cardinality while preserving route-level observability. This is critical for production deployments with diverse URL patterns.

#### Metric Label Structure

All HTTP client metrics use the following labels:

- `method` - HTTP method (GET, POST, PUT, DELETE, etc.)
- `host` - Hostname with non-standard ports (e.g., `api.github.com`, `localhost:8080`)
- `path_template` - Parameterized path with dynamic segments replaced by placeholders
- `status_code` - HTTP response status code (or "error" for failed requests)
- `error_type` - Type of error (only for `http_client_errors_total`)

#### Path Parameterization (Tier 1 Patterns)

The client automatically applies **Tier 1 parameterization patterns** to URL paths, replacing dynamic segments with bounded placeholders:

1. **UUIDs** → `{id}`

   ```text
   /api/users/550e8400-e29b-41d4-a716-446655440000 → /api/users/{id}
   ```

2. **Integer IDs** → `{id}`

   ```text
   /api/users/123/posts/456 → /api/users/{id}/posts/{id}
   ```

3. **SHA Hashes (40-64 chars)** → `{hash}`

   ```text
   /repos/owner/repo/commits/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b → /repos/owner/repo/commits/{hash}
   ```

#### Port Handling

- **Default ports omitted**: `:80` for HTTP and `:443` for HTTPS are stripped
- **Non-standard ports included**: `localhost:8080`, `api.example.com:3000`

#### Query Parameters

All query parameters are **automatically stripped** from metric labels to prevent cardinality explosion:

```text
https://api.example.com/users?page=1&limit=10 → host: api.example.com, path: /users
```

#### Benefits

✅ **Bounded cardinality**: Limited to unique route patterns rather than infinite URL variations  
✅ **Route-level visibility**: Track performance and errors for each API endpoint  
✅ **Production-ready**: Suitable for high-traffic services, not just CLI tools  
✅ **OpenTelemetry-aligned**: Follows semantic conventions from OpenTelemetry standard

#### Cardinality Impact

**Before parameterization** (problematic):

```text
scafctl_http_client_duration_seconds{method="GET",url="https://api.github.com/users/1",status_code="200"}
scafctl_http_client_duration_seconds{method="GET",url="https://api.github.com/users/2",status_code="200"}
...
scafctl_http_client_duration_seconds{method="GET",url="https://api.github.com/users/999999",status_code="200"}
```

Result: **Unbounded time series** (999,999+ unique metric entries)

**After parameterization** (optimal):

```text
scafctl_http_client_duration_seconds{method="GET",host="api.github.com",path_template="/users/{id}",status_code="200"}
```

Result: **Single time series** for all user requests

#### Real-World Examples

| Original URL | Host Label | Path Template Label |
| --- | --- | --- |
| `https://api.github.com/repos/owner/repo/pulls/42` | `api.github.com` | `/repos/owner/repo/pulls/{id}` |
| `http://localhost:8080/api/users/123?verbose=true` | `localhost:8080` | `/api/users/{id}` |
| `https://example.com/commits/abc123def456...` | `example.com` | `/commits/{hash}` |
| `https://api.example.com:443/api/v1/resources` | `api.example.com` | `/api/v1/resources` |

### Example 9: Using Filesystem Cache

```go
package main

import (
    "context"
    "fmt"
    "io"
    "log"
    "time"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

func main() {
    config := httpc.DefaultConfig()
    config.EnableCache = true
    config.CacheType = httpc.CacheTypeFilesystem
    config.CacheDir = "~/.myapp/http-cache"
    config.CacheTTL = 15 * time.Minute
    
    client := httpc.NewClient(config)
    ctx := context.Background()
    
    resp, err := client.Get(ctx, "https://api.github.com/zen")
    if err != nil {
        log.Fatal(err)
    }
    resp.Body.Close()
    fmt.Println("First request completed (cached to filesystem)")
    
    resp2, err := client.Get(ctx, "https://api.github.com/zen")
    if err != nil {
        log.Fatal(err)
    }
    resp2.Body.Close()
    fmt.Println("Second request completed (from disk cache)")
}
```

### Example 10: Cache Management

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

func main() {
    config := httpc.DefaultConfig()
    config.EnableCache = true
    config.CacheType = httpc.CacheTypeFilesystem
    config.CacheDir = "~/.myapp/http-cache"
    config.CacheTTL = 10 * time.Minute
    
    client := httpc.NewClient(config)
    ctx := context.Background()
    
    // Make some requests to populate cache
    urls := []string{
        "https://httpbin.org/get",
        "https://httpbin.org/headers",
        "https://httpbin.org/user-agent",
    }
    
    for _, url := range urls {
        resp, err := client.Get(ctx, url)
        if err != nil {
            log.Fatal(err)
        }
        resp.Body.Close()
        fmt.Printf("Cached: %s\n", url)
    }
    
    // Delete a specific cache entry
    err := client.DeleteCacheEntry(ctx, urls[0])
    if err != nil {
        log.Printf("Failed to delete cache entry: %v", err)
    }
    fmt.Println("Deleted cache entry for:", urls[0])
    
    // Clean expired entries
    err = client.CleanExpiredCache()
    if err != nil {
        log.Printf("Failed to clean expired cache: %v", err)
    }
    fmt.Println("Cleaned expired cache entries")
    
    // Clear all cache
    err = client.ClearCache()
    if err != nil {
        log.Printf("Failed to clear cache: %v", err)
    }
    fmt.Println("Cleared all cache entries")
}
```

## Error Handling

### Size Limit Errors

When filesystem caching is enabled with a size limit, `Set()` operations may fail:

```go
import (
    "errors"
    "github.com/oakwood-commons/scafctl/pkg/httpc"
)

err := cache.Set(ctx, "key", largeData, ttl)
if errors.Is(err, httpc.ErrCacheSizeLimitExceeded) {
    // Data too large to cache - continue without caching
    fmt.Println("Data exceeds cache size limit")
} else if err != nil {
    // Handle other errors
    log.Fatal(err)
}
```

### Exported Errors

- `ErrCacheSizeLimitExceeded` - Returned when cached data exceeds configured `MaxCacheFileSize`

## Resource Cleanup

Always close the client when done to ensure proper cleanup:

```go
client := httpc.NewClient(config)
defer client.Close() // Cleans up cache and other resources

// Use client...
```

## License

See the main project LICENSE file.
