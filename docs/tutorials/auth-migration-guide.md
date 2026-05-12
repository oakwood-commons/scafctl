# Auth Handler Migration Guide

This guide covers migrating from built-in auth handlers to the plugin-based
auth handler system in scafctl.

## Overview

scafctl is transitioning from compiled-in auth handlers (entra, github, gcp) to
a plugin-based architecture where handlers run as separate gRPC processes. This
enables independent updates, third-party handlers, and catalog-based distribution.

## What Changed

### Auto-Resolution

Auth handlers are now resolved automatically from the official auth handler
registry. When you run `scafctl auth login github`, scafctl:

1. Checks the local auth registry for a handler named "github"
2. If not found, checks the official registry for a catalog reference
3. Auto-fetches and installs the plugin from the catalog
4. Registers it and proceeds with login

### Handler Discovery

Use `scafctl auth handlers` to see all known handlers and their status:

~~~sh
scafctl auth handlers
~~~

Output columns:

| Column      | Description                                    |
|-------------|------------------------------------------------|
| name        | Handler identifier (e.g., entra, github, gcp)  |
| displayName | Human-readable name                            |
| status      | installed, available, or not-found             |
| source      | built-in, plugin, or catalog                   |
| flows       | Supported authentication flows                 |
| loggedIn    | Whether currently authenticated                |

### Strict Mode

Use `--strict` to disable auto-fetching and require all handlers to be
pre-installed:

~~~sh
scafctl auth login github --strict
~~~

This is useful in CI environments where you want to fail fast if a handler
is not available.

## Configuration Changes

### Custom Entra Domains

Entra handler configuration remains in the config file:

~~~yaml
auth:
  entra:
    clientId: "your-client-id"
    tenantId: "your-tenant-id"
    defaultScopes:
      - "https://graph.microsoft.com/.default"
~~~

These settings are forwarded to the plugin via `ConfigureAuthHandler` RPC.

### GitHub Enterprise Server (GHES) Hostname

~~~yaml
auth:
  github:
    hostname: "github.example.com"
    clientId: "your-ghes-client-id"
~~~

### Google Regional Domains

~~~yaml
auth:
  gcp:
    clientId: "your-client-id"
    impersonateServiceAccount: "sa@project.iam.gserviceaccount.com"
~~~

### Trusted Verification Domains

Per-handler trusted domains can be configured globally:

~~~yaml
auth:
  trustedVerificationDomains:
    - "login.microsoftonline.com"
    - "github.com"
~~~

## Telemetry

The plugin auth system includes OpenTelemetry instrumentation:

- **auth.plugin.login** -- span for each login operation
- **auth.plugin.status** -- span for each status query
- **auth.plugin.get_token** -- span for each token retrieval
- **auth_plugin_startup_duration_seconds** -- histogram for plugin process startup latency

Use `scafctl auth diagnose` to see startup latency per handler:

~~~
[ok]   entra: startup latency  Handler startup: 65ms (plugin)
[ok]   github: startup latency  Handler startup: <1ms (built-in)
~~~

## Offline Behavior

When offline, plugin-based handlers behave as follows:

- **Cached tokens**: Token retrieval works normally from the local cache
- **Login**: Requires network for OAuth flows; PAT/service-principal flows may
  work offline
- **Auto-fetch**: Plugin download from catalog requires network; use `--strict`
  to skip

## Emergency Rollback

If the plugin system causes issues, the built-in handlers are still available
in the current release. They take priority in the registry when both are present.

## Token Migration

Existing tokens cached by built-in handlers are compatible with the plugin
handlers. Use `scafctl auth migrate` to migrate token storage if needed:

~~~sh
scafctl auth migrate
~~~

## Diagnostics

Run `scafctl auth diagnose` for a full health check including:

- Registry status (which handlers are registered)
- Handler source (built-in vs plugin vs catalog)
- Plugin startup latency
- Configuration validation
- Token cache health
- Network connectivity
