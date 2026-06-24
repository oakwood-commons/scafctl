---
title: "Plugin Caching (API Server Mode)"
weight: 10
---

# Plugin Caching in API Server Mode

## Overview

When running as an API server (`scafctl serve`), scafctl uses a bounded,
LRU-evicting disk cache to store plugin binaries. This avoids re-downloading
plugins on every request while keeping disk usage under a configurable budget.

The caching system is built from two internal layers:

- **Versioned Cache** (`pkg/cache/versionedcache`) -- maintains an in-memory
  semver index for O(1) latest-version lookups
- **Disk Cache** (`pkg/cache/diskcache`) -- provides bounded LRU storage with
  pinning, atomic writes, and content-hash deduplication

These layers are only instantiated in API server mode. CLI commands use a
simpler unbounded filesystem cache with no eviction and no LRU tracking.

---

## Configuration

All plugin caching settings live under the `apiServer.plugins` section of the
scafctl configuration file:

~~~yaml
apiServer:
  plugins:
    diskCacheMaxSize: "1GB"      # LRU byte budget (default: "512MB")
    disableDiskCache: false       # Set true to skip caching entirely
    allowExternal: false          # Allow loading non-official plugins via API
    allowedPlugins: []            # Allowlist of plugin names (empty = all)
    allowedCatalogs: []           # Restrict which catalogs plugins are fetched from
~~~

### Settings Reference

| Setting | Config Key | Default | Description |
|---------|-----------|---------|-------------|
| Disk cache max size | `apiServer.plugins.diskCacheMaxSize` | `"512MB"` | LRU byte budget. Accepts human-readable strings (`"256MB"`, `"1GB"`, `"2GB"`) |
| Disable disk cache | `apiServer.plugins.disableDiskCache` | `false` | Bypasses cache reads -- every request re-fetches plugins from catalogs. Binaries are still written to disk (required for execution) but are not read on subsequent requests |
| Cache directory | -- | `$XDG_CACHE_HOME/scafctl/plugins/` | Root directory for cached binaries. Determined by `settings.PluginCacheDirFor(binaryName)`. Embedders with a custom binary name get their own isolated directory |
| Fetch concurrency | -- | `4` (`settings.DefaultFetchConcurrency`) | Maximum concurrent plugin fetch operations |
| Content hash | -- | xxHash64 | Non-cryptographic hash for write deduplication (not configurable) |
| File mode | -- | `0o755` | Permissions on cached binaries (not configurable) |

### Cache Sizing

The default 512 MB budget comfortably holds ~35 plugin binaries with room for
version overlap during updates. Adjust based on your workload:

- **Low traffic / few plugins**: `"256MB"` is sufficient
- **High concurrency / many plugins**: `"1GB"` or more prevents excessive eviction churn
- **Signature enforce mode**: Size generously -- every execution re-fetches and writes to the cache, though content-hash dedup avoids redundant I/O when binaries are unchanged

---

## How It Works

### Startup

When the API server starts, it initializes the managed cache and warms it:

~~~go
cacheSize, _ := pluginCacheMaxSize(cfg) // parses "512MB" -> int64
mc, err := plugin.NewManagedCache("", cacheSize)
mc.WarmUp() // scans disk, populates LRU + version index
~~~

`WarmUp()` walks the cache directory, parses each path into
`name/version/platform` components, and registers entries in both the disk
cache LRU and the versioned cache's semver index. Files are adopted **without
triggering eviction** -- the cache may temporarily exceed its budget until the
next write. This prevents data loss from a cold start where all entries would
otherwise be immediately evicted.

If the managed cache fails to initialize (e.g., invalid directory permissions),
the server falls back to unbounded CLI-mode caching with a warning.

### Request Flow

For each incoming API request, the plugin fetcher checks the cache before
downloading. Plugins are fetched concurrently using `errgroup` with a limit of
4, and duplicate in-flight requests for the same plugin are coalesced via
`singleflight`.

1. **Cache probe**: `GetLatestCachedPin()` -- checks for any cached version
   across all known catalog identities
2. **Catalog resolve**: On miss, resolves from catalog via `resolvePlugin()`
   (singleflight-deduplicated)
3. **Version check**: `GetPin()` -- checks if the resolved version + registry
   hash is already cached
4. **Fetch**: On miss, downloads the binary via `fetchPlugin()`
   (singleflight-deduplicated)
5. **Cache store**: `SetPin()` -- writes the binary to disk and pins it
   atomically
6. **Register**: Starts the plugin client from the pinned path

### Pin Lifecycle

After the request completes, the caller releases the pin via `defer release()`.
This decrements the entry's reference count and, if pending eviction was
deferred, allows the LRU to reclaim the entry.

---

## Pinning

Pinning marks a cache entry as **in-use**, preventing the LRU from evicting it
while a request is being served. The pin returns a `release` function that the
caller must invoke when the binary is no longer needed.

~~~go
path, release, ok := cache.GetPin(name, version, platform, digest)
if !ok {
    // not cached
}
defer release()
// use binary at path
~~~

### Atomic Pin Operations

A direct `Get()` followed by `Pin()` has a TOCTOU race -- the entry could be
evicted between the two calls. The cache provides atomic alternatives:

| Method | Equivalent | Purpose |
|--------|-----------|---------|
| `GetPin()` | `Get()` + `Pin()` | Retrieve and pin a specific version |
| `GetLatestCachedPin()` | `GetLatestCached()` + `Pin()` | Retrieve and pin the newest version |
| `SetPin()` | `Put()` + `Pin()` | Write and pin a binary in one step |

### Pin Release Semantics

Pin release uses `atomic.Bool` CAS to guarantee idempotency -- calling
`release()` multiple times is safe. If a panic occurs during request handling,
the recover middleware catches it and deferred `release()` calls still fire
during stack unwinding. This ensures pins cannot leak.

---

## Versioned Cache

The versioned cache (`pkg/cache/versionedcache`) wraps the disk cache with a
semver-aware in-memory index. It tracks all cached versions per
name+platform combination, sorted in descending order.

### Latest Version Lookup

~~~go
version, ok := cache.Latest(name, platform) // O(1) - reads head of sorted list
~~~

This is used by the plugin fetcher as a fallback when catalog resolution fails
(e.g., running offline). The index enables returning the best available version
without scanning the filesystem.

### Index Consistency

The index stays consistent through two mechanisms:

1. **On write**: `Set()` / `SetPin()` insert the version via binary search into
   the sorted list
2. **On eviction**: The diskcache `OnEviction` callback removes the version
   from the index automatically

### Cross-Registry Isolation

Plugins from different catalogs are isolated using a composite name:
`"pluginName/registryHash"`. The 16-character hex registry hash is derived from
the catalog's registry URL, preventing version collisions when the same plugin
name exists in multiple registries.

---

## Disk Cache

The disk cache (`pkg/cache/diskcache`) is a bounded LRU with these properties:

- **Size budget**: Entries are evicted in LRU order when total size exceeds
  `maxSize`. Pinned entries are skipped during eviction (marked `pendingEvict`)
  and reclaimed when unpinned
- **Atomic writes**: Data is written to a temp file, fsynced, then renamed.
  This prevents partial reads from concurrent access
- **Content-hash dedup**: On `Set()`, the xxHash64 of new data is compared
  against the stored hash. If they match, the disk write is skipped entirely
- **Eviction callbacks**: The versioned cache registers an `OnEviction` callback
  to keep its semver index in sync

### Content-Hash Dedup

Dedup avoids redundant I/O in two key scenarios:

1. **Signature enforce mode**: Every execution re-fetches from the catalog and
   calls `SetPin()` with identical bytes. Without dedup, this writes a ~15-50 MB
   temp file and renames it on every run
2. **Concurrent API requests**: Multiple requests for the same plugin version
   race to cache it. The first write succeeds; subsequent writes detect the
   matching hash and return immediately

The hash is non-cryptographic and used solely for write dedup.
Security-critical integrity checks use SHA-256 digest verification separately.

---

## Cache Layout

~~~
$XDG_CACHE_HOME/scafctl/plugins/
└── <name>/                        # Plugin name (or "auth-handler-<name>")
    └── [<registryHash>/]          # Optional 16-char hex registry hash
        └── <version>/             # Semantic version
            └── <os>-<arch>/       # Platform (e.g., darwin-arm64)
                └── <name>[.exe]   # Binary (with .exe on Windows)
~~~

The `<registryHash>` directory level is present when a plugin is fetched from a
catalog and omitted for plugins installed without catalog metadata (legacy path).

Example:

~~~
~/.cache/scafctl/plugins/
├── aws-provider/
│   └── a1b2c3d4e5f67890/         # Registry hash for catalog "prod"
│       ├── 1.5.0/
│       │   └── darwin-arm64/
│       │       └── aws-provider
│       └── 1.5.3/
│           ├── darwin-arm64/
│           │   └── aws-provider
│           └── windows-amd64/
│               └── aws-provider.exe
├── auth-handler-github/
│   └── a1b2c3d4e5f67890/
│       └── 0.3.0/
│           └── darwin-arm64/
│               └── auth-handler-github
└── exec/
    └── 0.6.1/                     # Legacy path (no registry hash)
        └── linux-amd64/
            └── exec
~~~

---

## Digest Verification

The cache supports optional SHA-256 digest verification on reads. When an
expected digest is provided to `Get()` or `GetPin()`, the binary is read and
hashed before the path is returned. Digests use the format `sha256:<hex>`.

~~~go
path, ok := cache.Get(name, version, platform, "sha256:abc123...")
~~~

If the digest is empty, no verification is performed.

---

## CLI Mode

In CLI mode (`scafctl run`, `scafctl lint`, etc.), the cache performs direct
filesystem operations without any LRU, pinning, or version indexing. Binaries
are written atomically and checked via `os.Stat`. There is no eviction --
binaries persist until manually removed via `scafctl plugins remove` or by
deleting the cache directory.

All pin operations return a no-op release function since there is no eviction
risk.

---

## API Reference

### Plugin Cache (`pkg/plugin/cache.go`)

Most methods accept variadic `CacheOption` arguments. The primary option is
`WithRegistryHash(hash)` which adds the registry hash directory level to cache
paths. When the hash is empty, the option is a no-op.

| Method | Signature | Description |
|--------|-----------|-------------|
| `NewCache` | `NewCache(cacheDir string) *Cache` | Create unbounded CLI cache |
| `NewManagedCache` | `NewManagedCache(cacheDir string, maxSize int64) (*Cache, error)` | Create bounded API server cache |
| `Get` | `Get(name, version, platform, expectedDigest string, opts ...CacheOption) (string, bool)` | Retrieve binary path with optional digest check |
| `GetPin` | `GetPin(name, version, platform, expectedDigest string, opts ...CacheOption) (string, func(), bool)` | Retrieve + pin atomically |
| `GetLatestCached` | `GetLatestCached(name, platform string, opts ...CacheOption) (string, string, bool)` | Find newest cached version |
| `GetLatestCachedPin` | `GetLatestCachedPin(name, platform string, opts ...CacheOption) (string, string, func(), bool)` | Find newest + pin atomically |
| `Put` | `Put(name, version, platform string, data []byte, opts ...CacheOption) (string, error)` | Write binary with atomic rename |
| `SetPin` | `SetPin(name, version, platform string, data []byte, opts ...CacheOption) (string, func(), error)` | Write + pin atomically |
| `Pin` | `Pin(name, version, platform string, opts ...CacheOption) (string, func(), bool)` | Pin existing entry |
| `WarmUp` | `WarmUp() error` | Populate LRU from disk (managed mode only) |
| `Digest` | `Digest(name, version, platform string, opts ...CacheOption) (string, error)` | Compute `sha256:<hex>` digest |
| `Remove` | `Remove(name, version, platform string, opts ...CacheOption) error` | Delete a cached binary |
| `List` | `List() ([]CachedPlugin, error)` | List all cached entries |
| `ListCurrentPlatform` | `ListCurrentPlatform() ([]CachedPlugin, error)` | List latest version per plugin for current OS/arch |

### Versioned Cache (`pkg/cache/versionedcache/cache.go`)

| Method | Signature | Description |
|--------|-----------|-------------|
| `New` | `New(cacheDir string, maxSize int64, opts ...diskcache.CacheOption) (*Cache, error)` | Create bounded cache with semver index |
| `Set` | `Set(name, version, platform string, data []byte) error` | Write data and index the version |
| `SetPin` | `SetPin(name, version, platform string, data []byte) (string, func(), error)` | Write + pin + index atomically |
| `Pin` | `Pin(name, version, platform string) (string, func(), bool)` | Pin entry, prevent eviction |
| `Latest` | `Latest(name, platform string) (string, bool)` | O(1) highest version lookup |
| `Delete` | `Delete(name, version, platform string) bool` | Remove entry (index updated via callback) |
| `Versions` | `Versions(name, platform string) []string` | All indexed versions, descending |
| `WarmUp` | `WarmUp() error` | Scan disk and populate LRU + index |

### CachedPlugin

~~~go
type CachedPlugin struct {
    Name         string // Plugin name
    Version      string // Plugin version
    Platform     string // Target platform (os/arch)
    Path         string // Absolute path to cached binary
    Size         int64  // Binary size in bytes
    RegistryHash string // Registry hash directory (empty for local installs)
}
~~~

---

## Windows Support

- Cached binaries on Windows platforms receive a `.exe` extension
- Legacy caches without `.exe` are migrated automatically on first access (rename in place)
- Executable permission checks (`0o111`) are skipped on Windows where they are not meaningful
- Atomic rename uses `MoveFileEx` with `MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH` on Windows (with retry for transient `ERROR_SHARING_VIOLATION` / `ERROR_LOCK_VIOLATION` errors from antivirus or indexers)
- On Unix, atomic rename uses the standard `os.Rename` (POSIX guarantees atomicity)
