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
│   ├── validation/           # Match/notMatch/expression validation
│   └── sleep/                # Sleep/timing
├── resolvers/                # Resolver feature tests
│   ├── dag/                  # Dependency graph ordering
│   ├── until/                # Fallback chains, until conditions
│   ├── type-coercion/        # Type field coercion
│   ├── transform-chain/      # Multi-step transform pipelines
│   ├── conditional/          # When conditions
│   ├── timeout/              # Per-resolver timeout
│   └── sensitive/            # Sensitive value masking
├── rendering/                # Template rendering tests
├── composition/              # Multi-file compose tests
│   └── parts/                # Composed YAML fragments
└── edge-cases/               # Negative/error tests
    ├── validation-failures/  # Intentional validation errors
    ├── invalid-provider/     # Unknown provider handling
    └── timeout-enforcement/  # Timeout violation behavior
```

## Tags

| Tag | Description |
|-----|-------------|
| `smoke` | Quick verification tests, good for CI gates |
| `provider` | Provider-specific tests |
| `static`, `env`, `cel`, `exec`, `file`, `directory`, `go-template`, `validation`, `sleep` | Individual provider tests |
| `resolver` | Resolver feature tests |
| `dag`, `until`, `type-coercion`, `transform`, `conditional`, `timeout`, `sensitive` | Individual resolver feature tests |
| `rendering` | Template rendering tests |
| `composition` | Multi-file compose tests |
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

## Excluded Providers

These providers are excluded from functional tests due to external dependencies:
- **`http`** — requires a network endpoint
- **`git`** — requires a git repository with specific state
- **`secret`** — requires keyring/credential store
- **`identity`** — requires authentication handlers

They can be added later with mocked endpoints or conditional `skipExpression` guards.
