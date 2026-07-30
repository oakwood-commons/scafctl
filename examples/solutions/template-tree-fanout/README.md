# Template Tree Fan-Out Example (forEach + pathTemplate)

Demonstrates rendering an **entire directory tree of templates once per
collection item**, routing each item's copy to its own output directory -- in a
single `render-tree` resolver. This is the native fan-out primitive:
`render-tree` iterates `forEach.in` and `pathTemplate` computes a distinct
output path per (item, template).

Use this when you need the **cross-product** of a template tree and a
collection (e.g. the same Terraform module tree materialized per environment).
For fanning out a *single* template with per-entry variables, see the sibling
[`template-fanout`](../template-fanout/) example.

## What It Does

1. **Lists** the environments to generate a tree for (`environments` resolver)
2. **Reads** the shared template tree from `templates/` (`templateFiles`
   resolver, `directory` `list`)
3. **Fans out** with `go-template` `render-tree`: `forEach` renders every
   template once per environment, and `pathTemplate` routes each rendered file
   to `envs/<name>/...`
4. **Writes** the fanned-out tree under `./output/` using `file` `write-tree`,
   preserving each entry's already-environment-scoped path

## How forEach + pathTemplate Work

`forEach` renders every entry once per element of `in`. Each iteration exposes
the current element under the `item` alias (and `__item`) and its 0-based index
under the optional `index` alias (and `__index`). `pathTemplate` is **required**
whenever `forEach` is set: it computes each entry's output path so items never
collide (a duplicate path is a hard error).

```yaml
operation: render-tree
entries:
  expr: '_.templateFiles.entries'
data:
  platformAppName: myapp        # shared across every rendered file
forEach:
  item: env                     # current element -> {{ .env }} / __item
  in:
    rslvr: environments         # the collection to fan out over
pathTemplate: >-
  envs/{{ .env.name }}/{{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}
```

Inside `pathTemplate` you get the `forEach` aliases, `__item`/`__index`, the
shared `data`, resolver context, and the reserved per-entry path parts
`__filePath`, `__fileName`, `__fileStem`, `__fileExtension`, `__fileDir` -- the
same variables as the `file` provider's `write-tree` `outputPath`. Here
`__fileStem` drops the `.tpl` suffix and `__fileDir` preserves the template's
subdirectory.

`forEach.in` accepts any `ValueRef` (`{rslvr: name}`, `{expr: CEL}`,
`{literal: [...]}`) or a literal array; it must resolve to an array.

## Running

```bash
scafctl run solution -f examples/solutions/template-tree-fanout/solution.yaml
```

## Output

After running, `./output/` contains the full template tree once per
environment:

```
output/
└── envs/
    ├── dev/
    │   ├── backend.tf
    │   └── modules/
    │       └── app.tf
    ├── staging/
    │   ├── backend.tf
    │   └── modules/
    │       └── app.tf
    └── prod/
        ├── backend.tf
        └── modules/
            └── app.tf
```

Every file is rendered from the same templates but with its environment's
values (`bucket`, `region`, `name`) plus the shared `platformAppName`.

## Key Concepts

| Step | Provider | Operation | Purpose |
|------|----------|-----------|---------|
| 1 | `static` | -- | Collection to fan out over |
| 2 | `directory` | `list` | Read the shared template tree |
| 3 | `go-template` | `render-tree` | Render the tree once per item via `forEach` + `pathTemplate` |
| 4 | `file` | `write-tree` | Write results preserving each entry's path |
