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
| ------- | ------------- | --------- |
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

> **Note:** By default, `plugins list` shows only the latest cached version of
> each plugin (per platform). Pass `--all-versions` (or its alias `--all`) to
> see every cached version:
>
> ```bash
> scafctl plugins list --all-versions
> ```

## Lock Files for Reproducibility

When you package a solution with `scafctl package solution`, plugin versions are pinned in a lock file (`.scafctl.lock.yaml`). The lock file records, **for every plugin**:

- Exact resolved version
- Content digest (sha256)
- Source catalog alias (`resolvedFrom`) and the machine-independent canonical
  origin (`resolvedCanonical`) it was fetched from

The `resolvedCanonical` origin is what makes the pin portable across machines and
catalog aliases; plugins declared with an explicit `source` block additionally
record that `source` block. See
[How the origin is recorded in the lock file](#how-the-origin-is-recorded-in-the-lock-file).

When running with a lock file, scafctl uses the pinned versions exactly. **Without a lock file**, scafctl resolves from catalogs and **requires the catalog to provide a digest**. If no digest is available, the fetch fails:

```
plugin my-plugin@1.0.0: no digest available for verification;
Run 'scafctl package solution' to generate a lock file with pinned digests
```

This mandatory digest verification prevents supply chain attacks where a compromised catalog or man-in-the-middle attacker could serve a malicious binary. Always use lock files for production deployments.

> **How the lock file is consulted** -- exact pins, constraint ranges, or
> best-effort -- is controlled by the **lock mode**. See
> [Plugin Lock Modes](lock-modes-tutorial.md) for the `--lock-mode` flag and the
> source-based defaults.

## Signature Verification

Beyond digest verification, scafctl supports [Sigstore/cosign](https://docs.sigstore.dev/) keyless signature verification for plugin binaries. This provides cryptographic proof that a plugin was built by a trusted identity.

### Configuring Signature Verification

Add the `plugins.signatures` section to your config:

```yaml
# ~/.config/scafctl/config.yaml
plugins:
  signatures:
    mode: "warn"          # or "enforce"
    trustedIssuers:
      - "https://token.actions.githubusercontent.com"
    trustedIdentities:
      - "https://github.com/oakwood-commons/*"
```

### Verification Modes

| Mode | Behavior |
| ------ | ---------- |
| `off` (default) | No signature check; digest verification only |
| `warn` | Verify signature; log a warning on failure but continue execution |
| `enforce` | Verify signature; fail with an error on missing or invalid signature |

### How Verification Works

When a plugin binary is fetched from a catalog with signature verification enabled:

1. The OCI artifact digest is resolved from the catalog
2. scafctl queries the Rekor transparency log for a cosign signature
3. The signing certificate is validated against Fulcio CA roots
4. The certificate's OIDC issuer and identity are matched against the policy
5. On success, the verification result is logged (issuer, identity, timestamp)
6. On failure, the mode determines the outcome (warn or fail)

### Build Tag Requirement

Signature verification requires the `cosign` build tag:

{{< tabs "plugin-signature-build" >}}
{{% tab "Bash" %}}

```bash
# Build scafctl with cosign signature verification support
go build -tags cosign -o scafctl ./cmd/scafctl/scafctl.go
```

{{% /tab %}}
{{< /tabs >}}

Without the build tag, the stub verifier logs a warning (`warn` mode) or returns
an error (`enforce` mode) indicating that cosign support is not compiled in.

### Embedder Policy Override

Embedders can enforce a signature policy programmatically, overriding user config:

```go
opts := &scafctl.RootOptions{
    PluginSignaturePolicy: &plugin.SignaturePolicy{
        Mode:              plugin.SignatureModeEnforce,
        TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
        TrustedIdentities: []string{"https://github.com/my-org/*"},
    },
}
```

### CI/CD Recommendation

For production CI/CD pipelines, combine signature enforcement with strict mode:

{{< tabs "plugin-signature-ci" >}}
{{% tab "Bash" %}}

```bash
# Enforce both explicit plugin declarations and valid signatures
scafctl run solution -f solution.yaml --strict
```

{{% /tab %}}
{{< /tabs >}}

With `plugins.signatures.mode: "enforce"` in the CI config, this ensures all
plugins are declared, version-pinned, and cryptographically signed.

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

### Pinning a plugin to a specific registry

By default a plugin is resolved by its short `name` through the catalog chain
above -- the first catalog that has an artifact of that name wins. To bind a
plugin dependency to an **explicit OCI registry** instead, add a `source` block:

```yaml
bundle:
  plugins:
    - name: exec                          # solution-local alias (the provider handle)
      kind: provider
      version: "^1.5.0"                    # semver constraint or "latest"
      source:
        registry: ghcr.io/oakwood-commons/providers   # OCI registry + namespace
        artifact: exec                                 # artifact leaf (defaults to name)
```

Field reference:

| Field | Required | Description |
| ------- | ---------- | ------------- |
| `source.registry` | Yes (when `source` is set) | OCI registry and namespace the plugin is fetched from. Must map to a **configured catalog** (see below). |
| `source.artifact` | No | Artifact leaf name within the registry. Defaults to `name` when omitted. |

Behavior:

- **Without `source`** the dependency is *local/short-name*: resolved by `name`
  through the catalog chain (local catalog first, then remote catalogs in order).
- **With `source`** the dependency is *sourced*: fetched from the named
  registry, bypassing chain order. This is useful when the same short name
  exists in multiple catalogs, or to pin a provider to a trusted origin.
- `name` is only the **solution-local alias** you reference from a resolver or
  action's `provider:` field. It can differ from `source.artifact` -- when the
  two differ the dependency is "aliased".
- `source.registry` **must resolve to a configured catalog**. scafctl will not
  fetch from an arbitrary registry: if the registry is not listed under
  `catalogs`, preparation fails with:

  ~~~text
  provider registry "ghcr.io/oakwood-commons/providers" is not a configured
  catalog; add it to the catalogs configuration or reference a configured registry
  ~~~

  Add the registry to your config to authorize it:

  ```yaml
  # ~/.config/scafctl/config.yaml
  catalogs:
    - name: oakwood
      type: oci
      url: ghcr.io/oakwood-commons/providers
  ```

For **official first-party providers**, the default registry
(`ghcr.io/oakwood-commons/providers/<name>`) is already known, so a short `name`
declaration is enough -- an explicit `source` is only needed to override the
origin. Use the `list_official_providers` MCP tool to see each official
provider's default catalog reference.

### How the origin is recorded in the lock file

Every plugin -- short-name **and** sourced -- records its resolved origin in the
lock file, so later runs fetch the exact same artifact from the exact same place
regardless of catalog chain order or local catalog aliases. The fields common to
**all** lock entries are:

| Lock field | Value | Purpose |
| ------------ | ------- | --------- |
| `name` | the **resolved artifact leaf** (`source.artifact`, or `name` when omitted) | the real OCI leaf, not the solution-local alias |
| `version` | the exact resolved semver (e.g. `1.5.3`) | pins the moving constraint to one version |
| `constraint` | the requested constraint as written (e.g. `^1.5.0`) | refreshed on every build; used to decide whether the pin still satisfies the request |
| `digest` / `digests` | SHA-256 content digest(s) | supply-chain verification on every fetch |
| `resolvedCanonical` | the machine-independent canonical origin it was fetched from (e.g. `ghcr.io/org/plugins`) | **recorded for every plugin**; survives catalog **renames** and is portable across machines |
| `resolvedFrom` | the local catalog **alias** the origin mapped to | human-facing; may differ per machine |

A plugin declared with an explicit `source` block additionally stores a `source`
block in its lock entry:

| Lock field | Value | Purpose |
| ------------ | ------- | --------- |
| `source.registry` | the canonical registry origin from `source.registry` | marks the entry as *sourced* and binds its lock identity to that origin |

Do not confuse the two: `resolvedCanonical` is present on **every** entry and is
how scafctl records where a plugin was fetched from, while the `source` block is
present **only** on entries whose `bundle.plugins` declaration used `source`.

The **solution-local alias** (`name` in `bundle.plugins`) is deliberately *not*
stored -- the lock records the resolved `(canonical origin, artifact leaf, kind)`
identity instead. Two aliases that point at the same origin+artifact therefore
dedupe to a single lock entry, and renaming the alias in the solution does not
churn the lock.

A **sourced** lock entry is matched back to its dependency by the
`(source.registry, artifact leaf, kind)` tuple; a **short-name** entry is matched
by `(artifact leaf, kind)` against entries with no `source` block. Either way,
because the canonical origin is stored, a teammate on a different machine who has
the same registry configured under a *different* catalog alias still replays the
identical pin. On the next build the pinned `version` is replayed as long as it
still satisfies the current `constraint`; if you tighten or bump the constraint
past the pin, that one entry is re-resolved and re-pinned while everything else
stays frozen.

This is what makes a plugin deterministic: the origin (`resolvedCanonical`, plus
`source.registry` when sourced), the artifact (`name`), the exact `version`, and
the content `digest` are all frozen in the lock, so `scafctl run solution` pulls
byte-identical plugins on every machine. Always commit the lock file for
production deployments. See [Plugin Lock Modes](lock-modes-tutorial.md) for how
strictly those pins are enforced.

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

## Schema Caching (Offline Access)

In addition to caching plugin binaries, scafctl caches **provider descriptor schemas** locally. This means that once a plugin provider's schema has been fetched via `get_provider_schema` or `get_provider_output_shape` in the MCP server, subsequent schema lookups can be served from disk without spawning the plugin process or requiring network access.

### How It Works

1. When `get_provider_schema` or `get_provider_output_shape` successfully resolves a plugin provider, the full descriptor (input schema, output schemas, capabilities, examples) is persisted to disk.
2. On subsequent requests, if the plugin binary is unavailable (offline, not yet installed, network error), the cached descriptor is returned with `"source": "cached"` in the response.
3. Cache entries expire after 24 hours by default. After expiry, scafctl attempts a fresh fetch and updates the cache.
4. Running `scafctl plugins install` automatically invalidates cached descriptors for the installed plugins so the next schema request returns up-to-date data.

### Cache Location

```
$XDG_CACHE_HOME/scafctl/provider-schemas/
|-- exec.json
|-- git.json
|-- github.json
|-- directory.json
|-- ...
```

| Platform | Default Path |
|----------|-------------|
| Linux    | `~/.cache/scafctl/provider-schemas/` |
| macOS    | `~/.cache/scafctl/provider-schemas/` |
| Windows  | `%LOCALAPPDATA%\cache\scafctl\provider-schemas\` |

### Managing the Schema Cache

{{< tabs "plugin-auto-fetch-tutorial-cmd-4" >}}
{{% tab "Bash" %}}

```bash
# Schema cache is at $XDG_CACHE_HOME/scafctl/provider-schemas/
# To clear all cached schemas (forces re-fetch on next access):
rm -rf ~/.cache/scafctl/provider-schemas/

# To invalidate a single provider:
rm ~/.cache/scafctl/provider-schemas/exec.json
```

{{% /tab %}}
{{% tab "PowerShell" %}}

```powershell
# Schema cache is at $env:LOCALAPPDATA\cache\scafctl\provider-schemas\
# To clear all cached schemas:
Remove-Item -Recurse -Force "$env:LOCALAPPDATA\cache\scafctl\provider-schemas\"

# To invalidate a single provider:
Remove-Item "$env:LOCALAPPDATA\cache\scafctl\provider-schemas\exec.json"
```

{{% /tab %}}
{{< /tabs >}}

### Use Cases

- **CI/CD**: Pre-fetch plugin schemas in a warm-up step, then run MCP sessions offline
- **Air-gapped environments**: Copy the `provider-schemas/` directory to disconnected machines
- **Faster MCP startup**: Cached schemas avoid spawning plugin processes for schema-only queries

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

No explicit `plugins install` step is needed -- but pre-fetching is recommended for predictability.

## Official Providers

scafctl ships a built-in registry of **official providers** distributed as
external plugins from `ghcr.io/oakwood-commons/providers/<name>`:

| Provider | Description |
| ---------- | ------------- |
| `directory` | File system directory operations |
| `env` | Environment variable lookup |
| `exec` | Shell command execution |
| `git` | Git repository operations |
| `github` | GitHub API interactions |
| `hcl` | HCL/Terraform file parsing |
| `identity` | Identity token provider |
| `metadata` | Solution and system metadata |
| `secret` | Secret retrieval |
| `sleep` | Delay execution |

Official providers are **plugins**, not built-ins. That distinction matters for
how they are resolved.

### Solution execution requires explicit declaration

During solution execution -- `scafctl run solution`, `scafctl run resolver`, and
`scafctl render solution` -- **every non-built-in provider a solution references must be
declared in `bundle.plugins`, including official providers**. There is no silent
auto-fetch on this path.

If a solution references an official provider (for example `exec`) without
declaring it, preparation fails **before any network fetch is attempted**:

~~~text
providers [exec] are not builtins and are not declared in bundle.plugins;
add them to bundle.plugins
~~~

Declaring the provider fixes it:

```yaml
bundle:
  plugins:
    - name: exec
      kind: provider
      version: "^1.0.0"
```

This keeps runs deterministic and auditable: the complete set of external
plugins a solution depends on is always visible in the solution and pinned in its
lock file. See [Plugin Lock Modes](lock-modes-tutorial.md) for how those pins are
applied.

### `run provider <name>` auto-resolves

The direct provider-invocation command, `scafctl run provider <name>`, is the one
place that still fetches an official provider on demand. It is a convenience for
ad-hoc use and is **not** part of solution execution:

{{< tabs "plugin-auto-fetch-tutorial-cmd-run-provider" >}}
{{% tab "Bash" %}}

~~~bash
# Fetches the exec provider plugin on first use, then runs it
scafctl run provider exec command='echo hello' -o json

# Fetches the env provider plugin
scafctl run provider env name=HOME -o json
~~~

{{% /tab %}}
{{% tab "PowerShell" %}}

~~~powershell
# Fetches the exec provider plugin on first use, then runs it
scafctl run provider exec command='echo hello' -o json

# Fetches the env provider plugin
scafctl run provider env name=HOME -o json
~~~

{{% /tab %}}
{{< /tabs >}}

The binary is fetched on first use and cached for subsequent calls. Dynamic help
(`--help`) also triggers the fetch so you can view the provider's schema.

### API server pre-loading

When you run the API server (`scafctl serve`), official providers are pre-loaded
into the shared plugin pool at **startup** as a warm-up optimization. This does
not change request-time behavior: a solution submitted to the server must still
declare its providers in `bundle.plugins`, exactly like the CLI, or preparation
fails with the same undeclared-provider error.

### Disabling official-provider resolution

To turn off the `run provider` convenience fetch and the API startup pre-load
(for air-gapped or restricted environments), disable official providers in your
config:

```yaml
# ~/.config/scafctl/config.yaml
settings:
  disableOfficialProviders: true
```

### `--strict` and official auth handlers

Unlike providers, official **auth handlers** (fetched when a solution references
them via the `identity` provider) are still auto-resolved during solution
execution. The `--strict` flag on `run` and `render` disables that auth-handler
auto-resolution, requiring every auth handler to be declared in `bundle.plugins`
as well:

{{< tabs "plugin-auto-fetch-tutorial-cmd-strict" >}}
{{% tab "Bash" %}}

~~~bash
# Also require official auth handlers to be declared explicitly
scafctl run solution -f ./solution.yaml --strict
scafctl run resolver -f ./solution.yaml --strict
~~~

{{% /tab %}}
{{% tab "PowerShell" %}}

~~~powershell
# Also require official auth handlers to be declared explicitly
scafctl run solution -f ./solution.yaml --strict
scafctl run resolver -f ./solution.yaml --strict
~~~

{{% /tab %}}
{{< /tabs >}}

Use `--strict` in CI so that neither providers nor auth handlers rely on implicit
resolution.

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
scafctl package solution -f solution.yaml --version 1.0.0

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
scafctl package solution -f solution.yaml --version 1.0.0

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
