# Plugins

## Purpose

Plugins are the extension mechanism for scafctl. They allow external binaries to contribute functionality to the system in a controlled, versioned, and discoverable way.

The primary purpose of plugins is to supply providers. Plugins are not a separate execution concept from providers. They are the delivery and isolation mechanism used to obtain providers.

---

## What a Plugin Is

A plugin is an external process that implements one or more scafctl extension interfaces and communicates with scafctl over RPC.

Plugins are:

- Discovered and loaded at runtime
- Versioned independently from scafctl
- Isolated from the core process
- Capable of exposing multiple providers

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

## Primary Use: Provider Distribution

Plugins exist primarily to distribute providers.

Under this model:

- Providers define behavior
- Plugins package providers
- scafctl orchestrates provider execution

A plugin may expose one or more providers.
---

## Why Plugins Exist (Instead of Built-ins Only)

Plugins exist to:

- Avoid baking all providers into scafctl
- Enable third-party and internal extensions
- Allow providers to evolve independently
- Isolate failures and crashes
- Support multiple languages via gRPC boundaries
- Keep the core binary small and stable

This mirrors patterns used by Terraform, Vault, Nomad, and Packer.

---

## Plugin Architecture

### Dependencies

- **External**: hashicorp/go-plugin (gRPC-based plugin system)
- **External**: google.golang.org/grpc (gRPC for plugin communication)
- **External**: google.golang.org/protobuf (Protocol buffers)

scafctl uses go-plugin with gRPC-based handshake.

Protocol buffer definition for plugin communication.

```protobuf
syntax = "proto3";
package plugin;
option go_package = "github.com/oakwood-commons/scafctl/pkg/plugin/proto";

// PluginService is the main plugin service
service PluginService {
  // GetProviders returns all providers exposed by this plugin
  rpc GetProviders(GetProvidersRequest) returns (GetProvidersResponse);
  
  // GetProviderDescriptor returns metadata for a specific provider
  rpc GetProviderDescriptor(GetProviderDescriptorRequest) returns (GetProviderDescriptorResponse);
  
  // ExecuteProvider executes a provider
  rpc ExecuteProvider(ExecuteProviderRequest) returns (ExecuteProviderResponse);
}

message GetProvidersRequest {}

message GetProvidersResponse {
  repeated string provider_names = 1;
}

message GetProviderDescriptorRequest {
  string provider_name = 1;
}

message GetProviderDescriptorResponse {
  ProviderDescriptor descriptor = 1;
}

message ProviderDescriptor {
  string name = 1;
  string description = 2;
  Schema schema = 3;
}

message Schema {
  map<string, Parameter> parameters = 1;
}

message Parameter {
  string type = 1;
  bool required = 2;
  string description = 3;
  bytes default_value = 4; // JSON-encoded
}

message ExecuteProviderRequest {
  string provider_name = 1;
  bytes input = 2; // JSON-encoded input map
}

message ExecuteProviderResponse {
  bytes output = 1; // JSON-encoded output
  string error = 2;  // Empty if no error
}
```

Conceptually:

- scafctl discovers a plugin binary
- scafctl negotiates protocol version
- Plugin advertises capabilities
- scafctl registers providers exposed by the plugin
- Providers are invoked through gRPC

The plugin process lifecycle is managed entirely by scafctl.

---

## Plugin Capabilities

Today, plugins are intended to expose providers.

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
3. scafctl invokes the provider via gRPC
4. The plugin executes provider logic
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

When a solution declares plugin dependencies:

```yaml
dependencies:
  plugins:
    - name: aws-provider
      version: ^1.5.0
```

scafctl will:
1. Check if the plugin exists in the local catalog
2. Pull missing plugins from configured remote catalogs
3. Validate version constraints are met
4. Load the plugin binary

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

## Summary

Plugins are the extensibility layer of scafctl. They exist to supply providers in an isolated, versioned, and scalable way using go-plugin. Plugins are not a new execution model or abstraction. They are the mechanism by which providers are distributed and invoked, keeping the core system small, stable, and extensible.

Plugins are distributed through the catalog system as OCI artifacts, enabling:
- Versioned plugin releases with semantic versioning
- Multi-platform support (linux/amd64, darwin/arm64, etc.)
- Offline distribution via `scafctl save/load`
- Automatic dependency resolution when solutions declare required plugins
