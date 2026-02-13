---
title: "Functional Testing"
weight: 12
---

# Functional Testing

## Purpose

Functional testing validates that solutions behave correctly by executing scafctl commands against them and asserting on the output. Solution authors define test cases inline in their solution spec. The `scafctl test functional` command discovers tests, sets up isolated sandboxes, runs builtin and user-defined tests, and reports results.

This is the primary mechanism for validating solutions in CI and during development.

---

## Implementation Status

| Feature | Status | Notes |
| ------- | ------ | ----- |
| `test functional` CLI command | 📋 Planned | Not yet implemented |
| `test list` CLI subcommand | 📋 Planned | List tests without running |
| Test spec types | 📋 Planned | No Go types defined |
| Builtin tests | 📋 Planned | Parse, resolve, render, lint |
| Command-based test execution | 📋 Planned | Execute any scafctl subcommand |
| CEL assertions | 📋 Planned | Infrastructure exists in `pkg/celexp/` |
| Regex assertions | 📋 Planned | |
| Contains assertions | 📋 Planned | |
| Negation assertions | 📋 Planned | `notContains`, `notRegex` |
| Golden file snapshots | 📋 Planned | New implementation; `pkg/resolver/diff.go` is resolver-specific and not reusable for golden file comparison |
| Init scripts (exec provider) | 📋 Planned | |
| Test file includes | 📋 Planned | Bundle integration required |
| Temp directory sandbox | 📋 Planned | |
| JUnit XML reporting | 📋 Planned | |
| Compose support for tests | 📋 Planned | Compose merger needs extension |
| Parallel test execution | 📋 Planned | |
| CEL assertion diagnostics | 📋 Planned | Sub-expression evaluation for failures |
| Suite-level setup | 📋 Planned | Shared init across tests |
| Test tags and filtering | 📋 Planned | `--tag` flag, `tags` field on test cases |
| Per-test environment variables | 📋 Planned | `env` field on test cases |
| Cleanup steps | 📋 Planned | `cleanup` field, runs even on failure |
| Test inheritance (extends) | 📋 Planned | Multi-extends with `_` prefix templates |
| Assertion target (stderr) | 📋 Planned | `target` field: `stdout`, `stderr`, `combined` |
| File assertions (`output.files`) | 📋 Planned | Diff-based sandbox file change detection |
| Fail-fast (per-solution) | 📋 Planned | `--fail-fast` stops remaining tests per solution |
| Test name validation | 📋 Planned | Must match `^[a-zA-Z0-9][a-zA-Z0-9_-]*$` |
| Selective builtin skip | 📋 Planned | `skipBuiltins` accepts `bool` or `[]string` |
| In-process command execution | 📋 Planned | Cobra tree invocation, custom `exitFunc` |
| Concurrency control | 📋 Planned | `-j` flag, `--sequential` as sugar for `-j 1` |
| Conditional skip (`skipExpression`) | 📋 Planned | CEL-based runtime skip evaluation |
| Test retries | 📋 Planned | `retries` field for flaky test resilience |
| Suite-level cleanup | 📋 Planned | `testConfig.cleanup` for teardown after all tests |
| File size guard | 📋 Planned | Cap `files[].content` at 10MB to prevent OOM |
| In-process execution safety | 📋 Planned | Mutex serialization to avoid `Root()` data races |
| Unused template lint warning | 📋 Planned | Warn on templates never referenced via `extends` |

---

## Test Definition

### Location

Tests are defined under `spec.tests` in the solution YAML. Like resolvers, tests support the `compose` mechanism for splitting into separate files.

---

### Inline Example

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: terraform-scaffold
spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
          - provider: static
            inputs:
              value: dev
      validate:
        with:
          - provider: validation
            inputs:
              expression: '__self in ["dev", "staging", "prod"]'
            message: "Invalid environment"
  workflow:
    actions:
      render-main:
        provider: template
        inputs:
          template:
            tmpl: "main.tf.tmpl"
          output: "{{environment}}/main.tf"
  tests:
    _base-render:
      description: "Base render test template"
      command: [render, solution]
      assertions:
        - expression: 'size(output.actions) >= 1'

    renders-dev-defaults:
      description: "Default environment renders dev configuration"
      extends: [_base-render]
      tags: [smoke, render]
      assertions:
        - expression: 'size(output.actions) == 1'
          message: "Should produce exactly one action"
        - contains: "dev/main.tf"

    renders-prod-with-resolver-run:
      description: "Run resolver with prod override"
      command: [run, resolver]
      args: ["-r", "env=prod"]
      tags: [resolver]
      assertions:
        - expression: 'output.environment == "prod"'
        - regex: '"environment":\s*"prod"'

    render-prod-override:
      description: "Render with prod override produces correct paths"
      extends: [_base-render]
      args: ["-r", "env=prod"]
      tags: [render]
      assertions:
        - expression: 'output.actions["render-main"].inputs.output == "prod/main.tf"'

    rejects-invalid-env:
      description: "Invalid environment fails validation"
      command: [run, resolver]
      args: ["-r", "env=invalid"]
      expectFailure: true
      tags: [validation]
      assertions:
        - contains: "Invalid environment"
        - notContains: "panic"
        - contains: "validation"
          target: stderr

    passes-lint:
      description: "Solution passes lint with no errors"
      command: [lint]
      tags: [lint]
      assertions:
        - expression: 'output.errorCount == 0'

    snapshot-action-graph:
      description: "Action graph matches golden file"
      command: [render, solution]
      args: ["-r", "env=dev"]
      tags: [snapshot]
      snapshot: "testdata/expected-render.json"

    renders-with-setup:
      description: "Render with custom setup and cleanup"
      command: [render, solution]
      env:
        CUSTOM_VAR: "test-value"
      init:
        - command: "mkdir -p templates"
      cleanup:
        - command: "echo 'cleanup complete'"
      assertions:
        - expression: 'size(output.actions) >= 1'
        - expression: 'output.files["dev/main.tf"].exists'

    temporarily-disabled:
      description: "This test is skipped during development"
      skip: true
      skipReason: "Waiting on upstream provider fix"
      command: [render, solution]
      assertions:
        - expression: 'size(output.actions) == 1'
~~~

---

### Composed into Separate Files

~~~yaml
# solution.yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: terraform-scaffold
compose:
  - resolvers/environment.yaml
  - tests/rendering.yaml
  - tests/validation.yaml
spec:
  workflow:
    actions:
      render-main:
        provider: template
        inputs:
          template:
            tmpl: "main.tf.tmpl"
          output: "{{environment}}/main.tf"
~~~

~~~yaml
# tests/rendering.yaml
spec:
  tests:
    renders-dev-defaults:
      description: "Default environment renders dev configuration"
      command: [render, solution]
      tags: [smoke, render]
      assertions:
        - expression: 'size(output.actions) == 1'

    renders-prod-override:
      description: "Render with prod override produces correct paths"
      command: [render, solution]
      args: ["-r", "env=prod"]
      tags: [render]
      assertions:
        - expression: 'output.actions["render-main"].inputs.output == "prod/main.tf"'
~~~

~~~yaml
# tests/validation.yaml
spec:
  tests:
    rejects-invalid-env:
      description: "Invalid environment fails validation"
      command: [run, resolver]
      args: ["-r", "env=invalid"]
      expectFailure: true
      tags: [validation]
      assertions:
        - contains: "Invalid environment"
~~~

---

## Test Case Spec

Each test case is a named entry under `spec.tests`. Test names must match `^[a-zA-Z0-9][a-zA-Z0-9_-]*$` (letters, numbers, hyphens, underscores; must start with a letter or number). Names starting with `_` are test templates — they are not executed directly but can be inherited via `extends`.

~~~yaml
tests:
  <test-name>:
    description: <string>
    command: <list[string]>
    args: <list[string]>
    extends: <list[string]>
    tags: <list[string]>
    env: <map[string, string]>
    files: <list[string]>
    init: <list[InitStep]>
    cleanup: <list[InitStep]>
    assertions: <list[Assertion]>
    snapshot: <string>
    injectFile: <bool>
    expectFailure: <bool>
    exitCode: <int>
    timeout: <duration>
    skip: <bool>
    skipReason: <string>
    skipExpression: <Expression>
    retries: <int>
~~~

### Field Details

| Field | Type | Required | Default | Description |
| ----- | ---- | -------- | ------- | ----------- |
| `description` | `string` | Yes | — | Human-readable test description |
| `command` | `[]string` | No | `[render, solution]` | scafctl subcommand as an array (e.g., `[render, solution]`, `[run, resolver]`, `[lint]`). By default the runner auto-injects `-f <sandbox-path>` — set `injectFile: false` to disable |
| `args` | `[]string` | No | `[]` | Additional CLI flags appended after the command. `-f` must never be included here — use `injectFile` to control file injection |
| `extends` | `[]string` | No | `[]` | Names of test templates to inherit from. Applied left-to-right; this test's fields override inherited values. See [Test Inheritance](#test-inheritance) |
| `tags` | `[]string` | No | `[]` | Tags for categorization and filtering. Use `--tag` to run only tests with matching tags |
| `env` | `map[string]string` | No | `{}` | Environment variables set for this test's init, command, and cleanup steps. Merged with process environment |
| `files` | `[]string` | No | `[]` | Relative paths or globs for files required by this test. Supports `**` recursive globs |
| `init` | `[]InitStep` | No | `[]` | Setup steps executed sequentially before the command |
| `cleanup` | `[]InitStep` | No | `[]` | Teardown steps executed after the command, even on failure. See [Cleanup Steps](#cleanup-steps) |
| `assertions` | `[]Assertion` | Conditional | — | Required unless `snapshot` is set. All assertions are evaluated regardless of prior failures |
| `snapshot` | `string` | No | — | Relative path to a golden file for normalized comparison |
| `injectFile` | `bool` | No | `true` | When `true` (default), the runner auto-injects `-f <sandbox-solution-path>`. Set to `false` for commands that don't accept `-f` (e.g., `config get`, `auth status`) or for catalog solution tests that use `--catalog` instead. `-f` must never appear in `args` regardless of this setting |
| `expectFailure` | `bool` | No | `false` | When `true`, the test passes if the command exits non-zero |
| `exitCode` | `int` | No | — | Exact expected exit code. Takes precedence over `expectFailure` |
| `timeout` | `duration` | No | `30s` | Per-test timeout |
| `skip` | `bool` | No | `false` | Skip this test |
| `skipReason` | `string` | No | — | Human-readable reason for skipping. Shown in test output |
| `skipExpression` | `Expression` | No | — | CEL expression evaluated at discovery time. If `true`, the test is skipped with the expression as the reason. Context variables: `os` (GOOS), `arch` (GOARCH), `env` (environment variables map). Example: `'os == "windows"'` |
| `retries` | `int` | No | `0` | Number of retry attempts for a failing test. The test passes if any attempt succeeds. Retry count shown in output: `PASS (retry 2/3)` |

---

## Test Configuration

Solution-level test configuration is defined under `spec.testConfig`:

~~~yaml
spec:
  testConfig:
    skipBuiltins: true
    setup:
      - command: "scafctl config set defaults.environment staging"
      - command: "mkdir -p templates"
    cleanup:
      - command: "echo 'suite teardown complete'"
~~~

`skipBuiltins` accepts either a boolean or a list of builtin test names for selective skipping:

~~~yaml
# Skip all builtins
testConfig:
  skipBuiltins: true

# Skip only specific builtins
testConfig:
  skipBuiltins:
    - resolve-defaults
    - render-defaults
~~~

| Field | Type | Default | Description |
| ----- | ---- | ------- | ----------- |
| `skipBuiltins` | `bool \| []string` | `false` | Disable builtin tests. `true` disables all; a list of names disables only those builtins (e.g., `["resolve-defaults"]`) |
| `setup` | `[]InitStep` | `[]` | Suite-level setup steps. Run once, then the resulting sandbox is copied per-test |
| `cleanup` | `[]InitStep` | `[]` | Suite-level teardown steps. Run once after all tests for the solution complete, even on failure. Symmetric with `setup` |

### Suite-Level Setup

When `testConfig.setup` is defined:

1. Create a base sandbox and copy the solution + bundle files
2. Run `setup` steps sequentially in the base sandbox
3. For each test case, copy the prepared base sandbox to an isolated per-test sandbox
4. Run per-test `init` steps, then the command

This avoids duplicating the same init steps across every test. If any setup step fails, all tests for that solution report as `error`.

### Compose Merge Semantics

When `testConfig` appears in multiple compose files:

| Field | Merge Behavior |
| ----- | -------------- |
| `skipBuiltins` (bool) | `true` wins — if any compose file sets `skipBuiltins: true`, all builtins are skipped |
| `skipBuiltins` (list) | Unioned (deduplicated) across all compose files |
| `setup` | Appended in compose-file order (first file's steps run first). This is a new merge strategy distinct from the existing reject-duplicates and union patterns |
| `cleanup` | Appended in compose-file order (first file's steps run first) |

> **Note**: `SkipBuiltinsValue` requires both `UnmarshalYAML` and `MarshalYAML` implementations to survive the `deepCopySolution` YAML round-trip used in compose.

---

## Init Scripts

Tests can define setup steps that run before the test command. Init steps execute sequentially in the sandbox directory. Init uses the exec provider's input schema, giving access to all execution options.

~~~yaml
tests:
  renders-with-custom-config:
    description: "Renders with custom configuration"
    init:
      - command: "mkdir -p templates && echo '# Generated' > templates/main.tf.tmpl"
      - command: "scafctl config set defaults.environment staging"
        env:
          SCAFCTL_CONFIG_DIR: "$SCAFCTL_SANDBOX_DIR"
      - command: "echo 'setting up test data'"
        shell: bash
        timeout: 10
        workingDir: "templates"
~~~

### InitStep

Init steps accept the same fields as the exec provider:

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `command` | `string` | Yes | Command to execute. Supports POSIX shell syntax (pipes, redirections, variables) |
| `args` | `[]string` | No | Additional arguments, automatically shell-quoted |
| `stdin` | `string` | No | Standard input to provide to the command |
| `workingDir` | `string` | No | Working directory (relative to sandbox root) |
| `env` | `map[string]string` | No | Environment variables merged with the parent process |
| `timeout` | `int` | No | Timeout in seconds (default: 30) |
| `shell` | `string` | No | Shell interpreter: `auto` (default), `sh`, `bash`, `pwsh`, `cmd` |

Init failures cause the test to report as `error` (not `fail`). Stdout/stderr from init steps are captured and included in verbose output for debugging.

### Environment Variables

The test runner automatically injects the following environment variables into every init step and test command:

| Variable | Description |
| -------- | ----------- |
| `SCAFCTL_SANDBOX_DIR` | Absolute path to the sandbox directory |

These are standard process environment variables — no custom template syntax.

---

## Test Files

Tests can declare additional files required for execution. These files are copied into the sandbox alongside the solution.

~~~yaml
spec:
  tests:
    renders-with-custom-template:
      description: "Renders with a test-specific template"
      files:
        - testdata/custom-main.tf.tmpl
        - testdata/variables.json
      command: [render, solution]
      assertions:
        - expression: 'size(output.actions) >= 1'

bundle:
  include:
    - testdata/**
~~~

### How Files Work

| Phase | Behavior |
| ----- | -------- |
| **Development** | `files` paths are resolved relative to the solution directory. The runner copies them into the sandbox before init/command execution |
| **Build** | `scafctl build` auto-discovers files referenced in `spec.tests[*].files` and includes them in the bundle artifact as a `TestInclude` discovery source |
| **Lint** | `scafctl lint` produces an **error** if test files are not covered by `bundle.include` patterns. Tests must work from remote catalog artifacts |
| **Bundle extraction** | Test files are extracted alongside solution files when a bundled solution is unpacked |

Files are copied into the sandbox maintaining their relative directory structure. Path traversal above the solution root (`..`) is rejected. Symlinks are not supported and are rejected.

---

## Test Inheritance

Tests can inherit from template tests using the `extends` field. This reduces duplication when many tests share common configuration.

### Template Tests

Test names starting with `_` are **templates** — they are not executed directly and do not appear in test output. They exist only to be inherited by other tests.

`scafctl lint` produces a **warning** for templates that are never referenced by any `extends` field. Template-only compose files (containing only `_`-prefixed tests) are valid.

### Extends Rules

- `extends` accepts a list of test template names, applied left-to-right
- The extending test's fields override inherited values
- Circular extends chains are detected and rejected
- Templates can extend other templates

### Field Merge Strategy

| Field | Merge Behavior |
| ----- | -------------- |
| `command` | Child wins if set |
| `args` | Appended (base args first, then child args) |
| `assertions` | Appended (base assertions first, then child assertions) |
| `files` | Appended (deduplicated) |
| `init` | Base init steps prepended before child init steps |
| `cleanup` | Base cleanup steps appended after child cleanup steps |
| `tags` | Appended (deduplicated) |
| `env` | Merged map (child values override base on key conflict) |
| `description` | Child wins if set |
| `timeout` | Child wins if set |
| `expectFailure` | Child wins if set |
| `exitCode` | Child wins if set |
| `skip` | Child wins if set |
| `injectFile` | Child wins if set |
| `snapshot` | Child wins if set |
| `skipExpression` | Child wins if set |
| `retries` | Child wins if set |

### Example

~~~yaml
tests:
  _base-render:
    description: "Base render test"
    command: [render, solution]
    tags: [render]
    assertions:
      - expression: 'size(output.actions) >= 1'

  _base-prod:
    description: "Base prod test"
    args: ["-r", "env=prod"]
    tags: [prod]

  render-prod:
    description: "Render prod configuration"
    extends: [_base-render, _base-prod]
    assertions:
      - expression: 'output.actions["render-main"].inputs.output == "prod/main.tf"'
~~~

The resolved `render-prod` test inherits:
- `command: [render, solution]` from `_base-render`
- `args: ["-r", "env=prod"]` from `_base-prod`
- `tags: [render, prod]` merged from both bases
- `assertions`: all three assertions (one from `_base-render`, one from `render-prod` itself)
- `description: "Render prod configuration"` overridden by the child

---

## Cleanup Steps

Tests can define cleanup steps that run after the test command, **even if the command or assertions fail**. Cleanup uses the same `InitStep` schema as `init`.

~~~yaml
tests:
  renders-with-temp-state:
    description: "Render with temporary state file"
    command: [render, solution]
    init:
      - command: "echo '{"key": "value"}' > state.json"
    cleanup:
      - command: "echo 'cleanup complete'"
    assertions:
      - expression: 'size(output.actions) >= 1'
~~~

Cleanup steps:
- Execute sequentially in the sandbox directory
- Run even when the test command fails, init fails, or assertions fail
- Cleanup failures are logged in verbose output but do not change the test status (the original pass/fail/error result is preserved)
- Have access to the same environment variables as init steps (`SCAFCTL_SANDBOX_DIR`, per-test `env`)

---

## Test Tags

Tests can be tagged for categorization and selective execution.

~~~yaml
tests:
  renders-dev:
    description: "Render dev config"
    command: [render, solution]
    tags: [smoke, render, fast]
    assertions:
      - expression: 'size(output.actions) >= 1'
~~~

Filter tests by tag using the `--tag` flag:

~~~bash
# Run only tests tagged "smoke"
scafctl test functional -f solution.yaml --tag smoke

# Combine with name filter
scafctl test functional -f solution.yaml --tag render --filter "*prod*"
~~~

A test matches the `--tag` filter if it has **any** of the specified tags. Tags inherited via `extends` are included in the match.

---

## Test Name Validation

Test names must match the pattern `^[a-zA-Z0-9][a-zA-Z0-9_-]*$`:

- Must start with a letter or digit
- May contain letters, digits, hyphens (`-`), and underscores (`_`)
- Template names starting with `_` are the exception — they must match `^_[a-zA-Z0-9][a-zA-Z0-9_-]*$`

This constraint ensures compatibility with JUnit XML output, CLI `--filter` glob matching, and compose file merge keys.

Invalid names are rejected during YAML parsing and surfaced as lint errors.

---

## Builtin Tests

Every solution automatically receives builtin tests unless `testConfig.skipBuiltins` is set. Builtins validate baseline correctness without requiring explicit test definitions.

| Builtin Test | Command | Passes When |
| ------------ | ------- | ----------- |
| `builtin:parse` | (internal) | Solution YAML parses without errors |
| `builtin:lint` | `lint` | No lint errors (warnings allowed) |
| `builtin:resolve-defaults` | `run resolver` | All resolvers resolve with default values |
| `builtin:render-defaults` | `render solution` | Render succeeds with default values |

Builtins run before user-defined tests. By default, if a builtin fails, user-defined tests still run (they are independent). Use `--fail-fast` to stop remaining tests for that solution on first failure.

Selective skipping is supported via `testConfig.skipBuiltins` — set to `true` to skip all, or provide a list of specific builtin names (without the `builtin:` prefix) to skip only those.

---

## Assertions

Each assertion has exactly one of `expression`, `regex`, `contains`, `notRegex`, or `notContains`, plus an optional `message` and `target`. Exactly one assertion type must be set — this is enforced via runtime validation after YAML unmarshal.

**All assertions in a test are always evaluated**, regardless of whether earlier assertions fail. This ensures the user sees all problems at once rather than fixing them one at a time.

| Field | Type | Description |
| ----- | ---- | ----------- |
| `expression` | `Expression` | CEL expression evaluating to `bool`. Runs against structured output context |
| `regex` | `string` | Regex pattern that must match somewhere in the target text |
| `contains` | `string` | Substring that must appear in the target text |
| `notRegex` | `string` | Regex pattern that must NOT match anywhere in the target text |
| `notContains` | `string` | Substring that must NOT appear in the target text |
| `target` | `string` | Text to match against: `stdout` (default), `stderr`, or `combined` (stdout + stderr). Only applies to `regex`, `contains`, `notRegex`, `notContains`. CEL expressions access both via context variables |
| `message` | `string` | Custom failure message (optional). If omitted, the assertion itself is shown |

### Target Field

The `target` field controls which output stream `regex`, `contains`, `notRegex`, and `notContains` assertions match against:

~~~yaml
assertions:
  # Matches against stdout (default)
  - contains: "rendered successfully"

  # Matches against stderr
  - contains: "warning: deprecated field"
    target: stderr

  # Matches against combined stdout + stderr
  - notContains: "panic"
    target: combined

  # CEL expressions always have access to both via context variables
  - expression: 'stderr.contains("warning") && stdout.contains("success")'
~~~

The `target` field has no effect on `expression` assertions — CEL expressions access `stdout`, `stderr`, `exitCode`, `output`, and `files` as separate context variables.

### Assertion Context

#### How Output Is Captured

When a test executes a scafctl command:

1. The runner checks if the command supports `-o json` by calling `cmd.Flags().Lookup("output")` on the constructed cobra command after traversal. If the flag exists and the test's `args` don't already contain `-o` or `--output`, the runner appends `-o json`
2. Both raw and structured output are available to assertions

#### CEL Context Variables

| Variable | Type | Always Available | Description |
| -------- | ---- | ---------------- | ----------- |
| `stdout` | `string` | Yes | Raw stdout text |
| `stderr` | `string` | Yes | Raw stderr text |
| `exitCode` | `int` | Yes | Process exit code |
| `output` | `map[string, any]` | When `-o json` is supported | Parsed JSON output. **`nil` when the command doesn't support `-o json`**. CEL expressions referencing `output` when nil receive a diagnostic error: *"variable 'output' is nil — this command does not support structured output"* rather than a raw CEL error |
| `files` | `map[string, FileInfo]` | Yes | Files created or modified in the sandbox during command execution. Key is relative path. Each `FileInfo` has `exists` (bool) and `content` (string) |

The `output` variable structure depends on the command:

| Command | `output` structure |
| ------- | ------------------ |
| `render solution` | Action graph: `output.actions`, each with `provider`, `inputs`, `dependsOn`, `when` |
| `run resolver` | Resolver map: `output.<resolverName>` = resolved value |
| `run solution` | Execution result: `output.status`, `output.actions`, `output.duration` |
| `lint` | Lint result: `output.findings`, `output.errorCount`, `output.warnCount` |
| `snapshot diff` | Diff result: `output.added`, `output.removed`, `output.modified` |

#### File Assertions (`files` variable)

The `files` variable exposes files that were **created or modified** in the sandbox during command execution. The runner snapshots all file paths and modification times before the command runs, then diffs after execution. Only new or changed files appear in `files`.

~~~yaml
assertions:
  # Check that a file was created
  - expression: 'files["prod/main.tf"].exists'

  # Check file content
  - expression: 'files["prod/main.tf"].content.contains("resource")'

  # Check number of generated files
  - expression: 'size(files) == 3'
~~~

Each entry in `files` is keyed by the relative path from the sandbox root and has:
- `exists` (`bool`): always `true` for entries in the map (present for consistency)
- `content` (`string`): the full file content as a string

> **Size guard**: Files larger than 10MB have their `content` set to `"<file too large>"` and a warning is emitted in verbose output. This prevents OOM on solutions that generate large binary artifacts.

#### Regex and Contains Context

`regex`, `contains`, `notRegex`, and `notContains` assertions match against the stream specified by the `target` field (default: `stdout`). When `target` is `combined`, stdout and stderr are concatenated with a newline separator. This is useful for:

- Commands that don't support `-o json` (e.g., `explain solution`)
- Quick substring checks without CEL overhead
- Pattern matching on formatted output
- Ensuring sensitive values or panic traces don't appear in output

#### CEL Assertion Diagnostics

When a CEL assertion fails, the runner evaluates sub-expressions to provide actionable diagnostics rather than just "expected true, got false":

~~~
✗ expression: size(output.actions) == 3
  size(output.actions) = 5
  Expected 3, got 5
~~~

~~~
✗ expression: output.actions["render-main"].inputs.output == "prod/main.tf"
  output.actions["render-main"].inputs.output = "dev/main.tf"
  Expected "prod/main.tf", got "dev/main.tf"
~~~

The runner inspects comparison expressions (`==`, `!=`, `<`, `>`, `in`) and evaluates both sides independently to surface actual vs expected values.

---

## Snapshot Assertions

When `snapshot` is set, the test runner:

1. Executes the command and captures stdout
2. Normalizes the output through a fixed pipeline:
   - Sort JSON map keys deterministically
   - Replace ISO-8601 timestamps (`2006-01-02T15:04:05Z` patterns) with `<TIMESTAMP>`
   - Replace UUIDs (`[0-9a-f]{8}-[0-9a-f]{4}-...`) with `<UUID>`
   - Replace absolute paths matching the sandbox directory with `<SANDBOX>`
3. Compares against the golden file at the specified path (relative to solution directory)
4. On mismatch, displays a unified diff showing expected vs actual

> **Future**: A `snapshotScrubbers` field on the test case for custom regex replacements (e.g., replacing dynamic API keys or version strings). Not included in v1.

Snapshots can be used alongside other assertions — all must pass.

### Snapshot Diff Output

When a snapshot doesn't match, the failure output shows a unified diff:

~~~
✗ snapshot: testdata/expected-render.json
  --- expected
  +++ actual
  @@ -3,7 +3,7 @@
     "render-main": {
       "provider": "template",
       "inputs": {
  -      "output": "dev/main.tf"
  +      "output": "staging/main.tf"
       }
     }
~~~

### Updating Snapshots

~~~bash
scafctl test functional -f solution.yaml --update-snapshots
~~~

This re-runs all tests with `snapshot` fields and overwrites the golden files with actual output. Use a glob to selectively update:

~~~bash
scafctl test functional -f solution.yaml --update-snapshots --filter "snapshot-*"
~~~

---

## Execution Model

### In-Process Command Execution

The test runner executes scafctl commands **in-process** by invoking the cobra command tree directly, rather than shelling out to a scafctl binary. This is faster, avoids requiring a built binary on PATH, and simplifies output capture.

The runner:
1. Constructs the cobra root command using the same `Root()` function from `pkg/cmd/scafctl/root.go`
2. Sets the `command` array as args (e.g., `[render, solution]` → cobra traversal)
3. Injects `-f <sandbox-solution-path>` by default. Set `injectFile: false` on the test case to disable (e.g., for catalog solution tests). The runner **always errors** if `-f` appears in the test's `args`, regardless of `injectFile`
4. Redirects stdout/stderr to buffers for capture
5. Uses `writer.WithExitFunc()` to intercept `os.Exit` calls and convert them to `*exitcode.ExitError` values, preventing the test runner from terminating

### In-Process Execution Safety

`Root()` in `pkg/cmd/scafctl/root.go` uses 6 **package-level mutable variables** (`cliParams`, `configPath`, `appConfig`, `debugFlag`, `logFormat`, `logFile`). Calling `Root()` concurrently from multiple goroutines causes data races.

**Mitigation**: All in-process cobra invocations are serialized behind a `sync.Mutex`. Tests still run in parallel for sandbox setup, file copying, init steps, assertions, and cleanup — only the actual cobra command execution is serialized. Since commands themselves are fast (typically <100ms), this has minimal impact on total test duration.

> **Future**: Refactor `Root()` to accept a struct of options and eliminate package-level state, enabling fully parallel in-process execution. This is a larger change that can be done incrementally.

### Sandbox

Each test runs in an isolated temporary directory:

1. Copy the solution file and its bundle files to a temp directory
2. Copy test `files` into the sandbox (maintaining relative paths). Symlinks are rejected
3. Snapshot all file paths and modification times (for `output.files` diff)
4. Inject per-test `env` variables and `SCAFCTL_SANDBOX_DIR`
5. Run init steps in the sandbox
6. Execute the scafctl command in-process
7. Diff sandbox files against snapshot to populate `output.files`
8. Capture output and run assertions
9. Run cleanup steps (even on failure)
10. Clean up the temp directory (unless `--keep-sandbox` is set)

This ensures init scripts cannot modify source files.

### Discovery

The test runner discovers tests in two ways:

1. **Single solution**: `scafctl test functional -f solution.yaml` — runs tests defined in that solution
2. **Directory scan**: `scafctl test functional --tests-path path/to/solutions/` — recursively discovers all solution files and runs their `spec.tests`

Solutions with no `spec.tests` still run builtin tests (unless `skipBuiltins` is set).

Test templates (names starting with `_`) are resolved via `extends` but never executed directly.

### Execution Flow

For each test case:

1. Resolve `extends` chains and merge inherited fields
2. Validate test name matches `^[a-zA-Z0-9][a-zA-Z0-9_-]*$`
3. If `skip: true` → status `skip`, stop
4. If `skipExpression` is set → evaluate CEL expression with `os`, `arch`, `env` context. If `true` → status `skip` with expression as reason, stop
5. Create temp sandbox and copy solution + bundle files + test files
6. Snapshot sandbox file list and modification times
7. Run init steps sequentially; if any fails → status `error`, run cleanup, stop
8. Build the command: construct cobra tree with `<command> <args...>`. If `injectFile` is `true` (default), prepend `-f <sandbox-solution>`
9. If command supports `-o json` (detected via `cmd.Flags().Lookup("output")`) and test doesn't specify `-o` → append `-o json`
10. Inject `SCAFCTL_SANDBOX_DIR` and per-test `env` environment variables
11. Execute in-process with timeout (serialized behind mutex); capture stdout, stderr, exit code via `writer.WithExitFunc()`
12. Diff sandbox files against snapshot → populate `files` context variable
13. Parse JSON stdout if available → populate `output` context variable (nil if command doesn't support `-o json`)
14. Check exit code against `exitCode` or `expectFailure`
15. If `snapshot` is set → run snapshot comparison (show unified diff on mismatch)
16. Run **all** assertions (CEL against parsed output, regex/contains against target stream). All assertions always run regardless of prior failures
17. Run cleanup steps (even on failure or error)
18. All checks pass → `pass`; any check fails → `fail`
19. If `fail` and `retries > 0` → re-run from step 5 up to `retries` times. If any retry passes → `pass (retry N/M)`. Retry attempts are shown in verbose output

### Parallelism

Test cases run in parallel by default, limited by the `-j` / `--concurrency` flag (default: `runtime.NumCPU()`). Each test has its own sandbox, so there is no shared state. In-process cobra invocations are serialized behind a mutex (see [In-Process Execution Safety](#in-process-execution-safety)).

Use `--sequential` (sugar for `-j 1`) to disable parallel execution (useful for debugging).

### Fail-Fast

Use `--fail-fast` to stop executing remaining tests **for the current solution** on first failure. Tests for other solutions continue to run. This is useful for quick feedback during debugging.

Without `--fail-fast`, all tests for all solutions execute and all failures are reported.

### Timeouts

| Level | Default | Flag |
| ----- | ------- | ---- |
| Per-test | 30s | `--test-timeout` |
| Global | 5m | `--timeout` |
| Per-test override | — | `timeout` field in test spec |

---

## CLI Interface

### `scafctl test functional`

Discovers and runs functional tests.

~~~bash
scafctl test functional [flags]
~~~

#### Flags

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `-f`, `--file` | `string` | — | Path to a single solution file |
| `--tests-path` | `string` | — | Directory to scan for solution files |
| `-o`, `--output` | `string` | `table` | Output format: `table`, `json`, `yaml`, `quiet` |
| `--report-file` | `string` | — | Write JUnit XML report to this path |
| `--update-snapshots` | `bool` | `false` | Update golden files instead of comparing. Combine with `--filter` for selective updates |
| `--sequential` | `bool` | `false` | Disable parallel test execution (sugar for `-j 1`) |
| `-j`, `--concurrency` | `int` | `runtime.NumCPU()` | Maximum number of tests to run in parallel. In-process cobra invocations are always serialized regardless of this value |
| `--skip-builtins` | `bool` | `false` | Skip builtin tests for all solutions |
| `--test-timeout` | `duration` | `30s` | Per-test timeout |
| `--timeout` | `duration` | `5m` | Global timeout for all tests |
| `--filter` | `[]string` | — | Run only tests matching this name pattern (glob via `doublestar.Match`). Matches against the test name only (not `solution/test-name`). Builtin tests are matched with their full name including `builtin:` prefix. Multiple `--filter` flags allowed; a test runs if it matches any filter. Registered via `StringArrayVar` per project convention |
| `--tag` | `[]string` | — | Run only tests with these tags. Multiple `--tag` flags allowed (e.g., `--tag smoke --tag render`). A test matches if it has **any** of the specified tags. Registered via `StringArrayVar` per project convention |
| `--fail-fast` | `bool` | `false` | Stop remaining tests for the current solution on first failure. Other solutions continue |
| `-v`, `--verbose` | `bool` | `false` | Show full command, init output, and raw stdout/stderr |
| `--keep-sandbox` | `bool` | `false` | Preserve sandbox directories for failed tests |
| `--no-color` | `bool` | `false` | Disable colored output |
| `-q`, `--quiet` | `bool` | `false` | Only output failures |

#### Exit Codes

| Code | Constant | Meaning |
| ---- | -------- | ------- |
| 0 | `exitcode.Success` | All tests passed |
| 11 | `exitcode.TestFailed` (new) | One or more tests failed |
| 3 | `exitcode.InvalidInput` | Configuration or usage error |

---

### `scafctl test list`

Lists all tests without executing them.

~~~bash
scafctl test list [flags]
~~~

#### Flags

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
| `-f`, `--file` | `string` | — | Path to a single solution file |
| `--tests-path` | `string` | — | Directory to scan for solution files |
| `-o`, `--output` | `string` | `table` | Output format: `table`, `json`, `yaml`, `quiet` |
| `--include-builtins` | `bool` | `false` | Include builtin tests in the listing |
| `--tag` | `[]string` | — | Filter to tests with these tags. Multiple `--tag` flags allowed |

#### Example Output

~~~
SOLUTION             TEST                        COMMAND            TAGS                 SKIP
terraform-scaffold   renders-dev-defaults        render solution    smoke,render         -
terraform-scaffold   renders-prod-override       render solution    render               -
terraform-scaffold   rejects-invalid-env         run resolver       validation           -
terraform-scaffold   temporarily-disabled        render solution                         Waiting on upstream provider fix
~~~

---

## Output

### Table (default)

~~~
SOLUTION             TEST                        STATUS   DURATION
terraform-scaffold   builtin:parse               PASS     1ms
terraform-scaffold   builtin:lint                PASS     45ms
terraform-scaffold   builtin:resolve-defaults    PASS     18ms
terraform-scaffold   builtin:render-defaults     PASS     22ms
terraform-scaffold   renders-dev-defaults        PASS     12ms
terraform-scaffold   renders-prod-override       PASS     15ms
terraform-scaffold   rejects-invalid-env         PASS     8ms
terraform-scaffold   temporarily-disabled        SKIP     -

7 passed, 0 failed, 0 errors, 1 skipped (121ms)
~~~

### Error Output

~~~
SOLUTION             TEST                        STATUS   DURATION
terraform-scaffold   renders-with-setup           ERROR    3ms

  Init [1/2] failed:
    $ mkdir -p /restricted/path
    mkdir: permission denied
    (exit 1)

  Init step failure is an error, not an assertion failure.

terraform-scaffold   renders-dev-defaults        PASS     12ms

1 passed, 0 failed, 1 error, 0 skipped (15ms)
~~~

### Failure Output

~~~
SOLUTION             TEST                        STATUS   DURATION
terraform-scaffold   renders-dev-defaults        FAIL     14ms

  ✗ expression: size(output.actions) == 1
    size(output.actions) = 3
    Expected 1, got 3
    Message: Should produce exactly one action

  ✗ contains: "dev/main.tf"
    Substring not found in stdout

terraform-scaffold   renders-prod-override       PASS     15ms

1 passed, 1 failed, 0 errors, 0 skipped (29ms)
~~~

### Verbose Failure Output (`-v`)

~~~
SOLUTION             TEST                        STATUS   DURATION
terraform-scaffold   renders-dev-defaults        FAIL     14ms

  Command: scafctl render solution -f /tmp/scafctl-test-abc123/solution.yaml -o json
  Sandbox: /tmp/scafctl-test-abc123/

  Init [1/1]:
    $ mkdir -p templates
    (exit 0)

  Stdout:
    {"actions":{"render-main":{"provider":"template","inputs":{"output":"staging/main.tf"}},...}}

  Stderr:
    (empty)

  Exit code: 0

  ✗ expression: size(output.actions) == 1
    size(output.actions) = 3
    Expected 1, got 3
    Message: Should produce exactly one action
~~~

### JSON (`-o json`)

~~~json
{
  "results": [
    {
      "solution": "terraform-scaffold",
      "test": "renders-dev-defaults",
      "status": "pass",
      "duration": "12ms",
      "command": "render solution",
      "assertions": [
        { "type": "expression", "value": "size(output.actions) == 1", "passed": true },
        { "type": "contains", "value": "dev/main.tf", "passed": true }
      ]
    },
    {
      "solution": "terraform-scaffold",
      "test": "temporarily-disabled",
      "status": "skip",
      "skipReason": "Waiting on upstream provider fix"
    }
  ],
  "summary": { "passed": 3, "failed": 0, "errors": 0, "skipped": 1, "duration": "35ms" }
}
~~~

### JUnit XML (`--report-file`)

Written to the specified path alongside normal terminal output. One `<testsuite>` per solution, one `<testcase>` per test. Skipped tests emit `<skipped message="reason"/>`. Failed assertions use `<failure>` with diagnostic output. Infrastructure/setup errors use `<error>` (distinct from `<failure>`) to differentiate assertion failures from environment issues.

Example `<error>` element:

~~~xml
<testcase name="renders-with-setup" classname="terraform-scaffold" time="0.003">
  <error message="init step 1 failed: exit code 1">
    $ mkdir -p /restricted/path
    mkdir: permission denied
  </error>
</testcase>
~~~

---

## Build Integration

### Bundler Discovery

`scafctl build` and the bundler's `DiscoverFiles()` must scan `spec.tests[*].files` entries as an additional discovery source. These are tagged as `TestInclude` to distinguish them from `StaticAnalysis` and `ExplicitInclude` sources.

This ensures test files are included in the bundle artifact and available when tests run from a remote catalog.

### Lint Rule

`scafctl lint` produces an **error** when files referenced in `spec.tests[*].files` are not covered by `bundle.include` patterns. Tests must work when the solution is fetched from a remote catalog, so all test files must be bundled.

---

## Go Types

### Package: `pkg/solution/testing`

~~~go
// TestCase defines a single functional test for a solution.
type TestCase struct {
    Name          string            `json:"name" yaml:"name" doc:"Test name (auto-set from map key)"`
    Description   string            `json:"description" yaml:"description" doc:"Human-readable test description"`
    Command       []string          `json:"command,omitempty" yaml:"command,omitempty" doc:"scafctl subcommand as array" example:"[render, solution]"`
    Args          []string          `json:"args,omitempty" yaml:"args,omitempty" doc:"Additional CLI flags. -f is always auto-injected by the runner"`
    Extends       []string          `json:"extends,omitempty" yaml:"extends,omitempty" doc:"Names of test templates to inherit from"`
    Tags          []string          `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Tags for categorization and --tag filtering"`
    Env           map[string]string `json:"env,omitempty" yaml:"env,omitempty" doc:"Per-test environment variables"`
    Files         []string          `json:"files,omitempty" yaml:"files,omitempty" doc:"Relative paths or globs for test files"`
    Init          []InitStep        `json:"init,omitempty" yaml:"init,omitempty" doc:"Setup steps run before the command"`
    Cleanup       []InitStep        `json:"cleanup,omitempty" yaml:"cleanup,omitempty" doc:"Teardown steps run after the command, even on failure"`
    Assertions    []Assertion       `json:"assertions,omitempty" yaml:"assertions,omitempty" doc:"Output assertions. All are evaluated regardless of prior failures"`
    Snapshot      string            `json:"snapshot,omitempty" yaml:"snapshot,omitempty" doc:"Golden file path for normalized comparison"`
    InjectFile    *bool             `json:"injectFile,omitempty" yaml:"injectFile,omitempty" doc:"Auto-inject -f sandbox path. Default true. Set false for catalog tests"`
    ExpectFailure bool              `json:"expectFailure,omitempty" yaml:"expectFailure,omitempty" doc:"Pass if command exits non-zero"`
    ExitCode      *int              `json:"exitCode,omitempty" yaml:"exitCode,omitempty" doc:"Exact expected exit code. Overrides expectFailure"`
    Timeout       *time.Duration    `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Per-test timeout"`
    Skip          bool              `json:"skip,omitempty" yaml:"skip,omitempty" doc:"Skip this test"`
    SkipReason    string            `json:"skipReason,omitempty" yaml:"skipReason,omitempty" doc:"Human-readable skip reason"`
    SkipExpression celexp.Expression `json:"skipExpression,omitempty" yaml:"skipExpression,omitempty" doc:"CEL expression evaluated at discovery time. If true, test is skipped. Context: os, arch, env"`
    Retries       int               `json:"retries,omitempty" yaml:"retries,omitempty" doc:"Number of retry attempts for failing tests" maximum:"10"`
}

// IsTemplate returns true if this test is a template (name starts with _).
func (tc *TestCase) IsTemplate() bool {
    return strings.HasPrefix(tc.Name, "_")
}

// TestConfig holds solution-level test configuration.
type TestConfig struct {
    SkipBuiltins SkipBuiltinsValue `json:"skipBuiltins,omitempty" yaml:"skipBuiltins,omitempty" doc:"Disable builtins: true for all, or list of specific names"`
    Setup        []InitStep        `json:"setup,omitempty" yaml:"setup,omitempty" doc:"Suite-level setup steps. Run once, copied per-test"`
    Cleanup      []InitStep        `json:"cleanup,omitempty" yaml:"cleanup,omitempty" doc:"Suite-level teardown steps. Run once after all tests complete, even on failure"`
}

// SkipBuiltinsValue supports both bool and []string via custom UnmarshalYAML.
// When bool: true skips all builtins, false skips none.
// When []string: skips only the named builtins (without "builtin:" prefix).
// Both UnmarshalYAML and MarshalYAML are required to survive
// the deepCopySolution YAML round-trip used in compose.
type SkipBuiltinsValue struct {
    All   bool     // true = skip all builtins
    Names []string // specific builtin names to skip
}

// InitStep is a setup/cleanup command.
// Uses the same input schema as the exec provider.
type InitStep struct {
    Command    string            `json:"command" yaml:"command" doc:"Command to execute" maxLength:"1000"`
    Args       []string          `json:"args,omitempty" yaml:"args,omitempty" doc:"Additional arguments, auto shell-quoted" maxItems:"100"`
    Stdin      string            `json:"stdin,omitempty" yaml:"stdin,omitempty" doc:"Standard input"`
    WorkingDir string            `json:"workingDir,omitempty" yaml:"workingDir,omitempty" doc:"Working directory relative to sandbox root"`
    Env        map[string]string `json:"env,omitempty" yaml:"env,omitempty" doc:"Environment variables merged with parent process"`
    Timeout    int               `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Timeout in seconds" maximum:"3600"`
    Shell      string            `json:"shell,omitempty" yaml:"shell,omitempty" doc:"Shell interpreter" pattern:"^(auto|sh|bash|pwsh|cmd)$"`
}

// Assertion validates command output.
// Exactly one of Expression, Regex, Contains, NotRegex, or NotContains must be set.
// Enforced via Validate() after YAML unmarshal.
type Assertion struct {
    Expression  celexp.Expression `json:"expression,omitempty" yaml:"expression,omitempty" doc:"CEL expression evaluating to bool"`
    Regex       string            `json:"regex,omitempty" yaml:"regex,omitempty" doc:"Regex pattern that must match"`
    Contains    string            `json:"contains,omitempty" yaml:"contains,omitempty" doc:"Substring that must appear"`
    NotRegex    string            `json:"notRegex,omitempty" yaml:"notRegex,omitempty" doc:"Regex pattern that must NOT match"`
    NotContains string            `json:"notContains,omitempty" yaml:"notContains,omitempty" doc:"Substring that must NOT appear"`
    Target      string            `json:"target,omitempty" yaml:"target,omitempty" doc:"Match target: stdout (default), stderr, combined" pattern:"^(stdout|stderr|combined)$"`
    Message     string            `json:"message,omitempty" yaml:"message,omitempty" doc:"Custom failure message"`
}

// Validate checks that exactly one assertion type is set and target is valid.
func (a *Assertion) Validate() error { /* ... */ }

// FileInfo represents a file created or modified in the sandbox.
type FileInfo struct {
    Exists  bool   `json:"exists"`
    Content string `json:"content"`
}

// CommandOutput is the assertion context passed to CEL expressions.
type CommandOutput struct {
    Stdout   string            `json:"stdout"`
    Stderr   string            `json:"stderr"`
    ExitCode int               `json:"exitCode"`
    Output   map[string]any    `json:"output,omitempty"`
    Files    map[string]FileInfo `json:"files"`
}

// TestResult captures the outcome of a single test.
type TestResult struct {
    Solution         string            `json:"solution"`
    Test             string            `json:"test"`
    Status           Status            `json:"status"`
    Duration         time.Duration     `json:"duration"`
    Command          string            `json:"command"`
    AssertionResults []AssertionResult  `json:"assertions,omitempty"`
    SkipReason       string            `json:"skipReason,omitempty"`
    Error            error             `json:"-"`
    SandboxPath      string            `json:"sandboxPath,omitempty"`
}

// AssertionResult captures the outcome of a single assertion.
type AssertionResult struct {
    Type     string `json:"type"`
    Value    string `json:"value"`
    Passed   bool   `json:"passed"`
    Message  string `json:"message,omitempty"`
    Expected any    `json:"expected,omitempty"`
    Actual   any    `json:"actual,omitempty"`
}

// Status represents the outcome of a test.
type Status string

const (
    StatusPass  Status = "pass"
    StatusFail  Status = "fail"
    StatusSkip  Status = "skip"
    StatusError Status = "error"
)
~~~

### Additions to Existing Types

- `pkg/solution/spec.go`: Add `Tests map[string]*testing.TestCase` and `TestConfig *testing.TestConfig` fields to `Spec`. Add `HasTests() bool` and `HasTestConfig() bool` helper methods following the existing `Has*()` pattern
- `pkg/solution/bundler/compose.go`: Extend compose to merge `spec.tests` (by name, reject duplicates) and `spec.testConfig` (`skipBuiltins`: true-wins for bool / union for lists; `setup`/`cleanup`: appended in compose-file order)
- `pkg/solution/bundler/discover.go`: Add `TestInclude` discovery source; scan `spec.tests[*].files` entries

---

## Files to Create/Modify

| Action | Path | Description |
| ------ | ---- | ----------- |
| Create | `pkg/solution/testing/types.go` | Test spec types, `SkipBuiltinsValue` with custom unmarshal |
| Create | `pkg/solution/testing/runner.go` | In-process cobra execution, sandbox orchestration, mutex-serialized command invocation |
| Create | `pkg/solution/testing/context.go` | Build CEL assertion context from command output |
| Create | `pkg/solution/testing/assertions.go` | CEL, regex, contains, negation assertion evaluation with `target` |
| Create | `pkg/solution/testing/diagnostics.go` | CEL sub-expression evaluation for failure diagnostics |
| Create | `pkg/solution/testing/builtins.go` | Builtin test definitions and execution |
| Create | `pkg/solution/testing/sandbox.go` | Temp directory creation, file copying, file diff for `output.files` |
| Create | `pkg/solution/testing/snapshot.go` | Golden file comparison, update, unified diff output |
| Create | `pkg/solution/testing/inheritance.go` | `extends` resolution, merge logic, circular detection |
| Create | `pkg/solution/testing/discovery.go` | Test discovery, `--filter`/`--tag` filtering, template exclusion |
| Create | `pkg/solution/testing/reporter.go` | kvx result formatting |
| Create | `pkg/solution/testing/junit.go` | JUnit XML report writer (`<failure>` vs `<error>`) |
| Create | `pkg/solution/testing/runner_test.go` | Runner unit tests |
| Create | `pkg/solution/testing/assertions_test.go` | Assertion unit tests (including `target` field) |
| Create | `pkg/solution/testing/snapshot_test.go` | Snapshot comparison tests |
| Create | `pkg/solution/testing/diagnostics_test.go` | CEL diagnostics tests |
| Create | `pkg/solution/testing/inheritance_test.go` | Extends chain, merge rules, circular detection tests |
| Create | `pkg/solution/testing/discovery_test.go` | Glob filter, tag filter, template exclusion tests |
| Create | `pkg/cmd/scafctl/test/test.go` | `test` parent command |
| Create | `pkg/cmd/scafctl/test/functional.go` | `test functional` command |
| Create | `pkg/cmd/scafctl/test/list.go` | `test list` command |
| Modify | `pkg/cmd/scafctl/root.go` | Register `test` command |
| Modify | `pkg/solution/spec.go` | Add `Tests` and `TestConfig` fields |
| Modify | `pkg/solution/bundler/compose.go` | Merge `spec.tests` and `spec.testConfig` in compose |
| Modify | `pkg/solution/bundler/discover.go` | Add `TestInclude` discovery source |
| Modify | `pkg/cmd/scafctl/lint/` | Add lint rule for unbundled test files (error), invalid test names, and unused templates (warning) |
| Modify | `pkg/exitcode/exitcode.go` | Add `TestFailed = 11` constant |
| Create | `tests/integration/solutions/` | Functional test fixtures |
| Create | `docs/design/functional-testing.md` | This design doc |
| Modify | `docs/design/testing.md` | Reference this design doc from section 5 |
| Create | `docs/tutorials/functional-testing.md` | Tutorial for solution authors (see [Tutorial Outline](#tutorial-outline) below) |
| Create | `examples/solutions/tested-solution/` | Example solution with inline tests |

---

## Verification

1. **Unit tests**: `go test ./pkg/solution/testing/...`
2. **Race detection**: `go test -race ./pkg/solution/testing/...` to verify no data races in mutex-serialized execution
3. **CLI integration tests**: Add `test functional` and `test list` to `tests/integration/cli_test.go`
4. **Self-hosted**: `scafctl test functional --tests-path tests/integration/solutions`
5. **Taskfile**: `task integration` passes
6. **Lint**: `golangci-lint run --fix`
7. **Build integration**: `scafctl build` on a solution with test files produces a bundle that includes them
8. **Concurrency**: Run with `-j 1` and default concurrency to validate both paths
9. **YAML round-trip**: Unit test verifying `SkipBuiltinsValue` survives `deepCopySolution` YAML marshal/unmarshal

---

## Future Enhancements

### Auto-Generated Tests (`-o test`)

A future output type for commands that support `-o`. When used, scafctl captures the command and its arguments, executes it, and generates a complete test definition with assertions derived from the actual output.

~~~bash
scafctl render solution -f solution.yaml -r env=prod -o test
~~~

Would output:

~~~yaml
renders-prod:
  description: "Auto-generated test for: render solution -r env=prod"
  command: [render, solution]
  args: ["-r", "env=prod"]
  assertions:
    - expression: 'size(output.actions) == 3'
    - expression: 'output.actions["render-main"].inputs.output == "prod/main.tf"'
    - expression: 'output.actions["render-main"].provider == "template"'
  snapshot: "testdata/renders-prod.json"
~~~

The generator would execute the command, derive CEL assertions from the output shape, write a snapshot golden file, and emit the test YAML to stdout.

---

### Catalog Regression Testing (`scafctl pipeline`)

A future command that executes functional tests across solutions in a remote catalog. This enables the scafctl team to validate that changes to scafctl don't break existing solutions.

~~~bash
scafctl pipeline test --catalog https://catalog.example.com --solutions "terraform-*"
~~~

Would fetch matching solutions, extract bundled test files, run `test functional` against each, and report aggregate results.

This is the primary use case for requiring test files to be bundled and why `scafctl lint` errors on unbundled test files.

---

### Test Scaffolding (`scafctl test init`)

A future command that generates a starter test suite for an existing solution by analyzing its structure:

~~~bash
scafctl test init -f solution.yaml
~~~

Would parse the solution, identify resolvers with defaults, and output skeleton test YAML. Unlike `-o test` (which captures actual output), `test init` generates a starting point before you run anything.

---

### Watch Mode (`--watch`)

A future flag that re-runs tests when solution files change:

~~~bash
scafctl test functional -f solution.yaml --watch
~~~

Monitors the solution file and its bundle/compose files for changes, then re-runs affected tests.

---

### Tutorial Outline

The planned tutorial at `docs/tutorials/functional-testing.md` should cover:

1. **Writing your first test** — minimal solution with one test case, `scafctl test functional` invocation, reading output
2. **Assertions deep dive** — CEL expressions vs regex/contains, `target` field, `output` variable structure per command, negation assertions
3. **Test inheritance** — `_`-prefixed templates, `extends` chains, merge behavior for args/assertions/tags
4. **Snapshots** — golden file workflow, `--update-snapshots`, normalization pipeline
5. **CI integration** — JUnit XML reporting with `--report-file`, exit codes, `--fail-fast` patterns
6. **Advanced features** — init/cleanup steps, test files, `skipExpression`, retries, suite-level setup

The example solution at `examples/solutions/tested-solution/` should include:

- A solution with 2-3 resolvers and a template action
- 3-4 inline tests covering: CEL expression assertion, contains/regex assertion, `expectFailure` for validation, and a snapshot test
- A `testdata/` directory with a golden file
- A `bundle.include` covering the test files

---

## Decisions

- **Command-based tests** over render-only: tests can exercise any scafctl subcommand, making the framework a general-purpose solution validation tool
- **Command as array**: `command: [render, solution]` instead of a string, for unambiguous parsing and consistency with args
- **In-process execution** over subprocess: the runner invokes the cobra command tree directly using `Root()`. Faster, no built binary dependency, simpler output capture
- **Auto-inject `-f` by default**: the runner injects `-f <sandbox-solution-path>` unless `injectFile: false`. `-f` must never appear in `args` regardless of `injectFile`. Disabled for catalog solution tests where no local file is needed
- **Custom `exitFunc`**: uses `writer.WithExitFunc()` to intercept `os.Exit` calls during in-process execution, converting them to `*exitcode.ExitError` values
- **Tests in `spec.tests`** with compose support: follows existing split-file pattern, keeps tests colocated with the solution
- **Temp directory sandbox**: init scripts can modify files safely without affecting source. Symlinks rejected
- **Five assertion types** (CEL, regex, contains, notRegex, notContains): CEL for structured assertions; text matching for quick checks; negation for safety
- **Assertion `target` field**: `stdout` (default), `stderr`, or `combined` for text assertions. Cleaner than separate `stderrContains`/`stderrRegex` fields
- **All assertions always evaluated**: failures don't short-circuit. User sees all problems at once
- **Structured + raw output**: auto-inject `-o json` when supported, always provide raw stdout/stderr
- **`output.files` via diff**: snapshot sandbox files before command, diff after, expose only new/modified files in CEL context as `map[string]FileInfo`
- **Environment variables** over Go templates: `SCAFCTL_SANDBOX_DIR` — no custom template engine, natural for shell commands
- **Per-test `env`**: additional environment variables set for init, command, and cleanup
- **Builtins on by default**: baseline correctness without boilerplate
- **Selective builtin skip**: `skipBuiltins` accepts `bool` (all) or `[]string` (specific names) via custom `UnmarshalYAML`
- **Init uses exec provider schema**: one input model, consistent with the rest of scafctl
- **Cleanup steps**: `cleanup` field runs even on failure, like `finally` in actions. Cleanup failures are logged but don't change test status
- **Test inheritance**: multi-extends via `extends: [base1, base2]`, applied left-to-right. Template tests prefixed with `_` are not executed
- **Test tags**: `tags` field for categorization, `--tag` flag for filtering. Match if test has any specified tag
- **Test name validation**: must match `^[a-zA-Z0-9][a-zA-Z0-9_-]*$` for JUnit/CLI compatibility. Templates match `^_[a-zA-Z0-9][a-zA-Z0-9_-]*$`
- **Test files in bundle**: `scafctl build` auto-discovers test file references; `scafctl lint` errors if not in `bundle.include`. Required for remote catalog testing
- **Parallel by default**: each test has its own sandbox. `--sequential` opt-out for debugging
- **Fail-fast per-solution**: `--fail-fast` stops remaining tests for the current solution on first failure. Other solutions continue
- **kvx + JUnit XML reporting**: kvx for consistency; JUnit for CI integration. JUnit distinguishes `<failure>` (assertion) from `<error>` (setup/infrastructure)
- **`expectFailure` + `exitCode`**: simple inversion vs specific code
- **CEL assertion diagnostics**: sub-expression evaluation to show actual vs expected
- **Normalized snapshots**: always normalize (strip timestamps, sort map keys). Not configurable
- **Unified diff for snapshots**: actionable mismatch output. Selective updates via `--update-snapshots --filter`
- **Suite-level setup**: run once, copy per-test. Avoids init duplication
- **`--keep-sandbox`**: preserve failed test directories for manual inspection
- **Skip + skipReason**: standard test framework feature for development workflow
- **Lint error** (not warning) for unbundled test files: catalog regression testing requires bundled tests
- **Lint warning** for unused templates: templates defined but never referenced via `extends` are likely dead code
- **Root() data race mitigation**: mutex serialization of in-process cobra invocations. Commands are fast (<100ms), so serialization has minimal impact. Future: refactor `Root()` to eliminate package-level state
- **Exit codes**: new `TestFailed = 11` constant rather than reusing `ValidationFailed = 2`, which has different semantics
- **`--tag` and `--filter` as `[]string`**: registered via `StringArrayVar` per project convention (not `StringSliceVar` which uses CSV parsing). Multiple flags allowed; OR logic
- **`--filter` glob library**: `doublestar.Match` — already a project dependency in `pkg/solution/bundler/discover.go`
- **`-o json` auto-detection**: `cmd.Flags().Lookup("output")` cobra flag introspection — no command registry needed
- **`output` nil when unsupported**: diagnostic error rather than empty map to prevent silent assertion failures
- **Concurrency control**: `-j N` flag with `--sequential` as sugar for `-j 1` — standard test runner pattern
- **File size guard**: 10MB cap on `files[].content` to prevent OOM without blocking tests
- **Conditional skip via CEL**: `skipExpression` field evaluated at discovery time with `os`, `arch`, `env` context
- **Test retries**: `retries` field for flaky test resilience, capped at 10 attempts
- **Suite-level cleanup**: `testConfig.cleanup` runs after all tests, symmetric with `testConfig.setup`
- **Compose `testConfig` merge**: `setup`/`cleanup` steps appended in compose-file order (new merge strategy); `skipBuiltins` uses `true`-wins for bool, union for lists
- **`SkipBuiltinsValue` round-trip**: requires both `UnmarshalYAML` and `MarshalYAML` for compose `deepCopySolution` compatibility
- **Snapshot normalization pipeline**: fixed set of scrubbers (timestamps, UUIDs, sandbox paths, sorted keys). Custom scrubbers deferred to future enhancement
