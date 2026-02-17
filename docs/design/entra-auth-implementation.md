---
title: Entra ID Authentication Implementation Decision
---

# Entra ID Authentication: Manual Implementation vs MSAL

## Status

**Decided** — Manual implementation retained.

## Context

The `pkg/auth/entra` package implements OAuth 2.0 authentication against Azure Entra ID (formerly Azure AD). A question arose whether to use the official [Microsoft Authentication Library for Go (MSAL)](https://github.com/AzureAD/microsoft-authentication-library-for-go) instead of the current hand-rolled implementation.

## Current Implementation

The package manually implements all OAuth 2.0 flows using raw `net/http` POST calls to Azure Entra ID token endpoints. There are zero Azure SDK or MSAL dependencies in the project.

### Flows Implemented

| Flow | Grant Type | File | Use Case |
|------|-----------|------|----------|
| Device Code | `urn:ietf:params:oauth:grant-type:device_code` | `device_flow.go` | Interactive CLI login |
| Client Credentials (Service Principal) | `client_credentials` | `service_principal.go` | Non-interactive, from `AZURE_CLIENT_*` env vars |
| Workload Identity | `client_credentials` with `client_assertion` (JWT bearer) | `workload_identity.go` | Kubernetes federated token exchange |
| Refresh Token | `refresh_token` | `token.go` | Silent token renewal with rotation |

### Flow Priority

Auto-detection order: **Workload Identity > Service Principal > Device Code**.

### Token Caching

Two layers of persistence, both backed by `secrets.Store` (OS keychain/credential store):

1. **Refresh Token + Metadata** — persistent refresh token and `TokenMetadata` (claims, expiry, tenant) for device code flow
2. **Access Token Cache** (`TokenCache`) — per-scope cached access tokens keyed by `scafctl.auth.entra.token.<base64url(scope)>`, used by all three flows via a generic `getCachedOrAcquireToken` helper

### Test Coverage

- ~1,500 lines of production code
- ~3,500 lines of tests (unit + integration + live)
- `MockHTTPClient` with queued responses and request recording
- `httptest.Server`-based integration tests simulating real Entra endpoints
- Live integration tests (build tag `integration`) against real Azure

## Pros of the Manual Approach

| Advantage | Detail |
|-----------|--------|
| **Zero external dependencies** | No Azure SDK in `go.mod`. MSAL brings transitive deps that increase module size and maintenance surface. |
| **Full control over caching** | Custom `TokenCache` integrates directly with `secrets.Store`. MSAL uses an in-memory cache with a serialization contract — an adapter to bridge it to `secrets.Store` would be needed regardless. |
| **Testability** | The `HTTPClient` interface + `MockHTTPClient` makes every flow fully testable without mocking MSAL internals. MSAL's test surface is harder to stub cleanly. |
| **Workload Identity** | MSAL Go doesn't natively support the `AZURE_FEDERATED_TOKEN_FILE` Kubernetes pattern. Custom code or `azidentity` (a heavier dependency) would still be required. |
| **Slim binary** | No compiled code from unused MSAL features. |

## Cons of the Manual Approach

| Disadvantage | Detail |
|--------------|--------|
| **Maintenance burden** | OAuth 2.0 protocol implementation must be maintained manually. Microsoft endpoint behavior changes or new error codes require manual updates. |
| **No PKCE / Auth Code flow** | MSAL supports interactive browser auth with PKCE out of the box. Adding this manually is significant work. |
| **No certificate-based auth** | MSAL supports client certificate credentials natively; currently missing. |
| **No instance discovery / sovereign clouds** | MSAL handles authority validation, instance discovery metadata, and sovereign cloud endpoints (Azure Government, Azure China). The manual code hardcodes `login.microsoftonline.com`. |
| **No Conditional Access / Claims Challenge** | MSAL handles the `claims` parameter for Conditional Access challenges automatically. |
| **Basic JWT parsing** | Manual `splitJWT` + base64url decode has no signature validation, no `nbf`/`aud` checks. MSAL validates tokens properly. |
| **Potential security gaps** | MSAL is reviewed by Microsoft's security team. A hand-rolled implementation may miss subtle requirements (token binding, nonce validation, etc.). |

## What Switching to MSAL Would Involve

Switching is not a clean drop-in replacement:

1. **Cache adapter required** — MSAL uses an in-memory cache with `ExportReplace`/`ExportAdd` serialization. An adapter to persist to `secrets.Store` would need to be written, which is roughly equivalent to what `cache.go` does today.
2. **Workload Identity still custom** — MSAL Go doesn't support `AZURE_FEDERATED_TOKEN_FILE`. That flow would remain manual, or require adding `azidentity` (which supports it but brings a much heavier dependency tree).
3. **Test strategy changes** — Mock strategy would shift from mocking HTTP calls to mocking MSAL client objects, which are harder to stub cleanly.
5. **Two dependency options**:
   - `microsoft-authentication-library-for-go` (MSAL) — lower-level, provides device code + client credentials
   - `azure-identity` (`azidentity`) — higher-level, wraps MSAL, adds `DefaultAzureCredential` chain and workload identity support, but significantly heavier

## Recommendation

**Retain the manual implementation.** The current approach is reasonable given the project's constraints:

- Only 3 well-defined flows are needed (device code, client credentials, workload identity)
- Custom caching requirements (`secrets.Store` integration) would require adapter code regardless
- The code is well-tested (~3,500 lines of tests including integration)
- The project values minimal dependencies

> **Note:** The multi-resource scope grouping logic has been removed, simplifying
> the device code flow. Login now targets a single set of scopes in one flow.
> Access tokens for additional resources can be minted at runtime using the
> refresh token. This slightly lowers the barrier to a future MSAL migration,
> since there is less custom protocol-level logic to preserve. However, the
> cache adapter and workload identity gaps remain the primary friction points.

### When to Reconsider

Switch to MSAL if any of the following become requirements:

- **Interactive browser auth with PKCE** — significant work to implement manually
- **Client certificate authentication** — non-trivial to implement correctly (key signing, x5c/x5t headers)
- **Sovereign cloud support** — instance discovery and authority validation across Azure Government, Azure China, etc.
- **Conditional Access / Claims Challenges** — protocol-level complexity that MSAL handles transparently

At that point, the maintenance cost of adding these flows manually would exceed the cost of integrating MSAL plus writing the required adapter code.

### Pragmatic Middle Ground

The `HTTPClient` interface and `auth.Handler` abstraction mean the internal implementation can be swapped without affecting consumers. If MSAL adoption becomes warranted, the change is isolated to `pkg/auth/entra` internals.
