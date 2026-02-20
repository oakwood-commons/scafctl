# Consolidate `Tests` & `TestConfig` into `spec.testing`

## Summary

Move `Spec.Tests` and `Spec.TestConfig` from being sibling fields on the solution `Spec` struct into a single `Spec.Testing *soltesting.TestSuite` field. This groups all test-related configuration under one top-level property.

**This is a breaking change** — all existing solution YAML files must restructure their `spec:` section.

## Before / After

### YAML Structure

**Before:**

```yaml
spec:
  resolvers: ...
  workflow: ...
  tests:
    my-test:
      command: apply
      args: {}
  testConfig:
    skipBuiltins: true
    env:
      FOO: bar
```

**After:**

```yaml
spec:
  resolvers: ...
  workflow: ...
  testing:
    config:
      skipBuiltins: true
      env:
        FOO: bar
    cases:
      my-test:
        command: apply
        args: {}
```

### Go Types

**Before:**

```go
type Spec struct {
    Resolvers  map[string]*resolver.Resolver      `json:"resolvers,omitempty" yaml:"resolvers,omitempty"`
    Workflow   *action.Workflow                    `json:"workflow,omitempty" yaml:"workflow,omitempty"`
    Tests      map[string]*soltesting.TestCase     `json:"tests,omitempty" yaml:"tests,omitempty"`
    TestConfig *soltesting.TestConfig              `json:"testConfig,omitempty" yaml:"testConfig,omitempty"`
}
```

**After:**

```go
type Spec struct {
    Resolvers map[string]*resolver.Resolver `json:"resolvers,omitempty" yaml:"resolvers,omitempty"`
    Workflow  *action.Workflow              `json:"workflow,omitempty" yaml:"workflow,omitempty"`
    Testing   *soltesting.TestSuite         `json:"testing,omitempty" yaml:"testing,omitempty"`
}
```

**New `TestSuite` struct** (in `pkg/solution/soltesting`):

```go
type TestSuite struct {
    Config *TestConfig             `json:"config,omitempty" yaml:"config,omitempty" doc:"Test configuration"`
    Cases  map[string]*TestCase    `json:"cases,omitempty" yaml:"cases,omitempty" doc:"Test case definitions keyed by name"`
}
```

## Naming Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Wrapper YAML key | `testing` | Intuitive, concise, clearly scopes what's inside |
| Test map sub-field | `cases` | Avoids redundancy (`testing.tests` reads poorly) |
| Config sub-field | `config` | Clean — `testing.config` is self-explanatory |
| Go struct name | `soltesting.TestSuite` | Conventional, clear, aligns with existing patterns |

## Implementation Plan

### Phase 1: Core Type Changes

#### 1. Add `TestSuite` struct to `pkg/solution/soltesting/types.go`

Create the new wrapper struct with `Cases` and `Config` fields, appropriate JSON/YAML tags, and Huma `doc` tags. Add convenience methods:

- `HasCases() bool`
- `HasConfig() bool`

#### 2. Update `Spec` struct in `pkg/solution/spec.go`

- Remove `Tests` and `TestConfig` fields
- Add `Testing *soltesting.TestSuite` field (json/yaml: `testing`)
- Update `HasTests()` to delegate to `s.Testing.HasCases()`
- Update `HasTestConfig()` to delegate to `s.Testing.HasConfig()`
- Add `HasTesting()` convenience method

#### 3. Update `pkg/solution/spec_test.go`

Update all `sol.Spec.Tests` → `sol.Spec.Testing.Cases` and `sol.Spec.TestConfig` → `sol.Spec.Testing.Config` references. Update test function names/assertions.

### Phase 2: soltesting Package Internal Updates

#### 4. Update `SolutionTests` in `pkg/solution/soltesting/discovery.go`

- Update `SolutionTests` struct: rename `Tests` → `Cases`, `TestConfig` → `Config` for consistency
- Update the inline anonymous struct (used for lightweight YAML parsing) to use the new `testing:` nesting
- Update all `st.Tests` → `st.Cases` and `st.TestConfig` → `st.Config` references (~20 lines)

#### 5. Update `pkg/solution/soltesting/discovery_test.go`

Update struct literal references (~25 lines).

#### 6. Update `pkg/solution/soltesting/runner.go`

Update `st.Tests` → `st.Cases` and `st.TestConfig` → `st.Config` references (~30 lines).

#### 7. Update `pkg/solution/soltesting/runner_test.go`

Update `SolutionTests` struct literals (~12 instances).

#### 8. Update `pkg/solution/soltesting/reporter.go`

Update `st.TestConfig` and `st.Tests` references.

#### 9. Update `ScaffoldResult` in `pkg/solution/soltesting/scaffold.go`

Change `Tests map[string]*TestCase` to output the new nested structure. The scaffold output (for `test init`) should emit YAML matching the new `testing.cases` shape.

#### 10. Update `pkg/solution/soltesting/scaffold_test.go`

Update `result.Tests` references (~20 lines).

#### 11. Update inline structs in `pkg/solution/soltesting/watch.go`

Update inline anonymous struct and `doc.Spec.Tests` references.

#### 12. Update `pkg/solution/soltesting/generate.go`

Update comment references to `spec.tests` and YAML output format.

### Phase 3: Bundler/Compose Changes

#### 13. Update `composePart` in `pkg/solution/bundler/compose.go`

- Update inline struct to use new `Testing` field
- Refactor `mergeTests()` and `mergeTestConfig()` to operate on `merged.Spec.Testing.Cases` and `merged.Spec.Testing.Config`
- Ensure nil-safety (initialize `Testing` if nil before merging)

#### 14. Update `pkg/solution/bundler/compose_test.go`

Update `result.Spec.Tests`/`result.Spec.TestConfig` references (~30 lines).

#### 15. Update `pkg/solution/bundler/discover.go`

Update `sol.Spec.Tests` reference and comment.

### Phase 4: CLI Command Updates

#### 16. Update `pkg/cmd/scafctl/lint/lint.go`

Update `sol.Spec.HasTests()`, `sol.Spec.Tests` references to use new paths through `Testing`.

#### 17. Update `pkg/cmd/scafctl/test/functional.go`

Update `solutions[i].TestConfig` references.

#### 18. Update CLI help text in `pkg/cmd/scafctl/test/test.go` and `pkg/cmd/scafctl/test/init.go`

Update doc strings referencing `spec.tests` to `spec.testing.cases`.

#### 19. Update `pkg/mcp/tools_examples.go`

Update description referencing `spec.tests`.

### Phase 5: YAML Files (~43 files)

#### 20. Update example solution

[`examples/solutions/tested-solution/solution.yaml`](../../examples/solutions/tested-solution/solution.yaml): restructure from `spec.tests`/`spec.testConfig` to `spec.testing.cases`/`spec.testing.config`.

#### 21. Update all integration test solution YAML files

All files in [`tests/integration/solutions/`](../../tests/integration/solutions/): same restructuring. Each file's `spec:` section needs `tests:` and `testConfig:` moved under a new `testing:` block, renamed to `cases:` and `config:`.

#### 22. Update compose partial YAML files

- `tests/integration/solutions/composed/tests/rendering.yaml`
- `tests/integration/solutions/composition/parts/tests.yaml`

### Phase 6: Inline YAML in Go Tests

#### 23. Update inline YAML snippets in `tests/integration/cli_test.go`

~9 embedded YAML strings containing `tests:` and `testConfig:` keys need restructuring to the new nesting.

### Phase 7: Documentation

#### 24. Rewrite `docs/design/functional-testing.md`

~50 references to `spec.tests`, `spec.testConfig`, YAML examples, struct references. All need updating to `spec.testing.cases`/`spec.testing.config`.

#### 25. Rewrite `docs/tutorials/functional-testing.md`

~15 references + YAML examples.

### Phase 8: Verification

#### 26. Build, test, and lint

- `go build ./...` — compiles cleanly
- `go test ./...` — all unit tests pass
- `golangci-lint run` — no lint issues
- Integration tests pass (validates YAML restructuring)
- Manual review of `test init` scaffold output — confirms new YAML shape
- Verify JSON schema output in `pkg/schema/solution_schema.go` reflects `testing.cases`/`testing.config`

## Scope Summary

| Category | Count |
|----------|-------|
| Go source files | ~21 |
| YAML solution files | ~43 |
| Inline YAML in Go tests | ~9 snippets in 1 file |
| Documentation files | 2 |
| **Total files affected** | **~67** |

## Notes

- **Backward compatibility**: Not required per project conventions. This is a **breaking change** for all existing solution YAML files.
- **`SolutionTests`** in `soltesting` already groups `Tests` + `TestConfig` — after this refactor its fields should be aligned to `Cases`/`Config` for consistency.
- **Schema auto-generation**: The JSON schema in `pkg/schema/solution_schema.go` is auto-generated from struct reflection via Huma tags. It will update automatically when the `Spec` struct changes, but output should be verified.
- **`ScaffoldResult`**: The `test init` command output format changes to match the new nesting structure.
