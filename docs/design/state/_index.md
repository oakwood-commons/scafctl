---
title: "State"
weight: 14
---

# State

> The replay model described here is **parameter-based replay**. See
> [parameter-replay-design.md](parameter-replay-design.md) for the design
> rationale. The earlier `saveToState` field was removed before release -- it
> does not exist in the runtime. Resolvers may *read* the loaded state snapshot
> through the read-only `state` provider; writing state is exclusively the
> state manager's save targets (see
> [Read Access and Write Access](#read-access-and-write-access)).

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
resolved values are locked in state on the first run. Reading and writing are
configured separately: `state.load` only ever READS, before resolvers run, and
the `state.save` targets are the ONLY write mechanism, after a successful run.
Save targets marked `checkpoint: true` are also written before workflow actions
(run solution and run action) so locks minted this run survive a later action
failure. See
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
| Load/save config split (`state.load` + `state.save`) | Done | `pkg/state/types.go`, `pkg/state/override.go` |
| `pkg/state/` package (types, manager, context, store, override, legacy) | Done | `pkg/state/` |
| Legacy key rejection (`state.backend`, `state.emit`, `saveOverrides`) | Done | `pkg/state/legacy.go` |
| Checkpoint writes before actions | Done | `pkg/state/manager.go`, `pkg/cmd/scafctl/run/` |
| `file` provider state operations | Done | `pkg/provider/builtin/fileprovider/file_state.go` |
| `http` provider state operations | Done | `pkg/provider/builtin/httpprovider/http_state.go` |
| `github` provider state operations | External | Separate repository (not part of this project) |
| State loading lifecycle (pre-execution) | Done | `pkg/cmd/scafctl/run/solution.go`, `resolver.go` |
| `--no-state`, `--state-file`, `--state-output`, `--no-state-output`, `--allow-missing-locks` flags | Done | `pkg/cmd/scafctl/run/`, `pkg/cmd/scafctl/render/solution.go` |
| `scafctl state` CLI commands | Done | `pkg/cmd/scafctl/state/` |
| Lint rules (state.load / state.save configuration) | Done | `pkg/lint/` |
| Immutable resolver support | Done | `pkg/resolver/resolver.go` (field), `pkg/state/manager.go` (enforcement), `pkg/lint/` (rules) |

> Note: a `saveToState` resolver field appeared in an earlier draft of this
> design. It was **removed** in favor of parameter replay and is not part of
> the runtime.

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

State uses a **single-layer provider model**: persistence is a provider
capability, and the state manager drives load and save around resolver
execution. Reading is configured by `state.load`; writing is configured by the
`state.save` targets. Resolvers do not write state directly -- replay happens
through the parameter set the manager merges before resolvers run.

| Layer | Provider | Capability | Role |
|-------|----------|-----------|------|
| State provider | `file`, `http`, or `github` | `state` | Reads/writes the state data to storage |

State operations are merged into existing providers (`file`, `http`, `github`)
rather than using dedicated state providers. This means:

- The `file`, `http`, and `github` providers each implement `CapabilityState` with `state_load`, `state_save`, and `state_delete` operations
- All persistence goes through the provider system -- no special-case I/O outside of providers
- Community or internal teams can implement custom state providers by adding `CapabilityState` to any provider

### The `state` Capability

The `CapabilityState` capability signals that a provider can implement state
persistence. It is not used by resolvers or actions directly -- only by the
state manager during the pre-execution and post-execution phases.

Required output fields for `state` capability:

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | Whether the operation succeeded |

---

## Solution Configuration

State is declared via a top-level `state` field on the `Solution` struct, as a
peer to `spec`, `catalog`, `bundle`, and `compose`.

Reading and writing are configured separately: `load` only ever READS state
(before resolvers run), and `save` lists the ONLY places state is written
(after a successful run). Both are optional:

| load | save | Behavior |
|------|------|----------|
| yes  | yes  | Persist across runs (the common case -- see [Inheriting the Load Location](#inheriting-the-load-location-extends-load)) |
| yes  | no   | Read-only replay -- nothing is ever written |
| no   | yes  | Start from empty state every run, publish the result |
| no   | no   | Nothing to do (lint warning `empty-state-config`) |

### Config Type

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | `ValueRef` | No | Dynamic activation -- literal bool, CEL expression, Go template, or `rslvr:` reference to a state-independent resolver. Absent means enabled. Evaluated at load time, before the main resolver pass. Use `__params` to reference CLI parameters (e.g., `expr: "__params.enable_state == true"`) |
| `load` | `LoadConfig` | No | Where state is READ from before resolvers run. Load never writes. Without it every run starts from empty state |
| `save` | `[]SaveTarget` | No | Where state is WRITTEN after a successful run -- the only write mechanism. Each target is independent: its own provider, format, parameter narrowing, inputs, enable condition, and checkpoint flag. Without one nothing is ever saved |

### Load Configuration (`state.load`)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | `string` | Yes | Name of a registered provider with `CapabilityState` (e.g., `"file"`) |
| `inputs` | `map[string]*ValueRef` | Yes | Provider-specific inputs resolved at **load time**. May use literals, `__params` (CLI parameters), or references to state-independent resolvers -- a resolver that reads the state snapshot (via the `state` provider), or one that transitively depends on such a resolver, cannot be referenced (see [Dynamic State Configuration](#dynamic-state-configuration)) |

The load side has no `format` and no `parameters`: load tolerates any
document shape (see [Save Format](#save-format)), and format plus parameter
narrowing are save-target fields only.

### Save Targets (`state.save`)

Each entry in `save` is one independent write of the state document after a
successful run:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `extends` | `string` | No* | `load` is the only supported value: the target inherits the declared load block's provider and inputs. Mutually exclusive with `provider`; requires a `load` block. See [Inheriting the Load Location](#inheriting-the-load-location-extends-load) |
| `provider` | `string` | Yes (unless `extends`) | Name of a registered provider with `CapabilityState` (e.g., `"file"`) |
| `format` | `string` | No | Document shape this target receives: `"full"` (default) or `"intent"`. See [Save Format](#save-format) |
| `parameters` | `object` | No | Narrows which parameters an `"intent"`-format target carries (`include` / `exclude`). Lint rejects it on a `"full"` (or unset) target. See [Parameter Narrowing](#parameter-narrowing) |
| `inputs` | `map[string]*ValueRef` | No | Provider-specific inputs resolved at **save time** -- every resolver has run by then, so they may use resolver references (`rslvr:`) and `_` in CEL. They may NOT reference the state being saved (`__state`; lint `state-save-state-ref`). With `extends: load`, merged on top of the inherited load inputs |
| `enabled` | `ValueRef` | No | Per-target gate, resolved at save time (may reference any resolver). Defaults to `true` (always save) when unset |
| `checkpoint` | `bool` | No | Opt-in (default `false`, `full`-format targets only): also write this target **before** workflow actions run, locking immutable values so they survive a later action failure. See [Checkpoints](#checkpoints) |

`*` A target with neither `extends` nor `provider` is a lint error
(`missing-state-save-provider`).

### Inheriting the Load Location (`extends: load`)

Some providers need different configuration for load vs save. A GitHub
provider may read state from the `main` branch but write it to a feature
branch determined at runtime by a resolver.

With `extends: load`, a save target starts from the load block **as declared
in the solution**: it copies the load block's provider and inputs, then merges
the target's own `inputs` on top. The target wins on a key conflict, and a
nil/dangling input key never erases an inherited one. This is the replacement
for the removed `saveOverrides` field: the same "load from one place, write to
another" pattern, expressed as an independent target.

Rules:

- `load` is the only supported value (lint `invalid-state-save-extends`).
- `extends` and `provider` are mutually exclusive.
- `extends` requires a `load` block to inherit from.
- `extends` copies the load block **declared in the solution**. Replacing where
  a run reads from -- such as the CLI `--state-file` flag -- never redirects
  where an `extends` target writes (see
  [Explicit State File](#explicit-state-file---state-file)).

Example -- load from `main`, save to a feature branch:

~~~yaml
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
~~~

At load time, `ref` (from the load inputs) determines where to read. At save
time, `branch` (from the target's own inputs) determines where to write.

### Checkpoints

Save targets are written after a successful run -- by default, a failed action
means nothing was written, and any immutable value minted during that run is
lost (the next run mints a new one, orphaning whatever the failed run
created).

`checkpoint: true` (opt-in, default `false`, valid only on a `full`-format
target; lint `invalid-state-save-checkpoint`) makes `run solution` and `run
action` also write that target **before** the workflow actions run -- after
resolvers and immutable verification have succeeded -- in addition to the
final, post-success write. The checkpoint write keeps the saved parameter set
as loaded (new `-r` values are saved only by the final save) and prints
nothing. A successful run therefore writes a checkpoint target twice; for a
side-effecting provider (`github` commits, `http` requests) that is two writes
per run.

Without any checkpoint target, nothing is written when an action fails.

~~~yaml
state:
  enabled: true
  load:
    provider: file
    inputs:
      path: ".scafctl/state.json"
  save:
    - extends: load
      checkpoint: true   # locks immutable values before actions run
~~~

### Example

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
      path:
        tmpl: "deploy-app/{{ .__params.project_name }}.json"
  save:
    - extends: load
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

---

## Save Format and Projections

### Per-Target `enabled`

A solution is not limited to a single saved copy of its state; `save` is a
list, and each entry is written independently. Every target can carry its own
`enabled` condition, evaluated at **save time** -- after every resolver has
run -- so it may reference **any** resolver, with no acyclic restriction to
honor (unlike the load-time `state.enabled`, which may only reference
state-independent resolvers; see
[Dynamic State Configuration](#dynamic-state-configuration)):

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
      inputs: { path: "intent/sandbox.json" }
      # Only publish the intent for environments meant to be committed.
      enabled: { expr: "_.environment == 'sandbox'" }
~~~

Absent `enabled` defaults to `true` (always save).

This is the mechanism for keeping a **full-fidelity state file locally** (so
immutable locks and action fingerprints persist across runs) while also
publishing a **lean, human-readable, signable "intent" document** intended to
be committed -- for example by a managed pipeline that replays that intent to
regenerate trusted output. Both targets are written on every successful save
from this one solution. See
[examples/solutions/state-save/solution.yaml](https://github.com/oakwood-commons/scafctl/blob/main/examples/solutions/state-save/solution.yaml)
for a complete working example.

### Failure Semantics

There is no "primary" target: targets are written in **declaration order**. A
failure saving one target aborts the **remaining** targets but does not undo
the targets that already succeeded.

### Save Format

A save target's `format` controls what shape of the state document that target
receives at save time:

| Value | Meaning |
|-------|---------|
| `"full"` (default; empty string behaves identically) | The complete state document: `schemaVersion`, `metadata` (including the volatile `createdAt`/`lastUpdatedAt`/`runtime` fields), `command`, `parameters`, `resolvers`, `fingerprints`, `attestation`. |
| `"intent"` | The lean, replay-relevant projection: `schemaVersion`, `metadata.solution`, `metadata.version`, `parameters`, and `attestation` (when present). Omits `command`, `resolvers`, `fingerprints`, and the volatile `metadata` sub-fields. |

`format` is a save-target field only and never affects **load** -- decoding a
state document tolerates a lean document missing sections (see
[State Data Schema](#state-data-schema)), so an intent-format file loads back
cleanly regardless of which provider wrote it.

The `intent` projection is deliberately deterministic and free of timestamps:
saving the same parameters twice produces byte-identical JSON (modulo map key
ordering, which `encoding/json` already sorts), which is what makes an intent
document a stable, signable artifact -- a signature over it survives repeated,
unchanged replays without spuriously invalidating.

### The lossy shortcut, and its guardrails

An intent document carries no immutable resolver locks (they live in the
`resolvers` section, which the intent projection omits) and no action
fingerprints. Writing only intent-format targets is a valid, supported
configuration for the case where the intent document *is* the only state that
matters -- for example, a managed pipeline whose entire job is replaying a
committed intent file. But it is **lossy**, and two lint warnings flag the
configurations in which that hurts:

- `state-requires-load` (warning): the solution uses immutable resolvers or
  action fingerprints but has no `load` block -- with nothing read back, locks
  are never verified and fingerprints never match.
- `state-requires-full-save` (warning): the solution uses immutable resolvers
  or action fingerprints but no `save` target writes the `full` format -- the
  locks and fingerprints are never persisted, so the next run cannot verify
  or reuse them.

~~~yaml
# Lossy: the immutable lock is never persisted (lint: state-requires-full-save).
# From the second run on, the loaded intent carries parameters but no lock, so
# the lock-less replay guard refuses the run unless --allow-missing-locks is
# passed.
state:
  enabled: true
  load:
    provider: file
    inputs: { path: "intent.json" }
  save:
    - provider: file
      format: intent
      inputs: { path: "intent.json" }
spec:
  resolvers:
    cluster_id:
      type: string
      immutable: true
      resolve: { with: [{ provider: parameter, inputs: { key: cluster_id } }] }
~~~

The two ways to resolve the warning:

- Add a `full`-format save target alongside the intent target -- for example
  `extends: load` (the recommended pattern above) -- so the full-fidelity copy
  still persists the immutable lock.
- Remove `immutable: true` if that resolver's cross-run consistency genuinely
  does not need enforcing.

Replaying an intent document (the shape with parameters but no locks) through
a solution with immutable resolvers is additionally guarded at run time; see
[The Lock-Less Replay Guard](#the-lock-less-replay-guard---allow-missing-locks).

### Parameter Narrowing

A save target's `parameters` field narrows which saved parameters an
`"intent"`-format target projects. It is only meaningful under
`format: intent` -- `scafctl lint` rejects it on a `"full"` (or unset) target
(`invalid-state-parameter-narrowing`), because narrowing the authoritative
state document would silently drop a parameter the solution still relies on
for replay, with no error to catch it.

| Field | Type | Description |
|-------|------|--------------|
| `include` | `[]string` | Allowlist: only these parameter names are projected. Every other saved parameter is dropped. |
| `exclude` | `[]string` | Denylist: every saved parameter is projected except these names. |

`include` and `exclude` are mutually exclusive -- setting both is a lint error
(`conflicting-state-parameter-narrowing`).

**Why this exists.** An intent document is meant to be committed and signed,
so it should carry only the parameters that are actually part of the
solution's domain -- not run-control parameters like a publish/generate mode
switch, or destination coordinates for a one-off publish step. Without
narrowing, *every* saved parameter reaches the intent, including those.

**Choosing `include` vs `exclude`.** `include` is the safer default: an
unlisted parameter is silently dropped, so a parameter added to the solution
later can never leak into the committed intent by omission. `exclude` is more
practical for a solution with a large, evolving parameter surface, where
hand-maintaining an allowlist would be a constant chore -- there, denying the
small, stable set of non-domain parameters is the tractable list to keep
current.

A name in `include` that never appears in the saved parameters this run is
silently dropped, not an error -- which parameters are actually present
legitimately varies run to run (a solution may accept optional parameters).

~~~yaml
state:
  enabled: true
  load:
    provider: file
    inputs: { path: ".scafctl/state.json" }   # full local working copy
  save:
    - extends: load
    - provider: file
      format: intent
      parameters:
        include: [appName, environment, replicas]   # domain params only
      inputs: { path: "intent.json" }                # committed, signed
spec:
  resolvers:
    appName: { type: string, resolve: { with: [{ provider: parameter, inputs: { key: appName } }] } }
    environment: { type: string, resolve: { with: [{ provider: parameter, inputs: { key: environment } }] } }
    replicas: { type: string, resolve: { with: [{ provider: parameter, inputs: { key: replicas } }] } }
    # A run-control parameter that must never reach the committed intent:
    mode: { type: string, resolve: { with: [{ provider: parameter, inputs: { key: mode } }] } }
~~~

The `extends: load` target still saves `mode`; only the written
`intent.json` is narrowed to `appName`, `environment`, and `replicas`.

---

## Dynamic State Configuration

### Dynamic `enabled` Field

The `enabled` field is a `ValueRef`, which means it supports:

- **Literal**: `enabled: true`
- **CEL expression**: `enabled: { expr: "__params.enable_state == true" }`
- **Go template**: `enabled: { tmpl: "{{ .__params.enable_state }}" }`
- **Resolver reference**: `enabled: { rslvr: persistState }` -- the referenced
  resolver must be state-independent

`enabled` and the load inputs are evaluated before state is loaded, so they
may reference only state-independent resolvers (see
[State Loading Lifecycle](#state-loading-lifecycle)). CEL expressions and
templates access CLI parameters (`-r` flags) via `__params` and the Phase-A
resolver outputs via `_`.

### Dynamic Load Inputs

Load inputs are `ValueRef` types -- the same polymorphic type used throughout
scafctl. This enables per-project state files:

~~~yaml
state:
  enabled: true
  load:
    provider: file
    inputs:
      path:
        tmpl: "deploy-app/{{ .__params.project_name }}.json"
  save:
    - extends: load
~~~

Here, `project_name` is a CLI parameter passed via `-r project_name=myapp`. Project A and Project B each get their own state file.

Save-target inputs use the same `ValueRef` forms but are resolved after every
resolver has run, so they may additionally use resolver references freely
(see [Save Targets](#save-targets-statesave)).

### Bypassing State (`--no-state`)

The `run solution`, `run resolver`, `run action`, and `render solution` commands accept a `--no-state` flag that bypasses the state lifecycle entirely for a single invocation. When set, the command-layer wiring simply does not construct a `state.Manager`, so:

- `Load` is never called (no pre-execution read, no parameter replay).
- `VerifyImmutables` / immutable-lock commits are skipped.
- No save target is ever written.

Because the gate lives in the command layer (the `stateMgr` stays `nil`), no changes are required in `pkg/state`. When the solution declares a `state` block and `--no-state` is passed, a one-line stderr notice is emitted (respecting `--quiet`). Resolvers that read the `state` provider receive the provider's no-state fallback. The flag is intended for CI/offline runs; it deliberately disables immutability enforcement for that run.

`--no-state` disables state entirely and **cannot be combined** with
`--state-file`, `--state-output`, `--no-state-output`, or
`--allow-missing-locks`; combining them is an error.

### Explicit State File (`--state-file`)

The `run solution`, `run resolver`, and `run action` commands accept a
`--state-file <path>` flag that replaces the load block with a **read** of
`<path>` through the builtin `file` provider. Each of the state flags replaces
exactly one half of the configuration; the solution's own `state` block
remains the primary way to configure state.

Behavior:

- **PATH must exist.** Pointing at a missing file is an input error (exit code
  3: `--state-file: state file "PATH" does not exist`). A declared load block
  tolerates a first run (no file yet) -- an explicitly named file is expected
  to exist.
- **The declared save targets still run.** An `extends: load` target keeps
  writing the load location the **solution** declared (an override resolves
  `extends` against the declared load block before `--state-file` replaces
  it), so the path you passed in is never overwritten -- the flag redirects
  where a run reads, never where a solution's save targets write.
- **When the solution declares no `state` block**, the flag enables state for
  the run as a **read-only replay**: parameters replay from the file and
  nothing is written.
- **When the solution declares a state block**, one line on stderr reports
  what was replaced (`--state-file: reading state from X instead of the
  solution's "github" load`), so a user is never silently switched off a
  configured load.
- **The flag forces state on.** When the solution's `state.enabled` could turn
  state off (anything but absent or a literal `true`), the run still loads
  and saves, and stderr says so: `--state-file: overriding the solution's
  state.enabled; state is enabled for this run`. The condition is not
  evaluated first, because it may depend on parameters that only the file
  being read carries. `--state-output` forces state on the same way.
- **Relative paths resolve against the invoking working directory** -- for
  `--state-file`, `--state-output`, and declared state paths alike -- even
  for a bundled catalog solution, which runs with its temporary extraction
  directory as the process directory.
- It is **mutually exclusive with `--no-state`**; combining them is an error.

The old full-substitution behavior (also writing back to PATH in place, and
inheriting the declared save format) is gone. To write state at an explicit
location, combine the flag with `--state-output` (below).

The path is deliberately a flag rather than a `-r` parameter. Routing it
through a parameter (`path: { expr: "__params.statePath" }`) would merge the
plumbing key into the saved parameter set and persist it, permanently
polluting the document. The flag keeps `parameters` clean.

#### Overriding the Save Side (`--state-output`, `--no-state-output`)

`--state-output <path>` and `--no-state-output` replace the save half of the
configuration (every declared save target) for one run:

- **`--state-output PATH`** replaces every save target with a single
  **full**-format file save at PATH (never a checkpoint). It enables state
  even when the solution declares no `state` block, so a plain solution can
  publish state for a run. Combined with `--state-file`, this is the pipeline
  replay pattern -- read from a committed intent, write fresh state elsewhere:

  ~~~bash
  scafctl run solution -f ./app.yaml --state-file intent.json --state-output .state/env.json
  ~~~

- **`--no-state-output`** skips every save target. State is still loaded and
  immutable values are still verified -- useful to test a replay without
  touching any saved location.

Each replaced declaration is reported on stderr, so a user is never silently
disconnected from a configured save:

- `--state-output: writing full state to X instead of the solution's N save target(s)`
- `--no-state-output: skipping the solution's N save target(s)`

`--state-output` and `--no-state-output` are mutually exclusive.

#### The Lock-Less Replay Guard (`--allow-missing-locks`)

An intent document carries parameters but no `metadata.createdAt` and no
immutable locks. Replaying such a document through a solution with
`immutable: true` resolvers would re-derive those values and replace any
previously saved locks, so the run **fails** (exit code 3) when the loaded
document has parameters but no `metadata.createdAt` and at least one of the
solution's immutable resolvers has no lock in it. The message names the
resolvers and how to proceed:

~~~
state "intent.json" has parameters but no immutable locks: immutable
resolver(s) [clusterId] would be re-derived and any previously saved locks
replaced; re-run with --allow-missing-locks if this is intended
~~~

Passing **`--allow-missing-locks`** waives the guard; the run proceeds with a
stderr warning. `render solution` and dry runs only warn (they never save, so
they cannot replace a lock).

A genuine full-format state file always carries a creation timestamp, so it
never trips the guard -- including when a newer solution version adds its
first immutable resolver.

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

A run pointed at an intent document replays the parameters. What gets written
where is governed by the run's save half as usual: the solution's declared
save targets still run (an `extends: load` target keeps writing the
solution's declared load location, so the intent file itself is never
overwritten), `--state-output` writes the fresh full document elsewhere, and a
run with no save targets writes nothing. A pipeline can therefore replay a
committed intent and regenerate a trusted full state file without the intent
ever being expanded in place.

When a loaded state document records a `metadata.solution` or
`metadata.version` that differs from the solution being run, an **advisory
stderr warning** is emitted; it never fails the run, because an intent may
legitimately omit metadata and running a newer solution version against older
state is a normal upgrade path.

### Run-Time Feedback

The state lifecycle is not silent: `run solution`, `run resolver`, and `run
action` each print one-line stderr notices around execution (what was loaded,
what was written). This is separate from the advisory notices above (the
override notices, the `--no-state` notice, the solution/version mismatch
warning) -- those report *anomalies*; these report the *steady-state*
outcome, so state's effect on a run is never invisible.

**Load** (printed before resolver/action execution, only when a load block is
in effect):

- First run (no prior state found): `state: no prior state at <location> (first run)`
- Replay: `state: reusing <N> parameter(s) and <M> locked value(s) from <location>`

**Save** (printed once per save target actually written, after a successful
run, in declaration order):

- `state: saved <location> (<format>)`

`<location>` is the target's resolved `path` or `url` input when it has one
(a file path or a REST endpoint), otherwise a generic `<provider> provider`
label. `<format>` is the target's declared `format` (`full` or `intent`).

Two things deliberately print nothing: a checkpoint write (it is an interim
lock-in ahead of side effects, not the run's save confirmation) and a target
skipped by its own `enabled` condition (an explicitly disabled target is not
news, and reporting it would make a solution with several conditional targets
noisy on every run).

Like every other state notice, these are written to stderr via the shared
`Writer`, so they respect `--quiet` (fully suppressed) and never appear in
structured stdout (`-o json`/`-o yaml`) -- a machine consumer never has to
filter them out of the document it parses.

---

## Migrating from `state.backend`

> **Breaking change.** The single `state.backend` block and the `state.emit`
> list were replaced by `state.load` (read only) plus a `state.save` target
> list (the only write mechanism). The old keys are rejected while the
> solution is decoded, so every entrypoint -- run, render, lint, embedders --
> fails loudly with a migration hint instead of silently ignoring `state`
> altogether (an ignored `state.backend` would turn state off with no error
> at all):

~~~
unsupported state configuration: state.backend (line 7): state.backend was
split into state.load (read only) and state.save (the only write mechanism):
rename backend to load, then add `save: [{extends: load}]` to keep writing to
the same place
~~~

| Old (removed) | New |
|---------------|-----|
| `state.backend` | `state.load` plus a `state.save` target with `extends: load` |
| `state.emit` entries | Additional entries in the `state.save` list |
| `backend.saveOverrides` | A save target's own `inputs` (with `extends: load`, merged on top of the load inputs) |
| `backend.inputs` resolved at load *and* save time | Load inputs resolved at load time; save-target inputs resolved at save time |
| `backend.format` / `backend.parameters` on the load side | Per-save-target `format` / `parameters`; the load side has neither |
| Lint rules `missing-state-backend` / `invalid-state-backend` | `missing-state-load-provider` / `invalid-state-load-provider` |
| Lint rules `missing-state-emit-backend` / `invalid-state-emit-backend` | `missing-state-save-provider` / `invalid-state-save-provider` |
| Lint rule `state-save-override-state-ref` | `state-save-state-ref` |
| Lint rule `state-format-lossy-with-immutable` | `state-requires-load` and `state-requires-full-save` |
| `state: updated ...` / `state: emitted ...` notices | `state: saved <location> (<format>)` per written target |
| `<provider> backend` label | `<provider> provider` label |

Before (still decodable -- now rejected with the hint above):

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
  "resolvers": {
    "cluster_id": {
      "value": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
      "type": "string",
      "immutable": true,
      "createdAt": "2026-02-12T10:00:00Z",
      "updatedAt": "2026-02-12T10:00:00Z"
    }
  },
  "fingerprints": {
    "__fingerprint:create-files:file": {
      "value": "sha256:...",
      "updatedAt": "2026-02-12T11:30:00Z"
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
| `resolvers` | Map of persisted resolver name to `PersistedEntry`. Entries with `immutable: true` are locked values that are verified on later runs; persist-only entries (`persist: true`) are overwritten each run |
| `fingerprints` | Map of action fingerprint key (`__fingerprint:<actionName>:<type>`) to stored hash, for up-to-date checks |
| `attestation` | Optional, opaque producer attestation carried with an intent document. scafctl never interprets or verifies it; it is stored verbatim and round-tripped across load, projection (see [Save Format](#save-format)), and save so a downstream verifier can read it back unchanged. Any cryptographic signature over the document is expected to be **detached** (stored beside the file), since a signature cannot cover bytes that contain the signature. |

### PersistedEntry

| Field | Type | Description |
|-------|------|-------------|
| `value` | `any` | The persisted resolver value |
| `type` | `string` | The resolver's declared type (string, int, float, bool, array, any) |
| `immutable` | `bool` | Whether the entry is locked and verified across runs (immutable), or overwritten each run (persist-only) |
| `createdAt` | `timestamp` | When this entry was first stored. For an immutable entry this is the lock time |
| `updatedAt` | `timestamp` | When this entry was last written. For an immutable entry this equals `createdAt` |

### Command Capture

State stores the most recent invocation's command information -- **latest only, no history**. This enables a validation application to replay the exact command:

- `command.subcommand` -- the CLI subcommand (e.g., `run solution`)
- `command.parameters` -- the key-value pairs passed via `-r/--resolver` flags

Solution identity (name, version) is already in `metadata` and does not need to be duplicated in `command`.

### Storage Location

The built-in `file` provider resolves relative state paths against the current working directory (via `provider.GetWorkingDirectory`: the context working directory if set, otherwise the process CWD). This matches the CLI state commands below, so both agree on the same file. Absolute paths are used as-is.

CLI state commands (`scafctl state list`, `get`, `set`, `delete`, `clear`) resolve relative `--path` values against the current working directory too.

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

The merged parameter set (plus immutable locks and persisted resolver values)
is written to every enabled save target in a single pass only after the run
has succeeded -- after resolvers complete (`run resolver`), or after every
workflow action completes successfully (`run solution`, `run action`). This
ensures:

- No partial state on failures -- a failed resolver or action writes nothing
  (checkpoint-targeted full-format writes are the deliberate exception; see
  [Checkpoints](#checkpoints))
- Consistent state -- all targets reflect the same execution
- Per-target shaping -- each target receives the document in its own `format`

---

## Read Access and Write Access

Resolution of state is asymmetric:

- **Reads.** A resolver may *read* the loaded state snapshot through the
  read-only `state` provider. A resolver that reads state is
  **state-dependent**: it cannot be referenced by `state.enabled` or
  `state.load.inputs`, because those fields are evaluated before state is
  loaded (lint `state-ref-state-dependent`).
- **Writes.** Resolvers never write state entries directly. The only write
  path is the state manager's save targets, resolved and written after a
  successful run; save-target fields may not reference the state being saved
  (`__state`; lint `state-save-state-ref`), and the only reads/writes of state
  storage go through providers implementing `CapabilityState` during the
  pre- and post-execution phases.

---

## State Provider Load Response Contract

Every `CapabilityState` provider returns a map from `state_load`. The core state
loader interprets it as follows:

| Field | Type | Meaning |
|-------|------|---------|
| `success` | bool | Whether the operation succeeded. |
| `data` | object | The decoded/serialized `Data` document read from storage. |
| `found` | bool | Optional. Set to `false` to report that no state object exists yet (a first run). Absent defaults to `true`. |

**Reporting absence (first run).** When the backing object does not exist (a
missing file, an HTTP/GitHub 404), a provider MUST NOT return an empty or
zero-version document as if it were a real state file -- that value decodes to
`schemaVersion: 0` and would otherwise trip the schema-version floor with a
misleading "delete the state file and recreate it" error before any file
exists. Instead, report absence in one of two ways:

1. **Preferred:** set `found: false`. The loader then starts from fresh empty
   state (`state.NewData()`) without decoding or version-checking the payload.
2. **Accepted:** return `state.NewData()` (or a contentless payload -- empty
   bytes, `null`, or `{}`) as `data`. The loader treats a contentless payload as
   fresh empty state as a safety net for providers that do not set `found`.

The schema-version guard (`ErrIncompatibleSchemaVersion` /
`ErrUnsupportedSchemaVersion`) applies only to a **non-empty document that
carries real content**, so a genuine older state file is still rejected rather
than silently accepted (which would discard immutable locks).

The built-in `file` and `http` providers set `found: false` on absence.

---

## File Provider State Operations

The built-in `file` provider supports state persistence via `CapabilityState`. State operations use `state_load`, `state_save`, and `state_delete` as the `operation` input.

### Input Schema

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `operation` | string (enum: `state_load`, `state_save`, `state_delete`) | Yes | Operation to perform |
| `path` | string | Yes | File path (relative to the current working directory, or absolute) |
| `data` | object | For `state_save` | The full `Data` object to persist |

### Operations

| Operation | Behavior |
|-----------|----------|
| `state_load` | Reads JSON from the resolved path (relative to the current working directory). Reports `found: false` (fresh state) if the file does not exist (first run). |
| `state_save` | Writes `Data` as JSON to the resolved path. Creates directories as needed. Uses atomic write (temp + rename). |
| `state_delete` | Removes the state file at the resolved path. |

### Dry-Run Behavior

During dry-run: `state_load` returns empty state, `state_save` and `state_delete` report what-if actions.

## GitHub Provider State Operations

The `github` provider also implements `CapabilityState`, storing state as JSON
files in a GitHub repository. With `extends: load` it supports asymmetric
read/write targets: loading state from one branch (e.g., `main`) and saving to
another (e.g., a feature branch created by the action workflow).

### Input Schema

| Field | Type | Required For | Description |
|-------|------|-------------|-------------|
| `operation` | string (enum: `state_load`, `state_save`, `state_delete`) | All | Operation to perform |
| `owner` | string | All | Repository owner |
| `repo` | string | All | Repository name |
| `path` | string | All | File path in the repository |
| `ref` | string | `state_load` | Branch/ref to read state from (e.g., `main`). Only needed at load time. |
| `branch` | string | `state_save`, `state_delete` | Branch to write state to. Set on the save target's `inputs` (resolved at save time, so it can reference a resolver). |
| `message` | string | No | Commit message (defaults to `"chore(state): update state"`) |
| `data` | object | `state_save` | The full state data object to persist |

### Asymmetric Read/Write Configuration

The GitHub provider is designed for PR-based workflows where:

1. State is **loaded** from the default branch (`main`) -- reflecting the last merged state
2. State is **saved** to a feature branch -- alongside scaffolded files in the same PR

This is achieved with a `extends: load` save target (see
[Inheriting the Load Location](#inheriting-the-load-location-extends-load)):

~~~yaml
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
~~~

At load time, `ref` (from the load inputs) determines where to read. At save
time, `branch` (from the target's own inputs) determines where to write.

### Concurrency: `expectedHeadOid`

The GitHub `createCommitOnBranch` GraphQL mutation requires `expectedHeadOid`. The github provider fetches the current HEAD OID of the target branch immediately before committing. This serves two purposes:

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
  load:
    provider: github
    inputs:
      owner: { literal: "my-org" }
      repo: { literal: "infra-state" }
      path: { expr: "'state/' + __params.app_name + '.json'" }
      ref: { literal: "main" }
  save:
    - extends: load
      inputs:
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

Without a checkpoint, an action failure writes nothing: the `extends: load`
save target runs only after every action succeeds. To keep an immutable lock
minted this run across a later action failure on a retry-creating workflow,
mark the target `checkpoint: true` (see
[Checkpoints](#checkpoints)) -- the checkpoint commit lands on `featureBranch`
before any action runs. After actions complete, the state manager saves the
merged parameters as a second commit on `featureBranch`. The PR contains both
the scaffolded files and the state file.

### Future Providers

State is a provider capability, so new state providers are just providers implementing `CapabilityState`:

| State provider | Provider Name | Inputs |
|---------|---------------|--------|
| Local file (built-in) | `file` | `path` |
| GitHub repo (external) | `github` | `owner`, `repo`, `path`, `branch` |
| S3 (future) | `s3` (or plugin) | `bucket`, `key`, `region` |
| HTTP API (future) | `http` (or plugin) | `url`, `method`, `headers` |

No changes to the resolver executor or provider executor are needed to add a new state provider. The state manager handles all provider-specific input resolution (including the `extends: load` merge) transparently.

---

## State Loading Lifecycle

The `enabled` and `load.inputs` fields are resolved using CLI parameters (`-r`
flags) available as `__params` in CEL and template expressions, plus the
outputs of the **state-independent** resolvers referenced in those fields as
`_`. For those references, the engine first runs a minimal **Phase A** --
just the referenced resolvers and their transitive dependencies -- evaluates
the load-time fields with the results, loads state, and the Phase-A results
are reused in the main resolver pass (they are not executed twice). A resolver
that reads state (via the `state` provider) or transitively depends on
one that does cannot run in Phase A; referencing it is a circular dependency
and is rejected (lint `state-ref-state-dependent`).

### Steps

1. **Decode** -- Extract `state` config from the solution. Legacy keys
   (`state.backend`, `state.emit`, load-side `format`/`parameters`,
   `saveOverrides`) are rejected here with a migration hint (see
   [Migrating from `state.backend`](#migrating-from-statebackend)).

2. **Apply CLI overrides** -- `--state-file` replaces the load block,
   `--state-output` / `--no-state-output` replace the save targets. Each
   replacement of something the solution declared is reported on stderr.

3. **Phase A** -- Resolve the referenced state-independent resolvers, evaluate
   `enabled`, and resolve the load inputs' `ValueRef`s. If `enabled` is falsy,
   skip state entirely and proceed with normal stateless execution.

4. **Load state** -- Call the load provider with `operation: state_load` via
   `provider.Execute()` with `WithExecutionMode(ctx, CapabilityState)`. This
   is a standalone provider call -- completely independent of the resolver
   system. Absence is tolerated (fresh empty state) unless the load was
   explicit (`--state-file` requires an existing file). A loaded document with
   parameters but no immutable locks trips the lock-less replay guard when the
   solution has immutable resolvers (see
   [The Lock-Less Replay Guard](#the-lock-less-replay-guard---allow-missing-locks)).

5. **Capture command** -- Store the current subcommand and parameters in the `command` section of the loaded state data.

6. **Merge and inject** -- Merge the saved `parameters` from the loaded state with the current CLI parameters (CLI values win on conflict). The loaded state data is injected into `context.Context` via `state.WithState(ctx, stateData)`, while the merged parameter set is returned separately as `LoadResult.MergedParams` for the command layer to pass onward to the parameter provider.

7. **Normal execution** -- `resolver.Executor.Execute()` runs. Resolvers resolve their values from the merged (replayed) parameters via the `parameter` provider.

8. **Verify immutable locks** -- Resolved immutable values are checked against the loaded locks whenever the resolvers completed (even with no save target): read-only replays and `--no-state-output` runs still enforce locks. A violation aborts the run before any action runs or anything is saved.

9. **Checkpoint** -- For `run solution` / `run action`, save targets marked `checkpoint: true` are written now (after verification, before any action), keeping the saved parameter set as loaded and printing nothing.

10. **Actions** -- The workflow actions run (`run solution` / `run action` only; `run resolver` has no actions and saves right after step 8).

11. **Save** -- After a successful run, resolve every save target's fields (any resolver may be referenced, except `__state`), then write every `enabled` target in declaration order, each projected per its own `format`. A failing target aborts the remaining targets but does not undo the ones already written.

### Integration Point

State loading happens in the command layer (`pkg/cmd/scafctl/run/common.go`) before `executor.Execute()` is called. The `provider.Executor` is fully standalone and can be called independently of the resolver system -- this is the same pattern used by `run provider`.

### Sequence Diagram

```
+------+    +----------+    +----------+    +----------+
| CLI  |    | State    |    | Load     |    | Resolver |
|      |    | Manager  |    | Provider |    | Executor |
+------+    +----------+    +----------+    +----------+
   |             |                |                |
   |  run sol    |                |                |
   |------------>|                |                |
   |             |                |                |
   |             | Phase A: resolve state-independent  |
   |             | resolvers, evaluate enabled,        |
   |             | resolve load inputs (using __params)|
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
   |             |  verify immutable locks         |
   |             |  [checkpoint targets written]   |
   |             |  actions run                    |
   |             |                |                |
   |             | save: write every enabled       |
   |             | save target, in declaration     |
   |             | order, per its format           |
   |             |-----------------------------------> (save providers)
   |  done       |                |
   |<------------|                |
```

---

## Validation Rules

The load-time fields (`state.enabled`, `state.load.inputs`) and the
save-target fields have different reference rules, reflecting when each is
resolved:

- **Load time (Phase A, before state exists):** `state.enabled` and
  `state.load.inputs` may reference only **state-independent** resolvers --
  ones that do not read the state snapshot (via the `state` provider) and
  do not transitively depend on one that does. CLI parameters are available as
  `__params`.
- **Save time (after every resolver has run):** a save target's `inputs` and
  `enabled` may reference **any** resolver (`rslvr:` or `_` in CEL) -- there
  is no acyclic constraint left to honor -- and use `__params` for CLI
  parameters. They may not reference the state being saved (`__state`).
- **Defaults:** an absent `state.enabled` means enabled; absent `state.load`
  reads nothing (empty state); absent `state.save` writes nothing; a `state`
  block with neither load nor save does nothing.

### State Lint Rules

| Rule | Severity | Trigger |
|------|----------|---------|
| `missing-state-load-provider` | Error | The `state.load` block does not specify a provider |
| `invalid-state-load-provider` | Error | The `state.load` provider is not registered or lacks `CapabilityState` |
| `missing-state-save-provider` | Error | A `state.save` target specifies neither `provider` nor `extends: load` |
| `invalid-state-save-provider` | Error | A `state.save` target's provider is not registered or lacks `CapabilityState` |
| `invalid-state-save-extends` | Error | A save target's `extends` is invalid: an unsupported value, combined with `provider`, or used without a `state.load` block |
| `invalid-state-save-checkpoint` | Error | A save target sets `checkpoint: true` but does not use the `"full"` format |
| `invalid-state-format` | Error | A save target's `format` is not `full` or `intent` |
| `invalid-state-parameter-narrowing` | Error | A save target sets `parameters` narrowing but its `format` is not `"intent"` |
| `conflicting-state-parameter-narrowing` | Error | A save target's `parameters` sets both `include` and `exclude` |
| `immutable-requires-state` | Error | A resolver is `immutable: true` but the solution has no `state` block |
| `state-ref-state-dependent` | Error | `state.enabled` or `state.load.inputs` references a state-dependent resolver (circular dependency) |
| `state-ref-unknown` | Error | `state.enabled` or `state.load.inputs` references a resolver that is not defined in `spec.resolvers` |
| `state-save-state-ref` | Error | A save target's input or `enabled` condition references the state being saved (`__state`) |
| `state-requires-load` | Warning | The solution uses immutable resolvers or action fingerprints, but `state` has no `load` block |
| `state-requires-full-save` | Warning | The solution uses immutable resolvers or action fingerprints, but no save target writes the `"full"` format |
| `empty-state-config` | Warning | The `state` block configures neither `load` nor `save` |
| `immutable-without-checkpoint` | Info | The solution has immutable resolvers and workflow actions, but no save target sets `checkpoint: true` |
| `state-github-no-save-branch` | Info | A GitHub save target has no `branch` input (PR workflows usually want a resolver-derived save branch) |

Additionally, a sensitive resolver's parameter is stored in plaintext when
state is enabled -- see [Sensitive Values](#sensitive-values).

---

## Sensitive Values

Resolvers can be marked `sensitive: true` (e.g., API keys, tokens). When state is enabled, the CLI parameter that feeds a sensitive resolver is stored **in plaintext** in the state file's `parameters` map.

Encryption is intentionally not used because:

- The validation application runs on a separate machine and would not have access to decryption keys
- Encryption would break the validation replay workflow

Storing such a parameter in plaintext is an explicit, informed decision by the
solution author: be mindful of what the state file contains and where it lives,
especially for save targets meant to be committed (a narrowed intent document
can keep a runtime credential parameter out of the committed artifact --
see [Parameter Narrowing](#parameter-narrowing)).

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
| `pkg/state/types.go` | `Config`, `LoadConfig`, `SaveTarget`, `Data`, `PersistedEntry`, `CommandInfo` types |
| `pkg/state/manager.go` | `Manager` -- orchestrates pre-execution loading, immutable verification, checkpoint and post-execution saving, context integration |
| `pkg/state/override.go` | `ApplyOverrides` -- the programmatic form of the `--state-file` / `--state-output` / `--no-state-output` flags, plus `extends: load` materialization |
| `pkg/state/legacy.go` | Rejection (with migration hints) of removed state keys during solution decoding |
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

- On the first run, a resolver marked `immutable: true` has its resolved value written into the state file's `resolvers` map (with `immutable: true`)
- On subsequent runs, the resolver still executes; its value is verified against the locked entry before any action runs or anything is saved -- if it differs, execution fails, so a violated lock aborts side effects. New values are locked at save time (and by checkpoint writes, before actions)
- The only way to change an immutable value is via `scafctl state delete` or `scafctl state clear`

Immutable locks live only in a **full-format** document. A solution with immutable resolvers needs a `load` block and a full-format save target, or the locks are never persisted or verified (lint `state-requires-load` and `state-requires-full-save`).

### Lint Rules

| Rule | Severity | Trigger |
|------|----------|---------|
| `immutable-requires-state` | Error | `immutable: true` on a resolver but the solution has no `state` block configured |
| `state-requires-load` | Warning | Immutable resolvers (or action fingerprints) but no `state.load` block |
| `state-requires-full-save` | Warning | Immutable resolvers (or action fingerprints) but no `full`-format save target |
| `immutable-without-checkpoint` | Info | Immutable resolvers and workflow actions, but no `checkpoint: true` save target: a lock minted this run is only saved after every action succeeds |

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

On the first run, `exec` generates a UUID and the manager locks it in the `resolvers` map. On all subsequent runs, the resolver runs again but its value is compared against the locked entry; if it differs, execution fails, guaranteeing the value never changes.

---

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| **State as provider capability** | All I/O stays in the provider system. State operations are implemented by existing providers (`file`, `http`, `github`) via `CapabilityState`. Plugin providers can add state support to any provider. |
| **Load and save are separate** | `state.load` only ever reads (before resolvers run); `state.save` is the only write mechanism (after a successful run). One-sided configurations are meaningful: read-only replay, or fresh-state publish. Neither is a lint error, but a block with neither is (`empty-state-config`). |
| **`extends: load` over an override merge** | A target inherits the declared load block's provider and inputs, with its own inputs merged on top (target wins). One mechanism covers "save where I loaded", "load from main, save to a PR branch", and every shape in between, with no duplicated provider setup. |
| **Checkpoint before actions** | Immutable values minted during a run are at risk until the first write. `checkpoint: true` opts a full-format target in to being written before the workflow actions, so a lock survives a later action failure; the parameter set stays as loaded until the final, post-success save. |
| **Single-layer provider model** | State providers (`file`, `http`, `github`) handle persistence. There is no resolver-facing write path -- the state manager merges saved parameters before resolvers run and enforces immutables after. The provider is swappable without affecting how resolvers behave. |
| **Parameter replay over per-resolver opt-in** | The CLI parameters (`-r`) used on each run are saved and merged on the next run (CLI wins on conflict). Resolvers reproduce their outputs from the replayed parameters. State never silently replaces provider execution. |
| **`enabled` as `ValueRef`** | Dynamic state activation via CEL, templates, or state-independent resolver references. `__params` carries CLI parameters; resolver references are resolved in a Phase-A pre-load pass. |
| **Top-level `state` field** | State is a solution-level concern, not a resolver/workflow concern. It sits alongside `spec`, `catalog`, `bundle`, and `compose`. |
| **Pre-execution in command layer** | State loading uses standalone `provider.Execute()` before `resolver.Executor.Execute()`. No changes to the resolver executor's core loop. |
| **One override flag per half** | `--state-file` replaces only the load side; `--state-output`/`--no-state-output` replace only the save side. Overriding the load never redirects an `extends: load` save target, so reading from an explicit file can never clobber a solution's declared save location. |
| **Lock-less replay guard** | A document with parameters but no creation timestamp (the shape of an intent document) is refused when immutable values would be re-derived, unless `--allow-missing-locks` explicitly accepts the replacement. A full-format file always carries the timestamp, so it never trips. |
| **Command capture** | Subcommand + parameters only (latest invocation, no history). Sufficient for validation replay. Solution identity comes from metadata. |
| **Sensitive plaintext** | Encryption would break the validation workflow (remote app lacks keys). Broadcast guidance: keep non-domain parameters out of committed intent documents via `parameters.exclude`. |
| **Batch save** | State is written to every enabled save target in one pass after the run succeeds. No partial state on failures. |
| **Schema version** | `schemaVersion` exists for forward-compatible format migrations (current: 3; oldest loadable: 2). |
| **JSON format** | Aligns with the snapshot system serialization format. |
| **Local solutions allowed** | No restriction on state for non-catalog solutions -- useful for the user's own repeated executions even without external validation. |
| **Immutable enforcement** | `immutable: true` resolvers lock their value (in the `resolvers` map, flagged `immutable`) and fail the run if a later value differs. |
