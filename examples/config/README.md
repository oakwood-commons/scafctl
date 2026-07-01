# scafctl Example Configurations

This directory contains example configuration files for scafctl.

## Files

| File | Description |
|------|-------------|
| [minimal-config.yaml](minimal-config.yaml) | Minimal configuration to get started |
| [full-config.yaml](full-config.yaml) | Complete reference with all options documented |

## Configuration Locations

scafctl follows the [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/):

| Platform | Config Path |
|----------|-------------|
| Linux    | `~/.config/scafctl/config.yaml` |
| macOS    | `~/.config/scafctl/config.yaml` |
| Windows  | `%LOCALAPPDATA%\scafctl\config.yaml` |

## Usage

### Using `config init` (Recommended)

Generate a configuration file interactively:

```bash
# Create minimal config (recommended for new users)
scafctl config init

# Create full config with all options documented
scafctl config init --full

# Preview without creating file
scafctl config init --dry-run

# Write to custom location
scafctl config init --output ./my-config.yaml
```

### Manual Setup

Copy an example configuration:

## Configuration Sections

### `settings`
General application behavior: default catalog, colored output, quiet mode.

### `logging`
Log level (none/error/warn/info/debug/trace or numeric V-level), format (console/json), timestamps.

### `httpClient`
Global HTTP settings: timeouts, retries, caching, circuit breaker.

### `cel`
CEL expression engine: cache size, cost limits, metrics.

### `resolver`
Resolver execution: timeouts, concurrency, value size limits.

### `action`
Action execution: timeouts, grace period, concurrency.

### `catalogs`
List of registered catalogs (filesystem, http, oci).

### `auth.handlers`
Per-handler configuration namespaces keyed by handler name (e.g. `openshift`).
The reserved `hostname` block (aliases + dynamic resolver) is consumed by the
host to resolve `--hostname` selectors at login; every other key is forwarded
opaquely to the handler plugin. See
[Host-Aware Login](../auth/hostname-resolution.md) for the full walkthrough.

```yaml
auth:
  handlers:
    openshift:
      hostname:
        aliases:
          prod: https://api.prod.example.com:6443
      # Any other keys are passed through to the handler plugin unchanged.
      apiTimeout: 30s
```

## Drop-in Config Directory (`config.d`)

In addition to the main `config.yaml`, scafctl merges any `*.yaml`/`*.yml`
fragments placed in a `config.d/` directory next to it. This lets tooling,
provisioners, or teams layer configuration without editing the user's main file.

```text
~/.config/scafctl/
  config.yaml          # user config (highest file precedence)
  config.d/
    10-catalogs.yaml   # merged first
    20-telemetry.yaml  # merged after 10-*, overrides it
```

Precedence, lowest to highest:

1. Built-in defaults
2. Embedder base config (when scafctl is embedded in another CLI)
3. `config.d/*.yaml` fragments, in lexical filename order
4. `config.yaml` (the user's main file)
5. Environment variables (`SCAFCTL_*`)
6. Command-line flags

Maps are deep-merged across layers; arrays (such as `catalogs`) are replaced
wholesale by the highest-precedence layer that defines them. A missing
`config.d` directory is not an error; a malformed fragment fails the load with
the offending filename.

## Environment Variables

All config values can be overridden via environment variables:

```bash
# Use SCAFCTL_ prefix with underscores for nested keys
export SCAFCTL_SETTINGS_NOCOLOR=true
export SCAFCTL_HTTPCLIENT_TIMEOUT=60s

# Logging-specific env vars (override config and flags)
export SCAFCTL_LOG_LEVEL=debug      # Set log level
export SCAFCTL_LOG_FORMAT=json       # Set log format
export SCAFCTL_LOG_PATH=/tmp/scafctl.log  # Write logs to file
export SCAFCTL_DEBUG=1               # Shortcut: enable debug logging
```

## See Also

- `scafctl config view` - Show current configuration
- `scafctl config show` - Show effective config with sources
- `scafctl config validate` - Validate a config file
- `scafctl config schema` - Show JSON schema for config
