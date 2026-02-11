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
| macOS    | `~/Library/Application Support/scafctl/config.yaml` |
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
