# Functional Testing — Phased Implementation Plan

> **Source**: [functional-testing.md](functional-testing.md)  
> **Branch strategy**: Single branch; phases are sequential milestones, not separate PRs.  
> **Delete this file** after implementation is complete.

---

## TL;DR

Implement the `scafctl test functional` and `scafctl test list` commands in 8 phases, building bottom-up from Go types through sandbox orchestration, assertions, CLI integration, and documentation. Each phase produces a compilable, testable increment. The design doc specifies ~18 new files in `pkg/solution/testing/` and `pkg/cmd/scafctl/test/`, plus modifications to `spec.go`, `compose.go`, `discover.go`, `exitcode.go`, `root.go`, and `lint.go`.

---

## Phase 1 — Foundation Types & Exit Code

**Goal**: Define all Go types from the design doc and add the `TestFailed` exit code. Everything compiles; nothing executes yet.

### Steps

1. **Create `pkg/solution/testing/` package** — add a doc.go or package comment establishing the package purpose.

2. **Create `pkg/solution/testing/types.go`** — implement all types from the design doc's "Go Types" section:
   - `TestCase` struct with all 20+ fields, JSON/YAML/doc/validation tags per project conventions (Huma tags: `doc`, `maxLength`, `example`, `maximum`, `maxItems`, `pattern`, `patternDescription`).
   - `TestConfig` struct with `SkipBuiltins`, `Env`, `Setup`, `Cleanup` fields.
   - `SkipBuiltinsValue` with custom `UnmarshalYAML` (handle `bool | []string`) and `MarshalYAML` (round-trip safe for `deepCopySolution`). Add `UnmarshalJSON`/`MarshalJSON` for completeness.
   - `InitStep` struct matching exec provider's input schema.
   - `Assertion` struct with exactly-one-of validation pattern.
   - `Duration` wrapper around `time.Duration` with `UnmarshalYAML`/`MarshalYAML`/`UnmarshalJSON`/`MarshalJSON` using Go duration string format (`"30s"`, `"2m"`).
   - `FileInfo` struct (`Exists bool`, `Content string`).
   - `CommandOutput` struct (`Stdout`, `Stderr`, `ExitCode`, `Output`, `Files`).
   - `TestResult` and `AssertionResult` structs for result reporting.
   - `Status` type with constants: `StatusPass`, `StatusFail`, `StatusSkip`, `StatusError`.
   - Constants: `MaxAssertionsPerTest = 100`, `MaxFilesPerTest = 50`, `MaxTagsPerTest = 20`, `MaxExtendsDepth = 10`, `MaxTestsPerSolution = 500`, `MaxRetries = 10`.
   - `TestCase.IsTemplate()` method — returns `strings.HasPrefix(tc.Name, "_")`.

3. **Implement `TestCase.Validate()`** — comprehensive validation:
   - Name matches `^[a-zA-Z0-9][a-zA-Z0-9_-]*$` (or `^_[a-zA-Z0-9][a-zA-Z0-9_-]*$` for templates).
   - `ExitCode` and `ExpectFailure` are mutually exclusive.
   - Non-template tests must have `command` set (after extends resolution) and at least one of `assertions` or `snapshot`.
   - `Args` must not contain `-f` or `--file`.
   - `Retries` is 0–`MaxRetries`.
   - Field count limits: assertions ≤ 100, files ≤ 50, tags ≤ 20.
   - `Timeout` if set must be positive.

4. **Implement `Assertion.Validate()`** — exactly one assertion type set; `target` is one of `stdout`, `stderr`, `combined`, or empty (defaults to `stdout`).

5. **Modify `pkg/exitcode/exitcode.go`** — add `TestFailed = 11` constant below `PermissionDenied = 10`. Update `Description()` switch with `"One or more tests failed"`.

6. **Create `pkg/solution/testing/types_test.go`** — unit tests for:
   - `SkipBuiltinsValue` YAML marshal/unmarshal round-trip (bool `true`, bool `false`, `[]string`).
   - `Duration` marshal/unmarshal round-trip (`"30s"`, `"2m"`, `"1m30s"`).
   - `TestCase.Validate()` — valid cases, mutual exclusion errors, name format errors, field limit violations.
   - `Assertion.Validate()` — exactly-one-of enforcement, target validation.
   - `TestCase.IsTemplate()` — `_base` → true, `render-test` → false.

### Verification

```bash
go build ./pkg/solution/testing/...
go test ./pkg/solution/testing/...
go vet ./pkg/solution/testing/...
golangci-lint run ./pkg/solution/testing/...
```

---

## Phase 2 — Spec Integration & Compose

**Goal**: Wire `Tests` and `TestConfig` into the solution spec and compose pipeline so that test definitions are parsed, merged, and round-tripped correctly.

### Steps

1. **Modify `pkg/solution/spec.go`** — add two fields to the `Spec` struct:
   - `Tests map[string]*testing.TestCase` with `json:"tests,omitempty" yaml:"tests,omitempty" doc:"..."`.
   - `TestConfig *testing.TestConfig` with `json:"testConfig,omitempty" yaml:"testConfig,omitempty" doc:"..."`.
   - Add `HasTests() bool` — `len(s.Tests) > 0`.
   - Add `HasTestConfig() bool` — `s.TestConfig != nil`.
   - Follow the existing `HasResolvers()` / `HasWorkflow()` pattern.
   - Import `pkg/solution/testing` — use alias `soltesting` to avoid collision with Go's `testing` package.

2. **Modify `pkg/solution/bundler/compose.go`** — extend `composePart` struct:
   - Add `Tests map[string]*testing.TestCase` under `Spec`.
   - Add `TestConfig *testing.TestConfig` under `Spec`.
   - In `Compose()`, implement merge logic:
     - `Tests`: reject-duplicates strategy (same as resolvers) — error if two compose files define the same test name.
     - `TestConfig.SkipBuiltins`: `true`-wins for bool; union (deduplicated) for `[]string`.
     - `TestConfig.Env`: merged map, last compose file wins on key conflict.
     - `TestConfig.Setup`: appended in compose-file order (new merge strategy).
     - `TestConfig.Cleanup`: appended in compose-file order.
   - After merge, iterate `Tests` map and set `Name` field from map key (same pattern as `ResolversToSlice()`).

3. **Modify `pkg/solution/bundler/discover.go`** — add `TestInclude DiscoverySource = 2` constant. In `DiscoverFiles()`, scan `spec.Tests[*].Files` entries and add them as `TestInclude` file entries.

4. **Create `pkg/solution/testing/compose_test.go`** (or add to existing compose tests) — test compose merge behavior:
   - Two compose files with distinct tests → merge succeeds.
   - Two compose files with same test name → error.
   - `testConfig.skipBuiltins` merge: `true` + `false` → `true`; `["lint"]` + `["parse"]` → `["lint", "parse"]`.
   - `testConfig.setup` appended in order.
   - `testConfig.env` last-file-wins on conflict.

5. **Update existing spec tests** — verify that existing solutions without `tests` or `testConfig` still parse correctly (backward compat). Add a test with inline tests and verify round-trip through `deepCopySolution`.

### Verification

```bash
go test ./pkg/solution/...
go test ./pkg/solution/bundler/...
# Verify existing integration tests still pass:
go test ./tests/integration/...
```

---

## Phase 3 — Sandbox & File Management

**Goal**: Implement isolated temp directory creation, file copying, and file-diff tracking for `output.files`.

### Steps

1. **Create `pkg/solution/testing/sandbox.go`** — implement `Sandbox` type:
   - `NewSandbox(solutionPath string, bundleFiles []string, testFiles []string) (*Sandbox, error)` — creates temp dir, copies solution + bundle files + test files maintaining relative paths.
   - `PreSnapshot()` — records all file paths and modification times in the sandbox.
   - `PostSnapshot() map[string]FileInfo` — diffs current sandbox against pre-snapshot; returns only new/modified files. Applies the 10MB size guard (`content = "<file too large>"`) and binary file guard (`content = "<binary file>"` for non-UTF-8).
   - `Path() string` — returns sandbox root path.
   - `SolutionPath() string` — returns path to the solution file within the sandbox.
   - `Cleanup()` — removes temp directory.
   - Reject symlinks during file copy (return error).
   - Reject path traversal above solution root (`..`).

2. **Create `pkg/solution/testing/sandbox_test.go`** — test:
   - Basic sandbox creation and cleanup.
   - File diff detection (new file, modified file, unchanged file).
   - 10MB size guard.
   - Binary file guard.
   - Symlink rejection.
   - Path traversal rejection.
   - Suite-level setup: create base sandbox, copy for per-test isolation.

3. **Implement suite-level sandbox** in `sandbox.go`:
   - `NewBaseSandbox(solutionPath string, bundleFiles []string, setupSteps []InitStep) (*Sandbox, error)` — creates base sandbox and runs setup steps.
   - `CopyForTest(testFiles []string) (*Sandbox, error)` — deep-copies the base sandbox into a new temp dir and adds test-specific files.

### Verification

```bash
go test ./pkg/solution/testing/... -run Sandbox
# Verify temp dirs are created and cleaned up (no leaked dirs).
```

---

## Phase 4 — Test Inheritance & Discovery

**Goal**: Implement `extends` resolution with merge logic, test discovery from files/directories, and filtering by name/tag/solution.

### Steps

1. **Create `pkg/solution/testing/inheritance.go`** — implement extends resolution:
   - `ResolveExtends(tests map[string]*TestCase) error` — resolves all `extends` chains in-place.
   - For each test with `extends`, apply templates left-to-right using the merge strategy from the design doc:
     - `command`: child wins if set.
     - `args`: appended (base first, then child).
     - `assertions`: appended (base first, then child).
     - `files`: appended, deduplicated.
     - `init`: base steps prepended before child steps.
     - `cleanup`: base steps appended after child steps.
     - `tags`: appended, deduplicated.
     - `env`: merged map, child wins on key conflict.
     - Scalar fields (`description`, `timeout`, `expectFailure`, `exitCode`, `skip`, `injectFile`, `snapshot`, `skipExpression`, `retries`): child wins if set.
   - Detect circular extends chains → return error.
   - Enforce `MaxExtendsDepth = 10`.
   - Validate that all `extends` references exist in the tests map → error if not.

2. **Create `pkg/solution/testing/discovery.go`** — implement test discovery:
   - `DiscoverSolutions(testsPath string) ([]SolutionTests, error)` — recursively find solution files, parse specs, extract tests.
   - `DiscoverFromFile(filePath string) (*SolutionTests, error)` — parse single solution file.
   - `SolutionTests` struct: `Solution *solution.Solution`, `Tests map[string]*TestCase`, `TestConfig *TestConfig`, `FilePath string`.
   - `FilterTests(tests []SolutionTests, opts FilterOptions) []SolutionTests` — apply `--filter`, `--tag`, `--solution` filters. All filters ANDed.
   - `FilterOptions` struct: `NamePatterns []string`, `Tags []string`, `SolutionPatterns []string`.
   - `--filter` parsing: if contains `/`, split into solution glob + test glob; otherwise match test name only.
   - Use `doublestar.Match` for glob matching (already a dependency).
   - Exclude template tests (names starting with `_`) from execution list.
   - Sort tests alphabetically; builtins first.

3. **Create `pkg/solution/testing/inheritance_test.go`** — test:
   - Simple single-extends chain.
   - Multi-extends: `extends: [_base1, _base2]` with correct merge order.
   - Deep chain (template extends template).
   - Circular detection → error.
   - Non-existent reference → error.
   - Depth limit (11 levels) → error.
   - All field merge strategies individually.

4. **Create `pkg/solution/testing/discovery_test.go`** — test:
   - Directory scan finds multiple solutions.
   - Single-file discovery.
   - `--filter` glob matching (simple, `solution/test-name` format).
   - `--tag` filtering (any-match).
   - `--solution` filtering.
   - Combined filter (AND logic).
   - Template exclusion from execution.
   - Alphabetical ordering.

### Verification

```bash
go test ./pkg/solution/testing/... -run "Inherit|Discover"
```

---

## Phase 5 — Assertions & CEL Diagnostics

**Goal**: Implement all five assertion types, target routing, CEL context building, and sub-expression diagnostic evaluation.

### Steps

1. **Create `pkg/solution/testing/context.go`** — build CEL assertion context:
   - `BuildAssertionContext(cmdOutput *CommandOutput) (map[string]any, error)` — creates the CEL variable map with `stdout`, `stderr`, `exitCode`, `output`, `files`.
   - Handle `output` being `nil` when command doesn't support `-o json` — CEL expressions referencing `output` should produce a test `error` with the diagnostic message: *"variable 'output' is nil — this command does not support structured output"*.
   - Register custom CEL variables using `pkg/celexp` patterns (see `BuildCELContext()` in `pkg/celexp/context.go`).

2. **Create `pkg/solution/testing/assertions.go`** — evaluate assertions:
   - `EvaluateAssertions(assertions []Assertion, cmdOutput *CommandOutput) []AssertionResult` — evaluate all assertions, never short-circuit.
   - For `expression`: call `celexp.EvaluateExpression()` with the assertion context. If result is not `bool`, return error. If `output` is nil and expression references it, return `StatusError` result.
   - For `regex`: compile pattern, match against target text (resolved via `target` field).
   - For `contains`: `strings.Contains` against target text.
   - For `notRegex`: compile pattern, ensure no match against target text.
   - For `notContains`: `!strings.Contains` against target text.
   - `resolveTarget(cmdOutput *CommandOutput, target string) string` — returns stdout (default), stderr, or combined (stdout + "\n" + stderr).

3. **Create `pkg/solution/testing/diagnostics.go`** — CEL failure diagnostics:
   - `DiagnoseExpression(expr string, context map[string]any) string` — when a CEL assertion fails, inspect comparison expressions (`==`, `!=`, `<`, `>`, `in`) and evaluate both sides independently.
   - Return formatted diagnostic: `size(output.actions) = 5\n  Expected 3, got 5`.
   - Use CEL AST introspection (`cel.Ast()`) to identify comparison nodes.
   - For non-comparison expressions, return the expression and its result.
   - Degrade gracefully to "expected true, got false" for expressions too complex to decompose.

4. **Create `pkg/solution/testing/assertions_test.go`** — test:
   - Each assertion type: expression (pass/fail), regex (pass/fail), contains (pass/fail), notRegex, notContains.
   - Target routing: stdout (default), stderr, combined.
   - All assertions evaluated even when some fail.
   - `output` nil → error diagnostic.
   - Custom message passed through.

5. **Create `pkg/solution/testing/diagnostics_test.go`** — test:
   - `==` comparison: shows both sides.
   - `size()` comparison.
   - Non-comparison expression: shows expression and result.
   - Complex nested expression: graceful degradation.

### Verification

```bash
go test ./pkg/solution/testing/... -run "Assert|Context|Diagnos"
```

---

## Phase 6 — Snapshots & Builtins

**Goal**: Implement golden file comparison with normalization, and the four builtin test definitions.

### Steps

1. **Create `pkg/solution/testing/snapshot.go`** — golden file comparison:
   - `CompareSnapshot(actual string, snapshotPath string) (bool, string, error)` — normalize `actual`, read golden file, compare. Returns `(match, unifiedDiff, error)`.
   - `UpdateSnapshot(actual string, snapshotPath string) error` — normalize and overwrite golden file.
   - `Normalize(input string) string` — fixed normalization pipeline:
     1. Sort JSON map keys deterministically (parse JSON if valid, re-serialize with sorted keys).
     2. Replace ISO-8601 timestamps (`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[Z+\-\d:]*`) with `<TIMESTAMP>`.
     3. Replace UUIDs (`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`) with `<UUID>`.
     4. Replace sandbox absolute paths with `<SANDBOX>`.
   - Unified diff output using `github.com/google/go-cmp` (already a dependency) or a simple unified diff implementation.

2. **Create `pkg/solution/testing/builtins.go`** — define four builtin tests:
   - `BuiltinTests(testConfig *TestConfig) []*TestCase` — returns the builtin test cases, filtered by `skipBuiltins`.
   - `builtin:parse` — internal YAML parse validation (no command execution needed; just verify the solution parsed).
   - `builtin:lint` — `command: [lint]`, assertion: `output.errorCount == 0`.
   - `builtin:resolve-defaults` — `command: [run, resolver]`, assertion: exit code 0.
   - `builtin:render-defaults` — `command: [render, solution]`, assertion: exit code 0.
   - Builtins are prefixed with `builtin:` in names and always sort first alphabetically.
   - `shouldSkipBuiltin(name string, skipValue SkipBuiltinsValue) bool` — check if a specific builtin is skipped.

3. **Create `pkg/solution/testing/snapshot_test.go`** — test:
   - Normalization: timestamps replaced, UUIDs replaced, paths replaced, JSON keys sorted.
   - Snapshot match → true, empty diff.
   - Snapshot mismatch → false, meaningful unified diff.
   - Update overwrites file.
   - Non-JSON input normalization (only timestamp/UUID/path replacements, no key sorting).
   - `expectFailure` with snapshot: snapshot compares stdout from failing command.

4. **Create `pkg/solution/testing/builtins_test.go`** — test:
   - All four builtins returned when `skipBuiltins` is default.
   - `skipBuiltins: true` → empty list.
   - `skipBuiltins: ["lint", "parse"]` → only `resolve-defaults` and `render-defaults` returned.
   - Builtin names include `builtin:` prefix.

### Verification

```bash
go test ./pkg/solution/testing/... -run "Snapshot|Builtin"
```

---

## Phase 7 — Runner & CLI Commands

**Goal**: Implement the core test runner, init/cleanup execution, JUnit reporting, and both CLI commands.

### Steps

1. **Create `pkg/solution/testing/runner.go`** — the main test execution engine:
   - `Runner` struct with fields: `Concurrency int`, `FailFast bool`, `UpdateSnapshots bool`, `Verbose bool`, `KeepSandbox bool`, `TestTimeout time.Duration`, `GlobalTimeout time.Duration`, `SkipBuiltins bool`, `DryRun bool`, `IOStreams *terminal.IOStreams`.
   - `Run(ctx context.Context, solutions []SolutionTests) ([]TestResult, error)` — orchestrates execution:
     1. For each solution: generate builtins, resolve extends, validate all tests, apply filters, sort (builtins first, then alphabetical).
     2. If `DryRun` → validate and return without executing.
     3. If `testConfig.setup` → create base sandbox, run setup steps. On failure → all tests report `error`.
     4. For each test: create sandbox (copy base or from source), run init, execute command, collect output, evaluate assertions/snapshot, run cleanup.
     5. Respect concurrency limit (`-j`) using a semaphore (`chan struct{}`).
     6. Handle `--fail-fast` per-solution (stop remaining tests for the solution on first failure).
     7. Handle retries: on failure with `retries > 0`, re-run from fresh sandbox up to N times.
     8. Run `testConfig.cleanup` after all tests complete.
   - `executeTest(ctx context.Context, tc *TestCase, sandbox *Sandbox, testConfig *TestConfig) *TestResult` — single test execution:
     1. Check `skip` / `skipExpression`.
     2. Run init steps via `pkg/shellexec` (exec provider pattern).
     3. Build cobra command: `Root(&RootOptions{IOStreams: ..., ExitFunc: ...})`, set args.
     4. Auto-inject `-f <sandbox-solution-path>` unless `injectFile: false`.
     5. Auto-detect/append `-o json` via `cmd.Flags().Lookup("output")`.
     6. Inject env vars: process → `testConfig.env` → `TestCase.env`, plus `SCAFCTL_SANDBOX_DIR`.
     7. Execute with timeout (per-test or `--test-timeout`).
     8. Capture stdout/stderr from IOStreams buffers, exit code from ExitFunc.
     9. Build `CommandOutput`: parse JSON stdout if available, compute file diffs.
     10. Check exit code (`expectFailure` / `exitCode`).
     11. Run snapshot comparison if `snapshot` is set.
     12. Evaluate all assertions.
     13. Run cleanup steps (even on failure).
     14. Return `TestResult`.

2. **Create `pkg/solution/testing/reporter.go`** — kvx result formatting:
   - `ReportResults(results []TestResult, opts *kvx.OutputOptions) error` — formats results for table/json/yaml/quiet output.
   - Table columns: `SOLUTION`, `TEST`, `STATUS`, `DURATION`.
   - Verbose table: add `(N/M)` assertion counts.
   - Summary line: `N passed, N failed, N errors, N skipped (duration)`.
   - Failure/error detail output with assertion diagnostics.

3. **Create `pkg/solution/testing/junit.go`** — JUnit XML report:
   - `WriteJUnitReport(results []TestResult, path string) error`.
   - One `<testsuite>` per solution, one `<testcase>` per test.
   - `<failure>` for assertion failures, `<error>` for init/infrastructure errors.
   - `<skipped message="reason"/>` for skipped tests.
   - Use `encoding/xml` from stdlib (no new dependency needed).

4. **Create `pkg/cmd/scafctl/test/test.go`** — parent command:
   - `CommandTest(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command` — follows `CommandRender`/`CommandRun` pattern.
   - `Use: "test"`, `Short: "Run and manage functional tests"`.
   - Add subcommands: `CommandFunctional(...)`, `CommandList(...)`.

5. **Create `pkg/cmd/scafctl/test/functional.go`** — `test functional` command:
   - Register all flags from the design doc: `-f`, `--tests-path`, `-o`, `--report-file`, `--update-snapshots`, `--sequential`, `-j`, `--skip-builtins`, `--test-timeout`, `--timeout`, `--filter`, `--tag`, `--solution`, `--dry-run`, `--fail-fast`, `-v`, `--keep-sandbox`, `--no-color`, `-q`.
   - `--filter`, `--tag`, `--solution` registered via `StringArrayVar`.
   - `RunE`: discover solutions → filter → create `Runner` → execute → report → write JUnit if `--report-file` → return exit code.

6. **Create `pkg/cmd/scafctl/test/list.go`** — `test list` command:
   - Flags: `-f`, `--tests-path`, `-o`, `--include-builtins`, `--tag`, `--solution`, `--filter`.
   - Table columns: `SOLUTION`, `TEST`, `COMMAND`, `TAGS`, `SKIP`.
   - Discover + filter + format (no execution).

7. **Modify `pkg/cmd/scafctl/root.go`** — register `test` command:
   - Add import for `pkg/cmd/scafctl/test`.
   - Add `cmd.AddCommand(test.CommandTest(cliParams, ioStreams, path))` alongside existing command registrations.

8. **Create test files**:
   - `pkg/solution/testing/runner_test.go` — test full execution flow with a minimal solution, test pass/fail/skip/error paths, retry behavior, concurrency, fail-fast, dry-run.
   - `pkg/cmd/scafctl/test/functional_test.go` — test flag parsing, basic command execution.

### Verification

```bash
go build ./cmd/scafctl/scafctl.go
go test ./pkg/solution/testing/...
go test ./pkg/cmd/scafctl/test/...
go test -race ./pkg/solution/testing/...
# Manual smoke test:
go run ./cmd/scafctl/scafctl.go test functional --help
```

---

## Phase 8 — Lint Rules, Integration Tests, Docs & Examples

**Goal**: Add lint rules, create self-hosted integration test fixtures, write tutorial and example solution, verify end-to-end.

### Steps

1. **Modify `pkg/cmd/scafctl/lint/lint.go`** — add three lint rules:
   - **Error**: Test files not covered by `bundle.include` — iterate `spec.tests[*].files`, check each against `bundle.include` glob patterns.
   - **Error**: Invalid test names — names not matching the required regex.
   - **Warning**: Unused test templates — templates (names starting with `_`) not referenced by any `extends` field.
   - Follow the existing inline rule pattern (add to the rule evaluation section alongside `unused-resolver`, `invalid-dependency`, etc.).

2. **Create `tests/integration/solutions/`** — self-hosted test fixtures:
   - `tests/integration/solutions/hello-world/solution.yaml` — minimal solution with 2-3 inline tests (CEL assertion, contains assertion, `expectFailure` for validation).
   - `tests/integration/solutions/hello-world/testdata/expected-render.json` — golden file for snapshot test.
   - `tests/integration/solutions/composed/solution.yaml` + `tests/integration/solutions/composed/tests/rendering.yaml` — compose-based test split.
   - Ensure `task integration` (`scafctl test functional --tests-path tests/integration/solutions --no-color -q`) passes with these fixtures.

3. **Add CLI integration tests** to `tests/integration/cli_test.go`:
   - `TestTestFunctional` — run `scafctl test functional -f <path>` on a minimal solution, verify exit code 0, expected output.
   - `TestTestFunctionalFailure` — run on a solution with a deliberately failing test, verify exit code 11.
   - `TestTestList` — run `scafctl test list -f <path>`, verify table output includes test names.
   - `TestTestFunctionalJsonOutput` — run with `-o json`, verify valid JSON with expected structure.
   - `TestTestFunctionalJUnit` — run with `--report-file`, verify XML file is written.
   - `TestTestFunctionalDryRun` — run with `--dry-run`, verify no tests execute.
   - `TestTestFunctionalFilter` — run with `--filter`, verify only matching tests run.

4. **Create `docs/tutorials/functional-testing.md`** — tutorial per the design doc's outline:
   - Writing your first test.
   - Assertions deep dive (CEL vs regex/contains, `target`, `output` structure).
   - Test inheritance (`_`-prefixed templates, `extends`, merge behavior).
   - Snapshots (golden file workflow, `--update-snapshots`, normalization).
   - CI integration (JUnit XML, exit codes, `--fail-fast`).
   - Advanced features (init/cleanup, test files, `skipExpression`, retries, suite-level setup).

5. **Create `examples/solutions/tested-solution/`** — example per the design doc:
   - `solution.yaml` — solution with 2-3 resolvers and a template action, plus 3-4 inline tests.
   - `testdata/expected-render.json` — golden file.
   - `bundle.include` covering `testdata/**`.

6. **Update `docs/design/testing.md`** (if it exists) — add reference to the functional testing design doc.

7. **Final linting pass**: `golangci-lint run --fix` across all new and modified files.

### Verification

```bash
go test ./...
go test -race ./pkg/solution/testing/...
golangci-lint run
go build -ldflags "..." -o dist/scafctl ./cmd/scafctl/scafctl.go
# Self-hosted:
dist/scafctl test functional --tests-path tests/integration/solutions --no-color -q
dist/scafctl test list --tests-path tests/integration/solutions
dist/scafctl test functional --tests-path tests/integration/solutions --report-file /tmp/junit.xml
```

---

## Dependency Graph

```
Phase 1 (Types & Exit Code)
    │
    ▼
Phase 2 (Spec + Compose)
    │
    ├──────────────────────────────────────┐
    ▼                ▼                     ▼
Phase 3 (Sandbox)   Phase 4 (Inheritance   Phase 5 (Assertions
                     + Discovery)          + Diagnostics)
    │                │                     │
    │                ▼                     │
    │           Phase 6 (Snapshots         │
    │            + Builtins)               │
    │                │                     │
    └────────────────┴─────────────────────┘
                     │
                     ▼
             Phase 7 (Runner + CLI)
                     │
                     ▼
             Phase 8 (Lint, Integration,
                      Docs & Examples)
```

Phases 3, 4, 5, and 6 can be worked on in parallel after Phase 2 is complete, since they have minimal interdependencies. Phase 7 is the integration point that ties everything together. Phase 8 is the final verification and documentation pass.

---

## Files Summary

### New Files (~21)

| File | Phase | Description |
|------|-------|-------------|
| `pkg/solution/testing/types.go` | 1 | All Go types, constants, validation |
| `pkg/solution/testing/types_test.go` | 1 | Type, marshal, validation unit tests |
| `pkg/solution/testing/sandbox.go` | 3 | Temp dir isolation, file copy, diff |
| `pkg/solution/testing/sandbox_test.go` | 3 | Sandbox unit tests |
| `pkg/solution/testing/inheritance.go` | 4 | Extends resolution, merge, cycle detection |
| `pkg/solution/testing/inheritance_test.go` | 4 | Inheritance unit tests |
| `pkg/solution/testing/discovery.go` | 4 | Solution/test discovery, filtering |
| `pkg/solution/testing/discovery_test.go` | 4 | Discovery unit tests |
| `pkg/solution/testing/context.go` | 5 | CEL assertion context building |
| `pkg/solution/testing/assertions.go` | 5 | All five assertion types + target routing |
| `pkg/solution/testing/assertions_test.go` | 5 | Assertion unit tests |
| `pkg/solution/testing/diagnostics.go` | 5 | CEL sub-expression failure diagnostics |
| `pkg/solution/testing/diagnostics_test.go` | 5 | Diagnostics unit tests |
| `pkg/solution/testing/snapshot.go` | 6 | Golden file comparison, normalization |
| `pkg/solution/testing/snapshot_test.go` | 6 | Snapshot unit tests |
| `pkg/solution/testing/builtins.go` | 6 | Four builtin test definitions |
| `pkg/solution/testing/builtins_test.go` | 6 | Builtin unit tests |
| `pkg/solution/testing/runner.go` | 7 | Core test execution engine |
| `pkg/solution/testing/runner_test.go` | 7 | Runner unit tests |
| `pkg/solution/testing/reporter.go` | 7 | kvx result formatting |
| `pkg/solution/testing/junit.go` | 7 | JUnit XML report writer |
| `pkg/cmd/scafctl/test/test.go` | 7 | `test` parent command |
| `pkg/cmd/scafctl/test/functional.go` | 7 | `test functional` command |
| `pkg/cmd/scafctl/test/functional_test.go` | 7 | Functional command tests |
| `pkg/cmd/scafctl/test/list.go` | 7 | `test list` command |
| `docs/tutorials/functional-testing.md` | 8 | User tutorial |
| `examples/solutions/tested-solution/` | 8 | Example solution with tests |
| `tests/integration/solutions/` | 8 | Self-hosted test fixtures |

### Modified Files (~6)

| File | Phase | Change |
|------|-------|--------|
| `pkg/exitcode/exitcode.go` | 1 | Add `TestFailed = 11` |
| `pkg/solution/spec.go` | 2 | Add `Tests`, `TestConfig` fields + helpers |
| `pkg/solution/bundler/compose.go` | 2 | Extend `composePart`, merge logic |
| `pkg/solution/bundler/discover.go` | 2 | Add `TestInclude` discovery source |
| `pkg/cmd/scafctl/root.go` | 7 | Register `test` command |
| `pkg/cmd/scafctl/lint/lint.go` | 8 | Three new lint rules |
| `tests/integration/cli_test.go` | 8 | CLI integration tests for `test` commands |

---

## Key Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| `testing` package name collision with Go stdlib | Use import alias `soltesting "pkg/solution/testing"` everywhere |
| `SkipBuiltinsValue` breaking compose round-trip | Unit test YAML marshal/unmarshal in Phase 1; integration test in Phase 2 |
| Global state leaks under `-j > 1` | Run `go test -race` in every phase; `Root()` isolation is already proven |
| CEL AST introspection complexity for diagnostics | Start with simple comparison detection (`==`, `!=`); degrade gracefully to "expected true, got false" for complex expressions |
| JUnit XML correctness | Validate against JUnit XSD or use a CI system to parse the output |
| Large scope (~21 new files, ~6 modified) | Phases are independently compilable and testable; progress is measurable per phase |
