---
title: "Extension Concepts"
weight: 115
---

# Extension Concepts

scafctl is built on two types of **extensions**: **providers** and **auth handlers**. Each can be delivered as a **builtin** (compiled into the scafctl binary) or as a **plugin** (a standalone binary communicating over gRPC).

This page defines the core terminology and links to the detailed development guides.

## Terminology

| Term | Definition |
|------|-----------|
| **Provider** | A stateless execution unit that performs a single operation: fetching data (`from`), transforming values (`transform`), validating inputs (`validation`), executing side effects (`action`), or authenticating requests (`authentication`). Providers are the building blocks used inside solution resolvers. |
| **Auth Handler** | A stateful authentication manager that handles identity verification, credential storage, token acquisition, and request injection. Auth handlers manage OAuth flows, cache tokens across invocations, and are used by providers (like `http`) to authenticate outgoing requests. |
| **Builtin** | An extension compiled directly into the scafctl binary. Builtin extensions are registered at startup and require no external binaries. Contributing a builtin extension means adding code to the scafctl repository. |
| **Plugin** | An extension delivered as a standalone executable that communicates with scafctl over gRPC using [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin). Plugins run as separate processes, can be written in any gRPC-capable language, and are distributed independently via OCI catalogs, Go modules, or binary releases. |
| **Extension** | Umbrella term for any provider or auth handler, whether builtin or plugin. |
| **Capability** | A declared feature of a provider (e.g., `from`, `transform`, `action`) or auth handler (e.g., `scopes_on_login`, `tenant_id`). Capabilities let scafctl adapt behavior dynamically without hardcoded knowledge of each extension. |

## Extension Matrix

| | **Builtin** | **Plugin** |
|---|---|---|
| **Provider** | Compiled into scafctl; 10 built-in providers (http, cel, static, parameter...) | Standalone gRPC binary; auto-fetched from OCI catalogs (10 official: exec, git, env...) |
| **Auth Handler** | Compiled into scafctl; 3 built-in handlers (entra, github, gcp) | Standalone gRPC binary; auto-fetched from OCI catalogs |

## When to Choose Builtin vs Plugin

| Factor | Builtin | Plugin |
|--------|---------|--------|
| **Distribution** | Ships with scafctl | Distributed independently |
| **Language** | Go only | Any language with gRPC support |
| **Dependency management** | Part of scafctl's `go.mod` | Isolated dependencies |
| **Process isolation** | Runs in-process | Separate process (crash isolation) |
| **Update cycle** | Tied to scafctl releases | Independent release cadence |
| **Use case** | Core functionality, general-purpose | Third-party integrations, proprietary logic |
| **Contributing** | PR to scafctl repo | Publish to any OCI registry |

### Built-in Boundary Rule

A provider **must** stay built-in when:

1. It imports `pkg/celexp` or `pkg/gotmpl` (core engine coupling)
2. Its execution is sub-microsecond with zero I/O (serialization overhead dominates)
3. It requires direct access to internal host state that cannot cross a gRPC boundary

Everything else defaults to plugin. See [Plugins Design -- Built-in Boundary Rule](../design/plugins.md#built-in-boundary-rule) for the full rationale and provider table.

## Key Differences: Providers vs Auth Handlers

| Aspect | Provider | Auth Handler |
|--------|----------|-------------|
| **Interface** | 2 methods: `Descriptor()`, `Execute()` | 8+ methods: `Login()`, `Logout()`, `Status()`, `GetToken()`, `InjectAuth()`, ... |
| **State** | Stateless — each execution is independent | Stateful — manages cached tokens, refresh tokens, sessions |
| **Used by** | Solution resolvers (via `from`, `transform`, etc.) | Providers (e.g., `http` provider calls `InjectAuth()` on requests) |
| **Registry** | `provider.Registry` (versioned, overwrite-protected) | `auth.Registry` (name-keyed) |
| **Capabilities** | `from`, `transform`, `validation`, `action`, `authentication` | `scopes_on_login`, `scopes_on_token_request`, `tenant_id`, `hostname`, `federated_token` |
| **Plugin artifact kind** | `provider` | `auth-handler` |

## Plugin Communication

Both provider plugins and auth handler plugins use [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin) with gRPC:

```
┌─────────────────────┐        gRPC        ┌──────────────────────┐
│      scafctl        │◄──────────────────►│    Your Plugin       │
│  (host process)     │                     │  (plugin process)    │
│                     │                     │                      │
│  - Discovers plugin │                     │  - Implements gRPC   │
│  - Manages lifecycle│                     │    service interface  │
│  - Registers in     │                     │  - Exposes extensions│
│    appropriate      │                     │  - Handles execution │
│    registry         │                     │                      │
└─────────────────────┘                     └──────────────────────┘
```

Each plugin type has its own handshake and gRPC service:

| Plugin Type | Handshake Cookie | gRPC Service | Go Interface |
|------------|-----------------|--------------|-------------|
| Provider | `scafctl_provider_plugin` | `PluginService` | `ProviderPlugin` |
| Auth Handler | `scafctl_auth_handler_plugin` | `AuthHandlerService` | `AuthHandlerPlugin` |

A single plugin binary exposes **one type**: either providers or auth handlers, not both.

## Plugin Lifecycle

1. **Declaration** — plugin dependencies are declared in a solution's `bundle.plugins` section with a `kind` (`provider` or `auth-handler`)
2. **Resolution** — scafctl resolves version constraints against configured OCI catalogs
3. **Caching** — binaries are downloaded and cached at `$XDG_CACHE_HOME/scafctl/plugins/`
4. **Loading** — scafctl starts the plugin process and performs a gRPC handshake
5. **Discovery** — scafctl queries the plugin for available extensions (`GetProviders` or `GetAuthHandlers`)
6. **Registration** — each extension is wrapped and registered in the appropriate registry
7. **Execution** — extensions are used like builtins (transparent to solution authors)
8. **Cleanup** — plugin processes are terminated when scafctl exits

## Development Guides

| Guide | What It Covers |
|-------|---------------|
| [Provider Development Guide](provider-development.md) | Building providers — both builtin (compiled into scafctl) and plugin (standalone gRPC binary) |
| [Auth Handler Development Guide](auth-handler-development.md) | Building auth handlers — both builtin (compiled into scafctl) and plugin (standalone gRPC binary) |
| [Plugin Auto-Fetching Tutorial](plugin-auto-fetch-tutorial.md) | How consumers auto-fetch plugins from OCI catalogs at runtime |
| [Provider Reference](provider-reference.md) | Complete documentation for all built-in providers |
| [Authentication Tutorial](auth-tutorial.md) | Using authentication in solutions (consumer perspective) |
