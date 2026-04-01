---
title: REST API Server Tutorial
weight: 50
---

# REST API Server

scafctl includes a built-in REST API server that exposes all major CLI features as HTTP endpoints. The server uses [chi](https://github.com/go-chi/chi) for routing and [Huma](https://huma.rocks) for OpenAPI-compliant endpoint registration.

## Starting the Server

```bash
# Start with defaults (port 8080, host 127.0.0.1)
scafctl serve

# Start on a custom port
scafctl serve --port 9090

# Start with TLS
scafctl serve --enable-tls --tls-cert cert.pem --tls-key key.pem

# Custom API version prefix
scafctl serve --api-version v2
```

## Configuration

The server reads its configuration from the `apiServer` section of the scafctl config file:

```yaml
apiServer:
  host: "127.0.0.1"   # use 0.0.0.0 to expose publicly
  port: 8080
  apiVersion: "v1"
  shutdownTimeout: "30s"
  requestTimeout: "60s"
  maxRequestSize: 10485760  # 10MB
  maxConcurrent: 1000

  tls:
    enabled: false
    cert: "/etc/ssl/cert.pem"
    key: "/etc/ssl/key.pem"

  cors:
    enabled: true
    allowedOrigins: ["*"]
    allowedMethods: ["GET", "POST", "PUT", "DELETE"]
    allowedHeaders: ["Content-Type", "Authorization"]
    maxAge: 3600

  rateLimit:
    global:
      maxRequests: 100
      window: "1m"

  auth:
    azureOIDC:
      enabled: false
      tenantId: "your-tenant-id"
      clientId: "your-client-id"

  compression:
    level: 6

  audit:
    enabled: true

  tracing:
    enabled: false
```

CLI flags override configuration file values.

## Endpoints

### Health and Operational

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | API root with HATEOAS links |
| GET | `/health` | Health check with component status |
| GET | `/health/live` | Liveness probe (always 200) |
| GET | `/health/ready` | Readiness probe (503 during shutdown) |
| GET | `/metrics` | Prometheus metrics |

### Solutions

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/solutions/lint` | Lint a solution file |
| POST | `/v1/solutions/inspect` | Inspect a solution structure |
| POST | `/v1/solutions/dryrun` | Dry-run a solution |

### Providers

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/providers` | List all providers |
| GET | `/v1/providers/{name}` | Get provider details |
| GET | `/v1/providers/{name}/schema` | Get provider JSON schema |

### Evaluation

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/eval/cel` | Evaluate a CEL expression |
| POST | `/v1/eval/template` | Evaluate a Go template |

### Catalogs, Schemas, Config, Snapshots

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/catalogs` | List catalogs |
| GET | `/v1/catalogs/{name}` | Get catalog details |
| GET | `/v1/schemas` | List available schemas |
| GET | `/v1/schemas/{name}` | Get a specific schema |
| POST | `/v1/schemas/validate` | Validate data against a schema |
| GET | `/v1/config` | Get current configuration |
| GET | `/v1/settings` | Get scafctl settings |
| GET | `/v1/snapshots` | List snapshots |
| GET | `/v1/snapshots/{id}` | Get a specific snapshot |

### Explain and Diff

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/explain` | Explain a solution structure |
| POST | `/v1/diff` | Diff two solutions |

### Admin

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/admin/info` | Server info (version, uptime) |
| POST | `/v1/admin/reload-config` | Reload configuration |
| POST | `/v1/admin/clear-cache` | Clear caches |

## OpenAPI Specification

Export the full OpenAPI spec without starting the server:

```bash
# JSON to stdout
scafctl serve openapi

# YAML to file
scafctl serve openapi --format yaml --output openapi.yaml
```

The spec is also served at `/{version}/openapi.json` when the server is running.

## Authentication

The server supports Azure Entra ID (formerly Azure AD) OIDC authentication. When enabled, all business endpoints require a valid JWT bearer token. Health probes and metrics bypass authentication.

```yaml
apiServer:
  auth:
    azureOIDC:
      enabled: true
      tenantId: "00000000-0000-0000-0000-000000000000"
      clientId: "00000000-0000-0000-0000-000000000001"
```

## Middleware Stack

The server uses a two-layer middleware stack:

**Global middleware** (all routes including health probes):
1. Recovery (panic handling)
2. Request ID generation
3. Trailing slash normalization
4. Request logging

**API middleware** (business endpoints only):
1. CORS
2. Request timeout
3. Concurrency throttling
4. Authentication (Entra OIDC)
5. Rate limiting
6. Request size limits
7. Response compression
8. Security headers
9. Prometheus metrics
10. Audit logging
11. OpenTelemetry tracing

## Example: Lint a Solution via API

```bash
curl -X POST http://localhost:8080/v1/solutions/lint \
  -H "Content-Type: application/json" \
  -d '{"path": "./solution.yaml"}'
```

Response:

```json
{
  "file": "./solution.yaml",
  "findings": [],
  "errorCount": 0,
  "warnCount": 0,
  "infoCount": 0
}
```

## Example: List Providers

```bash
curl http://localhost:8080/v1/providers
```

## Graceful Shutdown

The server handles SIGINT and SIGTERM signals for graceful shutdown. During shutdown:

1. The readiness probe returns 503
2. In-flight requests are given time to complete (configurable via `shutdownTimeout`)
3. New connections are rejected
