---
name: solution-spec
description: "Solution YAML specification reference for scafctl. Schema, resolver phases, action workflow, ValueRef format, DAG resolution, and testing. Use when working on solution loading, parsing, execution, or spec types."
---

# Solution Spec Reference

> The authoritative field schema is not duplicated here. For the exact, version-accurate
> structure of a solution, resolver, action, or workflow, call the MCP tools
> **`get_solution_schema`** and **`explain_kind`** (kinds: solution, resolver, action,
> workflow, spec, provider, schema, retry). Scaffold a valid starting point with
> **`scaffold_solution`** and validate with **`lint_solution`**. This skill covers the
> runtime semantics and authoring judgment those tools do not encode.

## Top-Level Structure

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-solution          # Required, DNS-safe
  version: 1.0.0             # Semver
  displayName: My Solution
  description: Short description
catalog:
  visibility: public         # public | private | internal
spec:
  resolvers: {}              # map[string]*Resolver
  workflow: {}               # Optional action workflow
  testing: {}                # Optional test suite
```

## Resolver Structure

Resolvers are the DAG nodes. Map keys are resolver names (DNS-safe). Each has up
to three phases, each a `with:` list of provider steps.

```yaml
spec:
  resolvers:
    myResolver:
      description: What this resolver does
      type: string           # string|int|float|bool|array|object|time|duration|any
      sensitive: false       # Redact from logs/output
      when: "_.some_flag"    # CEL condition -- skip if false
      dependsOn: [other]     # Explicit deps (rarely needed -- auto-inferred)
      timeout: 30s

      # Phase 1: Resolve -- get the value (first non-null step wins)
      resolve:
        # 'until' is a peer of 'with' (a CEL condition; __self = candidate).
        until:
          expr: "__self != ''"
        with:
          - provider: parameter
            inputs:
              key: "name"
              default: "world"
          - provider: env       # Fallback chain -- first non-null wins
            inputs:
              name: MY_ENV_VAR

      # Phase 2: Transform -- reshape the value
      transform:
        with:
          - provider: cel
            inputs:
              expression: "__self.upperAscii()"

      # Phase 3: Validate -- check the value
      validate:
        with:
          - provider: validation
            inputs:
              expression: "size(__self) > 0"
              message: "Must not be empty"

      messages:
        error: "Failed to resolve {{.name}}"  # Go template
```

Verify exact field names and types with `get_solution_schema` /
`explain_kind resolver`.

### Phase Execution Order (runtime semantics)

For the full behavioral detail see `explain_concepts name=phase-execution`:

1. **Resolve**: `with:` steps run in order; the **first successful/non-null**
   result wins and stops the chain. A failing step falls through to the next
   (fallback chain). `until:` (which can read `__self`) can stop earlier.
2. **Transform**: steps run **sequentially**, each seeing the prior value via
   `__self`. Transform defaults to fatal-on-error.
3. **Validate**: all inline rules are evaluated and failures aggregated;
   validation is **non-fatal by default** (value still returned, dependents
   still run). Rules referencing only `__self` run inline; rules referencing
   another resolver (`_.other`) are deferred and do NOT add DAG edges.

### forEach (Resolve and Transform Steps)

`forEach` is supported on `resolve.with` and `transform.with` (not `validate`).
On resolve, `forEach.in` is **required** (there is no resolved value to iterate
yet); on transform it defaults to `__self`. `__item`/`__index` are injected;
custom `item`/`index` aliases are added alongside. See `explain_concepts name=foreach`
and `list_context_variables phase=forEach`.

### Dependency Resolution (DAG)

Dependencies are auto-inferred from value references -- CEL `_.name`, `rslvr:`
inputs, and `{{ .name }}` template accessors -- plus any explicit `dependsOn`.
Use `extract_resolver_refs` to see what a given expression/template infers. Only
add `dependsOn` for pure ordering with no value reference. The executor topo-sorts
the DAG into phases and runs each phase's nodes concurrently.

## ValueRef Format

Used anywhere a value can be dynamic (resolver inputs, action inputs, messages):

| Format | YAML | When to Use |
|--------|------|-------------|
| Literal | `key: value` | Static values known at authoring time |
| Resolver | `key: {rslvr: resolver-name}` | Another resolver's raw output |
| CEL | `key: {expr: "_.field.upperAscii()"}` | Data manipulation, conditionals, coercion |
| Go Template | `key: {tmpl: "Hello {{.name}}"}` | Text rendering, multi-line, file content |

**Decision guide**: literal for static; `rslvr` for a raw value; `expr` for data
logic; `tmpl` for text. (CEL for data, templates for text.)

## Action Workflow

Actions execute after all resolvers complete and form their own DAG. Verify the
exact shape with `explain_kind workflow` / `explain_kind action`.

```yaml
spec:
  workflow:
    resultSchemaMode: error   # error | warn | ignore
    actions:
      write-config:
        provider: directory
        inputs:
          source: {rslvr: templates-dir}
          destination: {expr: "_.output_path"}
        when: "_.enabled"         # CEL condition
        continueOnError: false    # bool or CEL; replaces deprecated onError
        timeout: 30s
        exclusive: [other-write]  # Mutual exclusion
        retry:
          maxAttempts: 3
          backoff: exponential
```

### Action-Specific Context

Actions get extra context variables not available to resolvers: `__actions`
(completed action results), `__execution` (resolver execution metadata), and
`__cwd`. In actions, `__error` is a structured map (`__error.message`,
`__error.statusCode`, `__error.type`, `__error.exitCode`, `__error.attempt`,
`__error.maxAttempts`). See `list_context_variables phase=action`.

## Testing

Solutions support functional tests under `spec.testing.cases` (a **map** keyed
by test name -- not a sequence). Scaffold with `generate_test_scaffold`, run with
`run_solution_tests`, and see `explain_concepts name=functional-testing`.

```yaml
spec:
  testing:
    cases:
      basic-test:
        description: basic run
        command: [run, solution]
        assertions:
          - expression: "__exitCode == 0"
```

### Snapshot masking (golden baselines)

See `explain_concepts name=snapshot-masking` for the full model. Built-in presets
(`timestamp`, `uuid`, `sandbox`) are on by default; declare `masks` to normalize
other volatile values; `snapshotSource: files` snapshots the rendered tree.

```yaml
cases:
  golden-render:
    description: renders with volatile ids masked
    command: [run, solution]
    snapshotSource: files          # stdout (default) | files
    masks:
      - name: entra-group          # custom regex mask
        pattern: '\[[^\]]*\]'
        placeholder: "<GROUP>"
        path: "envs/**/*.auto.tfvars"  # path glob requires snapshotSource: files
      - use: email                 # opt-in preset: email | ipv4 | mac
      - use: uuid                   # disable a built-in preset
        disabled: true
```

Any declared mask makes the test report a relaxed status (`PASS*`). Regenerate
golden files with `--update-snapshots`.

## Built-in Providers

For the list of built-in and official providers and their capabilities, call the
MCP tool **`list_providers`** (and `get_provider_schema` for a provider's I/O).
This is authoritative and version-accurate.

## Key Types (pkg/spec/)

- `Solution`: Top-level with APIVersion, Kind, Metadata, Catalog, Spec
- `Resolver`: Name, Type, Phases (Resolve/Transform/Validate), When, DependsOn
- `ValueRef`: Literal | Resolver | Expr | Tmpl
- `Workflow`: Actions map, ResultSchemaMode
- `Action`: Provider, Inputs, DependsOn, When, ContinueOnError, Retry, ForEach

## Key Packages

- `pkg/spec/`: YAML types and parsing
- `pkg/solution/`: Solution loading and execution orchestration
- `pkg/resolver/`: DAG building, phase execution, dependency extraction
- `pkg/action/`: Action workflow execution
- `pkg/provider/`: Provider interface and registry

## See Also

- MCP tools: `get_solution_schema`, `explain_kind`, `scaffold_solution`,
  `lint_solution`, `list_providers`, `run_solution_tests`, `extract_resolver_refs`.
- Concepts (`explain_concepts name=<...>`): `phase-execution`, `snapshot-masking`,
  `context-variables`, `authoring-workflow`.
