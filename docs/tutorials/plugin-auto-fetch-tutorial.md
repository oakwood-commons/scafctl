---
title: "Plugin Auto-Fetching"
weight: 135
---

# Plugin Auto-Fetching from Catalogs

This tutorial explains how scafctl automatically fetches plugin binaries from remote catalogs at runtime. You can declare plugin dependencies in your solution, and scafctl will resolve, download, cache, and load them without a prior build step.

## Overview

The plugin auto-fetch flow:

```
Solution declares         Catalog chain            Plugin cache         Provider
plugin dependencies  →  resolves version     →   checks cache     →   registration
                        (local → remote)         (cache hit/miss)     (gRPC plugin)
```

1. **Declare** plugin dependencies in your solution's `bundle.plugins` section
2. **Resolve** — scafctl looks up the plugin version in the catalog chain (local first, then remote OCI registries)
3. **Cache** — if the binary is already cached locally, it's reused; otherwise it's fetched and written to the cache
4. **Load** — the cached binary is launched as a gRPC plugin and its providers are registered

## Declaring Plugin Dependencies

Add a `bundle.plugins` section to your solution:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-solution
  version: 1.0.0
spec:
  resolvers:
    data:
      resolve:
        with:
          - provider: custom-provider
            inputs:
              query: "SELECT * FROM table"
  bundle:
    plugins:
      - name: custom-provider
        kind: provider
        version: "^1.0.0"
```

### Fields

| Field | Description | Example |
|-------|-------------|---------|
| `name` | Plugin catalog reference | `aws-provider` |
| `kind` | Plugin type: `provider` or `auth-handler` | `provider` |
| `version` | Semver constraint | `^1.5.0`, `>=2.0.0`, `1.2.3` |

Version constraints follow [semver](https://semver.org/) conventions:
- `^1.5.0` — any 1.x.y where x ≥ 5
- `~1.5.0` — any 1.5.x
- `>=2.0.0` — 2.0.0 or higher
- `1.2.3` — exact match

## Pre-Fetching Plugins

Use `scafctl plugins install` to download plugin binaries before running a solution:

{{< tabs "plugin-auto-fetch-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
```bash
# Install plugins for a solution
scafctl plugins install -f solution.yaml

# Install for a specific platform (useful in CI)
scafctl plugins install -f solution.yaml --platform linux/amd64

# Use a custom cache directory
scafctl plugins install -f solution.yaml --cache-dir /tmp/plugins
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Install plugins for a solution
scafctl plugins install -f solution.yaml

# Install for a specific platform (useful in CI)
scafctl plugins install -f solution.yaml --platform linux/amd64

# Use a custom cache directory
scafctl plugins install -f solution.yaml --cache-dir /tmp/plugins
```
{{% /tab %}}
{{< /tabs >}}

This is useful for:
- **CI/CD**: Pre-fetch plugins in a setup step, then run solutions offline
- **Air-gapped environments**: Fetch once on a connected machine, copy the cache
- **Reproducibility**: Pin versions with a lock file, then install from locks

## Listing Cached Plugins

View what's in your local plugin cache:

{{< tabs "plugin-auto-fetch-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
```bash
# Table view (default)
scafctl plugins list

# JSON output
scafctl plugins list -o json

# YAML output
scafctl plugins list -o yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Table view (default)
scafctl plugins list

# JSON output
scafctl plugins list -o json

# YAML output
scafctl plugins list -o yaml
```
{{% /tab %}}
{{< /tabs >}}

## Lock Files for Reproducibility

When you build a solution with `scafctl build solution`, plugin versions are pinned in a lock file (`.scafctl.lock.yaml`). The lock file records:

- Exact resolved version
- Content digest (sha256)
- Source catalog

When running with a lock file, scafctl uses the pinned versions exactly. **Without a lock file**, scafctl resolves from catalogs and **requires the catalog to provide a digest**. If no digest is available, the fetch fails:

```
plugin my-plugin@1.0.0: no digest available for verification;
Run 'scafctl build solution' to generate a lock file with pinned digests
```

This mandatory digest verification prevents supply chain attacks where a compromised catalog or man-in-the-middle attacker could serve a malicious binary. Always use lock files for production deployments.

## Catalog Chain

Plugins are resolved through a catalog chain that tries sources in order:

1. **Local catalog** — `$XDG_DATA_HOME/scafctl/catalog/`
2. **Remote OCI catalogs** — configured in `~/.config/scafctl/config.yaml`

### Configuring Remote Catalogs

Add OCI registries to your config:

```yaml
# ~/.config/scafctl/config.yaml
catalogs:
  - name: company-registry
    type: oci
    url: registry.company.com/scafctl
  - name: community
    type: oci
    url: ghcr.io/scafctl-community
```

The chain stops at the first catalog that has the requested artifact.

## Plugin Cache

Downloaded binaries are stored in a content-addressed cache:

```
$XDG_CACHE_HOME/scafctl/plugins/
└── custom-provider/
    └── 1.5.3/
        └── darwin-arm64/
            └── custom-provider    # executable binary
```

### Cache Structure

- `<name>/<version>/<os>-<arch>/<name>` — platform-safe directory layout
- Digest verification on cache reads (when lock file provides a digest)
- Atomic writes (temp file + rename) prevent corruption
- Cache is shared across all solutions

### Managing the Cache

{{< tabs "plugin-auto-fetch-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
# List cached plugins
scafctl plugins list

# Cache is at $XDG_CACHE_HOME/scafctl/plugins/
# To clear the entire cache:
rm -rf ~/.cache/scafctl/plugins/
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# List cached plugins
scafctl plugins list

# Cache is at $env:LOCALAPPDATA\scafctl\plugins\ (Windows)
# To clear the entire cache:
Remove-Item -Recurse -Force "$env:LOCALAPPDATA\scafctl\plugins\"
```
{{% /tab %}}
{{< /tabs >}}

## Multi-Platform Support

Plugin artifacts can include platform-specific binaries. The `AnnotationPlatform` annotation on catalog artifacts identifies the target platform:

```
dev.scafctl.plugin.platform: linux/amd64
```

When fetching, scafctl:
1. Lists all artifacts for the plugin version
2. Matches the `dev.scafctl.plugin.platform` annotation against the current (or requested) platform
3. Falls back to a direct fetch if no platform annotation exists (single-platform plugin)

### Specifying a Target Platform

{{< tabs "plugin-auto-fetch-tutorial-cmd-4" >}}
{{% tab "Bash" %}}
```bash
# Fetch for a different platform
scafctl plugins install -f solution.yaml --platform linux/amd64
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Fetch for a different platform
scafctl plugins install -f solution.yaml --platform linux/amd64
```
{{% /tab %}}
{{< /tabs >}}

This is useful for cross-platform CI where you build on one architecture but deploy on another.

## Runtime Auto-Fetch (During Solution Execution)

When you run a solution that declares plugin dependencies:

{{< tabs "plugin-auto-fetch-tutorial-cmd-5" >}}
{{% tab "Bash" %}}
```bash
scafctl run solution -f solution.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run solution -f solution.yaml
```
{{% /tab %}}
{{< /tabs >}}

The prepare phase automatically:
1. Reads `bundle.plugins` from the solution
2. Checks the lock file for pinned versions
3. Fetches any missing plugins from the catalog chain
4. Caches the binaries locally
5. Loads the plugins and registers their providers
6. Cleans up plugin processes on exit

No explicit `plugins install` step is needed — but pre-fetching is recommended for predictability.

## Example: End-to-End Workflow

{{< tabs "plugin-auto-fetch-tutorial-cmd-6" >}}
{{% tab "Bash" %}}
```bash
# 1. Develop your solution with plugin dependencies
cat > solution.yaml << 'EOF'
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: data-pipeline
  version: 1.0.0
spec:
  resolvers:
    data:
      resolve:
        with:
          - provider: my-db-provider
            inputs:
              connection: "postgres://localhost/db"
  bundle:
    plugins:
      - name: my-db-provider
        kind: provider
        version: "^2.0.0"
EOF

# 2. Build to create a lock file (pins plugin versions)
scafctl build solution -f solution.yaml --version 1.0.0

# 3. Pre-fetch plugins (optional but recommended)
scafctl plugins install -f solution.yaml

# 4. Run the solution (plugins loaded from cache)
scafctl run solution -f solution.yaml

# 5. Check what's cached
scafctl plugins list
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# 1. Develop your solution with plugin dependencies
@'
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: data-pipeline
  version: 1.0.0
spec:
  resolvers:
    data:
      resolve:
        with:
          - provider: my-db-provider
            inputs:
              connection: "postgres://localhost/db"
  bundle:
    plugins:
      - name: my-db-provider
        kind: provider
        version: "^2.0.0"
'@ | Set-Content solution.yaml

# 2. Build to create a lock file (pins plugin versions)
scafctl build solution -f solution.yaml --version 1.0.0

# 3. Pre-fetch plugins (optional but recommended)
scafctl plugins install -f solution.yaml

# 4. Run the solution (plugins loaded from cache)
scafctl run solution -f solution.yaml

# 5. Check what's cached
scafctl plugins list
```
{{% /tab %}}
{{< /tabs >}}

## Troubleshooting

### Plugin not found in any catalog

```
Error: plugin my-plugin (provider): resolving version: ...not found in any catalog
```

- Verify the plugin is published to a configured catalog
- Check `scafctl catalog list --kind provider` to see available providers
- Ensure your config has the correct remote catalog URL

### Version constraint not satisfied

```
Error: resolved version 3.0.0 does not satisfy constraint ^1.0.0
```

- The catalog's latest version doesn't match your constraint
- Update the constraint in your solution, or publish a compatible version

### Cache corruption

If a cached binary seems corrupt:

{{< tabs "plugin-auto-fetch-tutorial-cmd-7" >}}
{{% tab "Bash" %}}
```bash
# Remove the specific plugin from cache
rm -rf ~/.cache/scafctl/plugins/<plugin-name>/<version>/

# Re-fetch
scafctl plugins install -f solution.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Remove the specific plugin from cache
Remove-Item -Recurse -Force "$env:LOCALAPPDATA\scafctl\plugins\<plugin-name>\<version>\"

# Re-fetch
scafctl plugins install -f solution.yaml
```
{{% /tab %}}
{{< /tabs >}}
