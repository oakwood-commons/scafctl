# Test Scaffolding Demo

Demonstrates `scafctl test init` — a command that generates a starter test suite by
analyzing a solution's resolvers, validation rules, and workflow actions.

## Quick Start

```bash
# Generate a test scaffold from the solution
scafctl test init -f solution.yaml

# Save the scaffold to a file
scafctl test init -f solution.yaml > generated-tests.yaml
```

## What Gets Generated

For this solution, `test init` produces:

| Test | Description |
|------|-------------|
| `lint` | Verify solution has no lint errors |
| `render-defaults` | Verify solution renders with default values |
| `resolve-defaults` | Verify all resolvers resolve with default values |
| `resolver-language` | Verify resolver "language" produces expected output |
| `resolver-language-invalid` | Verify language validation rejects bad input |
| `resolver-outputDir` | Verify resolver "outputDir" produces expected output |
| `resolver-projectName` | Verify resolver "projectName" produces expected output |
| `resolver-version` | Verify resolver "version" produces expected output |
| `resolver-version-invalid` | Verify version validation rejects bad input |
| `action-create-dir` | Verify action "create-dir" executes successfully |
| `action-generate` | Verify action "generate" executes successfully |
| `action-release` | Verify conditional action "release" (tagged `conditional`) |

## Workflow

1. **Generate** the scaffold:
   ```bash
   scafctl test init -f solution.yaml
   ```

2. **Review and customize** the output — add specific assertions, tune invalid inputs,
   and remove tests you don't need.

3. **Paste** the `tests:` section into your solution YAML under `spec`, or save it as a
   separate compose file.

4. **Run** the tests:
   ```bash
   scafctl test functional -f solution.yaml
   ```

## Difference from `-o test`

- `test init` performs **structural analysis only** — no commands are executed
- `-o test` (future) would **execute** a command and capture its output to generate assertions
- Use `test init` to bootstrap; use `-o test` to capture known-good behavior

## Tutorial

See the [Functional Testing Tutorial](../../../docs/tutorials/functional-testing.md#test-scaffolding-scafctl-test-init)
for a comprehensive guide.
