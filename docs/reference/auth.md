# Authentication Reference (Proposal)

> This document captures the planned authentication model for scafctl. The implementation and generated CLI docs will follow once the engine supports pluggable auth providers, secure storage, and CLI export.

## Goals

1. **Pluggable providers** – Auth flows are provided by modules (built-in or third-party) with a common interface.
2. **Reusable credentials** – Once logged in, scafctl can reuse and refresh tokens without prompting the user.
3. **Secure storage** – Tokens are persisted in the OS keychain when available, falling back to encrypted files.
4. **Resolver/Action integration** – Workflows access auth data through the resolver context and provider inputs.
5. **Profile-aware** – Multiple environments (dev/prod/etc.) coexist via named profiles in the config file.

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
}
```

Key points:

- `ProviderInfo` lists supported flows (device code, workload identity federation, client credentials), required options, and environment requirements.
- `LoginOptions` carries CLI flags (`tenant`, `clientId`, etc.) resolved through the config profile.
- `AuthSession` adheres to the schema defined in [Auth Session Schema](../schemas/auth-session.md).

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

## Security Considerations

- **Token minimization**: Only store refresh tokens; derive access tokens on demand.
- **Encryption**: Always prefer OS keychain. If not possible, use AES-GCM with key scoped to current OS user and mark files with restrictive permissions.
- **Audit / logging**: Never print raw tokens. Mask them in debug logs.
- **Revocation**: Provide `auth logout` to call provider revocation endpoints and delete sessions.
- **Rotation**: CLI should expose `auth rotate` (future) to force refresh without logout/login.

## Extending with Additional Providers

1. Implement the provider interface in Go and compile as a plugin (or register statically).
2. Ship a manifest declaring provider name, version, required config options, scopes supported, and CLI flag bindings.
3. Users install the provider (`scafctl provider install auth.mycompany`) and run `scafctl auth login mycompany`.

## Next Steps

- Finalize provider interface in engine code.
- Implement token broker + session store abstraction.
- Prototype Entra provider using device code + workload identity federation.
- Integrate CLI commands and ensure help text matches the proposal.
- Add tests covering login/refresh/logout flows.
