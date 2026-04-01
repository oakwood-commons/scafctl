# REST API Server Examples

Example configurations and curl commands for the scafctl REST API server.

## Quick Start

```bash
# Start with defaults
scafctl serve

# Start on custom port
scafctl serve --port 9090
```

## Configuration Examples

### Minimal Config

Use `minimal-config.yaml` for local development with no authentication:

```bash
scafctl serve --config examples/serve/minimal-config.yaml
```

### Production Config

Use `production-config.yaml` for a production deployment with Entra OIDC, TLS, and audit logging.

## API Interaction

See `curl-examples.sh` (bash) or `curl-examples.ps1` (PowerShell) for a comprehensive set of commands covering all endpoints.

## OpenAPI Export

```bash
# Export as JSON
scafctl serve openapi --format json --output openapi.json

# Export as YAML
scafctl serve openapi --format yaml --output openapi.yaml
```
