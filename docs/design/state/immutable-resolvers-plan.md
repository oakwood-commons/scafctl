---
title: "Immutable Resolvers Implementation Plan"
weight: 16
---

# Immutable Resolvers Implementation Plan

This document is the implementation plan for the "Immutable Resolvers" future
enhancement described in the [state design doc](state.md#immutable-resolvers-future-enhancement).

---

## Overview

Immutable resolvers lock state values permanently after first write. Once an
entry is marked immutable, subsequent executions cannot overwrite it -- only
explicit manual deletion (`scafctl state delete` or `scafctl state clear`) can
remove it.

### Use Case

Infrastructure identifiers (cluster IDs, deployment UUIDs, tenant keys) that
must remain stable across all future runs. The first execution generates the
value; all subsequent executions reuse it without risk of accidental mutation.

### Example

~~~yaml
resolvers:
  cluster_id:
    type: string
    immutable: true
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

**First run**: `state` returns null (no state exists), `exec` generates a UUID,
`saveToState` persists it with `immutable: true`. **Subsequent runs**: `state`
returns the locked UUID, the fallback chain stops, the value cannot be overwritten.

---

## Current State of Infrastructure

The following pieces already exist:

| Component | Status | Location |
|-----------|--------|----------|
| `Entry.Immutable` field in state data schema | Done | `pkg/state/types.go` |
| `ErrImmutableEntry` sentinel error | Done | `pkg/state/errors.go` |
| Write-time immutability guard in state provider | Done | `pkg/provider/builtin/stateprovider/state.go` |
| CLI `state set` immutability check | Done | `pkg/cmd/scafctl/state/set.go` |
| JSON schema includes `"immutable": false` | Done | State files already carry this field |

What's **missing** is the connection from the resolver declaration (`immutable: true` in
YAML) to the state entry's `Immutable` flag being set during the batch save.

---

## Implementation Plan

### Phase 1: Resolver Struct Field

Add `Immutable bool` to the `Resolver` struct in `pkg/resolver/resolver.go`.

~~~go
SaveToState bool `json:"saveToState,omitempty" yaml:"saveToState,omitempty" doc:"Persist resolver result to state after execution"`
Immutable   bool `json:"immutable,omitempty"   yaml:"immutable,omitempty"   doc:"Lock state value permanently after first write (requires saveToState)"`
~~~

**Constraint**: `immutable: true` only makes sense when `saveToState: true` is
also set. If `immutable: true` without `saveToState: true`, emit a lint warning.

### Phase 2: State Manager Save Logic

Modify `state.Manager.Save()` to propagate `Immutable` from resolver config to
the `Entry`:

1. During the save loop, check if the resolver has `Immutable == true`
2. If yes, check if the entry already exists in state with `Immutable == true`
   - If it does: **error** -- the value is locked and cannot be overwritten via
     `saveToState`. The resolver should have read from state via the `state`
     provider in its fallback chain.
   - If it does not: set `Entry.Immutable = true` on the new entry
3. If no: set `Entry.Immutable = false` (current behavior, unchanged)

### Phase 3: Lint Rules

Add two new lint rules:

| Rule ID | Severity | Condition | Message |
|---------|----------|-----------|---------|
| `immutable-without-save` | Warning | `immutable: true` but `saveToState` is not `true` | `immutable has no effect without saveToState: true` |
| `immutable-no-state-read` | Warning | `immutable: true` and `saveToState: true` but resolver does not use `state` provider in its `resolve.with` chain | `immutable resolver should read from state provider to avoid re-execution` |

The second rule is a best-practice hint -- without the `state` provider in the
fallback chain, the resolver will always re-execute and attempt to overwrite.
The overwrite will be blocked, causing an error rather than gracefully reusing
the cached value.

### Phase 4: Documentation Updates

| File | Change |
|------|--------|
| `docs/design/state.md` | Update implementation status table: change "Immutable resolver support" from "Future" to "Done" |
| `docs/design/future-enhancements.md` | Add immutable resolvers entry (or mark as completed if added during this work) |
| `docs/design/resolvers.md` | Add `immutable` field to resolver field reference |

### Phase 5: MCP / Schema Updates

| File | Change |
|------|--------|
| `pkg/schema/` | Add `immutable` to resolver JSON schema if applicable |
| `pkg/mcp/tools_*.go` | No changes needed -- MCP tools work on state entries which already have the `Immutable` field |

---

## Files to Modify

| File | Change |
|------|--------|
| `pkg/resolver/resolver.go` | Add `Immutable bool` field to `Resolver` struct |
| `pkg/state/manager.go` | Propagate `Immutable` flag during `Save()`, enforce immutability check |
| `pkg/lint/rules.go` | Add `immutable-without-save` and `immutable-no-state-read` rules |
| `pkg/lint/lint.go` (or rule implementation file) | Rule logic for new lint rules |
| `docs/design/state.md` | Update status table |
| `docs/design/future-enhancements.md` | Add or update immutable resolvers entry |

---

## Behavioral Specification

### First Write (entry does not exist)

1. Resolver executes its provider chain
2. Result collected by `Save()` with `saveToState: true`
3. `Entry` created with `Immutable: true`, `Value: <result>`
4. Entry persisted to backend

### Subsequent Runs (entry exists, immutable)

**Happy path** (resolver reads from state):

1. `state` provider reads entry, returns locked value
2. Fallback chain stops (value is non-null)
3. `saveToState` collects the same value
4. `Save()` sees entry already exists with `Immutable: true` and value matches
   -- **skip update** (no error, no-op)

**Error path** (resolver does NOT read from state):

1. Resolver executes its provider chain (e.g., `exec` generates a new UUID)
2. `saveToState` collects the new value
3. `Save()` sees entry already exists with `Immutable: true` and value differs
   -- **error**: `"cannot overwrite immutable state entry %q; use 'scafctl state delete' to remove"`

### Explicit Override (CLI)

- `scafctl state delete --path <file> --key <key>` removes the entry (including
  immutable ones) -- this is already implemented
- `scafctl state clear --path <file>` removes all entries -- already implemented
- `scafctl state set --path <file> --key <key> --value <val>` is blocked for
  immutable entries -- already implemented

---

## Testing Plan

### Unit Tests

| Test | File |
|------|------|
| Resolver with `immutable: true` and `saveToState: true` -- first write sets `Entry.Immutable` | `pkg/state/manager_test.go` |
| Subsequent save with same value -- no-op (no error) | `pkg/state/manager_test.go` |
| Subsequent save with different value -- error | `pkg/state/manager_test.go` |
| `immutable: true` without `saveToState: true` -- lint warning | `pkg/lint/lint_test.go` |
| `immutable: true` without state provider in chain -- lint warning | `pkg/lint/lint_test.go` |
| Resolver struct marshals/unmarshals `immutable` field correctly | `pkg/resolver/resolver_test.go` |

### Integration Tests

| Test | File |
|------|------|
| Full solution with immutable resolver -- first run succeeds, second run reuses value | `tests/integration/` |
| Full solution with immutable resolver -- no state read, second run errors | `tests/integration/` |

---

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Users confused by "cannot overwrite" errors | Clear error message with remediation (`scafctl state delete`) |
| Immutable set accidentally on wrong resolver | Lint rule `immutable-no-state-read` warns about common misconfiguration |
| Breaking change if immutability semantics change later | `schemaVersion` in state file allows migration; `Immutable` field already exists in schema |

---

## Execution Order

1. Add `Immutable` field to `Resolver` struct
2. Update `Save()` logic in state manager
3. Add lint rules
4. Add unit tests
5. Add integration test
6. Update documentation
