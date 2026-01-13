# Plugin System

The scafctl plugin system allows extending the provider framework with external plugins using hashicorp/go-plugin.

## Architecture

- **hashicorp/go-plugin**: Manages plugin lifecycle, process isolation, and crash recovery
- **gRPC**: Communication protocol between scafctl and plugins
- **Protocol Buffers**: Interface definitions

## Plugin Interface

Plugins must implement the `ProviderPlugin` interface:

```go
type ProviderPlugin interface {
    // GetProviders returns all provider names exposed by this plugin
    GetProviders(ctx context.Context) ([]string, error)

    // GetProviderDescriptor returns metadata for a specific provider
    GetProviderDescriptor(ctx context.Context, providerName string) (*provider.Descriptor, error)

    // ExecuteProvider executes a provider with the given input
    ExecuteProvider(ctx context.Context, providerName string, input map[string]any) (*provider.Output, error)
}
```

## Creating a Plugin

1. Import the plugin package:
```go
import "github.com/oakwood-commons/scafctl/pkg/plugin"
```

2. Implement the `ProviderPlugin` interface

3. Call `plugin.Serve()` in your main function:
```go
func main() {
    plugin.Serve(&YourPlugin{})
}
```

4. Build your plugin as an executable:
```go
go build -o my-plugin main.go
```

## Plugin Discovery

Plugins are discovered by scanning configured plugin directories for executable files. The system:

1. Searches for executable files in plugin directories
2. Attempts to connect to each potential plugin
3. Registers providers from successfully loaded plugins
4. Skips plugins that fail to load

## Example Plugin

See `examples/plugins/echo/` for a complete example plugin implementation.

## Security

- Plugins run in isolated processes
- Communication over gRPC provides clear security boundaries
- Plugins are validated using handshake configuration
- Failed plugins don't crash the main process

## Testing

Use `MockProviderPlugin` for testing plugin implementations without running actual plugin processes.
