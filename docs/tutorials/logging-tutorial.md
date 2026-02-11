---
title: "Logging & Debugging"
weight: 95
---

# Logging & Debugging Tutorial

This tutorial covers controlling scafctl's log output — verbosity, format, destination, and environment variable overrides.

## Overview

By default, scafctl produces **no structured log output**. Only styled user messages (errors, warnings, success indicators) are shown. This keeps the CLI clean for human users and pipe-friendly for scripts.

When you need to see what's happening under the hood — debugging a solution, reporting a bug, or feeding structured logs to a log aggregation system — scafctl gives you full control over log verbosity, format, and destination.

```
┌──────────────────────────────────────────────────┐
│  Default:     No logs, just styled output        │
│  --debug:     Console-format debug logs          │
│  --log-level: Named or numeric verbosity         │
│  --log-format: console (default) or json         │
│  --log-file:  Write logs to a file               │
│  Env vars:    Override from CI/CD or scripts     │
└──────────────────────────────────────────────────┘
```

## Quick Start

### Default Behavior

Run any command — you'll see only styled user-facing messages:

```bash
scafctl run solution -f solution.yaml
# Output: styled results, errors show as ❌ messages

scafctl run solution -f invalid.yaml
# Output: ❌ failed to load solution from 'invalid.yaml': ...
# No JSON log noise on stderr
```

### Enable Debug Logging

The quickest way to see what scafctl is doing:

```bash
scafctl run solution -f solution.yaml --debug

# or equivalently:
scafctl run solution -f solution.yaml --log-level debug
```

This shows colored, human-readable logs on stderr alongside normal output:

```
2026-01-15T10:30:00.000-0500    DEBUG   run/solution.go:326     running solution {"file": "solution.yaml", ...}
2026-01-15T10:30:00.001-0500    DEBUG   get/get.go:347  Reading solution from local filesystem  {"path": "solution.yaml"}
```

## Log Levels

scafctl supports both **named levels** (recommended) and **numeric V-levels** for fine-grained control.

### Named Levels

| Level | Description | Use Case |
|-------|-------------|----------|
| `none` | Suppress all structured log output | Default; normal CLI usage |
| `error` | Errors only | Production monitoring |
| `warn` | Warnings and errors | Catch potential issues |
| `info` | Informational messages | See what's happening |
| `debug` | Verbose debugging | Troubleshooting solutions |
| `trace` | Very verbose | Deep debugging |

```bash
# Show only errors
scafctl run solution -f solution.yaml --log-level error

# Show info and above
scafctl run solution -f solution.yaml --log-level info

# Maximum verbosity
scafctl run solution -f solution.yaml --log-level trace
```

### Numeric V-Levels

For fine-grained control matching the internal `V()` levels used in the code:

| V-Level | Equivalent Named Level | What It Shows |
|---------|----------------------|---------------|
| `1` | `debug` | Provider execution, resolver lifecycle |
| `2` | `trace` | Internal data flow, template rendering |
| `3` | _(no alias)_ | Ultra-verbose internals |

```bash
# Same as --log-level debug
scafctl run solution -f solution.yaml --log-level 1

# Same as --log-level trace
scafctl run solution -f solution.yaml --log-level 2

# Ultra-verbose (no named alias)
scafctl run solution -f solution.yaml --log-level 3
```

## Log Formats

### Console Format (Default)

Human-readable, colored output. Best for terminal use:

```bash
scafctl run solution -f solution.yaml --debug
# or explicitly:
scafctl run solution -f solution.yaml --log-level debug --log-format console
```

Output:

```
2026-01-15T10:30:00.000-0500    DEBUG   run/solution.go:326     running solution {"file": "solution.yaml", ...}
2026-01-15T10:30:00.001-0500    ERROR   get/get.go:360  Failed to unmarshal     {"error": "..."}
```

### JSON Format

Structured JSON, ideal for log aggregation (Splunk, Datadog, ELK), piping to `jq`, or machine parsing:

```bash
scafctl run solution -f solution.yaml --log-level info --log-format json
```

Output:

```json
{"level":"info","timestamp":"2026-01-15T10:30:00.000-0500","caller":"run/solution.go:326","message":"running solution","file":"solution.yaml"}
{"level":"error","timestamp":"2026-01-15T10:30:00.001-0500","caller":"get/get.go:360","message":"Failed to unmarshal","error":"..."}
```

Filter with `jq`:

```bash
scafctl run solution -f solution.yaml --log-level debug --log-format json 2>&1 | jq 'select(.level == "error")'
```

## Log File Output

Write logs to a file instead of (or in addition to) stderr:

```bash
# Logs go to file only; stderr shows just styled output
scafctl run solution -f solution.yaml --log-level debug --log-file /tmp/scafctl.log

# Logs go to BOTH file and stderr (combine with --debug)
scafctl run solution -f solution.yaml --debug --log-file /tmp/scafctl.log
```

When `--debug` and `--log-file` are used together, logs are written to both destinations. Without `--debug`, the file receives the logs and stderr stays clean.

View the log file:

```bash
tail -f /tmp/scafctl.log
```

## Environment Variables

Environment variables are useful for CI/CD pipelines, container environments, and scripts where you can't pass flags.

| Variable | Description | Example |
|----------|-------------|---------|
| `SCAFCTL_DEBUG` | Enable debug logging (set to `1`, `true`, or any non-empty non-`0` value) | `SCAFCTL_DEBUG=1` |
| `SCAFCTL_LOG_LEVEL` | Set log level | `SCAFCTL_LOG_LEVEL=info` |
| `SCAFCTL_LOG_FORMAT` | Set log format | `SCAFCTL_LOG_FORMAT=json` |
| `SCAFCTL_LOG_PATH` | Write logs to a file | `SCAFCTL_LOG_PATH=/var/log/scafctl.log` |

### Examples

```bash
# CI/CD: structured JSON logs for log aggregation
export SCAFCTL_LOG_LEVEL=info
export SCAFCTL_LOG_FORMAT=json
scafctl run solution -f solution.yaml

# Container: debug to a file
export SCAFCTL_DEBUG=1
export SCAFCTL_LOG_PATH=/var/log/scafctl.log
scafctl run solution -f solution.yaml

# Quick debug in shell
SCAFCTL_DEBUG=1 scafctl run solution -f solution.yaml
```

## Precedence

When multiple sources set the log level, scafctl applies them in this order (highest priority first):

```
1. --log-level flag          (explicit flag always wins)
2. --debug / -d flag         (shorthand for --log-level debug)
3. SCAFCTL_LOG_LEVEL env     (environment variable)
4. SCAFCTL_DEBUG env         (debug shortcut)
5. config file logging.level (persistent preference)
6. default: "none"           (no logs)
```

The same pattern applies to format and file path:
- `--log-format` flag > `SCAFCTL_LOG_FORMAT` env > config `logging.format` > default `"console"`
- `--log-file` flag > `SCAFCTL_LOG_PATH` env > default (no file)

## Configuration File

You can set logging defaults in your config file so you don't need flags every time:

```yaml
# ~/.config/scafctl/config.yaml (Linux)
# ~/Library/Application Support/scafctl/config.yaml (macOS)
logging:
  level: "info"       # Always show info-level logs
  format: "console"   # Human-readable (default)
  timestamps: true    # Include timestamps (default)
```

Manage via CLI:

```bash
# Set persistent log level
scafctl config set logging.level info

# Set persistent format
scafctl config set logging.format json

# Reset to defaults
scafctl config unset logging.level
scafctl config unset logging.format
```

## Common Workflows

### Debugging a Failing Solution

```bash
# Step 1: Run with debug to see what's happening
scafctl run solution -f solution.yaml --debug

# Step 2: If you need even more detail
scafctl run solution -f solution.yaml --log-level trace

# Step 3: Capture logs for a bug report
scafctl run solution -f solution.yaml --log-level trace --log-format json --log-file debug.log 2>&1
```

### CI/CD Pipeline

```bash
# Fail-fast with only error logs in JSON for log aggregation
scafctl run solution -f solution.yaml --log-level error --log-format json
```

Or set via environment:

```yaml
# GitHub Actions example
env:
  SCAFCTL_LOG_LEVEL: error
  SCAFCTL_LOG_FORMAT: json
steps:
  - run: scafctl run solution -f solution.yaml
```

### Separating Logs from Output

Use `--log-file` to keep stderr clean while still capturing logs:

```bash
# Logs to file, styled output to stderr, data to stdout
scafctl run solution -f solution.yaml --log-level debug --log-file /tmp/run.log -o json > results.json

# Review logs separately
cat /tmp/run.log
```

### Temporary Debug Session

Set and unset config for a debugging session:

```bash
# Enable debug temporarily via config
scafctl config set logging.level debug

# Run your commands...
scafctl run solution -f solution.yaml

# Reset when done
scafctl config unset logging.level
```

Or use the environment variable (no config changes needed):

```bash
SCAFCTL_DEBUG=1 scafctl run solution -f solution.yaml
```

## Summary

| What You Want | Command |
|---|---|
| Clean output (default) | `scafctl run solution -f s.yaml` |
| Quick debug | `scafctl ... --debug` |
| Specific level | `scafctl ... --log-level warn` |
| JSON for tooling | `scafctl ... --log-level info --log-format json` |
| Logs to file | `scafctl ... --debug --log-file debug.log` |
| CI/CD env vars | `SCAFCTL_LOG_LEVEL=error SCAFCTL_LOG_FORMAT=json` |

## Next Steps

- [Configuration Tutorial](config-tutorial.md) — Manage all application settings
- [Getting Started](getting-started.md) — Run your first solution
