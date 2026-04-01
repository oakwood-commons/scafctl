# Web API Implementation Plan

## Overview

Add a [Huma](https://github.com/danielgtaylor/huma)+[chi](https://github.com/go-chi/chi)-based REST API to scafctl, started via a new `scafctl serve` command. The API mirrors all major CLI features (run, render, lint, eval, catalog, solutions, providers, config, etc.) with Entra OIDC authentication, Prometheus metrics, OpenTelemetry tracing, audit logging, CEL-based filtering, and admin endpoints.

Architecture follows the [jqapi](https://github.com/ford-cloud/jqapi) reference implementation: chi router → layered middleware → Huma endpoint registration → handler context with dependency injection. All operations are synchronous request-response initially, with an async task model planned as future work.

---

## Phase 1: Server Foundation (`pkg/api/`)

### Step 1.1: Create API Server Package

Create `pkg/api/server.go` with a `Server` struct modeled after jqapi's `internal/server/server.go`.

**Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `cfg` | `*config.Config` | Application configuration |
| `router` | `*chi.Mux` | Root chi router |
| `apiRouter` | `chi.Router` | API route group with heavy middleware |
| `api` | `huma.API` | Huma API instance |
| `httpSrv` | `*http.Server` | Go HTTP server |
| `isShuttingDown` | `int32` | Atomic shutdown flag |
| `startTime` | `time.Time` | Server start time |
| `logger` | `logr.Logger` | Structured logger |
| `providerReg` | `*provider.Registry` | Provider registry |
| `authReg` | `*auth.Registry` | Auth handler registry |

**Constructor:**

```go
func NewServer(cfg *config.Config, opts ...ServerOption) (*Server, error)
```

Functional options pattern (like `pkg/mcp/server.go`):

- `WithServerLogger(lgr logr.Logger)`
- `WithServerRegistry(reg *provider.Registry)`
- `WithServerAuthRegistry(reg *auth.Registry)`
- `WithServerVersion(version string)`
- `WithServerContext(ctx context.Context)`

**Methods:**

- `Router() *chi.Mux` — returns root router for middleware setup
- `SetAPIRouter(r chi.Router)` — sets the API route group with heavy middleware
- `APIRouter() chi.Router` — returns API router (falls back to root if unset)
- `API() huma.API` — returns Huma API for endpoint registration
- `InitAPI()` — initializes Huma after middleware setup (must be called after `SetupMiddleware`)
- `Start() error` — starts the HTTP server (blocks until shutdown)
- `Shutdown(ctx context.Context) error` — graceful shutdown with configurable timeout

**Graceful Shutdown:**

- Signal handling: SIGINT/SIGTERM
- Context cancellation propagation
- Configurable shutdown timeout (default: 30s)
- Sets `isShuttingDown` flag so readiness probes return 503

**Reference:**

- jqapi `internal/server/server.go` — chi+Huma server pattern
- scafctl `pkg/mcp/server.go` — functional options, `mergeContext` for layered cancellation

### Step 1.2: Create API Config Types

Extend `pkg/config/types.go` by adding an `APIServer` field to the `Config` struct:

```go
type Config struct {
    // ... existing fields ...
    APIServer APIServerConfig `json:"apiServer,omitempty" yaml:"apiServer,omitempty"`
}
```

New `APIServerConfig` struct:

```go
type APIServerConfig struct {
    Host            string                    `json:"host"            yaml:"host"            doc:"Host to bind to"                                       example:"127.0.0.1"`
    Port            int                       `json:"port"            yaml:"port"            doc:"Port to listen on"                                     example:"8080"  maximum:"65535"`
    APIVersion      string                    `json:"apiVersion"      yaml:"apiVersion"      doc:"API version prefix"                                    example:"v1"`
    ShutdownTimeout string                    `json:"shutdownTimeout" yaml:"shutdownTimeout" doc:"Graceful shutdown timeout"                              example:"30s"`
    RequestTimeout  string                    `json:"requestTimeout"  yaml:"requestTimeout"  doc:"Default request timeout"                               example:"60s"`
    MaxRequestSize  int64                     `json:"maxRequestSize"  yaml:"maxRequestSize"  doc:"Maximum request body size in bytes"                    example:"10485760"`
    TLS             APITLSConfig              `json:"tls"             yaml:"tls"             doc:"TLS configuration"`
    CORS            APICORSConfig             `json:"cors"            yaml:"cors"            doc:"CORS configuration"`
    RateLimit       APIRateLimitConfig        `json:"rateLimit"       yaml:"rateLimit"       doc:"Rate limiting configuration"`
    Auth            APIAuthConfig             `json:"auth"            yaml:"auth"            doc:"Authentication configuration"`
    Compression     APICompressionConfig      `json:"compression"     yaml:"compression"     doc:"Response compression configuration"`
    OpenAPI         APIOpenAPIConfig           `json:"openAPI"         yaml:"openAPI"         doc:"OpenAPI specification configuration"`
    Profiler        APIProfilerConfig         `json:"profiler"        yaml:"profiler"        doc:"Profiler configuration"`
    Audit           APIAuditConfig            `json:"audit"           yaml:"audit"           doc:"Audit logging configuration"`
    Tracing         APITracingConfig          `json:"tracing"         yaml:"tracing"         doc:"OpenTelemetry tracing configuration"`
}
```

**Sub-config structs:**

| Struct | Fields |
|--------|--------|
| `APITLSConfig` | `Enabled bool`, `Cert string`, `Key string` |
| `APICORSConfig` | `Enabled bool`, `AllowedOrigins []string`, `AllowedMethods []string`, `AllowedHeaders []string`, `MaxAge int` |
| `APIRateLimitConfig` | `Global *APIRateLimitEntry`, `Endpoints map[string]*APIRateLimitEntry` |
| `APIRateLimitEntry` | `MaxRequests int`, `Window string` |
| `APIAuthConfig` | `AzureOIDC APIAzureOIDCConfig` |
| `APIAzureOIDCConfig` | `Enabled bool`, `TenantID string`, `ClientID string` |
| `APICompressionConfig` | `Level int` |
| `APIOpenAPIConfig` | `Servers []APIOpenAPIServerConfig`, `Title string`, `Description string` |
| `APIOpenAPIServerConfig` | `URL string`, `Description string` |
| `APIProfilerConfig` | `Enabled bool`, `AllowUnauthProd bool` |
| `APIAuditConfig` | `Enabled bool` |
| `APITracingConfig` | references existing `TelemetryConfig` |

**Constants** (add to `pkg/settings/settings.go`):

```go
const (
    DefaultAPIPort            = 8080
    DefaultAPIHost            = "127.0.0.1"
    DefaultAPIVersion         = "v1"
    DefaultShutdownTimeout    = "30s"
    DefaultRequestTimeout     = "60s"
    DefaultMaxRequestSize     = 10 * 1024 * 1024 // 10MB
    DefaultCompressionLevel   = 6
)
```

### Step 1.3: Huma API Initialization

Create `pkg/api/huma.go` with Huma configuration builder methods:

```go
func (s *Server) buildHumaConfig(apiVersion string) huma.Config
func (s *Server) configureOpenAPIInfo(humaConfig *huma.Config)
func (s *Server) configureOpenAPIServers(humaConfig *huma.Config)
func (s *Server) configureSecuritySchemes(humaConfig *huma.Config)
func (s *Server) configureDocsPaths(humaConfig *huma.Config, apiVersion string)
```

- Security scheme: OAuth2 with Entra client ID scope (`api://{clientID}/.default`)
- Uses `humachi.New(apiRouter, humaConfig)` adapter
- Configures docs path at `/{version}/docs` and OpenAPI at `/{version}/openapi.json`
- Default format: `application/json`

**Reference:** jqapi `Server.buildHumaConfig()`, `Server.InitAPI()`

---

## Phase 2: Middleware Stack (`pkg/api/middleware/`)

### Step 2.1: Two-Layer Middleware Setup

Create `pkg/api/middleware.go`:

```go
func SetupMiddleware(router *chi.Mux, cfg *APIServerConfig) (chi.Router, error)
```

Returns the API route group (`chi.Router`) with heavy middleware applied. Health probes and operational endpoints registered on the root router bypass the API middleware entirely.

**Global Middleware** (all routes including health probes):

| Order | Middleware | Purpose |
|-------|-----------|---------|
| 1 | Panic Recovery | Catches panics, logs stack trace, returns 500, records metric |
| 2 | Request ID | Generates unique ID per request (`X-Request-ID` header) |
| 3 | Strip Slashes | Normalizes trailing slashes in URLs |
| 4 | Request Logging | Structured log of every request (method, path, status, duration) |

**API Middleware** (returned `chi.Router`, business endpoints only):

| Order | Middleware | Purpose |
|-------|-----------|---------|
| 1 | CORS | Cross-Origin Resource Sharing (if enabled) |
| 2 | Timeout | Enforce request timeouts (prevents resource exhaustion) |
| 3 | Max Concurrent Requests | Request throttling (chi Throttle) |
| 4 | Method-Allowed | 405 enforcement before auth (per RFC 7231) |
| 5 | Authentication | Entra OIDC JWT validation |
| 6 | Rate Limiting | Per-IP rate limiting (global + per-endpoint) |
| 7 | Request Size Limits | Prevent oversized requests |
| 8 | Compression | gzip/deflate response compression |
| 9 | Security Headers | `X-Content-Type-Options`, `X-Frame-Options`, etc. |
| 10 | Metrics | Record per-request metrics |
| 11 | Audit Logging | Structured audit log for compliance |

**Security requirement:** Returns error if Entra OIDC is enabled but misconfigured (refuses to start unauthenticated when auth is expected).

**Reference:** jqapi `internal/api/middleware.go` — exact same two-layer pattern

### Step 2.2: Entra OIDC Auth Middleware

Create `pkg/api/middleware/auth.go`:

- JWKS endpoint discovery and caching
- JWT validation: signature, expiration, audience, issuer
- Tenant ID and Client ID verification
- Extract claims and inject into request context (user identity, groups, roles)
- Leverage existing `pkg/auth/entra/` for token handling patterns
- Follow jqapi's `middleware.NewAzureOIDCHandler()` pattern

*Can be developed in parallel with Step 2.3.*

### Step 2.3: Rate Limiting & Security Middleware

Create `pkg/api/middleware/ratelimit.go`:

- Per-IP rate limiting using token bucket or sliding window algorithm
- Global rate limit configuration
- Per-endpoint rate limit overrides
- Returns `429 Too Many Requests` with `Retry-After` header

Create `pkg/api/middleware/security.go`:

- Security headers: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Strict-Transport-Security` (when TLS), `Content-Security-Policy`
- Request size limits: reject requests exceeding `MaxRequestSize`
- Max concurrent requests: chi Throttle-based in-flight request limiting

*Can be developed in parallel with Step 2.2.*

### Step 2.4: Metrics Middleware

Create `pkg/api/middleware/metrics.go`:

- Record per-request metrics: count, duration histogram, status code distribution
- Per-endpoint breakdown
- Integrate with existing `pkg/metrics/` infrastructure

### Step 2.5: Audit Logging Middleware

Create `pkg/api/middleware/audit.go`:

Structured audit log for every API request including:

| Field | Source | Description |
|-------|--------|-------------|
| Caller identity | Entra claims | User/service principal making the request |
| Source IP | Request | Client IP address (respects trusted proxy headers) |
| Request ID | Middleware | Unique request identifier |
| Method + Path | Request | HTTP method and URL path |
| Request body summary | Request | For mutations (POST/PUT/PATCH), a redacted summary of the body |
| Response status | Response | HTTP status code |
| Duration | Timer | Request processing time |

- Uses existing `pkg/logger/` for structured output
- Configurable via `APIServer.Audit.Enabled`

**Security: Body Redaction**

Before writing request bodies to the audit log, sensitive fields are redacted to prevent credential leakage. The following patterns are redacted:

- Fields with keys matching `password`, `secret`, `token`, `key`, `credential`, `authorization` (case-insensitive)
- The `Authorization` HTTP header is never logged
- Redacted values are replaced with `"[REDACTED]"`

This prevents OWASP-listed sensitive data exposure through audit logs.

### Step 2.6: OpenTelemetry Tracing Middleware

Create `pkg/api/middleware/tracing.go`:

- Propagate trace context from incoming requests (W3C Trace Context headers)
- Integrate with existing `pkg/telemetry/` OTLP pipeline
- Add span attributes: endpoint, method, status code, user identity
- Trace context flows through handler → domain package calls
- Create child spans for significant operations within handlers

---

## Phase 3: Handler Context & Shared Types (`pkg/api/`)

### Step 3.1: Handler Context (Dependency Injection)

*Depends on Phase 1.*

Create `pkg/api/context.go`:

```go
type HandlerContext struct {
    Config           *config.Config
    ProviderRegistry *provider.Registry
    AuthRegistry     *auth.Registry
    Logger           logr.Logger
    IsShuttingDown   *int32
    StartTime        time.Time
}

func NewHandlerContext(
    cfg *config.Config,
    providerReg *provider.Registry,
    authReg *auth.Registry,
    logger logr.Logger,
    isShuttingDown *int32,
    startTime time.Time,
) *HandlerContext
```

All handlers access scafctl domain packages through this struct (like jqapi's `HandlerContext`). Components can be swapped with mock implementations for testing.

### Step 3.2: Error Handling

Create `pkg/api/errors.go`:

```go
func HandleError(ctx context.Context, err error, operation string, statusCode int, userMessage string) error
func HandleValidationError(ctx context.Context, fieldName, message string) error
```

- Log the error with context
- Map scafctl domain errors to HTTP status codes
- Return appropriate Huma errors
- Use RFC 7807 Problem Details format (`application/problem+json`)

*Can be developed in parallel with Step 3.3.*

### Step 3.3: Shared Types

Create `pkg/api/types.go` with request/response types. All types use Huma validation tags.

**Pagination:**

```go
type PaginationInfo struct {
    Page       int  `json:"page"        minimum:"1"     maximum:"10000" example:"1"   doc:"Current page number (1-indexed)"`
    PerPage    int  `json:"per_page"    minimum:"1"     maximum:"1000"  example:"100" doc:"Items per page"`
    Total      int  `json:"total"       minimum:"0"     maximum:"1000000" example:"250" doc:"Total number of items"`
    TotalPages int  `json:"total_pages" minimum:"0"     maximum:"10000" example:"3"   doc:"Total number of pages"`
    HasNext    bool `json:"has_next"    example:"true"  doc:"Whether there is a next page"`
    HasPrev    bool `json:"has_prev"    example:"false" doc:"Whether there is a previous page"`
}
```

**Health:**

```go
type HealthResponseBody struct {
    Status     string            `json:"status"     maxLength:"20" pattern:"..." example:"healthy" doc:"Health status"`
    Version    string            `json:"version"    maxLength:"50" pattern:"..." example:"v1.0.0"  doc:"Service version"`
    Uptime     string            `json:"uptime"     maxLength:"50" pattern:"..." example:"1h30m"   doc:"Service uptime"`
    Components []ComponentStatus `json:"components" maxItems:"100" doc:"Component health statuses"`
}
```

**Root:**

```go
type RootResponse struct {
    Name        string    `json:"name"        maxLength:"100" example:"scafctl API" doc:"API name"`
    Version     string    `json:"version"     maxLength:"50"  example:"v1.0.0"      doc:"API version"`
    Description string    `json:"description" maxLength:"500"                        doc:"API description"`
    Links       []APILink `json:"links"       maxItems:"50"                          doc:"Related API links"`
}
```

*Can be developed in parallel with Step 3.2.*

### Step 3.4: CEL Filtering

Create `pkg/api/filtering.go`:

- Add `filter` query parameter to all list endpoints
- Reuse `pkg/celexp/EvaluateExpression()` to evaluate filter expressions against each result item
- Apply pagination after filtering

**Security: CEL Sandboxing**

User-supplied CEL filter expressions are evaluated in a restricted sandbox:

- **Cost limit**: All filter evaluations use `celexp.WithCostLimit()` (default: `settings.DefaultAPIFilterCostLimit = 10000`) to prevent denial-of-service via expensive expressions
- **Max expression length**: The `filter` query parameter is limited to 2000 characters via Huma validation (`maxLength:"2000"` on `FilterParam`)
- **Read-only**: CEL expressions cannot perform I/O, mutate state, or access the filesystem — enforced by the CEL runtime
- **No custom functions**: Filter expressions only have access to the standard CEL library plus the `item` variable

**Example usage:**

```
GET /v1/solutions?filter=name.startsWith("my-") && tags.exists(t, t == "production")
GET /v1/providers?filter=name == "helm" || name == "terraform"
GET /v1/catalogs?filter=solutions.size() > 10
```

---

## Phase 4: API Endpoints (`pkg/api/endpoints/`)

All steps in this phase *depend on Phase 3* and can run *in parallel with each other*.

### Step 4.1: Health & Operational Endpoints

`pkg/api/endpoints/health.go`:

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/health` | Comprehensive health check (component statuses) | No |
| GET | `/health/live` | Liveness probe (lightweight, process alive) | No |
| GET | `/health/ready` | Readiness probe (checks dependencies, returns 503 during shutdown) | No |
| GET | `/` | Root endpoint: API name, version, description, links to docs/openapi/health/metrics | No |

All registered on the **root router** (bypass auth/rate-limit middleware).

### Step 4.2: Prometheus Metrics Endpoint

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/metrics` | Prometheus-format metrics | No |

Registered on the **root router**. Exposes:

- HTTP request count (by endpoint, method, status)
- Request duration histogram
- Active connections gauge
- Leverages existing `pkg/metrics/` infrastructure

### Step 4.3: Solution Endpoints

`pkg/api/endpoints/solutions.go`:

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/v1/solutions` | List available solutions (supports `?filter=`) | Yes |
| GET | `/v1/solutions/{name}` | Inspect/get a solution's details | Yes |
| POST | `/v1/solutions/run` | Run a solution (mirrors `scafctl run`) | Yes |
| POST | `/v1/solutions/render` | Render solution templates (mirrors `scafctl render`) | Yes |
| POST | `/v1/solutions/lint` | Validate solution structure (mirrors `scafctl lint`) | Yes |
| POST | `/v1/solutions/test` | Run functional tests (mirrors `scafctl test`) | Yes |
| POST | `/v1/solutions/dryrun` | Dry-run a solution (mirrors `scafctl run --dry-run`) | Yes |

Handlers delegate to existing domain packages: `pkg/scaffold/`, `pkg/lint/`, `pkg/solution/`, `pkg/dryrun/`.

### Step 4.4: Provider Endpoints

`pkg/api/endpoints/providers.go`:

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/v1/providers` | List registered providers (supports `?filter=`) | Yes |
| GET | `/v1/providers/{name}` | Inspect a provider | Yes |
| GET | `/v1/providers/{name}/schema` | Get provider JSON schema | Yes |

Delegates to `pkg/provider/` registry.

### Step 4.5: CEL & Template Eval Endpoints

`pkg/api/endpoints/eval.go`:

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| POST | `/v1/eval/cel` | Evaluate CEL expressions (mirrors `scafctl eval cel`) | Yes |
| POST | `/v1/eval/template` | Evaluate Go templates (mirrors `scafctl eval tmpl`) | Yes |

Delegates to `pkg/celexp/` and `pkg/gotmpl/`.

### Step 4.6: Catalog Endpoints

`pkg/api/endpoints/catalog.go`:

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/v1/catalogs` | List catalogs (supports `?filter=`) | Yes |
| GET | `/v1/catalogs/{name}` | Get catalog details | Yes |
| GET | `/v1/catalogs/{name}/solutions` | List solutions in a catalog | Yes |
| POST | `/v1/catalogs/sync` | Sync catalog(s) | Yes |

Delegates to `pkg/catalog/`.

### Step 4.7: Schema Endpoints

`pkg/api/endpoints/schema.go`:

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/v1/schemas` | List available schemas (supports `?filter=`) | Yes |
| GET | `/v1/schemas/{name}` | Get a schema | Yes |
| POST | `/v1/schemas/validate` | Validate data against a schema | Yes |

Delegates to `pkg/schema/`.

### Step 4.8: Config & Settings Endpoints

`pkg/api/endpoints/config.go`:

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/v1/config` | Get current configuration | Yes |
| GET | `/v1/settings` | Get runtime settings | Yes |

Delegates to `pkg/config/` and `pkg/settings/`.

### Step 4.9: Snapshot Endpoints

`pkg/api/endpoints/snapshot.go`:

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/v1/snapshots` | List snapshots (supports `?filter=`) | Yes |
| GET | `/v1/snapshots/{id}` | Get snapshot details | Yes |

Delegates to `pkg/dryrun/` and `pkg/scaffold/`.

### Step 4.10: Explain & Diff Endpoints

`pkg/api/endpoints/explain.go`:

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| POST | `/v1/explain` | Detailed solution analysis (mirrors `scafctl explain`) | Yes |
| POST | `/v1/diff` | Solution diff (mirrors `scafctl solution diff`) | Yes |

Delegates to existing domain packages.

### Step 4.11: Admin Endpoints

`pkg/api/endpoints/admin.go`:

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| POST | `/v1/admin/reload-config` | Hot-reload configuration without restart | Yes* |
| POST | `/v1/admin/clear-cache` | Clear CEL/template/HTTP caches | Yes* |
| GET | `/v1/admin/info` | Server info (version, uptime, config summary, component status) | Yes* |

**Authorization model:**

- When Entra OIDC auth is **enabled**: admin endpoints require a valid JWT **and** the caller must have the `admin` role in their Entra token claims (`roles` claim). Requests without the `admin` role receive `403 Forbidden`.
- When auth is **disabled**: admin endpoints are restricted to **localhost-only** requests. Requests from non-loopback IPs receive `403 Forbidden`.

Registered under the "Admin" OpenAPI tag.

### Step 4.12: Endpoint Registration

Create `pkg/api/register.go`:

```go
func RegisterEndpoints(api huma.API, router *chi.Mux, ctx *HandlerContext)
func RegisterEndpointsForExport(api huma.API) // for OpenAPI spec generation without starting server
```

- Central registration following jqapi pattern
- Each endpoint registered with `huma.Register(api, huma.Operation{...}, handler)`
- OpenAPI tags: Solutions, Providers, Eval, Catalogs, Schemas, Config, Health, Admin
- Security definitions per endpoint (auth-required vs public)

---

## Phase 5: CLI Command & Wiring (`pkg/cmd/scafctl/serve/`)

### Step 5.1: `scafctl serve` Command

*Depends on Phases 1-4.*

Create `pkg/cmd/scafctl/serve/serve.go`:

```go
func CommandServe(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command
```

Pattern follows `pkg/cmd/scafctl/mcp/serve.go` (dependency injection from Cobra context).

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--host` | `127.0.0.1` | Host to bind to |
| `--port` | `8080` | Port to listen on (1-65535) |
| `--tls-cert` | | Path to TLS certificate |
| `--tls-key` | | Path to TLS private key |
| `--enable-tls` | `false` | Enable TLS |
| `--api-version` | `v1` | API version prefix |

**`runServe()` flow:**

1. Pull dependencies from Cobra context (logger, config, auth registry, provider registry)
2. Create server via `api.NewServer(cfg, ...opts)`
3. Setup middleware via `api.SetupMiddleware(srv.Router(), cfg.APIServer)`
4. Set API router: `srv.SetAPIRouter(apiRouter)`
5. Initialize Huma API: `srv.InitAPI()`
6. Register endpoints: `api.RegisterEndpoints(srv.API(), srv.Router(), handlerCtx)`
7. Register custom 404/405 handlers
8. Start server: `srv.Start()` (blocks until SIGINT/SIGTERM)

**No business logic in this package** — only dependency wiring.

### Step 5.2: Wire into Root Command

Edit `pkg/cmd/scafctl/root.go`:

- Add `serve` command to `groupPlugin` or a new `groupServer` command group
- Register: `cmd.AddCommand(serve.CommandServe(cliParams, ioStreams, path))`

### Step 5.3: OpenAPI Export Subcommand

Create `pkg/cmd/scafctl/serve/openapi.go`:

```
scafctl serve openapi --format json|yaml --output file.json
```

- Generates the full OpenAPI specification without starting the server
- Creates a minimal router + Huma API, registers all endpoints via `RegisterEndpointsForExport()`
- Follows jqapi's `exportOpenAPI` pattern

---

## Phase 6: MCP & Documentation

### Step 6.1: MCP Tools

*Can be developed in parallel with Step 6.2.*

Create `pkg/mcp/tools_api.go`:

- `s.registerAPITools()` — tools for inspecting API server configuration, generating OpenAPI spec

### Step 6.2: Documentation & Examples

*Can be developed in parallel with Step 6.1.*

| Artifact | Path | Description |
|----------|------|-------------|
| Tutorial | `docs/tutorials/serve-tutorial.md` | Tutorial for starting and using the API |
| Design doc | `docs/design/api-surface.md` | Detailed API surface design |
| Examples | `examples/serve/` | Example configurations and curl commands |
| Index update | `docs/_index.md` | Add API section to documentation index |

---

## Phase 7: Testing

### Step 7.1: Unit Tests

- `*_test.go` for every `pkg/api/` file
- Middleware tests (like jqapi's `middleware_test.go`)
- Audit logging tests (including body redaction verification), tracing tests, CEL filtering tests

**Benchmark tests** for every endpoint and middleware (like jqapi's `handlers_bench_test.go`):

| Target | Benchmark | Metrics |
|--------|-----------|--------|
| Health endpoints | `BenchmarkHealthEndpoint` | req/s, allocs/op |
| Solution list + pagination | `BenchmarkSolutionList` | req/s, allocs/op |
| CEL filter evaluation | `BenchmarkFilterItems` | ns/op, allocs/op |
| Audit middleware | `BenchmarkAuditLog` | ns/op, allocs/op |
| Auth middleware (JWT validation) | `BenchmarkAuthMiddleware` | ns/op, allocs/op |
| Rate limit middleware | `BenchmarkRateLimit` | ns/op, allocs/op |
| Request logging middleware | `BenchmarkRequestLogging` | ns/op, allocs/op |
| Pagination helper | `BenchmarkPaginate` | ns/op, allocs/op |
| Full middleware stack | `BenchmarkMiddlewareStack` | ns/op, allocs/op |

### Step 7.2: Integration Tests

- Add `scafctl serve` to `tests/integration/cli_test.go`
- Create `tests/integration/api_test.go` — API endpoint integration tests using `httptest`
- Solution integration tests in `tests/integration/solutions/` for API-driven workflows

### Step 7.3: Verify

- `task test:e2e`

---

## Files

### Existing Files to Modify

| File | Change |
|------|--------|
| `pkg/config/types.go` | Add `APIServer APIServerConfig` field to `Config` |
| `pkg/cmd/scafctl/root.go` | Register `serve` command |
| `pkg/settings/settings.go` | Add API server defaults/constants |
| `go.mod` | Add `huma/v2`, `chi/v5`, `go-chi/cors` dependencies |
| `tests/integration/cli_test.go` | Add `scafctl serve` integration tests |

### New Files to Create

**Core server:**

| File | Purpose |
|------|---------|
| `pkg/api/server.go` | Server struct, NewServer, Start, Shutdown |
| `pkg/api/huma.go` | Huma config builder |
| `pkg/api/context.go` | HandlerContext (dependency injection) |
| `pkg/api/errors.go` | Centralized error handling |
| `pkg/api/types.go` | Shared request/response types |
| `pkg/api/register.go` | Endpoint registration |
| `pkg/api/filtering.go` | CEL-based list filtering |

**Middleware:**

| File | Purpose |
|------|---------|
| `pkg/api/middleware.go` | SetupMiddleware (two-layer) |
| `pkg/api/middleware/auth.go` | Entra OIDC middleware |
| `pkg/api/middleware/ratelimit.go` | Rate limiting |
| `pkg/api/middleware/security.go` | Security headers & limits |
| `pkg/api/middleware/metrics.go` | Request metrics collection |
| `pkg/api/middleware/audit.go` | Audit logging |
| `pkg/api/middleware/tracing.go` | OpenTelemetry tracing |

**Endpoints:**

| File | Purpose |
|------|---------|
| `pkg/api/endpoints/health.go` | Health/liveness/readiness |
| `pkg/api/endpoints/solutions.go` | Solution CRUD + run/render/lint/test/dryrun |
| `pkg/api/endpoints/providers.go` | Provider listing/inspection |
| `pkg/api/endpoints/eval.go` | CEL/template evaluation |
| `pkg/api/endpoints/catalog.go` | Catalog management |
| `pkg/api/endpoints/schema.go` | Schema operations |
| `pkg/api/endpoints/config.go` | Config/settings |
| `pkg/api/endpoints/snapshot.go` | Snapshots |
| `pkg/api/endpoints/explain.go` | Explain/diff |
| `pkg/api/endpoints/admin.go` | Admin endpoints (reload, cache, info) |

**CLI command:**

| File | Purpose |
|------|---------|
| `pkg/cmd/scafctl/serve/serve.go` | CLI `serve` command |
| `pkg/cmd/scafctl/serve/openapi.go` | OpenAPI export subcommand |

**Other:**

| File | Purpose |
|------|---------|
| `pkg/mcp/tools_api.go` | MCP tools for API server |
| `docs/tutorials/serve.md` | Tutorial |
| `docs/design/api-surface.md` | API surface design doc |
| `examples/serve/` | Example configs |

### Reference Patterns (existing code to follow)

| Reference | What to learn from it |
|-----------|----------------------|
| `pkg/mcp/server.go` | Functional options, server lifecycle, context injection |
| `pkg/mcp/context.go` | Context setup with layered dependencies |
| `pkg/cmd/scafctl/mcp/serve.go` | Command wiring, dependency injection from Cobra context |
| `pkg/auth/entra/handler.go` | Entra auth implementation patterns |
| jqapi `internal/server/server.go` | chi+Huma server setup, InitAPI, graceful shutdown |
| jqapi `internal/api/middleware.go` | Two-layer middleware (global vs API group) |
| jqapi `internal/api/endpoints.go` | Huma endpoint registration with `huma.Register()` |
| jqapi `internal/api/handlers.go` | Handler pattern with HandlerContext |

---

## Verification

| # | Check | Type |
|---|-------|------|
| 1 | `go build ./...` compiles | Automated |
| 2 | `go test ./pkg/api/...` passes | Automated |
| 3 | `go test ./tests/integration/...` passes | Automated |
| 4 | `task test:e2e` passes | Automated |
| 5 | `golangci-lint run --fix` clean | Automated |
| 6 | `scafctl serve --port 8080` starts, `curl localhost:8080/health` → 200 | Manual |
| 7 | `curl localhost:8080/v1/docs` serves OpenAPI documentation | Manual |
| 8 | `scafctl serve openapi --format yaml` exports valid spec | Manual |
| 9 | `curl localhost:8080/metrics` returns Prometheus metrics | Manual |
| 10 | With `--enable-tls`, server starts on HTTPS | Manual |
| 11 | With Entra OIDC enabled, unauthenticated requests → 401 | Manual |
| 12 | `GET /v1/solutions?filter=name.startsWith("my-")` returns filtered results | Manual |
| 13 | `POST /v1/admin/reload-config` reloads config | Manual |
| 14 | Audit log entries appear for all API requests when audit is enabled | Manual |

---

## Decisions

| Decision | Rationale |
|----------|-----------|
| Package location: `pkg/api/` (not `internal/`) | Follows scafctl convention of `pkg/` for all packages |
| No business logic in command package | `pkg/cmd/scafctl/serve/` only wires deps; all logic in `pkg/api/` |
| Breaking changes acceptable | Per project conventions. New `APIServer` config field is additive |
| Entra OIDC only (no basic auth, no API keys) | Matches jqapi security stance |
| Health probes bypass auth | Following jqapi pattern — operational endpoints on root router skip API middleware |
| Synchronous request-response | All operations block until completion. Simpler to implement. Async model deferred |
| Single `v1` prefix | Configurable via `APIVersion`. Multi-version support is future work || JSON naming: camelCase for config, snake_case for API responses | Config types use `camelCase` (Go/YAML convention); API response types use `snake_case` (REST convention for external consumers). This is intentional and consistent within each layer |
| Offset-based pagination only | All list endpoints return bounded datasets (providers, solutions, catalogs are registry-backed, not database-backed). Offset pagination is sufficient. Cursor-based pagination deferred to future work |
| Admin authorization: role-based or localhost-only | When Entra auth is enabled, admin endpoints require `admin` role claim. When auth is disabled, admin endpoints are restricted to localhost. Defense in depth |
| Audit log body redaction | Request bodies logged in audit entries have sensitive fields (`password`, `secret`, `token`, `key`, `credential`) redacted to `[REDACTED]`. Prevents OWASP-listed credential leakage |
| CEL filter cost limit | API filter expressions use `WithCostLimit(settings.DefaultAPIFilterCostLimit)` (10,000) — lower than CLI default (1,000,000) — to prevent DoS via expensive user-supplied expressions |
| CEL eval cost limit | The eval endpoint uses `WithCostLimit(settings.DefaultAPIEvalCostLimit)` (100,000) — higher than the filter limit because eval is the primary use-case, but still bounded |
| Port type: `int` | Config and CLI flags use `int` for the port field, enabling numeric validation (1-65535). String-based port is error-prone |
---

## Future Work (not in scope for this plan)

### 1. Async Task Model

For long-running operations (solution run/render/lint/test):

- `POST /v1/solutions/run` → returns `202 Accepted` with a task ID
- `GET /v1/tasks/{id}` — poll for task status and result
- Optional SSE streaming (`GET /v1/tasks/{id}/stream`) for real-time progress events

**Implementation:**

- In-memory task store (`sync.Map` or mutex-protected map on `Server` struct)
- Task fields: ID, status (pending/running/completed/failed), progress %, log lines, result, timestamps
- Configurable TTL (e.g., 30 minutes), background goroutine evicts expired tasks
- Optional: filesystem persistence to XDG data dir for survival across restarts
- No external database required (no Redis, no SQLite, no Postgres)

### 2. Cursor-Based Pagination

Add optional cursor-based pagination alongside the existing offset-based model for endpoints that may grow to large datasets. Return `next_cursor` in response metadata, accept `cursor` + `limit` query parameters. Useful if/when list endpoints are backed by external storage instead of in-memory registries.

### 3. Multi-Version API Coexistence

Support running v1 and v2 simultaneously with different route groups and handler versions. Route groups are already version-prefixed to enable this.

### 4. WebSocket Support

Persistent bidirectional connections for interactive solution development sessions.

**Endpoint:** `GET /v1/ws` upgrades to WebSocket (via `gorilla/websocket` or `nhooyr.io/websocket`).

**Enables:**

- **Real-time streaming** — solution run output (resolver outputs, provider progress, action logs) streamed as they happen, rather than waiting for the full HTTP response
- **Interactive prompting** — server pushes input prompts to client mid-execution and waits for responses over the same connection
- **Live file watching** — server watches solution files for changes and pushes change notifications to the client, enabling hot-reload development workflows
- **Collaborative sessions** — multiple clients connect to the same session and see each other's changes

**Complexity considerations:**

- Connection lifecycle management (open, close, reconnect)
- Heartbeat/ping-pong for connection health
- Reconnection handling (client-side retry with backoff)
- Per-connection authentication (validate JWT on upgrade)
- State management (per-connection session state)

**When to add:** After the REST API is stable and a concrete client (web UI, VS Code extension) exists to consume it. chi supports WebSocket upgrades natively, so the existing router infrastructure carries over.
