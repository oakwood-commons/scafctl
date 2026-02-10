---
title: "Plugin Development Guide"
weight: 130
---

# Plugin Development Guide

This guide explains how to create external plugins for scafctl. Plugins extend scafctl with custom providers that run as separate processes, allowing you to:

- Use any language that supports gRPC
- Isolate plugin crashes from the main process
- Distribute providers independently
- Add third-party integrations

> **Terminology Note**: "Plugin" refers to the go-plugin binary implementation. When distributing via the catalog, provider plugins are pushed as **provider** artifacts and auth handler plugins as **auth-handler** artifacts. Users interact with these as catalog artifact kinds, not as "plugins". See [Publishing to the Catalog](#publishing-to-the-catalog) for details.

## Architecture Overview

```
┌─────────────────────┐        gRPC        ┌──────────────────────┐
│      scafctl        │◄──────────────────►│    Your Plugin       │
│                     │                     │                      │
│  - Discovers plugin │                     │  - Implements gRPC   │
│  - Calls providers  │                     │  - Exposes providers │
│  - Manages lifecycle│                     │  - Handles execution │
└─────────────────────┘                     └──────────────────────┘
```

Plugins use [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin) with gRPC for communication.

## Quick Start

### 1. Create Plugin Directory

```bash
mkdir my-plugin
cd my-plugin
go mod init github.com/myorg/my-plugin
go get github.com/oakwood-commons/scafctl
```

### 2. Implement Plugin Interface

```go
// main.go
package main

import (
    "context"
    "fmt"

    "github.com/Masterminds/semver/v3"
    "github.com/google/jsonschema-go/jsonschema"
    "github.com/oakwood-commons/scafctl/pkg/plugin"
    "github.com/oakwood-commons/scafctl/pkg/provider"
    "github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

type MyPlugin struct{}

// GetProviders returns provider names exposed by this plugin
func (p *MyPlugin) GetProviders(ctx context.Context) ([]string, error) {
    return []string{"my-custom-provider"}, nil
}

// GetProviderDescriptor returns metadata for a specific provider
func (p *MyPlugin) GetProviderDescriptor(ctx context.Context, name string) (*provider.Descriptor, error) {
    switch name {
    case "my-custom-provider":
        return &provider.Descriptor{
            Name:        "my-custom-provider",
            DisplayName: "My Custom Provider",
            APIVersion:  "v1",
            Version:     semver.MustParse("1.0.0"),
            Description: "A custom provider that does something useful",
            Capabilities: []provider.Capability{
                provider.CapabilityFrom,
                provider.CapabilityTransform,
            },
            Schema: schemahelper.ObjectSchema([]string{"input"}, map[string]*jsonschema.Schema{
                "input": schemahelper.StringProp("The input value to process"),
            }),
            OutputSchemas: map[provider.Capability]*jsonschema.Schema{
                provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
                    "output": schemahelper.StringProp("The processed output"),
                }),
                provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
                    "output": schemahelper.StringProp("The transformed output"),
                }),
            },
            Category:     "custom",
            Tags:         []string{"custom", "example"},
            MockBehavior: "Returns a mock output without processing",
        }, nil
    default:
        return nil, fmt.Errorf("unknown provider: %s", name)
    }
}

// ExecuteProvider runs the provider logic
func (p *MyPlugin) ExecuteProvider(ctx context.Context, name string, input map[string]any) (*provider.Output, error) {
    switch name {
    case "my-custom-provider":
        value, _ := input["input"].(string)
        return &provider.Output{
            Data: map[string]any{
                "output": "Processed: " + value,
            },
        }, nil
    default:
        return nil, fmt.Errorf("unknown provider: %s", name)
    }
}

func main() {
    plugin.Serve(&MyPlugin{})
}
```

### 3. Build the Plugin

```bash
go build -o my-plugin .
```

### 4. Install the Plugin

Copy to the scafctl plugins directory:

```bash
# Linux/macOS
mkdir -p ~/.scafctl/plugins
cp my-plugin ~/.scafctl/plugins/

# Or specify in config
cat >> ~/.scafctl/config.yaml << EOF
plugins:
  directories:
    - ~/.scafctl/plugins
    - /opt/scafctl/plugins
EOF
```

### 5. Use Your Provider

```yaml
# solution.yaml
apiVersion: scafctl/v1
kind: Solution
metadata:
  name: my-solution
spec:
  resolvers:
    data:
      resolve:
        from:
          provider: my-custom-provider
          inputs:
            input: "Hello World"
```

## Plugin Interface

Plugins must implement three methods:

### GetProviders

Returns a list of provider names this plugin exposes:

```go
func (p *MyPlugin) GetProviders(ctx context.Context) ([]string, error) {
    return []string{
        "provider-one",
        "provider-two",
        "provider-three",
    }, nil
}
```

### GetProviderDescriptor

Returns the full descriptor for a specific provider:

```go
func (p *MyPlugin) GetProviderDescriptor(ctx context.Context, name string) (*provider.Descriptor, error) {
    switch name {
    case "provider-one":
        return &provider.Descriptor{
            Name:         "provider-one",
            // ... full descriptor
        }, nil
    default:
        return nil, fmt.Errorf("unknown provider: %s", name)
    }
}
```

### ExecuteProvider

Executes the provider logic with validated inputs:

```go
func (p *MyPlugin) ExecuteProvider(ctx context.Context, name string, input map[string]any) (*provider.Output, error) {
    switch name {
    case "provider-one":
        // Your implementation
        return &provider.Output{
            Data: result,
        }, nil
    default:
        return nil, fmt.Errorf("unknown provider: %s", name)
    }
}
```

## Multi-Provider Plugin

A single plugin can expose multiple providers:

```go
type MultiPlugin struct {
    providers map[string]ProviderHandler
}

type ProviderHandler interface {
    Descriptor() *provider.Descriptor
    Execute(ctx context.Context, input map[string]any) (*provider.Output, error)
}

func NewMultiPlugin() *MultiPlugin {
    return &MultiPlugin{
        providers: map[string]ProviderHandler{
            "slack-notify":  &SlackProvider{},
            "slack-channel": &SlackChannelProvider{},
            "slack-user":    &SlackUserProvider{},
        },
    }
}

func (p *MultiPlugin) GetProviders(ctx context.Context) ([]string, error) {
    names := make([]string, 0, len(p.providers))
    for name := range p.providers {
        names = append(names, name)
    }
    return names, nil
}

func (p *MultiPlugin) GetProviderDescriptor(ctx context.Context, name string) (*provider.Descriptor, error) {
    handler, ok := p.providers[name]
    if !ok {
        return nil, fmt.Errorf("unknown provider: %s", name)
    }
    return handler.Descriptor(), nil
}

func (p *MultiPlugin) ExecuteProvider(ctx context.Context, name string, input map[string]any) (*provider.Output, error) {
    handler, ok := p.providers[name]
    if !ok {
        return nil, fmt.Errorf("unknown provider: %s", name)
    }
    return handler.Execute(ctx, input)
}
```

## Complete Example: Slack Plugin

```go
package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"

    "github.com/Masterminds/semver/v3"
    "github.com/google/jsonschema-go/jsonschema"
    "github.com/oakwood-commons/scafctl/pkg/plugin"
    "github.com/oakwood-commons/scafctl/pkg/provider"
    "github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

type SlackPlugin struct{}

func (p *SlackPlugin) GetProviders(ctx context.Context) ([]string, error) {
    return []string{"slack"}, nil
}

func (p *SlackPlugin) GetProviderDescriptor(ctx context.Context, name string) (*provider.Descriptor, error) {
    if name != "slack" {
        return nil, fmt.Errorf("unknown provider: %s", name)
    }

    return &provider.Descriptor{
        Name:        "slack",
        DisplayName: "Slack Notifier",
        APIVersion:  "v1",
        Version:     semver.MustParse("1.0.0"),
        Description: "Send messages to Slack channels via webhooks",
        Category:    "notification",
        Tags:        []string{"slack", "notification", "messaging"},
        Capabilities: []provider.Capability{
            provider.CapabilityAction,
        },
        SensitiveFields: []string{"webhookUrl"},
        Schema: schemahelper.ObjectSchema([]string{"webhookUrl", "message"}, map[string]*jsonschema.Schema{
            "webhookUrl": schemahelper.StringProp("Slack incoming webhook URL",
                schemahelper.WithFormat("uri"),
                schemahelper.WithWriteOnly(),
            ),
            "channel": schemahelper.StringProp("Channel to post to (overrides webhook default)",
                schemahelper.WithExample("#deployments"),
            ),
            "message": schemahelper.StringProp("Message text to send",
                schemahelper.WithMaxLength(40000),
            ),
            "username": schemahelper.StringProp("Bot username to display",
                schemahelper.WithDefault(json.RawMessage(`"scafctl"`)),
            ),
            "iconEmoji": schemahelper.StringProp("Emoji to use as bot icon",
                schemahelper.WithDefault(json.RawMessage(`":robot_face:"`)),
            ),
        }),
        OutputSchemas: map[provider.Capability]*jsonschema.Schema{
            provider.CapabilityAction: schemahelper.ObjectSchema([]string{"success"}, map[string]*jsonschema.Schema{
                "success":   schemahelper.BoolProp("Whether message was sent successfully"),
                "timestamp": schemahelper.StringProp("Message timestamp from Slack"),
            }),
        },
        MockBehavior: "Logs the message that would be sent without calling Slack API",
        Examples: []provider.Example{
            {
                Name:        "Deployment notification",
                Description: "Send a deployment notification",
                YAML: `name: notify-deploy
provider: slack
inputs:
  webhookUrl:
    expr: _.secrets.slackWebhook
  channel: "#deployments"
  message:
    tmpl: "🚀 Deployed {{ .version }} to {{ .environment }}"`,
            },
        },
    }, nil
}

func (p *SlackPlugin) ExecuteProvider(ctx context.Context, name string, input map[string]any) (*provider.Output, error) {
    if name != "slack" {
        return nil, fmt.Errorf("unknown provider: %s", name)
    }

    webhookURL := input["webhookUrl"].(string)
    message := input["message"].(string)

    // Build Slack payload
    payload := map[string]any{
        "text": message,
    }

    if channel, ok := input["channel"].(string); ok && channel != "" {
        payload["channel"] = channel
    }
    if username, ok := input["username"].(string); ok && username != "" {
        payload["username"] = username
    }
    if iconEmoji, ok := input["iconEmoji"].(string); ok && iconEmoji != "" {
        payload["icon_emoji"] = iconEmoji
    }

    // Send to Slack
    body, _ := json.Marshal(payload)
    resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
    if err != nil {
        return nil, fmt.Errorf("slack: failed to send message: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("slack: unexpected status %d", resp.StatusCode)
    }

    return &provider.Output{
        Data: map[string]any{
            "success": true,
        },
    }, nil
}

func main() {
    plugin.Serve(&SlackPlugin{})
}
```

## Error Handling

Return meaningful errors that help users debug issues:

```go
func (p *MyPlugin) ExecuteProvider(ctx context.Context, name string, input map[string]any) (*provider.Output, error) {
    // Validate required fields
    apiKey, ok := input["apiKey"].(string)
    if !ok || apiKey == "" {
        return nil, fmt.Errorf("%s: apiKey is required", name)
    }

    // Wrap errors with context
    result, err := callExternalAPI(apiKey)
    if err != nil {
        return nil, fmt.Errorf("%s: API call failed: %w", name, err)
    }

    // Return warnings for non-fatal issues
    return &provider.Output{
        Data: result,
        Warnings: []string{
            "API rate limit is at 80%",
        },
    }, nil
}
```

## Testing Plugins

### Unit Testing

```go
func TestMyPlugin_GetProviders(t *testing.T) {
    p := &MyPlugin{}
    
    providers, err := p.GetProviders(context.Background())
    require.NoError(t, err)
    assert.Contains(t, providers, "my-custom-provider")
}

func TestMyPlugin_Execute(t *testing.T) {
    p := &MyPlugin{}
    
    output, err := p.ExecuteProvider(context.Background(), "my-custom-provider", map[string]any{
        "input": "test",
    })
    
    require.NoError(t, err)
    assert.NotNil(t, output.Data)
}
```

### Integration Testing

Test against a running plugin process:

```go
func TestPluginIntegration(t *testing.T) {
    // Build plugin
    cmd := exec.Command("go", "build", "-o", "test-plugin", ".")
    require.NoError(t, cmd.Run())
    defer os.Remove("test-plugin")

    // Connect to plugin
    client, err := plugin.NewClient("./test-plugin")
    require.NoError(t, err)
    defer client.Close()

    // Test provider
    providers, err := client.GetProviders(context.Background())
    require.NoError(t, err)
    assert.NotEmpty(t, providers)
}
```

## Plugin Discovery

scafctl discovers plugins through:

1. **Config file paths**:
   ```yaml
   # ~/.scafctl/config.yaml
   plugins:
     directories:
       - ~/.scafctl/plugins
       - /usr/local/share/scafctl/plugins
   ```

2. **Environment variable**:
   ```bash
   export SCAFCTL_PLUGIN_PATH=~/.scafctl/plugins:/opt/plugins
   ```

3. **Default locations**:
   - `~/.scafctl/plugins/`
   - `/usr/local/share/scafctl/plugins/` (Linux/macOS)

## Plugin Naming Conventions

Plugin executables should be named descriptively:

```
scafctl-plugin-slack      # Slack integration
scafctl-plugin-aws        # AWS providers
scafctl-plugin-terraform  # Terraform integration
my-company-plugin         # Custom company plugin
```

## Security Considerations

1. **Process isolation**: Plugins run in separate processes
2. **Credential handling**: Use `SensitiveFields: []string{"fieldName"}` on the Descriptor and `schemahelper.WithWriteOnly()` on the property for sensitive inputs
3. **Input validation**: Always validate inputs even after schema validation
4. **Network access**: Plugins can make network calls; review plugin sources
5. **Handshake validation**: Plugin protocol includes version handshake

## Debugging Plugins

### Enable debug logging

```bash
scafctl --log-level -1 run solution -f solution.yaml
```

### Test plugin manually

```bash
# Run plugin directly (will wait for gRPC connection)
./my-plugin

# In another terminal, use grpcurl to test
grpcurl -plaintext localhost:1234 list
```

### Common issues

| Issue | Cause | Solution |
|-------|-------|----------|
| "plugin not found" | Wrong path or not executable | Check `chmod +x` and path |
| "handshake failed" | Version mismatch | Rebuild with same scafctl version |
| "unknown provider" | Typo in provider name | Check `GetProviders()` return |
| "connection refused" | Plugin crashed on startup | Run plugin manually to see error |

## Distribution

### As Go Module

```bash
# Users can install with
go install github.com/myorg/scafctl-plugin-foo@latest
mv $(go env GOPATH)/bin/scafctl-plugin-foo ~/.scafctl/plugins/
```

### As Binary Release

```bash
# Download and install
curl -L https://github.com/myorg/scafctl-plugin-foo/releases/download/v1.0.0/plugin-linux-amd64 \
    -o ~/.scafctl/plugins/scafctl-plugin-foo
chmod +x ~/.scafctl/plugins/scafctl-plugin-foo
```

### As Container

```dockerfile
FROM golang:1.21 as builder
COPY . /app
WORKDIR /app
RUN go build -o plugin .

FROM alpine:latest
COPY --from=builder /app/plugin /plugin
ENTRYPOINT ["/plugin"]
```

## Publishing to the Catalog

Once your plugin is built, you can distribute it via the scafctl catalog as a **provider** or **auth-handler** artifact.

### Build and Push a Provider

```bash
# Build the provider artifact into the local catalog
scafctl build provider ./my-provider --version 1.0.0

# Push to a remote registry
scafctl catalog push my-provider@1.0.0 --catalog ghcr.io/myorg

# The artifact is stored at: ghcr.io/myorg/providers/my-provider:1.0.0
```

### Build and Push an Auth Handler

```bash
# Build the auth handler artifact
scafctl build auth-handler ./my-auth-handler --version 1.0.0

# Push to a remote registry
scafctl catalog push my-auth-handler@1.0.0 --catalog ghcr.io/myorg

# The artifact is stored at: ghcr.io/myorg/auth-handlers/my-auth-handler:1.0.0
```

### Pull and Use

```bash
# Pull a provider from a remote registry
scafctl catalog pull ghcr.io/myorg/providers/my-provider@1.0.0

# The provider is now available locally
scafctl catalog list --kind provider
```

## Next Steps

- Review the [echo plugin example](../examples/plugins/echo/) for a working reference
- See [Provider Development Guide](provider-development.md) for provider patterns
- Check [Contributing Guidelines](../CONTRIBUTING.md) for submission process
