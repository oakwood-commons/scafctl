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
| ------- | ------ | ------------- |
| `Quiet` | `bool` | Suppress non-essential output |
| `NoColor` | `bool` | Disable colored output |
| `BinaryName` | `string` | CLI binary name (e.g. "scafctl" or an embedder name) |
| `Profile` | `string` | Active authentication profile name (auth handlers) |
| `HostServiceID` | `uint32` | GRPCBroker service ID for HostService callbacks |
| `Settings` | `map[string]json.RawMessage` | Extensible key-value settings |
| `AuthCallbackPages` | `*AuthCallbackPages` | Optional pre-rendered success/failure HTML documents (<= 256 KiB each) for interactive OAuth callback servers |

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

Auth handler plugins may also implement optional SDK interfaces such as `AuthGroupsProvider` (`GetGroups(ctx, handlerName)`) to support group membership queries via `AuthHandlerWrapper.GetGroups` and `HostService.GetAuthGroups` (plugins that do not implement it return gRPC `Unimplemented`, mapped on the host to `auth.ErrGroupsNotSupported`).

After plugin load, scafctl calls `ConfigureAuthHandler` once per handler. The
request uses the `ProviderConfig` fields (`quiet`, `no_color`, `binary_name`,
`profile`, `settings`, `host_service_id`, `protocol_version`,
`host_capabilities`, and `auth_callback_pages`). The response includes optional
`diagnostics` for structured warnings/errors.

Auth handler plugins have full access to `HostService` callbacks (secrets, auth
identity) via the GRPCBroker, mirroring the provider plugin pattern.

`RegisterAuthHandlerPlugins` and `RegisterScopedAuthHandlerPlugins` return
`[]*AuthHandlerClient` with each discovered handler isolated to its own
`scafctl.auth.<name>.` secret namespace before `HostService` is exposed. The
caller should defer `KillAllAuthHandlers(clients)` for cleanup.

## HostService Callbacks

Plugins that need host-side resources use the HostService callback service,
accessed via the GRPCBroker using the service ID from `ProviderConfig.HostServiceID`.

| Callback | Purpose |
| ---------- | --------- |
| `GetSecret` / `SetSecret` / `DeleteSecret` / `ListSecrets` | Access the host's secret store |
| `GetAuthIdentity` | Retrieve identity claims from the host's auth registry |
| `ListAuthHandlers` | List available auth handlers (filtered by AllowedAuthHandlers) |
| `GetAuthToken` | Retrieve a valid access token from the host's auth registry |
| `GetAuthGroups` | Retrieve group memberships for the authenticated user via `auth.GroupsProvider` |
| `PromptAuthResponse` | Prompt the user for a pasted OAuth redirect URL during an active login |

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
7. **Shutdown**: `Client.Kill()` asks the plugin to shut down and waits briefly for
   it to exit, force-killing it if it does not, then removes the directory that
   held its socket (see [Process Cleanup](#process-cleanup))

### Auth Handler Plugins

1. **Discovery**: scafctl finds plugin binaries
2. **Handshake**: Protocol version and magic cookie are validated
3. **GetAuthHandlers**: Plugin advertises handler names, flows, and capabilities
4. **ConfigureAuthHandler**: Host sends config + protocol version + HostService ID + host capabilities + callback pages
5. **Authentication**: `Login`, `Logout`, `GetStatus`, `GetToken`, and optional `GetGroups` on user or provider request
6. **Shutdown**: `AuthHandlerClient.Kill()`, as for provider plugins

`StopProvider` and `StopAuthHandler` are part of the protocol and let a plugin
abort in-flight work, but the host does not call them as part of shutting a
plugin down: a plugin process ends when the host kills its client.

## Process Cleanup

A plugin is a separate operating-system process, and it ignores SIGINT. Nothing
else will stop it if the host forgets to, so every path that starts a plugin
client must end with `Kill()` (or `KillAll` for a list). The host guarantees the
following when it does.

- **Graceful first.** `Kill()` asks the plugin's go-plugin server to shut down and
  gives it two seconds to exit, closing its own socket. Only a plugin that does
  not exit in that time, or has already died, is force-killed. This holds even
  when a call to the plugin is still blocked.
- **The host owns the socket directory.** On Unix the host creates a private
  `plugin-dir*` directory under the temp directory for each plugin and passes it
  in `PLUGIN_UNIX_SOCKET_DIR`; the plugin creates its socket there. `Kill()`
  deletes the directory. A plugin that crashes, panics, is killed, or fails its
  handshake cannot remove its own socket, so without this its socket file would
  stay in the temp directory forever. If the temp directory path is too long for
  a socket path inside a subdirectory, or on Windows (where go-plugin uses TCP),
  the plugin creates its socket itself as before. So it does if
  `PLUGIN_UNIX_SOCKET_DIR` is already set in the environment scafctl runs in:
  that choice is honored, and the operator's directory is never deleted.
- **A failed start leaves nothing.** If the handshake fails, the plugin is killed
  and the socket directory removed before `NewClient` returns the error.
- **Pools and servers release what they hold.** `Pool.Shutdown` and
  `VersionPool.Shutdown` kill every managed plugin. A client handed to a pool
  that is already closed (`Adopt`) is killed, not dropped. `api.Server.Start`
  releases its pool and preloaded clients on every exit path, after in-flight
  requests drain: a signal, a cancelled server context, a failure to listen, or
  invalid TLS configuration.

A plugin built without the SDK should create its socket inside the directory named
by `PLUGIN_UNIX_SOCKET_DIR` when that variable is set, so the host can remove it.

Known limitation: the CLI does not yet intercept SIGINT or SIGTERM, so interrupting
a command with Ctrl+C while a plugin is running ends the command without running
its cleanup, and the plugin process outlives it.

## Testing Plugin Lifecycle

The `plugintest` package builds a configurable fixture plugin
(`testdata/lifecycleplugin`) that can fail to start, hang, crash or panic mid-call,
return undecodable output, end a stream early, report a different protocol version,
or send fields the schema does not define. Its `Sandbox` gives each test its own temp
directory, so the processes, sockets and socket directories found there are exactly
the ones that test caused:

```go
sb := plugintest.NewSandbox(t, plugintest.Config{})
client, err := plugin.NewClient(sb.Binary)
require.NoError(t, err)
// ... use client ...
client.Kill()
sb.RequireClean(t, 5*time.Second)
```

`NewDetachedSandbox` and `Sandbox.Env` do the same for a host that runs as a
subprocess, such as the `scafctl` binary. See `lifecycle_test.go` for the lifecycle
tests built on it.

## Example Plugin

See `examples/plugins/echo/` for a complete example plugin implementation.

## Security

- Plugins run in isolated processes
- Communication over gRPC provides clear security boundaries
- Plugins are validated using handshake configuration
- Failed plugins don't crash the main process
- Secret access is scoped by plugin-specific prefix
- Auth handler plugins registered by the CLI get secret access scoped to their own `scafctl.auth.<name>.` namespace (`WithSecretScope`); embedders composing `WithHostDeps` themselves keep allow-all semantics until they add `WithSecretScope`
- Auth handler access restricted by AllowedAuthHandlers allowlist
- Plugin binary signatures can be verified via Sigstore/cosign (see below)

## Signature Verification

Plugin binaries fetched from catalogs support Sigstore/cosign keyless signature
verification. This is controlled by a `SignaturePolicy`:

| Mode | Behavior |
| ------ | ---------- |
| `off` | Digest-only verification (default) |
| `warn` | Verify signature; warn on failure |
| `enforce` | Verify signature; fail on missing or invalid signature |

### Types

- `SignaturePolicy` -- policy with `Mode`, `TrustedIssuers`, and `TrustedIdentities`
- `SignatureMode` -- string enum: `off`, `warn`, `enforce`
- `SignatureResult` -- verification outcome with `Verified`, `Issuer`, `Identity`, `SignedAt`
- `SignatureVerifier` -- interface for signature verification implementations

### Build Tags

The cosign implementation is behind a `cosign` build tag:

- **With tag**: `cosign.go` provides a real Sigstore verifier
- **Without tag** (default): `cosign_stub.go` returns `ErrCosignNotAvailable`

### Context API

- `WithSignaturePolicy(ctx, policy)` -- store a policy in context
- `SignaturePolicyFromContext(ctx)` -- retrieve a policy from context
- `HandleVerificationError(policy, err, logger)` -- apply mode-based error handling

## Testing

Use `MockProviderPlugin` for testing plugin implementations without running actual plugin processes.
