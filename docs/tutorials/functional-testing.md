---
title: "Functional Testing"
weight: 85
---

# Functional Testing

Functional testing lets you define automated tests for your solutions directly in the solution YAML.
Tests execute scafctl commands in isolated sandboxes and validate output using assertions, snapshots,
and CEL expressions.

This tutorial walks through every feature of `scafctl test functional` — from basic tests to
advanced CI integration.

---

## Writing Your First Test

Add a `tests` section to your solution's `spec`. Create a file called `solution.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-solution
spec:
  resolvers:
    greeting:
      description: A greeting message
      resolve:
        with:
          - provider: static
            inputs:
              value: Hello, World!

  tests:
    render-basic:
      description: Verify solution renders successfully
      command: [render, solution]
      assertions:
        - expression: __exitCode == 0
        - contains: greeting
```

Run the test:

```bash
scafctl test functional -f solution.yaml
```

Expected output:

```
SOLUTION       TEST              STATUS   DURATION
my-solution    builtin:lint      PASS     12ms
my-solution    builtin:parse     PASS     1ms
my-solution    render-basic      PASS     8ms

3 passed, 0 failed, 0 errors, 0 skipped (21ms)
```

Each test specifies a `command` (the scafctl subcommand to run) and one or more
`assertions` to validate the output. The runner automatically injects
`-f <sandbox-copy-of-solution>` unless you set `injectFile: false`.

> **Note:** The YAML snippets in the remaining sections of this tutorial show only the `tests:` block or relevant portion. To use them, add them to the `spec.tests` section of your solution file — like the `solution.yaml` example above.

---

## Assertions

Assertions validate command output. Each assertion sets exactly one of the
assertion fields: `expression`, `contains`, `notContains`, `regex`, or `notRegex`.
All assertions in a test are **always evaluated** regardless of whether earlier ones fail,
so you see every problem at once.

### CEL Expressions

CEL assertions are the most powerful. The expression has access to these context variables:

| Variable | Type | Description |
|----------|------|-------------|
| `__stdout` | `string` | Raw standard output text |
| `__stderr` | `string` | Raw standard error text |
| `__exitCode` | `int` | Process exit code |
| `__output` | `map` | Parsed JSON from stdout (`nil` if stdout is not valid JSON) |
| `__files` | `map` | Files created or modified in the sandbox during execution |

```yaml
assertions:
  # Check exit code
  - expression: __exitCode == 0

  # Check parsed JSON data (requires -o json in args)
  - expression: __output.greeting == "Hello, World!"

  # Check stderr is empty
  - expression: __stderr == ""

  # Complex expression
  - expression: __exitCode == 0 && size(__stdout) > 0

  # Access both stdout and stderr
  - expression: '__stderr.contains("warning") && __stdout.contains("success")'
```

> **Important**: `__output` is only populated when stdout is valid JSON. If you need structured
> output, include `"-o", "json"` in your test's `args` field. If your expression references
> `__output` when it's `nil`, the test reports as `error` (not `fail`) with a diagnostic message.

#### `__output` Structure Per Command

The shape of `__output` depends on which command you run:

| Command | `__output` structure |
|---------|---------------------|
| `render solution` | `__output.actions` — each action has `provider`, `inputs`, `dependsOn`, `when` |
| `run resolver` | `__output.<resolverName>` = resolved value |
| `run solution` | `__output.status`, `__output.actions`, `__output.duration` |
| `lint` | `__output.findings`, `__output.errorCount`, `__output.warnCount` |

Use verbose mode (`-v`) to inspect the raw JSON structure for any command.

### Contains

Checks that a substring exists in the output:

```yaml
assertions:
  - contains: "expected text"
```

### Not Contains

Checks that a substring does NOT exist:

```yaml
assertions:
  - notContains: "ERROR"
```

### Regex

Matches output against a regular expression:

```yaml
assertions:
  - regex: "version: \\d+\\.\\d+\\.\\d+"
```

### Not Regex

Ensures a pattern does NOT match anywhere in the output:

```yaml
assertions:
  - notRegex: "panic|fatal"
```

### Target Field

By default, `contains`, `notContains`, `regex`, and `notRegex` match against **stdout**.
Use the `target` field to match against `stderr` or `combined` (stdout + stderr):

```yaml
assertions:
  # Match against stdout (default)
  - contains: "rendered successfully"

  # Match against stderr
  - contains: "warning: deprecated field"
    target: stderr

  # Match against combined stdout + stderr
  - notContains: "panic"
    target: combined
```

The `target` field has no effect on `expression` assertions — CEL expressions access
`__stdout`, `__stderr`, and all other variables directly.

### Custom Failure Messages

Add a `message` to any assertion for clearer failure output:

```yaml
assertions:
  - expression: 'size(__output.actions) == 1'
    message: "Should produce exactly one action"
```

When this assertion fails, you'll see:

```
✗ expression: size(__output.actions) == 1
  size(__output.actions) = 3
  Expected 1, got 3
  Message: Should produce exactly one action
```

### CEL Assertion Diagnostics

The runner automatically evaluates sub-expressions when CEL assertions fail, providing
actionable diagnostics instead of just "expected true, got false":

```
✗ expression: __output.actions["render-main"].inputs.output == "prod/main.tf"
  __output.actions["render-main"].inputs.output = "dev/main.tf"
  Expected "prod/main.tf", got "dev/main.tf"
```

### File Assertions (`__files`)

The `__files` variable exposes files created or modified in the sandbox during command execution.
The runner snapshots file paths and modification times before the command runs, then diffs
after execution. Only new or changed files appear in `__files`.

```yaml
assertions:
  # Check that a file was created
  - expression: '__files["output/main.tf"].exists'

  # Check file content
  - expression: '__files["output/main.tf"].content.contains("resource")'

  # Check number of generated files
  - expression: 'size(__files) == 3'
```

Each entry is keyed by relative path from the sandbox root with `exists` (bool) and `content` (string) fields.
Files larger than 10MB have content set to `"<file too large>"`, and binary files get `"<binary file>"`.

---

## Test Inheritance

Define reusable test templates with names starting with `_`. Templates are not executed
directly but can be inherited via `extends`:

```yaml
tests:
  _render-base:
    description: Base template for rendering tests
    command: [render, solution]
    tags: [render]
    assertions:
      - expression: __exitCode == 0

  _prod-base:
    description: Base template for prod tests
    args: ["-r", "env=prod"]
    tags: [prod]

  render-smoke:
    description: Quick render check
    extends: [_render-base]
    assertions:
      - contains: greeting

  render-prod:
    description: Render with prod configuration
    extends: [_render-base, _prod-base]
    assertions:
      - expression: '__output.actions["render-main"].inputs.output == "prod/main.tf"'
```

Multiple templates can be specified — they are applied left to right. The resolved
`render-prod` test inherits:
- `command: [render, solution]` from `_render-base`
- `args: ["-r", "env=prod"]` from `_prod-base`
- `tags: [render, prod]` merged from both bases
- All three assertions (one from `_render-base`, one from `render-prod`)

### Field Merge Strategy

| Field | Merge Behavior |
|-------|----------------|
| `command`, `description`, `timeout`, `expectFailure`, `exitCode`, `skip`, `injectFile`, `snapshot`, `skipExpression`, `retries` | Child wins if set |
| `args` | Appended (base args first, then child) |
| `assertions` | Appended (base first, then child) |
| `files`, `tags` | Appended (deduplicated) |
| `init` | Base steps prepended before child steps |
| `cleanup` | Base steps appended after child steps |
| `env` | Merged map (child values override base on key conflict) |

Templates can extend other templates. **Extends chain depth is limited to 10 levels.**
Circular extends chains and references to non-existent test names are validation errors.

> `scafctl lint` produces a **warning** for templates that are never referenced by any `extends` field.

---

## Snapshots

Snapshots compare command output against a golden file:

```yaml
tests:
  render-snapshot:
    description: Compare render output to golden file
    command: [render, solution]
    args: ["-o", "json"]
    snapshot: testdata/expected-render.json
```

Create or update snapshots:

```bash
# Update all snapshots
scafctl test functional -f solution.yaml --update-snapshots

# Update specific snapshots
scafctl test functional -f solution.yaml --update-snapshots --filter "snapshot-*"

# Run normally (compares against existing snapshots)
scafctl test functional -f solution.yaml
```

Snapshots are automatically normalized:
- JSON map keys are sorted deterministically
- Temporary paths are replaced with `<SANDBOX>`
- Timestamps are replaced with `<TIMESTAMP>`
- UUIDs are replaced with `<UUID>`

On mismatch, a unified diff is displayed:

```
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
```

Snapshots can be used alongside other assertions — all must pass.

### Bundle Include

If your solution uses `bundle.include`, make sure test files are covered:

```yaml
bundle:
  include:
    - "testdata/**"

spec:
  tests:
    my-test:
      files: ["testdata/input.txt"]
      # ...
```

The `unbundled-test-file` lint rule will flag test files not covered by `bundle.include`.

---

## Init and Cleanup Steps

Tests can define setup commands before and teardown commands after execution:

```yaml
tests:
  integration-test:
    description: Test with setup/teardown
    init:
      - command: "mkdir -p testdata"
      - command: "echo 'hello' > testdata/input.txt"
    command: [run, resolver]
    cleanup:
      - command: "echo 'cleanup complete'"
    assertions:
      - expression: __exitCode == 0
```

Init steps use the same schema as the exec provider — you can set `args`, `stdin`,
`workingDir`, `env`, `timeout`, and `shell`:

```yaml
init:
  - command: "scafctl config set defaults.environment staging"
    env:
      SCAFCTL_CONFIG_DIR: "$SCAFCTL_SANDBOX_DIR"
  - command: "echo 'setting up test data'"
    shell: bash
    timeout: 10
    workingDir: "templates"
```

Cleanup steps run **even when the test fails**. Cleanup failures are logged but don't
change the test status. Init failures cause the test to report as `error` (not `fail`).

### Environment Variables

The runner automatically injects `SCAFCTL_SANDBOX_DIR` (the absolute path to the sandbox
directory) into every init step and test command.

---

## Per-Test Environment Variables

Set environment variables for a specific test's init, command, and cleanup steps:

```yaml
tests:
  custom-env-test:
    description: Test with custom environment
    env:
      CUSTOM_VAR: "test-value"
      API_ENDPOINT: "http://localhost:8080"
    command: [run, resolver]
    assertions:
      - expression: __exitCode == 0
```

### Environment Precedence

Variables are resolved in this precedence order (highest wins):

| Priority | Source | Description |
|----------|--------|-------------|
| 1 (lowest) | Process environment | Inherited from the parent process |
| 2 | `testConfig.env` | Suite-level env applied to all tests |
| 3 | `TestCase.env` | Per-test env overrides suite-level on key conflict |
| 4 (highest) | `InitStep.env` | Per-step env overrides all others on key conflict |

`SCAFCTL_SANDBOX_DIR` is always injected by the runner and cannot be overridden.

---

## Test Files

Copy additional files into the sandbox for test execution:

```yaml
tests:
  file-test:
    description: Test with additional files
    files:
      - testdata/config.json
      - templates/*.yaml
    command: [render, solution]
    assertions:
      - expression: __exitCode == 0
```

Files are copied maintaining their relative directory structure. Symlinks and path
traversal above the solution root (`..`) are rejected. **Glob patterns matching zero
files produce a test `error`** to catch typos early.

---

## Expected Failures

Test that a command fails as expected:

```yaml
tests:
  validation-error:
    description: Invalid input should fail
    command: [render, solution]
    args: ["-r", "env=invalid"]
    expectFailure: true
    assertions:
      - contains: "Invalid environment"
      - notContains: "panic"
```

For exact exit code matching:

```yaml
tests:
  specific-exit-code:
    description: Expect specific exit code
    command: [lint]
    exitCode: 3
    assertions:
      - contains: "validation error"
```

> **Note**: `exitCode` and `expectFailure` are **mutually exclusive** — setting both is a
> validation error. `exitCode` is strictly more expressive.

---

## Timeouts

Set per-test timeouts using Go duration strings:

```yaml
tests:
  slow-test:
    description: Test that takes longer
    timeout: "2m"
    command: [run, solution]
    assertions:
      - expression: __exitCode == 0
```

Default per-test timeout is `30s`. Override globally with `--test-timeout`:

```bash
scafctl test functional -f solution.yaml --test-timeout 1m
```

Set a global timeout for the entire run with `--timeout`:

```bash
scafctl test functional -f solution.yaml --timeout 10m
```

---

## Skipping Tests

### Static Skip

```yaml
tests:
  temporarily-disabled:
    description: Skipped during development
    skip: true
    skipReason: "Waiting on upstream provider fix"
    command: [render, solution]
    assertions:
      - expression: __exitCode == 0
```

### Conditional Skip

Skip based on runtime conditions using CEL expressions with `os`, `arch`, and `env` context:

```yaml
tests:
  linux-only:
    description: Only runs on Linux
    skipExpression: 'os != "linux"'
    command: [run, solution]
    assertions:
      - expression: __exitCode == 0

  ci-only:
    description: Only runs in CI
    skipExpression: '!("CI" in env)'
    command: [run, solution]
    assertions:
      - expression: __exitCode == 0
```

---

## Retries

Retry flaky tests up to 10 times:

```yaml
tests:
  flaky-test:
    description: Retry on failure
    retries: 3
    command: [run, resolver]
    assertions:
      - expression: __exitCode == 0
```

Each retry creates a **fresh sandbox**. The test passes if any attempt succeeds:

```
my-solution   flaky-test   PASS (retry 2/3)   45ms
```

---

## File Injection Control

By default, the runner auto-injects `-f <sandbox-solution-path>` into every command.
Disable this for commands that don't accept `-f`:

```yaml
tests:
  config-check:
    description: Check config (does not use -f)
    injectFile: false
    command: [config, get]
    assertions:
      - expression: __exitCode == 0
```

> **Important**: Never include `-f` or `--file` in `args` — the runner always rejects this
> regardless of the `injectFile` setting.

---

## Test Tags

Tag tests for selective execution:

```yaml
tests:
  renders-dev:
    description: Render dev config
    command: [render, solution]
    tags: [smoke, render, fast]
    assertions:
      - expression: __exitCode == 0
```

Filter by tags:

```bash
# Run only "smoke" tests
scafctl test functional -f solution.yaml --tag smoke

# Combine with name filter
scafctl test functional -f solution.yaml --tag render --filter "render-*"
```

A test matches if it has **any** of the specified tags (OR logic). Tags inherited via
`extends` are included in the match.

---

## Suite-Level Configuration

Configure settings that apply to all tests in a solution:

```yaml
spec:
  testConfig:
    skipBuiltins: true
    env:
      TEST_MODE: "true"
      SCAFCTL_CONFIG_DIR: "$SCAFCTL_SANDBOX_DIR"
    setup:
      - command: "mkdir -p templates"
      - command: "scafctl config set defaults.environment staging"
    cleanup:
      - command: "echo 'suite teardown complete'"
```

### Suite-Level Setup

When `testConfig.setup` is defined:

1. A base sandbox is created and solution + bundle files are copied
2. Setup steps run sequentially in the base sandbox
3. For each test, the prepared base sandbox is copied to an isolated per-test sandbox
4. Per-test `init` steps then run in the per-test sandbox

This avoids duplicating init steps across every test. If any setup step fails, all tests
for that solution report as `error`.

### Selective Builtin Skipping

`skipBuiltins` accepts either a boolean or a list of names:

```yaml
# Skip all builtins
testConfig:
  skipBuiltins: true

# Skip only specific builtins
testConfig:
  skipBuiltins:
    - resolve-defaults
    - render-defaults
```

---

## Compose Test Files

Split tests across multiple files using compose. The `compose` field goes at the **top level**
of the solution YAML, not inside `spec`.

Create a main solution file called `solution.yaml`:

```yaml
# solution.yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-solution
compose:
  - resolvers/environment.yaml
  - tests/smoke.yaml
  - tests/validation.yaml
spec:
  workflow:
    actions:
      render-main:
        provider: template
        inputs:
          template:
            tmpl: "main.tf.tmpl"
          output: "main.tf"
```

Create a file called `tests/smoke.yaml`:

```yaml
# tests/smoke.yaml
spec:
  tests:
    smoke-render:
      description: Smoke test for rendering
      command: [render, solution]
      tags: [smoke]
      assertions:
        - expression: __exitCode == 0
```

Create a file called `tests/validation.yaml`:

```yaml
# tests/validation.yaml
spec:
  tests:
    rejects-invalid:
      description: Invalid input fails
      command: [run, resolver]
      args: ["-r", "env=invalid"]
      expectFailure: true
      tags: [validation]
      assertions:
        - contains: "Invalid"
```

Tests from all compose files are merged by name (duplicates are rejected). Execution order
is alphabetical regardless of compose file order.

### Compose Merge for testConfig

When `testConfig` appears in multiple compose files:

| Field | Merge Behavior |
|-------|----------------|
| `skipBuiltins` (bool) | `true` wins |
| `skipBuiltins` (list) | Unioned (deduplicated) |
| `setup` / `cleanup` | Appended in compose-file order |
| `env` | Merged map; last compose file wins on key conflict |

---

## CI Integration

### JUnit XML Reports

Generate JUnit XML reports for CI systems:

```bash
scafctl test functional -f solution.yaml --report-file results.xml
```

JUnit output uses `<failure>` for assertion failures and `<error>` for infrastructure/setup
issues (e.g., init step failures), making it easy to distinguish test bugs from environment
problems.

### Exit Codes

| Code | Constant | Meaning |
|------|----------|---------|
| `0` | `exitcode.Success` | All tests passed |
| `11` | `exitcode.TestFailed` | One or more tests failed |
| `3` | `exitcode.InvalidInput` | Configuration or usage error |

### Fail Fast

Stop on the first failure per solution:

```bash
scafctl test functional -f solution.yaml --fail-fast
```

Other solutions continue to run. Use this for quick feedback during debugging.

### Quiet Output

For CI pipelines, use quiet format for exit-code-only behavior:

```bash
scafctl test functional -f solution.yaml -o quiet
```

---

## Listing Tests

List available tests without running them:

```bash
scafctl test list -f solution.yaml

# Include builtin tests
scafctl test list -f solution.yaml --include-builtins

# List from a directory
scafctl test list --tests-path ./solutions/
```

Example output:

```
SOLUTION             TEST                    COMMAND          TAGS           SKIP
my-solution          render-basic            render solution  smoke,render   -
my-solution          render-prod             render solution  render         -
my-solution          validation-error        run resolver     validation     -
my-solution          temporarily-disabled    render solution                 Waiting on upstream fix
```

---

## Filtering

Run specific tests, solutions, or tags:

```bash
# Filter by test name glob
scafctl test functional -f solution.yaml --filter "render-*"

# Filter by tag
scafctl test functional -f solution.yaml --tag smoke

# Filter by solution name (directory scan)
scafctl test functional --tests-path ./solutions/ --solution "terraform-*"

# Combined solution/test-name format
scafctl test functional --tests-path ./solutions/ --filter "terraform-*/render-*"

# Combine all filters (ANDed: must match all)
scafctl test functional --tests-path ./solutions/ \
  --solution "terraform-*" --tag smoke --filter "render-*"
```

When `--tag`, `--filter`, and `--solution` are combined, they are **ANDed**: a test must
match the solution filter AND the name filter AND have a matching tag.

---

## Concurrency

Tests run sequentially by default. Use `-j` to run tests in parallel:

```bash
# Run up to 4 tests concurrently
scafctl test functional -f solution.yaml -j 4

# Force sequential execution (equivalent to -j 1)
scafctl test functional -f solution.yaml --sequential
```

Each test gets its own isolated sandbox — no shared mutable state between concurrent tests.

---

## Verbose Output

Use `-v` to see command details, init output, and assertion counts:

```bash
scafctl test functional -f solution.yaml -v
```

Verbose output for passing tests shows assertion counts:

```
SOLUTION         TEST                    STATUS         DURATION
my-solution      render-basic            PASS (2/2)     12ms
my-solution      render-prod             PASS (3/3)     15ms
```

Verbose failure output includes the full command, sandbox path, stdout, stderr, and exit code.

---

## Debugging

### Keep Sandbox

Preserve sandbox directories for failed tests to inspect files manually:

```bash
scafctl test functional -f solution.yaml --keep-sandbox
```

### Dry Run

Validate test definitions without executing any commands:

```bash
scafctl test functional -f solution.yaml --dry-run
```

This resolves `extends` chains, validates test names, and reports discovery results.
Exits 0 if valid, exit code 3 if invalid.

---

## Builtin Tests

By default, four builtin tests run for every solution:

| Name | Description |
|------|-------------|
| `builtin:parse` | Validates YAML parsing |
| `builtin:lint` | Runs lint rules (warnings OK) |
| `builtin:resolve-defaults` | Resolves with default parameters |
| `builtin:render-defaults` | Renders with default parameters |

Builtins run before user-defined tests. Skip all builtins:

```bash
scafctl test functional -f solution.yaml --skip-builtins
```

Or skip specific ones in your solution's `testConfig`:

```yaml
spec:
  testConfig:
    skipBuiltins:
      - lint
      - resolve-defaults
```

---

## Test Name Rules

Test names must match `^[a-zA-Z0-9][a-zA-Z0-9_-]*$`:
- Start with a letter or digit
- May contain letters, digits, hyphens, and underscores
- Template names start with `_` (e.g., `_base-render`)

Invalid names are rejected during parsing and surfaced as lint errors.

---

## Command Aliases

The CLI provides shorthand aliases:

```bash
# These are all equivalent:
scafctl test functional -f solution.yaml
scafctl test func -f solution.yaml
scafctl test fn -f solution.yaml

# These are all equivalent:
scafctl test list -f solution.yaml
scafctl test ls -f solution.yaml
scafctl test l -f solution.yaml
```

---

## Complete Flag Reference

### `scafctl test functional`

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--file` | `-f` | — | Path to a single solution file |
| `--tests-path` | | — | Directory to scan for solution files |
| `--output` | `-o` | `table` | Output format: `table`, `json`, `yaml`, `quiet` |
| `--report-file` | | — | Path to write JUnit XML report |
| `--update-snapshots` | | `false` | Update golden files instead of comparing |
| `--sequential` | | `false` | Run tests sequentially (sugar for `-j 1`) |
| `--concurrency` | `-j` | `1` | Maximum number of tests to run in parallel |
| `--skip-builtins` | | `false` | Skip builtin tests for all solutions |
| `--test-timeout` | | — | Default timeout per test (e.g., `30s`, `5m`) |
| `--timeout` | | — | Global timeout for all tests |
| `--filter` | | — | Name glob pattern (repeatable). Supports `solution/test-name` format |
| `--tag` | | — | Tag filter (repeatable). A test matches if it has any specified tag |
| `--solution` | | — | Solution name glob (repeatable). ANDed with `--filter` and `--tag` |
| `--dry-run` | | `false` | Validate tests without executing |
| `--fail-fast` | | `false` | Stop remaining tests per solution on first failure |
| `--verbose` | `-v` | `false` | Show full command, init output, and assertion counts |
| `--keep-sandbox` | | `false` | Preserve sandbox directories after test execution |
| `--no-progress` | | `false` | Disable live progress spinners during test execution |
| `--watch` | `-w` | `false` | Watch solution files for changes and re-run tests |

### `scafctl test list`

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--file` | `-f` | — | Path to a single solution file |
| `--tests-path` | | — | Directory to scan for solution files |
| `--output` | `-o` | `table` | Output format: `table`, `json`, `yaml`, `quiet` |
| `--include-builtins` | | `false` | Include builtin tests in the listing |
| `--tag` | | — | Filter by tag (repeatable) |
| `--solution` | | — | Filter by solution name glob (repeatable) |
| `--filter` | | — | Filter by test name glob (repeatable) |


## Watch Mode

Watch mode monitors your solution files for changes and automatically re-runs
tests, giving you a tight feedback loop during development.

### Basic Usage

```bash
scafctl test functional -f solution.yaml --watch
```

The `--watch` (or `-w`) flag:
1. Runs all matched tests immediately
2. Watches the solution file, compose files, and parent directories
3. On file change, debounces rapid writes (300ms), then re-runs tests
4. Clears the terminal before each re-run (on TTY terminals)
5. Repeats until you press **Ctrl-C**

### Scoped Watches

Combine `--watch` with filters to focus on specific tests:

```bash
# Only re-run smoke tests
scafctl test functional -f solution.yaml --watch --tag smoke

# Only re-run tests matching a name pattern
scafctl test functional -f solution.yaml --watch --filter "render-*"

# Watch an entire directory of solutions
scafctl test functional --tests-path ./solutions/ --watch
```

### What Gets Watched

The watcher monitors:
- The solution file itself (e.g., `solution.yaml`)
- All compose files referenced by the solution's `compose` field
- Parent directories of solution files (to detect new/renamed files)

Only `.yaml` and `.yml` file changes trigger re-runs. Non-YAML files are ignored.

### Debouncing

When an editor saves a file, it often writes multiple times in quick succession
(rename-write-rename, or write-then-format). The watcher collapses these into a
single re-run by waiting 300ms after the last change event before triggering.

### Example Session

```
$ scafctl test functional -f solution.yaml --watch --tag smoke
[watch] watching solution.yaml for changes...
[watch] (initial run) — running tests...
SOLUTION            TEST              STATUS   DURATION
tested-solution     builtin:parse     PASS     2ms
tested-solution     render-basic      PASS     9ms

2 passed, 0 failed, 0 errors, 0 skipped (11ms)
[watch] waiting for file changes... (Ctrl-C to exit)

# ...edit solution.yaml, save...

[watch] solution.yaml — running tests...
SOLUTION            TEST              STATUS   DURATION
tested-solution     builtin:parse     PASS     1ms
tested-solution     render-basic      PASS     8ms

2 passed, 0 failed, 0 errors, 0 skipped (9ms)
[watch] waiting for file changes... (Ctrl-C to exit)

^C
[watch] stopped
```

### Watch Mode with Compose Files

When a solution uses `compose` to split tests across files, the watcher
automatically monitors all referenced compose files:

```yaml
# solution.yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-solution
compose:
  - tests/*.yaml
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: Hello
```

```bash
# All compose files under tests/ are watched automatically
scafctl test functional -f solution.yaml --watch
```

Editing any file under `tests/` triggers a re-run. Creating a new compose file
in the directory also triggers re-discovery and a new run.

### Tips

- **Use with `--verbose`** to see full assertion counts on each re-run:
  ```bash
  scafctl test functional -f solution.yaml --watch -v
  ```
- **Combine with `--fail-fast`** to stop early when iterating on a broken test:
  ```bash
  scafctl test functional -f solution.yaml --watch --fail-fast
  ```
- **CI environments** should not use `--watch` — it's designed for interactive
  development only.
- **Progress output** is automatically re-created for each watch cycle on TTY
  terminals. Use `--no-progress` if you find the spinners distracting:
  ```bash
  scafctl test functional -f solution.yaml --watch --no-progress
  ```

---

## Test Scaffolding (`scafctl test init`)

Writing tests from scratch can be tedious — especially for solutions with many resolvers and
validation rules. The `test init` command generates a starter test suite by analyzing your
solution's structure. No commands are executed; it performs structural analysis only.

### Basic Usage

```bash
scafctl test init -f solution.yaml
```

This outputs YAML to stdout that you can paste into your solution's `spec.tests` section or
redirect to a file:

```bash
# Save to a file
scafctl test init -f solution.yaml > tests.yaml

# Append to an existing compose test file
scafctl test init -f solution.yaml >> solution-tests.yaml
```

### What Gets Generated

The command analyzes your solution and generates:

| Test Category | What It Creates |
|---------------|-----------------|
| **Smoke tests** | `resolve-defaults` — verifies all resolvers resolve with defaults |
| | `render-defaults` — verifies the solution renders with defaults |
| | `lint` — verifies the solution has no lint errors |
| **Per-resolver tests** | `resolver-<name>` — verifies each resolver produces non-null output |
| **Validation failure tests** | `resolver-<name>-invalid` — verifies resolvers with validation rules reject invalid input (`expectFailure: true`) |
| **Per-action tests** | `action-<name>` — verifies each workflow action executes successfully |

### Example

Given this solution:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-app
  version: 1.0.0
spec:
  resolvers:
    repo:
      description: Repository name
      resolve:
        with:
          - provider: static
            inputs:
              value: my-app

    version:
      description: Version to build
      resolve:
        with:
          - provider: parameter
            inputs:
              key: version
          - provider: static
            inputs:
              value: dev
      validate:
        with:
          - provider: validation
            inputs:
              match: '^(dev|\d+\.\d+\.\d+.*)$'
            message: "Invalid version format"
```

Running `scafctl test init -f solution.yaml` produces:

```yaml
# Generated test scaffold for solution.yaml
# Paste this into your solution's spec section or a compose test file.
# Customize assertions and parameters to match your expected behavior.

tests:
    lint:
        description: Verify solution has no lint errors
        command:
            - lint
        tags:
            - smoke
            - lint
        exitCode: 0
    render-defaults:
        description: Verify solution renders with default values
        command:
            - render
            - solution
        tags:
            - smoke
            - render
        exitCode: 0
    resolve-defaults:
        description: Verify all resolvers resolve with default values
        command:
            - run
            - resolver
        args:
            - -o
            - json
        tags:
            - smoke
            - resolvers
        exitCode: 0
    resolver-repo:
        description: Verify resolver "repo" produces expected output
        command:
            - run
            - resolver
        args:
            - --resolver
            - repo
            - -o
            - json
        tags:
            - resolvers
        exitCode: 0
        assertions:
            - expression: __output.repo != null
              message: Resolver "repo" should produce a non-null value
    resolver-version:
        description: Verify resolver "version" produces expected output
        command:
            - run
            - resolver
        args:
            - --resolver
            - version
            - -o
            - json
        tags:
            - resolvers
        exitCode: 0
        assertions:
            - expression: __output.version != null
              message: Resolver "version" should produce a non-null value
    resolver-version-invalid:
        description: Verify resolver "version" rejects values not matching pattern
        command:
            - run
            - resolver
        args:
            - --resolver
            - version
            - --param
            - version=___invalid___
        tags:
            - resolvers
            - validation
            - negative
        expectFailure: true
```

### Customizing Generated Tests

The scaffold is a starting point. After generating, you should:

1. **Add specific assertions** — replace generic `__output.X != null` with checks matching your expected values
2. **Tune validation failure inputs** — replace `___invalid___` with realistic bad inputs
3. **Add tags** — organize tests with domain-specific tags like `smoke`, `integration`
4. **Add test templates** — extract common command/assertion patterns into `_`-prefixed templates and use `extends`
5. **Remove unnecessary tests** — if some resolvers don't need individual tests, remove them

### Difference from `-o test`

| Feature | `test init` | `-o test` (future) |
|---------|------------|-------------------|
| **Execution** | No commands run — structural analysis only | Executes the command and captures output |
| **Output** | Skeleton tests with generic assertions | Complete tests with output-derived assertions + snapshots |
| **Use case** | Bootstrapping a new test suite | Capturing known-good behavior |

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

✅ **Implemented.** See the [Test Scaffolding](#test-scaffolding-scafctl-test-init) section above.

## Next Steps

- [Configuration Tutorial](config-tutorial.md) — Manage application configuration
- [Snapshots Tutorial](snapshots-tutorial.md) — Capture and compare execution snapshots
- [Resolver Tutorial](resolver-tutorial.md) — Deep dive into resolvers
- [Provider Reference](provider-reference.md) — Complete provider documentation

---