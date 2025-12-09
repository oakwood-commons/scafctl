# HTTP Client Package

A robust HTTP client for Go with built-in support for retries, timeouts, and caching.

## Features

- **Automatic Retries**: Uses `github.com/hashicorp/go-retryablehttp` for intelligent retry logic
- **HTTP Caching**: Leverages `ivan.dev/httpcache` for efficient response caching
- **Configurable Timeouts**: Set custom timeouts for all requests
- **Flexible Retry Policies**: Customize retry behavior with custom policies
- **Structured Logging**: Integrates with `logr.Logger` for consistent logging
- **Context Support**: Full support for context-based cancellation and timeouts

## Installation

```bash
go get github.com/kcloutie/scafctl/pkg/httpc
```

## Quick Start

### Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "io"
    
    "github.com/kcloutie/scafctl/pkg/httpc"
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

## Configuration Options

### ClientConfig Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Timeout` | `time.Duration` | `30s` | Maximum time to wait for a request |
| `RetryMax` | `int` | `3` | Maximum number of retries |
| `RetryWaitMin` | `time.Duration` | `1s` | Minimum wait time between retries |
| `RetryWaitMax` | `time.Duration` | `30s` | Maximum wait time between retries |
| `EnableCache` | `bool` | `true` | Enable HTTP response caching |
| `CacheType` | `CacheType` | `memory` | Type of cache: `memory` or `filesystem` |
| `CacheDir` | `string` | `~/.scafctl/http-cache` | Directory for filesystem cache (only used when CacheType is `filesystem`) |
| `CacheTTL` | `time.Duration` | `5m` | Time-to-live for cached responses |
| `Logger` | `logr.Logger` | Discard | Logger for client operations |
| `CheckRetry` | `retryablehttp.CheckRetry` | `nil` | Custom retry policy function |
| `Backoff` | `retryablehttp.Backoff` | `nil` | Custom backoff policy function |
| `ErrorHandler` | `retryablehttp.ErrorHandler` | `nil` | Called if retries are exhausted |

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

### Cache Management

The client provides methods for managing the cache:

```go
// Clear all cached entries (filesystem cache only)
err := client.ClearCache()

// Clean expired cache entries (filesystem cache only)
err := client.CleanExpiredCache()

// Delete a specific cache entry by URL
err := client.DeleteCacheEntry(ctx, "https://api.example.com/data")
```

**Note:** `ClearCache()` and `CleanExpiredCache()` are only supported for filesystem cache. Memory cache doesn't expose these operations due to the underlying implementation.

## Caching Behavior

The client supports two types of caching:

### Memory Cache (Default)

Uses an in-memory LFU (Least Frequently Used) cache:

- Fast and efficient
- Limited to 1000 entries
- Cache is lost when the application restarts

```go
config := httpc.DefaultConfig()
config.EnableCache = true
config.CacheType = httpc.CacheTypeMemory
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

The cache respects standard HTTP caching headers. For best results, ensure your API returns appropriate cache headers:

```http
Cache-Control: max-age=3600
```

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
    
    "github.com/kcloutie/scafctl/pkg/httpc"
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
    
    "github.com/kcloutie/scafctl/pkg/httpc"
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
    
    "github.com/kcloutie/scafctl/pkg/httpc"
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
    
    "github.com/kcloutie/scafctl/pkg/httpc"
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
    
    "github.com/kcloutie/scafctl/pkg/httpc"
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
    
    "github.com/kcloutie/scafctl/pkg/httpc"
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
    
    "github.com/kcloutie/scafctl/pkg/httpc"
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

### Example 8: Filesystem Cache

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "github.com/kcloutie/scafctl/pkg/httpc"
)

func main() {
    config := httpc.DefaultConfig()
    config.EnableCache = true
    config.CacheType = httpc.CacheTypeFilesystem
    config.CacheDir = "~/.myapp/http-cache"
    config.CacheTTL = 10 * time.Minute
    
    client := httpc.NewClient(config)
    
    ctx := context.Background()
    
    // First request - hits the server and caches to disk
    resp1, err := client.Get(ctx, "https://httpbin.org/get")
    if err != nil {
        log.Fatal(err)
    }
    resp1.Body.Close()
    fmt.Println("First request completed (cached to disk)")
    
    // Second request - served from disk cache (even after restart!)
    resp2, err := client.Get(ctx, "https://httpbin.org/get")
    if err != nil {
        log.Fatal(err)
    }
    resp2.Body.Close()
    fmt.Println("Second request completed (from disk cache)")
}
```

### Example 9: Cache Management

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "github.com/kcloutie/scafctl/pkg/httpc"
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

## License

See the main project LICENSE file.
