---
description: "Go testing conventions for scafctl: table-driven tests, testify/assert, benchmarks, race detection, and coverage. Use when writing or editing Go test files."
applyTo: "**/*_test.go"
---

# Go Testing Conventions

## Framework

- Use standard `go test` with **table-driven tests**
- Use `testify/assert` for assertions
- Place mocks in `mock.go` files

## E2E Tests

E2E tests (`task test:e2e`) are expensive. Follow these rules:

1. Only run when validating a complete set of changes, not for iterative checks
2. Run **once** and capture output: `task test:e2e 2>&1 | tee /tmp/e2e-results.txt`
3. Review the saved file instead of re-running: `grep -E 'FAIL|PASS|ok' /tmp/e2e-results.txt`
4. For iterative development, run targeted unit tests: `go test ./pkg/some/package/...`

## Race Detection

Always run with the `-race` flag:

```bash
go test -race ./...
```

## Coverage

```bash
go test -cover ./...
```

### Coverage Profile Output

When generating coverage profiles (`-coverprofile`), always write to `temp/` (gitignored scratch directory). Never write coverage files to the repo root.

```bash
# Correct
go test -coverprofile temp/cover.out ./pkg/some/package/...

# Wrong -- pollutes repo root
go test -coverprofile cover.out ./pkg/some/package/...
```

### Coverage Targets

| Code Type | Package Target | Patch Target |
|-----------|---------------|-------------|
| Domain packages (`pkg/...`) | 80%+ | 80%+ |
| CLI commands (`pkg/cmd/...`) | 65%+ | 70%+ |
| Critical business logic | 90%+ | 100% |
| Generated code | Exclude | Exclude |

### Patch Coverage (CRITICAL)

Every PR must have **70%+ patch coverage** (percentage of new/changed lines covered by tests). This is enforced by Codecov.

- When adding new code, write tests for it in the same PR
- CLI command files (`pkg/cmd/`) are the most common offenders -- test RunE logic via integration tests or by extracting testable functions
- Never submit a new file with 0% coverage; at minimum test the happy path and one error path
- If a function is hard to test (e.g., cobra RunE with complex setup), extract the core logic into a helper function and test that

## Benchmarks

Add benchmark tests for any new features or providers:

```go
func BenchmarkMyFeature(b *testing.B) {
    b.ReportAllocs()
    b.ResetTimer()

    for b.Loop() {
        // benchmark code
    }
}
```

## Testing kvx TUI / `-i` output

CLI commands that render arrays via kvx cannot be visually verified by piping
(a non-TTY shell only shows the flat text fallback, never the card/detail TUI).
To assert the real interactive rendering, use kvx's snapshot API: convert the
data with `core.LoadObject`, parse the display schema with
`tui.ParseSchemaWithDisplay`, and render a fixed-size frame with
`tui.RenderSnapshot(root, tui.Config{Width, Height, DisplaySchema, StartKeys})`.
See the "kvx Rendering" section in
`.github/instructions/cli-commands.instructions.md` for the full harness and the
`core.LoadObject` gotcha (struct slices must be converted or the card view won't
trigger).

## Reference

See skill: `golang-testing` for testing patterns, benchmarks, fuzzing, and coverage.
