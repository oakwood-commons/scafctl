# Lint Schema Validation Tests

This directory contains functional tests and fixture YAML files used to test
the JSON Schema validation checks in `scafctl lint`.

## Running

```bash
# Run all lint-schema tests (recursive)
scafctl test functional --tests-path tests/integration/solutions/lint-schema/ --skip-builtins

# Run a specific scenario
scafctl test functional --tests-path tests/integration/solutions/lint-schema/unknown-field/ --skip-builtins
```

## Structure

- `solution.yaml` — Positive test: valid solution passes lint with no schema violations.
- `valid-minimal.yaml` — Fixture used by Go integration tests in `cli_test.go`.
- `unknown-field.yaml` — Fixture used by Go integration tests in `cli_test.go`.
- `unknown-field/solution.yaml` — Negative test: unknown top-level field detected.
- `unknown-nested-field/solution.yaml` — Negative test: unknown nested field in spec detected.
