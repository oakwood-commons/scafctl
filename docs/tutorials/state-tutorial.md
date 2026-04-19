---
title: "State Tutorial"
weight: 95
---

# State Tutorial

This tutorial walks you through using state persistence to retain resolver values across solution executions. You'll learn how to configure state, read from it on subsequent runs, and manage state files via the CLI.

## Prerequisites

- scafctl installed and available in your PATH
- Basic familiarity with YAML syntax and solution files
- Understanding of resolvers and the provider system

## Table of Contents

1. [Overview](#overview)
2. [Enabling State](#enabling-state)
3. [Saving Resolver Values](#saving-resolver-values)
4. [Reading from State](#reading-from-state)
5. [First Run vs Subsequent Runs](#first-run-vs-subsequent-runs)
6. [Dynamic State Paths](#dynamic-state-paths)
7. [GitHub Backend](#github-backend)
8. [CLI Commands](#cli-commands)
9. [Sensitive Values](#sensitive-values)
10. [Common Patterns](#common-patterns)

---

## Overview

State adds optional, per-solution persistence of resolver values. It enables two workflows:

- **Re-run with same data** -- Retain resolved values between runs without re-prompting.
- **Validation replay** -- Replay the exact command with the same flags and verify results.

State is opt-in. Solutions without a `state` block behave as they always have -- stateless and self-contained.

### How It Works

1. Before resolvers run, state is loaded from the configured backend (e.g., local file).
2. The `state` provider can read previously saved values during resolver execution.
3. After resolvers complete, values marked with `saveToState: true` are persisted.

---

## Enabling State

Add a top-level `state` block to your solution:

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-stateful-app
  version: 1.0.0
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: "my-stateful-app.json"
spec:
  resolvers:
    greeting:
      type: string
      saveToState: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "greeting"
~~~

Key fields:

- `state.enabled` -- Activates state. Can be a literal `true`, a CEL expression, resolver reference, or template.
- `state.backend.provider` -- The provider that handles persistence. Use `file` for local files or `github` for GitHub repos.
- `state.backend.inputs` -- Provider-specific inputs. For `file`, only `path` is required.

The `path` is relative to the XDG state directory (`~/.local/state/scafctl/` on macOS/Linux).

---

## Saving Resolver Values

Mark resolvers with `saveToState: true` to persist their values:

~~~yaml
spec:
  resolvers:
    api_key:
      type: string
      sensitive: true
      saveToState: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "API Key"

    region:
      type: string
      saveToState: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "AWS Region"
              default: "us-east-1"
~~~

All `saveToState` values are collected after all resolvers complete, then flushed to the backend in a single save call. This ensures no partial state on failures.

---

## Reading from State

Use the `state` provider to read previously saved values:

~~~yaml
spec:
  resolvers:
    api_key:
      type: string
      saveToState: true
      resolve:
        with:
          # Try reading from state first
          - provider: state
            inputs:
              key: "api_key"
              required: false
          # Fall back to prompting the user
          - provider: parameter
            inputs:
              key: "API Key"
~~~

The resolver fallback chain makes this work naturally:

1. On first run, `state` returns null (no state file exists), so the resolver falls through to `parameter`.
2. After execution, the result is saved to state via `saveToState: true`.
3. On subsequent runs, `state` returns the cached value and the chain stops.

### State Provider Inputs

| Input | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `key` | string | Yes | -- | State entry key (typically a resolver name) |
| `required` | bool | No | `false` | Error when key is not found |
| `fallback` | any | No | `null` | Value when key is not found and `required` is `false` |

---

## First Run vs Subsequent Runs

Here's the full flow using a concrete example. Create `state-demo.yaml`:

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: state-demo
  version: 1.0.0
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: "state-demo.json"
spec:
  resolvers:
    username:
      type: string
      saveToState: true
      resolve:
        with:
          - provider: state
            inputs:
              key: "username"
              required: false
          - provider: parameter
            inputs:
              key: "username"

    run_count:
      type: int
      saveToState: true
      resolve:
        with:
          - provider: state
            inputs:
              key: "run_count"
              fallback: 0
~~~

**First run:**

```
scafctl run resolver -f state-demo.yaml -r username=alice
```

Output:

```
username: alice
run_count: 0
```

**Second run** (no parameters needed -- values come from state):

```
scafctl run resolver -f state-demo.yaml
```

Output:

```
username: alice
run_count: 0
```

The `username` is now read from state. The `run_count` shows `0` from the fallback because the state provider read phase runs before the new save.

---

## Dynamic State Paths

Use Go templates in backend inputs to create per-project state files:

~~~yaml
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path:
        tmpl: "deploy/{{ .project_name }}.json"
spec:
  resolvers:
    project_name:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "project"
~~~

Each project gets its own state file:

```
~/.local/state/scafctl/deploy/frontend.json
~/.local/state/scafctl/deploy/backend.json
```

---

## GitHub Backend

Store state in a GitHub repository instead of the local filesystem:

~~~yaml
state:
  enabled: true
  backend:
    provider: github
    inputs:
      owner: "my-org"
      repo: "state-store"
      path: "state/my-app.json"
      branch: "main"
~~~

This commits state changes directly to the repository using the GitHub GraphQL API. Authentication is handled by the configured GitHub auth handler.

---

## CLI Commands

The `scafctl state` command group lets you inspect and modify state files directly.

### List keys

```
scafctl state list --path state-demo.json
```

### Get a value

```
scafctl state get --path state-demo.json --key username
```

### Set a value manually

```
scafctl state set --path state-demo.json --key username --value bob
```

### Delete a key

```
scafctl state delete --path state-demo.json --key username
```

### Clear all values

```
scafctl state clear --path state-demo.json
```

All commands support `-o json`, `-o yaml`, and `-o quiet` output formats.

The `--path` flag is relative to the XDG state directory. Use an absolute path to reference files outside the state directory.

---

## Sensitive Values

When a resolver is marked `sensitive: true` and `saveToState: true`, the value is stored **in plaintext** in the state file. A lint warning is emitted to alert the solution author:

~~~yaml
resolvers:
  api_key:
    type: string
    sensitive: true
    saveToState: true
    resolve:
      with:
        - provider: parameter
          inputs:
            key: "API Key"
~~~

```
scafctl lint -f solution.yaml
```

```
WARNING [state-sensitive-value] Sensitive resolver "api_key" with saveToState will be stored in plaintext
```

This is an explicit, informed decision. Encryption is not used because the validation application running on a separate machine would not have access to decryption keys.

---

## Common Patterns

### Cache expensive API calls

~~~yaml
resolvers:
  auth_token:
    type: string
    saveToState: true
    resolve:
      with:
        - provider: state
          inputs:
            key: "auth_token"
            required: false
        - provider: http
          inputs:
            url: "https://auth.example.com/token"
            method: POST
~~~

On first run, the HTTP call fetches the token. On subsequent runs, it comes from state.

### Dynamic state activation

~~~yaml
state:
  enabled:
    expr: "env('ENABLE_STATE') == 'true'"
  backend:
    provider: file
    inputs:
      path: "my-app.json"
~~~

State is only active when the `ENABLE_STATE` environment variable is set to `true`.

### Writing state from actions

~~~yaml
workflow:
  actions:
    - name: save-deployment-id
      provider: state
      inputs:
        key: "deployment_id"
        value:
          rslvr: deployment_result
~~~

Actions can explicitly write values to state using the `state` provider with `action` capability.
