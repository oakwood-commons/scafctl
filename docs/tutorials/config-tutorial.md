---
title: "Configuration Management"
weight: 90
---

# Configuration Management Tutorial

This tutorial covers managing scafctl's application configuration using the `config` CLI commands.

## Overview

scafctl uses a YAML configuration file to control application behavior including logging, HTTP client settings, resolver timeouts, catalog locations, and more.

| Command | Description |
|---------|-------------|
| `config init` | Create config file |
| `config view` | View current config |
| `config get/set` | Read/write values |
| `config validate` | Validate config |
| `config schema` | View JSON schema |
| `config paths` | Show XDG paths |

## Quick Start

### 1. Initialize Configuration

Create a new config file:

{{< tabs "config-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
```bash
# Create minimal config
scafctl config init

# Create with all options documented
scafctl config init --full

# Preview without writing
scafctl config init --dry-run

# Write to custom path
scafctl config init --output ./my-config.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Create minimal config
scafctl config init

# Create with all options documented
scafctl config init --full

# Preview without writing
scafctl config init --dry-run

# Write to custom path
scafctl config init --output ./my-config.yaml
```
{{% /tab %}}
{{< /tabs >}}

### 2. View Configuration

See the current effective configuration:

{{< tabs "config-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
```bash
# Full config (kvx auto-picks the format)
scafctl config view

# As YAML
scafctl config view -o yaml

# As JSON
scafctl config view -o json

# Filter with CEL expression
scafctl config view -e '_.settings'

# Interactive TUI explorer
scafctl config view -i
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Full config (kvx auto-picks the format)
scafctl config view

# As YAML
scafctl config view -o yaml

# As JSON
scafctl config view -o json

# Filter with CEL expression
scafctl config view -e '_.settings'

# Interactive TUI explorer
scafctl config view -i
```
{{% /tab %}}
{{< /tabs >}}

### 3. Show Config Sources

See where each value comes from (file, dropin, environment, default) and
filter by source:

{{< tabs "config-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
# Annotate every key with its source and list env-var overrides
scafctl config view --show-origin

# Only values coming from the config file
scafctl config view --source=file

# Only environment variable overrides
scafctl config view --source=env
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Annotate every key with its source and list env-var overrides
scafctl config view --show-origin

# Only values coming from the config file
scafctl config view --source=file

# Only environment variable overrides
scafctl config view --source=env
```
{{% /tab %}}
{{< /tabs >}}

## Getting and Setting Values

### Read Values

Use dot notation to read specific config values:

{{< tabs "config-tutorial-cmd-4" >}}
{{% tab "Bash" %}}
```bash
# Get logging level
scafctl config get logging.level

# Get default catalog
scafctl config get settings.defaultCatalog

# Get HTTP timeout
scafctl config get httpClient.timeout
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Get logging level
scafctl config get logging.level

# Get default catalog
scafctl config get settings.defaultCatalog

# Get HTTP timeout
scafctl config get httpClient.timeout
```
{{% /tab %}}
{{< /tabs >}}

### Write Values

> **Warning (`config.d` drop-in fragments):** When non-empty `config.d/*.yaml` fragments are loaded, `scafctl config set` only persists `logging.level`, `logging.format`, `logging.timestamps`, `logging.enableProfiling`, `settings.quiet`, `settings.noColor`, `settings.defaultCatalog`, `httpClient.allowPrivateIPs`, `httpClient.trustProxyResolution`, `httpClient.trustedProxy`, `httpClient.allowedPrivateCIDRs`, and `auth.browser` to `config.yaml`. For any other key (such as `settings.disableOfficialAuthHandlers`, `apiServer.*`, `resolver.*`, or `httpClient.timeout`), edit `config.yaml` or a `config.d/*.yaml` fragment directly so changes survive reload.

{{< tabs "config-tutorial-cmd-5" >}}
{{% tab "Bash" %}}
```bash
# Set logging level
scafctl config set logging.level debug

# Set HTTP timeout
scafctl config set httpClient.timeout 60s

# Set resolver concurrency
scafctl config set resolver.concurrency 8

# Enable a boolean
scafctl config set httpClient.caching.enabled true
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Set logging level
scafctl config set logging.level debug

# Set HTTP timeout
scafctl config set httpClient.timeout 60s

# Set resolver concurrency
scafctl config set resolver.concurrency 8

# Enable a boolean
scafctl config set httpClient.caching.enabled true
```
{{% /tab %}}
{{< /tabs >}}

### Reset to Default

{{< tabs "config-tutorial-cmd-6" >}}
{{% tab "Bash" %}}
```bash
# Reset a value to its default
scafctl config unset logging.level

# Reset HTTP configuration
scafctl config unset httpClient.timeout
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Reset a value to its default
scafctl config unset logging.level

# Reset HTTP configuration
scafctl config unset httpClient.timeout
```
{{% /tab %}}
{{< /tabs >}}

## Managing Catalogs

### Add a Catalog

{{< tabs "config-tutorial-cmd-7" >}}
{{% tab "Bash" %}}
```bash
# Add a local filesystem catalog
scafctl catalog remote add my-catalog --type filesystem --path ~/my-solutions

# Add an OCI registry catalog
scafctl catalog remote add company --type oci --url oci://ghcr.io/myorg/scafctl-catalog

# Add and set as default
scafctl catalog remote add primary --type filesystem --path ~/catalogs/main --default

# Add with auth provider for automatic token injection
scafctl catalog remote add corp --type oci --url oci://registry.example.com/artifacts --auth-provider github
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Add a local filesystem catalog
scafctl catalog remote add my-catalog --type filesystem --path ~/my-solutions

# Add an OCI registry catalog
scafctl catalog remote add company --type oci --url oci://ghcr.io/myorg/scafctl-catalog

# Add and set as default
scafctl catalog remote add primary --type filesystem --path ~/catalogs/main --default

# Add with auth provider for automatic token injection
scafctl catalog remote add corp --type oci --url oci://registry.example.com/artifacts --auth-provider github
```
{{% /tab %}}
{{< /tabs >}}

### Remove a Catalog

{{< tabs "config-tutorial-cmd-8" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog remote remove my-catalog
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog remote remove my-catalog
```
{{% /tab %}}
{{< /tabs >}}

### Set Default Catalog

{{< tabs "config-tutorial-cmd-9" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog remote default my-catalog
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog remote default my-catalog
```
{{% /tab %}}
{{< /tabs >}}

## Validation

### Validate Config File

{{< tabs "config-tutorial-cmd-10" >}}
{{% tab "Bash" %}}
```bash
# Validate current config
scafctl config validate

# Validate a specific file
scafctl config validate path/to/config.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Validate current config
scafctl config validate

# Validate a specific file
scafctl config validate path/to/config.yaml
```
{{% /tab %}}
{{< /tabs >}}

### View Config Schema

Default output is pretty JSON so `scafctl config schema > file.json` feeds
editor/IDE tooling directly. All standard kvx flags are supported so you can
render as YAML, filter with CEL, or explore interactively.

{{< tabs "config-tutorial-cmd-11" >}}
{{% tab "Bash" %}}
```bash
# Pretty-printed JSON Schema (default)
scafctl config schema

# Save schema for editor autocompletion
scafctl config schema > ~/.config/scafctl/config-schema.json

# Compact JSON (single line)
scafctl config schema --compact

# YAML view
scafctl config schema -o yaml

# Extract a subtree with CEL
scafctl config schema -e '_.properties.catalogs'

# Explore the schema interactively
scafctl config schema -i
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Pretty-printed JSON Schema (default)
scafctl config schema

# Save schema for editor autocompletion
scafctl config schema > $HOME\.config\scafctl\config-schema.json

# Compact JSON (single line)
scafctl config schema --compact

# YAML view
scafctl config schema -o yaml

# Extract a subtree with CEL
scafctl config schema -e '_.properties.catalogs'

# Explore the schema interactively
scafctl config schema -i
```
{{% /tab %}}
{{< /tabs >}}

## View System Paths

See where scafctl stores files on your system:

{{< tabs "config-tutorial-cmd-12" >}}
{{% tab "Bash" %}}
```bash
# Show all XDG paths
scafctl config paths

# As JSON
scafctl config paths -o json

# Show paths for a different platform
scafctl config paths --platform linux
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Show all XDG paths
scafctl config paths

# As JSON
scafctl config paths -o json

# Show paths for a different platform
scafctl config paths --platform linux
```
{{% /tab %}}
{{< /tabs >}}

Typical output:

```
 💡 scafctl Paths
Platform: darwin/arm64
Config:      ~/.config/scafctl/config.yaml
Secrets:     ~/.local/share/scafctl/secrets
Data:        ~/.local/share/scafctl
Catalog:     ~/.local/share/scafctl/catalog
Cache:       ~/.cache/scafctl
HTTP Cache:  ~/.cache/scafctl/http-cache
State:       ~/.local/state/scafctl
Override paths with XDG environment variables or SCAFCTL_SECRETS_DIR.

 💡 Config sources (merge order)

  1. built-in defaults
  2. ~/.config/scafctl/config.d/10-telemetry.yaml
  3. ~/.config/scafctl/config.d/50-clusters.yaml
  4. ~/.config/scafctl/config.yaml
  5. SCAFCTL_* environment variables

Later sources override earlier ones.
```

The **Config sources** section lists every layer that feeds the merged
configuration, in the order they are applied. Later sources override earlier
ones, so a value in `config.d/50-clusters.yaml` wins over one in
`10-telemetry.yaml`, and your `config.yaml` wins over both. This makes the
`config.d/` drop-in directory discoverable -- otherwise it is easy to miss where
a value like `kube.clusters` is actually set. A missing user config file is
shown as `(not present)`.

## Configuration Reference

### Key Settings

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `logging.level` | string | `none` | Log level: `none`, `error`, `warn`, `info`, `debug`, `trace`, or numeric V-level |
| `logging.format` | string | `console` | Log format: `console` (colored), `json` (structured) |
| `settings.defaultCatalog` | string | `local` | Default catalog name |
| `httpClient.timeout` | duration | `30s` | HTTP request timeout |
| `httpClient.retry.maxRetries` | int | `3` | Max HTTP retries |
| `httpClient.caching.enabled` | bool | `true` | Enable HTTP response caching |
| `httpClient.maxResponseBodySize` | int | `104857600` | Max HTTP response body size (bytes, default 100 MB) |
| `httpClient.allowPrivateIPs` | bool | `false` | Allow requests to every private/loopback IP range at once. Prefer `allowedPrivateCIDRs` |
| `httpClient.allowedPrivateCIDRs` | []string | unset | Private ranges (or bare addresses) this client may reach. Checked against the resolved address when the connection is opened. Overrides `allowPrivateIPs` when set. Cloud metadata is never permitted |
| `httpClient.trustedProxy` | bool | `false` | Trust a configured HTTP/HTTPS proxy (`HTTP_PROXY`/`HTTPS_PROXY`) to enforce its own egress policy. Proxy routing is disabled entirely for policy-protected clients unless this is `true` |
| `httpClient.trustProxyResolution` | bool | `false` | On the trusted-proxy path, allow a proxied target that does not resolve locally to proceed, leaving egress policy to the proxy. Has no effect unless `httpClient.trustedProxy` is also `true`. Defaults to failing closed |
| `settings.requireSecureKeyring` | bool | `false` | Fail if OS keyring unavailable instead of insecure fallback |
| `resolver.timeout` | duration | `5m` | Overall resolver timeout |
| `resolver.concurrency` | int | `4` | Max parallel resolver execution |
| `action.timeout` | duration | `10m` | Overall action timeout |
| `action.concurrency` | int | `4` | Max parallel action execution |
| `plugins.grpcMaxMessageSize` | int | `67108864` | Max gRPC message size in bytes for plugin communication (64 MB default). Increase for very large provider inputs/outputs |
| `plugins.fetchCooldown` | duration | `5m` | Cooldown between failed plugin auto-fetch retries |
| `settings.disableOfficialAuthHandlers` | bool | `false` | Disable auto-resolution of the official auth handlers (`entra`, `github`, `gcp`) |
| `settings.disableThirdPartyAuthHandlers` | bool | `false` | Disable catalog-by-name resolution of non-official auth handlers (official-only policy) |
| `auth.browser` | string | unset | Preferred web browser for interactive login (`msedge`, `chrome`, `firefox`, `safari`, `default`, or an executable path; overridden by `--browser`, falls back to `$BROWSER` and OS default) |
| `auth.entra.*` | object | -- | Entra (Azure AD) CLI auth handler config (`tenantId` defaults to `common`, `defaultFlow` defaults to `interactive`; supports `clientId`, `authority`, `defaultScopes`, `defaultFlow`, `clientSecret`, `federatedTokenFile`, `federatedToken`, `httpClient`, `activeProfile`, `profiles`) |
| `auth.github.hostname` | string | `github.com` | GitHub hostname (or GHES hostname) |
| `auth.github.clientId` | string | built-in | OAuth App client ID |
| `auth.github.defaultScopes` | []string | `[gist, read:org, repo, workflow]` | Default OAuth scopes |
| `auth.handlers.<name>.plugin.ref` | string | handler name | Catalog artifact name for a pinned third-party auth handler |
| `auth.handlers.<name>.plugin.version` | string | `latest` | Version constraint for a pinned third-party auth handler |
| `auth.handlers.<name>.trustedVerificationDomains` | []string | -- | Per-handler trusted device-code verification domains (required trust source for third-party handlers) |
| `apiServer.auth.handlers.<name>` | object | -- | Server-mode settings forwarded to an auth handler plugin's `ActivateServerMode` RPC during `scafctl serve` |

### Config File Location

The config file is located at the XDG config path:

| Platform | Default Location |
|----------|------------------|
| macOS | `~/.config/scafctl/config.yaml` |
| Linux | `~/.config/scafctl/config.yaml` |
| Windows | `%APPDATA%\scafctl\config.yaml` |

Override with the `--config` flag on any command:

{{< tabs "config-tutorial-cmd-13" >}}
{{% tab "Bash" %}}
```bash
scafctl run solution my-solution --config ./custom-config.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run solution my-solution --config ./custom-config.yaml
```
{{% /tab %}}
{{< /tabs >}}

## Examples

### Minimal Config

See [examples/config/minimal-config.yaml](../../examples/config/minimal-config.yaml):

```yaml
version: 1
settings:
  defaultCatalog: "local"
logging:
  level: "none"
catalogs:
  - name: local
    type: filesystem
    path: ~/scafctl-catalog/
```

### Full Config

See [examples/config/full-config.yaml](../../examples/config/full-config.yaml) for a complete reference with all options documented.

## Common Workflows

### Initial Setup

{{< tabs "config-tutorial-cmd-14" >}}
{{% tab "Bash" %}}
```bash
# 1. Initialize config
scafctl config init

# 2. Set up a catalog
scafctl catalog remote add local --type filesystem --path ~/scafctl-catalog --default

# 3. Verify
scafctl config view
scafctl config validate
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# 1. Initialize config
scafctl config init

# 2. Set up a catalog
scafctl catalog remote add local --type filesystem --path ~/scafctl-catalog --default

# 3. Verify
scafctl config view
scafctl config validate
```
{{% /tab %}}
{{< /tabs >}}

### Switch Environments

{{< tabs "config-tutorial-cmd-15" >}}
{{% tab "Bash" %}}
```bash
# Use staging catalog
scafctl catalog remote default staging

# Increase logging for debugging
scafctl config set logging.level debug

# After debugging, reset
scafctl config unset logging.level
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Use staging catalog
scafctl catalog remote default staging

# Increase logging for debugging
scafctl config set logging.level debug

# After debugging, reset
scafctl config unset logging.level
```
{{% /tab %}}
{{< /tabs >}}

## API Server: Token Delegation

The `apiServer` section includes configuration for server-mode auth handlers and token pass-through when running in API mode (`scafctl serve`). These settings control how the server acquires, exchanges, or forwards tokens to downstream providers.

### Server-Mode Auth Handler (Entra ID)

Configure the Entra auth handler plugin under `apiServer.auth.handlers.entra` for server-identity (`client_credentials` or `workload_identity`) and delegated (`obo` / machine) token flows:

> **Warning (`config.d` drop-in fragments):** When non-empty `config.d/*.yaml` fragments are loaded, `scafctl config set apiServer.*` reports success without persisting the new server-handler settings to `config.yaml`, so a reload (`scafctl serve`) loses the changes and may run without the intended Entra handler. If you use `config.d` fragments, configure `apiServer.auth.handlers.entra` directly in YAML (`~/.config/scafctl/config.yaml` or a `~/.config/scafctl/config.d/*.yaml` fragment) instead of using the shell commands below:

```yaml
apiServer:
  auth:
    handlers:
      entra:
        tenantId: "your-tenant-id"
        clientId: "your-client-id"
        serverFlow: "client_credentials"
        credential:
          clientSecret: "env://SCAFCTL_API_ENTRA_CLIENT_SECRET"
        # Optional; omit delegated to deny delegation
        delegated:
          userFlow: "obo"
          machine: true
```

When no non-empty `config.d` fragments are present, you can also set the same fields from the CLI:

{{< tabs "config-tutorial-cmd-16" >}}
{{% tab "Bash" %}}
```bash
# Warning: if non-empty config.d/*.yaml fragments are present, edit config.yaml directly instead (see above).
# Set up Entra server-mode handler for token acquisition and delegation
scafctl config set apiServer.auth.handlers.entra.tenantId "your-tenant-id"
scafctl config set apiServer.auth.handlers.entra.clientId "your-client-id"
scafctl config set apiServer.auth.handlers.entra.serverFlow "client_credentials"
scafctl config set apiServer.auth.handlers.entra.credential.clientSecret "env://SCAFCTL_API_ENTRA_CLIENT_SECRET"

# Configure delegated caller flows (optional; omit delegated to deny delegation)
scafctl config set apiServer.auth.handlers.entra.delegated.userFlow "obo"
scafctl config set apiServer.auth.handlers.entra.delegated.machine true

# View the Entra server-mode handler config
scafctl config get apiServer.auth.handlers.entra
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Warning: if non-empty config.d/*.yaml fragments are present, edit config.yaml directly instead (see above).
# Set up Entra server-mode handler for token acquisition and delegation
scafctl config set apiServer.auth.handlers.entra.tenantId "your-tenant-id"
scafctl config set apiServer.auth.handlers.entra.clientId "your-client-id"
scafctl config set apiServer.auth.handlers.entra.serverFlow "client_credentials"
scafctl config set apiServer.auth.handlers.entra.credential.clientSecret "env://SCAFCTL_API_ENTRA_CLIENT_SECRET"

# Configure delegated caller flows (optional; omit delegated to deny delegation)
scafctl config set apiServer.auth.handlers.entra.delegated.userFlow "obo"
scafctl config set apiServer.auth.handlers.entra.delegated.machine true

# View the Entra server-mode handler config
scafctl config get apiServer.auth.handlers.entra
```
{{% /tab %}}
{{< /tabs >}}

Credential fields (`credential.clientSecret` for `serverFlow: "client_credentials"`, or `credential.wifToken` for `serverFlow: "workload_identity"`) use `SecretRef` URI format:
- `env://VAR_NAME` -- reads from an environment variable
- `file:///path/to/secret` -- reads from a file

### Token Pass-Through

Configure which provider-specific tokens can be forwarded from API request headers in `~/.config/scafctl/config.yaml` (or a `~/.config/scafctl/config.d/*.yaml` fragment). Because `scafctl config set` parses arguments as scalar values rather than YAML lists (and does not persist `apiServer.*` keys when non-empty `config.d` fragments are loaded), set `allowedHeaders` in YAML rather than via `scafctl config set`:

```yaml
apiServer:
  tokenPassThrough:
    allowedHeaders:
      - "Github"
      - "Artifactory"
```

Inspect the effective token pass-through configuration from the CLI:

{{< tabs "config-tutorial-cmd-17" >}}
{{% tab "Bash" %}}
```bash
# View the token pass-through config
scafctl config get apiServer.tokenPassThrough
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# View the token pass-through config
scafctl config get apiServer.tokenPassThrough
```
{{% /tab %}}
{{< /tabs >}}

When configured, callers send tokens via `X-Authorization-<Suffix>` headers (e.g., `X-Authorization-Github`). The server makes these available to providers via the `authProvider` input field. When `tokenPassThrough` is omitted entirely, only `Github` is allowed by default.

For a complete guide, see [API Mode Authentication](api-authentication.md).

## Next Steps

- [API Mode Authentication](api-authentication.md) -- Configure OBO, client credentials, and pass-through delegation
- [Authentication Tutorial](auth-tutorial.md) -- Set up GitHub and Entra authentication
- [Exec Provider Tutorial](exec-provider-tutorial.md) -- Cross-platform shell execution
- [Logging & Debugging Tutorial](logging-tutorial.md) -- Control log verbosity, format, and output
- [Cache Tutorial](cache-tutorial.md) -- Manage cached data
- [Catalog Tutorial](catalog-tutorial.md) -- Build and manage solutions in the catalog
