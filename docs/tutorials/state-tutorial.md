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
9. [Save Targets and `extends: load`](#save-targets-and-extends-load)
10. [Multiple Save Targets](#multiple-save-targets)
11. [Migrating from `state.backend`](#migrating-from-statebackend)
12. [Common Patterns](#common-patterns)

---

## How State Works

The state system is built on a simple principle: **CLI parameters are the backbone of replay**.

When state is enabled and you run a solution with `-r key=value` flags, those parameters are automatically persisted to state. On subsequent runs, saved parameters are merged with any new CLI parameters (CLI wins on conflict), so the solution replays with the same inputs without you having to re-provide them.

This eliminates the need to manually configure which resolver values to save. Every parameter you pass is remembered.

Reading and writing are configured separately in the `state` block:

- **`state.load`** -- where state is READ from, before resolvers run. Load never writes.
- **`state.save`** -- the list of places state is WRITTEN, after a successful run. This is the only write mechanism: nothing is saved unless a save target says so.

Both are optional, giving four modes:

| `load` | `save` | Behavior |
|--------|--------|----------|
| yes | yes | Persist across runs (the common case) |
| yes | no | Read-only replay -- nothing is ever written |
| no | yes | Start from empty state every run, publish the result |
| no | no | Does nothing (lint warning `empty-state-config`) |

The state file stores several things:

| Section | Purpose |
|---------|---------|
| `parameters` | Merged set of all CLI parameters across runs (drives replay) |
| `resolvers` | Persisted resolver values, including locked values of `immutable` resolvers |
| `fingerprints` | Action file hashes for up-to-date checks |

---

## Your First Stateful Solution

Let's create a solution that remembers your inputs across runs.

### Step 1: Create the Solution File

Create a file called `state-demo.yaml`:

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: state-demo
  version: 1.0.0

state:
  enabled: true
  load:
    provider: file
    inputs:
      path: "state-demo.json"
  save:
    - extends: load

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
~~~

### Step 2: Run the Solution

{{< tabs "state-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
~~~bash
scafctl run resolver -f state-demo.yaml -r username=alice -r region=eu-west-1
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
scafctl run resolver -f state-demo.yaml -r username=alice -r region=eu-west-1
~~~
{{% /tab %}}
{{< /tabs >}}

Output (stderr notices shown together with the resolved values):

~~~text
state: no prior state at state-demo.json (first run)
region: eu-west-1
team: default
username: alice
state: saved state-demo.json (full)
~~~

The parameters `username=alice` and `region=eu-west-1` are now saved to `state-demo.json` in the current working directory (where the command above was run). The `load` block read state before resolvers ran (nothing was there yet), and the `extends: load` save target wrote the document back to the same file after the run succeeded.

### Understanding the Structure

- **state.enabled** -- Activates state persistence. Can be a literal `true`, a CEL expression, a template, or a `rslvr:` reference to a state-independent resolver. Because state is loaded before resolvers run, resolvers that read state cannot be referenced here.
- **state.load.provider** -- The provider state is read from. Load never writes. Use `file` for local files.
- **state.load.inputs.path** -- Where to read the state file from. Relative paths are resolved against the current working directory. Absolute paths are used as-is. The CLI state commands (`scafctl state list`, etc.) resolve relative paths against the current working directory too, so both agree on the same file.
- **state.save** -- The only way state is written. The `extends: load` value is the default target: inherit the load block's provider and inputs, so the run writes back to the file it read from.

No per-resolver configuration is needed. All CLI parameters are persisted automatically when state is enabled.

---

## Replaying from State

On subsequent runs, saved parameters are automatically injected as if you had passed them via `-r`. The `parameter` provider sees them without any extra configuration.

### Step 1: Second Run (No Parameters Needed)

{{< tabs "state-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
~~~bash
scafctl run resolver -f state-demo.yaml
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
scafctl run resolver -f state-demo.yaml
~~~
{{% /tab %}}
{{< /tabs >}}

Output:

~~~text
state: reusing 2 parameter(s) and 0 locked value(s) from state-demo.json
region: eu-west-1
team: default
username: alice
state: saved state-demo.json (full)
~~~

Both values come from the saved parameters in state. No re-prompting needed.

### How It Works

1. State is loaded from the load block's provider before resolver execution.
2. Saved parameters are merged with CLI parameters (CLI wins on conflict).
3. The merged parameter set is made available to resolvers via the `parameter` provider.
4. After execution, the merged parameters are written by the save targets.

This means the `parameter` provider seamlessly reads from state on repeat runs -- no fallback chains or special configuration required.

---

## Parameter Merging

Parameters accumulate across runs. Existing keys are overwritten by CLI values, and keys not provided on the CLI are preserved from state.

### Example: Overriding a Saved Parameter

{{< tabs "state-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
~~~bash
# First run: set username and region
scafctl run resolver -f state-demo.yaml -r username=alice -r region=eu-west-1

# Second run: override region, keep username from state
scafctl run resolver -f state-demo.yaml -r region=us-west-2
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
# First run: set username and region
scafctl run resolver -f state-demo.yaml -r username=alice -r region=eu-west-1

# Second run: override region, keep username from state
scafctl run resolver -f state-demo.yaml -r region=us-west-2
~~~
{{% /tab %}}
{{< /tabs >}}

After the second run, the state file contains:

~~~json
{
  "parameters": {
    "username": "alice",
    "region": "us-west-2"
  }
}
~~~

- `username` was preserved from the first run (not provided again).
- `region` was overwritten by the CLI value.

### Example: Adding a New Parameter

You can add a new parameter on a later run, as long as the solution declares it as a resolver parameter:

{{< tabs "state-tutorial-cmd-3b" >}}
{{% tab "Bash" %}}
~~~bash
# Third run: add team (declared in the solution), keep username and region from state
scafctl run resolver -f state-demo.yaml -r team=platform
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
# Third run: add team (declared in the solution), keep username and region from state
scafctl run resolver -f state-demo.yaml -r team=platform
~~~
{{% /tab %}}
{{< /tabs >}}

After the third run, the state file contains:

~~~json
{
  "parameters": {
    "username": "alice",
    "region": "us-west-2",
    "team": "platform"
  }
}
~~~

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

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: state-immutable
  version: 1.0.0

state:
  enabled: true
  load:
    provider: file
    inputs:
      path: "state-immutable.json"
  save:
    - extends: load

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
~~~

Immutable locks live only in a **full-format** document, and they only work if the saved document is read back -- so a solution with immutable resolvers needs a `load` block and a full-format save target (lint warns on `state-requires-load` / `state-requires-full-save` otherwise). The `extends: load` target defaults to the `full` format.

### Step 2: First Run

{{< tabs "state-tutorial-cmd-4" >}}
{{% tab "Bash" %}}
~~~bash
scafctl run resolver -f state-immutable.yaml -r project_id=proj-abc123 -r region=eu-west-1
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
scafctl run resolver -f state-immutable.yaml -r project_id=proj-abc123 -r region=eu-west-1
~~~
{{% /tab %}}
{{< /tabs >}}

Output:

~~~text
project_id: proj-abc123
region: eu-west-1
~~~

The value `proj-abc123` is now locked in the `resolvers` section of state (with `immutable: true`).

### Step 3: Attempt to Change (Fails)

{{< tabs "state-tutorial-cmd-5" >}}
{{% tab "Bash" %}}
~~~bash
scafctl run resolver -f state-immutable.yaml -r project_id=proj-xyz789
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
scafctl run resolver -f state-immutable.yaml -r project_id=proj-xyz789
~~~
{{% /tab %}}
{{< /tabs >}}

Output:

~~~text
Error: state save: cannot overwrite immutable state entry "project_id": resolved value differs from locked value; use the state delete command to remove it first
~~~

### Step 4: Unlocking an Immutable Value

To change an immutable value, explicitly delete it from state first. The `--force` flag removes the key from both `parameters` and `resolvers` in a single call:

{{< tabs "state-tutorial-cmd-6" >}}
{{% tab "Bash" %}}
~~~bash
# Delete from both parameters and locked immutable values in one call
scafctl state delete --path state-immutable.json --key project_id --force
scafctl run resolver -f state-immutable.yaml -r project_id=proj-xyz789
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
# Delete from both parameters and locked immutable values in one call
scafctl state delete --path state-immutable.json --key project_id --force
scafctl run resolver -f state-immutable.yaml -r project_id=proj-xyz789
~~~
{{% /tab %}}
{{< /tabs >}}

### State File Structure with Immutables

~~~json
{
  "schemaVersion": 3,
  "parameters": {
    "project_id": "proj-abc123",
    "region": "eu-west-1"
  },
  "resolvers": {
    "project_id": {
      "value": "proj-abc123",
      "type": "string",
      "immutable": true,
      "createdAt": "2026-01-15T10:00:00Z"
    }
  }
}
~~~

Note that `project_id` appears in both `parameters` (for replay) and `resolvers` (for change detection). They serve different purposes: `parameters` drives replay input, the `immutable` entries in `resolvers` enforce value consistency.

---

## Dynamic State Paths

Use Go templates in load inputs to create per-project state files.

### Step 1: Create the Solution File

Create a file called `state-dynamic.yaml`:

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: state-dynamic
  version: 1.0.0

state:
  enabled: true
  load:
    provider: file
    inputs:
      path:
        tmpl: "deploy/{{ .__params.project }}.json"
  save:
    - extends: load

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
~~~

### Step 2: Run with Different Projects

{{< tabs "state-tutorial-cmd-7" >}}
{{% tab "Bash" %}}
~~~bash
scafctl run resolver -f state-dynamic.yaml -r project=frontend -r region=us-west-2
scafctl run resolver -f state-dynamic.yaml -r project=backend -r region=eu-west-1
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
scafctl run resolver -f state-dynamic.yaml -r project=frontend -r region=us-west-2
scafctl run resolver -f state-dynamic.yaml -r project=backend -r region=eu-west-1
~~~
{{% /tab %}}
{{< /tabs >}}

Each project gets its own state file with its own parameter history:

~~~text
<solution-dir>/deploy/frontend.json
<solution-dir>/deploy/backend.json
~~~

---

## CLI Commands

The `scafctl state` command group lets you inspect and modify state files directly.

### List Keys

{{< tabs "state-tutorial-cmd-8" >}}
{{% tab "Bash" %}}
~~~bash
scafctl state list --path state-demo.json
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
scafctl state list --path state-demo.json
~~~
{{% /tab %}}
{{< /tabs >}}

### Get a Parameter Value

{{< tabs "state-tutorial-cmd-9" >}}
{{% tab "Bash" %}}
~~~bash
scafctl state get --path state-demo.json --key username
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
scafctl state get --path state-demo.json --key username
~~~
{{% /tab %}}
{{< /tabs >}}

### Set a Parameter Value Manually

{{< tabs "state-tutorial-cmd-10" >}}
{{% tab "Bash" %}}
~~~bash
scafctl state set --path state-demo.json --key username --value bob
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
scafctl state set --path state-demo.json --key username --value bob
~~~
{{% /tab %}}
{{< /tabs >}}

### Delete a Key

{{< tabs "state-tutorial-cmd-11" >}}
{{% tab "Bash" %}}
~~~bash
# Delete a parameter
scafctl state delete --path state-demo.json --key username

# Delete an immutable value (requires --force)
scafctl state delete --path state-demo.json --key project_id --force
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
# Delete a parameter
scafctl state delete --path state-demo.json --key username

# Delete an immutable value (requires --force)
scafctl state delete --path state-demo.json --key project_id --force
~~~
{{% /tab %}}
{{< /tabs >}}

### Clear All Values

{{< tabs "state-tutorial-cmd-12" >}}
{{% tab "Bash" %}}
~~~bash
scafctl state clear --path state-demo.json
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
scafctl state clear --path state-demo.json
~~~
{{% /tab %}}
{{< /tabs >}}

> [!NOTE]
> `scafctl state list` and `scafctl state get` support `-o json`, `-o yaml`, and `-o quiet` output formats. The `--path` flag is relative to the current working directory. Use an absolute path to reference files in other locations.

---

## Command Behavior

State behavior varies across the commands that support it.

### `run resolver`

Loads state before resolvers execute and **runs the save targets immediately after resolvers complete**.

- Saved parameters are merged with CLI parameters before execution
- Resolvers execute using the merged parameter set
- After all resolvers succeed, every enabled save target is written
- Immutable checks run after resolver execution
- If any resolver fails, state is NOT saved (no partial state)

This is the simplest state lifecycle -- load, merge, resolve, save.

### `run solution` and `run action`

Loads state before resolvers execute and **runs the save targets only after actions complete successfully**.

- Saved parameters merged with CLI parameters (same as `run resolver`)
- Resolvers execute using merged parameters; resolved immutable values are verified against their locks before any action runs
- Save targets marked `checkpoint: true` (full format only) are written **before** the workflow actions run, so immutable values locked this run survive a later action failure
- Actions execute using resolver data
- Every enabled save target is written only after successful action execution
- If any action fails (and no checkpoint target exists), state is NOT saved -- even if resolvers succeeded

This ensures state reflects only fully successful executions, with the checkpoint flag as the deliberate opt-in for value continuity across failed runs.

### `render solution`

Loads state (read-only) but **never runs save targets**.

- Saved parameters are merged with CLI parameters
- Resolvers execute using merged parameters
- The action graph is rendered (not executed) using resolved values
- State is NEVER written -- render is a read-only operation

Use `render solution` to preview what an action graph would look like with current state values, without modifying state.

### Summary Table

| Command | Loads State | Saves State | Save Trigger |
|---------|-------------|-------------|--------------|
| `run resolver` | Yes | Yes | After resolvers complete |
| `run solution` | Yes | Yes | After actions succeed (checkpoint targets: also before actions) |
| `run action` | Yes | Yes | After actions succeed (checkpoint targets: also before actions) |
| `render solution` | Yes | No (read-only) | -- |

### Run-Time Feedback

Every load and save prints a one-line stderr notice, so state's effect on a
run is never silent:

~~~text
$ scafctl run resolver -f ./solution.yaml -r username=alice -r region=us-west1
state: no prior state at .scafctl/state.json (first run)
state: saved .scafctl/state.json (full)
...

$ scafctl run resolver -f ./solution.yaml
state: reusing 2 parameter(s) and 0 locked value(s) from .scafctl/state.json
state: saved .scafctl/state.json (full)
...
~~~

A save line is printed once per save target actually written, in declaration
order -- so a solution publishing both a full local file and a lean intent
document reports both (e.g., `state: saved intent/sandbox.json (intent)`). A
disabled save target (its `enabled` condition evaluated false) prints nothing,
and so does a checkpoint write. When a save target's provider has no `path`
or `url` input to report, the line names the provider instead of a location
(e.g., `state: saved http provider (full)`).

These notices are on stderr, respect `--quiet` (fully suppressed), and never
appear in structured stdout (`-o json`/`-o yaml`).

### Skipping State (`--no-state`)

Pass `--no-state` to `run solution`, `run resolver`, `run action`, or `render solution` to skip the **entire** state lifecycle for that invocation:

- State is **not loaded** before resolvers run.
- Immutable values are **neither verified nor locked**.
- No save target runs.

This is intended for CI pipelines and offline environments where the state provider is unavailable or persistence is undesirable. When the solution declares a `state` block and `--no-state` is set, a one-line notice is written to stderr (suppressed by `--quiet`).

`--no-state` disables state entirely and **cannot be combined** with
`--state-file`, `--state-output`, `--no-state-output`, or
`--allow-missing-locks` -- combining them is an error.

Two consequences to keep in mind:

- Resolvers that read the `state` provider fall back to their defaults, since no prior values are loaded.
- Immutability is **not enforced** while `--no-state` is active -- values that are normally locked can change freely. Use the flag deliberately.

~~~bash
# Run without touching state at all
scafctl run solution -f ./solution.yaml --no-state
~~~

### Pointing at a State File (`--state-file`)

Pass `--state-file <path>` to `run solution`, `run resolver`, or `run action` to replace the solution's `load` block with a **read** of an existing file, using the `file` provider. Each state flag replaces exactly one half of the configuration; a solution's own `state` block remains the primary way to configure state.

- `PATH` must exist. A missing file is an input error (exit code 3: `--state-file: state file "PATH" does not exist`). A declared load block tolerates a first run; an explicitly named file is expected to be there.
- The solution's declared **save targets still run** -- an `extends: load` target keeps writing the load location the solution declared, so the file you passed in is never overwritten. The flag redirects where a run reads, never where a solution's saves write.
- When the solution declares **no** `state` block, the flag enables state for the run as a **read-only replay** -- parameters replay from the file, nothing is written.
- When the solution declares a `state` block, a one-line notice reports what was replaced (so you are never silently switched off a configured load, e.g. `github`): `--state-file: reading state from X instead of the solution's "github" load`.
- The flag **forces state on**. If the solution's `state.enabled` could turn state off, the run still loads and saves, with the notice `--state-file: overriding the solution's state.enabled; state is enabled for this run`. The condition is not evaluated first because it may depend on parameters that only the file carries. (`--state-output` forces state on the same way.)
- Relative paths resolve against the directory you ran the command from -- also for a bundled catalog solution, which runs from a temporary extraction directory.
- It is **mutually exclusive with `--no-state`**.

~~~bash
# Drive a run from a specific state file
scafctl run solution -f ./solution.yaml --state-file ./intent/sandbox.json
~~~

#### Intent documents: a state file is a superset of its own inputs

A state file loads tolerantly -- missing sections default to empty. So a
**subset** document carrying only what is needed to replay (exactly the shape
the `intent` [format](#multiple-save-targets) produces) is a valid state input:

~~~json
{
  "schemaVersion": 3,
  "metadata": { "solution": "deploy-app", "version": "1.5.4" },
  "parameters": { "appName": "my-app", "environmentName": "sandbox" }
}
~~~

This document is the human-readable, replay-complete subset of a state file. A
run pointed at it replays the parameters; what gets written depends on the
run's save half as usual -- the solution's declared save targets still run
(an `extends: load` target keeps writing the solution's declared load
location, so the intent file itself is never overwritten in place), and adding
`--state-output` writes the fresh full document somewhere new. If the file
records a `metadata.solution` / `metadata.version` that differs from the
solution being run, an advisory stderr warning is emitted; it never fails the
run.

An optional opaque `attestation` field may ride along in the state document;
scafctl never interprets it and preserves it verbatim across the
load -> save round-trip, so a downstream verifier can read it back unchanged.

#### Overriding the Save Side (`--state-output`, `--no-state-output`)

- **`--state-output PATH`** replaces every declared save target with a single
  full-format file save at PATH (never a checkpoint). It enables state even
  without a `state` block -- on its own it is save-only (the run starts from
  empty state); combined with `--state-file` it reads from one file and writes
  to another. A notice reports the replaced targets:
  `--state-output: writing full state to X instead of the solution's N save target(s)`.
- **`--no-state-output`** skips every declared save target; state is still
  loaded and immutable values are still verified. Notice:
  `--no-state-output: skipping the solution's N save target(s)`.
- `--state-output` and `--no-state-output` are mutually exclusive.

#### The Lock-Less Replay Guard (`--allow-missing-locks`)

An intent document has parameters but no `metadata.createdAt` and no immutable
locks. Replaying one through a solution with `immutable: true` resolvers would
re-derive those values and replace any previously saved locks, so the run
**fails** (exit code 3) with a message like:

~~~text
state "intent.json" has parameters but no immutable locks: immutable
resolver(s) [clusterId] would be re-derived and any previously saved locks
replaced; re-run with --allow-missing-locks if this is intended
~~~

Pass `--allow-missing-locks` to proceed anyway (with a stderr warning).
`render solution` and dry runs only warn -- they never save, so they cannot
replace a lock. A genuine full-format state file always has `createdAt`, so it
never trips the guard.

~~~bash
# Pipeline pattern: replay a committed intent, publish fresh full state
scafctl run solution -f ./app.yaml --state-file intent.json \
  --state-output .state/env.json --allow-missing-locks
~~~

---

## Save Targets and `extends: load`

By default, a `state.save` target would need to repeat the load block's
provider setup to write back where the data came from. `extends: load` skips
that: the target inherits the **declared** load block's provider and inputs,
with its own `inputs` merged on top (the target wins on a key conflict; a
nil/dangling input key never erases an inherited one). `extends` and
`provider` are mutually exclusive on a target, and `extends` requires a `load`
block (lint `invalid-state-save-extends`).

### The Problem

State loads before resolvers run, so load-time fields (`state.enabled`,
`state.load.inputs`) cannot use resolver references (`rslvr:`) or `_` in CEL
-- those values do not exist yet. But at save time, resolvers have already
executed: a save target's own `inputs` and `enabled` are resolved then, and
may reference any resolver.

### How It Works

1. At **load time**: only the load block's `inputs` are resolved (using `__params`, literals, and state-independent resolver values from the Phase-A pre-load pass).
2. At **save time**: each save target's `inputs` and `enabled` are resolved with every resolver's output available. For an `extends: load` target, its inputs are merged on top of the inherited load inputs.

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
  load:
    provider: github
    inputs:
      owner: { literal: "my-org" }
      repo: { literal: "my-repo" }
      path:
        expr: "'state/' + __params.app_name + '.json'"
      ref: { literal: "main" }
  save:
    - extends: load
      inputs:
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

{{< tabs "state-tutorial-cmd-extends" >}}
{{% tab "Bash" %}}
~~~bash
scafctl run resolver -f ./pr-workflow.yaml \
  -r app_name=my-app -r branch_name=feat/my-feature
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
scafctl run resolver -f ./pr-workflow.yaml `
  -r app_name=my-app -r branch_name=feat/my-feature
~~~
{{% /tab %}}
{{< /tabs >}}

At load time, state is read from `main` using the `ref` input. At save time,
the `extends: load` target writes back through the load block's provider with
the load inputs, plus its own `branch` and `message` inputs -- so it saves to
`feat/my-feature`. After the PR merges, subsequent runs load the merged state
from `main`.

### Lint Rules

Lint rules help validate save-target configuration:

| Rule | Severity | What It Catches |
|------|----------|----------------|
| `state-save-state-ref` | Error | A save target's input or `enabled` condition referencing the state being saved (`__state`; circular dependency) |
| `state-ref-state-dependent` | Error | `state.enabled` or `state.load.inputs` referencing a state-dependent resolver -- one that reads state or depends on one that does (circular dependency) |
| `state-ref-unknown` | Error | `state.enabled` or `state.load.inputs` referencing a resolver that is not defined in `spec.resolvers` (almost always a typo) |
| `state-github-no-save-branch` | Info | GitHub save target without a save-specific `branch` input (hint for PR workflows) |

Run `scafctl lint` to check your configuration.

### Restrictions

- Save-target `inputs` and `enabled` **can** use `rslvr:`, `_` in CEL, and `__params` -- every resolver has run by save time.
- They **cannot** reference the state being saved (`__state`; circular dependency, lint `state-save-state-ref`).
- `extends: load` and `provider` are mutually exclusive on a target, and `extends` requires a `load` block; the target's inputs are merged on top of the inherited ones (target wins on conflict).
- Load-time fields (`state.enabled`, `state.load.inputs`) may only reference state-independent resolvers; a state-dependent reference is rejected (lint `state-ref-state-dependent`).

> [!TIP]
> See [examples/solutions/state/github-state.yaml](https://github.com/oakwood-commons/scafctl/blob/main/examples/solutions/state/github-state.yaml) for a complete working example.

---

## Multiple Save Targets

`state.save` is a list, so a solution can write its state to **more than one
place** -- each entry an independent write with its own provider, format,
inputs, `enabled` condition, and `checkpoint` flag.

### The Problem

You want a full-fidelity state file locally (so immutable locks and action
fingerprints persist across runs), but you also want to publish a lean,
human-readable, **signable** document -- for example an "intent" file a
managed pipeline commits and later replays to regenerate trusted output. One
write can only be one shape at a time; the save list lets it be both.

### How It Works

Each `state.save` entry is one target. `format` controls what shape of the
document that target receives:

- `"full"` (the default) -- the complete state document: parameters, locked
  immutable values, fingerprints, command info.
- `"intent"` -- only `schemaVersion`, `metadata.solution`/`metadata.version`,
  `parameters`, and `attestation` (when present). No timestamps, no resolver
  locks, no fingerprints -- deterministic and stable for signing.

### Example: Full State Locally, Intent Published

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: deploy-app
  version: 1.0.0

state:
  enabled: true
  load:
    provider: file
    inputs:
      path: ".scafctl/state.json"
  save:
    - extends: load                       # full document back to the load file
    - provider: file
      format: intent
      inputs:
        path: "intent/sandbox.json"

spec:
  resolvers:
    app_name:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "app_name"

    cluster_id:
      type: string
      immutable: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "cluster_id"
~~~

Run it:

~~~bash
scafctl run resolver -f ./deploy-app.yaml -r app_name=my-app
~~~

Two files are written: `.scafctl/state.json` (everything, including the
`cluster_id` immutable lock) and `intent/sandbox.json` (just the solution
identity and `app_name`). Commit only the second file -- it is what you'd sign
and hand to a pipeline via `--state-file` (see
[Pointing at a State File](#pointing-at-a-state-file---state-file)).
[examples/solutions/state-save/solution.yaml](https://github.com/oakwood-commons/scafctl/blob/main/examples/solutions/state-save/solution.yaml)
is a complete, runnable version of this pattern (including a `checkpoint`
target and parameter narrowing).

### Per-Target `enabled`

Unlike the top-level `state.enabled` (which is evaluated at **load** time,
and can therefore only reference state-independent resolvers), a save
target's `enabled` is evaluated at **save** time -- after every resolver has
run. It may reference **any** resolver, with no acyclic restriction:

~~~yaml
save:
  - extends: load
  - provider: file
    format: intent
    inputs: { path: "intent/sandbox.json" }
    enabled: { expr: "_.environment == 'sandbox'" }
~~~

Absent `enabled` defaults to `true` (always save). A target whose `enabled`
evaluates false is skipped silently -- no `state: saved` line for it.

### Failure Semantics

There is no "primary" target: targets are written in **declaration order**.
A failure saving one target aborts the **remaining** targets, but does not
undo the targets that already succeeded.

### Checkpoints

Save targets run only after a successful run -- so by default, a failed action
writes nothing and any immutable value minted during the run is lost (the next
run mints a new one). Marking a **full**-format target with
`checkpoint: true` (opt-in, default false) makes `run solution` and
`run action` write it **before** the workflow actions run -- after resolvers
and immutable verification -- so locks minted this run survive a later action
failure. The checkpoint write keeps the saved parameters as loaded (new `-r`
values are saved only by the final, post-success save) and prints nothing.

A checkpoint target is therefore written **twice** on a successful run --
before the actions and again after them. For a side-effecting provider (a
`github` target commits, an `http` target sends a request), that means two
writes per successful run.

~~~yaml
save:
  - extends: load
    checkpoint: true
~~~

### The Lossy Shortcut

You can make every save target `format: intent` when the intent document
genuinely is the only state that matters (e.g. a pipeline whose entire job is
replaying a committed intent). But it is **lossy**: immutable resolver locks
and action fingerprints live only in the full document, which the intent
format omits, so `scafctl lint` warns
(`state-requires-full-save`) whenever a solution with immutable resolvers or
action fingerprints has no full-format save target -- and `state-requires-load`
warns when there is also no `load` block to read the saved locks back. Prefer
the full-plus-intent pattern above unless you specifically want this
trade-off.

Related run-time guard: replaying an intent document (parameters, no locks)
through a solution with immutable resolvers fails (exit 3) unless
`--allow-missing-locks` is passed -- see
[The Lock-Less Replay Guard](#the-lock-less-replay-guard---allow-missing-locks).

### Parameter Narrowing

An `"intent"`-format target projects every saved parameter by default.
`parameters.include`/`parameters.exclude` narrow that down -- useful for
dropping a run-control parameter (a publish/generate mode switch, one-off
destination coordinates) that has no business in a committed, signed artifact:

~~~yaml
state:
  enabled: true
  load:
    provider: file
    inputs: { path: ".scafctl/state.json" }
  save:
    - extends: load
    - provider: file
      format: intent
      parameters:
        exclude: [mode]   # run-control param stays out of the committed intent
      inputs: { path: "intent/sandbox.json" }
~~~

- `include` is an allowlist: only the listed names are projected. Safer for a
  new solution -- an unlisted (e.g. newly added) parameter can never leak by
  omission.
- `exclude` is a denylist: everything except the listed names is projected.
  More practical when a solution has a large, evolving parameter surface,
  where the small set of non-domain parameters is the shorter, more stable
  list to maintain.
- `include` and `exclude` are mutually exclusive -- setting both is a lint
  error.
- Only valid under `format: intent`. Setting it on a `"full"` (or unset)
  target is a lint error too: narrowing the authoritative state document
  would silently drop a parameter the solution still relies on for replay.

### Lint Rules

| Rule | Severity | What It Catches |
|------|----------|----------------|
| `missing-state-save-provider` | Error | A `state.save` entry specifies neither `provider` nor `extends: load` |
| `invalid-state-save-provider` | Error | A save target's `provider` is unregistered or lacks `CapabilityState` |
| `invalid-state-save-extends` | Error | Bad `extends`: unsupported value, combined with `provider`, or used without a `state.load` block |
| `invalid-state-save-checkpoint` | Error | `checkpoint: true` on a target that is not the `"full"` format |
| `invalid-state-format` | Error | `format` is set to something other than `full` or `intent` |
| `invalid-state-parameter-narrowing` | Error | A target's `parameters` narrowing is set, but its `format` is not `"intent"` |
| `conflicting-state-parameter-narrowing` | Error | A target's `parameters` sets both `include` and `exclude` |
| `state-requires-load` | Warning | Immutable resolvers or action fingerprints, but no `state.load` block |
| `state-requires-full-save` | Warning | Immutable resolvers or action fingerprints, but no full-format save target |
| `immutable-without-checkpoint` | Info | Immutable resolvers and workflow actions, but no `checkpoint: true` save target |
| `empty-state-config` | Warning | The `state` block configures neither `load` nor `save` |

Run `scafctl lint` to check your configuration.

---

## Migrating from `state.backend`

> [!WARNING]
> **Breaking change.** The standalone `state.backend` block and the
> `state.emit` list were replaced by `state.load` plus a `state.save` list.
> Old keys are rejected when the solution is decoded -- every entrypoint fails
> loudly with a migration hint, because a silently ignored `state.backend`
> would turn state off with no error at all.

| Old (removed) | New |
|---------------|-----|
| `state.backend` | `state.load` (read only) + a `state.save` target; `extends: load` keeps writing to the same place |
| `state.emit` entries | Additional entries in the `state.save` list |
| `backend` (or emit) `saveOverrides` | The save target's own `inputs` (with `extends: load`, merged on top of the load inputs) |
| Load-side `format` / `parameters` | Move onto a `state.save` target; the load side has neither |
| `state: updated ...` / `state: emitted ...` notices | `state: saved <location> (<format>)` per written target |

Before (rejected):

~~~yaml
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: ".scafctl/state.json"
  emit:
    - provider: file
      format: intent
      inputs:
        path: "intent.json"
~~~

After:

~~~yaml
state:
  enabled: true
  load:
    provider: file
    inputs:
      path: ".scafctl/state.json"
  save:
    - extends: load
    - provider: file
      format: intent
      inputs:
        path: "intent.json"
~~~

For the full old -> new mapping (including lint rule renames and the
`<provider> backend` -> `<provider> provider` label), see
[Migrating from `state.backend`](../design/state/state.md#migrating-from-statebackend)
in the design reference.

---

## Common Patterns

### Dynamic state activation

~~~yaml
state:
  enabled:
    expr: "has(__params.enable_state) && __params.enable_state == 'true'"
  load:
    provider: file
    inputs:
      path: "my-app.json"
  save:
    - extends: load
~~~

State is only active when the `enable_state` CLI parameter is set to `true` (e.g., `-r enable_state=true`). The `has()` guard prevents a CEL error when the parameter is omitted, and the value is compared as a string since CLI parameters are always strings.

### Replay in CI validation

A CI validator can replay a solution whose state lives alongside generated code:

~~~yaml
# app-registration.yaml -- state file lives alongside generated code
state:
  enabled: true
  load:
    provider: file
    inputs:
      path: "./apps/my-app/state.json"
  save:
    - extends: load
~~~

~~~bash
# Replay from the current working directory (parameters are loaded automatically from state)
scafctl run solution -f app-registration.yaml
~~~

Because all CLI parameters are persisted in state, the solution replays deterministically without any `-r` flags. The validator can then diff the output against the PR contents.

### Immutable infrastructure identifiers

~~~yaml
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
~~~

These values are locked after first execution. If someone accidentally passes a different `resource_group` on a subsequent run, the execution fails rather than silently deploying to the wrong resource group.

### Combining parameters with computed values

~~~yaml
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
~~~

On replay, `base_url` comes from saved parameters, and `api_endpoint` is recomputed identically. Computed resolvers do not need any special configuration -- they derive from the persisted parameters naturally.
