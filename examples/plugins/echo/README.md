# Echo Plugin Example

This is an example go-plugin for scafctl that demonstrates how to create a simple provider.

> **Note**: This is the go-plugin source code. When distributed via the catalog, it becomes a **provider** artifact stored at `<registry>/providers/echo:<version>`.

## Building

```bash
go build -o echo-plugin main.go
```

## Publishing to the Catalog

```bash
# Build into local catalog
scafctl build provider . --version 1.0.0

# Push to remote registry
scafctl catalog push echo@1.0.0 --catalog ghcr.io/myorg
# Result: ghcr.io/myorg/providers/echo:1.0.0
```

## Usage

The plugin exposes a single "echo" provider that returns its input, optionally converting it to uppercase.

### Input Schema

- `message` (string, required): The message to echo
- `uppercase` (boolean, optional): Whether to convert the message to uppercase (default: false)

### Output Schema

- `echoed` (string): The echoed message

## Example

```yaml
providers:
  - name: echo
    provider: echo
    inputs:
      message: "Hello, World!"
      uppercase: true
```

This will output:
```json
{
  "echoed": "HELLO, WORLD!"
}
```
