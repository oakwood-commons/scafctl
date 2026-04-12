---
title: "Plugin Development Guide"
weight: 130
---

# Plugin Development Guide

> [!NOTE]
> **This page is an overview.** Detailed plugin development instructions are now part of each extension type's development guide.

## What is a Plugin?

A plugin is a standalone executable that extends scafctl by communicating over gRPC using [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin). Plugins run in separate processes, providing crash isolation and independent distribution.

scafctl supports two types of plugins:

| Plugin Type | Artifact Kind | Interface | Guide |
|-------------|---------------|-----------|-------|
| **Provider Plugin** | `provider` | `plugin.ProviderPlugin` (8 methods) | [Provider Development Guide -- Delivering as a Plugin](provider-development.md#delivering-as-a-plugin) |
| **Auth Handler Plugin** | `auth-handler` | `plugin.AuthHandlerPlugin` (9 methods) | [Auth Handler Development Guide -- Delivering as a Plugin](auth-handler-development.md#delivering-as-a-plugin) |

## Architecture

```mermaid
flowchart LR
  A["scafctl<br/>- Discovers plugin<br/>- Configures providers<br/>- Calls extensions<br/>- Manages lifecycle"] <-- "gRPC" --> B["Your Plugin<br/>- Implements gRPC<br/>- Exposes handlers<br/>- Handles execution<br/>- Calls HostService"]
```

Each plugin binary exposes **one** extension type (provider OR auth handler). The handshake cookie determines which type the host expects.

### Plugin Lifecycle

1. **Discovery** -- scafctl finds the plugin binary (via catalog auto-fetch or directory scanning)
2. **Load** -- scafctl starts the plugin process and negotiates protocol version
3. **Configure** -- scafctl calls `ConfigureProvider` once per provider with host settings (quiet, color, binary name, HostService broker ID)
4. **Execute** -- scafctl invokes providers as needed (unary or streaming)
5. **Callbacks** -- plugins can call back to the host for secrets and auth tokens via the `HostService` gRPC service
6. **Shutdown** -- scafctl terminates the plugin process when done

## Plugin Discovery

scafctl resolves plugins through two mechanisms:

1. **Catalog Auto-Fetch (Recommended)** — Declare plugins in `bundle.plugins` and scafctl fetches, caches, and loads them automatically:

   ```yaml
   spec:
     bundle:
       plugins:
         - name: my-plugin
           kind: provider          # or "auth-handler"
           version: ">=1.0.0"
   ```

2. **Directory Scanning** — For local development, place plugin binaries in the plugin cache:

{{< tabs "plugin-development-cmd-1" >}}
{{% tab "Bash" %}}
```bash
   mkdir -p "$(scafctl paths cache)/plugins"
   cp my-plugin "$(scafctl paths cache)/plugins/"
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
   $pluginDir = "$(scafctl paths cache)/plugins"
   New-Item -ItemType Directory -Force -Path $pluginDir
   Copy-Item my-plugin $pluginDir
```
{{% /tab %}}
{{< /tabs >}}

## Plugin CLI Commands

{{< tabs "plugin-development-cmd-2" >}}
{{% tab "Bash" %}}
```bash
# Pre-fetch plugins declared in a solution
scafctl plugins install -f my-solution.yaml

# List cached plugin binaries
scafctl plugins list

# Push to a remote registry
scafctl catalog push my-plugin@1.0.0 --catalog ghcr.io/myorg
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Pre-fetch plugins declared in a solution
scafctl plugins install -f my-solution.yaml

# List cached plugin binaries
scafctl plugins list

# Push to a remote registry
scafctl catalog push my-plugin@1.0.0 --catalog ghcr.io/myorg
```
{{% /tab %}}
{{< /tabs >}}

## Next Steps

- [Extension Concepts](extension-concepts.md) — Provider vs Auth Handler vs Plugin terminology
- [Provider Development Guide](provider-development.md) — Build providers (builtin + plugin)
- [Auth Handler Development Guide](auth-handler-development.md) — Build auth handlers (builtin + plugin)
- [Plugin Auto-Fetching Tutorial](plugin-auto-fetch-tutorial.md) — Catalog-based distribution
