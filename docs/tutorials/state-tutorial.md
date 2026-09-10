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
10. [Emit Targets](#emit-targets)
11. [Common Patterns](#common-patterns)

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

### Run-Time Feedback

Every load and save prints a one-line stderr notice, so state's effect on a
run is never silent:

~~~bash
$ scafctl run resolver -f ./solution.yaml -r username=alice -r region=us-west1
state: no prior state at .scafctl/state.json (first run)
state: updated .scafctl/state.json (full)
...

$ scafctl run resolver -f ./solution.yaml
state: reusing 2 parameter(s) and 0 locked value(s) from .scafctl/state.json
state: updated .scafctl/state.json (full)
...
~~~

A save line is printed once per backend actually written -- the primary,
plus each enabled [Emit Target](#emit-targets) (`state: emitted <path>
(intent)`) -- so a solution publishing both a full local file and a lean
intent document reports both. A disabled emit target (its `enabled`
evaluated false) prints nothing.

These notices are on stderr, respect `--quiet` (fully suppressed), and never
appear in structured stdout (`-o json`/`-o yaml`).

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

### Pointing at a State File (`--state-file`)

Pass `--state-file <path>` to `run solution`, `run resolver`, or `run action` to
read and write state at a specific file, using the `file` backend. It is an
**alternate input source** for state; a solution's own `state` block remains
the primary way to configure state.

- When the solution declares **no** `state` block, the flag enables state for
  the run -- so a solution can be driven by an external state file without
  being authored for it.
- When the solution declares a `state` block, the flag **overrides it
  entirely** (provider, format, and `emit` targets included) and a one-line
  notice reports what was replaced (so you are never silently switched off a
  configured backend, e.g. `github`), including how many `emit` targets were
  dropped if the replaced block had any. The synthesized config **inherits the
  `format`** the solution's own primary backend declared.
- State is read from and written back to the path, so successive runs
  accumulate into it.
- It is **mutually exclusive with `--no-state`**.

~~~bash
# Drive a run from a specific state file
scafctl run solution -f ./solution.yaml --state-file ./intent/sandbox.json
~~~

#### Intent documents: a state file is a superset of its own inputs

A state file loads tolerantly -- missing sections default to empty. So a
**subset** document carrying only what is needed to replay (exactly the shape
the `intent` [format](#emit-targets) produces) is a valid state input:

~~~json
{
  "schemaVersion": 3,
  "metadata": { "solution": "deploy-app", "version": "1.5.4" },
  "parameters": { "appName": "my-app", "environmentName": "sandbox" }
}
~~~

This document is the human-readable, replay-complete subset of a state file. A
run pointed at it via `--state-file` replays the parameters and, at save time,
**regenerates the full state document in place** (runtime metadata,
timestamps, resolver locks) -- unless the inherited format is itself `intent`,
in which case it stays lean. If the file records a `metadata.solution` /
`metadata.version` that differs from the solution being run, an advisory
stderr warning is emitted; it never fails the run.

An optional opaque `attestation` field may ride along in the state document;
scafctl never interprets it and preserves it verbatim across the
load -> save round-trip, so a downstream verifier can read it back unchanged.

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

## Emit Targets

A solution's state is not limited to a single saved copy. `emit` lets you save
**additional, save-only projections** of the same state document, each through
its own backend -- the primary `backend` is still the only one used for
**load**; `emit` targets are extra outputs produced only when saving.

### The Problem

You want a full-fidelity state file locally (so immutable locks and action
fingerprints persist across runs), but you also want to publish a lean,
human-readable, **signable** document -- for example an "intent" file a
managed pipeline commits and later replays to regenerate trusted output. A
single backend can only be one shape at a time; `emit` lets it be both.

### How It Works

Each entry in `emit` is a full `Backend` (`provider`, `format`, `inputs`,
`saveOverrides`) plus an optional per-target `enabled` condition. `format`
controls what shape of the document that backend receives:

- `"full"` (the default) -- the complete state document.
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
  backend:                                 # full-fidelity primary
    provider: file
    inputs:
      path: ".scafctl/state.json"
  emit:
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

### Per-Target `enabled`

Unlike the top-level `state.enabled` (which is evaluated at **load** time,
before any resolver has run, and can therefore only reference
state-independent resolvers), an `emit` target's `enabled` is evaluated at
**save** time -- after every resolver has run. It may reference **any**
resolver, with no acyclic restriction:

~~~yaml
emit:
  - provider: file
    format: intent
    inputs: { path: "intent/sandbox.json" }
    enabled: { expr: "_.environment == 'sandbox'" }
~~~

Absent `enabled` defaults to `true` (always emit).

### Failure Semantics

- A failure saving the **primary** backend aborts before any `emit` target
  runs.
- A failure saving one `emit` target aborts the **remaining** targets (in
  order) but does not undo the primary save or any earlier `emit` target that
  already succeeded.

### The Lossy Shortcut

You can set `format: intent` directly on the **primary** backend with no
`emit` at all -- useful when the intent document genuinely is the only state
that matters (e.g. a pipeline whose entire job is replaying a committed
intent). But it is **lossy**: immutable resolver locks live in the `resolvers`
section, which the intent format omits, so `scafctl lint` warns
(`state-format-lossy-with-immutable`) whenever a solution combines
`format: intent` on the primary backend with any `immutable: true` resolver.
Prefer the full-primary-plus-emit pattern above unless you specifically want
this trade-off.

### Lint Rules

| Rule | Severity | What It Catches |
|------|----------|----------------|
| `missing-state-emit-backend` | Error | An `emit` entry with no `provider` |
| `invalid-state-emit-backend` | Error | An `emit` entry's `provider` is unregistered or lacks `CapabilityState` |
| `invalid-state-format` | Error | `format` is set to something other than `full` or `intent` |
| `state-format-lossy-with-immutable` | Warning | The primary backend's `format` is `intent` while the solution has an `immutable` resolver |

Run `scafctl lint` to check your configuration.

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
