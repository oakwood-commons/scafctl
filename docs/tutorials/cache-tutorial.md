---
title: "Cache Tutorial"
weight: 100
---

# Managing the Cache

This tutorial covers how to use scafctl's cache management commands to view and clear cached data.

## What is Cached?

scafctl caches certain data to improve performance:

| Cache Type | Description | Use Case |
|------------|-------------|----------|
| **HTTP Cache** | Responses from HTTP provider requests | Avoid repeated network calls |
| **Build Cache** | Incremental build fingerprints | Skip unchanged solution rebuilds |
| **Remote Artifact Cache** | Auto-cached artifacts from remote catalogs | Offline access to previously fetched solutions |

Caching reduces network latency, speeds up builds, and allows offline access to previously fetched resources.

## Where is the Cache Stored?

The cache uses XDG Base Directory paths:

| Platform | Default Location |
|----------|------------------|
| macOS | `~/.cache/scafctl/` |
| Linux | `~/.cache/scafctl/` |
| Windows | `%LOCALAPPDATA%\cache\scafctl\` |

You can view actual paths on your system with:

{{< tabs "cache-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
```bash
scafctl config paths
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl config paths
```
{{% /tab %}}
{{< /tabs >}}

## Viewing Cache Information

### Check Cache Status

See how much disk space the cache is using:

{{< tabs "cache-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
```bash
scafctl cache info
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl cache info
```
{{% /tab %}}
{{< /tabs >}}

Output:
```
 💡 Cache Information
HTTP Cache:  2.4 MB (156 files)
             ~/.cache/scafctl/http-cache
Build Cache:  324 B (1 files)
             ~/.cache/scafctl/build-cache
Total: 2.4 MB (157 files)
```

### JSON Output

Get cache info as JSON for scripting:

{{< tabs "cache-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
scafctl cache info -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl cache info -o json
```
{{% /tab %}}
{{< /tabs >}}

Output:
```json
{
  "caches": [
    {
      "name": "HTTP Cache",
      "path": "/Users/me/.cache/scafctl/http-cache",
      "size": 2516582,
      "sizeHuman": "2.4 MB",
      "fileCount": 156,
      "description": "HTTP response cache"
    },
    {
      "name": "Build Cache",
      "path": "/Users/me/.cache/scafctl/build-cache",
      "size": 324,
      "sizeHuman": "324 B",
      "fileCount": 1,
      "description": "Incremental build fingerprints"
    }
  ],
  "totalSize": 2516906,
  "totalHuman": "2.4 MB",
  "totalFiles": 157
}
```

## Clearing the Cache

### Clear All Caches

Remove all cached content:

{{< tabs "cache-tutorial-cmd-4" >}}
{{% tab "Bash" %}}
```bash
scafctl cache clear
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl cache clear
```
{{% /tab %}}
{{< /tabs >}}

You'll be prompted to confirm:
```
? Clear all cached content? (y/N)
```

Output after confirmation:
```
 ✅ Cleared cache
 💡 Removed files: 156
 💡 Reclaimed: 2.4 MB
```

### Skip Confirmation

Use `--force` to skip the confirmation prompt:

{{< tabs "cache-tutorial-cmd-5" >}}
{{% tab "Bash" %}}
```bash
scafctl cache clear --force
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl cache clear --force
```
{{% /tab %}}
{{< /tabs >}}

This is useful in scripts or CI/CD pipelines.

### Clear Specific Cache Type

Clear only a specific type of cache using `--kind`:

{{< tabs "cache-tutorial-cmd-6" >}}
{{% tab "Bash" %}}
```bash
# Clear only HTTP cache
scafctl cache clear --kind http

# Clear only build cache
scafctl cache clear --kind build

# Clear all caches (default)
scafctl cache clear --kind all
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Clear only HTTP cache
scafctl cache clear --kind http

# Clear only build cache
scafctl cache clear --kind build

# Clear all caches (default)
scafctl cache clear --kind all
```
{{% /tab %}}
{{< /tabs >}}

### Clear by Pattern

Clear cache entries matching a specific pattern:

{{< tabs "cache-tutorial-cmd-7" >}}
{{% tab "Bash" %}}
```bash
# Clear all entries with "github" in the name
scafctl cache clear --name "*github*"

# Clear entries starting with "api"
scafctl cache clear --name "api*"
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Clear all entries with "github" in the name
scafctl cache clear --name "*github*"

# Clear entries starting with "api"
scafctl cache clear --name "api*"
```
{{% /tab %}}
{{< /tabs >}}

The pattern supports glob wildcards (`*`, `?`).

### JSON Output

Get structured output for scripting:

{{< tabs "cache-tutorial-cmd-8" >}}
{{% tab "Bash" %}}
```bash
scafctl cache clear --force -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl cache clear --force -o json
```
{{% /tab %}}
{{< /tabs >}}

Output:
```json
{
  "removedFiles": 156,
  "removedBytes": 2516582,
  "reclaimedHuman": "2.4 MB",
  "kind": "all"
}
```

## Common Scenarios

### Troubleshooting Stale Data

If an HTTP provider is returning outdated data, clear the HTTP cache:

{{< tabs "cache-tutorial-cmd-9" >}}
{{% tab "Bash" %}}
```bash
scafctl cache clear --kind http --force
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl cache clear --kind http --force
```
{{% /tab %}}
{{< /tabs >}}

Then re-run your solution to fetch fresh data.

### Reclaiming Disk Space

Check cache size and clear if needed:

{{< tabs "cache-tutorial-cmd-10" >}}
{{% tab "Bash" %}}
```bash
# Check size
scafctl cache info

# Clear if too large
scafctl cache clear --force
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Check size
scafctl cache info

# Clear if too large
scafctl cache clear --force
```
{{% /tab %}}
{{< /tabs >}}

### Automated Cleanup in CI/CD

Add cache cleanup to your CI/CD pipeline:

```yaml
# GitHub Actions example
- name: Clear scafctl cache
  run: scafctl cache clear --force

- name: Run solution
  run: scafctl run solution deploy -r env=staging
```

### Pre-flight Cleanup

Before running a critical deployment, ensure fresh data:

{{< tabs "cache-tutorial-cmd-11" >}}
{{% tab "Bash" %}}
```bash
#!/bin/bash
# deploy.sh

# Clear all caches for fresh data
scafctl cache clear --force

# Run deployment
scafctl run solution deploy -r env=production
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# PowerShell equivalent
# deploy.sh

# Clear all caches for fresh data
scafctl cache clear --force

# Run deployment
scafctl run solution deploy -r env=production
```
{{% /tab %}}
{{< /tabs >}}

## Command Reference

### `scafctl cache info`

Display cache information and disk usage.

| Flag | Short | Description |
|------|-------|-------------|
| `--output` | `-o` | Output format: `table`, `json`, `yaml` |

### `scafctl cache clear`

Clear cached content.

| Flag | Short | Description |
|------|-------|-------------|
| `--kind` | `-k` | Cache type to clear: `all`, `http`, `build` |
| `--name` | `-n` | Pattern to match cache entries |
| `--force` | `-f` | Skip confirmation prompt |
| `--output` | `-o` | Output format: `table`, `json`, `yaml` |

## Build Cache

The build cache enables incremental builds by fingerprinting all build inputs (solution content, bundled files, plugin versions, and lock file). When inputs haven't changed, subsequent `scafctl build solution` invocations skip the entire build pipeline and return the cached result.

### How It Works

1. During `scafctl build solution`, a SHA-256 fingerprint is computed from all build inputs
2. If a matching fingerprint exists in the cache, the build returns immediately
3. After a successful build, the fingerprint and artifact metadata are cached

### Controlling Build Cache

{{< tabs "cache-tutorial-cmd-12" >}}
{{% tab "Bash" %}}
```bash
# Build with cache (default)
scafctl build solution my-solution.yaml

# Force a full rebuild, bypassing cache
scafctl build solution my-solution.yaml --no-cache

# Clear build cache
scafctl cache clear --kind build --force
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Build with cache (default)
scafctl build solution my-solution.yaml

# Force a full rebuild, bypassing cache
scafctl build solution my-solution.yaml --no-cache

# Clear build cache
scafctl cache clear --kind build --force
```
{{% /tab %}}
{{< /tabs >}}

### Configuration

The build cache can be configured in the scafctl config file:

```yaml
build:
  enableCache: true              # Enable/disable build caching (default: true)
  cacheDir: ~/.cache/scafctl/build-cache  # Build cache directory
  autoCacheRemoteArtifacts: true  # Auto-cache remote catalog fetches locally
  pluginCacheDir: ~/.cache/scafctl/plugins  # Plugin cache directory
```

Set configuration via CLI:

{{< tabs "cache-tutorial-cmd-13" >}}
{{% tab "Bash" %}}
```bash
# Disable build cache globally
scafctl config set build.enableCache false

# Disable remote artifact auto-caching
scafctl config set build.autoCacheRemoteArtifacts false
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Disable build cache globally
scafctl config set build.enableCache false

# Disable remote artifact auto-caching
scafctl config set build.autoCacheRemoteArtifacts false
```
{{% /tab %}}
{{< /tabs >}}

## Artifact Cache TTL

The artifact cache supports a time-to-live (TTL) setting that controls how long cached catalog artifacts remain valid. When an artifact's age exceeds the configured TTL, it is treated as a cache miss and is re-fetched from the catalog.

### How TTL Works

1. When a catalog artifact is fetched, scafctl stores it on disk along with creation-time metadata
2. On subsequent requests, scafctl checks the artifact's age against the configured TTL
3. If the artifact is older than the TTL, it is considered expired and re-fetched
4. A TTL of zero (the default) means artifacts never expire

### Configuring TTL

Set the artifact cache TTL in the scafctl config file:

```yaml
catalog:
  cacheTTL: 10m    # Artifacts expire after 10 minutes
```

Common TTL values:

| Value | Meaning |
|-------|---------|
| `0` | Never expire (default) |
| `5m` | 5 minutes |
| `1h` | 1 hour |
| `24h` | 1 day |
| `168h` | 1 week |

Set via CLI:

{{< tabs "cache-tutorial-cmd-14" >}}
{{% tab "Bash" %}}
```bash
# Set a 1-hour TTL for artifact cache
scafctl config set catalog.cacheTTL 1h

# Disable TTL (never expire)
scafctl config set catalog.cacheTTL 0
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Set a 1-hour TTL for artifact cache
scafctl config set catalog.cacheTTL 1h

# Disable TTL (never expire)
scafctl config set catalog.cacheTTL 0
```
{{% /tab %}}
{{< /tabs >}}

### When to Use TTL

- **Active development**: Use a short TTL (e.g., `5m`) to pick up frequent catalog updates
- **CI/CD pipelines**: Use a moderate TTL (e.g., `1h`) to balance freshness with performance
- **Stable environments**: Use zero TTL or a long TTL (e.g., `168h`) for maximum cache benefit
- **Offline use**: Use zero TTL so previously fetched artifacts remain available indefinitely

### Manually Clearing Expired Artifacts

Even with a TTL configured, expired artifacts remain on disk until replaced. To reclaim disk space:

{{< tabs "cache-tutorial-cmd-15" >}}
{{% tab "Bash" %}}
```bash
scafctl cache clear --kind build --force
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl cache clear --kind build --force
```
{{% /tab %}}
{{< /tabs >}}

## Next Steps

- [Provider Reference](provider-reference.md) — Complete provider documentation
- [Provider Development](provider-development.md) — Build custom providers
- [Configuration Tutorial](config-tutorial.md) — Manage application configuration
- [Logging & Debugging Tutorial](logging-tutorial.md) — Control log verbosity, format, and output
