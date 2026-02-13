---
title: "Testing"
weight: 10
---

# Testing

## Purpose

Testing in scafctl ensures that solutions behave correctly, deterministically, and safely before performing side effects.

scafctl separates data resolution, transformation, and execution. Solution-level testing requires no mocks — render mode produces a fully concrete execution plan without side effects. At the package level, providers and services use test doubles to isolate external dependencies.

Testing is an outcome of the design, not a separate subsystem.

---

## Core Principles

- All data enters through resolvers
- All computation is provider-backed
- All side effects are isolated to actions
- Render mode produces a fully concrete execution plan

Because of this:

- Resolvers are unit-testable
- Providers are unit-testable
- Solutions are integration-testable without execution
- CLI commands are integration-testable via binary execution

---

## Testing Layers

scafctl supports five testing layers.

---

## 1. Resolver Tests (Unit)

### Scope

Resolver tests validate:

- resolve behavior
- transform logic
- validation rules
- emitted values
- dependency ordering

Resolvers are pure and deterministic. They do not perform side effects.

---

### What Is Tested

- Parameter overrides
- Default fallbacks
- Transform correctness
- Validation failures
- DAG dependency resolution

---

### Example

Given this resolver:

~~~yaml
resolvers:
  environment:
    resolve:
      with:
        - provider: parameter
          inputs:
            key: env
        - provider: static
          inputs:
            value: dev

    transform:
      with:
        - provider: cel
          inputs:
            expression: "__self.toLowerCase()"

    validate:
      with:
        - provider: validation
          inputs:
            expression: "__self in [\"dev\", \"staging\", \"prod\"]"
          message: "Invalid environment"
~~~

Test cases:

- `-r env=Prod` emits `prod`
- `-r env=foo` fails validation
- no parameter emits `dev`

---

### Execution Model

Resolver tests:

- run in-process
- do not require filesystem or network
- do not invoke actions
- do not require render mode

---

## 2. Provider Tests (Unit)

### Scope

Providers are tested independently of:

- resolvers
- actions
- CLI parsing

#### Unit

Providers have unit tests defined in their own code base.

#### Integration

Provider tests validate individual provider behavior given concrete inputs.

Provider behavior should also be testable when used within a solution. External dependencies (such as HTTP APIs or authentication systems) must be isolated using mocks, fakes, or test fixtures so that provider tests run deterministically without requiring real network access or credentials.

---

### What Is Tested

- Input schema validation
- Typed input handling
- Deterministic outputs
- Error conditions

---

### Examples

- Template provider renders expected output for given context
- Expression provider evaluates CEL correctly
- Filesystem provider reads and writes expected content in a temp directory
- API provider builds correct request structure (in render mode)

---

### Important Rule

Providers never receive expressions, templates, or resolver references.

All inputs are concrete by the time a provider executes.

---

## 3. Solution Tests (Integration, Render-Only)

### Scope

Solution tests validate the full solution behavior without executing side effects.

Render mode is the primary testing mechanism for solutions.

---

### Render Mode

In render mode, scafctl:

- executes all required resolvers
- evaluates expressions
- renders templates
- resolves conditions
- produces a fully concrete action graph

Render mode does not:

- perform filesystem writes
- make network calls
- execute shell commands

---

### What Is Tested

- Resolver integration
- Action ordering
- foreach expansion
- provider inputs
- conditional logic
- templated output content

---

### Example

~~~bash
scafctl render solution terraform-scaffold \
  -r environments=dev,prod
~~~

Assertions may include:

- two action instances are rendered
- paths are correct
- template outputs match snapshots
- inputs contain no expressions or placeholders

---

### Snapshot Testing

Rendered output may be snapshot-tested.

This is equivalent to:

- `terraform plan`
- `helm template`
- `kubectl apply --dry-run`

Snapshots are stable because render output is deterministic.

#### Golden File Pattern

Snapshot fixtures live in `testdata/snapshots/` directories alongside the package under test. Each snapshot consists of:

- An input file (YAML) defining the test case
- An expected output file (JSON) containing the expected result

For example, `pkg/resolver/testdata/snapshots/` contains pairs like `simple_chain.yaml` and `simple_chain_expected.json`.

Tests load the input, execute the logic, and compare the output against the golden file. To update snapshots after intentional changes, use the `--snapshot` CLI flag or re-generate the expected output files.

---

## 4. CLI Tests (Integration)

### Scope

CLI integration tests validate end-to-end command behavior by building the scafctl binary and executing it as an external process. This layer covers:

- command routing and flag parsing
- stdout/stderr output format and content
- exit codes
- interaction between commands and solution files

---

### Structure

Tests live in `tests/integration/cli_test.go` and follow this pattern:

1. `TestMain` builds the binary once into a temp directory
2. `runScafctl(t, args...)` executes the binary, captures stdout, stderr, and exit code with a 30-second timeout
3. All tests use `t.Parallel()` for speed
4. Assertions use `testify/assert` and `testify/require`

---

### Example

~~~go
func TestVersionCommand(t *testing.T) {
    t.Parallel()
    stdout, stderr, exitCode := runScafctl(t, "version")
    assert.Equal(t, 0, exitCode)
    assert.Empty(t, stderr)
    assert.Contains(t, stdout, "scafctl")
}
~~~

---

### What Is Tested

- `version`, `help`, `render`, `run`, `explain`, `get`, `config`, `secrets`, `snapshot` commands
- Error output on invalid input
- Exit code correctness
- Output format (JSON, YAML, table, quiet)

---

### Adding New Commands

Every new CLI command must have corresponding tests added to `tests/integration/cli_test.go`.

---

## 5. Functional Tests (Solution-Level)

See [Functional Testing](functional-testing.md) for the full design of the `scafctl test functional` command, test spec format, assertion types, sandbox model, builtin tests, and CI integration.

---

## Test Doubles

### Convention

Use hand-rolled mock structs for test doubles. Place them in a `mock.go` file in the same package as the interface they implement.

Hand-rolled mocks follow a consistent pattern:

- Configurable return values and errors as exported struct fields
- Call tracking with counters or argument slices
- Thread safety via `sync.Mutex` or `sync.RWMutex`

~~~go
type MockStore struct {
    mu sync.RWMutex

    Data      map[string][]byte  // configurable return data
    GetErr    error              // error injection
    GetCalls  []string           // call tracking
}

func (m *MockStore) Get(ctx context.Context, name string) ([]byte, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.GetCalls = append(m.GetCalls, name)
    if m.GetErr != nil {
        return nil, m.GetErr
    }
    return m.Data[name], nil
}
~~~

This style avoids extra dependencies and keeps test doubles simple and readable.

---

## Testing Utilities

### IOStreams

Use `terminal.NewTestIOStreams()` to create an `IOStreams` instance backed by buffers for asserting against stdout and stderr:

~~~go
streams, outBuf, errBuf := terminal.NewTestIOStreams()
// ... execute command with streams ...
assert.Contains(t, outBuf.String(), "expected output")
~~~

### Writer

Use `writer.New()` with `writer.WithExitFunc()` to intercept `os.Exit` calls in tests:

~~~go
streams, outBuf, errBuf := terminal.NewTestIOStreams()
w := writer.New(streams, cliParams, writer.WithExitFunc(func(code int) {
    // capture exit code instead of exiting
}))
~~~

Retrieve the writer from context in production code with `writer.FromContext(ctx)` or `writer.MustFromContext(ctx)`.

### Logger

Use `logger.FromContext(ctx)` for context-aware loggers. In tests, create a no-op or buffer-backed logger and inject it with `logger.WithLogger(ctx, lgr)`.

---

## Benchmark Tests

Performance-sensitive packages should include benchmark tests. Benchmarks currently exist in:

- `pkg/celexp/` — CEL compilation, evaluation, cache key generation
- `pkg/celexp/conversion/` — type conversion performance
- `pkg/celexp/env/` — global environment cache access

Benchmarks follow standard Go conventions:

~~~go
func BenchmarkCompile(b *testing.B) {
    for b.Loop() {
        // code under benchmark
    }
}
~~~

Run benchmarks with:

~~~bash
go test -bench=. -benchmem ./pkg/celexp/...
~~~

Add benchmarks when:

- introducing hot-path code (expression evaluation, template rendering)
- adding or modifying caching logic
- optimizing existing performance-critical paths

---

## What Is Not Tested

The following are intentionally not tested at the solution level:

- shell command correctness
- API side effect behavior
- external system availability

These concerns are tested at the package unit-test level where appropriate (e.g., `pkg/secrets/`, `pkg/auth/`), using mock implementations to isolate external dependencies. At the solution level, testing focuses on the declarative execution plan, not runtime side effects.

---

## CI Usage

CI pipelines use the following task chain:

~~~bash
task test:e2e   # runs: info → lint → test-cover → integration
~~~

This breaks down into:

| Step | Command | Purpose |
| ---- | ------- | ------- |
| Unit tests | `go test ./... -cover` | All package-level unit tests with coverage |
| Lint | `golangci-lint run` | Static analysis and code quality |
| Integration | `scafctl test functional --tests-path tests/integration/solutions` | Solution-level functional tests |

For Go integration tests (CLI tests):

~~~bash
go test -v ./tests/integration/...
~~~

No side effects are required for any CI step.

---

## Why This Model Works

Compared to imperative task runners:

- no shell simulation is required
- no environment guessing is required

Everything is explicit, declarative, and inspectable.

Testing works because the architecture enforces:

- purity where required
- isolation where required
- determinism everywhere else

---

## Summary

scafctl testing is structured around its execution model.

- Resolvers are unit-tested
- Providers are unit-tested
- Solutions are render-tested via `scafctl render` and `scafctl test functional`
- CLI commands are integration-tested via binary execution
- Performance-critical paths are benchmarked

Hand-rolled mock structs in `mock.go` files provide test doubles at the package level. Test utilities (`NewTestIOStreams`, `writer.WithExitFunc`) enable output capture without testing-specific abstractions in production code.
