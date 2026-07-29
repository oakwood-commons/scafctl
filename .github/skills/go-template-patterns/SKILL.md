---
name: go-template-patterns
description: "Go template patterns, built-in functions, custom scafctl functions, and conventions for scafctl. Use when working on Go template evaluation, the gotmpl package, or template-based providers."
---

# Go Template Patterns in scafctl

> Function reference is not duplicated here. For the authoritative, version-accurate
> list of Go-template functions (Sprig plus custom scafctl functions like `toHcl`,
> `toYaml`, `fromYaml`, `cel`, `slugify`), call the MCP tool
> **`list_go_template_functions`**. Test a template with **`evaluate_go_template`**.
> This skill covers the knowledge those tools do not: the execution API,
> dependency inference, file-generation variables, and conventions.

## Execution API

```go
result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
    Content:    "Hello {{.name}}",
    Name:       "greeting",
    Data:       map[string]any{"name": "world"},
    LeftDelim:  "{{",           // Default
    RightDelim: "}}",           // Default
    MissingKey: gotmpl.MissingKeyDefault,
    Funcs:      template.FuncMap{},  // Additional funcs
})
```

### MissingKey Options

| Option | Behavior |
|--------|----------|
| `MissingKeyDefault` | Prints `<no value>` for missing keys |
| `MissingKeyZero` | Returns zero value for the type |
| `MissingKeyError` | Stops execution with error |

## Template Data Context

In solution templates, the root data (`.`) contains all resolved values:

```gotemplate
{{.resolver_name}}              <!-- Direct resolver access -->
{{.config.database.host}}       <!-- Nested field access -->
```

Many context variables are injected into Go templates too (`{{ ._.region }}`,
`{{ .__self }}`, `{{ .__item }}`, etc.). For the authoritative per-phase matrix
and which variables reach templates, call **`list_context_variables`** or read
`explain_concepts name=context-variables`.

### Built-in file-generation variables

When rendering a directory tree (directory -> render-tree -> write-tree), the
file provider injects per-file path parts for the entry being written. They are
the entry's **relative** path (e.g. `k8s/deployment.yaml.tpl`), not an absolute
path, and are injected **without a leading dot** -- the dot is Go-template
accessor syntax, so you write `{{ .__fileStem }}` (see
`pkg/provider/builtin/fileprovider/file.go`):

| Injected name | Accessed as | Contains |
|---------------|-------------|----------|
| `__filePath` | `{{ .__filePath }}` | Relative path of the entry (forward slashes) |
| `__fileName` | `{{ .__fileName }}` | Base file name, including extension |
| `__fileStem` | `{{ .__fileStem }}` | File name with only the **last** extension removed (`deployment.yaml.tpl` -> `deployment.yaml`) |
| `__fileExtension` | `{{ .__fileExtension }}` | The **last** extension, including the leading dot (`.tpl`) |
| `__fileDir` | `{{ .__fileDir }}` | Directory portion of the relative path (empty for a top-level file) |

A common `outputPath` that strips a `.tpl` suffix while preserving the tree:

```gotemplate
{{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}
```

### Verbatim (raw) entries in render-tree

To copy whole files through a `render-tree` pass unchanged (fixtures that are
themselves template syntax, GitHub Actions `${{ ... }}`, etc.), mark them raw
instead of parsing them -- no need for a second copy pipeline or whole-file
ignore markers. Two mechanisms (combinable), verified against
`get_provider_schema name=go-template`:

- **`rawGlobs`** -- provider-level list of doublestar patterns matched against
  each entry's full relative `path` (`*.raw` = top-level, `**/*.raw` = nested).
  Best for entries produced dynamically by the `directory` provider. Mirrors
  Cookiecutter's `_copy_without_render`.
- **`raw`** -- per-entry bool on a hand-constructed entry; overrides `rawGlobs`
  (`true` forces verbatim, `false` forces rendering).

Raw content is copied byte-for-byte (no parse, delimiters, `ignoredBlocks`, or
per-entry `data`). The `missing-template-dependency` lint rule skips
`rawGlobs`-matched files. See the `template-mixed-tree` example.

## Dependency Inference (resolver ref vs data context)

For the full model, see `explain_concepts name=template-dependency-inference`;
to see the references a template actually produces, use **`extract_resolver_refs`**.

A go-template render step's template root namespace is the **union** of three
sources:

1. **Resolver values** -- every resolved value (`{{ .resolverName }}`).
2. **`data` input keys** -- top-level keys of the step's `data` input.
3. **`forEach` aliases** -- the `item` and `index` names bound by a `forEach`
   clause.

scafctl auto-infers a step's resolver dependencies by scanning the template for
root accessors. It disambiguates them as follows:

| Accessor | Treated as a resolver dependency? |
|----------|-----------------------------------|
| `{{ ._.name }}` | Always -- explicit resolver reference. |
| `{{ .field }}`, **no** `data` input | Yes, unless `field` is a `forEach` alias. |
| `{{ .field }}` where `field` is a statically-known `data` key | No -- it is local data context. |
| `{{ .field }}` with a **dynamic** `data` input (keys not statically known) | No -- assumed to come from `data`. |
| `{{ .__self }}`, `{{ .__item }}`, and other `.__` vars | No -- internal/special variables. |

A `data` input's keys are statically known when it is a literal map or a CEL
**map literal** expression (e.g. `expr: '{"appName": _.appName}'`). A `data`
input that is a resolver reference (`rslvr`), a template (`tmpl`), or a
non-map-literal CEL expression (e.g. `map.merge(...)`) is *dynamic* -- its key
set cannot be determined ahead of time, so bare `{{ .field }}` accessors are
assumed to be satisfied by `data` and are not inferred as resolver deps.

**Consequence:** when a `data` input is present, a resolver referenced *only*
via a bare `{{ .field }}` accessor is **not** auto-inferred as a dependency.
Reference it inside the `data` expression with `_.name`, use `{{ ._.name }}` in
the template, or add an explicit `dependsOn` entry.

```yaml
# The forEach alias `proj` and the data keys (projects, appName) are NOT
# resolver dependencies. `appName` becomes a dependency only because the data
# expression references it via `_.`.
resolve:
  with:
    - provider: go-template
      forEach:
        item: proj
        in:
          expr: "_.gcpProjects"
      inputs:
        operation: render
        template: |
          bucket = "{{ .projects.tfstate_bucket_name }}"
          app    = "{{ .appName }}"
        data:
          expr: '{"projects": proj, "appName": _.appName}'
```

The `template-unknown-accessor` lint rule warns when a bare `{{ .field }}` or
`{{ ._.name }}` accessor cannot resolve to any known resolver, data key, or
forEach alias -- a likely typo, since such accessors render empty at runtime
rather than failing. Use `explain_lint_rule template-unknown-accessor` for details.

## CEL vs Go Template Decision Guide

| Use Case | CEL (`expr`) | Go Template (`tmpl`) |
|----------|-------------|---------------------|
| Data manipulation | Preferred | Avoid |
| Conditionals | Preferred | Acceptable |
| Text rendering | Avoid | Preferred |
| Multi-line output | Avoid | Preferred |
| File content | Avoid | Preferred |
| Type coercion | Preferred | Limited |
| List/map operations | Preferred | Basic only |
| String concatenation | Either | Either |

**Rule of thumb**: CEL for **data**, Go templates for **text**.

## Template File Conventions

- Use `.tpl` extension for template files
- Templates are for **text rendering only** -- no data logic
- Access data via resolver names: `{{.my_resolver}}`
- Use `{{- ... -}}` for whitespace control

## Anti-Patterns

| Anti-Pattern | Why | Instead |
|-------------|-----|---------|
| Complex logic in templates | Hard to test, debug | Use CEL transform phase, then reference result |
| Data manipulation in templates | Templates are for rendering | Use CEL expressions |
| Deeply nested `{{if}}` blocks | Unreadable | Split into multiple resolvers with `when` |
| Magic values in templates | Maintenance burden | Use resolver references |

## Key Packages

- `pkg/gotmpl/`: Template execution, options, missing key modes
- `pkg/provider/builtin/gotmplprovider/`: Go template provider for transform phase
- `pkg/authorfuncs/`: Author-defined template helpers (`spec.functions`)

## Author-Defined Functions (`spec.functions`)

Solutions can declare reusable named template helpers under `spec.functions`,
callable from the Go templates the solution renders through the `go-template`
provider (in resolvers and actions) as `{{ name arg... }}`.

- Ordered, positional, typed `params`; exposed inside the body under the `args`
  namespace (`_.args.name` in CEL bodies, `{{ .args.name }}` in template bodies).
- Exactly one body per function: `cel:` XOR `template:`.
- Template bodies can call sibling author functions and all built-ins; CEL
  bodies cannot call author functions. Acyclicity is enforced at compile time.
- Run `explain function` (MCP `explain_kind kind=function`) for the field
  reference. See `examples/resolvers/author-functions.yaml`.

## See Also

- MCP tools: `list_go_template_functions`, `evaluate_go_template`,
  `extract_resolver_refs`, `list_context_variables`.
- Concepts (`explain_concepts name=<...>`): `template-dependency-inference`,
  `context-variables`, `go-template-provider`.
- The `cel-patterns` skill for the CEL side.
