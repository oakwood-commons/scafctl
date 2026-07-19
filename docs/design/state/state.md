---
title: "State"
weight: 14
---

# State

> The replay model described here is **parameter-based replay**. See
> [parameter-replay-design.md](parameter-replay-design.md) for the design
> rationale. The earlier `saveToState` field and resolver-facing `state`
> provider were removed before release -- they do not exist in the runtime.

## Purpose

State adds optional, per-solution persistence of the **CLI parameters** (`-r`
values) used to run a solution. It enables two primary workflows:

1. **Re-run without re-supplying inputs** -- Execute a solution repeatedly; the
   parameters from the previous run are replayed automatically, so resolvers
   produce the same values without re-prompting.
2. **Validation replay** -- A validation application can replay the exact
   command with the same parameters and verify it produces the same results.

State is opt-in. Solutions without a `state` block behave exactly as they do
today -- stateless, deterministic, and self-contained. State does not change the
resolver or provider execution model. It adds a persistence layer accessed
exclusively through the provider system.

The replay backbone is the **input parameters**, not resolver outputs:
scaffolding is deterministic, so the same inputs always produce the same
resolver set. The only exception is resolvers marked `immutable: true`, whose
resolved values are locked in state on the first run. Immutable locks are
committed after resolvers and deferred validation succeed but **before** actions
run; merged parameters are persisted **after** actions complete. See
[Two-Phase Validation](../two-phase-validation.md) for the full lifecycle.

State does not:

- Replace providers
- Alter resolver execution order
- Introduce implicit behavior
- Cache intermediate computations

---

## Implementation Status

| Feature | Status | Location |
|---------|--------|----------|
| `CapabilityState` on provider system | Done | `pkg/provider/provider.go` |
| `state.Config` on Solution struct | Done | `pkg/solution/solution.go` |
| Parameter replay (save / merge / replay CLI params) | Done | `pkg/state/manager.go`, `pkg/cmd/scafctl/run/` |
| `pkg/state/` package (types, manager, context, store) | Done | `pkg/state/` |
| `file` provider state operations | Done | `pkg/provider/builtin/fileprovider/file_state.go` |
| `http` provider state operations | Done | `pkg/provider/builtin/httpprovider/http_state.go` |
| `github` provider state operations | External | Separate repository (not part of this project) |
| State loading lifecycle (pre-execution) | Done | `pkg/cmd/scafctl/run/solution.go`, `resolver.go` |
| `scafctl state` CLI commands | Done | `pkg/cmd/scafctl/state/` |
| Validation rules (backend, sensitive warnings) | Done | `pkg/lint/` |
| Immutable resolver support | Done | `pkg/resolver/resolver.go` (field), `pkg/state/manager.go` (enforcement), `pkg/lint/` (rules) |

> Note: a `saveToState` resolver field and a resolver-facing `state` provider
> appeared in an earlier draft of this design. They were **removed** in favor of
> parameter replay and are not part of the runtime.

---

## Responsibilities

State is responsible for:

- Persisting resolver values between solution executions
- Storing the command and parameters used for each execution (for validation replay)
- Providing read/write access to stored values through the provider system
- Managing the state file lifecycle (create, load, save, delete)

State is not responsible for:

- Replacing provider execution (resolvers always run their configured providers)
- Caching intermediate computations
- Implicitly altering execution behavior
- Managing secrets or encryption (sensitive values are stored in plaintext -- see [Sensitive Values](#sensitive-values))

---

## Architecture

State uses a **single-layer backend model**: persistence is a provider
capability, and the state manager drives load/save around resolver execution.
Resolvers do not read or write state directly -- replay happens through the
parameter set the manager merges before resolvers run.

| Layer | Provider | Capability | Role |
|-------|----------|-----------|------|
| Backend | `file`, `http`, or `github` | `state` | Reads/writes the state data to storage |

State operations are merged into existing providers (`file`, `http`, `github`)
rather than using dedicated backend providers. This means:

- The `file`, `http`, and `github` providers each gained `CapabilityState` with `state_load`, `state_save`, and `state_delete` operations
- All persistence goes through the provider system -- no special-case I/O outside of providers
- Community or internal teams can implement custom backends by adding `CapabilityState` to any provider

### New Capability: `state`

A new `CapabilityState` is added to the provider capability system. This capability signals that a provider can act as a state persistence backend. It is not used by resolvers or actions directly -- only by the state manager during the pre-execution and post-execution phases.

Required output fields for `state` capability:

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | Whether the operation succeeded |

---

## Solution Configuration

State is declared via a top-level `state` field on the `Solution` struct, as a peer to `spec`, `catalog`, `bundle`, and `compose`.

### Config Type

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | `ValueRef` | Yes | Dynamic activation -- literal bool, CEL expression, or Go template. Resolver references (`rslvr:`) are not supported because state loads before resolvers run |
| `backend` | `Backend` | Yes | Backend provider configuration |

### Backend Type

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | `string` | Yes | Name of a registered provider with `CapabilityState` (e.g., `"file"`) |
| `inputs` | `map[string]*ValueRef` | Yes | Provider-specific inputs resolved at **both** load and save time. Must only use `literal`, `__params` expressions, or templates -- resolver references (`rslvr:`) and `_` in CEL are not available at load time. |
| `saveOverrides` | `map[string]*ValueRef` | No | Provider-specific inputs resolved **only** at save time. Can use resolver references (`rslvr:`), `_` in CEL, and all other ValueRef forms. Keys that overlap with `inputs` override them at save time. |

### Save-Time Inputs (`saveOverrides`)

Some state backends need different configuration for load vs save. For example, a GitHub backend may read state from the `main` branch but write state to a feature branch determined at runtime by a resolver.

`saveOverrides` are resolved only during `state_save` operations (after resolvers have executed). At load time, they are skipped entirely -- no errors are raised for resolver-dependent expressions.

At save time, the final input map is computed as:

```
finalInputs = merge(resolvedInputs, resolvedSaveOverrides)
```

Where `saveOverrides` keys override `inputs` keys. This allows a provider to use a default value from `inputs` (e.g., `branch: main`) that gets overridden by a save-specific value (e.g., `branch: { rslvr: featureBranch }`).

### Example

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: deploy-app
  version: 1.0.0
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path:
        tmpl: "deploy-app/{{ .__params.project_name }}.json"
spec:
  resolvers:
    project_name:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "project_name"
    # Replayed automatically from saved parameters on later runs
    api_key:
      type: string
      sensitive: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "api_key"
    # Locked in state after the first run; verified on later runs
    cluster_id:
      type: string
      immutable: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "cluster_id"
~~~

### Dynamic `enabled` Field

The `enabled` field is a `ValueRef`, which means it supports:

- **Literal**: `enabled: true`
- **CEL expression**: `enabled: { expr: "__params.enable_state == true" }`
- **Go template**: `enabled: { tmpl: "{{ .__params.enable_state }}" }`

Resolver references (`rslvr:`) are not supported because state is loaded before resolvers run. CEL expressions and templates can access CLI parameters (`-r` flags) via `__params`.

### Dynamic Backend Inputs

Backend inputs are `ValueRef` types -- the same polymorphic type used throughout scafctl. This enables per-project state files:

~~~yaml
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path:
        tmpl: "deploy-app/{{ .__params.project_name }}.json"
~~~

Here, `project_name` is a CLI parameter passed via `-r project_name=myapp`. Project A and Project B each get their own state file.

---

## State Data Schema

State is persisted as JSON. The schema includes a `schemaVersion` field for forward-compatible format migrations.

~~~json
{
  "schemaVersion": 3,
  "metadata": {
    "solution": "deploy-app",
    "version": "1.0.0",
    "createdAt": "2026-02-12T10:00:00Z",
    "lastUpdatedAt": "2026-02-12T11:30:00Z",
    "runtime": {
      "engine": { "name": "scafctl", "version": "1.8.0" },
      "cli": { "name": "mycli", "version": "3.2.0" }
    }
  },
  "command": {
    "subcommand": "run solution",
    "parameters": {
      "project": "foo"
    }
  },
  "parameters": {
    "project": "foo",
    "region": "us-east-1"
  },
  "immutables": {
    "cluster_id": {
      "value": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
      "type": "string",
      "createdAt": "2026-02-12T10:00:00Z"
    }
  }
}
~~~

### Fields

| Field | Description |
|-------|-------------|
| `schemaVersion` | Integer version for the state file format. Enables future migrations. |
| `metadata.solution` | Solution name from `metadata.name` |
| `metadata.version` | Solution version from `metadata.version` |
| `metadata.createdAt` | Timestamp of first state file creation |
| `metadata.lastUpdatedAt` | Timestamp of most recent state save |
| `metadata.runtime.engine.name` | Execution engine (scafctl library) name -- always `scafctl` |
| `metadata.runtime.engine.version` | Execution engine (scafctl library) build version |
| `metadata.runtime.cli.name` | Invoking CLI/frontend binary name. Equals the engine name for direct scafctl use; differs for embedded runners |
| `metadata.runtime.cli.version` | Invoking CLI/frontend version. Equals the engine version when not embedded or when the embedder supplies no version |
| `command.subcommand` | CLI subcommand used (e.g., `run solution`) |
| `command.parameters` | Key-value pairs from the most recent invocation's `-r/--resolver` flags |
| `parameters` | Merged set of all CLI parameters across runs (drives replay) |
| `immutables` | Map of immutable resolver name to locked `Entry` |

### Entry

| Field | Type | Description |
|-------|------|-------------|
| `value` | `any` | The locked resolver value |
| `type` | `string` | The resolver's declared type (string, int, float, bool, array, any) |
| `createdAt` | `timestamp` | When this entry was first locked |

### Command Capture

State stores the most recent invocation's command information -- **latest only, no history**. This enables a validation application to replay the exact command:

- `command.subcommand` -- the CLI subcommand (e.g., `run solution`)
- `command.parameters` -- the key-value pairs passed via `-r/--resolver` flags

Solution identity (name, version) is already in `metadata` and does not need to be duplicated in `command`.

### Storage Location

The built-in `file` provider backend resolves relative state paths against the solution file's parent directory (via `provider.SolutionDirectoryFromContext`). This keeps state files co-located with the solution that owns them. Absolute paths are used as-is.

CLI state commands (`scafctl state list`, `get`, `set`, `delete`, `clear`) resolve relative `--path` values against the current working directory.

---

## Parameter Replay

State persists the **CLI parameters** (`-r` values) used on each run and replays
them automatically on the next run. There is no resolver-level opt-in field --
all parameters are saved when state is enabled.

~~~yaml
resolvers:
  api_key:
    type: string
    resolve:
      with:
        - provider: parameter
          inputs:
            key: "API Key"
~~~

### Behavior

- When state is enabled, every CLI parameter passed via `-r` is recorded in the `parameters` map.
- On the next run, saved parameters are merged with the current CLI parameters (CLI values win on conflict) before resolvers execute.
- Resolvers run normally -- the `parameter` provider returns the merged (replayed) value, so the same inputs reproduce the same outputs without re-supplying them.
- New keys are added; existing keys are overwritten. Users never need to re-pass every parameter.

### Batch Save

The merged parameter set (plus any immutable values) is flushed to the backend in a single `save` call after **all** resolvers complete. This ensures:

- No partial state on failures -- if any resolver fails, state is not updated
- Minimal I/O -- one write per execution
- Consistent state -- all values reflect the same execution

---

## Backend Access Only

There is no resolver-facing `state` provider. Resolvers never read or write
state entries directly. Replay is driven entirely by the parameter set the
state manager merges before resolvers run (see [Parameter Replay](#parameter-replay)),
and immutable values are enforced by the manager after execution (see
[Immutable Resolvers](#immutable-resolvers)).

State is read and written only by the backend provider (`file`, `http`, or
`github`) via `CapabilityState` during the pre- and post-execution phases.

---

## File Provider State Operations

The built-in `file` provider supports state persistence via `CapabilityState`. State operations use `state_load`, `state_save`, and `state_delete` as the `operation` input.

### Input Schema

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `operation` | string (enum: `state_load`, `state_save`, `state_delete`) | Yes | Operation to perform |
| `path` | string | Yes | File path (relative to solution directory, or absolute) |
| `data` | object | For `state_save` | The full `Data` object to persist |

### Operations

| Operation | Behavior |
|-----------|----------|
| `state_load` | Reads JSON from the resolved path (relative to solution directory). Returns empty state structure if file does not exist (first run). |
| `state_save` | Writes `Data` as JSON to the resolved path. Creates directories as needed. Uses atomic write (temp + rename). |
| `state_delete` | Removes the state file at the resolved path. |

### Dry-Run Behavior

During dry-run: `state_load` returns empty state, `state_save` and `state_delete` report what-if actions.

## GitHub Provider State Operations

The `github` provider also supports `CapabilityState`, storing state as JSON files in a GitHub repository. The GitHub backend supports asymmetric read/write targets: loading state from one branch (e.g., `main`) and saving to another (e.g., a feature branch created by the action workflow).

### Input Schema

| Field | Type | Required For | Description |
|-------|------|-------------|-------------|
| `operation` | string (enum: `state_load`, `state_save`, `state_delete`) | All | Operation to perform |
| `owner` | string | All | Repository owner |
| `repo` | string | All | Repository name |
| `path` | string | All | File path in the repository |
| `ref` | string | `state_load` | Branch/ref to read state from (e.g., `main`). Only needed at load time. |
| `branch` | string | `state_save`, `state_delete` | Branch to write state to. Can reference a resolver via `saveOverrides`. |
| `message` | string | No | Commit message (defaults to `"chore(state): update state"`) |
| `data` | object | `state_save` | The full state data object to persist |

### Asymmetric Read/Write Configuration

The GitHub backend is designed for PR-based workflows where:

1. State is **loaded** from the default branch (`main`) -- reflecting the last merged state
2. State is **saved** to a feature branch -- alongside scaffolded files in the same PR

This is achieved using `saveOverrides`:

~~~yaml
state:
  enabled: true
  backend:
    provider: github
    inputs:
      owner: { literal: "my-org" }
      repo: { literal: "my-repo" }
      path: { expr: "'state/' + __params.app_name + '.json'" }
      ref: { literal: "main" }
    saveOverrides:
      branch: { rslvr: featureBranch }
      message: { expr: "'chore(state): save ' + __params.app_name" }
~~~

At load time: `ref` (from `inputs`) determines where to read. At save time: `branch` (from `saveOverrides`) determines where to write.

### Concurrency: `expectedHeadOid`

The GitHub `createCommitOnBranch` GraphQL mutation requires `expectedHeadOid`. The github backend fetches the current HEAD OID of the target branch immediately before committing. This serves two purposes:

1. **API requirement** -- GitHub rejects commits without a valid `expectedHeadOid`
2. **Lightweight optimistic locking** -- if a concurrent process committed to the same branch between the fetch and the commit, the mutation fails with a conflict error rather than silently overwriting

On conflict, the provider returns an error: `"state save conflict: concurrent commit on branch <name>"`. The user re-runs to pick up the latest state.

### Eventual Consistency in PR Workflows

In PR-based workflows, state on `main` is eventually consistent:

- State saved to a feature branch only reaches `main` when the PR merges
- Until merge, subsequent `state_load` reads from `main` and gets the pre-PR state
- This means immutables, fingerprints, and parameter replay reflect the last **merged** state

This is semantically correct: state reflects what is committed/deployed, not what is proposed. Actions re-execute on subsequent runs because the previous run's state has not landed on `main` yet.

### Operations

| Operation | Behavior |
|-----------|----------|
| `state_load` | Read JSON file from `owner/repo/path@ref`. Return empty state on 404 (first run). |
| `state_save` | Fetch HEAD OID of `branch`, call `createCommitOnBranch` with state JSON as file addition. Fail on OID conflict. |
| `state_delete` | Fetch HEAD OID of `branch`, call `createCommitOnBranch` with file deletion. Idempotent on missing file. |

### Dry-Run Behavior

During dry-run: `state_load` returns empty state, `state_save` and `state_delete` report what-if actions without making API calls.

### Full Workflow Example

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: deploy-infra
  version: 1.0.0
state:
  enabled: true
  backend:
    provider: github
    inputs:
      owner: { literal: "my-org" }
      repo: { literal: "infra-state" }
      path: { expr: "'state/' + __params.app_name + '.json'" }
      ref: { literal: "main" }
    saveOverrides:
      branch: { rslvr: featureBranch }
      message: { expr: "'chore(state): update ' + __params.app_name" }
spec:
  resolvers:
    appName:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "App Name"
    featureBranch:
      type: string
      resolve:
        with:
          - expr: "'scafctl/' + _.appName + '/' + string(timestamp.now())"
    clusterId:
      type: string
      immutable: true
      resolve:
        with:
          - provider: exec
            inputs:
              command: "uuidgen"
  workflow:
    actions:
      create-branch:
        provider: github
        inputs:
          operation: create_branch
          owner: { rslvr: org }
          repo: { literal: "infra-state" }
          branch: { rslvr: featureBranch }
      commit-files:
        dependsOn: [create-branch]
        provider: github
        inputs:
          operation: create_commit
          owner: { literal: "my-org" }
          repo: { literal: "infra-state" }
          branch: { rslvr: featureBranch }
          message: "feat: scaffold infrastructure"
          additions:
            - path: main.tf
              content: { rslvr: renderedTerraform }
      open-pr:
        dependsOn: [commit-files]
        provider: github
        inputs:
          operation: create_pull_request
          owner: { literal: "my-org" }
          repo: { literal: "infra-state" }
          branch: { rslvr: featureBranch }
          base: main
          title: { expr: "'feat: deploy ' + _.appName" }
~~~

Immutable locks (if any) are committed before actions run; after actions complete, the state manager saves the merged parameters as a second commit on `featureBranch`. The PR contains both the scaffolded files and the state file.

### Future Backends

The backend is a provider capability, so new backends are just providers implementing `CapabilityState`:

| Backend | Provider Name | Inputs |
|---------|---------------|--------|
| Local file (built-in) | `file` | `path` |
| GitHub repo (external) | `github` | `owner`, `repo`, `path`, `branch` |
| S3 (future) | `s3` (or plugin) | `bucket`, `key`, `region` |
| HTTP API (future) | `http` (or plugin) | `url`, `method`, `headers` |

No changes to the resolver executor or provider executor are needed to add a new backend. The state manager handles all backend-specific input resolution (including the `saveOverrides` split) transparently.

---

## State Loading Lifecycle

The `enabled` and `backend.inputs` fields are resolved using CLI parameters (`-r` flags) available as `__params` in CEL and template expressions. Resolver outputs (`_`) are only available at save time, not load time.

### Steps

1. **Parse** -- Extract `state` config from the solution.

2. **Validate** -- Ensure `state.enabled` and `state.backend.inputs` do NOT use direct resolver references (`rslvr:`). State loads before resolvers run, so resolver outputs are not available.

3. **Evaluate `enabled`** -- Resolve the `ValueRef` using CLI params (`__params`). If falsy, skip state entirely and proceed with normal stateless execution.

4. **Resolve backend inputs** -- Resolve all `ValueRef` inputs for the backend provider (e.g., the `path` template) using CLI params (`__params`). Only `inputs` are resolved at load time; `saveOverrides` are skipped.

5. **Load state** -- Call the backend provider with `operation: state_load` via `provider.Execute()` with `WithExecutionMode(ctx, CapabilityState)`. This is a standalone provider call -- completely independent of the resolver system.

6. **Capture command** -- Store the current subcommand and parameters in the `command` section of the loaded state data.

7. **Merge and inject** -- Merge the saved `parameters` from the loaded state with the current CLI parameters (CLI values win on conflict). The loaded state data is injected into `context.Context` via `state.WithState(ctx, stateData)`, while the merged parameter set is returned separately as `LoadResult.MergedParams` for the command layer to pass onward to the parameter provider.

8. **Normal execution** -- `resolver.Executor.Execute()` runs. Resolvers resolve their values from the merged (replayed) parameters via the `parameter` provider.

9. **Flush** -- After all resolvers complete, resolve both `inputs` and `saveOverrides` (merged, `saveOverrides` overrides). Persist the merged parameter set plus the locked values of any `immutable: true` resolvers, update state data, and call the backend provider with `operation: state_save`.

### Integration Point

State loading happens in the command layer (`pkg/cmd/scafctl/run/common.go`) before `executor.Execute()` is called. The `provider.Executor` is fully standalone and can be called independently of the resolver system -- this is the same pattern used by `run provider`.

### Sequence Diagram

```
+------+    +----------+    +------------+    +----------+
| CLI  |    | State    |    | Backend    |    | Resolver |
|      |    | Manager  |    | Provider   |    | Executor |
+------+    +----------+    +------------+    +----------+
   |             |                |                |
   |  run sol    |                |                |
   |------------>|                |                |
   |             |                |                |
   |             | evaluate enabled +              |
   |             | resolve backend inputs          |
   |             | (using __params from CLI)       |
   |             |                |                |
   |             | load state     |                |
   |             |--------------->|                |
   |             |  state data    |                |
   |             |<---------------|                |
   |             |                |                |
   |             | merge saved params with CLI     |
   |             | params (CLI wins), inject       |
   |             |                |                |
   |             | execute resolvers (replay via   |
   |             | parameter provider)             |
   |             |------------------------------->|
   |             |                |                |
   |             |  resolver results              |
   |             |<-------------------------------|
   |             |                |                |
   |             | save merged params + immutables |
   |             |--------------->|                |
   |             |<---------------|                |
   |             |                |                |
   |  done       |                |                |
   |<------------|                |                |
```

---

## Validation Rules

### Hard Errors

| Rule | Reason |
|------|--------|
| `state.enabled` and `state.backend.inputs` must NOT contain resolver references (`rslvr:`) | State loads before resolvers run, so resolver outputs are not available |
| `state.backend.provider` must resolve to a registered provider with `CapabilityState` | Ensures the backend is valid |
| `state.backend.saveOverrides` may contain resolver references (`rslvr:`) and `_` in CEL | These are only resolved at save time when resolver data is available |

### Lint Warnings

| Rule | Reason |
|------|--------|
| State enabled AND a resolver is marked `sensitive: true` | Its parameter value may be stored in plaintext in the state file (see [Sensitive Values](#sensitive-values)) |

---

## Sensitive Values

Resolvers can be marked `sensitive: true` (e.g., API keys, tokens). When state is enabled, the CLI parameter that feeds a sensitive resolver is stored **in plaintext** in the state file's `parameters` map.

Encryption is intentionally not used because:

- The validation application runs on a separate machine and would not have access to decryption keys
- Encryption would break the validation replay workflow

A **lint warning** (not error) is emitted when state is enabled and a resolver is marked `sensitive: true`, alerting the user that the corresponding parameter will be stored in plaintext. This is an explicit, informed decision by the solution author.

---

## CLI Commands

A `scafctl state` command group provides manual state management, mirroring the `scafctl secrets` and `scafctl config` patterns.

| Command | Description |
|---------|-------------|
| `scafctl state list --path <file>` | List all stored keys and metadata |
| `scafctl state get --path <file> --key <key>` | Get a specific value |
| `scafctl state set --path <file> --key <key> --value <value>` | Set a value manually |
| `scafctl state delete --path <file> --key <key>` | Delete a key |
| `scafctl state clear --path <file>` | Clear all values |

- `--path` is relative to the current working directory for CLI commands
- `list` and `get` support `-o table/json/yaml/quiet` via `kvx.OutputOptions`

---

## Package Layout

| Package | Purpose |
|---------|---------|
| `pkg/state/types.go` | `Config`, `Backend`, `Data`, `Entry`, `CommandInfo` types |
| `pkg/state/manager.go` | `Manager` -- orchestrates pre-execution loading, post-execution saving, context integration |
| `pkg/state/context.go` | `WithState(ctx, s)` / `FromContext(ctx)` for passing state through `context.Context` |
| `pkg/state/store.go` | `LoadFromFile()` / `SaveToFile()` for direct file I/O (used by CLI commands) |
| `pkg/state/mock.go` | Mock state for testing |
| `pkg/provider/builtin/fileprovider/file_state.go` | State operations for `file` provider (`CapabilityState`) |
| `pkg/provider/builtin/httpprovider/http_state.go` | State operations for `http` provider (`CapabilityState`) |
| (external) `github` provider | State operations for `github` provider (`CapabilityState`) -- separate repository |
| `pkg/cmd/scafctl/state/` | CLI commands (`list`, `get`, `set`, `delete`, `clear`) |

---

## Files to Modify

| File | Change |
|------|--------|
| `pkg/provider/provider.go` | Add `CapabilityState`, update `IsValid()`, add to `capabilityRequiredFields` |
| `pkg/resolver/resolver.go` | Add `Immutable bool` field to `Resolver` struct |
| `pkg/solution/solution.go` | Add `State *state.Config` field to `Solution` struct |
| `pkg/provider/builtin/builtin.go` | `file`, `http`, and `github` providers implement `CapabilityState` |
| `pkg/cmd/scafctl/run/common.go` | Integrate state loading lifecycle before `executor.Execute()` |
| `pkg/cmd/scafctl/run/solution.go` | Pass state config to common execution flow |
| `pkg/cmd/scafctl/render/solution.go` | Support state reads in render mode (writes are no-op) |
| `pkg/cmd/scafctl/root.go` | Register `scafctl state` command group |
| `docs/design/misc.md` | Revise "No persistent state between runs" -- note state is now opt-in |
| `docs/design/future-enhancements.md` | Add immutable resolver entry |

---

## Immutable Resolvers

The `immutable: true` field on the `Resolver` struct locks a resolver's resolved value permanently after the first run.

### Behavior

- On the first run, a resolver marked `immutable: true` has its resolved value written to the state file's `immutables` map
- On subsequent runs, the resolver still executes; if it produces the same value, the save is a silent no-op; if the value differs, `Save()` returns `ErrImmutableEntry` and execution fails
- The only way to change an immutable value is via `scafctl state delete` or `scafctl state clear`

### Lint Rules

| Rule | Severity | Trigger |
|------|----------|---------|
| `immutable-requires-state` | Error | `immutable: true` on a resolver but the solution has no `state` block configured |

### Example

~~~yaml
resolvers:
  cluster_id:
    type: string
    immutable: true
    resolve:
      with:
        - provider: exec
          inputs:
            command: "uuidgen"
~~~

On the first run, `exec` generates a UUID and the manager locks it in the `immutables` map. On all subsequent runs, the resolver runs again but its value is compared against the locked entry; if it differs, execution fails, guaranteeing the value never changes.

---

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| **Backend as provider capability** | All I/O stays in the provider system. State operations are merged into existing providers (`file`, `http`, `github`) via `CapabilityState`. Plugin providers can add state support to any provider. |
| **Single-layer backend model** | Backend providers (`file`, `http`, `github`) handle persistence. There is no resolver-facing state provider -- the state manager merges saved parameters before resolvers run and enforces immutables after. The backend is swappable without affecting how resolvers behave. |
| **Parameter replay over per-resolver opt-in** | The CLI parameters (`-r`) used on each run are saved and merged on the next run (CLI wins on conflict). Resolvers reproduce their outputs from the replayed parameters. State never silently replaces provider execution. |
| **`enabled` as `ValueRef`** | Dynamic state activation via CEL or templates using CLI params (`__params`). Resolver references are not supported because state loads before resolvers run. |
| **Top-level `state` field** | State is a solution-level concern, not a resolver/workflow concern. It sits alongside `spec`, `catalog`, `bundle`, and `compose`. |
| **Pre-execution in command layer** | State loading uses standalone `provider.Execute()` before `resolver.Executor.Execute()`. No changes to the resolver executor's core loop. |
| **Command capture** | Subcommand + parameters only (latest invocation, no history). Sufficient for validation replay. Solution identity comes from metadata. |
| **Sensitive plaintext + lint warning** | Encryption would break the validation workflow (remote app lacks keys). Users are explicitly warned. |
| **Batch save** | State flushed after all resolvers complete via single backend provider `save` call. No partial state on failures. |
| **Schema version** | `schemaVersion: 1` for forward-compatible format migrations. |
| **JSON format** | Aligns with the snapshot system serialization format. |
| **Local solutions allowed** | No restriction on state for non-catalog solutions -- useful for the user's own repeated executions even without external validation. |
| **Immutable enforcement** | `immutable: true` resolvers lock their value in the `immutables` map and fail the run if a later value differs. |
