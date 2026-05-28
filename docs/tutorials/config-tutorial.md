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
# Full config (YAML format)
scafctl config view

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
# Full config (YAML format)
scafctl config view

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

See where each value comes from (file, environment, default):

{{< tabs "config-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
scafctl config show
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl config show
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
scafctl catalog remote set-default my-catalog
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog remote set-default my-catalog
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

{{< tabs "config-tutorial-cmd-11" >}}
{{% tab "Bash" %}}
```bash
# Pretty-printed JSON Schema
scafctl config schema

# Minified (for piping)
scafctl config schema --compact
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Pretty-printed JSON Schema
scafctl config schema

# Minified (for piping)
scafctl config schema --compact
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
```

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
| `httpClient.allowPrivateIPs` | bool | `false` | Allow requests to private/loopback IPs (SSRF protection) |
| `settings.requireSecureKeyring` | bool | `false` | Fail if OS keyring unavailable instead of insecure fallback |
| `resolver.timeout` | duration | `5m` | Overall resolver timeout |
| `resolver.concurrency` | int | `4` | Max parallel resolver execution |
| `action.timeout` | duration | `10m` | Overall action timeout |
| `action.concurrency` | int | `4` | Max parallel action execution |
| `auth.entra.*` | object | — | Entra (Azure AD) auth handler config |
| `auth.github.hostname` | string | `github.com` | GitHub hostname (or GHES hostname) |
| `auth.github.clientId` | string | built-in | OAuth App client ID |
| `auth.github.defaultScopes` | []string | `[gist, read:org, repo, workflow]` | Default OAuth scopes |

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
scafctl catalog remote set-default staging

# Increase logging for debugging
scafctl config set logging.level debug

# After debugging, reset
scafctl config unset logging.level
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Use staging catalog
scafctl catalog remote set-default staging

# Increase logging for debugging
scafctl config set logging.level debug

# After debugging, reset
scafctl config unset logging.level
```
{{% /tab %}}
{{< /tabs >}}

## API Server: Token Delegation

The `apiServer` section includes configuration for token delegation when running in API mode (`scafctl serve`). These settings control how the server exchanges or forwards tokens to downstream providers.

### Server Identity (Entra ID)

Configure the server's identity for OBO and client credentials flows:

{{< tabs "config-tutorial-cmd-16" >}}
{{% tab "Bash" %}}
```bash
# Set up Entra identity for token delegation
scafctl config set apiServer.identity.entra.tenantId "your-tenant-id"
scafctl config set apiServer.identity.entra.clientId "your-client-id"
scafctl config set apiServer.identity.entra.credential.type "secret"
scafctl config set apiServer.identity.entra.credential.clientSecret "env://SCAFCTL_API_ENTRA_CLIENT_SECRET"

# Configure allowed delegation flows
scafctl config set apiServer.identity.entra.allowedFlows.flows '["obo", "client_credentials"]'

# View the identity config
scafctl config get apiServer.identity
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Set up Entra identity for token delegation
scafctl config set apiServer.identity.entra.tenantId "your-tenant-id"
scafctl config set apiServer.identity.entra.clientId "your-client-id"
scafctl config set apiServer.identity.entra.credential.type "secret"
scafctl config set apiServer.identity.entra.credential.clientSecret "env://SCAFCTL_API_ENTRA_CLIENT_SECRET"

# Configure allowed delegation flows
scafctl config set apiServer.identity.entra.allowedFlows.flows '["obo", "client_credentials"]'

# View the identity config
scafctl config get apiServer.identity
```
{{% /tab %}}
{{< /tabs >}}

The credential `clientSecret` field uses `SecretRef` URI format:
- `env://VAR_NAME` -- reads from an environment variable
- `file:///path/to/secret` -- reads from a file

### Token Pass-Through

Configure which provider-specific tokens can be forwarded from API request headers:

{{< tabs "config-tutorial-cmd-17" >}}
{{% tab "Bash" %}}
```bash
# Allow GitHub and Artifactory tokens to be passed through
scafctl config set apiServer.tokenPassThrough.allowedHeaders '["Github", "Artifactory"]'

# View the token pass-through config
scafctl config get apiServer.tokenPassThrough
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Allow GitHub and Artifactory tokens to be passed through
scafctl config set apiServer.tokenPassThrough.allowedHeaders '["Github", "Artifactory"]'

# View the token pass-through config
scafctl config get apiServer.tokenPassThrough
```
{{% /tab %}}
{{< /tabs >}}

When configured, callers send tokens via `X-Authorization-<Suffix>` headers (e.g., `X-Authorization-Github`). The server makes these available to providers via the `authProvider` input field. When `tokenPassThrough` is omitted entirely, only `Github` is allowed by default.

For a complete guide, see the [Token Delegation Tutorial](token-delegation-tutorial.md).

## Next Steps

- [Token Delegation Tutorial](token-delegation-tutorial.md) -- Configure OBO, client credentials, and pass-through delegation
- [Authentication Tutorial](auth-tutorial.md) — Set up GitHub and Entra authentication
- [Exec Provider Tutorial](exec-provider-tutorial.md) — Cross-platform shell execution
- [Logging & Debugging Tutorial](logging-tutorial.md) — Control log verbosity, format, and output
- [Cache Tutorial](cache-tutorial.md) — Manage cached data
- [Catalog Tutorial](catalog-tutorial.md) — Build and manage solutions in the catalog
