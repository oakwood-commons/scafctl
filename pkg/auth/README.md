# Auth Package

Multi-provider authentication framework for scafctl with pluggable handlers, encrypted token caching, and a shared token-acquisition pipeline.

## Features

- **Pluggable Handlers**: Register any number of auth providers (Entra ID, GCP, GitHub) via a thread-safe `Registry`
- **Encrypted Token Cache**: Disk-backed cache using `pkg/secrets`, keyed by `(flow, fingerprint, scope)`
- **Generic Token Pipeline**: Shared `GetCachedOrAcquireToken[T]` eliminates boilerplate across handlers
- **Normalized Claims**: Common `Claims` struct maps provider-specific identity data into a single shape
- **Sentinel Errors**: Typed errors (`ErrNotAuthenticated`, `ErrTokenExpired`, `ErrInvalidScope`, etc.) for programmatic error handling
- **Optional Interfaces**: Handlers can implement `TokenLister`, `TokenPurger`, or `GroupsProvider` for extended capabilities
- **Context Integration**: Store and retrieve the `Registry` via `context.Context` helpers

## Architecture

```text
pkg/auth/
├── handler.go        # Handler interface, Flow/Token/Status types
├── registry.go       # Thread-safe handler registry
├── context.go        # Context helpers (WithRegistry, RegistryFromContext)
├── cache.go          # TokenCache — encrypted disk cache via pkg/secrets
├── token_acquire.go  # GetCachedOrAcquireToken[T] — shared generic pipeline
├── claims.go         # Normalized identity claims
├── errors.go         # Sentinel errors and helpers
├── flow.go           # ParseFlow — user-facing flow string → Flow constant
├── capability.go     # Handler capability flags
├── fingerprint.go    # SHA-256 identity fingerprint for cache keys
├── groups.go         # Optional GroupsProvider interface
├── mock.go           # MockHandler for testing
├── entra/            # Microsoft Entra ID handler
├── gcp/              # Google Cloud Platform handler
├── github/           # GitHub handler
└── oauth/            # Shared OAuth 2.0 utilities (PKCE, callback server)
```

## Core Interface

Every handler implements `Handler`:

```go
type Handler interface {
    Name() string
    DisplayName() string
    Login(ctx context.Context, opts LoginOptions) (*Result, error)
    Logout(ctx context.Context) error
    Status(ctx context.Context) (*Status, error)
    GetToken(ctx context.Context, opts TokenOptions) (*Token, error)
    InjectAuth(ctx context.Context, req *http.Request, opts TokenOptions) error
    SupportedFlows() []Flow
    Capabilities() []Capability
}
```

## Supported Flows

| Flow                | Entra | GCP | GitHub | Description                                |
|---------------------|-------|-----|--------|--------------------------------------------|
| `interactive`       | ✓     | ✓   | ✓      | Browser-based OAuth with PKCE              |
| `device_code`       | ✓     |     | ✓      | Device authorization grant                 |
| `service_principal` | ✓     | ✓   |        | Client credentials / SA key JWT            |
| `workload_identity` | ✓     | ✓   |        | Federated token exchange                   |
| `pat`               |       |     | ✓      | Personal access token                      |
| `metadata`          |       | ✓   |        | GCE metadata server                        |
| `gcloud_adc`        |       | ✓   |        | gcloud application-default credentials     |
| `github_app`        |       |     | ✓      | GitHub App installation token              |

## Quick Start

### Register Handlers

```go
reg := auth.NewRegistry()

entraHandler, _ := entra.New(entra.WithConfig(entraConfig))
reg.Register(entraHandler)

gcpHandler, _ := gcp.New(gcp.WithConfig(gcpConfig))
reg.Register(gcpHandler)

// Attach to context
ctx = auth.WithRegistry(ctx, reg)
```

### Login

```go
handler, _ := auth.GetHandler(ctx, "gcp")
result, err := handler.Login(ctx, auth.LoginOptions{
    Flow:   auth.FlowServicePrincipal,
    Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
})
```

### Acquire a Token

```go
token, err := handler.GetToken(ctx, auth.TokenOptions{
    Flow:  auth.FlowServicePrincipal,
    Scope: "https://www.googleapis.com/auth/cloud-platform",
})
```

### Inject Into HTTP Requests

```go
req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
err := handler.InjectAuth(ctx, req, auth.TokenOptions{
    Scope: "https://www.googleapis.com/auth/cloud-platform",
})
// req now has Authorization: Bearer <token>
```

## Token Cache

`TokenCache` provides encrypted, disk-backed token storage via `pkg/secrets`.

Cache keys follow the pattern `<prefix>:<flow>:<fingerprint>:<scope>` where the fingerprint is a truncated SHA-256 of the identity (e.g. client email or tenant+client ID).

```go
cache := auth.NewTokenCache(secretStore, "gcp-token")

// Store
cache.Set(ctx, auth.FlowServicePrincipal, "fingerprint", "scope", token)

// Retrieve (returns nil if missing or expired)
cached, _ := cache.Get(ctx, auth.FlowServicePrincipal, "fingerprint", "scope")

// Housekeeping
cache.PurgeExpired(ctx)
cache.Clear(ctx)
```

## Shared Token Pipeline

`GetCachedOrAcquireToken[T]` is the generic function all handlers use for the cache → acquire → store cycle:

```go
token, err := auth.GetCachedOrAcquireToken(
    ctx,
    handler.tokenCache,
    opts,
    auth.FlowServicePrincipal,
    opts.Scope,                                    // cache key
    func() (*Creds, error) { return loadCreds() }, // credential loader
    func(c *Creds) bool { return c == nil },       // nil check
    func(c *Creds) string { return fingerprint },  // cache fingerprint
    func(ctx context.Context, c *Creds, scope string) (*auth.Token, error) {
        return acquireToken(ctx, c, scope)         // actual token acquisition
    },
    "MyFlow",                                      // log prefix
)
```

## Sentinel Errors

| Error                       | When                                       |
|-----------------------------|--------------------------------------------|\n| `ErrNotAuthenticated`       | No cached credentials available             |\n| `ErrAuthenticationFailed`   | Login or token acquisition failed           |\n| `ErrTokenExpired`           | Cached token past expiry                    |\n| `ErrInvalidScope`           | Empty or missing scope                      |\n| `ErrConsentRequired`        | User interaction needed                     |\n| `ErrHandlerNotFound`        | Handler not in registry                     |\n| `ErrFlowNotSupported`       | Handler doesn't support requested flow      |\n| `ErrUserCancelled`          | User aborted interactive flow               |\n| `ErrTimeout`                | Operation timed out                         |\n| `ErrAlreadyAuthenticated`   | Already logged in                           |\n| `ErrGrantInvalid`           | Refresh token or grant revoked              |\n| `ErrCapabilityNotSupported` | Handler lacks required capability           |

Provider-specific sentinel errors follow the same pattern:

- `gcp.ErrNoServiceAccountConfigured`
- `gcp.ErrNoWorkloadIdentityConfigured`
- `gcp.ErrNoGcloudADCConfigured`

## Testing

Use `MockHandler` for unit testing code that depends on auth:

```go
mock := auth.NewMockHandler("mock")
mock.SetToken(&auth.Token{AccessToken: "test-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})
```

Each sub-package also exposes mocks (e.g. `gcp.NewMockHTTPClient()`) for testing handler internals.

## Capabilities

Handlers declare capabilities to let callers discover feature support:

| Capability                | Description                      |
|---------------------------|----------------------------------|
| `CapScopesOnLogin`        | Accepts scopes at login time     |
| `CapScopesOnTokenRequest` | Accepts per-request scopes       |
| `CapTenantID`             | Supports tenant ID parameter     |
| `CapHostname`             | Supports hostname (e.g. GHES)    |
| `CapFederatedToken`       | Accepts federated token input    |
| `CapCallbackPort`         | Configurable OAuth callback port |

```go
if auth.HasCapability(handler.Capabilities(), auth.CapScopesOnTokenRequest) {
    // safe to pass per-request scopes
}
```

## Related Documentation

- [Authentication Design](../../docs/design/auth.md) — architecture overview and design decisions
- [Auth Tutorial](../../docs/tutorials/auth-tutorial.md) — getting started guide
- [Auth Handler Development](../../docs/tutorials/auth-handler-development.md) — writing new handlers
