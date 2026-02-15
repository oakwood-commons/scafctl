# Tested Solution Example

Demonstrates functional testing features in scafctl.

## Features Covered

| Feature | Test Name(s) |
|---------|-------------|
| CEL expression assertions | `render-json`, `resolver-output`, `resolver-with-params` |
| `contains` / `notContains` | `assertion-contains`, `assertion-not-contains` |
| `regex` / `notRegex` | `assertion-regex`, `assertion-not-regex` |
| `target` (stderr/combined) | `assertion-target-stderr` |
| Custom `message` | `assertion-message` |
| Test inheritance (`extends`) | `render-basic`, `render-json`, `resolver-output`, etc. |
| Multiple templates | `_render-base`, `_resolver-base` |
| Tags | `tagged-integration`, `render-basic` (smoke), `lint-clean` |
| Per-test `env` | `env-per-test` |
| Suite-level `testConfig.env` | `testConfig.env.TEST_MODE` |
| `testConfig.skipBuiltins` (list) | Skips `resolve-defaults` and `render-defaults` |
| Init / Cleanup steps | `init-cleanup` |
| `expectFailure` | `expect-failure` |
| Per-test `timeout` | `timeout-test` |
| Static `skip` / `skipReason` | `skip-static` |
| Conditional `skipExpression` | `skip-conditional` |
| `retries` | `retry-test` |

## Running

```bash
# Run all tests
scafctl test functional -f solution.yaml

# Run only smoke-tagged tests
scafctl test functional -f solution.yaml --tag smoke

# Watch mode - re-run tests on file changes
scafctl test functional -f solution.yaml --watch

# Watch with tag filter
scafctl test functional -f solution.yaml --watch --tag smoke

# List tests without running
scafctl test list -f solution.yaml

# Verbose output
scafctl test functional -f solution.yaml -v
```

## Tutorial

See the [Functional Testing Tutorial](../../../docs/tutorials/functional-testing.md) for
a comprehensive guide to all testing features.
