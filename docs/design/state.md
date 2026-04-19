---
title: "State"
weight: 14
---

# State

## Purpose

State adds optional, per-solution persistence of resolver values across executions. It enables two primary workflows:

1. **Re-run with same data** — Execute a solution repeatedly and retain resolved values between runs without re-prompting or re-fetching.
2. **Validation replay** — A validation application can replay the exact command with the same flags and verify it produces the same results.

State is opt-in. Solutions without a `state` block behave exactly as they do today — stateless, deterministic, and self-contained. State does not change the resolver or provider execution model. It adds a persistence layer accessed exclusively through the provider system.

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
| `SaveToState` field on Resolver | Done | `pkg/resolver/resolver.go` |
| `pkg/state/` package (types, manager, context, store) | Done | `pkg/state/` |
| `file` provider state operations | Done | `pkg/provider/builtin/fileprovider/file_state.go` |
| `github` provider state operations | Done | `pkg/provider/builtin/githubprovider/github_state.go` |
| `state` resolver-facing provider | Done | `pkg/provider/builtin/stateprovider/` |
| State loading lifecycle (pre-execution) | Done | `pkg/cmd/scafctl/run/solution.go`, `resolver.go` |
| `scafctl state` CLI commands | Done | `pkg/cmd/scafctl/state/` |
| Validation rules (circular deps, sensitive warnings) | Done | `pkg/lint/` |
| Immutable resolver support | 🔮 Future | See [Immutable Resolvers](#immutable-resolvers-future-enhancement) |

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
- Managing secrets or encryption (sensitive values are stored in plaintext — see [Sensitive Values](#sensitive-values))

---

## Architecture

State uses a **two-layer model** that keeps backend persistence separate from resolver/action access:

| Layer | Provider | Capabilities | Role |
|-------|----------|-------------|------|
| Backend | `file` or `github` | `state` (+ others) | Reads/writes the state data to storage |
| Resolver/Action access | `state` | `from`, `action` | Reads/writes individual state entries |

State operations are merged into existing providers (`file`, `github`) rather than using dedicated backend providers. This means:

- The `file` and `github` providers each gained `CapabilityState` with `state_load`, `state_save`, and `state_delete` operations
- All persistence goes through the provider system -- no special-case I/O outside of providers
- Community or internal teams can implement custom backends by adding `CapabilityState` to any provider

### New Capability: `state`

A new `CapabilityState` is added to the provider capability system. This capability signals that a provider can act as a state persistence backend. It is not used by resolvers or actions directly — only by the state manager during the pre-execution and post-execution phases.

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
| `enabled` | `ValueRef` | Yes | Dynamic activation -- literal bool, CEL expression, resolver ref, or Go template |
| `backend` | `Backend` | Yes | Backend provider configuration |

### Backend Type

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | `string` | Yes | Name of a registered provider with `CapabilityState` (e.g., `"file"`) |
| `inputs` | `map[string]*ValueRef` | Yes | Provider-specific inputs — follows the same pattern as resolver provider inputs |

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
        tmpl: "deploy-app/{{ .project_name }}.json"
spec:
  resolvers:
    project_name:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "Project Name"
    api_key:
      type: string
      sensitive: true
      saveToState: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "API Key"
    cached_token:
      type: string
      resolve:
        with:
          - provider: state
            inputs:
              key: "auth_token"
              required: false
              fallback: ""
~~~

### Dynamic `enabled` Field

The `enabled` field is a `ValueRef`, which means it supports:

- **Literal**: `enabled: true`
- **CEL expression**: `enabled: { expr: "env('ENABLE_STATE') == 'true'" }`
- **Resolver reference**: `enabled: { rslvr: "use_state" }`
- **Go template**: `enabled: { tmpl: "{{ .enable_state }}" }`

When `enabled` references resolvers, those resolvers are included in the [pre-execution mini-phase](#state-loading-lifecycle).

### Dynamic Backend Inputs

Backend inputs are `ValueRef` types -- the same polymorphic type used throughout scafctl. This enables per-project state files:

~~~yaml
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path:
        tmpl: "deploy-app/{{ .project_name }}.json"
~~~

Here, `project_name` is a resolver that runs during the pre-execution mini-phase. Project A and Project B each get their own state file.

---

## State Data Schema

State is persisted as JSON. The schema includes a `schemaVersion` field for forward-compatible format migrations.

~~~json
{
  "schemaVersion": 1,
  "metadata": {
    "solution": "deploy-app",
    "version": "1.0.0",
    "createdAt": "2026-02-12T10:00:00Z",
    "lastUpdatedAt": "2026-02-12T11:30:00Z",
    "scafctlVersion": "1.5.0"
  },
  "command": {
    "subcommand": "run solution",
    "parameters": {
      "project": "foo"
    }
  },
  "values": {
    "api_key": {
      "value": "sk-abc123",
      "type": "string",
      "updatedAt": "2026-02-12T10:00:00Z",
      "immutable": false
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
| `metadata.scafctlVersion` | Version of scafctl that last wrote the state |
| `command.subcommand` | CLI subcommand used (e.g., `run solution`) |
| `command.parameters` | Key-value pairs from `--parameter` flags |
| `values` | Map of resolver name to `Entry` |

### Entry

| Field | Type | Description |
|-------|------|-------------|
| `value` | `any` | The stored resolver value |
| `type` | `string` | The resolver's declared type (string, int, float, bool, array, any) |
| `updatedAt` | `timestamp` | When this entry was last written |
| `immutable` | `bool` | Whether this entry is locked (future enhancement — see [Immutable Resolvers](#immutable-resolvers-future-enhancement)) |

### Command Capture

State stores the most recent invocation's command information — **latest only, no history**. This enables a validation application to replay the exact command:

- `command.subcommand` — the CLI subcommand (e.g., `run solution`)
- `command.parameters` — the `--parameter` key-value pairs passed via `-r` flags

Solution identity (name, version) is already in `metadata` and does not need to be duplicated in `command`.

### Storage Location

The built-in `file` provider backend stores files under `paths.StateDir()` (`$XDG_STATE_HOME/scafctl/`), which is already defined and documented in `pkg/paths/`. This is the XDG-canonical location for user-specific state data like logs, history, and session state.

On macOS: `~/.local/state/scafctl/`

---

## SaveToState on Resolvers

A new `saveToState` field on the `Resolver` struct marks a resolver's result for state persistence:

~~~yaml
resolvers:
  api_key:
    type: string
    saveToState: true
    resolve:
      with:
        - provider: parameter
          inputs:
            key: "API Key"
~~~

### Behavior

- `saveToState` defaults to `false`
- When `true`, the resolver's result is collected for state persistence after execution
- The resolver always executes its configured provider — `saveToState` does **not** cause the resolver to skip execution or read from state implicitly
- To read from state on subsequent runs, use the `state` provider explicitly (see [State Provider](#state-provider))

### Batch Save

All `saveToState` values are collected after **all** resolvers complete, then flushed to the backend in a single `save` call. This ensures:

- No partial state on failures — if any resolver fails, state is not updated
- Minimal I/O — one write per execution, not one per resolver
- Consistent state — all values reflect the same execution

---

## State Provider

The `state` provider gives resolvers and actions explicit read/write access to individual state entries. It is a separate provider from the backend — it reads/writes the in-memory state data loaded during the pre-execution phase.

### Read Mode (`from` capability)

Used by resolvers to read previously stored values:

| Input | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `key` | string | Yes | — | State entry key (typically a resolver name) |
| `required` | bool | No | `false` | If `true`, error when key is not found |
| `fallback` | any | No | `null` | Value returned when key is not found and `required` is `false` |

**First run behavior**: When no state file exists (first execution), the state provider returns `null` or `fallback` for all reads. It does not error unless `required: true`.

Example — resolver that uses state on subsequent runs:

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

On the first run, `state` returns null (no state exists), and the resolver falls through to `http`. On subsequent runs, `state` returns the cached token and the fallback chain stops. In both cases, the result is saved to state via `saveToState: true`.

### Write Mode (`action` capability)

Used by actions to explicitly write values to state:

| Input | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `key` | string | Yes | — | State entry key |
| `value` | ValueRef | Yes | — | Value to store |
| `immutable` | bool | No | `false` | Lock value permanently (future enhancement) |

Example — action that writes to state:

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

### Dependency Extraction

The `state` provider implements `ExtractDependencies` on its descriptor so the DAG builder properly orders resolvers that depend on state values.

---

## File Provider State Operations

The built-in `file` provider supports state persistence via `CapabilityState`. State operations use `state_load`, `state_save`, and `state_delete` as the `operation` input.

### Input Schema

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `operation` | string (enum: `state_load`, `state_save`, `state_delete`) | Yes | Operation to perform |
| `path` | string | Yes | File path relative to `paths.StateDir()` |
| `data` | object | For `state_save` | The full `Data` object to persist |

### Operations

| Operation | Behavior |
|-----------|----------|
| `state_load` | Reads JSON from `paths.StateDir()/<path>`. Returns empty state structure if file does not exist (first run). |
| `state_save` | Writes `Data` as JSON to `paths.StateDir()/<path>`. Creates directories as needed. Uses atomic write (temp + rename). |
| `state_delete` | Removes the state file at `paths.StateDir()/<path>`. |

### Dry-Run Behavior

During dry-run: `state_load` returns empty state, `state_save` and `state_delete` report what-if actions.

## GitHub Provider State Operations

The `github` provider also supports `CapabilityState`, storing state as JSON files in a GitHub repository.

### Input Schema

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `operation` | string (enum: `state_load`, `state_save`, `state_delete`) | Yes | Operation to perform |
| `owner` | string | Yes | Repository owner |
| `repo` | string | Yes | Repository name |
| `path` | string | Yes | File path in the repository |
| `branch` | string | No | Branch name (defaults to default branch) |
| `data` | object | For `state_save` | The full `Data` object to persist |

### Future Backends

The backend is a provider capability, so new backends are just providers implementing `CapabilityState`:

| Backend | Provider Name | Inputs |
|---------|---------------|--------|
| Local file (built-in) | `file` | `path` |
| GitHub repo (built-in) | `github` | `owner`, `repo`, `path`, `branch` |
| S3 (future) | `s3` (or plugin) | `bucket`, `key`, `region` |
| HTTP API (future) | `http` (or plugin) | `url`, `method`, `headers` |

No changes to `pkg/state/` or the core execution flow are needed to add a new backend.

---

## State Loading Lifecycle

The `enabled` and `backend.inputs` fields can reference resolvers, creating ordering dependencies. The state loading lifecycle handles this:

### Steps

1. **Parse** — Extract `state` config from the solution. Identify resolvers referenced by `state.enabled` and `state.backend.inputs` using `ValueRef.ReferencesVariable()`.

2. **Validate** -- Ensure referenced resolvers do NOT have `saveToState: true` and do NOT use the `state` provider. This prevents circular dependencies.

3. **Pre-execution mini-phase** — Execute ONLY the resolvers referenced by `enabled` and backend inputs in a temporary resolver context. These are executed using a subset call to `resolver.Executor.Execute()` with just the required resolvers.

4. **Evaluate `enabled`** — Resolve the `ValueRef`. If falsy, skip state entirely and proceed with normal stateless execution.

5. **Resolve backend inputs** — Resolve all `ValueRef` inputs for the backend provider (e.g., the `path` template).

6. **Load state** — Call the backend provider with `operation: load` via `provider.Execute()` with `WithExecutionMode(ctx, CapabilityState)`. This is a standalone provider call — completely independent of the resolver system.

7. **Capture command** — Store the current subcommand and parameters in the `command` section of the loaded state data.

8. **Inject** — Put the loaded state data into `context.Context` via `state.WithState(ctx, stateData)`.

9. **Normal execution** — `resolver.Executor.Execute()` runs. Resolvers with `saveToState: true` persist their results. The `state` provider reads/writes entries via context.

10. **Flush** — After all resolvers complete, collect results from `saveToState` resolvers, update state data, and call the backend provider with `operation: save`.

### Integration Point

State loading happens in the command layer (`pkg/cmd/scafctl/run/common.go`) before `executor.Execute()` is called. The `provider.Executor` is fully standalone and can be called independently of the resolver system — this is the same pattern used by `run provider`.

### Sequence Diagram

```
┌──────┐    ┌──────────┐    ┌────────────┐    ┌──────────┐    ┌────────────┐
│ CLI  │    │ State    │    │ Backend    │    │ Resolver │    │ State      │
│      │    │ Manager  │    │ Provider   │    │ Executor │    │ Provider   │
└──┬───┘    └────┬─────┘    └─────┬──────┘    └────┬─────┘    └─────┬──────┘
   │             │                │                │                │
   │  run sol    │                │                │                │
   ├────────────>│                │                │                │
   │             │                │                │                │
   │             │ pre-exec mini-phase             │                │
   │             │ (resolve state.enabled +        │                │
   │             │  backend inputs)                │                │
   │             ├───────────────────────────────-->│                │
   │             │<────────────────────────────────┤                │
   │             │                │                │                │
   │             │ load state     │                │                │
   │             ├───────────────>│                │                │
   │             │  state data    │                │                │
   │             │<───────────────┤                │                │
   │             │                │                │                │
   │             │ inject ctx     │                │                │
   │             ├───────────────────────────────────────────────-->│
   │             │                │                │                │
   │             │ execute resolvers               │                │
   │             ├───────────────────────────────-->│                │
   │             │                │                │   read state   │
   │             │                │                │───────────────>│
   │             │                │                │<───────────────│
   │             │                │                │                │
   │             │  resolver results               │                │
   │             │<────────────────────────────────┤                │
   │             │                │                │                │
   │             │ save state     │                │                │
   │             ├───────────────>│                │                │
   │             │<───────────────┤                │                │
   │             │                │                │                │
   │  done       │                │                │                │
   │<────────────┤                │                │                │
```

---

## Validation Rules

### Hard Errors

| Rule | Reason |
|------|--------|
| Resolvers referenced in `state.enabled` or `state.backend.inputs` must NOT have `saveToState: true` | Prevents circular dependency: state loading depends on these resolvers, but they would also write to state |
| Resolvers referenced in `state.enabled` or `state.backend.inputs` must NOT use the `state` provider | Prevents circular dependency: state must be loaded before this provider can function |
| `state.backend.provider` must resolve to a registered provider with `CapabilityState` | Ensures the backend is valid |

### Lint Warnings

| Rule | Reason |
|------|--------|
| Resolver has `sensitive: true` AND `saveToState: true` | Sensitive data will be stored in plaintext in the state file (see [Sensitive Values](#sensitive-values)) |

---

## Sensitive Values

Resolvers can be marked `sensitive: true` (e.g., API keys, tokens). When a sensitive resolver also has `saveToState: true`, the value is stored **in plaintext** in the state file.

Encryption is intentionally not used because:

- The validation application runs on a separate machine and would not have access to decryption keys
- Encryption would break the validation replay workflow

A **lint warning** (not error) is emitted when `sensitive: true` and `saveToState: true` are both set, alerting the user that sensitive data will be stored in plaintext. This is an explicit, informed decision by the solution author.

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

- `--path` is relative to `paths.StateDir()`
- All commands support `-o table/json/yaml/quiet` via `kvx.OutputOptions`

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
| `pkg/provider/builtin/githubprovider/github_state.go` | State operations for `github` provider (`CapabilityState`) |
| `pkg/provider/builtin/stateprovider/` | `state` resolver/action provider (`CapabilityFrom`, `CapabilityAction`) |
| `pkg/cmd/scafctl/state/` | CLI commands (`list`, `get`, `set`, `delete`, `clear`) |

---

## Files to Modify

| File | Change |
|------|--------|
| `pkg/provider/provider.go` | Add `CapabilityState`, update `IsValid()`, add to `capabilityRequiredFields` |
| `pkg/resolver/resolver.go` | Add `SaveToState bool` field to `Resolver` struct |
| `pkg/solution/solution.go` | Add `State *state.Config` field to `Solution` struct |
| `pkg/provider/builtin/builtin.go` | Register `state` provider; `file` and `github` providers already have `CapabilityState` |
| `pkg/cmd/scafctl/run/common.go` | Integrate state loading lifecycle before `executor.Execute()` |
| `pkg/cmd/scafctl/run/solution.go` | Pass state config to common execution flow |
| `pkg/cmd/scafctl/render/solution.go` | Support state reads in render mode (writes are no-op) |
| `pkg/cmd/scafctl/root.go` | Register `scafctl state` command group |
| `docs/design/misc.md` | Revise "No persistent state between runs" — note state is now opt-in |
| `docs/design/future-enhancements.md` | Add immutable resolver entry |

---

## Immutable Resolvers (Future Enhancement)

A future `immutable: true` field on the `Resolver` struct enables locking state values permanently after first write.

### Behavior

- When a resolver has both `immutable: true` and `saveToState: true`, the `Entry.Immutable` flag is set to `true` on first write
- On subsequent runs, any attempt to overwrite an immutable state entry is rejected with an error — both via `saveToState` auto-persistence and via the `state` provider write mode
- The only way to change an immutable value is via `scafctl state delete` or `scafctl state clear`

### Example

~~~yaml
resolvers:
  cluster_id:
    type: string
    immutable: true       # future enhancement
    saveToState: true
    resolve:
      with:
        - provider: state
          inputs:
            key: "cluster_id"
            required: false
        - provider: exec
          inputs:
            command: "uuidgen"
~~~

On the first run: `state` returns null, `exec` generates a UUID, `saveToState` persists it as immutable. On all subsequent runs: `state` returns the locked UUID, the fallback chain stops, and the value cannot be overwritten.

### Infrastructure

The `Entry.Immutable` field is included in the state data schema from day one, defaulting to `false`. Enforcement is deferred to a future release.

---

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| **Backend as provider capability** | All I/O stays in the provider system. State operations are merged into existing providers (`file`, `github`) via `CapabilityState`. Plugin providers can add state support to any provider. |
| **Two-layer model** | Backend providers (`file`, `github`) handle persistence, while the `state` provider handles resolver/action access. The backend is swappable without affecting how resolvers interact with state. |
| **No implicit state-over-provider** | `saveToState` writes to state, the `state` provider reads from state. Resolvers always execute their configured provider. State never silently replaces provider execution. |
| **`enabled` as `ValueRef`** | Dynamic state activation via CEL, resolver refs, or templates. Referenced resolvers run in the pre-execution mini-phase. |
| **Top-level `state` field** | State is a solution-level concern, not a resolver/workflow concern. It sits alongside `spec`, `catalog`, `bundle`, and `compose`. |
| **Pre-execution in command layer** | State loading uses standalone `provider.Execute()` before `resolver.Executor.Execute()`. No changes to the resolver executor's core loop. |
| **Command capture** | Subcommand + parameters only (latest invocation, no history). Sufficient for validation replay. Solution identity comes from metadata. |
| **Sensitive plaintext + lint warning** | Encryption would break the validation workflow (remote app lacks keys). Users are explicitly warned. |
| **Batch save** | State flushed after all resolvers complete via single backend provider `save` call. No partial state on failures. |
| **Schema version** | `schemaVersion: 1` for forward-compatible format migrations. |
| **JSON format** | Aligns with the snapshot system serialization format. |
| **Local solutions allowed** | No restriction on state for non-catalog solutions — useful for the user's own repeated executions even without external validation. |
| **Immutable deferred** | `Entry.Immutable` field included in schema but enforcement is not implemented. |
