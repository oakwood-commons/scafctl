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
| `--no-state` lifecycle bypass | Done | `pkg/cmd/scafctl/run/`, `pkg/cmd/scafctl/render/solution.go` |
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
| `backend` | `Backend` | Yes | The **primary** backend: the only one used for load, and always saved to |
| `emit` | `[]EmitTarget` | No | Additional **save-only** projected state emissions, each written through its own backend. See [Emit Targets](#emit-targets) |

### Backend Type

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | `string` | Yes | Name of a registered provider with `CapabilityState` (e.g., `"file"`) |
| `format` | `string` | No | Save-time projection: `"full"` (default) saves the complete state document; `"intent"` saves the lean, replay-relevant projection. Does not affect load. See [Emit Targets](#emit-targets) |
| `inputs` | `map[string]*ValueRef` | Yes | Provider-specific inputs resolved at **both** load and save time. Must only use `literal`, `__params` expressions, or templates -- resolver references (`rslvr:`) and `_` in CEL are not available at load time. **Exception:** an `Emit` target's `inputs` are save-only (like `saveOverrides`), so they may use `_` and resolver references freely. |
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

## Emit Targets

A solution is not limited to a single saved copy of its state. `Config.Emit` is
a list of additional, **save-only** projections of the same state document,
each written through its own backend -- the primary `Backend` is always the
one used for load; `Emit` targets are extra outputs produced only when saving.

This is the mechanism for keeping a **full-fidelity state file locally** (so
immutable locks and action fingerprints persist across runs) while also
publishing a **lean, human-readable, signable "intent" document** intended to
be committed -- for example by a managed pipeline that replays that intent to
regenerate trusted output:

~~~yaml
state:
  enabled: true
  backend:                                # full-fidelity primary
    provider: file
    inputs:
      path: ".scafctl/state.json"
  emit:
    - provider: file
      format: intent
      inputs:
        path: "intent/sandbox.json"
~~~

Both files are written on every save from this one solution: `.scafctl/state.json`
keeps everything (resolver locks, fingerprints, command info), while
`intent/sandbox.json` gets only `schemaVersion`, `metadata.solution`/`version`,
`parameters`, and `attestation` (see [Backend Format](#backend-format) below).

### Emit target fields

Each entry in `emit` is a `Backend` (`provider`, `format`, `inputs`,
`saveOverrides`) plus one addition:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | `ValueRef` | No | Gates this specific emission, independent of the top-level `state.enabled`. Defaults to `true` (always emit) when unset. |

`enabled` is resolved at **save time**, after every resolver has run. This
means it may reference **any** resolver -- unlike the primary `state.enabled`,
which is a load-time, pre-resolver-execution field and can only reference
state-independent resolvers (see [Dynamic `enabled` Field](#dynamic-enabled-field)).
There is no acyclic constraint to honor at save time, because every resolver
(state-dependent or not) has already produced its final value:

~~~yaml
state:
  enabled: true
  backend:
    provider: file
    inputs: { path: ".scafctl/state.json" }
  emit:
    - provider: file
      format: intent
      inputs: { path: "intent/sandbox.json" }
      # Only publish the intent for environments meant to be committed.
      enabled: { expr: "_.environment == 'sandbox'" }
~~~

### Failure semantics

A failure saving the **primary** backend aborts before any `emit` target is
attempted. A failure saving one `emit` target aborts the **remaining** `emit`
targets (in declaration order) but does **not** undo the primary save, which
has already succeeded by that point, nor any earlier `emit` targets that
already succeeded.

### Backend Format

`Backend.Format` (used by both the primary backend and each `emit` target)
controls what shape of the state document that backend receives at save time:

| Value | Meaning |
|-------|---------|
| `"full"` (default; empty string behaves identically) | The complete state document: `schemaVersion`, `metadata` (including the volatile `createdAt`/`lastUpdatedAt`/`runtime` fields), `command`, `parameters`, `resolvers`, `fingerprints`, `attestation`. |
| `"intent"` | The lean, replay-relevant projection: `schemaVersion`, `metadata.solution`, `metadata.version`, `parameters`, and `attestation` (when present). Omits `command`, `resolvers`, `fingerprints`, and the volatile `metadata` sub-fields. |

`format` never affects **load** -- decoding a state document already tolerates
a lean document missing sections (see [State Data Schema](#state-data-schema)),
so an intent-format file loads back cleanly regardless of which backend wrote
it.

The `intent` projection is deliberately deterministic and free of timestamps:
saving the same parameters twice produces byte-identical JSON (modulo map key
ordering, which `encoding/json` already sorts), which is what makes an intent
document a stable, signable artifact -- a signature over it survives repeated,
unchanged replays without spuriously invalidating.

### The lossy shortcut, and its guardrail

Setting `format: intent` directly on the **primary** backend (with no `emit`
at all) is a valid, supported shortcut for the case where the intent document
*is* the only state that matters -- for example, a managed pipeline whose
entire job is replaying a committed intent file. But it is **lossy**:
immutable resolver locks live in the `resolvers` section, which the intent
projection omits, so an immutable value's cross-run consistency silently stops
being enforced against that backend alone.

~~~yaml
# Lossy: works, but an immutable resolver's lock is never actually persisted.
state:
  enabled: true
  backend:
    provider: file
    format: intent
    inputs: { path: "intent.json" }
spec:
  resolvers:
    cluster_id:
      type: string
      immutable: true
      resolve: { with: [{ provider: parameter, inputs: { key: cluster_id } }] }
~~~

`scafctl lint` warns on this combination (`state-format-lossy-with-immutable`).
The two ways to resolve the warning:

- Keep the primary backend in the default `full` format and move
  `format: intent` to an `emit` target instead (the recommended pattern above)
  -- the full-fidelity primary still enforces the immutable lock.
- Remove `immutable: true` if that resolver's cross-run consistency genuinely
  does not need enforcing.

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

### Bypassing State (`--no-state`)

The `run solution`, `run resolver`, `run action`, and `render solution` commands accept a `--no-state` flag that bypasses the state lifecycle entirely for a single invocation. When set, the command-layer wiring simply does not construct a `state.Manager`, so:

- `Load` is never called (no pre-execution read, no parameter replay).
- `VerifyImmutables` / immutable-lock commits are skipped.
- `Save` is never called (no post-execution write).

Because the gate lives in the command layer (the `stateMgr` stays `nil`), no changes are required in `pkg/state`. When the solution declares a `state` block and `--no-state` is passed, a one-line stderr notice is emitted (respecting `--quiet`). Resolvers that read the `state` provider receive the provider's no-state fallback. The flag is intended for CI/offline runs; it deliberately disables immutability enforcement for that run.

### Explicit State File (`--state-file`)

The `run solution`, `run resolver`, and `run action` commands accept a
`--state-file <path>` flag that points the run at an explicit state file using
the builtin `file` backend. It is an **alternate input source** for state; the
solution's own `state` block remains the primary way to configure state.

Behavior:

- **When the solution declares no `state` block**, the flag enables state for
  the run. This lets a solution be driven by an external state file without
  being authored for it.
- **When the solution declares a `state` block**, the flag overrides it
  entirely (provider, format, and `emit` targets included) and a one-line
  stderr notice reports what was replaced, so a user is never silently
  switched off a configured backend (for example a `github` backend). When the
  replaced block declared `emit` targets, the notice also reports how many
  were dropped (e.g. `(2 emit target(s) dropped)`), since those targets are
  otherwise silently lost.
- The synthesized config **inherits the `format` the solution's own primary
  backend declared** (or `"full"` when the solution declares no state block),
  so a solution's chosen save shape survives being pointed at an explicit
  file. It carries **no `emit` targets**: pointing a run at an explicit file is
  a complete substitution for state, not an additional output.
- State is **read from and written back to** the path, so successive runs
  accumulate into it exactly as a solution-configured file backend would.
- It is **mutually exclusive with `--no-state`**; combining them is an error.

The path is deliberately a flag rather than a `-r` parameter. Routing it
through a parameter (`path: { expr: "__params.statePath" }`) would merge the
plumbing key into the saved parameter set and persist it, permanently
polluting the document. The flag keeps `parameters` clean.

#### Intent documents: a state file is a superset of its own inputs

A state file loads through `DecodeData`, which tolerates missing sections
(unset maps are normalized to empty) and only enforces the schema-version
guard. So a **subset** document carrying just the fields the `intent` format
produces -- `schemaVersion`, `metadata.solution`/`metadata.version`, and
`parameters` -- is a valid state input:

~~~json
{
  "schemaVersion": 3,
  "metadata": { "solution": "deploy-app", "version": "1.5.4" },
  "parameters": { "appName": "my-app", "environmentName": "sandbox" }
}
~~~

A run pointed at it via `--state-file` replays the parameters and, at save
time, **regenerates the full state document in place** (stamping runtime
metadata, timestamps, resolver locks, etc., unless the inherited format is
itself `intent`, in which case the regenerated document stays lean). No
separate "expand intent to state" step is required -- load subset, run, save.

When a loaded state document records a `metadata.solution` or
`metadata.version` that differs from the solution being run, an **advisory
stderr warning** is emitted; it never fails the run, because an intent may
legitimately omit metadata and running a newer solution version against older
state is a normal upgrade path.

### Run-Time Feedback

The state lifecycle is not silent: `run solution`, `run resolver`, and `run
action` each print a one-line stderr notice before execution (what was
loaded) and after a successful save (what was written). This is separate from
the advisory notices above (the `--no-state` notice, the `--state-file`
override notice, the solution/version mismatch warning) -- those report
*anomalies*; this reports the *steady-state* outcome, so state's effect on a
run is never invisible.

**Load** (printed once, before resolver/action execution, when state is
enabled and not skipped):

- First run (no prior state found): `state: no prior state at <location> (first run)`
- Replay: `state: reusing <N> parameter(s) and <M> locked value(s) from <location>`

**Save** (printed once per backend actually written, after a successful save):

- Primary backend: `state: updated <location> (<format>)`
- Each enabled `emit` target: `state: emitted <location> (<format>)`

`<location>` is the backend's resolved `path` or `url` input when it has one
(a file path or a REST endpoint), otherwise a generic `<provider> backend`
label. `<format>` is the backend's declared `format` (`full` or `intent`).

An `emit` target skipped by its own `enabled` condition produces **no** line:
an explicitly disabled target is not news, and reporting it would make a
solution with several conditional targets noisy on every run.

Like every other state notice, these are written to stderr via the shared
`Writer`, so they respect `--quiet` (fully suppressed) and never appear in
structured stdout (`-o json`/`-o yaml`) -- a machine consumer never has to
filter them out of the document it parses.

The `SaveImmutables` commit that runs before workflow actions (see
[Immutable Resolvers](#immutable-resolvers)) is intentionally **not**
reported: it is an interim lock-in ahead of side effects, not the run's
user-facing save confirmation, which is reported once from the post-action
`SaveParams` commit. For the same reason, `SaveImmutables` never writes
`emit` targets -- only the primary backend. A side-effecting emit backend
(e.g. an HTTP endpoint) is written **at most once per run**, from the final
commit; writing it from the interim commit too would double-publish on
success and publish an intent for a run whose actions later fail.


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
  },
  "attestation": {
    "principal": "svc-deployer",
    "issuer": "https://issuer.example.com",
    "digest": "sha256:..."
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
| `attestation` | Optional, opaque producer attestation carried with an intent document. scafctl never interprets or verifies it; it is stored verbatim and round-tripped across load, projection (see [Backend Format](#backend-format)), and save so a downstream verifier can read it back unchanged. Any cryptographic signature over the document is expected to be **detached** (stored beside the file), since a signature cannot cover bytes that contain the signature. |

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

## State Backend Load Response Contract

Every `CapabilityState` backend returns a map from `state_load`. The core state
loader interprets it as follows:

| Field | Type | Meaning |
|-------|------|---------|
| `success` | bool | Whether the operation succeeded. |
| `data` | object | The decoded/serialized `Data` document read from storage. |
| `found` | bool | Optional. Set to `false` to report that no state object exists yet (a first run). Absent defaults to `true`. |

**Reporting absence (first run).** When the backing object does not exist (a
missing file, an HTTP/GitHub 404), a backend MUST NOT return an empty or
zero-version document as if it were a real state file -- that value decodes to
`schemaVersion: 0` and would otherwise trip the schema-version floor with a
misleading "delete the state file and recreate it" error before any file
exists. Instead, report absence in one of two ways:

1. **Preferred:** set `found: false`. The loader then starts from fresh empty
   state (`state.NewData()`) without decoding or version-checking the payload.
2. **Accepted:** return `state.NewData()` (or a contentless payload -- empty
   bytes, `null`, or `{}`) as `data`. The loader treats a contentless payload as
   fresh empty state as a safety net for backends that do not set `found`.

The schema-version guard (`ErrIncompatibleSchemaVersion` /
`ErrUnsupportedSchemaVersion`) applies only to a **non-empty document that
carries real content**, so a genuine older state file is still rejected rather
than silently accepted (which would discard immutable locks).

The built-in `file` and `http` backends set `found: false` on absence.

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
| `state_load` | Reads JSON from the resolved path (relative to solution directory). Reports `found: false` (fresh state) if the file does not exist (first run). |
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
| `state_load` | Read JSON file from `owner/repo/path@ref`. Report `found: false` (fresh state) on 404 (first run). |
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
