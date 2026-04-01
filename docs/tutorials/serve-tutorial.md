---
title: "REST API Server Tutorial"
weight: 70
---

# REST API Server Tutorial

This tutorial walks you through starting scafctl's REST API server and interacting with its endpoints. The API mirrors all major CLI features — solutions, providers, catalogs, schemas, eval, config, and more — over HTTP with OpenAPI documentation.

## Prerequisites

- scafctl installed and on your `$PATH` (`scafctl version` should work)
- curl or any HTTP client for testing

## Quick Start

### Start the Server

```bash
scafctl serve
```

The server starts on `http://localhost:8080` by default.

### Verify It Works

```bash
# Root endpoint — lists available API links
curl http://localhost:8080/

# Health check
curl http://localhost:8080/health

# Liveness probe (for orchestrators)
curl http://localhost:8080/health/live

# Readiness probe
curl http://localhost:8080/health/ready
```

## Configuration

### Custom Port and Host

```bash
# Start on a custom port
scafctl serve --port 9090

# Bind to a specific interface
scafctl serve --host 127.0.0.1 --port 3000
```

### TLS

```bash
scafctl serve --enable-tls --tls-cert cert.pem --tls-key key.pem
```

### Configuration File

The API server can be fully configured via the scafctl config file:

```yaml
apiServer:
  host: "0.0.0.0"
  port: 8080
  apiVersion: "v1"
  shutdownTimeout: "30s"
  requestTimeout: "60s"
  maxRequestSize: 10485760  # 10MB
  compression:
    level: 6
  cors:
    enabled: true
    allowedOrigins:
      - "http://localhost:3000"
    allowedMethods:
      - "GET"
      - "POST"
    maxAge: 3600
  rateLimit:
    global:
      maxRequests: 100
      window: "1m"
  audit:
    enabled: true
```

## API Endpoints

### Solutions

```bash
# List available solutions
curl http://localhost:8080/v1/solutions

# List with CEL filter
curl 'http://localhost:8080/v1/solutions?filter=item.name.startsWith("my-")'

# Lint a solution
curl -X POST http://localhost:8080/v1/solutions/lint \
  -H "Content-Type: application/json" \
  -d '{"path": "./my-solution"}'

# Render a solution
curl -X POST http://localhost:8080/v1/solutions/render \
  -H "Content-Type: application/json" \
  -d '{"path": "./my-solution", "inputs": {"name": "test"}}'

# Run a solution (dry-run)
curl -X POST http://localhost:8080/v1/solutions/dryrun \
  -H "Content-Type: application/json" \
  -d '{"path": "./my-solution", "inputs": {"name": "test"}}'
```

### Providers

```bash
# List registered providers
curl http://localhost:8080/v1/providers

# Get provider details
curl http://localhost:8080/v1/providers/write-new

# Get provider JSON schema
curl http://localhost:8080/v1/providers/write-new/schema
```

### CEL & Template Evaluation

```bash
# Evaluate a CEL expression
curl -X POST http://localhost:8080/v1/eval/cel \
  -H "Content-Type: application/json" \
  -d '{"expression": "1 + 2"}'

# Evaluate a Go template
curl -X POST http://localhost:8080/v1/eval/template \
  -H "Content-Type: application/json" \
  -d '{"template": "Hello, {{.name}}!", "data": {"name": "World"}}'
```

### Catalogs

```bash
# List catalogs
curl http://localhost:8080/v1/catalogs

# Get catalog details
curl http://localhost:8080/v1/catalogs/default

# List solutions in a catalog
curl http://localhost:8080/v1/catalogs/default/solutions
```

### Config & Settings

```bash
# Get current configuration
curl http://localhost:8080/v1/config

# Get runtime settings
curl http://localhost:8080/v1/settings
```

### Admin Endpoints

```bash
# Server info (version, uptime, provider count)
curl http://localhost:8080/v1/admin/info

# Hot-reload configuration
curl -X POST http://localhost:8080/v1/admin/reload-config

# Clear caches
curl -X POST http://localhost:8080/v1/admin/clear-cache
```

## Pagination and Filtering

List endpoints support pagination and CEL filtering:

```bash
# Paginate results
curl 'http://localhost:8080/v1/solutions?page=1&per_page=10'

# Filter with CEL expressions
curl 'http://localhost:8080/v1/providers?filter=item.name=="helm"'
```

## Metrics and Observability

```bash
# Prometheus metrics
curl http://localhost:8080/metrics
```

The server also supports OpenTelemetry tracing when configured:

```yaml
telemetry:
  endpoint: "localhost:4317"
  insecure: true
```

## OpenAPI Documentation

The API automatically generates OpenAPI documentation:

```bash
# View interactive docs
open http://localhost:8080/v1/docs

# Get OpenAPI spec
curl http://localhost:8080/v1/openapi

# Export OpenAPI spec without starting the server
scafctl serve openapi --format json --output openapi.json
scafctl serve openapi --format yaml --output openapi.yaml
```

## Authentication

When Entra OIDC is enabled, all business endpoints require a valid JWT token:

```yaml
apiServer:
  auth:
    azureOIDC:
      enabled: true
      tenantId: "your-tenant-id"
      clientId: "your-client-id"
```

```bash
# Authenticated request
curl -H "Authorization: Bearer <token>" http://localhost:8080/v1/solutions
```

Health probes (`/health`, `/health/live`, `/health/ready`) and metrics (`/metrics`) bypass authentication for orchestrator compatibility.

## Graceful Shutdown

The server handles `SIGINT` and `SIGTERM` signals gracefully:

1. Sets the readiness probe to return 503 (stops receiving new traffic)
2. Waits for in-flight requests to complete (up to `shutdownTimeout`)
3. Shuts down cleanly

```bash
# Send SIGTERM to gracefully stop
kill -SIGTERM $(pgrep scafctl)
```
