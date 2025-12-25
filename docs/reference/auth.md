# Authentication Architecture

## Overview
scafctl provides a pluggable authentication system supporting multiple identity providers (Azure/Entra, GitHub, AWS, etc.). Authentication credentials are securely stored and automatically refreshed, with decoded JWT claims exposed to resolvers and actions via the `_._.auth` context namespace for conditional logic and authorization checks.

## Goals
- Secure, OS-integrated credential storage (keychain/credential manager)
- Automatic token refresh with minimal user intervention
- Multi-provider support with extensible plugin architecture
- Claims-based authorization for resolvers and actions
- Zero-secret exposure in logs, outputs, or context (tokens discarded after decoding)
- Pluggable providers with common interface (built-in or third-party)
- Profile-aware for multiple environments (dev/prod/etc.)

## Authentication Flow

### Initial Login
1. **Command**: User runs `scafctl auth login <provider> [--profile <name>]`
2. **Provider Selection**: CLI loads provider config from `~/.scafctl/config.yaml` or flags
3. **Authentication**: Provider initiates OAuth/OIDC flow:
   - **Device Code Flow** (default for interactive): User visits URL, enters code, approves
   - **Client Credentials**: Service principal authenticates with client secret or certificate
   - **Workload Identity Federation (WIF)**: CI/CD environments exchange OIDC tokens
4. **Token Acquisition**: Provider exchanges code/credentials for access token, refresh token, and optionally ID token
5. **Session Storage**: Session is serialized to [AuthSession schema](../schemas/auth-session.md) and stored:
   - **Keychain** (preferred): Windows Credential Manager, macOS Keychain, Linux Secret Service
   - **Encrypted File** (fallback): `~/.scafctl/sessions/<profile>/<provider>.json.enc` with user-specific encryption key
6. **JWT Decoding**: ID token (or access token if JWT) is decoded; claims payload extracted for initial display
7. **Confirmation**: CLI displays logged-in user, tenant, and granted scopes

### Runtime Token Usage
1. **Execution Start**: `scafctl run` or other commands load active sessions from storage
2. **Token Validation**: Check expiration (`expiresAt`); if expired, trigger refresh
3. **Token Refresh**:
   - Use `refreshToken` to request new access token from provider
   - Update session with new `accessToken`, `expiresAt`, and `refreshedAt` timestamp
   - Re-decode JWT claims from new token
4. **Claims Extraction**: Decode JWT (ID token or access token if JWT-formatted):
   - Parse header and payload (base64url decode)
   - **Discard signature** and original token string
   - Store only claims payload in `_._.auth.<provider>.claims`
5. **Context Population**: Populate `_._.auth` with provider-keyed objects:
   ```yaml
   auth:
     azure:
       active: true
       claims: { aud, iss, sub, name, email, roles, exp, ... }
       headers: { Authorization: "Bearer <token-redacted>" }
     github:
       active: false
   ```
6. **Resolver/Action Access**: Expressions reference `_._.auth.azure.claims.roles` for conditional logic

### Token Lifecycle
- **Access Token Lifetime**: Typically 1 hour (provider-dependent)
- **Refresh Token Lifetime**: Days to months (provider-dependent); some never expire
- **Automatic Refresh**: scafctl refreshes access tokens transparently when expired
- **Session Expiry**: If refresh token expires or is revoked, user must re-authenticate (`auth login`)

### Logout
1. **Command**: `scafctl auth logout <provider> [--profile <name>]`
2. **Token Revocation** (optional): Attempt to revoke refresh token via provider API (best-effort)
3. **Session Deletion**: Remove session from keychain/encrypted file
4. **Confirmation**: Display logout success and affected profile/provider

## Architecture Overview

```
┌─────────────────────┐
│  CLI Command (auth) │
└─────────┬───────────┘
          │ calls
┌─────────▼───────────┐
│   Auth Controller   │  ← handles CLI UX, profile selection
└─────────┬───────────┘
          │ uses
┌─────────▼───────────┐
│   Token Broker API  │  ← normalize auth objects, refresh tokens
└─────────┬───────────┘
          │ loads
┌─────────▼───────────┐
│ Auth Provider (plug)│  ← device code flow, WIF, service principals...
└─────────┬───────────┘
          │ persists
┌─────────▼───────────┐
│ Session Store       │  ← OS keychain (preferred) or encrypted file
└─────────┬───────────┘
          │ exposes
┌─────────▼───────────┐
│ Resolver Context    │  ← _.auth.<provider>.accessToken, headers, etc.
└─────────────────────┘
```

## Provider Contract (Draft)

Auth providers must implement the following interface (language-agnostic pseudocode):

```go
type AuthProvider interface {
    // Metadata describing provider name, version, supported flows, scopes
    Describe(ctx Context) (ProviderInfo, error)

    // Interactive or headless login
    Login(ctx Context, opts LoginOptions) (AuthSession, error)

    // Revoke existing session (optional)
    Logout(ctx Context, session AuthSession) error

    // Refresh access token; called by token broker automatically
    Refresh(ctx Context, session AuthSession) (AuthSession, error)
    
    // Extract claims from JWT or fetch from API
    GetClaims(ctx Context, session AuthSession) (map[string]any, error)
}
```

Key points:

- `ProviderInfo` lists supported flows (device code, workload identity federation, client credentials), required options, and environment requirements.
- `LoginOptions` carries CLI flags (`tenant`, `clientId`, etc.) resolved through the config profile.
- `AuthSession` adheres to the schema defined in [Auth Session Schema](../schemas/auth-session.md).
- `GetClaims` returns decoded JWT claims or fetches user info from provider API (for non-JWT tokens like GitHub)

## Supported Providers

### Azure/Entra ID
- **Flows**: Device Code (interactive), Client Credentials (service principal), Workload Identity Federation (CI/CD)
- **Token Format**: JWT access token and ID token
- **Claims**: `aud`, `iss`, `sub`, `name`, `email`, `roles`, `groups`, `tenant_id`, `exp`, etc.
- **Refresh**: Refresh token valid for 90 days (configurable by tenant admin)
- **Scopes**: `https://graph.microsoft.com/.default`, custom app scopes

### GitHub
- **Flows**: OAuth Device Flow (interactive), Personal Access Token (manual), GitHub App (CI/CD)
- **Token Format**: Opaque access token (not JWT)
- **Claims**: Fetched from `/user` API after token acquisition (username, email, orgs, etc.)
- **Refresh**: OAuth tokens can be refreshed; PATs do not expire unless revoked
- **Scopes**: `repo`, `user`, `workflow`, etc.

### AWS (Future)
- **Flows**: OIDC Federation, STS Assume Role
- **Token Format**: Temporary credentials (access key ID, secret, session token)
- **Claims**: IAM role ARN, session name, assumed role user

### Custom Providers
- Providers implement `AuthProvider` interface
- Must return [AuthSession schema](../schemas/auth-session.md)
- Registered via `auth.RegisterProvider("custom", &CustomProvider{})`

## Token Broker Responsibilities

The token broker coordinates providers and session storage:

1. **Load session** for the requested provider/profile from the session store.
2. **Validate expiry**; if access token expires soon, call `Refresh`.
3. **Return normalized credential** (access token, headers, metadata) to callers.
4. **Persist** updates after refresh, including new expiry timestamps.
5. **Surface errors** (e.g., revoked refresh token) so CLI can prompt the user to login again.

All scafctl components request auth via the broker:

```go
token := broker.GetToken("entra", profile="prod", scope="https://graph.microsoft.com/.default")
```

The broker determines how to satisfy the scope (may call provider with additional parameters).

## Session Storage

Preferred storage order:

1. **OS keychain**: Windows Credential Manager, macOS Keychain, Linux Secret Service. Sessions stored as JSON blobs encrypted by the OS.
2. **Encrypted file**: When keychain unavailable, encrypt using a user-derived key (e.g., OS user SID) and store in `$XDG_DATA_HOME/scafctl/sessions/`.

See [Auth Session Schema](../schemas/auth-session.md) for the on-disk format (before encryption/keychain wrapping).

## Security Considerations

### Credential Storage
- **Never store tokens in plaintext**: Use OS keychain or encrypted files with user-specific keys
- **Access Control**: Keychain entries restricted to current user; encrypted files have 0600 permissions
- **Encryption Key**: Derived from user identity (e.g., user SID on Windows, UID on Linux) or machine-specific key

### Token Handling
- **No Token Exposure**: Original token strings and signatures are discarded after decoding claims
- **Redacted Logging**: Auth headers logged as `"Bearer <token-redacted>"` in debug mode
- **Memory Scrubbing**: Tokens zeroed from memory after use (Go `crypto/subtle` or explicit zeroing)
- **Context Security**: `_._.auth` treated as sensitive; avoid logging or exporting in reports

### Claims Validation
- **JWT Signature**: Validated during initial decode using provider's JWKS (JSON Web Key Set)
- **Expiration**: Claims with `exp` field checked; expired tokens trigger refresh
- **Audience**: Verified against expected `aud` values for the provider
- **Issuer**: Verified against trusted `iss` values (e.g., `https://login.microsoftonline.com/<tenant>/v2.0`)

### Threat Mitigation
- **Token Theft**: Mitigated by short access token lifetimes and OS-protected storage
- **Replay Attacks**: Audience and issuer validation prevent cross-resource token reuse
- **MITM**: All provider communication over HTTPS with certificate validation
- **Session Hijacking**: Sessions bound to user identity via encryption keys

## CLI Command Surface

### `scafctl auth login <provider>`
Authenticate with a provider and store session.

**Flags**:
- `--profile <name>`: Config profile to associate with session (default: `default`)
- `--tenant <id>`: Azure tenant ID (Entra only)
- `--client-id <id>`: OAuth client ID override
- `--scopes <scope1,scope2>`: Comma-separated scopes to request
- `--flow <type>`: Force specific flow (`device-code`, `client-credentials`, `wif`)

**Example**:
```bash
scafctl auth login azure --tenant example.onmicrosoft.com --scopes "https://graph.microsoft.com/.default"
```

### `scafctl auth logout <provider>`
Revoke and remove stored session.

**Flags**:
- `--profile <name>`: Profile to log out (default: `default`)
- `--all`: Log out of all profiles for the provider

**Example**:
```bash
scafctl auth logout azure --profile production
```

### `scafctl auth status`
Display active sessions and their status.

**Output**:
```
Profile   Provider   User                      Status    Expires
default   azure      user@example.com          Active    2025-12-25 18:32:00
cicd      azure      scafctl-sp@example.com    Active    Never (refresh)
default   github     octocat                   Expired   2025-12-24 12:00:00
```

**Flags**:
- `--profile <name>`: Filter by profile
- `--provider <name>`: Filter by provider
- `--json`: Output as JSON for scripting

### `scafctl auth refresh <provider>`
Manually refresh access token (usually automatic).

**Flags**:
- `--profile <name>`: Profile to refresh (default: `default`)

### `scafctl auth configure <provider>`
Interactively configure provider settings in `~/.scafctl/config.yaml`.

**Prompts**:
- Client ID, tenant, scopes, default flow, etc.

### `scafctl auth list-providers`
List installed auth providers and their capabilities.

### `scafctl auth describe <provider>`
View provider-specific options, supported flows, and configuration requirements.

## Config Integration

Profiles in the config file specify default auth provider and provider-specific options:

```yaml
profiles:
  default:
    auth:
      provider: entra
      credential: default-session
      options:
        tenant: example.onmicrosoft.com
        clientId: ${SCAFCTL_ENTRA_CLIENT_ID}
```

The `scafctl config` command manages this structure. Auth commands respect `--profile` to select the active profile.

## Integration with Resolvers/Actions

### Accessing Claims in CEL
```yaml
# Resolver example: conditionally fetch data based on user role
resolvers:
  - id: adminConfig
    type: http
    condition: |
      has(_._.auth.azure) && _._.auth.azure.active &&
      "Solution.Admin" in _._.auth.azure.claims.roles
    config:
      url: https://api.example.com/admin/config
```

### Checking Provider Availability
```yaml
# Action example: skip GitHub publish if not authenticated
actions:
  - id: publishToGitHub
    type: git-push
    condition: has(_._.auth.github) && _._.auth.github.active
    config:
      remote: origin
      branch: main
```

### Using Claims for Dynamic Values
```yaml
# Template example: embed user email in generated file
templates:
  - id: configFile
    content: |
      # Generated for {{ _._.auth.azure.claims.email }}
      owner: {{ _._.auth.azure.claims.name }}
```

### Resolver Fetching Token (Alternative Pattern)

```yaml
spec:
  resolvers:
    graphAuth:
      description: Microsoft Graph access token
      resolve:
        from:
          - provider: auth
            type: entra
            scope: https://graph.microsoft.com/.default
```

The `auth` resolver provider calls the token broker. The returned session is projected to `_.graphAuth`.

## CLI Command Surface (Proposed)

See [CLI Auth Command](../cli/commands/auth.md). Summary:

- `auth login <provider>` – interactive or headless login
- `auth status [provider]` – show saved sessions and expiration
- `auth logout [provider]` – revoke credentials and clear session
- `auth list-providers` – inspect installed providers
- `auth describe <provider>` – view provider-specific options and flows

## Usage in Workflows

### Resolver Fetching Token

```yaml
spec:
  resolvers:
    graphAuth:
      description: Microsoft Graph access token
      resolve:
        from:
          - provider: auth
            type: entra
            scope: https://graph.microsoft.com/.default
```

The `auth` resolver provider calls the token broker. The returned session is projected to `_.graphAuth`.

### Action Using Token

```yaml
spec:
  actions:
    notify-graph:
      provider: api
      when: _.graphAuth.accessToken != ""
      inputs:
        endpoint: https://graph.microsoft.com/v1.0/users
        method: GET
        headers:
          Authorization: "Bearer {{ _.graphAuth.accessToken }}"
```

The API provider can also inspect `_.graphAuth.headers` to reuse provider-supplied headers.

## Error Handling

### Authentication Failures
- **No Active Session**: CLI prompts user to run `scafctl auth login <provider>`
- **Expired Refresh Token**: Error message with instructions to re-authenticate
- **Token Refresh Failed**: Retry with exponential backoff; if persistent, require re-login
- **Invalid Claims**: Log warning; expose empty claims object; resolvers/actions check `has()` before accessing

### Provider Errors
- **Network Timeout**: Retry with backoff; fail after 3 attempts
- **Invalid Client**: Configuration error; display helpful message with config file path
- **Insufficient Scopes**: Display required vs. granted scopes; prompt re-login with correct scopes

## Extensibility

### Custom Provider Plugin
Providers implement `AuthProvider` interface (see Provider Contract section above).

Plugins registered via:
```go
auth.RegisterProvider("custom", &CustomProvider{})
```

### Provider Discovery
- Built-in providers in `pkg/auth/providers/`
- External plugins via `~/.scafctl/plugins/auth-<provider>.so` (Go plugin)
- Configuration in `config.yaml` under `auth.providers.<name>`

## Future Enhancements
- **Multi-Factor Authentication**: Support for FIDO2/WebAuthn flows
- **Credential Rotation**: Automatic rotation of service principal secrets (`auth rotate`)
- **Audit Logging**: Log all auth events (login, refresh, logout) to structured log
- **Session Sharing**: Share sessions across multiple scafctl processes safely
- **Biometric Unlock**: Use OS biometrics to unlock encrypted session storage

## References
- Schema: [docs/schemas/auth-session.md](../schemas/auth-session.md)
- Design: [docs/design/resolvers.md](../design/resolvers.md) (Internal System Namespace section)
- CLI Commands: [docs/cli/commands/auth.md](../cli/commands/auth.md)
- Providers: [docs/design/providers.md](../design/providers.md)
- Add tests covering login/refresh/logout flows.
