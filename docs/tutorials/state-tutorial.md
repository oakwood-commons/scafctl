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
9. [Common Patterns](#common-patterns)

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
username: alice
region: eu-west-1
```

The parameters `username=alice` and `region=eu-west-1` are now saved to `~/.local/state/scafctl/state-demo.json`.

### Understanding the Structure

- **state.enabled** -- Activates state persistence. Can be a literal `true`, a CEL expression, or template. Because state is loaded before resolvers run, resolver references (`rslvr:...`) are not supported here.
- **state.backend.provider** -- The provider that handles persistence. Use `file` for local files.
- **state.backend.inputs.path** -- Where to store the state file, relative to the XDG state directory (`~/.local/state/scafctl/` on macOS/Linux).

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
username: alice
region: eu-west-1
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

Parameters accumulate across runs. New keys are added, existing keys are overwritten by CLI values.

### Example: Gradual Parameter Building

{{< tabs "state-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
# First run: set username and region
scafctl run resolver -f state-demo.yaml -r username=alice -r region=eu-west-1

# Second run: override region, add a new parameter
scafctl run resolver -f state-demo.yaml -r region=us-west-2 -r team=platform
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# First run: set username and region
scafctl run resolver -f state-demo.yaml -r username=alice -r region=eu-west-1

# Second run: override region, add a new parameter
scafctl run resolver -f state-demo.yaml -r region=us-west-2 -r team=platform
```
{{% /tab %}}
{{< /tabs >}}

After the second run, the state file contains:

```json
{
  "parameters": {
    "username": "alice",
    "region": "us-west-2",
    "team": "platform"
  }
}
```

- `username` was preserved from the first run (not provided again).
- `region` was overwritten by the CLI value.
- `team` was added as a new key.

### Merge Rules

| Scenario | Behavior |
|----------|----------|
| Key in state, not in CLI | Preserved from state |
| Key in both state and CLI | CLI value wins |
| Key in CLI, not in state | Added to state |
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
Error: immutable entry "project_id": resolved value differs from locked value; use 'scafctl state delete' to remove it first
```

### Step 4: Unlocking an Immutable Value

To change an immutable value, explicitly delete it from state first:

{{< tabs "state-tutorial-cmd-6" >}}
{{% tab "Bash" %}}
```bash
scafctl state delete --path state-immutable.json --key project_id --immutable
scafctl run resolver -f state-immutable.yaml -r project_id=proj-xyz789
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl state delete --path state-immutable.json --key project_id --immutable
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
~/.local/state/scafctl/deploy/frontend.json
~/.local/state/scafctl/deploy/backend.json
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

# Delete an immutable value
scafctl state delete --path state-demo.json --key project_id --immutable
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Delete a parameter
scafctl state delete --path state-demo.json --key username

# Delete an immutable value
scafctl state delete --path state-demo.json --key project_id --immutable
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
> `scafctl state list` and `scafctl state get` support `-o json`, `-o yaml`, and `-o quiet` output formats. The `--path` flag is relative to the XDG state directory. Use an absolute path to reference files outside the state directory.

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

---

## Common Patterns

### Dynamic state activation

```yaml
state:
  enabled:
    expr: "__params.enable_state == true"
  backend:
    provider: file
    inputs:
      path: "my-app.json"
```

State is only active when the `enable_state` CLI parameter is set to `true` (e.g., `-r enable_state=true`).

### Replay in CI validation

A CI validator can replay a solution using the state file committed alongside generated code:

```bash
# Replay using the state file from the PR (parameters are loaded automatically)
scafctl run solution -f app-registration.yaml --state-file ./apps/my-app/state.json
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
