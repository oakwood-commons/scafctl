# Functional Test Solutions

Self-testing solution suite that exercises scafctl features from the solution execution level using `scafctl test functional`.

## Running

```bash
# Run all functional test solutions
scafctl test functional --tests-path tests/integration/solutions

# Run with tag filter
scafctl test functional --tests-path tests/integration/solutions --tag smoke
scafctl test functional --tests-path tests/integration/solutions --tag provider
scafctl test functional --tests-path tests/integration/solutions --tag edge-case

# Run a specific solution
scafctl test functional --tests-path tests/integration/solutions --solution "test-static-*"

# List all discovered tests
scafctl test list --tests-path tests/integration/solutions

# Via task runner (builds binary first)
task integration
```

## Structure

```
tests/integration/solutions/
├── hello-world/              # Minimal smoke test (pre-existing)
├── composed/                 # Minimal compose test (pre-existing)
├── providers/                # Provider-specific tests
│   ├── static/               # Literal values, all types
│   ├── env/                  # Environment variable operations
│   ├── cel/                  # CEL expression evaluation
│   ├── exec/                 # Shell command execution
│   ├── file/                 # File read/write/exists
│   ├── directory/            # Directory list/mkdir
│   ├── go-template/          # Go template rendering
│   ├── go-template-extensions/ # Go template extensions (Sprig + toHcl + toYaml/fromYaml)
│   ├── validation/           # Match/notMatch/expression validation
│   ├── sleep/                # Sleep/timing
│   ├── http/                 # HTTP requests (via mock server), autoParseJson, polling
│   ├── github/               # GitHub API provider (via mock server)
│   ├── exec-mocking/         # Exec command mocking (mock service rules)
│   ├── git/                  # Git operations (via local bare repo)
│   ├── debug/                # Debug output formatting
│   ├── parameter/            # CLI parameter passing (-r flag)
│   └── solution/             # Sub-solution composition
├── resolvers/                # Resolver feature tests
│   ├── dag/                  # Dependency graph ordering
│   ├── until/                # Fallback chains, until conditions
│   ├── type-coercion/        # Type field coercion
│   ├── transform-chain/      # Multi-step transform pipelines
│   ├── conditional/          # When conditions
│   ├── timeout/              # Per-resolver timeout
│   ├── foreach-filter/       # ForEach filter tests
│   ├── messages/             # Custom error messages (messages.error)
│   └── sensitive/            # Sensitive value masking
├── actions/                  # Workflow action tests
│   ├── exclusive/            # Mutual exclusion (exclusive field)
│   ├── sequential/           # Sequential dependsOn chains
│   ├── conditional/          # When conditions on actions
│   ├── parallel/             # Parallel execution with dependencies
│   ├── error-handling/       # onError continue/fail behavior
│   ├── retry/                # Retry with fixed/exponential/linear backoff
│   ├── conditional-retry/    # retryIf conditional retry
│   ├── finally/              # Finally cleanup section
│   ├── foreach/              # ForEach iteration over arrays
│   ├── timeout/              # Per-action timeout
│   └── result-schema/        # Result schema validation
├── rendering/                # Template rendering tests
├── composition/              # Multi-file compose tests
│   └── parts/                # Composed YAML fragments
├── plugins/                  # Plugin CLI command tests
│                             #   build plugin help, missing flags, subcommand discovery
├── test-generation/          # Test generation tests
└── edge-cases/               # Negative/error tests
    ├── validation-failures/  # Intentional validation errors
    ├── invalid-provider/     # Unknown provider handling
    ├── invalid-exclusive/    # Invalid exclusive references
    └── timeout-enforcement/  # Timeout violation behavior
```

## Tags

| Tag | Description |
|-----|-------------|
| `smoke` | Quick verification tests, good for CI gates |
| `provider` | Provider-specific tests |
| `static`, `env`, `cel`, `exec`, `file`, `directory`, `go-template`, `go-template-extensions`, `hcl`, `validation`, `sleep` | Individual provider tests |
| `http` | HTTP provider tests (uses mock server) |
| `git` | Git provider tests (uses local bare repo) |
| `debug` | Debug provider tests |
| `parameter` | Parameter provider tests |
| `solution-provider` | Solution provider composition tests |
| `action` | Workflow action tests |
| `sequential`, `conditional`, `parallel`, `exclusive` | Action ordering/condition tests |
| `error-handling`, `retry`, `conditional-retry` | Action error/retry tests |
| `finally`, `foreach`, `timeout`, `result-schema` | Action feature tests |
| `resolver` | Resolver feature tests |
| `dag`, `until`, `type-coercion`, `transform`, `conditional`, `timeout`, `sensitive` | Individual resolver feature tests |
| `rendering` | Template rendering tests |
| `composition` | Multi-file compose tests |
| `plugin` | Plugin CLI command tests (build plugin, list, help) |
| `edge-case` | Error handling and boundary tests |
| `negative` | Tests expecting failures |

## Conventions

- **One solution per feature area** for isolation — a failure in file provider tests doesn't mask CEL provider results
- **`_base` templates** with `extends` for DRY test definitions within each solution
- **`testConfig.skipBuiltins`** when builtins would fail by design (edge-case solutions)
- **Self-contained tests** — use `init`/`cleanup` steps and sandbox isolation; no external dependencies
- **JSON output** (`-o json`) with CEL `__output` assertions for structured verification
- **`expectFailure: true`** for negative tests that should produce errors

## Adding a New Test Solution

1. Create `tests/integration/solutions/<category>/<feature>/solution.yaml`
2. Define resolvers that exercise the feature
3. Add inline `tests:` with a `_base` template and tagged test cases
4. Set `testConfig.skipBuiltins` if builtins don't apply
5. Run `scafctl test functional -f <your-solution.yaml>` to verify
6. The `task integration` step will automatically discover it

## Mock Services

The test framework supports `testConfig.services` for starting background mock servers.
This eliminates external dependencies for providers like HTTP.

```yaml
testConfig:
  services:
    - name: mock-api
      type: http                    # Only "http" is currently supported
      portEnv: MOCK_HTTP_PORT       # Env var set to the server's random port
      baseUrlEnv: MOCK_BASE_URL     # Env var set to http://127.0.0.1:<port>
      routes:
        - path: /api/users
          method: GET               # Empty = match all methods
          status: 200               # Default 200
          body: '{"users": []}'     # Response body
          headers:                  # Response headers
            Content-Type: application/json
          delay: "100ms"            # Simulated latency
          echo: true                # Return request details as JSON
```

All mock servers expose a `/__health` endpoint for readiness checks.

## Excluded Providers

These providers are excluded from functional tests due to external dependencies:
- **`secret`** — requires keyring/credential store
- **`identity`** — requires authentication handlers

They can be added later with conditional `skip` expression guards.
