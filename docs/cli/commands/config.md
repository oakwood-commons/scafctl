# scafctl config (Proposal)

Manage scafctl CLI configuration. This command family will let users view and modify settings that persist across runs (catalog endpoints, default output format, authentication references, etc.).

> **Status:** Proposal. The configuration subsystem and Markdown exporter are not yet implemented; this document captures the intended UX so engineering can align with documentation.

## Usage

```
scafctl config <command> [flags]
```

## Available Subcommands

- `set` — Write a configuration value
- `get` — Read a configuration value
- `list` — Show all configuration values (optionally filtered)
- `delete` — Remove a configuration value
- `path` — Print the config file path in use

Each subcommand can be extended as the config surface expands (profiles, credentials, catalog endpoints, etc.).

## Shared Flags

```
      --profile <name>     Use a specific profile (defaults to "default")
      --file <path>        Override the config file path (defaults to OS-standard location)
      --quiet              Suppress informational output
      --debug              Emit debug logs
```

## Configuration Model (Planned)

- Default location: `%APPDATA%/scafctl/config.yaml` on Windows, `$XDG_CONFIG_HOME/scafctl/config.yaml` elsewhere
- YAML structure grouped by feature area:

```yaml
profiles:
  default:
    catalog:
      endpoint: https://catalog.scafctl.dev
      token: ${SCAFCTL_TOKEN}
    output:
      format: json
    auth:
      provider: entra
      credential: default-session
```

- Profiles allow separate configurations for different environments (dev vs prod)
- Sensitive values can reference environment variables (as in the example)

## Examples (Planned Behavior)

### Set default output format

```
scafctl config set output.format json
```

### Configure catalog endpoint

```
scafctl config set catalog.endpoint https://catalog.example.com
```

### Use a named profile

```
scafctl config set catalog.token $CATALOG_TOKEN --profile prod
```

### Inspect full configuration

```
scafctl config list
```

### Show config file path

```
scafctl config path
```

## Notes / Roadmap

- Future additions may include `config import/export` for sharing profiles.
- `config` will integrate with forthcoming `auth` commands to store credential references.
- Documented here so the CLI generator can later output the real help text automatically.

### Related Documentation

- [CLI Auth Command](./auth.md)
- [CLI Config Schema](../../schemas/config-schema.md)
- [Authentication Reference](../../reference/auth.md)
