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
| `test functional` CLI command | ✅ Done | `pkg/cmd/scafctl/test/functional.go` |
| `test list` CLI subcommand | ✅ Done | `pkg/cmd/scafctl/test/list.go` |
| Test spec types | ✅ Done | `pkg/solution/soltesting/types.go` |
| Builtin tests | ✅ Done | Parse, resolve, render, lint in `builtins.go` |
| Command-based test execution | ✅ Done | In-process cobra execution via `CommandBuilder` |
| CEL assertions | ✅ Done | `pkg/solution/soltesting/assertions.go` |
| Regex assertions | ✅ Done | |
| Contains assertions | ✅ Done | |
| Negation assertions | ✅ Done | `notContains`, `notRegex` |
| Golden file snapshots | ✅ Done | `pkg/solution/soltesting/snapshot.go` |
| Init scripts (exec provider) | ✅ Done | `InitStep` with exec provider schema |
| Test file includes | ✅ Done | `TestInclude` discovery source in bundler |
| Temp directory sandbox | ✅ Done | `pkg/solution/soltesting/sandbox.go` |
| JUnit XML reporting | ✅ Done | `pkg/solution/soltesting/junit.go` |
| Compose support for tests | ✅ Done | `mergeTests()` and `mergeTestConfig()` in compose |
| Parallel test execution | ✅ Done | Semaphore-based concurrency control |
| CEL assertion diagnostics | ✅ Done | `pkg/solution/soltesting/diagnostics.go` |
| Suite-level setup | ✅ Done | `testConfig.setup` with base sandbox copy |
| Test tags and filtering | ✅ Done | `--tag` flag, `tags` field on test cases |
| Per-test environment variables | ✅ Done | `env` field on test cases |
| Cleanup steps | ✅ Done | `cleanup` field, runs even on failure |
| Test inheritance (extends) | ✅ Done | `pkg/solution/soltesting/inheritance.go` |
| Assertion target (stderr) | ✅ Done | `target` field: `stdout`, `stderr`, `combined` |
| File assertions (`__files`) | ✅ Done | Diff-based sandbox file change detection |
| Fail-fast (per-solution) | ✅ Done | `--fail-fast` stops remaining tests per solution |
| Test name validation | ✅ Done | Enforced in `TestCase.Validate()` |
| Selective builtin skip | ✅ Done | `SkipBuiltinsValue` with custom unmarshal |
| In-process command execution | ✅ Done | `Root()` with `*RootOptions`, `CommandBuilder` |
| Concurrency control | ✅ Done | `-j` flag, `--sequential` as sugar for `-j 1` |
| Conditional skip (`skipExpression`) | ✅ Done | CEL-based runtime skip evaluation |
| Test retries | ✅ Done | `retries` field for flaky test resilience |
| Suite-level cleanup | ✅ Done | `testConfig.cleanup` for teardown after all tests |
| File size guard | ✅ Done | Cap `files[].content` at 10MB to prevent OOM |
| In-process execution safety | ✅ Done | `Root()` accepts `*RootOptions`, no package-level state |
| Unused template lint warning | ✅ Done | `unused-template` lint rule |
| Solution filtering (`--solution`) | ✅ Done | Glob-based solution name filtering |
| `--filter` solution/test format | ✅ Done | `--filter "solution/test-name"` glob support |
| `--dry-run` flag | ✅ Done | Validate test definitions without executing |
| Suite-level `env` | ✅ Done | `testConfig.env` shared across all tests |
| Binary file content guard | ✅ Done | Non-UTF-8 files get `content` set to `"<binary file>"` |
| Test execution ordering | ✅ Done | Alphabetical by name; builtins first |
| Field max limits | ✅ Done | `assertions: 100`, `files: 50`, `tags: 20`, extends depth: 10 |
| Glob zero-match error | ✅ Done | Test `files` globs matching zero files produce `error` |
| Environment precedence chain | ✅ Done | process → `testConfig.env` → `TestCase.env` → `InitStep.env` |
| `TestCase.Validate()` | ✅ Done | Comprehensive test case validation method |
| Extends non-existent error | ✅ Done | `extends` referencing non-existent test names is a parse-time error |
| Tests per solution limit | ✅ Done | Max 500 tests per solution |

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
        - expression: 'size(__output.actions) >= 1'

    renders-dev-defaults:
      description: "Default environment renders dev configuration"
      extends: [_base-render]
      tags: [smoke, render]
      assertions:
        - expression: 'size(__output.actions) == 1'
          message: "Should produce exactly one action"
        - contains: "dev/main.tf"

    renders-prod-with-resolver-run:
      description: "Run resolver with prod override"
      command: [run, resolver]
      args: ["-r", "env=prod"]
      tags: [resolver]
      assertions:
        - expression: '__output.environment == "prod"'
        - regex: '"environment":\s*"prod"'

    render-prod-override:
      description: "Render with prod override produces correct paths"
      extends: [_base-render]
      args: ["-r", "env=prod"]
      tags: [render]
      assertions:
        - expression: '__output.actions["render-main"].inputs.output == "prod/main.tf"'

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
        - expression: '__output.errorCount == 0'

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
        - expression: 'size(__output.actions) >= 1'
        - expression: '__output.files["dev/main.tf"].exists'

    temporarily-disabled:
      description: "This test is skipped during development"
      skip: true
      skipReason: "Waiting on upstream provider fix"
      command: [render, solution]
      assertions:
        - expression: 'size(__output.actions) == 1'
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
        - expression: 'size(__output.actions) == 1'

    renders-prod-override:
      description: "Render with prod override produces correct paths"
      command: [render, solution]
      args: ["-r", "env=prod"]
      tags: [render]
      assertions:
        - expression: '__output.actions["render-main"].inputs.output == "prod/main.tf"'
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
    timeout: <string>  # Go duration format, e.g., "30s", "2m"
    skip: <bool>
    skipReason: <string>
    skipExpression: <Expression>
    retries: <int>
~~~

Each test case is a named entry under `spec.tests`. The maximum number of tests per solution is **500**.

### Field Details

| Field | Type | Required | Default | Description |
| ----- | ---- | -------- | ------- | ----------- |
| `description` | `string` | Yes | — | Human-readable test description |
| `command` | `[]string` | No | `[render, solution]` | scafctl subcommand as an array (e.g., `[render, solution]`, `[run, resolver]`, `[lint]`). By default the runner auto-injects `-f <sandbox-path>` — set `injectFile: false` to disable |
| `args` | `[]string` | No | `[]` | Additional CLI flags appended after the command. `-f` must never be included here — use `injectFile` to control file injection |
| `extends` | `[]string` | No | `[]` | Names of test templates to inherit from. Applied left-to-right; this test's fields override inherited values. See [Test Inheritance](#test-inheritance) |
| `tags` | `[]string` | No | `[]` | Tags for categorization and filtering. Use `--tag` to run only tests with matching tags. Max 20 tags per test |
| `env` | `map[string]string` | No | `{}` | Environment variables set for this test's init, command, and cleanup steps. Merged with process environment. See [Environment Precedence](#environment-precedence) |
| `files` | `[]string` | No | `[]` | Relative paths or globs for files required by this test. Supports `**` recursive globs. Globs are resolved at sandbox setup time; zero-match globs produce a test `error`. Max 50 entries |
| `init` | `[]InitStep` | No | `[]` | Setup steps executed sequentially before the command |
| `cleanup` | `[]InitStep` | No | `[]` | Teardown steps executed after the command, even on failure. See [Cleanup Steps](#cleanup-steps) |
| `assertions` | `[]Assertion` | Conditional | — | Required unless `snapshot` is set. All assertions are evaluated regardless of prior failures. Max 100 assertions per test |
| `snapshot` | `string` | No | — | Relative path to a golden file for normalized comparison |
| `injectFile` | `bool` | No | `true` | When `true` (default), the runner auto-injects `-f <sandbox-solution-path>`. Set to `false` for commands that don't accept `-f` (e.g., `config get`, `auth status`) or for catalog solution tests that use `--catalog` instead. `-f` must never appear in `args` regardless of this setting |
| `expectFailure` | `bool` | No | `false` | When `true`, the test passes if the command exits non-zero |
| `exitCode` | `int` | No | — | Exact expected exit code. **Mutually exclusive** with `expectFailure` — setting both is a validation error |
| `timeout` | `string` | No | `"30s"` | Per-test timeout as a Go duration string (e.g., `"30s"`, `"2m"`, `"1m30s"`). Parsed via a custom `Duration` type with string-based YAML/JSON marshalling |
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
    env:
      SCAFCTL_CONFIG_DIR: "$SCAFCTL_SANDBOX_DIR"
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
| `env` | `map[string]string` | `{}` | Suite-level environment variables applied to all tests. Merged with process environment. Individual test `env` fields override on key conflict. See [Environment Precedence](#environment-precedence) |
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
| `env` | Merged map; last compose file wins on key conflict |

Compose-file order affects `testConfig.setup`, `testConfig.cleanup`, and `testConfig.env` merge ordering but does **not** affect test execution order. Tests from all compose files are merged into a single map and executed alphabetically (see [Test Execution Ordering](#test-execution-ordering)).

`spec.tests` entries are merged by name using the **reject-duplicates** strategy (same as resolvers and actions). If two compose files define a test with the same name, the compose merge fails with an error.

> **`composePart` struct**: The `composePart` struct in `pkg/solution/bundler/compose.go` must be extended with `Tests map[string]*testing.TestCase` and `TestConfig *testing.TestConfig` fields to parse test-related sections from compose files.

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

### Environment Precedence

Environment variables are resolved in the following precedence order (highest wins):

| Priority | Source | Description |
| -------- | ------ | ----------- |
| 1 (lowest) | Process environment | Inherited from the parent process |
| 2 | `testConfig.env` | Suite-level env applied to all tests |
| 3 | `TestCase.env` | Per-test env overrides suite-level on key conflict |
| 4 (highest) | `InitStep.env` | Per-step env overrides all others on key conflict |

Each level merges with the previous — keys not overridden are preserved. The `SCAFCTL_SANDBOX_DIR` variable is always injected by the runner and cannot be overridden.

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
        - expression: 'size(__output.actions) >= 1'

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

### Glob Resolution

Globs in the `files` field (e.g., `testdata/**/*.json`) are resolved at **sandbox setup time** — after suite-level setup completes but before per-test init steps run. This means:

- Globs are expanded against the solution source directory (or the suite-level base sandbox if `testConfig.setup` is defined)
- **Zero-match globs produce a test `error`**, not a silent no-op. This catches typos and missing test data early
- Glob patterns are validated at lint time — `scafctl lint` warns on syntactically invalid glob patterns
- Resolved paths are logged in verbose output for debugging

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
- **Extends chain depth is limited to 10 levels**. Chains deeper than 10 are rejected with a validation error
- **Referencing a non-existent test name** in `extends` is a validation error at parse time. `scafctl lint` also reports this as an error

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
      - expression: 'size(__output.actions) >= 1'

  _base-prod:
    description: "Base prod test"
    args: ["-r", "env=prod"]
    tags: [prod]

  render-prod:
    description: "Render prod configuration"
    extends: [_base-render, _base-prod]
    assertions:
      - expression: '__output.actions["render-main"].inputs.output == "prod/main.tf"'
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
      - expression: 'size(__output.actions) >= 1'
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
      - expression: 'size(__output.actions) >= 1'
~~~

Filter tests by tag using the `--tag` flag:

~~~bash
# Run only tests tagged "smoke"
scafctl test functional -f solution.yaml --tag smoke

# Combine with name filter
scafctl test functional -f solution.yaml --tag render --filter "*prod*"

# Filter by solution and tag
scafctl test functional --tests-path ./solutions/ --solution "terraform-*" --tag smoke

# Filter with solution/test-name format
scafctl test functional --tests-path ./solutions/ --filter "terraform-*/render-*"
~~~

A test matches the `--tag` filter if it has **any** of the specified tags. Tags inherited via `extends` are included in the match.

When `--tag`, `--filter`, and `--solution` are combined, they are **ANDed**: a test must match the solution filter AND the name filter AND have a matching tag.

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
  - expression: '__stderr.contains("warning") && __stdout.contains("success")'
~~~

The `target` field has no effect on `expression` assertions — CEL expressions access `__stdout`, `__stderr`, `__exitCode`, `__output`, and `__files` as separate context variables.

### Assertion Context

#### How Output Is Captured

When a test executes a scafctl command:

1. The runner captures stdout, stderr, and exit code
2. If stdout is valid JSON, it is parsed into the `__output` variable. Otherwise `__output` is `nil`
3. The tester is responsible for passing `-o json` in `args` when structured output is needed

#### CEL Context Variables

| Variable | Type | Always Available | Description |
| -------- | ---- | ---------------- | ----------- |
| `__stdout` | `string` | Yes | Raw stdout text |
| `__stderr` | `string` | Yes | Raw stderr text |
| `__exitCode` | `int` | Yes | Process exit code |
| `__output` | `map[string, any]` | When `-o json` is passed in `args` | Parsed JSON output. **`nil` when stdout is not valid JSON**. CEL expressions referencing `__output` when nil cause the test to report as `error` (not `fail`) with the diagnostic: *"variable '__output' is nil — this command does not support structured output or -o json was not specified"*. This is a configuration issue, not an assertion failure |
| `__files` | `map[string, FileInfo]` | Yes | Files created or modified in the sandbox during command execution. Key is relative path. Each `FileInfo` has `exists` (bool) and `content` (string) |

The `output` variable structure depends on the command:

| Command | `__output` structure |
| ------- | -------------------- |
| `render solution` | Action graph: `__output.actions`, each with `provider`, `inputs`, `dependsOn`, `when` |
| `run resolver` | Resolver map: `__output.<resolverName>` = resolved value |
| `run solution` | Execution result: `__output.status`, `__output.actions`, `__output.duration` |
| `lint` | Lint result: `__output.findings`, `__output.errorCount`, `__output.warnCount` |
| `snapshot diff` | Diff result: `__output.added`, `__output.removed`, `__output.modified` |

> **Note**: This table is non-exhaustive. For commands not listed, `output` follows the command's `-o json` schema. Use verbose mode (`-v`) to inspect the raw JSON structure for any command.

#### File Assertions (`__files` variable)

The `__files` variable exposes files that were **created or modified** in the sandbox during command execution. The runner snapshots all file paths and modification times before the command runs, then diffs after execution. Only new or changed files appear in `__files`.

~~~yaml
assertions:
  # Check that a file was created
  - expression: '__files["prod/main.tf"].exists'

  # Check file content
  - expression: '__files["prod/main.tf"].content.contains("resource")'

  # Check number of generated files
  - expression: 'size(__files) == 3'
~~~

Each entry in `__files` is keyed by the relative path from the sandbox root and has:
- `exists` (`bool`): always `true` for entries in the map (present for consistency)
- `content` (`string`): the full file content as a string

> **Size guard**: Files larger than 10MB have their `content` set to `"<file too large>"` and a warning is emitted in verbose output. This prevents OOM on solutions that generate large binary artifacts.

> **Binary file guard**: Files with non-UTF-8 content (binary files) have their `content` set to `"<binary file>"` and a warning is emitted in verbose output. The `exists` field is still `true`. Use `exists` checks rather than `content` checks for binary outputs.

#### Regex and Contains Context

`regex`, `contains`, `notRegex`, and `notContains` assertions match against the stream specified by the `target` field (default: `stdout`). When `target` is `combined`, stdout and stderr are concatenated with a newline separator. This is useful for:

- Commands that don't support `-o json` (e.g., `explain solution`)
- Quick substring checks without CEL overhead
- Pattern matching on formatted output
- Ensuring sensitive values or panic traces don't appear in output

#### CEL Assertion Diagnostics

When a CEL assertion fails, the runner evaluates sub-expressions to provide actionable diagnostics rather than just "expected true, got false":

~~~
✗ expression: size(__output.actions) == 3
  size(__output.actions) = 5
  Expected 3, got 5
~~~

~~~
✗ expression: __output.actions["render-main"].inputs.output == "prod/main.tf"
  __output.actions["render-main"].inputs.output = "dev/main.tf"
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

### Snapshot with `expectFailure`

When `snapshot` is combined with `expectFailure: true`, the snapshot captures stdout from the failing command. The snapshot comparison runs **after** the exit code check passes (i.e., after confirming the command did fail as expected). If the exit code check fails (command unexpectedly succeeds), the snapshot comparison is skipped and the test reports as `fail`.

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

`Root()` accepts a `*RootOptions` struct that enables isolated, concurrent invocations:

~~~go
opts := &scafctl.RootOptions{
    IOStreams:   terminal.NewIOStreams(nil, &stdout, &stderr, false),
    ExitFunc:    func(code int) { panic(&exitcode.ExitError{Code: code}) },
    ConfigPath:  "",
}
cli := scafctl.Root(opts)
cli.SetArgs([]string{"render", "solution", "-f", sandboxPath, "-o", "json"})
err := cli.Execute()
~~~

Each call to `Root()` creates its own `cliParams`, flag bindings, and writer — no package-level mutable state. This means multiple test goroutines can construct and execute cobra trees fully in parallel without data races or mutex serialization.

The runner:
1. Constructs the cobra root command using `Root(opts)` with custom `IOStreams` (backed by `bytes.Buffer`) and a custom `ExitFunc`
2. Sets the `command` array as args (e.g., `[render, solution]` → cobra traversal)
3. Injects `-f <sandbox-solution-path>` by default. Set `injectFile: false` on the test case to disable (e.g., for catalog solution tests). The runner **always errors** if `-f` appears in the test's `args`, regardless of `injectFile`
4. Stdout/stderr are captured via the `IOStreams` buffers passed in `RootOptions`
5. Uses `RootOptions.ExitFunc` to intercept `os.Exit` calls and convert them to `*exitcode.ExitError` values, preventing the test runner from terminating

### Sandbox

Each test runs in an isolated temporary directory:

1. Copy the solution file and its bundle files to a temp directory
2. Copy test `files` into the sandbox (maintaining relative paths). Symlinks are rejected
3. Snapshot all file paths and modification times (for `__files` diff)
4. Inject per-test `env` variables and `SCAFCTL_SANDBOX_DIR`
5. Run init steps in the sandbox
6. Execute the scafctl command in-process
7. Diff sandbox files against snapshot to populate `__files`
8. Capture output and run assertions
9. Run cleanup steps (even on failure)
10. Clean up the temp directory (unless `--keep-sandbox` is set)

This ensures init scripts cannot modify source files.

### Test Execution Ordering

Tests execute in a deterministic order:

1. **Builtin tests** run first, in alphabetical order (`builtin:lint`, `builtin:parse`, `builtin:render-defaults`, `builtin:resolve-defaults`)
2. **User-defined tests** run next, in alphabetical order by test name
3. **Template tests** (names starting with `_`) are never executed

With `-j > 1` (default), tests run in parallel and may complete in any order, but **result reporting is always alphabetical**. With `--sequential` (`-j 1`), both execution and reporting follow alphabetical order.

This is consistent with how the codebase handles action ordering within execution phases (`sort.Strings`). Tests should be independent — alphabetical ordering exposes hidden ordering dependencies.

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
9. If the test's `args` include `-o json` or `--output json`, the runner will pass them through. The tester is responsible for including `-o json` in `args` when structured output is needed
10. Inject `SCAFCTL_SANDBOX_DIR` and per-test `env` environment variables
11. Execute in-process with timeout; capture stdout, stderr, exit code via `RootOptions.ExitFunc`
12. Diff sandbox files against snapshot → populate `__files` context variable
13. Parse JSON stdout if available → populate `__output` context variable (nil if stdout is not valid JSON)
14. Check exit code against `exitCode` or `expectFailure`
15. If `snapshot` is set → run snapshot comparison (show unified diff on mismatch)
16. Run **all** assertions (CEL against parsed output, regex/contains against target stream). All assertions always run regardless of prior failures
17. Run cleanup steps (even on failure or error)
18. All checks pass → `pass`; any check fails → `fail`
19. If `fail` and `retries > 0` → re-run from step 5 up to `retries` times. Each retry creates a **fresh sandbox** (re-copies solution + bundle files from the suite-level base sandbox if `testConfig.setup` is present, otherwise from source). Init steps re-run on each retry. If any retry passes → `pass (retry N/M)`. Retry attempts are shown in verbose output

### Parallelism

Test cases run in parallel by default, limited by the `-j` / `--concurrency` flag (default: `runtime.NumCPU()`). Each test has its own sandbox and its own `Root()` invocation with isolated state — no shared mutable state, no mutex serialization needed.

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
| `-j`, `--concurrency` | `int` | `runtime.NumCPU()` | Maximum number of tests to run in parallel |
| `--skip-builtins` | `bool` | `false` | Skip builtin tests for all solutions |
| `--test-timeout` | `duration` | `30s` | Per-test timeout |
| `--timeout` | `duration` | `5m` | Global timeout for all tests |
| `--filter` | `[]string` | — | Run only tests matching this name pattern (glob via `doublestar.Match`). Supports two formats: (1) test name only (e.g., `"render-*"`) — matches against the test name, (2) `solution/test-name` format (e.g., `"terraform-*/render-*"`) — matches against both solution name and test name. When no `/` is present, matches test name only (backward-compatible). Builtin tests are matched with their full name including `builtin:` prefix. Multiple `--filter` flags allowed; a test runs if it matches any filter. Registered via `StringArrayVar` per project convention |
| `--tag` | `[]string` | — | Run only tests with these tags. Multiple `--tag` flags allowed (e.g., `--tag smoke --tag render`). A test matches if it has **any** of the specified tags. Registered via `StringArrayVar` per project convention |
| `--solution` | `[]string` | — | Run only tests from solutions matching this name pattern (glob via `doublestar.Match`). Multiple `--solution` flags allowed; a solution is included if it matches any pattern. When combined with `--filter` and `--tag`, all filters are ANDed: a test must match the solution filter AND the name filter AND have a matching tag |
| `--dry-run` | `bool` | `false` | Validate test definitions, resolve extends chains, and report discovery results without executing any tests. Useful for CI preflight checks. Exits 0 if valid, `exitcode.InvalidInput` (3) if invalid |
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
| `--solution` | `[]string` | — | Filter to solutions matching this name pattern (glob). Multiple `--solution` flags allowed |
| `--filter` | `[]string` | — | Filter to tests matching this name pattern (glob). Supports `solution/test-name` format. Multiple `--filter` flags allowed |

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

In verbose mode (`-v`), passing tests show assertion counts:

~~~
SOLUTION             TEST                        STATUS           DURATION
terraform-scaffold   renders-dev-defaults        PASS (2/2)       12ms
terraform-scaffold   renders-prod-override       PASS (3/3)       15ms
terraform-scaffold   rejects-invalid-env         PASS (2/2)       8ms
~~~

Failing tests show which assertions failed:

~~~
terraform-scaffold   renders-dev-defaults        FAIL (1/3)       14ms
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

  ✗ expression: size(__output.actions) == 1
    size(__output.actions) = 3
    Expected 1, got 3
    Message: Should produce exactly one action

  ✗ contains: "dev/main.tf"
    Substring not found in stdout

terraform-scaffold   renders-prod-override       PASS     15ms

1 passed, 1 failed, 0 errors, 0 skipped (29ms)
~~~

### Verbose Failure Output (`-v`)

~~~
SOLUTION             TEST                        STATUS           DURATION
terraform-scaffold   renders-dev-defaults        FAIL (1/2)       14ms

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

  ✗ expression: size(__output.actions) == 1
    size(__output.actions) = 3
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
        { "type": "expression", "value": "size(__output.actions) == 1", "passed": true },
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
    Extends       []string          `json:"extends,omitempty" yaml:"extends,omitempty" doc:"Names of test templates to inherit from" maxItems:"10"`
    Tags          []string          `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Tags for categorization and --tag filtering" maxItems:"20"`
    Env           map[string]string `json:"env,omitempty" yaml:"env,omitempty" doc:"Per-test environment variables"`
    Files         []string          `json:"files,omitempty" yaml:"files,omitempty" doc:"Relative paths or globs for test files" maxItems:"50"`
    Init          []InitStep        `json:"init,omitempty" yaml:"init,omitempty" doc:"Setup steps run before the command"`
    Cleanup       []InitStep        `json:"cleanup,omitempty" yaml:"cleanup,omitempty" doc:"Teardown steps run after the command, even on failure"`
    Assertions    []Assertion       `json:"assertions,omitempty" yaml:"assertions,omitempty" doc:"Output assertions. All are evaluated regardless of prior failures" maxItems:"100"`
    Snapshot      string            `json:"snapshot,omitempty" yaml:"snapshot,omitempty" doc:"Golden file path for normalized comparison"`
    InjectFile    *bool             `json:"injectFile,omitempty" yaml:"injectFile,omitempty" doc:"Auto-inject -f sandbox path. Default true. Set false for catalog tests"`
    ExpectFailure bool              `json:"expectFailure,omitempty" yaml:"expectFailure,omitempty" doc:"Pass if command exits non-zero"`
    ExitCode      *int              `json:"exitCode,omitempty" yaml:"exitCode,omitempty" doc:"Exact expected exit code. Mutually exclusive with expectFailure"`
    Timeout       *Duration         `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Per-test timeout as Go duration string" example:"30s"`
    Skip          bool              `json:"skip,omitempty" yaml:"skip,omitempty" doc:"Skip this test"`
    SkipReason    string            `json:"skipReason,omitempty" yaml:"skipReason,omitempty" doc:"Human-readable skip reason"`
    SkipExpression celexp.Expression `json:"skipExpression,omitempty" yaml:"skipExpression,omitempty" doc:"CEL expression evaluated at discovery time. If true, test is skipped. Context: os, arch, env"`
    Retries       int               `json:"retries,omitempty" yaml:"retries,omitempty" doc:"Number of retry attempts for failing tests" maximum:"10"`
}

// Duration is a time.Duration with string-based YAML/JSON marshalling.
// Supports Go duration strings like "30s", "2m", "1m30s".
// Implements both UnmarshalYAML/MarshalYAML and UnmarshalJSON/MarshalJSON.
type Duration struct {
    time.Duration
}

// IsTemplate returns true if this test is a template (name starts with _).
func (tc *TestCase) IsTemplate() bool {
    return strings.HasPrefix(tc.Name, "_")
}

// Validate performs comprehensive validation of a TestCase.
// Checks:
//   - command is non-empty (unless template or inherited via extends)
//   - exitCode and expectFailure are not both set (mutual exclusion)
//   - snapshot or assertions — at least one must be present (unless template)
//   - template names match ^_[a-zA-Z0-9][a-zA-Z0-9_-]*$
//   - non-template names match ^[a-zA-Z0-9][a-zA-Z0-9_-]*$
//   - args does not contain "-f" or "--file"
//   - retries is 0–10
//   - assertions count ≤ 100, files count ≤ 50, tags count ≤ 20
//   - extends depth ≤ 10 (enforced during inheritance resolution)
func (tc *TestCase) Validate() error { /* ... */ }

// Max limits enforced by Validate().
const (
    MaxAssertionsPerTest = 100
    MaxFilesPerTest      = 50
    MaxTagsPerTest       = 20
    MaxExtendsDepth      = 10
    MaxTestsPerSolution  = 500
    MaxRetries           = 10
)

// TestConfig holds solution-level test configuration.
type TestConfig struct {
    SkipBuiltins SkipBuiltinsValue `json:"skipBuiltins,omitempty" yaml:"skipBuiltins,omitempty" doc:"Disable builtins: true for all, or list of specific names"`
    Env          map[string]string `json:"env,omitempty" yaml:"env,omitempty" doc:"Suite-level environment variables applied to all tests"`
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
- `pkg/solution/bundler/compose.go`: Extend `composePart` struct with `Tests map[string]*testing.TestCase` and `TestConfig *testing.TestConfig` fields. Extend compose to merge `spec.tests` (by name, reject duplicates) and `spec.testConfig` (`skipBuiltins`: true-wins for bool / union for lists; `env`: merged map, last-file-wins on conflict; `setup`/`cleanup`: appended in compose-file order)
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
| Create | `pkg/solution/testing/sandbox.go` | Temp directory creation, file copying, file diff for `__files` |
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
    - expression: 'size(__output.actions) == 3'
    - expression: '__output.actions["render-main"].inputs.output == "prod/main.tf"'
    - expression: '__output.actions["render-main"].provider == "template"'
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
- **Structured + raw output**: the tester is responsible for passing `-o json` in `args` when structured output is needed; the runner always provides raw `__stdout`/`__stderr`
- **`__files` via diff**: snapshot sandbox files before command, diff after, expose only new/modified files in CEL context as `map[string]FileInfo`
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
- **Root() isolation via `RootOptions`**: `Root()` accepts a `*RootOptions` struct and creates all state locally — no package-level mutable variables. Each concurrent test invocation gets its own `cliParams`, flag bindings, `ioStreams`, and writer. This eliminates data races without requiring mutex serialization
- **Exit codes**: new `TestFailed = 11` constant rather than reusing `ValidationFailed = 2`, which has different semantics
- **`--tag`, `--filter`, and `--solution` as `[]string`**: registered via `StringArrayVar` per project convention (not `StringSliceVar` which uses CSV parsing). Multiple flags allowed; OR logic within each flag type, AND logic between them (test must match solution filter AND name filter AND tag filter)
- **`--filter` glob library**: `doublestar.Match` — already a project dependency in `pkg/solution/bundler/discover.go`
- **No auto-inject `-o json`**: the tester is responsible for passing `-o json` in `args` when structured output is needed. The runner parses stdout as JSON when possible and populates `__output`
- **`__output` nil when unsupported**: diagnostic error rather than empty map to prevent silent assertion failures
- **Concurrency control**: `-j N` flag with `--sequential` as sugar for `-j 1` — standard test runner pattern
- **File size guard**: 10MB cap on `files[].content` to prevent OOM without blocking tests
- **Conditional skip via CEL**: `skipExpression` field evaluated at discovery time with `os`, `arch`, `env` context
- **Test retries**: `retries` field for flaky test resilience, capped at 10 attempts
- **Suite-level cleanup**: `testConfig.cleanup` runs after all tests, symmetric with `testConfig.setup`
- **Compose `testConfig` merge**: `setup`/`cleanup` steps appended in compose-file order (new merge strategy); `skipBuiltins` uses `true`-wins for bool, union for lists
- **`SkipBuiltinsValue` round-trip**: requires both `UnmarshalYAML` and `MarshalYAML` for compose `deepCopySolution` compatibility
- **Snapshot normalization pipeline**: fixed set of scrubbers (timestamps, UUIDs, sandbox paths, sorted keys). Custom scrubbers deferred to future enhancement
- **Alphabetical test ordering** over YAML definition order: consistent with how actions use `sort.Strings` within execution phases. No new infrastructure (ordered maps) needed. Tests should be independent — alphabetical ordering exposes hidden ordering dependencies
- **`--filter` supports `solution/test-name` format**: when filter contains `/`, match against `solution-name/test-name`. When no `/`, match test name only (backward-compatible). Enables scoping in multi-solution runs
- **`--solution` flag** for solution-level filtering: glob-based, ANDed with `--filter` and `--tag`. Simpler than always using `solution/test-name` format when you only care about the solution
- **`--dry-run` flag** over separate `test validate` subcommand: simpler, one less command. Validates definitions and reports discovery without executing
- **Suite-level `env`** (`testConfig.env`): avoids repeating environment variables on every test case. Precedence: process → `testConfig.env` → `TestCase.env` → `InitStep.env`
- **Fresh sandbox per retry**: each retry creates a new sandbox to ensure side-effect isolation. Init steps re-run on each attempt. Suite-level base sandbox is re-copied, not re-run
- **Binary file content guard**: non-UTF-8 files get `content` set to `"<binary file>"`, parallel to the size guard. Prevents garbled CEL string comparisons
- **`output` nil → test `error`** (not `fail`): referencing `output` when the command doesn't support `-o json` is a configuration issue, not an assertion failure
- **Extends non-existent → validation error**: referencing a test name that doesn't exist in `extends` is caught at parse time, not silently ignored
- **Extends chain depth limit of 10**: prevents stack overflow and extremely complex inheritance. Deep chains indicate a design problem
- **Max field limits**: assertions (100), files (50), tags (20), tests per solution (500). Prevents accidentally expensive test suites while remaining generous for real-world use
- **Glob zero-match → test `error`**: catches typos and missing test data early rather than silently proceeding without files
- **Snapshot captures stdout even with `expectFailure`**: snapshot comparison runs after exit code check. Enables golden-file testing of error output
- **`exitCode` and `expectFailure` mutual exclusion**: both set is a `Validate()` error. `exitCode` is strictly more expressive
- **`TestCase.Validate()` method**: comprehensive validation covering name format, field limits, mutual exclusion, and `args` content. Catches errors early at parse time
- **Assertion count in verbose output**: `PASS (4/4)` / `FAIL (2/4)` gives quick visibility into test thoroughness without requiring `-o json`
- **Duration as string type**: `"30s"` YAML format with custom marshal/unmarshal. More human-readable than integer seconds and more explicit than Go's default nanosecond marshalling
- **Environment precedence chain documented**: process → `testConfig.env` → `TestCase.env` → `InitStep.env`. Each level merges with previous; only conflicting keys are overridden
- **Compose test ordering independent of file order**: tests from all compose files execute alphabetically, not in compose-file order. Only `testConfig.setup`/`cleanup`/`env` follow compose-file ordering
