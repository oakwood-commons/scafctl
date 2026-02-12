---
title: "Configuration Management"
weight: 90
---

# Configuration Management Tutorial

This tutorial covers managing scafctl's application configuration using the `config` CLI commands.

## Overview

scafctl uses a YAML configuration file to control application behavior including logging, HTTP client settings, resolver timeouts, catalog locations, and more.

```
┌──────────────────┐
│  config init     │ ── Create config file
│  config view     │ ── View current config
│  config get/set  │ ── Read/write values
│  config validate │ ── Validate config
│  config schema   │ ── View JSON schema
│  config paths    │ ── Show XDG paths
└──────────────────┘
```

## Quick Start

### 1. Initialize Configuration

Create a new config file:

```bash
# Create minimal config
scafctl config init

# Create with all options documented
scafctl config init --full

# Preview without writing
scafctl config init --dry-run

# Write to custom path
scafctl config init --output ./my-config.yaml
```

### 2. View Configuration

See the current effective configuration:

```bash
# Full config (YAML format)
scafctl config view

# As JSON
scafctl config view -o json

# Filter with CEL expression
scafctl config view -e '_.settings'

# Interactive TUI explorer
scafctl config view -i
```

### 3. Show Config Sources

See where each value comes from (file, environment, default):

```bash
scafctl config show
```

## Getting and Setting Values

### Read Values

Use dot notation to read specific config values:

```bash
# Get logging level
scafctl config get logging.level

# Get default catalog
scafctl config get settings.defaultCatalog

# Get HTTP timeout
scafctl config get httpClient.timeout
```

### Write Values

```bash
# Set logging level
scafctl config set logging.level debug

# Set HTTP timeout
scafctl config set httpClient.timeout 60s

# Set resolver concurrency
scafctl config set resolver.concurrency 8

# Enable a boolean
scafctl config set httpClient.caching.enabled true
```

### Reset to Default

```bash
# Reset a value to its default
scafctl config unset logging.level

# Reset HTTP configuration
scafctl config unset httpClient.timeout
```

## Managing Catalogs

### Add a Catalog

```bash
# Add a local filesystem catalog
scafctl config add-catalog my-catalog --type filesystem --path ~/my-solutions

# Add an OCI registry catalog
scafctl config add-catalog company --type oci --url ghcr.io/myorg/scafctl-catalog

# Add and set as default
scafctl config add-catalog primary --type filesystem --path ~/catalogs/main --default
```

### Remove a Catalog

```bash
scafctl config remove-catalog my-catalog
```

### Set Default Catalog

```bash
scafctl config use-catalog my-catalog
```

## Validation

### Validate Config File

```bash
# Validate current config
scafctl config validate

# Validate a specific file
scafctl config validate path/to/config.yaml
```

### View Config Schema

```bash
# Pretty-printed JSON Schema
scafctl config schema

# Minified (for piping)
scafctl config schema --compact
```

## View System Paths

See where scafctl stores files on your system:

```bash
# Show all XDG paths
scafctl config paths

# As JSON
scafctl config paths -o json

# Show paths for a different platform
scafctl config paths --platform linux
```

Typical output:

```
XDG Paths (darwin/arm64)

Config:   ~/.config/scafctl/config.yaml
Data:     ~/.local/share/scafctl/
Cache:    ~/.cache/scafctl/
State:    ~/.local/state/scafctl/
```

## Configuration Reference

### Key Settings

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `logging.level` | string | `none` | Log level: `none`, `error`, `warn`, `info`, `debug`, `trace`, or numeric V-level |
| `logging.format` | string | `console` | Log format: `console` (colored), `json` (structured) |
| `settings.defaultCatalog` | string | `local` | Default catalog name |
| `httpClient.timeout` | duration | `30s` | HTTP request timeout |
| `httpClient.retry.maxRetries` | int | `3` | Max HTTP retries |
| `httpClient.caching.enabled` | bool | `true` | Enable HTTP response caching |
| `resolver.timeout` | duration | `5m` | Overall resolver timeout |
| `resolver.concurrency` | int | `4` | Max parallel resolver execution |
| `action.timeout` | duration | `10m` | Overall action timeout |
| `action.concurrency` | int | `4` | Max parallel action execution |

### Config File Location

The config file is located at the XDG config path:

| Platform | Default Location |
|----------|------------------|
| macOS | `~/.config/scafctl/config.yaml` |
| Linux | `~/.config/scafctl/config.yaml` |
| Windows | `%APPDATA%\scafctl\config.yaml` |

Override with the `--config` flag on any command:

```bash
scafctl run solution my-solution --config ./custom-config.yaml
```

## Examples

### Minimal Config

See [examples/config/minimal-config.yaml](../../examples/config/minimal-config.yaml):

```yaml
version: 1
settings:
  defaultCatalog: "local"
logging:
  level: "none"
catalogs:
  - name: local
    type: filesystem
    path: ~/scafctl-catalog/
```

### Full Config

See [examples/config/full-config.yaml](../../examples/config/full-config.yaml) for a complete reference with all options documented.

## Common Workflows

### Initial Setup

```bash
# 1. Initialize config
scafctl config init

# 2. Set up a catalog
scafctl config add-catalog local --type filesystem --path ~/scafctl-catalog --default

# 3. Verify
scafctl config view
scafctl config validate
```

### Switch Environments

```bash
# Use staging catalog
scafctl config use-catalog staging

# Increase logging for debugging
scafctl config set logging.level debug

# After debugging, reset
scafctl config unset logging.level
```

## Next Steps

- [Logging & Debugging Tutorial](logging-tutorial.md) — Control log verbosity, format, and output destination
- [Catalog Tutorial](catalog-tutorial.md) — Build and manage solutions in the catalog
- [Cache Tutorial](cache-tutorial.md) — Manage cached data
- [Getting Started](getting-started.md) — Run your first solution
