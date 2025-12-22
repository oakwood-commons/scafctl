# scafctl get (Proposal)

Inspect solution metadata, resolvers, templates, providers, or actions without executing them.

## Usage

```
scafctl get <resource-ref> [flags]
```

## Resource Reference

```
<kind>:<name>[@version]
```

Supported kinds:

- `solution`
- `resolver`
- `template`
- `provider`
- `action`
- `catalog` (planned)

## Flags

```
      --output <format>    Output format: text (default), json, yaml
      --quiet              Suppress informational output
      --debug              Emit debug logs
```

## Examples

### Inspect solution metadata

```
scafctl get solution:terraform-multi-env
```

### View resolver definition

```
scafctl get resolver:repoConfig --output yaml
```

### List solution actions

```
scafctl get solution:terraform-multi-env --output json | jq '.spec.actions'
```

## Notes

- `get` never executes providers, resolvers, or actions.
- Useful for IDE tooling, documentation generation, and quick inspection.
