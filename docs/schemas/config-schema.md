# CLI Config Schema (Proposal)

> Defines the structure of `config.yaml` used by scafctl. Configuration is stored per user, typically at `$XDG_CONFIG_HOME/scafctl/config.yaml` (Linux/macOS) or `%APPDATA%\scafctl\config.yaml` (Windows). Profiles allow multiple environments (dev, prod, etc.).

```yaml
apiVersion: scafctl.io/v1
kind: Config

defaultProfile: default

profiles:
  default:
    auth:
      provider: entra              # Default auth provider
      credential: default-session  # Name of saved session/credential
      options:                     # Provider-specific defaults (used at login)
        tenant: example.onmicrosoft.com
    catalog:
      endpoint: https://catalog.scafctl.dev
      token: ${SCAFCTL_CATALOG_TOKEN}
    output:
      format: json
      color: auto                  # auto | always | never
    resolver:
      retryPolicy:
        attempts: 3
        backoff: exponential
  prod:
    auth:
      provider: entra
      credential: prod-session
      options:
        tenant: example.onmicrosoft.com
    catalog:
      endpoint: https://catalog.scafctl.dev/prod

plugins:
  directories:
    - ~/.scafctl/plugins

telemetry:
  enabled: true
  prompt: true

updates:
  check: true
  channel: stable
```

## Field Reference

| Field | Type | Description |
|-------|------|-------------|
| `apiVersion` | string | Must be `scafctl.io/v1` |
| `kind` | string | Always `Config` |
| `defaultProfile` | string | Profile used when `--profile` not specified |
| `profiles` | map[string]Profile | Named configuration blocks |
| `profiles.*.auth.provider` | string | Default auth provider for this profile |
| `profiles.*.auth.credential` | string | Preferred credential/session name |
| `profiles.*.auth.options` | map[string]string | Provider-specific options (tenant, clientId, etc.) |
| `profiles.*.catalog.endpoint` | string | Catalog API endpoint |
| `profiles.*.catalog.token` | string | Token or token reference (env var) |
| `profiles.*.output.format` | string | Default output format (`text`, `json`, `yaml`) |
| `profiles.*.output.color` | string | Color control (`auto`, `always`, `never`) |
| `profiles.*.resolver.retryPolicy` | object | Default retry settings for network resolvers |
| `plugins.directories` | array[string] | Extra paths to search for plugins |
| `telemetry.enabled` | boolean | Opt-in/opt-out telemetry collection |
| `telemetry.prompt` | boolean | Whether to prompt user on first run |
| `updates.check` | boolean | Enable automatic update checks |
| `updates.channel` | string | Update channel (`stable`, `beta`, etc.) |

## Profiles

- Profiles act like namespaces for settings. CLI commands accept `--profile <name>`.
- Auth sessions are stored per profile (e.g., `sessions/default/entra`).
- Missing profile fields cascade to defaults from `defaultProfile`.

## Environment Variable Expansion

- Values wrapped as `${ENV_NAME}` are expanded at runtime.
- For secrets (tokens), prefer env vars so config file can be committed without secrets.

## Editing via CLI

`scafctl config` subcommands manipulate the structure above. Examples:

```bash
# Set catalog endpoint for default profile
scafctl config set catalog.endpoint https://catalog.example.com

# Set default output format for prod profile
scafctl config set output.format json --profile prod

# Show config location
scafctl config path
```

## Migration & Compatibility

- The schema is versioned via `apiVersion`. Future changes bump the version (`scafctl.io/v2`) and include migration commands.
- Older clients should refuse to load newer schemas unless `--force` is provided.

## Related Documents

- [CLI Config Command](../cli/commands/config.md)
- [Auth Session Schema](./auth-session.md)
- [Authentication Reference](../reference/auth.md)
