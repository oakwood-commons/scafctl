# Plugin System

The scafctl plugin system allows extending the provider framework with external plugins using hashicorp/go-plugin.

## Architecture

- **hashicorp/go-plugin**: Manages plugin lifecycle, process isolation, and crash recovery
- **gRPC**: Communication protocol between scafctl and plugins
- **Protocol Buffers**: Interface definitions in `proto/plugin.proto`

## Plugin Interface

Plugins must implement the `ProviderPlugin` interface (8 methods):

```go
type ProviderPlugin interface {
    // GetProviders returns all provider names exposed by this plugin.
    GetProviders(ctx context.Context) ([]string, error)

    // GetProviderDescriptor returns metadata for a specific provider.
    GetProviderDescriptor(ctx context.Context, providerName string) (*provider.Descriptor, error)

    // ConfigureProvider sends host-side configuration to a named provider once
    // after plugin load. Implementations store the config internally for
    // subsequent Execute calls.
    ConfigureProvider(ctx context.Context, providerName string, cfg ProviderConfig) error

    // ExecuteProvider executes a provider with the given input.
    ExecuteProvider(ctx context.Context, providerName string, input map[string]any) (*provider.Output, error)

    // ExecuteProviderStream executes a provider that produces incremental
    // output. The callback is invoked for each chunk; the final chunk carries
    // the Result (or Error). Return ErrStreamingNotSupported if not implemented.
    ExecuteProviderStream(ctx context.Context, providerName string, input map[string]any, cb func(StreamChunk)) error

    // DescribeWhatIf returns a human-readable description of what the provider
    // would do with the given inputs, without executing.
    DescribeWhatIf(ctx context.Context, providerName string, input map[string]any) (string, error)

    // ExtractDependencies returns resolver dependency names from the given
    // inputs. Return nil to let the host use generic extraction.
    ExtractDependencies(ctx context.Context, providerName string, inputs map[string]any) ([]string, error)

    // StopProvider requests graceful shutdown of a running provider execution.
    // providerName may be empty to stop all providers. Return nil if not implemented.
    StopProvider(ctx context.Context, providerName string) error
}
```

## ProviderConfig

After plugin load, scafctl calls `ConfigureProvider` once per provider with host-side settings:

| Field | Type | Description |
|-------|------|-------------|
| `Quiet` | `bool` | Suppress non-essential output |
| `NoColor` | `bool` | Disable colored output |
| `BinaryName` | `string` | CLI binary name (e.g. "scafctl" or an embedder name) |
| `HostServiceID` | `uint32` | GRPCBroker service ID for HostService callbacks |
| `Settings` | `map[string]json.RawMessage` | Extensible key-value settings |

The `ConfigureProvider` request also carries a `protocol_version` field for
feature detection. The current version is defined by `PluginProtocolVersion`.

## Auth Handler Plugin Interface

Auth handler plugins implement the `AuthHandlerPlugin` interface (9 methods):

```go
type AuthHandlerPlugin interface {
    GetAuthHandlers(ctx context.Context) ([]AuthHandlerInfo, error)
    ConfigureAuthHandler(ctx context.Context, handlerName string, cfg ProviderConfig) error
    Login(ctx context.Context, handlerName string, req LoginRequest, cb func(DeviceCodePrompt)) (*LoginResponse, error)
    Logout(ctx context.Context, handlerName string) error
    GetStatus(ctx context.Context, handlerName string) (*auth.Status, error)
    GetToken(ctx context.Context, handlerName string, req TokenRequest) (*TokenResponse, error)
    ListCachedTokens(ctx context.Context, handlerName string) ([]*auth.CachedTokenInfo, error)
    PurgeExpiredTokens(ctx context.Context, handlerName string) (int, error)
    StopAuthHandler(ctx context.Context, handlerName string) error
}
```

After plugin load, scafctl calls `ConfigureAuthHandler` once per handler. The
request uses the same `ProviderConfig` fields as `ConfigureProvider` (quiet,
no_color, binary_name, settings, host_service_id, protocol_version). The
response includes optional `diagnostics` for structured warnings/errors.

Auth handler plugins have full access to `HostService` callbacks (secrets, auth
identity) via the GRPCBroker, mirroring the provider plugin pattern.

`RegisterAuthHandlerPlugins` returns `[]*AuthHandlerClient`. The caller should
defer `KillAllAuthHandlers(clients)` for cleanup.

## HostService Callbacks

Plugins that need host-side resources use the HostService callback service,
accessed via the GRPCBroker using the service ID from `ProviderConfig.HostServiceID`.

| Callback | Purpose |
|----------|---------|
| `GetSecret` / `SetSecret` / `DeleteSecret` / `ListSecrets` | Access the host's secret store |
| `GetAuthIdentity` | Retrieve identity claims from the host's auth registry |
| `ListAuthHandlers` | List available auth handlers (filtered by AllowedAuthHandlers) |
| `GetAuthToken` | Retrieve a valid access token from the host's auth registry |

Secret names are scoped by a plugin-specific prefix. Auth handler access is
restricted to the set declared in `AllowedAuthHandlers`.

## Streaming Execution

`ExecuteProviderStream` delivers incremental output (stdout/stderr chunks) to
the host in real time. The callback receives `StreamChunk` values:

- `Stdout` / `Stderr` -- incremental output bytes
- `Result` -- final `*provider.Output` on success
- `Error` -- terminal error string on failure

Plugins that do not support streaming should return `ErrStreamingNotSupported`.
The host automatically falls back to unary `ExecuteProvider`.

## Diagnostics and Exit Codes

The `ExecuteProviderResponse` proto carries structured `Diagnostic` messages
alongside the output. Each diagnostic has a severity, summary, detail, and
optional attribute path. An `exit_code` field enables plugins to propagate
typed exit codes back to the host.

## Protocol Version Negotiation

scafctl sends `PluginProtocolVersion` in the `ConfigureProvider` request. Plugins
can inspect this to enable or disable features based on the host's capabilities.
The plugin may return its own protocol version in the response for the host to
check.

## Creating a Plugin

1. Import the plugin SDK package:

    ```go
    import "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
    ```

2. Implement the `ProviderPlugin` interface (all 8 methods)

3. Call `plugin.Serve()` in your main function:

    ```go
    func main() {
        plugin.Serve(&YourPlugin{})
    }
    ```

4. Build your plugin as an executable:

    ```bash
    go build -o my-plugin main.go
    ```

## Descriptor Caching

The plugin client caches provider descriptors after the first `GetProviderDescriptor`
call to avoid repeated gRPC round-trips. The cache is protected by a `sync.RWMutex`
for concurrent access.

## Plugin Discovery

Plugins are discovered by scanning configured plugin directories for executable files. The system:

1. Searches for executable files in plugin directories
2. Attempts to connect to each potential plugin
3. Registers providers from successfully loaded plugins
4. Skips plugins that fail to load

`RegisterPluginProviders` returns all created `*Client` instances so the caller
can clean up with `KillAll` on shutdown.

## Plugin Lifecycle

### Provider Plugins

1. **Discovery**: scafctl finds plugin binaries
2. **Handshake**: Protocol version and magic cookie are validated
3. **GetProviders**: Plugin advertises provider names
4. **GetProviderDescriptor**: scafctl fetches and caches descriptors
5. **ConfigureProvider**: Host sends config + protocol version + HostService ID
6. **Execution**: `ExecuteProvider` or `ExecuteProviderStream` on invocation
7. **Shutdown**: `StopProvider` for graceful cleanup, then `Kill()` the process

### Auth Handler Plugins

1. **Discovery**: scafctl finds plugin binaries
2. **Handshake**: Protocol version and magic cookie are validated
3. **GetAuthHandlers**: Plugin advertises handler names, flows, and capabilities
4. **ConfigureAuthHandler**: Host sends config + protocol version + HostService ID
5. **Authentication**: `Login`, `Logout`, `GetStatus`, `GetToken` on user request
6. **Shutdown**: `StopAuthHandler` for graceful cleanup, then `Kill()` the process

## Example Plugin

See `examples/plugins/echo/` for a complete example plugin implementation.

## Security

- Plugins run in isolated processes
- Communication over gRPC provides clear security boundaries
- Plugins are validated using handshake configuration
- Failed plugins don't crash the main process
- Secret access is scoped by plugin-specific prefix
- Auth handler access restricted by AllowedAuthHandlers allowlist

## Testing

Use `MockProviderPlugin` for testing plugin implementations without running actual plugin processes.
