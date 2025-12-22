# scafctl test (Proposal)

Run CLI and engine tests defined in a solution's `testing` section.

## Usage

```
scafctl test <scope> [resource-ref] [flags]
```

- `scope` can be `run`, `engine`, or `all` (planned default: `run`).
- `resource-ref` is optional; when omitted, tests for the current solution are executed.

## Flags

```
      --solution <path>    Path to the solution file or directory (defaults to .)
      --filter <glob>      Filter tests by name
      --output <format>    Output format: text (default), json, yaml
      --quiet              Suppress informational output
      --debug              Emit debug logs
      --no-cache           Force tests to recompute resolver context from scratch
```

## Behavior

- Loads `testing.tests` entries from the target solution.
- Executes CLI tests by invoking `scafctl run` with the specified parameters.
- Executes engine tests against the resolver pipeline without the CLI.
- Aggregates results and returns non-zero exit code on failure.

## Examples

### Run all tests for current solution

```
scafctl test run solution:terraform-multi-env
```

### Run engine-only tests for a solution file

```
scafctl test engine ./examples/git-repo-normalizer/solution.yaml
```

### Filter tests by name pattern

```
scafctl test run solution:terraform-multi-env --filter "deploy-*"
```

### Emit JSON report

```
scafctl test all solution:terraform-multi-env --output json
```

### Force fresh resolver evaluations

```
scafctl test run solution:terraform-multi-env --no-cache
```

## Notes

- Tests reuse the same engine as regular CLI commands, ensuring parity.
- Future work: support parallel execution and richer reporting formats.
