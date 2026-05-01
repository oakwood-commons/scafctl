---
title: "Plugins"
weight: 9
---

# Plugins

## Purpose

Plugins are the extension mechanism for scafctl. They allow external binaries to contribute functionality to the system in a controlled, versioned, and discoverable way.

The primary purpose of plugins is to supply providers and auth handlers. "Plugin" is an internal implementation term - users interact with "providers" and "auth handlers" as catalog artifact kinds.

---

## Terminology

- **Plugin**: Internal term for a go-plugin binary. Not exposed to users.
- **Provider Artifact**: A plugin binary distributed via the catalog that exposes one or more providers. Users push/pull "providers" not "plugins".
- **Auth Handler Artifact**: A plugin binary distributed via the catalog that exposes one or more auth handlers.

---

## What a Plugin Is

A plugin is an external process that implements one or more scafctl extension interfaces and communicates with scafctl over RPC.

Plugins are:

- Discovered and loaded at runtime
- Versioned independently from scafctl
- Isolated from the core process
- Capable of exposing multiple providers or auth handlers

scafctl uses [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin) to manage plugin lifecycle, transport, and isolation.

---

## What a Plugin Is Not

A plugin is not:

- A provider itself
- A resolver
- An action
- A workflow engine
- A scripting environment

Plugins do not participate directly in execution graphs. They only expose capabilities that scafctl invokes.

---

## Primary Use: Provider and Auth Handler Distribution

Plugins exist primarily to distribute providers and auth handlers.

Under this model:

- Providers define behavior (data fetching, transformations, actions)
- Auth handlers define authentication flows (Entra, GitHub, custom identity providers)
- Plugin binaries package these capabilities
- scafctl orchestrates execution

A plugin may expose one or more providers OR one or more auth handlers (not both).

---

## Catalog Artifact Kinds

When distributed via the catalog, plugins are categorized by their purpose:

| Artifact Kind | Description | Repository Path |
|--------------|-------------|-----------------|
| `provider` | go-plugin binary exposing providers | `/providers/` |
| `auth-handler` | go-plugin binary exposing auth handlers | `/auth-handlers/` |

Users interact with these as distinct artifact kinds:

```bash
# Push a provider artifact
scafctl catalog push aws-provider@1.0.0 --catalog ghcr.io/myorg

# Pull a provider artifact  
scafctl catalog pull ghcr.io/myorg/providers/aws-provider@1.0.0

# Push an auth handler artifact
scafctl catalog push okta-handler@1.0.0 --catalog ghcr.io/myorg

# Pull an auth handler artifact
scafctl catalog pull ghcr.io/myorg/auth-handlers/okta-handler@1.0.0
```

---

## Why Plugins Exist (Instead of Built-ins Only)

Plugins exist to:

- Avoid baking all providers into scafctl
- Enable third-party and internal extensions
- Allow providers and auth handlers to evolve independently
- Isolate failures and crashes
- Support multiple languages via gRPC boundaries
- Keep the core binary small and stable

This mirrors patterns used by Terraform, Vault, Nomad, and Packer.

---

## Built-in Boundary Rule

A provider stays built-in when **any** of the following apply:

1. **Core engine dependency** -- The provider imports `pkg/celexp` or
   `pkg/gotmpl`. These packages are tightly coupled to the host's expression
   and template engines; serializing their state across a gRPC boundary is
   impractical.
2. **Serialization-dominated cost** -- The provider's execution is
   sub-microsecond with zero I/O and no external dependencies. The ~2-6ms
   gRPC round-trip overhead would represent a 10,000x+ penalty, making the
   plugin boundary pure waste.
3. **Orchestration coupling** -- The provider requires direct access to
   internal host state (e.g., the solution graph, resolver context mutations)
   that cannot be exposed through `HostService` callbacks.

All other providers default to plugin distribution. This keeps the core binary
small while ensuring providers that are inherently host-coupled or
performance-critical remain in-process.

### Current Built-in Providers

| Provider | Reason |
|----------|--------|
| `cel` | Imports `pkg/celexp` (rule 1) |
| `go-template` | Imports `pkg/gotmpl` (rule 1) |
| `validation` | Imports `pkg/celexp` for CEL-based validation rules (rule 1) |
| `static` | Sub-microsecond, zero I/O data passthrough (rule 2) |
| `parameter` | Sub-microsecond, zero I/O context lookup (rule 2) |
| `http` | General-purpose; kept built-in for bootstrap (no plugin infra needed to fetch plugins) |
| `file` | General-purpose; kept built-in for bootstrap |
| `debug` | Development utility, zero external deps |
| `message` | Terminal output only, coupled to host writer |
| `solution` | Orchestration coupling -- invokes nested solution runs (rule 3) |

---

## Plugin Architecture

### Dependencies

- **External**: hashicorp/go-plugin (gRPC-based plugin system)
- **External**: google.golang.org/grpc (gRPC for plugin communication)
- **External**: google.golang.org/protobuf (Protocol buffers)

scafctl uses go-plugin with gRPC-based handshake.

Protocol buffer definition for plugin communication (abridged -- see `pkg/plugin/proto/plugin.proto` for full definition).

```protobuf
syntax = "proto3";
package plugin;
option go_package = "github.com/oakwood-commons/scafctl/pkg/plugin/proto";

// PluginService is the provider plugin service.
service PluginService {
  rpc GetProviders(GetProvidersRequest) returns (GetProvidersResponse);
  rpc GetProviderDescriptor(GetProviderDescriptorRequest) returns (GetProviderDescriptorResponse);
  rpc ConfigureProvider(ConfigureProviderRequest) returns (ConfigureProviderResponse);
  rpc ExecuteProvider(ExecuteProviderRequest) returns (ExecuteProviderResponse);
  rpc ExecuteProviderStream(ExecuteProviderRequest) returns (stream ExecuteProviderStreamChunk);
  rpc DescribeWhatIf(DescribeWhatIfRequest) returns (DescribeWhatIfResponse);
  rpc ExtractDependencies(ExtractDependenciesRequest) returns (ExtractDependenciesResponse);
  rpc StopProvider(StopProviderRequest) returns (StopProviderResponse);
}

// AuthHandlerService is the auth handler plugin service.
service AuthHandlerService {
  rpc GetAuthHandlers(GetAuthHandlersRequest) returns (GetAuthHandlersResponse);
  rpc ConfigureAuthHandler(ConfigureAuthHandlerRequest) returns (ConfigureAuthHandlerResponse);
  rpc Login(LoginRequest) returns (LoginResponse);
  rpc Logout(LogoutRequest) returns (LogoutResponse);
  rpc GetStatus(GetStatusRequest) returns (GetStatusResponse);
  rpc GetToken(GetTokenRequest) returns (GetTokenResponse);
  rpc ListCachedTokens(ListCachedTokensRequest) returns (ListCachedTokensResponse);
  rpc PurgeExpiredTokens(PurgeExpiredTokensRequest) returns (PurgeExpiredTokensResponse);
  rpc StopAuthHandler(StopAuthHandlerRequest) returns (StopAuthHandlerResponse);
}

// HostService is a callback service that plugins can invoke on the host.
// Registered via the go-plugin GRPCBroker so plugins can access host-side
// resources (secrets, auth) that cannot be serialized.
service HostService {
  rpc GetSecret(GetSecretRequest) returns (GetSecretResponse);
  rpc SetSecret(SetSecretRequest) returns (SetSecretResponse);
  rpc DeleteSecret(DeleteSecretRequest) returns (DeleteSecretResponse);
  rpc ListSecrets(ListSecretsRequest) returns (ListSecretsResponse);
  rpc GetAuthIdentity(GetAuthIdentityRequest) returns (GetAuthIdentityResponse);
  rpc ListAuthHandlers(ListAuthHandlersRequest) returns (ListAuthHandlersResponse);
  rpc GetAuthToken(GetAuthTokenRequest) returns (GetAuthTokenResponse);
}
```

### RPC Lifecycle

| Phase | RPC | Direction | When |
|-------|-----|-----------|------|
| Discovery | `GetProviders` | host -> plugin | On plugin load |
| Schema | `GetProviderDescriptor` | host -> plugin | After discovery |
| Configuration | `ConfigureProvider` | host -> plugin | Once after load, before any execution |
| Execution | `ExecuteProvider` | host -> plugin | On provider invocation |
| Streaming | `ExecuteProviderStream` | host -> plugin | When IOStreams are available |
| Dry-run | `DescribeWhatIf` | host -> plugin | During `run solution --dry-run` |
| Dependencies | `ExtractDependencies` | host -> plugin | During DAG construction |
| Shutdown | `StopProvider` | host -> plugin | During cancellation or cleanup |
| Callbacks | `HostService.*` | plugin -> host | Anytime during execution |

### Auth Handler RPC Lifecycle

| Phase | RPC | Direction | When |
|-------|-----|-----------|------|
| Discovery | `GetAuthHandlers` | host -> plugin | On plugin load |
| Configuration | `ConfigureAuthHandler` | host -> plugin | Once after load, before any auth calls |
| Authentication | `Login` | host -> plugin | On `auth login` |
| Authentication | `Logout` | host -> plugin | On `auth logout` |
| Authentication | `GetStatus` | host -> plugin | On `auth status` |
| Token | `GetToken` | host -> plugin | On token request |
| Token | `ListCachedTokens` | host -> plugin | On cache listing |
| Token | `PurgeExpiredTokens` | host -> plugin | On cache cleanup |
| Shutdown | `StopAuthHandler` | host -> plugin | During cancellation or cleanup |
| Callbacks | `HostService.*` | plugin -> host | Anytime during handler operations |

### ConfigureProvider

Called once after plugin load with host-side configuration:

- `quiet` / `no_color` -- terminal output preferences
- `binary_name` -- the CLI binary name (e.g. "scafctl" or an embedder name)
- `settings` -- extensible key-value JSON settings
- `host_service_id` -- GRPCBroker service ID for HostService callbacks
- `protocol_version` -- the host's plugin protocol version for feature detection

### ConfigureAuthHandler

Called once after auth handler plugin load, using the same configuration model as `ConfigureProvider`:

- `handler_name` -- which auth handler to configure (multi-handler plugins)
- `quiet` / `no_color` -- terminal output preferences
- `binary_name` -- the CLI binary name (e.g. "scafctl" or an embedder name)
- `settings` -- extensible key-value JSON settings
- `host_service_id` -- GRPCBroker service ID for HostService callbacks
- `protocol_version` -- the host's plugin protocol version for feature detection

The response includes a `protocol_version` field and optional `diagnostics` (repeated `Diagnostic` messages) for structured warning/error reporting.

### StopAuthHandler

Called during CLI shutdown or context cancellation to allow auth handler plugins to release resources gracefully. The RPC carries only the `handler_name`. If the plugin does not implement `StopAuthHandler` (older plugins), the host silently ignores the `Unimplemented` gRPC status.

### HostService Callbacks

Plugins that need host-side resources (secrets, auth tokens) use the `HostService` callback service. The host registers this service via the go-plugin GRPCBroker during plugin startup.

| Callback | Purpose |
|----------|---------|
| `GetSecret` / `SetSecret` / `DeleteSecret` / `ListSecrets` | Access the host's secret store |
| `GetAuthIdentity` | Retrieve identity claims from the host's auth registry |
| `ListAuthHandlers` | List available auth handlers (filtered by AllowedAuthHandlers) |
| `GetAuthToken` | Retrieve a valid access token from the host's auth registry |

Plugins access HostService via a client injected during `ConfigureProvider`.

### Diagnostics and Exit Codes

The `ExecuteProviderResponse` carries structured `Diagnostic` messages alongside the output. Each diagnostic includes a severity level, a summary, an optional detail string, and an optional attribute path. This allows plugins to report multiple warnings or errors in a single response.

An `exit_code` field on the response enables plugins to propagate typed exit codes back to the host. The host maps these to scafctl's `exitcode.ExitError` type so that callers can distinguish between different failure modes.

### Streaming Fallback

When IOStreams are available in the execution context, scafctl attempts `ExecuteProviderStream` first. If the plugin returns `ErrStreamingNotSupported`, the host transparently falls back to unary `ExecuteProvider`. This allows plugins to opt into streaming incrementally without breaking non-streaming hosts.

### Protocol Version Negotiation

scafctl sends a `protocol_version` field in both the `ConfigureProvider` and `ConfigureAuthHandler` requests. The current version is defined by the `PluginProtocolVersion` constant. Plugins can inspect this value to enable or disable features based on the host's capabilities. The plugin may return its own protocol version in the response for the host to check.

This is separate from the hashicorp/go-plugin handshake `ProtocolVersion`, which gates basic RPC compatibility. The plugin protocol version enables finer-grained feature detection after the connection is established.

### Descriptor Caching

The plugin client caches provider descriptors after the first `GetProviderDescriptor` call to avoid repeated gRPC round-trips. The cache is protected by a `sync.RWMutex` for safe concurrent access. Descriptors are immutable once loaded.

### Schema Round-Trip

Provider descriptors carry both structured `Schema`/`OutputSchemas` fields and raw JSON bytes (`raw_schema`, `raw_output_schemas`). The raw bytes are preferred for lossless round-tripping of `jsonschema.Schema`; the structured fields serve as a backward-compatible fallback for older plugins.

### Official Catalog

scafctl appends an official OCI catalog (`ghcr.io/oakwood-commons`) to the catalog chain by default. This provides access to officially maintained provider and auth handler plugins.

To disable:

```yaml
settings:
  disableOfficialCatalog: true
```

### Official Provider Auto-Resolution

scafctl maintains a hardcoded registry of 10 official providers distributed as
external plugins. When a solution references one of these providers and it is
not already registered (via `bundle.plugins` or a local plugin), scafctl
transparently fetches the provider from the official catalog at runtime.

**Official providers**: `directory`, `env`, `exec`, `git`, `github`, `hcl`,
`identity`, `metadata`, `secret`, `sleep`

**Resolution flow**:

1. Solution references a provider (e.g., `provider: exec`)
2. scafctl checks the local provider registry -- not found
3. scafctl checks the official provider list -- match found
4. Plugin binary is fetched from `ghcr.io/oakwood-commons/providers/exec`
5. Plugin is cached locally and registered
6. Execution continues normally

**Key properties**:

- Zero configuration required for local development
- A warning is logged when auto-resolution occurs (visibility without friction)
- The `--strict` flag on `run solution` / `run resolver` disables
  auto-resolution and requires explicit `bundle.plugins` declarations
- Embedders can override or extend the official list via
  `RootOptions.OfficialProviders`
- Air-gapped environments can disable with
  `settings.disableOfficialProviders: true`

**Strict mode** (`--strict`):

Use in CI/CD to enforce explicit plugin declarations:

~~~bash
scafctl run solution -f solution.yaml --strict
~~~

This fails if any provider would require auto-resolution, ensuring all
dependencies are declared in `bundle.plugins` for reproducibility.

Conceptually:

- scafctl discovers a plugin binary
- scafctl negotiates protocol version
- Plugin advertises capabilities
- scafctl registers providers exposed by the plugin
- Providers are invoked through gRPC

The plugin process lifecycle is managed entirely by scafctl.

---

## Plugin Capabilities

Plugins expose providers or auth handlers that support full lifecycles:

**Provider plugins** support:

- **Discovery**: Advertise provider names and descriptors
- **Configuration**: Receive host-side settings (quiet, color, binary name)
- **Execution**: Synchronous and streaming execution modes
- **Dry-run**: Describe what would happen without executing (WhatIf)
- **Dependency extraction**: Custom dependency graph participation
- **Host callbacks**: Access secrets and auth tokens from the host

**Auth handler plugins** support:

- **Discovery**: Advertise handler names, flows, and capabilities
- **Configuration**: Receive host-side settings and HostService access
- **Authentication**: Login, logout, and status checking
- **Token management**: Token retrieval, cache listing, and expiry purging
- **Host callbacks**: Access secrets and auth identity from the host

Future capability types may include:

- Provider sets
- Schemas
- Validation helpers

However, plugins should not become a generic execution environment. Any new capability must align with scafctl core concepts.

---

## Provider Exposure Model

A plugin declares the providers it implements.

Conceptual example:

~~~text
plugin: scafctl-provider-api
provides:
  - provider: api
  - provider: http
~~~

Each provider exposed by a plugin:

- Has a stable name and version
- Declares capabilities (from, transform, validation, authentication, action)
- Declares an input schema (with typed parameters)
- Declares output schemas per capability for the `Data` property within `ProviderOutput`
- Provides catalog metadata (description, category, tags, examples, maintainers)
- Is invoked deterministically

scafctl treats built-in providers and plugin-provided providers identically. All providers expose a `ProviderDescriptor` that includes identity, versioning, schemas, capabilities, and catalog information.

---

## Invocation Flow

When a provider is used:

1. scafctl resolves all inputs
2. scafctl validates inputs against the provider schema
3. scafctl invokes the provider via gRPC (streaming when IOStreams are available, otherwise unary)
4. The plugin executes provider logic (optionally calling HostService for secrets/auth)
5. Provider returns `ProviderOutput` containing data, warnings, and metadata
6. scafctl validates output against the provider's output schema for the current capability
7. scafctl continues orchestration

Providers never see unresolved CEL, templates, or resolver references. All provider responses use the standardized `ProviderOutput` structure.

---

## Plugin Discovery

Plugins are discovered via multiple mechanisms:

- The local catalog (built or pulled plugins)
- Configured plugin directories on disk
- Explicit configuration
- Environment-based paths
- Solution dependencies (automatically fetched from remote catalogs)

When a solution declares plugin dependencies (under `bundle.plugins`):

```yaml
bundle:
  plugins:
    - name: aws-provider
      kind: provider
      version: "^1.5.0"
      defaults:
        region: us-east-1
```

scafctl will:
1. Check if the plugin exists in the local catalog
2. Pull missing plugins from configured remote catalogs
3. Validate version constraints are met
4. Load the plugin binary
5. Apply plugin defaults (shallow-merged beneath inline inputs)

See [catalog-build-bundling.md](catalog-build-bundling.md) for the full design of `bundle.plugins`, including the `kind` field, ValueRef-aware defaults, and lock file integration.

Discovery does not execute plugins. Execution occurs only when a provider is invoked.

---

## Versioning and Compatibility

Plugins declare:

- Supported protocol version
- Provider versions
- Optional feature flags

scafctl enforces compatibility at load time.

Incompatible plugins are rejected early.

---

## Security Model

Plugins are isolated processes.

Security properties:

- No direct memory access to scafctl
- Explicit gRPC boundaries
- No implicit filesystem or network access beyond what the plugin implements
- Providers are the only exposed surface
- All inputs validated before sending to plugin
- Schema validation at scafctl boundary
- Type checking for all parameters

Plugin execution is explicit and auditable.

---

## Why Plugins Are Not a Separate Concept from Providers

Conceptually:

- Providers define behavior
- Plugins deliver providers

Introducing plugins as a separate user-facing concept would add unnecessary indirection.

Users reason about:

- Providers
- Actions
- Resolvers

Plugins are an implementation detail that enables extensibility.

---

## Design Constraints

- Plugins must not orchestrate execution
- Plugins must not resolve data
- Plugins must not mutate scafctl state
- Plugins may only expose declared capabilities
- Providers remain the sole execution primitive

## Notes

- Plugins use hashicorp/go-plugin (same as Terraform, Vault, Packer)
- gRPC communication provides language flexibility (Go, Python, Rust, etc.)
- Plugin providers and built-in providers are indistinguishable to users
- Plugins are the primary extensibility mechanism
- Plugin crashes are handled gracefully
- Plugin directory is configurable
- Plugins may expose multiple providers
- Provider names must be unique across built-ins and plugins

---

## Auto-Fetch & Runtime Loading

When a solution declares plugin dependencies under `bundle.plugins`, scafctl automatically fetches missing binaries from configured catalogs at runtime.

### Architecture

| Component | Package | Responsibility |
|-----------|---------|----------------|
| **Fetcher** | `pkg/plugin/fetcher.go` | Orchestrates fetch + cache + registration |
| **Cache** | `pkg/plugin/cache.go` | Content-addressed binary cache under `$XDG_CACHE_HOME/scafctl/plugins/` |
| **ChainCatalog** | `pkg/catalog/chain.go` | Tries catalogs in order (local → remote OCI) |
| **PluginFetcher** | `pkg/catalog/plugin_fetcher.go` | Platform-aware binary extraction from catalog artifacts |
| **Platform** | `pkg/plugin/platform.go` | Detects OS/arch, generates cache keys |

### Flow

1. Solution is loaded with `bundle.plugins` entries
2. `Fetcher.FetchPlugins()` iterates declared plugins
3. For each plugin, the cache is checked first (by name + version + platform digest)
4. On cache miss, `ChainCatalog` queries configured catalogs in order
5. `PluginFetcher` extracts the platform-specific binary from the OCI artifact
6. Binary is stored in the content-addressed cache (atomic write, `0o755`)
7. `RegisterFetchedPlugins()` adds cached paths to the plugin registry

### CLI Commands

- **`scafctl plugins install`** — Pre-fetch plugin binaries from catalogs before a build or run
- **`scafctl plugins list`** — List cached plugin binaries with digest, size, and platform info

Both commands live in `pkg/cmd/scafctl/plugins/`.

### Cache Layout

```
$XDG_CACHE_HOME/scafctl/plugins/
└── <sha256-digest>/           # Content-addressed binary
```

See the [Plugin Auto-Fetching Tutorial](../tutorials/plugin-auto-fetch-tutorial.md) for a complete walkthrough.

---

## Multi-Platform Support via OCI Image Index

Plugin binaries are platform-specific — a Linux x86-64 binary cannot run on
macOS ARM64. To distribute a single plugin that works across OS/architecture
combinations, scafctl supports **OCI image indexes** (fat manifests).

### Architecture

| Component | Package | Responsibility |
|-----------|---------|----------------|
| **MultiPlatform helpers** | `pkg/catalog/multiplatform.go` | Platform↔OCI conversion, index matching |
| **StoreMultiPlatform** | `pkg/catalog/local_multiplatform.go` | Store multi-platform artifact as image index |
| **FetchByPlatform** | `pkg/catalog/local_multiplatform.go` | Fetch correct platform binary from image index |
| **PlatformAwareCatalog** | `pkg/catalog/plugin_fetcher.go` | Interface for catalogs with image index support |
| **build plugin** | `pkg/cmd/scafctl/build/plugin.go` | CLI for building multi-platform artifacts |

### OCI Image Index Structure

A multi-platform plugin is stored as an OCI image index referencing
per-platform image manifests:

```
image index (application/vnd.oci.image.index.v1+json)
├── platform: linux/amd64
│   └── manifest → config + binary layer
├── platform: darwin/arm64
│   └── manifest → config + binary layer
└── platform: windows/amd64
    └── manifest → config + binary layer
```

### Platform Resolution Strategy

When `PluginFetcher.FetchPlugin()` is called:

1. **OCI image index** — If the catalog implements `PlatformAwareCatalog`,
   try `FetchByPlatform()` which resolves the platform from the image index.
   If the artifact IS an image index but the platform is missing, return
   `PlatformNotFoundError` (no fallback — the artifact is explicitly
   multi-platform).

2. **Annotation matching** (legacy) — Fall back to listing artifacts and
   matching the `dev.scafctl.plugin.platform` annotation on individual
   manifests.

3. **Direct fetch** — Fall back to fetching the artifact directly
   (single-platform artifacts without platform metadata).

### Supported Platforms

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`
- `windows/amd64`

### Building Multi-Platform Artifacts

```bash
scafctl build plugin \
  --name my-provider \
  --kind provider \
  --version 1.0.0 \
  --platform linux/amd64=./dist/linux-amd64/my-provider \
  --platform darwin/arm64=./dist/darwin-arm64/my-provider
```

See the [Multi-Platform Plugin Build Tutorial](../tutorials/multi-platform-plugin-build.md) for a complete walkthrough.

---

## Summary

Plugins are the extensibility layer of scafctl. They exist to supply providers in an isolated, versioned, and scalable way using go-plugin. Plugins are not a new execution model or abstraction. They are the mechanism by which providers are distributed and invoked, keeping the core system small, stable, and extensible.

Plugins are distributed through the catalog system as OCI artifacts, enabling:
- Versioned plugin releases with semantic versioning
- Multi-platform support (linux/amd64, darwin/arm64, etc.)
- Offline distribution via `scafctl save/load`
- Automatic dependency resolution when solutions declare required plugins
