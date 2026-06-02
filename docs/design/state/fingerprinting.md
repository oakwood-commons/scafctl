# File Fingerprinting & Up-to-Date Checks for Actions

Related: [#241](https://github.com/oakwood-commons/scafctl/issues/241) (state persistence),
[PR #272](https://github.com/oakwood-commons/scafctl/pull/272) (state implementation)

## 1. Summary

Add file-based fingerprinting so actions declaring `sources` (and optionally `generates`) can be
automatically skipped when their input files haven't changed and their output files haven't been
externally modified since the last successful run. Fingerprint hashes are stored via the existing
state persistence system (PR #272).

This is **Option A** from the feature spec -- the most ergonomic approach for common build, deploy,
and code-generation workflows.

**Key invariant**: An action is up-to-date only when both source inputs AND generated outputs match
their stored checksums. Manual edits to generated files trigger re-execution.

---

## 2. Architecture Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| New package | `pkg/fingerprint/` | Keeps hash/glob/staleness logic testable independently of action/state packages |
| Schema additions | `sources` and `generates` on `Action` struct | Natural extension of existing action fields; no new YAML nesting |
| State key format | `__fingerprint:<action>:sources` and `__fingerprint:<action>:generates` | Uses existing `map[string]*Entry` with a reserved namespace prefix; two keys per action |
| Hash algorithm | SHA-256 of sorted file list + contents | Reuses existing `crypto/sha256` usage in fileprovider; deterministic ordering |
| Glob library | `doublestar` (or stdlib `filepath.Glob` + `filepath.WalkDir`) | `gobwas/glob` is already a dep but doesn't expand against filesystem; `doublestar` supports `**` recursive globs |
| Skip reason | New `SkipReasonUpToDate` constant | Distinguished from `SkipReasonCondition` in output/telemetry |
| Evaluation point | After `when:` check, before input resolution in `executor.go` | Same pattern as existing skip logic; fingerprint check skipped if `when:` already skips |
| Layers affected | `pkg/action` (types + executor), new `pkg/fingerprint`, `pkg/state` (key convention), CLI output | Minimal surface area |

---

## 3. Task Breakdown

| # | Task | File(s) | Complexity | Dependencies |
|---|------|---------|------------|--------------|
| 1 | Add `Sources`/`Generates` fields to `Action` struct | `pkg/action/types.go` | S | -- |
| 2 | Add `SkipReasonUpToDate` constant | `pkg/action/types.go` | S | -- |
| 3 | Create `pkg/fingerprint/` package: glob expansion, hashing, staleness check | `pkg/fingerprint/fingerprint.go`, `hash.go`, `glob.go` | M | -- |
| 4 | Add fingerprint state key convention and helpers to read/write fingerprints from state | `pkg/fingerprint/state.go` | S | #3 |
| 5 | Integrate fingerprint check into action executor (skip if up-to-date) | `pkg/action/executor.go` | M | #1, #2, #3, #4 |
| 6 | Post-execution: store new fingerprints (sources + generates) on successful action completion | `pkg/action/executor.go` | S | #5 |
| 7 | Add `--force` / `--no-cache` flag to bypass fingerprint checks | `pkg/cmd/scafctl/run/solution.go`, executor options | S | #5 |
| 8 | Update action YAML parsing/validation (warn on `sources` without state enabled, validate glob syntax) | `pkg/action/types.go`, `pkg/action/validate.go` | S | #1 |
| 9 | Wire `doublestar` dependency (if needed) or implement recursive glob with stdlib | `go.mod` | S | #3 |
| 10 | Unit tests for `pkg/fingerprint/` | `pkg/fingerprint/*_test.go` | M | #3, #4 |
| 11 | Unit tests for executor fingerprint skip logic | `pkg/action/executor_test.go` | M | #5, #6 |
| 12 | Integration test: solution with `sources`/`generates` | `tests/integration/solutions/` | M | #1--#6 |
| 13 | CLI output integration (display "skipped (up-to-date)" in action table) | `pkg/action/output.go` | S | #2 |
| 14 | MCP tool support (expose fingerprint info in state inspection) | `pkg/mcp/tools_state.go` | S | #4 |
| 15 | Documentation and examples | `docs/`, `examples/` | S | All |

---

## 4. Interface Design

### 4.1 Core Types

~~~go
// pkg/fingerprint/fingerprint.go

package fingerprint

import "context"

// Result holds the comparison outcome for an action's sources and generates.
type Result struct {
    // Stale is true when the action should be re-executed.
    Stale bool

    // CurrentHash is the SHA-256 hash of current source files.
    CurrentHash string

    // PreviousHash is the stored hash of sources from last successful run (empty on first run).
    PreviousHash string

    // GeneratesHash is the SHA-256 hash of current generated files.
    GeneratesHash string

    // PreviousGeneratesHash is the stored hash of generates from last successful run.
    PreviousGeneratesHash string

    // Reason provides a human-readable explanation.
    Reason Reason
}

// Reason categorizes why an action is stale or up-to-date.
type Reason string

const (
    ReasonFirstRun          Reason = "first run"
    ReasonSourcesChanged    Reason = "sources changed"
    ReasonGeneratesModified Reason = "generates modified"
    ReasonGeneratesMissing  Reason = "generates missing"
    ReasonUpToDate          Reason = "up-to-date"
)
~~~

### 4.2 Checker Interface

~~~go
// pkg/fingerprint/fingerprint.go

// Checker evaluates whether an action's sources/generates have changed since the last run.
type Checker interface {
    // Check compares current file fingerprints against stored state.
    // baseDir is the working directory for resolving relative globs.
    Check(ctx context.Context, actionName string, sources, generates []string, baseDir string) (*Result, error)

    // Record stores the current fingerprint (sources + generates) after a successful action.
    Record(ctx context.Context, actionName string, sources, generates []string, baseDir string) error
}
~~~

### 4.3 Hashing

~~~go
// pkg/fingerprint/hash.go

// HashFiles computes a deterministic SHA-256 hash over the contents of all
// files matching the given glob patterns, resolved relative to baseDir.
// Files are sorted lexicographically before hashing for determinism.
// Returns empty string and nil error if patterns is empty.
func HashFiles(baseDir string, patterns []string) (string, error)
~~~

### 4.4 Glob Expansion

~~~go
// pkg/fingerprint/glob.go

// ExpandGlobs resolves glob patterns against the filesystem, returning
// a sorted, deduplicated list of matching file paths (relative to baseDir).
// Supports ** for recursive matching.
func ExpandGlobs(baseDir string, patterns []string) ([]string, error)
~~~

### 4.5 State Helpers

~~~go
// pkg/fingerprint/state.go

// SourcesKey returns the state entry key for an action's source fingerprint.
// Format: "__fingerprint:<actionName>:sources"
func SourcesKey(actionName string) string

// GeneratesKey returns the state entry key for an action's generates fingerprint.
// Format: "__fingerprint:<actionName>:generates"
func GeneratesKey(actionName string) string

// LoadHash retrieves a previously stored hash from state.
func LoadHash(stateData *state.Data, key string) string

// SaveHash stores a hash into state data for persistence.
func SaveHash(stateData *state.Data, key, hash string)
~~~

### 4.6 Action Struct Additions

~~~go
// In pkg/action/types.go, added to Action struct:

// Sources declares glob patterns for input files this action depends on.
// When provided (and state is enabled), the action is skipped if source
// files haven't changed and generated files haven't been externally modified
// since the last successful run.
Sources []string `json:"sources,omitempty" yaml:"sources,omitempty" doc:"Glob patterns for input file dependencies" maxItems:"100"`

// Generates declares glob patterns for output files this action produces.
// If all generated files exist and their checksums match the stored hashes
// from the last successful run, the action is skipped. Manual edits to
// generated files will trigger re-execution.
Generates []string `json:"generates,omitempty" yaml:"generates,omitempty" doc:"Glob patterns for output files" maxItems:"100"`
~~~

### 4.7 New Skip Reason

~~~go
// In pkg/action/types.go:

// SkipReasonUpToDate indicates the action was skipped because its source
// fingerprint and generates fingerprint both match stored state.
SkipReasonUpToDate SkipReason = "up-to-date"
~~~

---

## 5. Staleness Check Logic

An action is **up-to-date** (skip) if and only if ALL conditions hold:

~~~
1. Previous state exists for this action
2. Sources hash == stored sources hash         (inputs unchanged)
3. All generates files exist                   (outputs present)
4. Generates hash == stored generates hash     (outputs not modified externally)
~~~

If ANY condition fails, the action is **stale** and executes:

| Condition Failed | Reason | Behavior |
|-----------------|--------|----------|
| No previous state | `first run` | Execute action |
| Sources hash mismatch | `sources changed` | Execute action |
| Generated files missing | `generates missing` | Execute action |
| Generates hash mismatch | `generates modified` | Execute action |
| All match | `up-to-date` | Skip action |

### Scenario: Scaffold then manually modify

1. First run: action executes, stores hashes of both sources and generates
2. User manually edits a generated file
3. Re-run: sources hash matches, but generates hash does NOT match stored value
4. Result: **stale** with reason `generates modified` -- action re-executes

This ensures correctness: the system never assumes generated output is valid after
external modification.

### Evaluation Order in Executor

~~~
1. Check when: condition → skip with SkipReasonCondition if false
2. Check fingerprint (sources + generates) → skip with SkipReasonUpToDate if up-to-date
3. Resolve inputs
4. Execute action
5. On success: record new fingerprints (sources + generates)
~~~

---

## 6. Error Handling

### Sentinel Errors

~~~go
// pkg/fingerprint/errors.go

var (
    // ErrNoMatches is returned when a glob pattern matches zero files.
    ErrNoMatches = errors.New("glob pattern matched no files")

    // ErrPatternInvalid is returned for malformed glob patterns.
    ErrPatternInvalid = errors.New("invalid glob pattern")
)
~~~

### Wrapping Strategy

- Glob expansion: `fmt.Errorf("expanding sources for action %q: %w", name, err)`
- Hash computation: `fmt.Errorf("computing fingerprint for action %q: %w", name, err)`
- State read/write: `fmt.Errorf("fingerprint state for action %q: %w", name, err)`

### Behavior on Error

| Error Type | Behavior | Rationale |
|-----------|----------|-----------|
| Invalid glob pattern | **Fail action** | Configuration error -- user must fix |
| Permission denied on file | **Run action** (fail-open) + warning | Transient; safer to re-run than skip |
| State unavailable / disabled | **Run action** | First-run equivalent; no skip possible |
| Empty sources (no patterns) | **Run action** | No fingerprint possible; normal execution |

---

## 7. Testing Strategy

| Type | Scope | File(s) |
|------|-------|---------|
| Unit | Glob expansion: `**/*.go`, `*`, nested dirs, symlinks, no-match | `pkg/fingerprint/glob_test.go` |
| Unit | Hash determinism: same files same hash, different different | `pkg/fingerprint/hash_test.go` |
| Unit | Staleness logic: first run, sources changed, generates modified, generates missing, up-to-date | `pkg/fingerprint/fingerprint_test.go` |
| Unit | State key helpers: format, load, save | `pkg/fingerprint/state_test.go` |
| Unit | Executor skip logic: sources unchanged -> skip, changed -> run | `pkg/action/executor_test.go` |
| Unit | Action validation: sources without state, invalid globs | `pkg/action/validate_test.go` |
| Benchmark | `HashFiles` with 100/1000/10000 files | `pkg/fingerprint/hash_test.go` |
| Benchmark | `ExpandGlobs` with recursive patterns in deep tree | `pkg/fingerprint/glob_test.go` |
| Integration (CLI) | `scafctl run solution` with sources -- verify skip on re-run | `tests/integration/` |
| Integration (Solution) | YAML fixture with sources/generates, manual modification scenario | `tests/integration/solutions/fingerprint/` |
| E2E | `task test:e2e` pass | Full suite |

All tests use table-driven patterns with `testify/assert`. Tests create temp directories with
known file contents to verify hash behavior deterministically.

---

## 8. Documentation

| Asset | Location | Content |
|-------|----------|---------|
| Design doc | `docs/design/state/fingerprinting.md` (this file) | Architecture, staleness logic, interface contracts |
| Tutorial | `docs/tutorials/fingerprinting-tutorial.md` | Step-by-step: build solution with sources/generates |
| Example solution | `examples/solutions/fingerprint-build.yaml` | Go build with source tracking |
| Provider reference update | `docs/tutorials/provider-reference.md` | Note on exec + sources interplay |
| MCP tool | `pkg/mcp/tools_state.go` | Expose fingerprint entries in state inspection |
| CHANGELOG | `CHANGELOG.md` | Feature entry under next version |

---

## 9. Risks & Edge Cases

| Risk | Mitigation |
|------|-----------|
| **Large file trees** -- hashing thousands of files could be slow | Benchmark; consider parallel hashing with `errgroup`; document performance expectations |
| **Symlinks** -- could cause infinite loops in glob expansion | Use `filepath.WalkDir` with symlink detection; skip or follow based on flag |
| **Race conditions** -- files change between hash and execution | Accept as inherent limitation; document that fingerprinting is advisory, not atomic |
| **State disabled** -- user adds `sources` but state is off | Validation warning at parse time; action runs normally (no-op fingerprint) |
| **Empty glob match** -- pattern matches nothing (e.g., typo) | Return `ErrNoMatches`; fail the action so user notices misconfiguration |
| **Cross-platform paths** -- Windows vs Unix glob separators | Use `filepath.ToSlash` for state keys; test on both |
| **`.gitignore` interaction** -- should ignored files be included? | Yes -- fingerprinting is explicit (user declares patterns); no implicit filtering |
| **State schema migration** -- adding fingerprint keys to existing state files | No migration needed -- new keys simply appear in the existing `Values` map |
| **`--force` escape hatch** -- user needs to bypass fingerprint | Add `--force` flag to `run solution` that sets a context value checked by executor |
| **Manual edits to generates trigger re-run** | Correct behavior -- ensures generated output matches what the action would produce. Users who intentionally customize output should remove `generates` or use file conflict strategies |
| **Generates hash computed at wrong time** | Record generates hash *after* the action completes and files are written to final state |
| **Breaking change** -- new YAML fields are additive | No break -- `sources`/`generates` are optional; existing solutions unaffected |

---

## 10. Example Usage

~~~yaml
apiVersion: scafctl/v1
kind: Solution
metadata:
  name: go-build
  version: 1.0.0

state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: ".scafctl/state.json"

spec:
  resolvers:
    app-name:
      value: myapp

  workflow:
    actions:
      build:
        description: Build the Go binary
        provider: exec
        inputs:
          command:
            tmpl: "go build -o dist/{{ .app-name }} ./cmd/{{ .app-name }}"
        sources:
          - "cmd/**/*.go"
          - "pkg/**/*.go"
          - "internal/**/*.go"
          - go.mod
          - go.sum
        generates:
          - "dist/myapp"

      test:
        description: Run unit tests
        provider: exec
        inputs:
          command: go test ./...
        sources:
          - "**/*_test.go"
          - "pkg/**/*.go"
          - "internal/**/*.go"
~~~

**First run**: Both actions execute; fingerprints stored in `.scafctl/state.json`.

**Second run (no changes)**: Both actions skipped with reason `up-to-date`.

**After editing `pkg/server/handler.go`**: Both actions re-execute (sources changed).

**After manually editing `dist/myapp`** (unlikely but illustrative): `build` re-executes
(generates modified), `test` unaffected (no generates declared).

---

## 11. Input Hashing (Resolved Inputs)

### Problem

File fingerprinting alone is insufficient when resolver-driven inputs change but
source/template files remain unchanged. For example, a Go template `config.yaml.tpl`
stays the same, but a resolver changes `environment` from `"staging"` to `"production"`.
The file-based fingerprint sees no change and skips the action -- producing stale output.

### Solution: Two-Phase Fingerprint Check

The fingerprint check is split into two phases to avoid unnecessary (and potentially
expensive) input resolution when files have already changed:

~~~
Phase 1 (CheckFiles) -- before input resolution:
  1. Hash source files
  2. Hash generated files
  3. Compare against stored hashes
  4. If files changed -> stale (skip Phase 2, proceed to execute)

Phase 2 (CheckInputs) -- after input resolution:
  1. Hash resolved inputs (JSON-serialized, SHA-256)
  2. Compare against stored inputs hash
  3. If inputs changed -> stale (proceed to execute)
  4. If inputs unchanged -> up-to-date (skip action)
~~~

### Staleness Matrix (Updated)

| Sources | Generates | Inputs | Result | Reason |
|---------|-----------|--------|--------|--------|
| No previous state | -- | -- | Stale | `first run` |
| Changed | -- | -- | Stale | `sources changed` |
| Match | Missing | -- | Stale | `generates missing` |
| Match | Changed | -- | Stale | `generates modified` |
| Match | Match | Changed | Stale | `inputs changed` |
| Match | Match | Match | Up-to-date | `up-to-date` |
| Match | Match | Both empty | Up-to-date | No inputs declared |

### API Changes

~~~go
// pkg/fingerprint/fingerprint.go

const ReasonInputsChanged Reason = "inputs changed"

// Result now includes:
type Result struct {
    // ... existing fields ...
    InputsHash         string  // SHA-256 of current resolved inputs
    PreviousInputsHash string  // Stored hash from last run
}

// Check is split into two methods:
func (c *Checker) CheckFiles(actionName string, sources, generates []string, baseDir string) (*Result, error)
func (c *Checker) CheckInputs(actionName string, resolvedInputs map[string]any) (*Result, error)

// Record now accepts resolved inputs:
func (c *Checker) Record(actionName string, sources, generates []string, baseDir string, resolvedInputs map[string]any) error
~~~

~~~go
// pkg/fingerprint/hash.go

// HashInputs computes a deterministic SHA-256 hash of resolved action inputs.
// Uses JSON marshaling for deterministic serialization (Go's encoding/json
// sorts map keys). Returns empty string for nil or empty inputs.
func HashInputs(inputs map[string]any) (string, error)
~~~

~~~go
// pkg/fingerprint/state.go

// State key format: "__fingerprint:<action>:inputs"
func inputsKey(actionName string) string
func LoadInputsHash(stateData *state.Data, actionName string) string
// SaveHashes now stores inputs hash alongside sources and generates hashes.
func SaveHashes(stateData *state.Data, actionName, sourcesHash, generatesHash, inputsHash string)
~~~

### Executor Integration

In `pkg/action/executor.go`, the fingerprint check is restructured:

~~~go
// Phase 1: File-based check (cheap, before input resolution)
fpResult, err := fpChecker.CheckFiles(actionName, action.Sources, action.Generates, cwd)
if fpResult.Stale {
    // Files changed -- must re-execute, resolve inputs and run
    resolvedInputs := resolveInputs(...)
    callProvider(...)
    fpChecker.Record(actionName, sources, generates, cwd, resolvedInputs)
    return
}

// Phase 2: Input-based check (after resolver evaluation)
resolvedInputs := resolveInputs(...)
inputResult, err := fpChecker.CheckInputs(actionName, resolvedInputs)
if inputResult.Stale {
    // Inputs changed -- re-execute
    callProvider(...)
    fpChecker.Record(actionName, sources, generates, cwd, resolvedInputs)
    return
}

// Both files and inputs match -- skip
skip(SkipReasonUpToDate)
~~~

### Edge Cases

- **No inputs declared**: Both current and previous inputs hashes are empty -- treated as up-to-date
  (inputs are not a factor when none are configured).
- **First run with inputs**: No previous hash exists -- action executes and stores the hash.
- **Non-deterministic inputs**: If inputs contain timestamps or random values, they will
  change on every run, effectively disabling the skip optimization for that action.
