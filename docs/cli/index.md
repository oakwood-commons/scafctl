# scafctl CLI Reference (Proposal)

> This Markdown mirrors what `scafctl --help --format markdown` will output once the CLI exporter lands. Until then it is a hand-authored approximation that follows the current CLI design.

## Summary

scafctl is a composable scaffolding and automation CLI. It resolves data, renders templates, and executes actions in a predictable, inspectable way.

## Usage

```
scafctl [command] [flags]
```

## Available Commands

- [run](./commands/run.md) — Execute a resource (solution, resolver, provider, template, action)
- [get](./commands/get.md) — Inspect definitions and metadata without executing
- [build](./commands/build.md) — Build a solution into a catalog-ready artifact
- [publish](./commands/publish.md) — Publish a built solution to a catalog
- [validate](./commands/validate.md) — Validate schemas, resolvers, templates, and references
- [test](./commands/test.md) — Execute CLI and engine tests defined in solutions
- [config](./commands/config.md) — Manage persistent CLI configuration
- [auth](./commands/auth.md) — Authenticate with pluggable providers
- [catalog](./commands/catalog.md) — Discover and manage catalog artifacts
- [migrate](./commands/migrate.md) — Upgrade legacy scafctl artifacts to the current schema
- [version](./commands/version.md) — Print scafctl version information
- help — Help about any command (`scafctl <command> --help`)

## Global Flags

```
  -h, --help            Show help for scafctl or a command
      --interactive     Prompt for missing values when possible
      --output <format> Output format: text (default), json, yaml
      --quiet           Suppress informational output
      --no-color        Disable ANSI color output
      --dry-run         Resolve and render without executing side effects
      --no-cache        Ignore internal caches and recompute values from scratch
      --force           Allow unsafe or destructive operations
      --debug           Emit debug logs to stderr
      --version         Print version and exit
```

## Expression Flags

```
  -e, --expression <cel>   CEL expression used to project data from the resolver context.
                           Repeatable. Defaults to -e _
```

## Resolver Input Flags

```
  -r <name>=<value>        Provide a value for a resolver. Repeatable.
```

## Examples

### Run a solution

```
scafctl run solution:gcp-basic
```

### Run with resolver inputs

```
scafctl run solution:gcp-basic -r appName=my-app -r env=dev
```

### Inspect resolved values only

```
scafctl run solution:gcp-basic --dry-run -e _
```

### Project specific values

```
scafctl run solution:gcp-basic -e _.envLowest -e _.githubRepo
```

### Run a provider standalone

```
scafctl run provider:cel --expression "'hi'.toAsciiUpper()"
```

### Validate a solution

```
scafctl validate .
```

## Learn More

For help with a specific command:

```
scafctl <command> --help
```
