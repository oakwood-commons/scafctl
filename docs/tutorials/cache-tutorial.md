# Managing the Cache

This tutorial covers how to use scafctl's cache management commands to view and clear cached data.

## What is Cached?

scafctl caches certain data to improve performance:

| Cache Type | Description | Use Case |
|------------|-------------|----------|
| **HTTP Cache** | Responses from HTTP provider requests | Avoid repeated network calls |

Caching reduces network latency and allows offline access to previously fetched resources.

## Where is the Cache Stored?

The cache uses XDG Base Directory paths:

| Platform | Default Location |
|----------|------------------|
| macOS | `~/Library/Caches/scafctl/` |
| Linux | `~/.cache/scafctl/` |
| Windows | `%LOCALAPPDATA%\cache\scafctl\` |

You can view actual paths on your system with:

```bash
scafctl config paths
```

## Viewing Cache Information

### Check Cache Status

See how much disk space the cache is using:

```bash
scafctl cache info
```

Output:
```
Cache Information

Platform: darwin/arm64

HTTP Cache:  2.4 MB (156 files)
             ~/Library/Caches/scafctl/http-cache

Total: 2.4 MB (156 files)
```

### JSON Output

Get cache info as JSON for scripting:

```bash
scafctl cache info -o json
```

Output:
```json
{
  "caches": [
    {
      "name": "HTTP Cache",
      "path": "/Users/me/Library/Caches/scafctl/http-cache",
      "size": 2516582,
      "sizeHuman": "2.4 MB",
      "fileCount": 156,
      "description": "HTTP response cache"
    }
  ],
  "totalSize": 2516582,
  "totalHuman": "2.4 MB",
  "totalFiles": 156
}
```

## Clearing the Cache

### Clear All Caches

Remove all cached content:

```bash
scafctl cache clear
```

You'll be prompted to confirm:
```
? Clear all cached content? (y/N)
```

Output after confirmation:
```
 ✅ Cleared cache
 ℹ️   Removed files: 156
 ℹ️   Reclaimed: 2.4 MB
```

### Skip Confirmation

Use `--force` to skip the confirmation prompt:

```bash
scafctl cache clear --force
```

This is useful in scripts or CI/CD pipelines.

### Clear Specific Cache Type

Clear only a specific type of cache using `--kind`:

```bash
# Clear only HTTP cache
scafctl cache clear --kind http

# Clear all caches (default)
scafctl cache clear --kind all
```

### Clear by Pattern

Clear cache entries matching a specific pattern:

```bash
# Clear all entries with "github" in the name
scafctl cache clear --name "*github*"

# Clear entries starting with "api"
scafctl cache clear --name "api*"
```

The pattern supports glob wildcards (`*`, `?`).

### JSON Output

Get structured output for scripting:

```bash
scafctl cache clear --force -o json
```

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

```bash
scafctl cache clear --kind http --force
```

Then re-run your solution to fetch fresh data.

### Reclaiming Disk Space

Check cache size and clear if needed:

```bash
# Check size
scafctl cache info

# Clear if too large
scafctl cache clear --force
```

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

```bash
#!/bin/bash
# deploy.sh

# Clear all caches for fresh data
scafctl cache clear --force

# Run deployment
scafctl run solution deploy -r env=production
```

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
| `--kind` | `-k` | Cache type to clear: `all`, `http` |
| `--name` | `-n` | Pattern to match cache entries |
| `--force` | `-f` | Skip confirmation prompt |
| `--output` | `-o` | Output format: `table`, `json`, `yaml` |

## See Also

- [Configuration Paths](../design/cli.md) — Understanding scafctl directory structure
- [HTTP Provider](provider-reference.md#http) — Configuring HTTP caching behavior
