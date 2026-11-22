---
title: "API Mode Authentication"
weight: 138
---

# API Mode Authentication

This tutorial explains how authentication works in the scafctl API server (`scafctl serve`) and how providers automatically obtain tokens for downstream API calls.

## Overview

When scafctl runs in **API mode**, incoming requests carry a caller's bearer token. The API server validates this token, extracts identity claims, and makes both the claims and the raw token available in the request context. Providers (like the HTTP provider) use the `auth.Registry` to acquire downstream tokens via registered auth handlers.

Two token acquisition strategies are available:

| Strategy | Use Case |
|----------|----------|
| **Auth Handler** | Acquire a token via a registered handler (e.g., entra, github, gcp) |
| **Pass-Through** | Forward a provider-specific token from the API request headers |

## Architecture: Auth Handler Interface

All token acquisition flows through a single interface:

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

Auth handlers are organized in an `auth.Registry` stored in the request context. The HTTP provider calls `auth.RegistryFromContext(ctx)` to look up handlers by name.

### ServerContext

Each token request includes a `ServerContext` field that signals the execution context:

| ServerContext | Constant | Purpose |
|---------------|----------|---------|
| `Delegate` | `auth.Delegate` | Acting on behalf of the API caller (OBO flow) |
| `Server` | `auth.Server` | Acting as the server's own identity |

### CallerType

The middleware determines whether the API caller is a user or an application from the JWT `idtyp` claim:

| CallerType | Condition | Behavior |
|------------|-----------|----------|
| `CallerUser` | `idtyp` absent or not "app" | Handler may use OBO flow |
| `CallerMachine` | `idtyp == "app"` | Handler may use client credentials |

### Assertion (OBO)

When the `authProvider` matches the OIDC provider that authenticated the caller, the caller's raw bearer token is passed as the `Assertion` field in `TokenOptions`. This enables On-Behalf-Of token exchange.

### Capabilities

Handlers declare capabilities that control validation:

| Capability | Effect |
|-----------|--------|
| `CapScopesOnTokenRequest` | The `scope` input is **required** when this handler is used |
| `CapScopesOnLogin` | Handler accepts scopes during login |
| `CapTenantID` | Handler supports tenant override |
| `CapHostname` | Handler supports hostname override (e.g., GHES) |
| `CapFederatedToken` | Handler supports workload identity federation |

## Prerequisites

- scafctl installed and configured
- Auth handlers configured for server mode under `apiServer.auth.handlers`
- For API mode: Azure Entra OIDC configured for caller validation (optional)
- Credentials stored securely (env vars, files, or token cache)

## Configuration

### API Server Authentication (Caller Validation)

The `apiServer.auth.azureOIDC` section configures validation of incoming bearer tokens:

```yaml
apiServer:
  host: 0.0.0.0
  port: 8080
  auth:
    azureOIDC:
      enabled: true
      tenantId: "your-tenant-id"
      clientId: "your-client-id"
```

When enabled, the OIDC middleware validates the caller's JWT, extracts claims (`AuthClaims`), and stores both the claims and raw access token in the request context.

### Auth Handler Server-Mode Settings

The `apiServer.auth.handlers` section passes opaque configuration to auth handler plugins when running in server mode:

```yaml
apiServer:
  auth:
    handlers:
      entra:
        clientId: "00000000-0000-0000-0000-000000000000"
        tenantId: "00000000-0000-0000-0000-000000000001"
        # authority: "https://login.microsoftonline.com"  # optional
        serverFlow: "client_credentials"  # or "workload_identity"
        credential:
          clientSecret: "env://SCAFCTL_API_ENTRA_CLIENT_SECRET"
          # wifToken: "file:///var/run/secrets/tokens/token"  # when serverFlow is "workload_identity"
        delegated:  # optional; omitted = delegation denied
          userFlow: "obo"
          machine: false
      github:
        # hostname: "github.com"  # optional (or GHES hostname)
        serverFlow: "github_app"  # or "pat"
        credential:
          app:
            clientId: "Iv23li00000000000000"
            installationId: 12345678
            privateKey: "file:///path/to/private-key.pem"
          # pat:
          #   token: "env://GITHUB_TOKEN"  # when serverFlow is "pat"
        delegated: false  # set true to allow HTTP provider (auth.Delegate) requests using this credential
```

These settings are marshaled to JSON and forwarded to the respective plugin's `ActivateServerMode` RPC at startup. The host (`scafctl`) does not validate or interpret the values -- **the schema under each handler name is defined entirely by the plugin**. Both the `entra` and `github` plugins decode their `ServerConfig` with `DisallowUnknownFields`, so any unrecognized or CLI-only key (such as a top-level `clientSecret`, `federatedTokenFile`, `federatedToken`, `appId`, `privateKeyPath`, or `httpClient`) fails `scafctl serve` startup.

**Entra `ServerConfig` fields (`apiServer.auth.handlers.entra`):**

| Field | Required | Description |
|-------|----------|-------------|
| `clientId` | Yes | Azure application (client) ID |
| `tenantId` | Yes | Azure tenant ID (GUID or domain) |
| `authority` | No | Authority URL (`https://` with host; defaults to `https://login.microsoftonline.com`) |
| `serverFlow` | Yes | Server credential flow: `"client_credentials"` or `"workload_identity"` |
| `credential.clientSecret` | When `serverFlow: "client_credentials"` | `SecretRef` URI (`env://VAR` or `file:///path`) for the client secret |
| `credential.wifToken` | When `serverFlow: "workload_identity"` | `SecretRef` URI (`env://VAR` or `file:///path`) for the projected federated token (re-read per request for rotation) |
| `delegated` | No | Delegation routing for `auth.Delegate` requests. Omitted = all delegated requests denied; when present, at least one of `userFlow` or `machine` must be enabled |
| `delegated.userFlow` | No | Flow for user callers (`CallerUser`): `"obo"`, or matching `serverFlow` (`"client_credentials"` / `"workload_identity"`). Empty denies user delegation |
| `delegated.machine` | No | When `true`, machine callers (`CallerMachine`) acquire tokens using `serverFlow` |

### The ServerMode Optional Interface

Auth handler plugins opt into server mode by implementing the `ServerMode` interface from the plugin SDK:

```go
// ServerMode is an optional interface. Plugins that support running in
// a non-interactive API server implement this to receive server-mode settings.
type ServerMode interface {
    ActivateServerMode(ctx context.Context, settings json.RawMessage) error
}
```

When `scafctl serve` starts:
1. It iterates each handler name listed in `apiServer.auth.handlers`
2. It asserts the handler implements `auth.ServerMode`
3. It calls `ActivateServerMode(ctx, settings)` with the raw JSON from config
4. The plugin unmarshals and validates the settings internally

If a handler is listed in `apiServer.auth.handlers` but does not implement `ServerMode`, startup fails with an error.

### Server Mode Activation and CLI Handler Disabling

After activating server-mode handlers, the serve command **disables all remaining handlers** that were not explicitly configured. This prevents CLI-only flows (device-code, browser-based OAuth) from being accidentally invoked in the non-interactive API server.

The logic:
1. Handlers listed in `apiServer.auth.handlers` are activated via `ActivateServerMode`
2. All other registered handlers are disabled with reason "not configured for server mode"
3. Disabled handlers return an error if any provider attempts to use them

This means only handlers you explicitly configure for server mode are available during API request processing.

### CLI-Mode vs. Server-Mode Handler Configuration

CLI authentication (`scafctl auth login`, `scafctl run ...`) and API server mode (`scafctl serve`) use **separate configuration sections and schemas**:

- **CLI mode** reads top-level `auth.entra`, `auth.github`, and `auth.gcp` (plus `auth.handlers.<name>` for third-party or host-resolved handlers) and forwards them via `ConfigureAuthHandler`. Server-only keys (`serverFlow`, `credential`, `delegated`) are not part of the CLI schema and are ignored there.
- **Server mode** reads `apiServer.auth.handlers.<name>` and forwards it via `ActivateServerMode`. It is self-contained and ignores `auth.entra` / `auth.github`.

**Entra CLI configuration (`auth.entra`)** -- CLI keys only:

```yaml
auth:
  entra:
    tenantId: "common"
    clientId: "00000000-0000-0000-0000-000000000000"
    # authority: "https://login.microsoftonline.com"
    defaultScopes:
      - "openid"
      - "profile"
    # defaultFlow: "interactive"  # or "device_code"
    # Non-interactive CLI credentials (top-level or under profiles.<name>; override AZURE_* env vars):
    # clientSecret: "..."
    # federatedTokenFile: "/var/run/secrets/tokens/token"
    # HTTP client overrides for CLI token and Graph requests:
    # httpClient:
    #   timeout: "30s"
    #   retryMax: 3
```

**GitHub CLI configuration (`auth.github`)** -- CLI keys only:

```yaml
auth:
  github:
    hostname: "github.com"
    clientId: "Ov23li00000000000000"
    defaultScopes:
      - "gist"
      - "read:org"
      - "repo"
      - "workflow"
    # GitHub App installation flow for CLI mode:
    # appId: 123456
    # installationId: 12345678
    # privateKeyPath: "/path/to/private-key.pem"
```

> **Note (`httpClient` and top-level credentials):** In CLI mode, the Entra plugin honors `auth.entra.httpClient` as well as top-level `clientSecret`, `federatedTokenFile`, and `federatedToken` (with per-profile overrides under `auth.entra.profiles.<name>`). In server mode, `apiServer.auth.handlers.entra` does **not** read `auth.entra`, rejects `httpClient` and top-level credential fields (`DisallowUnknownFields`), and instead requires `SecretRef` URIs under `credential.clientSecret` or `credential.wifToken`.

### Catalog Authentication in API Mode

When the API server fetches solutions or plugins from authenticated OCI registries (e.g., `ghcr.io`, `*.azurecr.io`, `*.pkg.dev`), it uses `BridgeAuthToRegistry` with `ServerContext: auth.Server`. This means the server's own identity is used -- not the API caller's token.

The catalog config's `authProvider` field determines which auth handler is used:

```yaml
catalogs:
  - name: internal
    type: oci
    url: "oci://ghcr.io/my-org/catalog"
    authProvider: github
    authScope: ""  # not needed for GitHub
```

At runtime:
1. `BridgeAuthToRegistry` calls `handler.GetToken(ctx, TokenOptions{ServerContext: auth.Server})`
2. The handler returns a token using its configured credentials
3. The token is used as the OCI registry password with the appropriate username

### Token Pass-Through

Pass-through forwards provider-specific tokens from API request headers directly to providers without going through auth handlers. The middleware extracts `X-Authorization-*` headers and stores them in the request context.

```yaml
apiServer:
  tokenPassThrough:
    allowedHeaders:
      - Github
      - Artifactory
      - Custom-Service
```

This allows the following request headers to be passed through:
- `X-Authorization-Github` -> available as provider token for "Github"
- `X-Authorization-Artifactory` -> available as provider token for "Artifactory"
- `X-Authorization-Custom-Service` -> available as provider token for "Custom-Service"

When `tokenPassThrough` is omitted entirely, the default allows `Github` only.

## Using Auth in Solutions

Use the `authProvider` and `scope` inputs on the HTTP provider:

### Entra OBO Example

```yaml
spec:
  resolvers:
    graphProfile:
      description: Fetch user profile from Microsoft Graph
      resolve:
        with:
          - provider: http
            inputs:
              url: "https://graph.microsoft.com/v1.0/me"
              method: GET
              authProvider: entra
              scope: "https://graph.microsoft.com/.default"
```

The HTTP provider will:
1. Check for a pass-through token in context
2. Look up the "entra" handler from `auth.RegistryFromContext(ctx)`
3. Build `TokenOptions` with:
   - `Scope`: from the `scope` input
   - `Hostname`: for handlers advertising `token_hostname`, resolved from the `hostname` input (via `auth.handlers.<name>.hostname`) or derived from the `scheme://host[:port]` origin of `url`
   - `ServerContext`: `auth.Delegate` (acting on behalf of the caller)
   - `Assertion`: the caller's raw bearer token (enables OBO exchange)
   - `Caller`: `CallerUser` or `CallerMachine` (from JWT `idtyp` claim)
4. Call `handler.GetToken(ctx, opts)` to acquire the downstream token
5. Inject the resulting token as `Authorization: Bearer <token>`
6. On 401 response, retry with `ForceRefresh: true`

### Pass-Through Example

```yaml
spec:
  resolvers:
    repoInfo:
      description: Fetch repo info using caller's own GitHub token
      resolve:
        with:
          - provider: http
            inputs:
              url: "https://api.github.com/repos/org/repo"
              method: GET
              authProvider: Github
```

The HTTP provider will:
1. Check `TokensFromContext(ctx)` for a canonical match on "Github"
2. Find the token extracted from the `X-Authorization-Github` request header
3. Skip the auth handler registry entirely
4. Inject the pass-through token as `Authorization: Bearer <token>`

**Note**: Pass-through provider names are matched after canonical HTTP header normalization. Use the exact casing from your `allowedHeaders` config.

## How It Works at Runtime

```
API Request with Bearer token + optional X-Authorization-* headers
        |
        v
+-----------------------+
|  Auth Middleware       |  Validates caller JWT, extracts AuthClaims
|  (Azure OIDC)         |  Stores claims + raw token in context
+-----------------------+
        |
        v
+-----------------------+
|  TokenPassthrough      |  Extracts X-Authorization-* headers
|  Middleware            |  Stores tokens in context (map[string]string)
+-----------------------+
        |
        v
+-----------------------+
|  Solution Execution    |
|  HTTP Provider         |
+-----------------------+
        |
        |--- authProvider set? ---> extractHeaderToken(ctx, name)
        |                           (pass-through check)
        |                                |
        |                     found?  ---+--- yes --> use directly
        |                                |
        |                                no
        |                                |
        |                                v
        |                    handlerToken(ctx, name, scope, ...)
        |                                |
        |                                v
        |                    +------------------------+
        |                    | auth.Registry          |
        |                    |  +------------------+  |
        |                    |  | entra handler    |  |
        |                    |  |  Delegate+Assert |  |
        |                    |  |  -> OBO flow     |  |
        |                    |  |  Server          |  |
        |                    |  |  -> CC flow      |  |
        |                    |  +------------------+  |
        |                    |  +------------------+  |
        |                    |  | github handler   |  |
        |                    |  |  Server          |  |
        |                    |  |  -> App/PAT      |  |
        |                    |  +------------------+  |
        |                    +------------------------+
        |
        v
   Authorization: Bearer <token>
```

## Calling Context: Delegate vs Server

The `ServerContext` field on `TokenOptions` tells the auth handler **who the token is for**:

| Context | Constant | When Used | Example |
|---------|----------|-----------|----------|
| Delegate | `auth.Delegate` | Calling downstream APIs on behalf of the API caller | HTTP provider making Graph API calls |
| Server | `auth.Server` | Acting as the server's own identity | Fetching catalog items from OCI registries |

**Delegate** (downstream API calls): The HTTP provider always sets `ServerContext: auth.Delegate` when acquiring tokens. Combined with the caller's assertion (bearer token) and caller type, this enables the handler to perform On-Behalf-Of (OBO) token exchange or select the appropriate flow for the caller identity.

**Server** (infrastructure operations): `BridgeAuthToRegistry` sets `ServerContext: auth.Server` when fetching solutions or plugins from authenticated OCI registries. The server uses its own credentials (client secret, GitHub App key, workload identity) independent of the API caller.

## Caller Types

When the HTTP provider requests a token, it determines whether the original API caller is a user or a machine (service principal) from the validated JWT:

| CallerType | JWT Condition | Passed to Handler As | Typical Handler Behavior |
|------------|---------------|---------------------|-------------------------|
| `CallerUser` | `idtyp` claim absent or not `"app"` | `opts.Caller = CallerUser` | OBO flow, delegated permissions |
| `CallerMachine` | `idtyp == "app"` | `opts.Caller = CallerMachine` | Client credentials, app permissions |

The caller type is only populated when the `authProvider` input matches the OIDC provider that authenticated the request. This prevents caller identity from leaking to unrelated handlers.

## Security Considerations

1. **Secret storage**: Use environment variables or files for credentials -- never inline secrets in config files. The `SecretRef` type supports `env://VAR_NAME` and `file:///path` schemes.
2. **Token pass-through headers**: Only configure headers for services you trust callers to provide tokens for.
3. **Scope requirement**: Handlers with `CapScopesOnTokenRequest` capability require a non-empty `scope` input.
4. **Assertion forwarding**: The caller's bearer token is only forwarded as an assertion when `authProvider` matches the OIDC provider name. This prevents accidental token leakage to unrelated handlers.
5. **Handler plugins**: Use `apiServer.plugins.allowExternal: false` (default) to prevent untrusted plugins from being loaded in API mode.
6. **Server mode isolation**: Only handlers explicitly listed in `apiServer.auth.handlers` are available in the API server. All others are disabled to prevent interactive CLI flows from being invoked.

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| "scope is required when authProvider X is set" | Handler has `CapScopesOnTokenRequest` but no `scope` provided | Add `scope` field to the HTTP provider inputs |
| "hostname input is not supported by authProvider X" | `hostname` input set for a handler without `token_hostname` | Omit `hostname` or use a `token_hostname`-capable auth handler |
| "auth provider X not found" | No handler registered with that name | Ensure the handler plugin is installed and configured under `apiServer.auth.handlers` |
| "failed to parse server config: json: unknown field ..." | Unrecognized or CLI-only key under `apiServer.auth.handlers.<name>` | Use the plugin's `ServerConfig` schema (e.g., `credential.clientSecret` instead of top-level `clientSecret`) |
| "no auth registry in context for provider X" | Auth registry not wired into execution context | Ensure server setup registers the auth registry |
| "authProvider X token is empty in request headers" | `X-Authorization-X` header present but empty | Send a non-empty bearer token in the `X-Authorization-X` header |
| 401 from downstream API | Token expired or insufficient scope | Check handler token cache, verify scope is correct |

## Next Steps

- [API Server Tutorial](api-server-tutorial.md) -- Full API server setup
- [Authentication Tutorial](auth-tutorial.md) -- CLI-mode auth handlers
- [Provider Reference (HTTP)](provider-reference.md#http) -- HTTP provider reference
- [Configuration Tutorial](config-tutorial.md) -- Config file management
