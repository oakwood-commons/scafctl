# scafctl auth (Proposal)

Manage authentication workflows for scafctl. Auth providers are pluggable, mirroring the design of resolver providers. Each provider encapsulates how to acquire, refresh, and revoke credentials (e.g., Microsoft Entra, GitHub, custom SSO).

> **Status:** Proposal. The authentication subsystem and CLI exporter are not implemented yet. This document captures the intended behavior so the future implementation and generated docs align.

## Usage

```
scafctl auth <command> [provider] [flags]
```

## Available Subcommands

- `login <provider>` — Acquire credentials using the specified auth provider
- `logout [provider]` — Revoke active credentials
- `status [provider]` — Show current auth state (token expiry, scopes)
- `list-providers` — List available auth providers and their capabilities
- `describe <provider>` — Show provider-specific options and required parameters

Providers can be built into scafctl or delivered as plugins, similar to resolver/action providers.

## Shared Flags

```
      --profile <name>     Select a configuration profile (defaults to "default")
      --quiet              Suppress informational output
      --debug              Emit debug logs
      --no-browser         Disable automatic browser launches (provider optional)
      --token-only         Print the resulting token to stdout (for scripting)
```

Individual auth providers may add extra flags (e.g., `--tenant`, `--client-id`).

## Authentication Object Model (Planned)

Auth providers return structured data recorded in the session store. A normalized auth object looks like:

```yaml
type: entra
provider: entra
profile: default
createdAt: 2025-12-20T17:32:00Z
expiresAt: 2025-12-20T18:32:00Z
scopes:
  - https://graph.microsoft.com/.default
credential:
  accessToken: eyJ...
  refreshToken: eyJ...
  tokenType: Bearer
  headers:
    Authorization: Bearer eyJ...
  metadata:
    tenantId: 12345
    user: user@gmail.com
```

- Stored under `%APPDATA%/scafctl/sessions/<profile>/<provider>.json` (Windows) or `$XDG_DATA_HOME/scafctl/sessions/...` elsewhere.
- Resolvers and actions access auth objects via the resolver context (e.g., `_.auth.entra.accessToken`).
- Providers handle refresh automatically when tokens near expiry.

## Examples (Planned Behavior)

### Login to Microsoft Entra

```
scafctl auth login entra --tenant example.onmicrosoft.com --client-id $CLIENT_ID
```

### Check auth status

```
scafctl auth status
```

### Show installed auth providers

```
scafctl auth list-providers
```

### Describe provider configuration options

```
scafctl auth describe entra
```

### Logout from current provider

```
scafctl auth logout entra
```

## Integrating Auth with Workflows

### 1. Resolvers

Resolvers can request auth credentials as part of the resolve phase:

```yaml
resolvers:
  entraToken:
    description: Access token for Graph API
    resolve:
      from:
        - provider: auth
          type: entra
          scope: https://graph.microsoft.com/.default
        - provider: static
          value: ""  # fallback when auth not available
```

### 2. Actions

Actions and providers consume the auth object via templating:

```yaml
actions:
  call-graph:
    provider: api
    inputs:
      endpoint: https://graph.microsoft.com/v1.0/users
      method: GET
      headers:
        Authorization: "Bearer {{ _.entraToken.accessToken }}"
```

### 3. Profiles & Config

Combine with `scafctl config` to set defaults:

```yaml
profiles:
  default:
    auth:
      provider: entra
      credential: default-session
    catalog:
      endpoint: https://catalog.scafctl.dev
```

CLI login writes session data for the active profile so subsequent runs reuse the credential.

## Roadmap Notes

- **Provider Marketplace:** Third parties can ship auth plugins (`scafctl auth login myprovider`).
- **Non-interactive Auth:** Support device code flow, client credential flow, and service principals.
- **Session Management:** Automatic refresh and background renewal for long-running commands.
- **Security:** Encrypt stored tokens using OS keychain APIs where available.
- **Generated Docs:** Once the CLI exporter exists, `scafctl auth --help --format markdown` will populate this file.

### Related Documentation

- [Authentication Reference](../../reference/auth.md)
- [Auth Session Schema](../../schemas/auth-session.md)
- [Config Schema](../../schemas/config-schema.md)
