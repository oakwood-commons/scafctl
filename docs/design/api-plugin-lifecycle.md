---
title: "API Plugin Lifecycle"
weight: 30
---

# API Plugin Lifecycle

How the scafctl REST API server manages provider plugin processes.

---

## Problem

The CLI spawns plugin processes per-invocation and kills them when the command
exits. This model does not work for a long-running API server because:

- Spawning a gRPC process per HTTP request is expensive (~5-50ms fork+exec,
  plus Go runtime bootstrap at ~10-20MB per process).
- Under load, per-request spawning leads to PID exhaustion, file descriptor
  limits, and memory pressure.
- If cleanup fails (crash, timeout), zombie processes accumulate.

---

## Solution: Plugin Pool

The API server uses a **plugin pool** (`pkg/plugin.Pool`) that manages shared,
long-lived plugin processes with lazy initialization and idle eviction.

### Lifecycle Overview

~~~text
Server Start
  |-- Pre-load official providers (exec, directory, git, ...)
  |-- Adopt pre-loaded clients into the Pool
  '-- Start idle eviction goroutine

Request (POST /solutions/run, /render, /dryrun)
  |-- Load solution from URL
  |-- pool.Ensure(ctx, sol.Bundle.Plugins)
  |     |-- Plugin already in pool and healthy? --> no-op (hot path, ~100ns)
  |     |-- Plugin not in pool? --> fetch binary --> spawn --> register
  |     '-- Plugin dead? --> evict, re-spawn on next Ensure
  |-- Execute solution using shared provider registry
  '-- Return response (plugin stays alive for subsequent requests)

Idle (no requests for idle timeout)
  '-- Eviction goroutine: kill idle plugins, unregister from registry

Server Shutdown
  '-- pool.Shutdown() --> kill all managed plugin processes
~~~

---

## Two Categories of Plugins

### Official Providers (Eager)

The 10 official providers extracted from the monorepo (directory, env, exec,
git, github, hcl, identity, metadata, secret, sleep) are pre-loaded at server
startup. Their gRPC processes start immediately and live for the server's
lifetime (unless idle-evicted).

This ensures zero latency on first request and fail-fast behavior -- if a
provider binary is missing or broken, the server logs a warning at startup.

### External Plugins (Lazy)

External plugins declared in a solution's `bundle.plugins` section are loaded
on-demand when a request references them. The pool:

1. Checks if the plugin is already running and healthy.
2. If not, fetches the binary from the catalog, spawns the process, registers
   its providers into the shared registry.
3. Keeps the process alive for subsequent requests.
4. Evicts after the idle timeout if no requests use it.

---

## Pool Configuration

The pool accepts three options, which can be tuned via server options:

| Option | Default | Description |
|--------|---------|-------------|
| `idleTimeout` | 5 minutes | Kill plugins unused for this duration. 0 disables eviction. |
| `maxPlugins` | 50 | Maximum concurrent external plugin processes. 0 is unlimited. |
| `healthCheckInterval` | 30 seconds | Background health check frequency. 0 checks only on use. |

---

## Concurrency Model

- The pool uses a `sync.Mutex` for the entry map and per-entry mutexes for
  state transitions.
- gRPC is multiplexed -- a single plugin process handles concurrent requests
  from multiple goroutines without spawning additional processes.
- If two requests call `pool.Ensure()` for the same new plugin simultaneously,
  only one spawn occurs. The second waiter blocks on a `ready` channel until
  the first completes.
- Entries with `refCount > 0` (acquired by in-flight requests) are never
  evicted.

---

## Health and Recovery

- **Ping**: `pool.Ping(ctx, name)` issues a lightweight `GetProviders` RPC to
  verify the plugin process is alive.
- **Dead detection**: If a gRPC call fails, the pool marks the entry as dead,
  unregisters its providers, and kills the process.
- **Recovery**: The next `pool.Ensure()` call for a dead plugin evicts the old
  entry and spawns a fresh process.

---

## Shutdown

`pool.Shutdown()` is called from `Server.Shutdown()`:

1. Stops the eviction goroutine.
2. Marks the pool as closed (rejects new `Ensure` calls with `ErrPoolClosed`).
3. Kills all managed plugin processes.

---

## Error Handling

| Scenario | Behavior | HTTP Status |
|----------|----------|-------------|
| Plugin binary not in catalog | `Ensure()` returns error | 502 Bad Gateway |
| Plugin process crashes on spawn | Entry marked dead, error returned | 502 Bad Gateway |
| Plugin crashes mid-request | gRPC error propagates; pool marks dead | 502 Bad Gateway |
| Pool at max capacity | `ErrPoolFull` returned | 503 Service Unavailable |
| Context cancelled during fetch | Respects `ctx.Done()`, cleans up | 504 Gateway Timeout |
| Plugin already registered (builtin) | `Ensure()` is a no-op | -- |

---

## Comparison With CLI

| Aspect | CLI | API Server |
|--------|-----|------------|
| Plugin scope | Per-invocation | Shared across requests |
| Official providers | Fetched per `scafctl run` | Pre-loaded at startup, adopted into pool |
| External plugins | Fetched + killed per run | Lazy-loaded, pooled, idle-evicted |
| Cleanup | `defer` chain in `prepare.Solution()` | `pool.Shutdown()` on server stop |
| Concurrency | Single-threaded | gRPC multiplexing, mutex-protected |

---

## Key Files

| File | Purpose |
|------|---------|
| `pkg/plugin/pool.go` | Pool implementation |
| `pkg/plugin/pool_test.go` | Pool unit tests and benchmarks |
| `pkg/api/server.go` | `WithServerPluginPool` option, pool shutdown |
| `pkg/api/context.go` | `PluginPool` field on `HandlerContext` |
| `pkg/api/endpoints/solutions.go` | `pool.Ensure()` calls in run/render/dryrun |
| `pkg/cmd/scafctl/serve/serve.go` | Pool creation, official provider adoption |

---

## Design Decisions

**Why not per-request spawn (Terraform model)?**
Terraform is a CLI tool -- each `terraform apply` is a short-lived process.
Spawning per HTTP request recreates the CGI anti-pattern: expensive, not
scalable, and prone to resource exhaustion.

**Why not pure long-lived without eviction (Grafana model)?**
For official providers this is fine (they are always needed). But external
plugins may be used by a single solution -- keeping them alive indefinitely
wastes resources. Idle eviction balances availability with resource efficiency.

**Why a single shared registry instead of per-request clones?**
Providers are stateless -- the same gRPC process safely handles concurrent
calls. Cloning the registry per request would add overhead without benefit.
The first-loaded-wins behavior for version conflicts matches CLI semantics.

---

## Security

Running external executables from HTTP requests introduces significant risk.
The API server applies five layered mitigations, all configured under
`apiServer.plugins` in `config.yaml`:

~~~yaml
apiServer:
  plugins:
    allowExternal: false          # default -- only official providers
    allowedPlugins: [my-plugin]   # explicit name allowlist
    allowedCatalogs: [internal]   # restrict fetch sources
~~~

### 1. External Plugins Disabled by Default

`allowExternal: false` (the default) causes `pool.Ensure()` to reject any
plugin not pre-loaded (adopted) at startup. Official providers are adopted
unconditionally and bypass this check.

### 2. Plugin Name Allowlist

When `allowedPlugins` is non-empty, only listed names may be loaded. Requests
referencing unlisted plugins receive a `403 Forbidden` response. Adopted
(official) plugins always bypass the allowlist.

### 3. Catalog Allowlist

`allowedCatalogs` restricts which configured catalogs the fetcher may pull
binaries from. If a solution references a plugin in an unlisted catalog, the
fetch is rejected before any download occurs.

### 4. Environment Variable Sanitization

Plugin processes spawned by the pool inherit only a fixed allowlist of
environment variables: PATH, HOME, TMPDIR, USER, LANG, TZ, and the
go-plugin protocol variables (ports, magic cookie). All other variables
(including credentials, tokens, and application-prefixed vars) are stripped.
This prevents credential leakage from the server environment into untrusted
plugin binaries. Controlled by the `WithSanitizedEnv()` client option, applied
automatically in `pool.spawn()`.

### 5. Adopted Plugins Bypass Security Checks

Official providers adopted at startup (`pool.Adopt()`) are trusted and skip
the allowlist/external checks. This ensures they always work regardless of
security policy configuration.

### Key Files

| File | Security Role |
|------|---------------|
| `pkg/plugin/pool.go` | `WithDisableExternal`, `WithAllowedPlugins` options |
| `pkg/plugin/client.go` | `WithSanitizedEnv`, `safePluginEnv`, `pluginCmdSanitized` |
| `pkg/plugin/fetcher.go` | `AllowedCatalogs`, `checkCatalogAllowed` |
| `pkg/config/types.go` | `APIPluginConfig` struct |
| `pkg/cmd/scafctl/serve/serve.go` | Wires config values into pool and fetcher |
