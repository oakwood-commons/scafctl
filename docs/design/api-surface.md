# API Surface Design

Detailed API surface for the scafctl REST API server. All endpoints use JSON request/response bodies and follow REST conventions.

## Base URL

```
http://localhost:8080
```

Version-prefixed endpoints use `/{apiVersion}/` (default: `/v1/`).

## Authentication

- **Method**: Entra OIDC (Azure AD) JWT Bearer tokens
- **Header**: `Authorization: Bearer <token>`
- **Public endpoints**: Health probes and metrics bypass authentication
- **Admin endpoints**: Require authentication + `admin` role claim (or localhost-only when auth is disabled)

## Response Format

### Success (single resource)
```json
{
  "fieldA": "value",
  "fieldB": 42
}
```

### Success (collection with pagination)
```json
{
  "items": [...],
  "pagination": {
    "page": 1,
    "per_page": 100,
    "total_items": 250,
    "total_pages": 3,
    "has_more": true
  }
}
```

### Error (RFC 7807 Problem Details)
```json
{
  "title": "Bad Request",
  "status": 400,
  "detail": "validation failed: name is required"
}
```

---

## Naming Conventions

| Layer | Convention | Example |
|-------|-----------|---------|
| Config (YAML/JSON) | camelCase | `apiVersion`, `shutdownTimeout` |
| API response fields | snake_case | `per_page`, `total_items`, `has_more` |
| URL paths | lowercase, kebab-case for multi-word | `/v1/solutions`, `/v1/admin/reload-config` |
| Query parameters | snake_case | `per_page`, `filter` |

---

## Endpoints

### Operational (Root Router — No Auth)

| Method | Path | Description | Status Codes |
|--------|------|-------------|-------------|
| GET | `/` | API root with HATEOAS links | 200 |
| GET | `/health` | Full health check with component status | 200 |
| GET | `/health/live` | Liveness probe | 200 |
| GET | `/health/ready` | Readiness probe | 200, 503 |
| GET | `/metrics` | Prometheus metrics | 200 |

### Solutions

| Method | Path | Description | Query Params | Status Codes |
|--------|------|-------------|-------------|-------------|
| POST | `/v1/solutions/run` | Run a solution (resolve + execute actions) | — | 200, 400, 422 |
| POST | `/v1/solutions/render` | Resolve inputs without executing actions | — | 200, 400, 422 |
| POST | `/v1/solutions/lint` | Lint a solution | — | 200, 400 |
| POST | `/v1/solutions/inspect` | Inspect solution structure | — | 200, 400 |
| POST | `/v1/solutions/test` | Validate solution against provider registry | — | 200, 400 |
| POST | `/v1/solutions/dryrun` | Dry-run a solution (what-if analysis) | — | 200, 400, 422 |

### Providers

| Method | Path | Description | Query Params | Status Codes |
|--------|------|-------------|-------------|-------------|
| GET | `/v1/providers` | List providers | `page`, `per_page`, `filter` | 200, 400 |
| GET | `/v1/providers/{name}` | Get provider details | — | 200, 404 |
| GET | `/v1/providers/{name}/schema` | Get provider schema | — | 200, 404 |

### Eval

| Method | Path | Description | Status Codes |
|--------|------|-------------|-------------|
| POST | `/v1/eval/cel` | Evaluate CEL expression | 200, 400 |
| POST | `/v1/eval/template` | Evaluate Go template | 200, 400 |

### Catalogs

| Method | Path | Description | Query Params | Status Codes |
|--------|------|-------------|-------------|-------------|
| GET | `/v1/catalogs` | List catalogs | `page`, `per_page`, `filter` | 200, 400 |
| GET | `/v1/catalogs/{name}` | Get catalog details | — | 200, 404 |
| GET | `/v1/catalogs/{name}/solutions` | List catalog solutions | `page`, `per_page` | 200, 404 |
| POST | `/v1/catalogs/sync` | Sync catalogs (planned) | — | 200, 400 |

### Schemas

| Method | Path | Description | Query Params | Status Codes |
|--------|------|-------------|-------------|-------------|
| GET | `/v1/schemas` | List schemas | `page`, `per_page` | 200 |
| GET | `/v1/schemas/{name}` | Get schema | — | 200, 404 |
| POST | `/v1/schemas/validate` | Validate against schema | — | 200, 400, 422 |

### Config

| Method | Path | Description | Status Codes |
|--------|------|-------------|-------------|
| GET | `/v1/config` | Get current config | 200 |
| GET | `/v1/settings` | Get runtime settings | 200 |

### Snapshots

| Method | Path | Description | Query Params | Status Codes |
|--------|------|-------------|-------------|-------------|
| GET | `/v1/snapshots` | List snapshots (planned) | `page`, `per_page` | 200 |
| GET | `/v1/snapshots/{id}` | Get snapshot details (planned) | — | 200, 404 |

### Explain & Diff

| Method | Path | Description | Status Codes |
|--------|------|-------------|-------------|
| POST | `/v1/explain` | Explain a solution | 200, 400 |
| POST | `/v1/diff` | Diff solutions | 200, 400 |

### Admin

| Method | Path | Description | Authorization | Status Codes |
|--------|------|-------------|--------------|-------------|
| GET | `/v1/admin/info` | Server info | admin role / localhost | 200, 403 |
| POST | `/v1/admin/reload-config` | Hot-reload config (planned) | admin role / localhost | 200, 403 |
| POST | `/v1/admin/clear-cache` | Clear caches (planned) | admin role / localhost | 200, 403 |

### Documentation (Auto-Generated by Huma)

| Method | Path | Description | Status Codes |
|--------|------|-------------|-------------|
| GET | `/v1/docs` | Interactive API documentation | 200 |
| GET | `/v1/openapi.json` | OpenAPI specification (JSON) | 200 |

---

## Common Query Parameters

| Parameter | Type | Description | Default | Constraints |
|-----------|------|-------------|---------|-------------|
| `page` | int | Page number (1-indexed) | 1 | 1–10000 |
| `per_page` | int | Items per page | 100 | 1–1000 |
| `filter` | string | CEL filter expression | — | max 2000 chars |

## Global Status Codes

These status codes can be returned by any endpoint, in addition to endpoint-specific codes:

| Code | Condition |
|------|-----------|
| 401 Unauthorized | Missing or invalid auth token (when auth is enabled) |
| 403 Forbidden | Insufficient role (admin endpoints), or non-loopback with auth disabled |
| 405 Method Not Allowed | HTTP method not supported for this resource |
| 413 Request Entity Too Large | Request body exceeds `maxRequestSize` (default 10 MB) |
| 429 Too Many Requests | Rate limit exceeded (includes `Retry-After` header) |
| 503 Service Unavailable | Server is shutting down (readiness probe only) |

---

## Middleware Stack

The API uses a two-layer middleware architecture. Health probes and metrics bypass the API middleware entirely.

### Global Middleware (All Routes)

| Order | Middleware | Purpose |
|-------|-----------|---------|
| 1 | Panic Recovery | Catches panics, logs stack trace, returns 500 |
| 2 | Request ID | Generates unique `X-Request-ID` per request |
| 3 | Strip Slashes | Normalizes trailing slashes in URLs |
| 4 | Request Logging | Structured log of method, path, status, duration |

### API Middleware (Business Endpoints)

| Order | Middleware | Purpose |
|-------|-----------|---------|
| 1 | CORS | Cross-Origin Resource Sharing (configurable) |
| 2 | Timeout | Request timeout enforcement |
| 3 | Throttle | Max concurrent connections |
| 4 | Authentication | Entra OIDC JWT validation |
| 5 | Rate Limiting | Per-IP sliding window limiter |
| 6 | Max Body Size | Request body size validation |
| 7 | Compression | gzip response compression |
| 8 | Security Headers | CSP, X-Frame-Options, HSTS, etc. |
| 9 | Metrics | OpenTelemetry metrics collection |
| 10 | Audit Logging | Structured audit logs with field redaction |
| 11 | Tracing | OpenTelemetry distributed tracing |

---

## Rate Limiting

When rate limiting is enabled, every API response includes standard rate limit headers:

| Header | Description |
|--------|-------------|
| `X-RateLimit-Limit` | Maximum requests allowed in the window |
| `X-RateLimit-Remaining` | Requests remaining in the current window |
| `X-RateLimit-Reset` | Unix timestamp when the rate limit window resets |
| `Retry-After` | Duration until retry is allowed (only on 429 responses) |

---

## Security

### Security Headers

All API responses include the following security headers:

| Header | Value |
|--------|-------|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `Content-Security-Policy` | `default-src 'none'` |
| `X-XSS-Protection` | `0` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Permissions-Policy` | `camera=(), microphone=(), geolocation=()` |
| `Strict-Transport-Security` | `max-age=63072000; includeSubDomains; preload` (TLS only) |

### TLS Support

The server supports TLS with configurable certificate and key paths. When TLS is enabled, HSTS headers are automatically added to all responses.

### CEL Filter Sandboxing

User-supplied filter expressions are evaluated with:
- **Cost limit**: 10,000 (lower than CLI default of 1,000,000)
- **Read-only**: No I/O, no mutations, no filesystem access
- **Max length**: 2,000 characters

### Audit Log Redaction

Request bodies in audit logs have sensitive fields redacted:
- Fields matching `password`, `secret`, `token`, `key`, `credential`, `authorization` → `[REDACTED]`

### Admin Authorization

| Auth State | Admin Access |
|-----------|-------------|
| Entra OIDC enabled | Requires `admin` role in JWT claims |
| Auth disabled | Localhost-only (non-loopback → 403) |

---

## Server Configuration

The API server is configured via the `apiServer` section in the scafctl config file:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `host` | string | `127.0.0.1` | Bind address |
| `port` | int | `8080` | Listen port |
| `apiVersion` | string | `v1` | URL path version prefix |
| `requestTimeout` | string | `30s` | Per-request timeout |
| `shutdownTimeout` | string | `30s` | Graceful shutdown window |
| `maxConcurrent` | int | `100` | Max concurrent connections |
| `maxRequestSize` | int64 | `10485760` | Max request body size (bytes) |
| `compression.level` | int | `6` | Gzip compression level |
| `cors.enabled` | bool | `false` | Enable CORS |
| `cors.allowedOrigins` | []string | — | Allowed origins |
| `cors.allowedMethods` | []string | — | Allowed HTTP methods |
| `cors.allowedHeaders` | []string | — | Allowed headers |
| `cors.maxAge` | int | — | Preflight cache duration (seconds) |
| `tls.enabled` | bool | `false` | Enable TLS |
| `tls.cert` | string | — | Path to TLS certificate |
| `tls.key` | string | — | Path to TLS private key |
| `auth.azureOIDC.enabled` | bool | `false` | Enable Entra OIDC auth |
| `auth.azureOIDC.tenantId` | string | — | Azure AD tenant ID |
| `auth.azureOIDC.clientId` | string | — | Azure AD client ID |
| `rateLimit.global.maxRequests` | int | — | Max requests per window |
| `rateLimit.global.window` | string | — | Rate limit window duration |
| `audit.enabled` | bool | `false` | Enable audit logging |
| `tracing.enabled` | bool | `false` | Enable OpenTelemetry tracing |
