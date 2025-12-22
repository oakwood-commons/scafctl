# scafctl run (Proposal)

Execute a scafctl resource. This is the primary entry point for running solutions, individual resolvers, providers, templates, or actions.

## Usage

```
scafctl run <resource-ref> [flags]
```

## Resource Reference

```
<kind>:<name>[@version]
```

Supported kinds:

- `solution`
- `resolver`
- `provider`
- `template`
- `action`

Examples:

- `solution:gcp-basic`
- `solution:gcp-basic@1.0.1`
- `resolver:platformGraph`
- `provider:cel`
- `template:managed-basic`
- `action:filesystem.write`

## Common Flags

```
  -r <name>=<value>        Provide input to a resolver (repeatable)
  -e, --expression <cel>   Project data from the resolver context (repeatable, defaults to -e _)
      --dry-run            Resolve and render without executing side effects
      --no-cache           Ignore internal caches and recompute resolver values
      --interactive        Prompt for missing resolver values when possible
      --output <format>    Output format: text (default), json, yaml
      --quiet              Suppress informational output
      --debug              Emit debug logs to stderr
```

## Resource Behavior

- **solution** — Resolves all resolvers, renders templates only if referenced, executes actions only when selected and not in `--dry-run`.
- **resolver** — Resolves a single resolver without running templates or actions.
- **provider** — Executes a provider standalone for inspection or ad-hoc usage.
- **template** — Renders a template without writing files (combine with actions to persist output).
- **action** — Executes a single action (can be previewed with `--dry-run`).

## Examples

### Run a solution

```
scafctl run solution:gcp-basic
```

### Run with resolver inputs

```
scafctl run solution:gcp-basic -r appName=my-app -r env=dev
```

### Inspect results without side effects

```
scafctl run solution:gcp-basic --dry-run
```

### Force a fresh evaluation

```
scafctl run solution:gcp-basic --dry-run --no-cache
```

### Project specific values

```
scafctl run solution:gcp-basic -e _.envLowest -e _.githubRepo
```

### Run a resolver directly

```
scafctl run resolver:platformGraph
```

### Run a provider standalone

```
scafctl run provider:cel --expression "'hi'.toAsciiUpper()"
```

## Notes

- `--dry-run` prevents actions from performing side effects.
- Expressions (`-e`) only change what is returned; they do not alter execution.
- Resolver inputs (`-r`) are the supported way to inject user data.
