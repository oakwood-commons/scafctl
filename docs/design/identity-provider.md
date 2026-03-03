---
title: "Identity Provider"
weight: 22
---

# Identity Provider

The **identity** provider exposes authentication identity information from auth handlers. It returns non-sensitive identity data such as claims, authentication status, and identity type. It **never exposes tokens or other secrets**.

## Operations

| Operation | Description | Scope Support |
|-----------|-------------|---------------|
| `claims` | Returns identity claims (name, email, subject, etc.) | Yes — parse scoped access token JWT |
| `status` | Returns authentication status, expiry, identity type | Yes — return scoped token metadata |
| `groups` | Returns Entra group memberships (ObjectIDs) | No |
| `list` | Lists all registered auth handlers | No |

## Inputs

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `operation` | string | Yes | Operation to perform: `status`, `claims`, `groups`, `list` |
| `handler` | string | No | Auth handler name (e.g., `entra`). Defaults to the first authenticated handler. |
| `scope` | string | No | OAuth scope for scoped token operations. When set, `claims` and `status` operations mint a token with this scope and return its details instead of stored metadata. |

## Default Behavior (without scope)

When no `scope` is provided, the identity provider reads **stored metadata** from the auth handler's local session. This metadata was extracted from the OIDC ID token JWT at login time:

- **`claims`** — returns claims (email, name, subject, tenantId, etc.) from the stored ID token
- **`status`** — returns session status (authenticated, expiresAt, identityType) from stored metadata

No network calls or token minting occurs.

## Scoped Token Behavior

When `scope` is provided, the identity provider calls `GetToken(scope)` on the auth handler to mint (or retrieve from cache) an **OAuth 2.0 access token** for the given scope. It then parses the access token JWT to extract claims and identity details.

This is useful when you need identity information tied to a specific API audience/resource rather than the login session.

### How it works

1. The auth handler mints an access token for the requested scope (using a stored refresh token or other credential)
2. Token caching is scope-aware — each scope gets its own cache slot, so repeated calls are efficient
3. The access token JWT payload is decoded (base64url, no signature verification) to extract claims
4. Claims and token metadata are returned in the same format as the default behavior
5. The access token value itself is **never** included in the output

### Scoped output fields

When `scope` is provided, the output includes additional fields:

| Field | Description |
|-------|-------------|
| `scopedToken` | `true` — indicates the response came from a scoped access token |
| `tokenScope` | The OAuth scope that was requested |
| `tokenType` | Token type (typically `Bearer`) — status operation only |
| `flow` | Authentication flow that produced the token (e.g., `device_code`) — status operation only |
| `sessionId` | Stable session identifier — status operation only |

### Opaque tokens

Some access tokens are not decodable JWTs (e.g., encrypted tokens for first-party Microsoft resources like Graph). When this happens:

- Claims are returned as `null`
- A warning is included explaining the token is opaque
- Token metadata (expiry, scope) is still returned where available from the `Token` struct
- The provider does **not** fail — it degrades gracefully

### Scope restrictions

- `scope` is only supported with `claims` and `status` operations
- Passing `scope` with `groups` or `list` returns an error explaining the restriction

## Examples

### Get claims from stored metadata

```yaml
name: get-claims
provider: identity
inputs:
  operation: claims
```

### Get claims from a scoped access token

```yaml
name: scoped-claims
provider: identity
inputs:
  operation: claims
  scope: api://my-app/.default
```

### Check scoped token status

```yaml
name: scoped-status
provider: identity
inputs:
  operation: status
  scope: https://management.azure.com/.default
  handler: entra
```

### Check authentication status

```yaml
name: check-auth
provider: identity
inputs:
  operation: status
  handler: entra
```

### Get Entra group memberships

```yaml
name: user-groups
provider: identity
inputs:
  operation: groups
  handler: entra
```

### List available handlers

```yaml
name: list-handlers
provider: identity
inputs:
  operation: list
```

## Architecture Notes

- **JWT parsing** is handled by the shared `auth.ParseJWTClaims()` function in `pkg/auth/jwt.go`, which supports both ID tokens and access tokens with claim name fallbacks for Entra v1/v2 differences
- **Token caching** is scope-aware in the auth layer — each `(flow, fingerprint, scope)` tuple gets a separate cache entry
- **No secrets exposed** — the identity provider never returns the access token value, maintaining the security contract
- **Identity type inference** — when parsing a scoped access token JWT, the provider infers whether the identity is a `user` or `service-principal` based on the presence of human-readable claims (name, email, username)
