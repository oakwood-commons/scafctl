---
title: "Implementation Gaps"
weight: 99
---

# Implementation Gaps

> **Generated:** 2026-02-24
>
> This document tracks features described in design documents that are **not yet fully implemented**. Fully implemented features are omitted. For complete design specs, see the linked source documents.

---

## Deferred (Explicitly Planned for Future)

### Conditional Retry (`retryIf`)

**Source:** [actions.md](actions.md)

Retry policies with a `retryIf` condition to retry only on specific error types. Requires an `__error` namespace with `message`, `statusCode`, `code`, `retryable`, and `attempt` fields.

### Matrix Strategy

**Source:** [actions.md](actions.md)

Parallel expansion across multiple dimensions (e.g., region × environment). Supports `exclude` and `include` modifiers. Only available in `workflow.actions`.

### Action Alias

**Source:** [actions.md](actions.md)

Short alias for action references in expressions (e.g., `config.results.endpoint` instead of `__actions.fetchConfiguration.results.endpoint`).

### Auth Claims Provider

**Source:** [auth.md](auth.md)

A dedicated provider to expose authentication claims (tenant ID, subject, scopes, etc.) for use in expressions and conditions.

### Future Catalog Artifact Types

**Source:** [catalog.md](catalog.md)

Additional artifact types beyond solutions, providers, and auth handlers — TBD as the system evolves.

### Extended Plugin Capabilities

**Source:** [plugins.md](plugins.md)

Plugins currently expose providers only. Future capability types may include provider sets, schemas, and validation helpers.

### Future Provider Capabilities

**Source:** [providers.md](providers.md)

The capability model is designed for extension. Future capabilities may include: `caching`, `streaming`, `batch`, `webhook`.

### Catalog Regression Testing (`scafctl pipeline`)

**Source:** [functional-testing.md](functional-testing.md)

A command that executes functional tests across solutions in a remote catalog, enabling validation that scafctl changes don't break existing solutions.

### `run_solution` MCP Tool

**Source:** [mcp-server.md](mcp-server.md)

Phases 9–10 of the MCP server design — executing solutions through the MCP server (dry-run default, full execution with confirmation). Explicitly deferred to a future release.

---

## Partially Implemented

### Plugin Catalog Distribution & Runtime Loading

**Source:** [plugins.md](plugins.md), [solutions.md](solutions.md), [catalog.md](catalog.md)

**What's done:**
- gRPC plugin architecture (`hashicorp/go-plugin`): `pkg/plugin/`
- Protobuf definitions, client/server, provider wrapper
- `bundle.plugins` type on `Solution` struct
- Plugin vendoring at build time: `pkg/solution/bundler/vendor_plugins.go`
- Version constraint checking: `pkg/solution/bundler/plugin.go`
- Lock file integration

**What's missing:**
- `build plugin` CLI command (only `build solution` exists)

**What's been implemented:**
- Dynamic plugin auto-fetching from remote catalogs at runtime (`pkg/plugin/fetcher.go`, `pkg/catalog/plugin_fetcher.go`)
- Multi-platform support via OCI annotations (`pkg/plugin/platform.go`, `pkg/catalog/plugin_fetcher.go`)
- Content-addressed plugin cache (`pkg/plugin/cache.go`)
- Catalog chain resolution — local-first, then remote OCI catalogs (`pkg/catalog/chain.go`, `pkg/catalog/chain_builder.go`)
- `scafctl plugins install` and `scafctl plugins list` CLI commands (`pkg/cmd/scafctl/plugins/`)
- Wired into solution execution via `prepare.Solution()` options
- Auth handler plugin runtime: `AuthHandlerPlugin` interface, gRPC service (`AuthHandlerService` with 7 RPCs), client/server/wrapper (`pkg/plugin/grpc_auth.go`, `pkg/plugin/wrapper_auth.go`), fetcher integration (`RegisterFetchedAuthHandlerPlugins`), preparation pipeline (`prepare.WithAuthRegistry`)

### GCP Auth Handler Documentation

**Source:** [gcp-auth-handler.md](gcp-auth-handler.md)

**What's done:**
- All code: 5 auth flows (ADC/PKCE, service account, metadata server, workload identity, gcloud ADC), impersonation, CLI wiring
- Most unit tests (9 test files exist)

**What's missing:**
- 3 unit test files: `adc_flow_test.go`, `impersonation_test.go`, `metadata_test.go`
- GCP auth tutorial (only `gcp-custom-oauth-tutorial.md` exists, not a general GCP auth tutorial)
- GCP auth examples in `examples/auth/`

### CLI Command Structure vs Design Tree

**Source:** [cli-contributing.md](cli-contributing.md)

**What's done:**
- All catalog operations are implemented under `scafctl catalog <verb>` (push, pull, tag, save, load, inspect, list, delete, prune)

**Divergence from design:**
- The design tree in `cli-contributing.md` shows `push`, `pull`, `inspect`, `tag`, `save`, `load` as top-level verbs (e.g., `scafctl push solution`). In practice, they live under `scafctl catalog push`. The design tree should be updated to reflect the actual structure, or the commands should be restructured.

> **Note:** The `cli-contributing.md` design tree also references `build plugin`, `push plugin`, `pull plugin`, `inspect plugin`, `tag plugin`, `save plugin` which don't exist. These are blocked on the plugin catalog distribution work above.

---

## Not Implemented (Excluding State)

> **Note:** [state.md](state.md) is excluded from this document — it is still under active design discussion.

### External Auth Handlers via Plugin/Catalog

**Source:** [auth.md](auth.md)

~~The design describes loading custom auth handlers (e.g., Okta) from the catalog via the go-plugin mechanism, similar to how provider plugins work. No plugin-based auth handler loading exists in code.~~

**Now Implemented.** Auth handler plugins are fully supported:
- `AuthHandlerPlugin` interface (`pkg/plugin/interface.go`) with 7 methods
- gRPC service (`AuthHandlerService`) with server-side streaming for Login device-code relay
- gRPC server/client (`pkg/plugin/grpc_auth.go`), wrapper adapter (`pkg/plugin/wrapper_auth.go`)
- `ServeAuthHandler` entry point, `NewAuthHandlerClient`, `DiscoverAuthHandlers`
- `RegisterFetchedAuthHandlerPlugins` in `pkg/plugin/fetcher.go`
- Wired into preparation pipeline via `prepare.WithAuthRegistry`
- See the [Auth Handler Development Guide](../tutorials/auth-handler-development.md#delivering-as-a-plugin)


