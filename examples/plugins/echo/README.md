# Echo Plugin Example

This is an example plugin for scafctl that demonstrates how to create a simple provider plugin.

## Building

```bash
go build -o echo-plugin main.go
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
