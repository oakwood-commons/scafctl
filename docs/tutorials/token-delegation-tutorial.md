---
title: "Token Delegation"
weight: 138
---

# Token Delegation Tutorial

This tutorial explains how to configure token delegation in the scafctl API server so that HTTP provider requests can automatically acquire downstream tokens on behalf of the caller.

## Overview

When scafctl runs in **API mode** (`scafctl serve`), incoming requests carry a caller's bearer token. Providers that call downstream APIs often need a *different* token scoped to a specific resource (e.g., Azure Key Vault, Microsoft Graph). Token delegation centralizes this exchange, caches results, and deduplicates concurrent requests.

Three delegation strategies are available:

| Strategy | Use Case |
|----------|----------|
| **OBO (On-Behalf-Of)** | Exchange a user's token for a downstream token |
| **Client Credentials** | Acquire a token using the server's own identity |
| **Pass-Through** | Forward a provider-specific token from the API request headers |

## Prerequisites

- scafctl installed and configured
- An Azure Entra ID app registration (for OBO/client credentials)
- A client secret or workload identity federation configured

## Configuration

Token delegation is configured in the `apiServer` section of your scafctl config file (`~/.config/scafctl/config.yaml`).

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

The `allowedFlows` section is a security boundary controlling which delegation flows the server may use:

| Configuration | Behavior |
|--------------|----------|
| Omitted (nil) | Only OBO is permitted (default) |
| Present with flows list | Only listed flows are permitted |
| Present with empty flows | All delegation is denied |

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

### Token Pass-Through

Pass-through delegation forwards provider-specific tokens from API request headers directly to providers without exchanging them. This is useful when callers already have valid tokens for downstream services.

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

## Using Delegation in Solutions

Once configured, use the `authProvider` and `authScope` inputs on the HTTP provider:

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
              authScope: "https://graph.microsoft.com/.default"
```

The HTTP provider will:
1. Look up the `entra` delegator in the registry
2. Call `DelegateToken(ctx, scope)` which selects OBO or client_credentials based on the caller type
3. Inject the resulting token as `Authorization: Bearer <token>`
4. Cache the token for subsequent requests with the same scope

### Pass-Through Example

```yaml
spec:
  resolvers:
    repoInfo:
      description: Fetch repository info using caller's GitHub token
      resolve:
        with:
          - provider: http
            inputs:
              url: "https://api.github.com/repos/org/repo"
              method: GET
              authProvider: Github
```

The HTTP provider will:
1. Look up `Github` in the delegator registry
2. Fall back to `passThrough:Github` delegator
3. Retrieve the token from the request context (originally from `X-Authorization-Github` header)
4. Inject it as `Authorization: Bearer <token>`

## How It Works at Runtime

```
API Request with Bearer token
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
|  HTTP Provider         |---- authProvider: entra ----+
+-----------------------+                              |
                                                       v
                                        +----------------------+
                                        |  DelegatorRegistry    |
                                        |  +-----------------+  |
                                        |  | EntraDelegator  |  |
                                        |  |  FlowRegistry   |  |
                                        |  |  TokenCache     |  |
                                        |  +-----------------+  |
                                        |  +-----------------+  |
                                        |  | PassThrough:    |  |
                                        |  |  Github         |  |
                                        |  +-----------------+  |
                                        +----------------------+
```

## Verifying Configuration

Check that the API server starts correctly with delegation configured:

```bash
# Start the server
SCAFCTL_API_ENTRA_CLIENT_SECRET="your-secret" scafctl serve

# Verify health
curl http://localhost:8080/health

# Test with a bearer token (OBO flow)
curl -H "Authorization: Bearer <user-token>" \
     http://localhost:8080/v1/solutions/my-solution/run

# Test with pass-through token
curl -H "Authorization: Bearer <user-token>" \
     -H "X-Authorization-Github: ghp_xxxx" \
     http://localhost:8080/v1/solutions/my-solution/run
```

## Security Considerations

1. **Flow allow-list**: Always explicitly list permitted flows. Omitting `allowedFlows` defaults to OBO-only, which is the safest default.
2. **Secret storage**: Use `env://` or `file://` references for credentials -- never inline secrets in config files.
3. **Token pass-through headers**: Only configure headers for services you trust callers to provide tokens for.
4. **Scope validation**: The HTTP provider requires `authScope` when using Entra delegation with scope-based auth handlers.
5. **Cache isolation**: Token cache keys include a SHA-256 hash of the caller token, ensuring tokens are never shared across callers.

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| "delegation flow X is not permitted" | Flow not in `allowedFlows` | Add the flow to the allow-list |
| "token for provider X not found in context" | Missing `X-Authorization-X` header | Ensure the caller sends the header |
| "auth provider X not found in delegation registry" | No delegator configured for that name | Check `identity` config or `tokenPassThrough.allowedHeaders` |
| "env:// scheme requires a variable name" | Empty `SecretRef` | Set the env var name after `env://` |
| Token expires immediately | `expiryBuffer` too large | Reduce the buffer or check token TTL |

## Next Steps

- [API Server Tutorial](api-server-tutorial.md) -- Full API server setup
- [Authentication Tutorial](auth-tutorial.md) -- CLI-mode auth handlers
- [HTTP Provider Tutorial](http-provider-tutorial.md) -- HTTP provider reference
- [Configuration Tutorial](config-tutorial.md) -- Config file management
