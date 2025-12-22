# Auth Session Schema (Proposal)

> Represents the persisted authentication state for a provider/profile pair. This schema is stored in the session store (keychain or encrypted file). All auth providers must emit sessions that conform to this structure.

```yaml
apiVersion: scafctl.io/v1
kind: AuthSession

metadata:
  provider: entra           # Provider identifier (matches CLI argument)
  profile: default          # Config profile associated with this session
  createdAt: 2025-12-20T17:32:00Z
  refreshedAt: 2025-12-20T17:45:10Z
  expiresAt: 2025-12-20T18:32:00Z
  scopes:                   # Scopes currently granted (optional)
    - https://graph.microsoft.com/.default
  flow: device-code         # Provider-specific flow used to acquire the session
  labels:                   # Arbitrary key/value metadata
    tenantId: example.onmicrosoft.com
    userPrincipalName: user@gmail.com

spec:
  credential:
    accessToken: eyJ...     # Optional; broker may omit for refresh-only storage
    refreshToken: eyJ...
    idToken: eyJ...         # Optional (OpenID Connect flows)
    tokenType: Bearer
    expiresAt: 2025-12-20T18:32:00Z
    refreshExpiresAt: 2026-01-01T00:00:00Z
    headers:                # Provider-supplied HTTP headers to attach to outbound requests
      Authorization: Bearer eyJ...
    audience: https://graph.microsoft.com

  options:                  # Provider-specific options used during login (persisted for refresh)
    tenant: example.onmicrosoft.com
    clientId: 12345678-aaaa-bbbb-cccc-999999999999
    resource: https://graph.microsoft.com

  storage:
    mechanism: keychain     # keychain | encrypted-file | custom
    location: windows-credential-manager
    keyPath: scafctl/sessions/default/entra
    encrypted: true
```

## Field Reference

| Field | Type | Description |
|-------|------|-------------|
| `apiVersion` | string | Must be `scafctl.io/v1` |
| `kind` | string | Always `AuthSession` |
| `metadata.provider` | string | Provider identifier (e.g., `entra`) |
| `metadata.profile` | string | Config profile name |
| `metadata.createdAt` | string (RFC3339) | When the session was first created |
| `metadata.refreshedAt` | string (RFC3339) | Last time tokens were refreshed |
| `metadata.expiresAt` | string (RFC3339) | When the current access token expires |
| `metadata.scopes` | array[string] | Scopes granted (optional) |
| `metadata.flow` | string | Provider-specific flow (`device-code`, `wif`, `client-credentials`, etc.) |
| `metadata.labels` | map[string]string | Additional metadata useful for display |
| `spec.credential.accessToken` | string | Current access token (optional) |
| `spec.credential.refreshToken` | string | Refresh token or equivalent secret |
| `spec.credential.idToken` | string | Optional OpenID Connect ID token |
| `spec.credential.tokenType` | string | e.g., `Bearer` |
| `spec.credential.expiresAt` | string | Access token expiry |
| `spec.credential.refreshExpiresAt` | string | Refresh token expiry (if provided) |
| `spec.credential.headers` | map[string]string | Provider-defined headers to attach to requests |
| `spec.credential.audience` | string | Audience/resource for the token |
| `spec.options` | map[string]string | Options used during initial login (tenant, clientId, etc.) |
| `spec.storage.mechanism` | string | `keychain`, `encrypted-file`, or custom identifier |
| `spec.storage.location` | string | Human-readable storage location |
| `spec.storage.keyPath` | string | Identifier/handle in the storage mechanism |
| `spec.storage.encrypted` | boolean | Whether the serialized session is encrypted |

## Best Practices

- **accessToken optional**: Providers may omit `accessToken` if storing only refresh tokens. The broker will request a new access token when needed.
- **headers vs token**: Some providers return multiple headers; store them in `headers` to avoid recomputing (e.g., AWS SigV4 derived headers).
- **rotation metadata**: Use `metadata.labels` for display-only information (tenant ID, user, etc.). Do not store secrets here.
- **encryption flag**: Indicates whether the serialized blob is encrypted before storage. When using OS keychain, this is usually `true` by default.

## Examples

### Device Code Flow (Entra)

```yaml
apiVersion: scafctl.io/v1
kind: AuthSession
metadata:
  provider: entra
  profile: default
  createdAt: 2025-12-20T17:32:00Z
  expiresAt: 2025-12-20T18:32:00Z
  flow: device-code
  labels:
    tenantId: example.onmicrosoft.com
    userPrincipalName: user@gmail.com
spec:
  credential:
    refreshToken: eyJ...
    tokenType: Bearer
    headers:
      Authorization: Bearer eyJ...  # access token derived on demand
  options:
    tenant: example.onmicrosoft.com
    clientId: 12345678-aaaa-bbbb-cccc-999999999999
  storage:
    mechanism: keychain
    location: windows-credential-manager
    keyPath: scafctl/sessions/default/entra
    encrypted: true
```

### Workload Identity Federation (WIF)

```yaml
apiVersion: scafctl.io/v1
kind: AuthSession
metadata:
  provider: entra
  profile: cicd
  createdAt: 2025-12-20T12:00:00Z
  flow: workload-identity-federation
  labels:
    workloadIdentityPool: scafctl-cicd
spec:
  credential:
    accessToken: eyJ...
    expiresAt: 2025-12-20T12:15:00Z
    refreshToken: ""              # Not used for WIF
    headers:
      Authorization: Bearer eyJ...
  options:
    tenant: example.onmicrosoft.com
    workloadIdentity: scafctl-cicd
  storage:
    mechanism: encrypted-file
    location: ~/.config/scafctl/sessions/cicd/entra.json.enc
    keyPath: cicd/entra
    encrypted: true
```
