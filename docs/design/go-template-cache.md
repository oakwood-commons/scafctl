---
title: "Go Template LRU Cache"
weight: 50
---

# Go Template LRU Cache

## Overview

The `pkg/gotmpl` package includes a thread-safe LRU (Least Recently Used) cache for compiled Go templates. The cache avoids redundant parsing of identical template content across evaluations, significantly improving performance for solutions that evaluate the same templates repeatedly (e.g., during resolver phases with shared template logic).

## Motivation

Go's `text/template.Parse()` is the dominant cost in template evaluation. In large solutions with many resolvers or actions that use similar templates, the same template content is re-parsed on every invocation. The LRU cache stores compiled `*template.Template` objects keyed by a SHA-256 hash of their content and configuration, eliminating redundant parse operations while bounding memory usage.

## Architecture

```
┌─────────────────────────────────────┐
│           Application               │
│                                     │
│  cmd/scafctl ──► InitFromAppConfig  │
│                   │                 │
│                   ▼                 │
│            SetCacheFactory          │
│                   │                 │
│                   ▼                 │
│           TemplateCache (LRU)       │
│           ┌───────────────┐         │
│           │ map[sha256]   │         │
│           │   → *template │         │
│           │ LRU list      │         │
│           │ metrics map   │         │
│           └───────────────┘         │
│                   ▲                 │
│                   │                 │
│   Service.Execute() ──► Get / Put   │
└─────────────────────────────────────┘
```

### Cache Key Generation

Keys are SHA-256 hashes of:
1. Template content (the full template string)
2. Left and right delimiters
3. `missingKey` option (default, zero, error)
4. Sorted function map keys

This ensures that the same template content with different configurations produces different cache entries, while identical configurations share entries.

### LRU Eviction

When the cache reaches its maximum size, the least recently used entry is evicted before a new entry is added. "Recently used" is tracked by moving entries to the front of a doubly linked list on each access.

### Thread Safety

All cache operations are protected by a `sync.RWMutex`:
- `Get` acquires a write lock (it mutates the LRU list)
- `Put` acquires a write lock
- `Stats` acquires a read lock

`Get` returns a *clone* of the cached template via `template.Clone()`, so callers can execute the template concurrently with different data without affecting the cached state.

### Metrics

The cache tracks:
- **Global**: hits, misses, evictions, hit rate
- **Per-template**: hit count and last access time (limited to top 1,000 templates by default to prevent unbounded growth)

## Configuration

### Application Config

```yaml
goTemplate:
  cacheSize: 10000     # Max compiled templates (default: 10000)
  enableMetrics: true   # Enable per-template metrics
```

### Initialization

The cache is wired to application config at startup via `InitFromAppConfig`:

```go
gotmpl.InitFromAppConfig(ctx, gotmpl.GoTemplateConfigInput{
    CacheSize:     cfg.GoTemplate.CacheSize,
    EnableMetrics: true,
})
```

This function is idempotent — only the first call takes effect.

### Defaults

| Setting | Default | Source |
|---------|---------|--------|
| `cacheSize` | 10,000 | `pkg/settings.DefaultGoTemplateCacheSize` |
| `enableMetrics` | `true` | — |

## API

### Package-Level Functions

| Function | Description |
|----------|-------------|
| `GetDefaultCache()` | Returns the singleton cache instance |
| `SetCacheFactory(fn)` | Registers a factory for the default cache (called by `InitFromAppConfig`) |
| `SetDefaultCacheSize(n)` | Sets cache size before first access |
| `ResetDefaultCache()` | Recreates the cache (testing only) |
| `InitFromAppConfig(ctx, cfg)` | Wires cache to app config (idempotent) |

### TemplateCache Methods

| Method | Description |
|--------|-------------|
| `Get(key)` | Retrieves a cloned template from cache |
| `Put(key, tmpl, name)` | Stores a compiled template |
| `Stats()` | Returns global cache statistics |
| `GetDetailedStats(topN)` | Returns stats with per-template breakdown |
| `Clear()` | Removes all entries, preserves stats |
| `ClearWithStats()` | Removes all entries and resets stats |
| `ResetStats()` | Resets stats without clearing entries |

## Testing

- `cache_test.go` — Unit tests for all cache operations, concurrency, eviction behavior
- `cache_benchmark_test.go` — Performance benchmarks for Get/Put under contention
- `appconfig_test.go` — Tests for `InitFromAppConfig` idempotency and reset

## Related

- [CEL Cache](cel.md) — Similar LRU cache for compiled CEL programs
- [Go Templates Tutorial](../tutorials/go-templates-tutorial.md) — End-user guide
- [pkg/gotmpl/README.md](../../pkg/gotmpl/README.md) — Package documentation
