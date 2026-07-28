---
title: "State Tutorial"
weight: 95
---

# State Tutorial

This tutorial walks you through using state persistence in scafctl. The state system automatically persists CLI parameters (`-r` values) across runs so that solutions can be replayed without re-providing inputs. You'll learn how parameter replay works, how to lock values with `immutable`, and how to manage state files via the CLI.

## Prerequisites

- scafctl installed and available in your PATH
- Basic familiarity with YAML syntax and solution files
- Understanding of resolvers and the provider system

## Table of Contents

1. [How State Works](#how-state-works)
2. [Your First Stateful Solution](#your-first-stateful-solution)
3. [Replaying from State](#replaying-from-state)
4. [Parameter Merging](#parameter-merging)
5. [Immutable Resolvers](#immutable-resolvers)
6. [Dynamic State Paths](#dynamic-state-paths)
7. [CLI Commands](#cli-commands)
8. [Command Behavior](#command-behavior)
9. [Save-Time Overrides](#save-time-overrides)
10. [Common Patterns](#common-patterns)

---

## How State Works

The state system is built on a simple principle: **CLI parameters are the backbone of replay**.

When state is enabled and you run a solution with `-r key=value` flags, those parameters are automatically persisted to the state file. On subsequent runs, saved parameters are merged with any new CLI parameters (CLI wins on conflict), so the solution replays with the same inputs without you having to re-provide them.

This eliminates the need to manually configure which resolver values to save. Every parameter you pass is remembered.

The state file stores three things:

| Section | Purpose |
|---------|---------|
| `parameters` | Merged set of all CLI parameters across runs (drives replay) |
| `immutables` | Locked resolver values that must not change between runs |
| `fingerprints` | Action file hashes for up-to-date checks |

---

## Your First Stateful Solution

Let's create a solution that remembers your inputs across runs.

### Step 1: Create the Solution File

Create a file called `state-demo.yaml`:

```yaml
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
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "username"

    region:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "region"
          - provider: static
            inputs:
              value: "us-east-1"

    team:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "team"
          - provider: static
            inputs:
              value: "default"
```

### Step 2: Run the Solution

{{< tabs "state-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f state-demo.yaml -r username=alice -r region=eu-west-1
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f state-demo.yaml -r username=alice -r region=eu-west-1
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
region: eu-west-1
team: default
username: alice
```

The parameters `username=alice` and `region=eu-west-1` are now saved to `state-demo.json` in the solution file's parent directory.

### Understanding the Structure

- **state.enabled** -- Activates state persistence. Can be a literal `true`, a CEL expression, or template. Because state is loaded before resolvers run, resolver references (`rslvr:...`) are not supported here.
- **state.backend.provider** -- The provider that handles persistence. Use `file` for local files.
- **state.backend.inputs.path** -- Where to store the state file. Relative paths are resolved against the solution file's parent directory. Absolute paths are used as-is. CLI state commands (`scafctl state list`, etc.) resolve relative paths against the current working directory.

No per-resolver configuration is needed. All CLI parameters are persisted automatically when state is enabled.

---

## Replaying from State

On subsequent runs, saved parameters are automatically injected as if you had passed them via `-r`. The `parameter` provider sees them without any extra configuration.

### Step 1: Second Run (No Parameters Needed)

{{< tabs "state-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f state-demo.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f state-demo.yaml
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
region: eu-west-1
team: default
username: alice
```

Both values come from the saved parameters in state. No re-prompting needed.

### How It Works

1. State file is loaded before resolver execution.
2. Saved parameters are merged with CLI parameters (CLI wins on conflict).
3. The merged parameter set is made available to resolvers via the `parameter` provider.
4. After execution, the merged parameters are saved back to state.

This means the `parameter` provider seamlessly reads from state on repeat runs -- no fallback chains or special configuration required.

---

## Parameter Merging

Parameters accumulate across runs. Existing keys are overwritten by CLI values, and keys not provided on the CLI are preserved from state.

### Example: Overriding a Saved Parameter

{{< tabs "state-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
# First run: set username and region
scafctl run resolver -f state-demo.yaml -r username=alice -r region=eu-west-1

# Second run: override region, keep username from state
scafctl run resolver -f state-demo.yaml -r region=us-west-2
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# First run: set username and region
scafctl run resolver -f state-demo.yaml -r username=alice -r region=eu-west-1

# Second run: override region, keep username from state
scafctl run resolver -f state-demo.yaml -r region=us-west-2
```
{{% /tab %}}
{{< /tabs >}}

After the second run, the state file contains:

```json
{
  "parameters": {
    "username": "alice",
    "region": "us-west-2"
  }
}
```

- `username` was preserved from the first run (not provided again).
- `region` was overwritten by the CLI value.

### Example: Adding a New Parameter

You can add a new parameter on a later run, as long as the solution declares it as a resolver parameter:

{{< tabs "state-tutorial-cmd-3b" >}}
{{% tab "Bash" %}}
```bash
# Third run: add team (declared in the solution), keep username and region from state
scafctl run resolver -f state-demo.yaml -r team=platform
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Third run: add team (declared in the solution), keep username and region from state
scafctl run resolver -f state-demo.yaml -r team=platform
```
{{% /tab %}}
{{< /tabs >}}

After the third run, the state file contains:

```json
{
  "parameters": {
    "username": "alice",
    "region": "us-west-2",
    "team": "platform"
  }
}
```

- `username` and `region` were preserved from state.
- `team` was added as a new key.

> [!NOTE]
> Only parameters that correspond to a declared resolver with `provider: parameter` are accepted. Passing an unknown key (e.g., `-r foo=bar` when no resolver uses `key: "foo"`) produces an error by default. To relax this (for example, when reusing a shared parameter file across solutions), pass `--on-unknown-resolver=warn` to proceed with a warning, or `--on-unknown-resolver=ignore` to accept unknown keys silently. The default policy can also be set via the `resolver.onUnknownResolver` config field.

### Merge Rules

| Scenario | Behavior |
|----------|----------|
| Key in state, not in CLI | Preserved from state |
| Key in both state and CLI | CLI value wins |
| Key in CLI, not in state | Added to state (must be a declared resolver parameter) |
| No CLI parameters | All saved parameters used as-is |

---

## Immutable Resolvers

Mark a resolver as `immutable: true` to lock its resolved value in state after first execution. On subsequent runs, the resolver must produce the same value or execution fails.

This is useful for values that must never change once established (e.g., resource IDs, project names used in infrastructure).

### Step 1: Create the Solution File

Create a file called `state-immutable.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: state-immutable
  version: 1.0.0

state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: "state-immutable.json"

spec:
  resolvers:
    project_id:
      type: string
      immutable: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "project_id"

    region:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "region"
          - provider: static
            inputs:
              value: "us-east-1"
```

### Step 2: First Run

{{< tabs "state-tutorial-cmd-4" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f state-immutable.yaml -r project_id=proj-abc123 -r region=eu-west-1
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f state-immutable.yaml -r project_id=proj-abc123 -r region=eu-west-1
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
project_id: proj-abc123
region: eu-west-1
```

The value `proj-abc123` is now locked in the `immutables` section of state.

### Step 3: Attempt to Change (Fails)

{{< tabs "state-tutorial-cmd-5" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f state-immutable.yaml -r project_id=proj-xyz789
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f state-immutable.yaml -r project_id=proj-xyz789
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
Error: state save: cannot overwrite immutable state entry "project_id": resolved value differs from locked value; use the state delete command to remove it first
```

### Step 4: Unlocking an Immutable Value

To change an immutable value, explicitly delete it from state first. The `--force` flag removes the key from both `parameters` and `immutables` in a single call:

{{< tabs "state-tutorial-cmd-6" >}}
{{% tab "Bash" %}}
```bash
# Delete from both parameters and immutables in one call
scafctl state delete --path state-immutable.json --key project_id --force
scafctl run resolver -f state-immutable.yaml -r project_id=proj-xyz789
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Delete from both parameters and immutables in one call
scafctl state delete --path state-immutable.json --key project_id --force
scafctl run resolver -f state-immutable.yaml -r project_id=proj-xyz789
```
{{% /tab %}}
{{< /tabs >}}

### State File Structure with Immutables

```json
{
  "schemaVersion": 1,
  "parameters": {
    "project_id": "proj-abc123",
    "region": "eu-west-1"
  },
  "immutables": {
    "project_id": {
      "value": "proj-abc123",
      "type": "string",
      "createdAt": "2026-01-15T10:00:00Z"
    }
  }
}
```

Note that `project_id` appears in both `parameters` (for replay) and `immutables` (for change detection). They serve different purposes: `parameters` drives replay input, `immutables` enforces value consistency.

---

## Dynamic State Paths

Use Go templates in backend inputs to create per-project state files.

### Step 1: Create the Solution File

Create a file called `state-dynamic.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: state-dynamic
  version: 1.0.0

state:
  enabled: true
  backend:
    provider: file
    inputs:
      path:
        tmpl: "deploy/{{ .__params.project }}.json"

spec:
  resolvers:
    project:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "project"

    region:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "region"
          - provider: static
            inputs:
              value: "us-east-1"
```

### Step 2: Run with Different Projects

{{< tabs "state-tutorial-cmd-7" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f state-dynamic.yaml -r project=frontend -r region=us-west-2
scafctl run resolver -f state-dynamic.yaml -r project=backend -r region=eu-west-1
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f state-dynamic.yaml -r project=frontend -r region=us-west-2
scafctl run resolver -f state-dynamic.yaml -r project=backend -r region=eu-west-1
```
{{% /tab %}}
{{< /tabs >}}

Each project gets its own state file with its own parameter history:

```
<solution-dir>/deploy/frontend.json
<solution-dir>/deploy/backend.json
```

---

## CLI Commands

The `scafctl state` command group lets you inspect and modify state files directly.

### List Keys

{{< tabs "state-tutorial-cmd-8" >}}
{{% tab "Bash" %}}
```bash
scafctl state list --path state-demo.json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl state list --path state-demo.json
```
{{% /tab %}}
{{< /tabs >}}

### Get a Parameter Value

{{< tabs "state-tutorial-cmd-9" >}}
{{% tab "Bash" %}}
```bash
scafctl state get --path state-demo.json --key username
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl state get --path state-demo.json --key username
```
{{% /tab %}}
{{< /tabs >}}

### Set a Parameter Value Manually

{{< tabs "state-tutorial-cmd-10" >}}
{{% tab "Bash" %}}
```bash
scafctl state set --path state-demo.json --key username --value bob
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl state set --path state-demo.json --key username --value bob
```
{{% /tab %}}
{{< /tabs >}}

### Delete a Key

{{< tabs "state-tutorial-cmd-11" >}}
{{% tab "Bash" %}}
```bash
# Delete a parameter
scafctl state delete --path state-demo.json --key username

# Delete an immutable value (requires --force)
scafctl state delete --path state-demo.json --key project_id --force
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Delete a parameter
scafctl state delete --path state-demo.json --key username

# Delete an immutable value (requires --force)
scafctl state delete --path state-demo.json --key project_id --force
```
{{% /tab %}}
{{< /tabs >}}

### Clear All Values

{{< tabs "state-tutorial-cmd-12" >}}
{{% tab "Bash" %}}
```bash
scafctl state clear --path state-demo.json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl state clear --path state-demo.json
```
{{% /tab %}}
{{< /tabs >}}

> [!NOTE]
> `scafctl state list` and `scafctl state get` support `-o json`, `-o yaml`, and `-o quiet` output formats. The `--path` flag is relative to the current working directory. Use an absolute path to reference files in other locations.

---

## Command Behavior

State behavior varies across the commands that support it.

### `run resolver`

Loads state before resolvers execute and **saves state immediately after resolvers complete**.

- Saved parameters are merged with CLI parameters before execution
- Resolvers execute using the merged parameter set
- After all resolvers succeed, the merged parameters are persisted
- Immutable checks run after resolver execution
- If any resolver fails, state is NOT saved (no partial state)

This is the simplest state lifecycle -- load, merge, resolve, save.

### `run solution` and `run action`

Loads state before resolvers execute but **saves state only after actions complete successfully**.

- Saved parameters merged with CLI parameters (same as `run resolver`)
- Resolvers execute using merged parameters
- Actions execute using resolver data
- State is saved only after successful action execution
- If actions fail, state is NOT saved -- even if resolvers succeeded

This ensures state reflects only fully successful executions.

### `render solution`

Loads state (read-only) but **never saves state**.

- Saved parameters are merged with CLI parameters
- Resolvers execute using merged parameters
- The action graph is rendered (not executed) using resolved values
- State is NEVER written -- render is a read-only operation

Use `render solution` to preview what an action graph would look like with current state values, without modifying state.

### Summary Table

| Command | Loads State | Saves State | Save Trigger |
|---------|-------------|-------------|--------------|
| `run resolver` | Yes | Yes | After resolvers complete |
| `run solution` | Yes | Yes | After actions succeed |
| `run action` | Yes | Yes | After actions succeed |
| `render solution` | Yes | No (read-only) | -- |

### Skipping State (`--no-state`)

Pass `--no-state` to `run solution`, `run resolver`, `run action`, or `render solution` to skip the **entire** state lifecycle for that invocation:

- State is **not loaded** before resolvers run.
- Immutable values are **neither verified nor locked**.
- State is **not saved** afterward.

This is intended for CI pipelines and offline environments where the state backend is unavailable or persistence is undesirable. When the solution declares a `state` block and `--no-state` is set, a one-line notice is written to stderr (suppressed by `--quiet`).

Two consequences to keep in mind:

- Resolvers that read the `state` provider fall back to their defaults, since no prior values are loaded.
- Immutability is **not enforced** while `--no-state` is active -- values that are normally locked can change freely. Use the flag deliberately.

~~~bash
# Run without touching the state backend
scafctl run solution -f ./solution.yaml --no-state
~~~

---

## Save-Time Overrides

Some workflows need different backend inputs at load time vs save time. For example, a GitHub backend may load state from the `main` branch but save state to a feature branch determined by a resolver. The `saveOverrides` field makes this possible.

### The Problem

Backend `inputs` are resolved at **both** load and save time. Because state loads before resolvers run, `inputs` cannot use resolver references (`rslvr:`) or `_` in CEL -- those values do not exist yet.

But at save time, resolvers have already executed. `saveOverrides` lets you provide inputs that are only resolved at save time, when resolver data is available.

### How It Works

1. At **load time**: only `inputs` are resolved (using `__params` and literals).
2. At **save time**: both `inputs` and `saveOverrides` are resolved. Keys in `saveOverrides` override keys in `inputs`.

### Example: GitHub PR Workflow

This pattern loads state from `main` and saves to a resolver-derived feature branch:

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: pr-workflow
  version: 1.0.0

state:
  enabled: true
  backend:
    provider: github
    inputs:
      owner: { literal: "my-org" }
      repo: { literal: "my-repo" }
      path:
        expr: "'state/' + __params.app_name + '.json'"
      ref: { literal: "main" }
    saveOverrides:
      branch: { rslvr: featureBranch }
      message:
        expr: "'chore(state): update state for ' + _.app_name"

spec:
  resolvers:
    app_name:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "app_name"

    featureBranch:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "branch_name"
~~~

Run it:

{{< tabs "state-tutorial-cmd-saveoverrides" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f ./pr-workflow.yaml \
  -r app_name=my-app -r branch_name=feat/my-feature
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f ./pr-workflow.yaml `
  -r app_name=my-app -r branch_name=feat/my-feature
```
{{% /tab %}}
{{< /tabs >}}

At load time, state is read from `main` using the `ref` input. At save time, the `branch` from `saveOverrides` overrides the default, saving state to `feat/my-feature`. After the PR merges, subsequent runs load the merged state from `main`.

### Lint Rules

Two lint rules help validate `saveOverrides` configuration:

| Rule | Severity | What It Catches |
|------|----------|----------------|
| `state-save-override-state-ref` | Error | `saveOverrides` referencing the `state` provider (circular dependency) |
| `state-github-no-save-branch` | Info | GitHub backend without a save-specific branch (hint for PR workflows) |

Run `scafctl lint` to check your configuration.

### Restrictions

- `saveOverrides` values **can** use `rslvr:`, `_` in CEL, and `__params`.
- `saveOverrides` values **cannot** reference the `state` provider (circular dependency).
- Keys in `saveOverrides` override same-named keys in `inputs` at save time only.
- At load time, `saveOverrides` are completely ignored -- no errors are raised for resolver-dependent expressions.

> [!TIP]
> See [examples/solutions/state/github-state.yaml](https://github.com/oakwood-commons/scafctl/blob/main/examples/solutions/state/github-state.yaml) for a complete working example.

---

## Common Patterns

### Dynamic state activation

```yaml
state:
  enabled:
    expr: "has(__params.enable_state) && __params.enable_state == 'true'"
  backend:
    provider: file
    inputs:
      path: "my-app.json"
```

State is only active when the `enable_state` CLI parameter is set to `true` (e.g., `-r enable_state=true`). The `has()` guard prevents a CEL error when the parameter is omitted, and the value is compared as a string since CLI parameters are always strings.

### Replay in CI validation

A CI validator can replay a solution where the state backend path points to a committed state file:

```yaml
# app-registration.yaml -- state file lives alongside generated code
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: "./apps/my-app/state.json"
```

```bash
# Replay from the solution directory (parameters are loaded automatically from state)
scafctl run solution -f app-registration.yaml
```

Because all CLI parameters are persisted in state, the solution replays deterministically without any `-r` flags. The validator can then diff the output against the PR contents.

### Immutable infrastructure identifiers

```yaml
resolvers:
  resource_group:
    type: string
    immutable: true
    resolve:
      with:
        - provider: parameter
          inputs:
            key: "resource_group"

  subscription_id:
    type: string
    immutable: true
    resolve:
      with:
        - provider: parameter
          inputs:
            key: "subscription_id"
```

These values are locked after first execution. If someone accidentally passes a different `resource_group` on a subsequent run, the execution fails rather than silently deploying to the wrong resource group.

### Combining parameters with computed values

```yaml
resolvers:
  base_url:
    type: string
    resolve:
      with:
        - provider: parameter
          inputs:
            key: "base_url"

  api_endpoint:
    # Derived from base_url -- computed fresh each run
    type: string
    resolve:
      with:
        - provider: static
          inputs:
            value:
              rslvr: base_url
    transform:
      with:
        - provider: cel
          inputs:
            expression: '__self + "/api/v2"'
```

On replay, `base_url` comes from saved parameters, and `api_endpoint` is recomputed identically. Computed resolvers do not need any special configuration -- they derive from the persisted parameters naturally.
