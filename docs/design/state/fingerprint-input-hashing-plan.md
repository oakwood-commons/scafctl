---
title: "Fingerprint Input Hashing Implementation Plan"
weight: 17
---

# Fingerprint Input Hashing Implementation Plan

Related: [fingerprinting.md](fingerprinting.md) (original design),
[#241](https://github.com/oakwood-commons/scafctl/issues/241) (state persistence)

---

## 1. Problem Statement

The current fingerprint system only hashes **file contents** matched by
`sources` and `generates` glob patterns. Resolver outputs -- which are the
dynamic data flowing into actions (template variables, CEL expressions,
provider configs) -- are invisible to the fingerprint check.

When a resolver value changes but the source template files are untouched, the
staleness check sees "same files, same outputs" and **incorrectly skips the
action**.

### Reproduction Scenario

~~~yaml
resolvers:
  environment:
    value: "staging"

workflow:
  actions:
    generate-config:
      provider: go-template
      sources:
        - "templates/config.yaml.tpl"
      generates:
        - "output/config.yaml"
      inputs:
        data:
          expr: "_.environment"
~~~

1. First run: action executes, fingerprints stored. Output contains `staging`.
2. User changes resolver `environment` to `"production"`.
3. Second run: `templates/config.yaml.tpl` unchanged (sources hash matches),
   `output/config.yaml` unchanged (generates hash matches).
4. **Result**: Action incorrectly skipped. Output still contains `staging`.

### Root Cause

The fingerprint `Check()` runs **before** `resolveInputs()` in the executor
(line 554 of `executor.go`). Resolved input values are not available at check
time, and even if they were, the `Check()` method only receives file glob
patterns -- not resolver data.

---

## 2. Solution: Two-Phase Fingerprint Check

Split the fingerprint check into two phases:

1. **Phase 1 (pre-resolve)**: Check `sources` + `generates` file hashes. If
   files changed, skip input hashing and re-run immediately (cheap fast path).
2. **Phase 2 (post-resolve)**: If files match, hash all `resolvedInputs` and
   compare against stored inputs hash. If inputs changed, re-run.

This follows the approach used by Bazel, Nx, and Nix -- every input that
influences the output is part of the cache key.

### Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Check strategy | Two-phase (files first, then inputs) | Avoids unnecessary input resolution cost when files alone prove staleness |
| Input scope | All resolved inputs | Simple, correct. Static input changes (YAML edits) also trigger re-run correctly |
| Non-deterministic inputs | No special handling | Documented limitation. If inputs contain timestamps/UUIDs, action always re-runs |
| State migration | Force re-run on missing inputs hash | Safe one-time cost. Missing `__fingerprint:*:inputs` key treated as first run |
| Serialization | `encoding/json.Marshal` (sorted keys) | Deterministic by default for Go maps. No custom sorting needed |

---

## 3. Updated Staleness Matrix

| Sources | Generates | Inputs | Result | Reason |
|---------|-----------|--------|--------|--------|
| No prior state | -- | -- | **Stale** | first run |
| Changed | -- | -- | **Stale** | sources changed |
| Match | Missing | -- | **Stale** | generates missing |
| Match | Changed | -- | **Stale** | generates modified |
| Match | Match | No prior state | **Stale** | first run |
| Match | Match | Changed | **Stale** | inputs changed |
| Match | Match | Match | **Skip** | up-to-date |

---

## 4. Updated Evaluation Order in Executor

~~~
1. Check when: condition         --> skip with SkipReasonCondition if false
2. Fingerprint Phase 1 (files)   --> if stale, proceed to step 3
                                     if fresh, proceed to step 3 then check Phase 2
3. Resolve inputs
4. Fingerprint Phase 2 (inputs)  --> skip with SkipReasonUpToDate if up-to-date
5. Execute action
6. On success: record fingerprints (sources + generates + inputs)
~~~

---

## 5. Task Breakdown

| # | Task | File(s) | Size | Depends On |
|---|------|---------|------|------------|
| 1 | Add `ReasonInputsChanged` constant | `pkg/fingerprint/fingerprint.go` | S | -- |
| 2 | Add `InputsHash` / `PreviousInputsHash` to `Result` | `pkg/fingerprint/fingerprint.go` | S | -- |
| 3 | Add `HashInputs(inputs map[string]any) (string, error)` | `pkg/fingerprint/hash.go` | S | -- |
| 4 | Add `inputsKey()` / `LoadInputsHash()` / update `SaveHashes()` | `pkg/fingerprint/state.go` | S | -- |
| 5 | Split `Check()` into `CheckFiles()` + `CheckInputs()` | `pkg/fingerprint/fingerprint.go` | M | 1--4 |
| 6 | Restructure executor: two-phase fingerprint integration | `pkg/action/executor.go` | M | 5 |
| 7 | Update `Record()` to also store inputs hash | `pkg/fingerprint/fingerprint.go` | S | 3, 4 |
| 8 | Unit tests for `HashInputs` | `pkg/fingerprint/hash_test.go` | M | 3 |
| 9 | Unit tests for `CheckInputs` + two-phase flow | `pkg/fingerprint/fingerprint_test.go` | M | 5 |
| 10 | Unit tests for inputs state helpers | `pkg/fingerprint/state_test.go` | S | 4 |
| 11 | Update executor tests for two-phase skip | `pkg/action/executor_test.go` | M | 6 |
| 12 | Integration test: resolver change triggers re-run | `tests/integration/` | M | 6 |
| 13 | Update design doc | `docs/design/state/fingerprinting.md` | S | All |

---

## 6. Interface Changes

### 6.1 New Reason Constant

~~~go
// pkg/fingerprint/fingerprint.go

const (
    // ReasonInputsChanged indicates resolved action inputs have changed since last run.
    ReasonInputsChanged Reason = "inputs changed"
)
~~~

### 6.2 Updated Result Struct

~~~go
// pkg/fingerprint/fingerprint.go -- add to existing Result struct

// InputsHash is the SHA-256 hash of current resolved inputs.
InputsHash string

// PreviousInputsHash is the stored hash of inputs from last successful run.
PreviousInputsHash string
~~~

### 6.3 Input Hashing

~~~go
// pkg/fingerprint/hash.go

// HashInputs computes a deterministic SHA-256 hash over the resolved action
// inputs map. Uses json.Marshal which sorts map keys lexicographically.
// Returns empty string and nil error if inputs is nil or empty.
func HashInputs(inputs map[string]any) (string, error)
~~~

### 6.4 State Helpers

~~~go
// pkg/fingerprint/state.go

// inputsKey returns the state entry key for an action's inputs fingerprint.
// Format: "__fingerprint:<actionName>:inputs"
func inputsKey(actionName string) string

// LoadInputsHash retrieves the previously stored inputs hash from state.
// Returns empty string if not found.
func LoadInputsHash(data *state.Data, actionName string) string

// SaveHashes stores the current fingerprint hashes into state data.
// Updated signature: now accepts inputsHash in addition to sources and generates.
func SaveHashes(data *state.Data, actionName, sourcesHash, generatesHash, inputsHash string)
~~~

### 6.5 Split Check Method

~~~go
// pkg/fingerprint/fingerprint.go

// CheckFiles performs Phase 1: compares source/generates file hashes against
// stored state. If Stale==true, the caller should execute the action without
// proceeding to Phase 2. If Stale==false, the caller should resolve inputs
// and call CheckInputs.
func (c *Checker) CheckFiles(ctx context.Context, actionName string,
    sources, generates []string, baseDir string) (*Result, error)

// CheckInputs performs Phase 2: compares resolved input hashes against stored
// state. Only call this when CheckFiles returned Stale==false (files match).
// resolvedInputs is the full map returned by resolveInputs().
func (c *Checker) CheckInputs(ctx context.Context, actionName string,
    resolvedInputs map[string]any) (*Result, error)
~~~

### 6.6 Updated Record

~~~go
// pkg/fingerprint/fingerprint.go

// Record stores the current fingerprints (sources + generates + inputs) after
// a successful action. Call this after action execution to persist the new
// baseline.
func (c *Checker) Record(ctx context.Context, actionName string,
    sources, generates []string, baseDir string,
    resolvedInputs map[string]any) error
~~~

---

## 7. Executor Integration Detail

Current flow (`pkg/action/executor.go` lines 554--690):

~~~
when check --> fingerprint Check() --> resolveInputs() --> execute --> Record()
~~~

New flow:

~~~go
// Phase 1: File-based fingerprint check (cheap, before input resolution)
var filesFresh bool
if e.fingerprintChecker != nil && !e.noCache && len(action.Sources) > 0 {
    fpResult, fpErr := e.fingerprintChecker.CheckFiles(ctx, actionName,
        action.Sources, action.Generates, e.cwd)
    if fpErr != nil {
        logger.FromContext(ctx).V(0).Info("fingerprint check failed, executing action",
            "action", actionName, "error", fpErr.Error())
    } else if !fpResult.Stale {
        filesFresh = true
    }
}

// Resolve inputs (always needed -- for execution and Phase 2 check)
resolvedInputs, err := e.resolveInputs(ctx, action, graph.AliasMap)
if err != nil { /* existing error handling */ }

// Phase 2: Input-based fingerprint check (only if files were fresh)
if filesFresh {
    fpResult, fpErr := e.fingerprintChecker.CheckInputs(ctx, actionName, resolvedInputs)
    if fpErr != nil {
        logger.FromContext(ctx).V(0).Info("fingerprint inputs check failed, executing action",
            "action", actionName, "error", fpErr.Error())
    } else if !fpResult.Stale {
        e.actionContext.MarkSkipped(actionName, SkipReasonUpToDate)
        if e.progressCallback != nil {
            e.progressCallback.OnActionSkipped(actionName, string(SkipReasonUpToDate))
        }
        return nil
    }
}

// ... continue to execute action ...

// After success: record with inputs
if e.fingerprintChecker != nil && len(action.Sources) > 0 {
    if recordErr := e.fingerprintChecker.Record(ctx, actionName,
        action.Sources, action.Generates, e.cwd, resolvedInputs); recordErr != nil {
        logger.FromContext(ctx).V(0).Info("fingerprint record failed",
            "action", actionName, "error", recordErr.Error())
    }
}
~~~

---

## 8. Testing Strategy

### HashInputs Tests (`pkg/fingerprint/hash_test.go`)

| Test Case | Input | Expected |
|-----------|-------|----------|
| Determinism | Same map called twice | Identical hash |
| Key ordering | `{"b":1, "a":2}` vs `{"a":2, "b":1}` | Identical hash |
| Nested structures | Maps with nested maps/slices | Consistent hash |
| Nil/empty inputs | `nil` / `map[string]any{}` | Empty string |
| Value change | `{"env":"staging"}` vs `{"env":"prod"}` | Different hash |
| Type sensitivity | `{"port": 8080}` vs `{"port": "8080"}` | Different hash |

### CheckInputs Tests (`pkg/fingerprint/fingerprint_test.go`)

| Test Case | Setup | Expected |
|-----------|-------|----------|
| First run (no prior inputs hash) | Files match, no `__fingerprint:*:inputs` in state | Stale, `ReasonFirstRun` |
| Inputs changed | Files match, inputs hash differs from stored | Stale, `ReasonInputsChanged` |
| Inputs unchanged | Files match, inputs hash matches stored | Up-to-date |
| Two-phase: files stale | Sources changed | Stale from Phase 1, Phase 2 never called |
| Two-phase: files fresh, inputs changed | Sources match, resolver value changed | Stale from Phase 2 |
| Two-phase: everything fresh | All hashes match | Skip |

### Executor Tests (`pkg/action/executor_test.go`)

| Test Case | Setup | Expected |
|-----------|-------|----------|
| Resolver change triggers re-run | Same sources, different resolved inputs | Action executes |
| Full cache hit skips action | Same sources + generates + inputs | Action skipped |
| File change short-circuits | Sources changed, inputs not checked | Action executes |

### Integration Test (`tests/integration/`)

Solution with a resolver feeding into a template action. Run once, change
resolver value, run again. Assert the action re-executed and output reflects
the new value.

---

## 9. Performance Characteristics

- **No regression for the common case**: When files change (most runs), Phase 1
  catches it immediately -- no input hashing overhead.
- **Input hashing cost**: One `json.Marshal` + SHA-256 per action, only when
  files haven't changed. Negligible for typical input sizes.
- **`resolveInputs` always runs**: This is a behavior change -- inputs are now
  resolved even when the action will be skipped by Phase 1. However, the
  resolved inputs are needed for Phase 2 and the cost is minimal (CEL/template
  evaluation is fast).

---

## 10. State Migration

Existing state files with `__fingerprint:*:sources` and
`__fingerprint:*:generates` keys but no `__fingerprint:*:inputs` key will
trigger a **one-time re-execution** (`ReasonFirstRun` on missing inputs hash).

- No manual migration step required.
- No YAML schema change -- `sources` and `generates` fields are unchanged.
- The `Check()` / `Record()` method signatures change -- direct callers (MCP
  tools, tests) need updating.

---

## 11. Risks and Edge Cases

| Risk | Mitigation |
|------|-----------|
| **Non-deterministic inputs** (timestamps, UUIDs) cause perpetual re-runs | Document limitation. Users should avoid `sources`/`generates` on actions with non-deterministic inputs, or use `--no-cache` |
| **Large resolved inputs** slow down hashing | `json.Marshal` + SHA-256 is fast even for large maps. Benchmark to confirm |
| **`resolveInputs` side effects** | `resolveInputs` is pure evaluation (CEL/template). No side effects. Safe to call before skip decision |
| **Float precision in JSON** | `encoding/json` uses consistent float formatting. Not a practical concern for config values |
| **Breaking API change** | `Check()` and `Record()` signatures change. Keep deprecated `Check()` wrapper during transition if needed |

---

## 12. Non-Goals

- **Exclude list for inputs**: Not implementing `fingerprint.exclude` to skip
  specific inputs. Documented limitation for now; can be added in a follow-up
  if demand arises.
- **Partial input hashing**: Not differentiating static vs dynamic inputs. All
  resolved inputs are hashed uniformly.
- **Input-only fingerprinting**: `sources` is still required to opt into
  fingerprinting. Actions without `sources` run every time, as before.
