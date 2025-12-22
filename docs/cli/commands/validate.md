# scafctl validate (Proposal)

Validate solution files, resolver definitions, templates, and references without executing them.

## Usage

```
scafctl validate [path] [flags]
```

- `path` defaults to the current directory. You can pass a solution file, folder, or glob.

## Flags

```
      --schema-only        Validate against the JSON schema without provider checks
      --no-cache           Skip cached metadata when validating
      --quiet              Suppress informational output
      --debug              Emit debug logs
      --output <format>    Output format: text (default), json, yaml
```

## Behavior

- Confirms that `solution.yaml` matches the scafctl schema.
- Validates resolver pipelines, provider references, and transform structure.
- Checks template references and action dependencies.
- Reports warnings and errors without executing actions.
- Detects legacy schema versions and recommends running `scafctl migrate` when upgrades are required.

## Examples

### Validate current solution

```
scafctl validate .
```

### Validate a specific file

```
scafctl validate ./examples/terraform/solution.yaml
```

### Generate JSON report

```
scafctl validate . --output json > validate-report.json
```

### Force full validation without caches

```
scafctl validate . --no-cache
```

### Respond to legacy schema errors

```
scafctl validate ./legacy/solution.yaml

# Output
error: legacy schema detected
hint: run scafctl migrate solution ./legacy/solution.yaml
```

Follow the hint, run `scafctl migrate`, then re-run validation or build to confirm the upgrade.

## Notes

- Validation is read-only and safe to run in CI pipelines.
- Combine with `scafctl test` for comprehensive coverage.
