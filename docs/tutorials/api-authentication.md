---
title: "API Mode Authentication"
weight: 138
---

# API Mode Authentication

This tutorial explains how to configure token acquisition in the scafctl API server so that providers can automatically obtain downstream tokens.

## Overview

When scafctl runs in **API mode** (`scafctl serve`), incoming requests carry a caller's bearer token. Providers that call downstream APIs often need a *different* token scoped to a specific resource (e.g., Azure Key Vault, Microsoft Graph). The `tokensource` package centralizes token acquisition, caches results, and deduplicates concurrent requests.

Four token acquisition strategies are available:

| Strategy | Use Case |
|----------|----------|
| **OBO (On-Behalf-Of)** | Exchange a user's token for a downstream token (Entra) |
| **Client Credentials** | Acquire a token using the server's own identity (Entra) |
| **GitHub App / PAT** | Acquire tokens using the server's GitHub identity |
| **Pass-Through** | Forward a provider-specific token from the API request headers |

## Architecture: TokenSource Interface

All token acquisition flows through a single interface:

```go
type TokenSource interface {
    Name() string
    GetToken(ctx context.Context, opts RequestOptions) (Token, error)
}
```

Token sources are organized in a `tokensource.Registry` stored in the request context. Consumers call `tokensource.GetToken(ctx, providerName, opts)` without knowing the underlying mechanism.

### CallerScope

Each token request includes a `CallerScope` that determines which flow is used. This is the primary routing mechanism for token acquisition:

| CallerScope | Purpose | Examples |
|-------------|---------|----------|
| `RequesterCaller` | Solution-level work on behalf of the user | Querying Graph API, creating GitHub PRs, fetching user data |
| `ServerCaller` | Infrastructure/operational concerns | Reading solutions from storage, catalog access, plugin fetching |

How each identity responds to the scope:

| CallerScope | Entra Behavior | GitHub Behavior |
|-------------|---------------|-----------------|
| `RequesterCaller` | OBO for user callers, client credentials for app callers | Unsupported (returns error) |
| `ServerCaller` | Client credentials flow (server's own identity) | Installation token or PAT |

Pass-through tokens bypass `CallerScope` entirely -- they are the caller's own token forwarded as-is from request headers.

### CLI vs API Mode

The registry is built differently depending on the runtime mode:

| Mode | Source | What Gets Registered |
|------|--------|---------------------|
| **CLI** | `auth.Registry` (auth handlers) | Each handler wrapped in a `AuthHandlerAdapter` |
| **API** | `serveridentity` providers | `Entra`, `GitHubApp`, or `GitHubPAT` registered directly |

In CLI mode, auth handlers use interactive/cached user credentials. In API mode, server identity providers use configured secrets and keys.

## Prerequisites

- scafctl installed and configured
- An Azure Entra ID app registration (for OBO/client credentials), and/or
- A GitHub App or PAT (for GitHub identity)
- Credentials stored securely (env vars or files)

## Configuration

Token sources are configured in the `apiServer` section of your scafctl config file.

### Entra ID Identity (OBO + Client Credentials)

```yaml
apiServer:
  host: 0.0.0.0
  port: 8080
  auth:
    azureOIDC:
      enabled: true
      tenantId: "your-tenant-id"
      clientId: "your-client-id"
  identity:
    entra:
      tenantId: "your-tenant-id"
      clientId: "your-client-id"
      credential:
        type: secret
        clientSecret: "env://SCAFCTL_API_ENTRA_CLIENT_SECRET"
      allowedFlows:
        flows:
          - obo
          - client_credentials
      tokenManager:
        cacheSize: 1024
        expiryBuffer: "10m"
        cleanupInterval: "5m"
```

#### Credential Types

**Client Secret** (simplest):

```yaml
credential:
  type: secret
  clientSecret: "env://SCAFCTL_API_ENTRA_CLIENT_SECRET"
```

The `clientSecret` field uses a `SecretRef` URI. Supported schemes:
- `env://VAR_NAME` -- reads the secret from an environment variable
- `file:///path/to/secret` -- reads the secret from a file

**Workload Identity Federation** (for Kubernetes/cloud workloads):

```yaml
credential:
  type: wif
  wifTokenPath: "/var/run/secrets/azure/tokens/federated-token"
```

#### Allowed Flows

The `allowedFlows` section is a security boundary controlling which flows the server may use:

| Configuration | Behavior |
|--------------|----------|
| Omitted (nil) | Only OBO is permitted (default) |
| Present with flows list | Only listed flows are permitted |
| Present with empty flows | All token acquisition is denied |

```yaml
# Only allow OBO (default behavior when omitted)
allowedFlows:
  flows:
    - obo

# Allow both OBO and client credentials
allowedFlows:
  flows:
    - obo
    - client_credentials
```

#### Token Manager

The `tokenManager` section controls caching and deduplication:

| Field | Default | Description |
|-------|---------|-------------|
| `cacheSize` | 1024 | Max cached tokens (LRU eviction) |
| `expiryBuffer` | 10m | Safety margin before token expiry |
| `cleanupInterval` | 5m | Background eviction interval |
| `expiryThreshold` | 30m | Minimum TTL to cache a token |
| `slowThreshold` | 500ms | Follower bail-out duration |
| `retryFollowerOnError` | true | Followers retry on leader error |

### GitHub Identity

GitHub identity enables the server to acquire tokens using its own GitHub credentials. Two credential types are supported.

**GitHub App**:

```yaml
apiServer:
  identity:
    github:
      hostname: "github.com"  # optional, set for GHES
      credential:
        type: app
        app:
          clientId: "x22333"
          installationId: 78901234
          privateKey: "env://GITHUB_APP_PRIVATE_KEY"
      tokenManager:
        cacheSize: 2
        expiryBuffer: "5m"
```

The App identity mints installation access tokens using a JWT signed with the private key. Only `ServerCaller` is supported -- the server always uses its own identity, not the requester's.

#### GitHub Token Manager

The `tokenManager` section is optional. When omitted (`nil`), caching is disabled. When present with zero values, the following defaults apply:

| Field | Default | Description |
|-------|---------|-------------|
| `cacheSize` | 2 | Max cached tokens (LRU eviction) |
| `expiryBuffer` | 5m | Safety margin before token expiry |
| `cleanupInterval` | 0 (disabled) | No periodic cleanup; relies on expiry buffer |
| `expiryThreshold` | 10m | Minimum TTL to cache a token |
| `slowThreshold` | 500ms | Follower bail-out duration |
| `retryFollowerOnError` | true | Followers retry on leader error |

**Personal Access Token**:

```yaml
apiServer:
  identity:
    github:
      credential:
        type: pat
        pat:
          token: "env://GITHUB_TOKEN"
```

PATs are static and do not expire from the API's perspective. No `tokenManager` is needed.

### Catalog Authentication in API Mode

When the API server fetches solutions or plugins from authenticated OCI registries (e.g., `ghcr.io`, `*.azurecr.io`, `*.pkg.dev`), it uses `BridgeAuthToRegistry` with `CallerScope: ServerCaller`. This means the server's own identity is used -- not the API caller's token.

The catalog config's `authProvider` field determines which token source is used:

```yaml
catalogs:
  - name: internal
    type: oci
    url: "oci://ghcr.io/my-org/catalog"
    authProvider: github
    authScope: ""  # not needed for GitHub
```

At runtime:
1. `BridgeAuthToRegistry` calls `tokensource.GetToken(ctx, "github", {Caller: ServerCaller})`
2. The GitHub identity returns an installation token (App) or static token (PAT)
3. The token is used as the OCI registry password with the appropriate username convention

Registry username conventions:

| Registry | Username |
|----------|----------|
| `ghcr.io` | `oauth2accesstoken` |
| `*.azurecr.io` | `00000000-0000-0000-0000-000000000000` |
| `gcr.io`, `*.pkg.dev` | `oauth2accesstoken` |
| Custom OAuth2 | Handler-defined or `oauth2accesstoken` |

### Token Pass-Through

Pass-through forwards provider-specific tokens from API request headers directly to providers without exchanging them. This bypasses the `TokenSource` interface entirely -- the middleware extracts headers and stores them in the request context.

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

## Using Token Sources in Solutions

Use the `authProvider` and `scope` inputs on the HTTP provider:

### OBO / Client Credentials Example

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
1. Check for a pass-through token in context (none for "entra")
2. Call `tokensource.GetToken(ctx, "entra", opts)` with `CallerScope: RequesterCaller`
3. The Entra `TokenSource` selects OBO or client_credentials based on caller type
4. Inject the resulting token as `Authorization: Bearer <token>`
5. On 401 response, retry with `ForceRefresh: true`

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
1. Call `ExtractPassthroughTokenFromContext(ctx, "Github")` -- finds the token from `X-Authorization-Github`
2. Skip the `TokenSource` registry entirely
3. Inject the pass-through token as `Authorization: Bearer <token>`

**Note**: Pass-through provider names are case-sensitive after canonical HTTP header normalization. Use the exact casing from your `allowedHeaders` config.

## How It Works at Runtime

```
API Request with Bearer token + optional X-Authorization-* headers
        |
        v
+-----------------------+
|  Auth Middleware       |  Validates caller token, extracts claims
|  (Azure OIDC)         |  Sets callerType = "user" or "app"
+-----------------------+
        |
        v
+-----------------------+
|  TokenPassthrough      |  Extracts X-Authorization-* headers
|  Middleware            |  Stores tokens in request context
+-----------------------+
        |
        v
+-----------------------+
|  Solution Execution    |
|  HTTP Provider         |
+-----------------------+
        |
        |--- authProvider set? ---> ExtractPassthroughTokenFromContext()
        |                           (pass-through check)
        |                                |
        |                     found?  ---+--- yes --> use directly
        |                                |
        |                                no
        |                                |
        |                                v
        |                    tokensource.GetToken(ctx, name, opts)
        |                                |
        |                                v
        |                    +----------------------+
        |                    | tokensource.Registry |
        |                    |  +----------------+  |
        |                    |  | Entra          |  |
        |                    |  |  ServerCaller  |  |
        |                    |  |  -> CC flow    |  |
        |                    |  | RequesterCaller|  |
        |                    |  |  -> OBO/CC     |  |
        |                    |  +----------------+  |
        |                    |  +----------------+  |
        |                    |  | GitHub         |  |
        |                    |  |  ServerCaller  |  |
        |                    |  |  -> App/PAT    |  |
        |                    |  +----------------+  |
        |                    +----------------------+
        |
        v
   Authorization: Bearer <token>
```

## Security Considerations

1. **Flow allow-list**: Always explicitly list permitted flows. Omitting `allowedFlows` defaults to OBO-only, which is the safest default.
2. **Secret storage**: Use `env://` or `file://` references for credentials -- never inline secrets in config files.
3. **Token pass-through headers**: Only configure headers for services you trust callers to provide tokens for.
4. **Scope requirement**: The Entra token source requires a non-empty `scope` when called with `RequesterCaller`.
5. **Cache isolation**: Token cache keys include a SHA-256 hash of the caller token, ensuring tokens are never shared across callers.
6. **GitHub CallerScope**: The GitHub identity only supports `ServerCaller`. It cannot be used to acquire tokens on behalf of API callers.

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| "scope is required for token delegation" | Missing `scope` in HTTP provider inputs | Add `scope` field to the provider inputs |
| "unsupported caller: requester" | GitHub identity called with RequesterCaller | Use pass-through for caller GitHub tokens |
| "no strategy found for caller" | Token source doesn't support the requested CallerScope | Check provider supports the caller type |
| "tokensource: source not found: X" | No identity configured for that name | Check `identity` config section |
| "token for provider X not found in context" | Missing `X-Authorization-X` header | Ensure the caller sends the header |
| "env:// scheme requires a variable name" | Empty `SecretRef` | Set the env var name after `env://` |
| Token expires immediately | `expiryBuffer` too large | Reduce the buffer or check token TTL |
| "no caller token in context" | Entra OBO called but no bearer token present | Ensure auth middleware is enabled and caller is authenticated |

## Next Steps

- [API Server Tutorial](api-server-tutorial.md) -- Full API server setup
- [Authentication Tutorial](auth-tutorial.md) -- CLI-mode auth handlers
- [HTTP Provider Tutorial](http-provider-tutorial.md) -- HTTP provider reference
- [Configuration Tutorial](config-tutorial.md) -- Config file management
