# API Server Configuration Example

This example shows how to configure the scafctl REST API server.

## Usage

```bash
# Start the server with this configuration
scafctl serve --config api-server-config.yaml

# Export OpenAPI spec
scafctl serve openapi --format yaml --output openapi.yaml
```

## Files

- `api-server-config.yaml` — Full API server configuration with all options
