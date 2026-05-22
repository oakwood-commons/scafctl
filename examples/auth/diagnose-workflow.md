# Auth Diagnose Workflow

The `scafctl auth diagnose` command (alias: `auth doctor`) runs diagnostic
checks on your authentication configuration and reports any issues.

## Basic Usage

~~~bash
# Run diagnostics for all handlers
scafctl auth diagnose

# Run diagnostics for a specific handler
scafctl auth diagnose entra

# Output as JSON (for scripting or MCP consumption)
scafctl auth diagnose -o json
~~~

## Live Token Check

By default, diagnose only inspects cached state. Use `--live-token` to also
attempt a live token fetch, confirming end-to-end token acquisition works:

~~~bash
scafctl auth diagnose --live-token
scafctl auth diagnose entra --live-token
~~~

## What It Checks

| Check | Description |
|-------|-------------|
| Auth registry | Are any handlers registered? |
| Config file | Does your config file exist and contain auth sections? |
| Environment variables | Are handler-specific env vars set correctly? |
| Clock skew | Is local time within tolerance of an HTTPS server? |
| Auth status | Are you logged in to each handler? |
| Cached token health | Are there expired or missing tokens? |
| Plugin binary | Is the handler plugin binary present and executable? |

## Example Output

~~~text
Auth Diagnostics
================

[PASS] auth registry: 2 handlers registered (entra, github)
[PASS] config file: /Users/you/.config/scafctl/config.yaml exists
[PASS] clock skew: local time within 2s of remote
[PASS] entra: logged in, token valid (expires in 47m)
[WARN] github: token expired 3h ago -- run 'scafctl auth login github'

1 warning found. Run with --live-token to verify token refresh.
~~~

## JSON Output

~~~json
{
  "checks": [
    {"name": "auth_registry", "status": "pass", "message": "2 handlers registered"},
    {"name": "clock_skew", "status": "pass", "message": "within 2s"},
    {"name": "handler_entra", "status": "pass", "message": "token valid"},
    {"name": "handler_github", "status": "warn", "message": "token expired"}
  ],
  "summary": {"pass": 3, "warn": 1, "fail": 0}
}
~~~

## CI/CD Usage

Use diagnose in pipelines to verify auth is configured before running
solutions that require credentials:

~~~bash
# Fail the pipeline if auth is not healthy
scafctl auth diagnose -o json | jq -e '.summary.fail == 0'
~~~

## Related

- [Auth Tutorial](../../docs/tutorials/auth-tutorial.md) -- full auth walkthrough
- [Handler Lifecycle](handler-lifecycle.md) -- install/remove handler plugins
- [Custom OAuth2 Config](custom-oauth2-config.md) -- adding custom handlers
