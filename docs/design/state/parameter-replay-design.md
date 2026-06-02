---
title: "Parameter-Based Replay Design"
weight: 18
---

# Parameter-Based Replay Design

Related: [state.md](state.md) (original state design),
[immutable-resolvers-plan.md](immutable-resolvers-plan.md)

---

## 1. Motivation

The current state system persists **resolver output values** and requires
solution authors to manually wire a `state` provider fallback chain into every
resolver that should be replayed. This is verbose, error-prone, and
conceptually redundant for scaffolding workflows where the same inputs should
always produce the same outputs.

### Problems with the Current Approach

1. **Boilerplate**: Every "replayable" resolver needs three things:
   `saveToState: true`, a `state` provider step at the start of the chain, and
   `required: false` / `onError: continue` wiring. This is mechanical and easy
   to forget.

2. **Breaks determinism contract**: Saving resolver *outputs* implies that
   re-running with the same inputs might produce different outputs. For
   scaffolding, this should not be the case -- the same inputs should always
   produce the same resolver set.

3. **Unnecessary complexity**: The `state` provider, `saveToState` field,
   circular dependency lint rules, and fallback chain patterns exist solely to
   support a replay model that is more complex than needed.

4. **User confusion**: Users must understand the difference between "the state
   provider reads from state" and "saveToState writes to state" -- two separate
   mechanisms that only make sense together.

### Core Insight

**scafctl scaffolding is a deterministic process.** The exact same input
parameters, when run with a solution's workflow, should create the exact same
resolver set with the same values and scaffold the exact same artifacts. The
replay backbone should therefore be the **input parameters**, not the resolver
outputs.

---

## 2. New Design: Parameter-Based Replay

### Principles

1. **Parameters are the source of truth** -- CLI `-r` values drive everything.
2. **State saves parameters, not resolver values** -- The state file records
   what was passed in, enabling parameter-free re-runs.
3. **Merge semantics** -- New params merge into saved params (add new keys,
   overwrite existing). Users never need to re-pass every parameter.
4. **Immutable resolvers are the exception** -- Only resolvers marked
   `immutable: true` have their *resolved values* saved to state, because
   their outputs are intentionally non-deterministic (e.g., `uuidgen`) and
   must be locked after first generation.

### Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Replay backbone | Input parameters (`-r` values) | Deterministic: same inputs = same outputs |
| State storage | Merged parameter set + immutable values | Minimal state surface; parameters are cheap to store |
| Merge strategy | CLI params override saved params; new keys are added | Users can change one value without re-specifying all |
| `saveToState` field | **Removed** | No longer needed -- parameters auto-persist |
| `state` provider | **Removed** | Resolvers no longer read from state directly |
| `immutable` field | **Retained** (standalone) | Locks non-deterministic resolver values after first run |
| Determinism assumption | Explicit | Documented contract: same params = same resolvers |
| Backward compatibility | Breaking change | State is pre-release; no existing users to migrate |

---

## 3. State Data Schema

~~~json
{
  "schemaVersion": 1,
  "metadata": {
    "solution": "deploy-app",
    "version": "1.0.0",
    "createdAt": "2026-02-12T10:00:00Z",
    "lastUpdatedAt": "2026-05-27T14:30:00Z",
    "scafctlVersion": "1.8.0"
  },
  "command": {
    "subcommand": "run solution",
    "parameters": {
      "project": "foo",
      "region": "us-east-1"
    }
  },
  "parameters": {
    "project": "foo",
    "region": "us-east-1",
    "environment": "production"
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
| `schemaVersion` | Format version (now `2`) |
| `metadata.*` | Solution identity and timestamps (unchanged) |
| `command` | Most recent invocation info (unchanged) |
| `parameters` | **New**: The merged parameter set used for replay |
| `immutables` | **New**: Locked resolver values that must not change |

### Removed Fields

| Field | Reason |
|-------|--------|
| `values` | Replaced by `parameters` + `immutables` |

---

## 4. Execution Flow

### 4.1 First Run

~~~
User: scafctl run solution -f app.yaml -r project=foo -r region=us-east-1
~~~

1. **Load state** -- Backend returns empty state (no file exists).
2. **Merge parameters** -- CLI params are the only source: `{project: foo, region: us-east-1}`.
3. **Execute resolvers** -- All resolvers execute with the merged params available via the `parameter` provider.
4. **Check immutables** -- For each resolver with `immutable: true`, save its resolved value to `immutables`.
5. **Save state** -- Write `parameters` (the merged set) and `immutables` to the backend.

### 4.2 Subsequent Run (No Parameters)

~~~
User: scafctl run solution -f app.yaml
~~~

1. **Load state** -- Backend returns saved state with `parameters: {project: foo, region: us-east-1}`.
2. **Merge parameters** -- No CLI params provided. Effective params = saved params.
3. **Execute resolvers** -- Same params as last run; deterministic resolvers produce same values.
4. **Check immutables** -- Immutable values match saved values (no error).
5. **Save state** -- No change (parameters unchanged, immutables unchanged).

### 4.3 Subsequent Run (Partial Override)

~~~
User: scafctl run solution -f app.yaml -r region=eu-west-1
~~~

1. **Load state** -- Backend returns `parameters: {project: foo, region: us-east-1}`.
2. **Merge parameters** -- CLI `region=eu-west-1` overrides saved. Effective: `{project: foo, region: eu-west-1}`.
3. **Execute resolvers** -- Runs with updated params.
4. **Check immutables** -- Immutable values verified.
5. **Save state** -- Write updated `parameters: {project: foo, region: eu-west-1}`.

### 4.4 Subsequent Run (New Parameter)

~~~
User: scafctl run solution -f app.yaml -r environment=production
~~~

1. **Load state** -- Backend returns `parameters: {project: foo, region: eu-west-1}`.
2. **Merge parameters** -- New key added. Effective: `{project: foo, region: eu-west-1, environment: production}`.
3. **Execute resolvers** -- Runs with expanded params.
4. **Save state** -- Write `parameters: {project: foo, region: eu-west-1, environment: production}`.

### 4.5 Immutable Violation

~~~
User changes solution to produce different cluster_id value
~~~

1. **Load state** -- Backend returns saved immutable `cluster_id: "a1b2..."`.
2. **Execute resolvers** -- `cluster_id` resolver produces `"x9y8..."`.
3. **Check immutables** -- Mismatch detected.
4. **Error** -- `"immutable resolver 'cluster_id' produced value different from state; use 'scafctl state delete' to remove it first"`.

---

## 5. Parameter Merge Logic

~~~go
// MergeParameters combines saved parameters from state with CLI parameters.
// CLI parameters take precedence (overwrite). New CLI keys are added.
// Returns the effective parameter set for this execution.
func MergeParameters(saved map[string]any, cli map[string]any) map[string]any {
    merged := make(map[string]any, len(saved)+len(cli))
    for k, v := range saved {
        merged[k] = v
    }
    for k, v := range cli {
        merged[k] = v // CLI overrides saved
    }
    return merged
}
~~~

The merge happens **after** state load and **before** resolver execution. The
merged map is what gets passed to `executeResolvers()` as the `params` argument.

---

## 6. Immutable Resolver Behavior

### Declaration

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

Note: No `saveToState`, no `state` provider step. The `immutable` field alone
is sufficient.

### Lifecycle

1. **First run**: Resolver executes normally. After all resolvers complete, the
   state manager checks each resolver with `immutable: true`. If no prior value
   exists in `immutables`, the resolved value is saved.

2. **Subsequent runs**: After resolvers execute, the state manager compares the
   resolved value against `immutables[name].value`. If they match, no action.
   If they differ, execution fails with an error.

3. **Override**: The user can run `scafctl state delete --key cluster_id` to
   remove the immutable lock, then re-run to generate a new value.

### Why Post-Execution Check?

The immutable check happens *after* resolver execution rather than injecting
the saved value *before* execution because:

- The resolver's configured providers always run (no implicit short-circuiting).
- The comparison verifies determinism: if the resolver produces the same value
  as stored, everything is consistent. If it produces a different value, that
  signals an unexpected environmental change that should be surfaced as an error.
- This avoids needing a mechanism to inject values into the resolver system
  from state (which was the `state` provider's job in the old design).

### Alternative: Pre-Execution Injection

An alternative approach would be to inject immutable values *before* resolver
execution, skipping the resolver entirely on subsequent runs:

~~~
1. Load state
2. For each immutable resolver with a saved value, inject into resolver context
3. Execute remaining resolvers (immutables skipped)
4. No post-execution check needed
~~~

**Trade-offs**:

| Approach | Pros | Cons |
|----------|------|------|
| Post-execution check | Verifies determinism; resolver always runs; simpler injection | Wasted work re-executing immutable resolvers; errors late |
| Pre-execution injection | No wasted work; errors early (never); immutable resolvers never call providers | Cannot detect environmental drift; need resolver skip mechanism |

**Recommendation**: Start with post-execution check (simpler to implement,
validates determinism). Consider pre-execution injection as a performance
optimization in a follow-up if immutable resolvers are expensive.

---

## 7. Removed Components

### 7.1 `state` Provider (pkg/provider/builtin/stateprovider/)

**Removed entirely.** Resolvers no longer read from state. The parameter
provider + merged params handle replay automatically.

### 7.2 `saveToState` Field (pkg/resolver/resolver.go)

**Removed.** There is no longer a concept of "opt-in to saving resolver values."
Parameters auto-persist. Immutable values auto-persist based on the `immutable`
flag alone.

### 7.3 Lint Rules

| Rule | Status |
|------|--------|
| `sensitive-state` | **Removed** -- no resolver values stored in state |
| `state-circular-dependency` | **Removed** -- no `saveToState` to create cycles |
| `immutable-without-save` | **Removed** -- `saveToState` no longer exists |
| `immutable-no-state-read` | **Removed** -- `state` provider no longer exists |
| `state-resolver-ref` | **Retained** -- state backend inputs still cannot use `rslvr:` |
| `immutable-requires-state` | **New** -- resolver with `immutable: true` requires a `state` block on the solution |

### 7.4 New Lint Rule: `immutable-requires-state`

If any resolver has `immutable: true` but the solution does not have a `state`
block configured and enabled, emit an error:

~~~
Error [immutable-requires-state] resolvers.cluster_id:
  resolver 'cluster_id' has immutable: true but no state block is configured.
  Immutable values require state persistence to enforce the lock across runs.
  Add a state block with a backend provider to the solution.
~~~

Severity: **Error** (not just warning) because without state, the immutable
contract cannot be honored -- the value would be regenerated every run.

### 7.4 Concepts

The `state-persistence` concept explanation must be rewritten to describe
parameter-based replay instead of the `saveToState` + `state` provider pattern.

---

## 8. Updated CLI Commands

### `scafctl state list`

Shows saved parameters and immutable values:

~~~
$ scafctl state list --path app-state.json

PARAMETERS
  project      foo
  region       eu-west-1
  environment  production

IMMUTABLES
  cluster_id   a1b2c3d4-e5f6-7890-abcd-ef1234567890  (string, locked 2026-02-12)
~~~

### `scafctl state get`

Gets a specific parameter or immutable value:

~~~
$ scafctl state get --path app-state.json --key region
eu-west-1

$ scafctl state get --path app-state.json --key cluster_id
a1b2c3d4-e5f6-7890-abcd-ef1234567890
~~~

### `scafctl state set`

Sets or overrides a parameter value in state (without running the solution):

~~~
$ scafctl state set --path app-state.json --key region --value us-west-2
~~~

For immutable values, `set` is blocked unless `--force` is used:

~~~
$ scafctl state set --path app-state.json --key cluster_id --value new-id
Error: cannot overwrite immutable entry "cluster_id"; use --force or delete first

$ scafctl state set --path app-state.json --key cluster_id --value new-id --force
~~~

### `scafctl state delete`

Deletes a parameter or immutable value:

~~~
$ scafctl state delete --path app-state.json --key region

$ scafctl state delete --path app-state.json --key cluster_id
Warning: 'cluster_id' is an immutable resolver value. Deleting it will cause
the resolver to regenerate a new value on the next run, which may produce a
different result. Are you sure? [y/N]
~~~

When deleting an immutable value, a confirmation warning is displayed to ensure
the user understands the implications. The `--force` flag bypasses the prompt.
Deleting an immutable value unlocks it -- the next run will regenerate and
re-lock it.

### `scafctl state clear`

Clears all parameters and immutables (full reset):

~~~
$ scafctl state clear --path app-state.json
~~~

---

## 9. Solution Configuration Changes

The top-level `state` block is **unchanged** in structure:

~~~yaml
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: "app-state.json"
~~~

The semantics change: state now stores parameters and immutables instead of
arbitrary resolver values.

The `state.backend.inputs` can still use `__params` in CEL/template
expressions. The "bootstrap parameter" pattern still applies -- at least one
parameter identifying the state file must always be passed via CLI:

~~~yaml
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path:
        tmpl: "deploy/{{ .__params.project }}.json"
~~~

Here, `project` must always be passed via `-r project=foo` because the state
file path depends on it. This is the same constraint as the current design.

---

## 10. Example: Before and After

### Before (Current Design)

~~~yaml
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

    region:
      type: string
      saveToState: true
      resolve:
        with:
          - provider: state
            inputs:
              key: "region"
              required: false
          - provider: parameter
            inputs:
              key: "region"
          - provider: static
            inputs:
              value: "us-east-1"
~~~

### After (New Design)

~~~yaml
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
~~~

The `state` provider steps and `saveToState` are gone. Replay works
automatically because saved parameters feed into the `parameter` provider.

### Immutable Example

~~~yaml
spec:
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

No `saveToState`, no `state` provider. The `immutable` flag causes the value
to be saved and locked automatically.

---

## 11. Impact on Fingerprinting

The fingerprint system ([fingerprinting.md](fingerprinting.md),
[fingerprint-input-hashing-plan.md](fingerprint-input-hashing-plan.md)) stores
hashes in the state file under `__fingerprint:*` keys. This system is
orthogonal to parameter-based replay:

- Fingerprint keys continue to live in the state data (under a reserved
  namespace that does not collide with `parameters` or `immutables`).
- The fingerprint input hashing plan remains valid -- it hashes *resolved
  action inputs* (post-resolver), not raw CLI parameters.
- No changes to `pkg/fingerprint/` are required.

The state `Data` struct should include a `Fingerprints` section (or keep the
existing `Values` map solely for fingerprint entries with reserved `__` prefix
keys).

---

## 12. Migration Strategy

State is in pre-release development with zero users. There is no existing state
data to migrate. The schema starts at version 1 with the new `parameters` +
`immutables` structure.

The previous `values`-based prototype code is removed entirely. No migration
logic is needed.

---

## 13. Task Breakdown

| # | Task | File(s) | Size | Depends On |
|---|------|---------|------|------------|
| 1 | Update `Data` struct: add `Parameters`, `Immutables`; remove `Values` | `pkg/state/types.go` | S | -- |
| 2 | Add `MergeParameters()` function | `pkg/state/merge.go` | S | -- |
| 3 | Rewrite `Manager.Load()`: load state, merge params | `pkg/state/manager.go` | M | 1, 2 |
| 4 | Rewrite `Manager.Save()`: save merged params + immutables | `pkg/state/manager.go` | M | 1 |
| 5 | Add immutable checking logic (post-execution) | `pkg/state/immutable.go` | M | 1 |
| 6 | Remove `SaveToState` field from `Resolver` struct | `pkg/resolver/resolver.go` | S | -- |
| 7 | Delete `pkg/provider/builtin/stateprovider/` | entire directory | S | -- |
| 8 | Remove state provider registration | `pkg/provider/builtin/builtin.go` | S | 7 |
| 9 | Update run commands: pass merged params, call immutable check | `pkg/cmd/scafctl/run/*.go` | M | 3, 4, 5 |
| 10 | Update `scafctl state` CLI commands for new schema | `pkg/cmd/scafctl/state/*.go` | M | 1 |
| 11 | Remove/update lint rules | `pkg/lint/lint.go`, `pkg/lint/rules.go` | M | 6, 7 |
| 12 | Update concepts | `pkg/concepts/builtin.go` | S | All |
| 13 | Add `immutable-requires-state` lint rule | `pkg/lint/lint.go`, `pkg/lint/rules.go` | S | 6 |
| 14 | Update MCP state tools | `pkg/mcp/tools_state.go` | M | 1, 10 |
| 15 | Unit tests: merge, immutable check, manager | `pkg/state/*_test.go` | M | 2, 3, 4, 5 |
| 16 | Unit tests: run commands with param replay | `pkg/cmd/scafctl/run/*_test.go` | M | 9 |
| 17 | Integration test: full replay flow | `tests/integration/` | M | 9 |
| 18 | Update state tutorial | `docs/tutorials/state-tutorial.md` | M | All |
| 19 | Update state design doc | `docs/design/state/state.md` | M | All |
| 20 | Handle fingerprint data in new schema | `pkg/fingerprint/state.go` | S | 1 |

---

## 14. Risks and Edge Cases

| Risk | Mitigation |
|------|-----------|
| **Non-deterministic resolvers without `immutable`** | Document: if a resolver is non-deterministic and the user wants its value locked, use `immutable: true`. Otherwise, it re-executes fresh each run. |
| **Bootstrap parameter (chicken-and-egg)** | At least one param identifying the state file must always be passed via CLI. This is unchanged from the current design. Document clearly. |
| **Large parameter sets** | Parameters are typically key-value strings. Even hundreds of params are small. Not a concern. |
| **Parameter type loss** | CLI params are strings. The parameter provider already handles type parsing (bool, int, array). Saved params preserve the raw string. No change in behavior. |
| **`state set` ambiguity** | The `set` command should clarify whether it sets a parameter or an immutable value. Default to parameter; use `--immutable` flag for immutables. |
| **Render command** | `scafctl render` should load saved params for replay but NOT save (read-only). Same as current state read-only behavior for render. |
| **Existing solutions using `state` provider** | Breaking change. Lint will error on unknown provider. Migration guide needed. |
| **Fingerprint keys** | Keep fingerprint data in a separate `fingerprints` map in the state struct, not mixed with parameters. |

---

## 15. Non-Goals

- **Parameter history** -- No tracking of previous parameter values. Only the
  latest merged set is stored.
- **Parameter validation** -- State does not validate that saved parameters are
  still valid for the current solution version. If a resolver's parameter key
  name changes, the old saved param is harmlessly ignored.
- **Selective parameter save** -- All parameters are always saved. No opt-out
  mechanism for individual params.
- **Cached resolver values** (non-immutable) -- Not providing a "cache resolver
  output but allow refresh" mechanism. Use `immutable` for lock-forever, or
  accept re-execution for non-deterministic resolvers.
- **Encryption** -- Parameters (including sensitive ones) are stored in
  plaintext. Same trade-off as the current design for validation replay.

---

## 16. Open Questions

1. **Should `scafctl state set` set parameters or immutables by default?**
   Recommendation: parameters by default, `--immutable` flag for immutables.

2. **Should fingerprint data live in the same state file or a separate file?**
   Recommendation: same file, under a dedicated `fingerprints` key. Keeps the
   single-file-per-solution model.

3. **Pre-execution vs post-execution immutable check?**
   Recommendation: start with post-execution (simpler). Add pre-execution
   injection as a performance follow-up if needed.

4. **Should there be a `--no-replay` flag to ignore saved parameters?**
   Recommendation: yes. Allows a "fresh run" that only uses CLI params without
   loading from state. Analogous to `--no-cache` for fingerprints.
