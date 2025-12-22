# scafctl version (Proposal)

Print scafctl version information.

## Usage

```
scafctl version [flags]
```

## Flags

```
      --output <format>    Output format: text (default), json, yaml (planned)
      --quiet              Suppress informational output
      --debug              Emit debug logs
```

## Output

- In text mode, prints the semantic version plus build metadata (e.g., `scafctl version 1.0.0 (commit abc123, built 2025-12-01)`).
- Structured formats include fields for version, commit, build date, and platform.

## Examples

```
scafctl version
```

```
scafctl version --output json
```

## Notes

- Useful for debugging support requests and ensuring CLI matches catalog expectations.
