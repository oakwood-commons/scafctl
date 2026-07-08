# Template Fan-Out Example (per-entry data)

Demonstrates rendering a single shared template **once per collection item**,
each with its own variables and its own output path, in a single `render-tree`
resolver. This replaces the older "separate `forEach` render resolver plus a
manual index-zip" pattern.

## What It Does

1. **Lists** the environments to generate config for (`environments` resolver)
2. **Defines** one shared Terraform backend template (`backendTemplate` resolver)
3. **Fans out** with `go-template` `render-tree`: builds one entry per
   environment, each carrying its own `data` (the environment), shallow-merged
   over the shared `data`
4. **Writes** one `backend.tf` per environment under `./output/` using `file`
   `write-tree`, preserving each entry's declared path

## How Per-Entry Data Works

Each entry may include an optional `data` map. It is shallow-merged over the
shared top-level `data` for that entry only. On key conflicts, per-entry values
win over shared `data`, iteration variables, and resolver context. Entries
without a `data` field render against the shared `data` alone, so existing
`render-tree` usage is unaffected.

```yaml
entries:
  expr: |
    _.environments.map(env, {
      "path":    "envs/" + env.name + "/backend.tf",
      "content": _.backendTemplate.entries[0].content,
      "data":    {"environment": env}   # per-entry, wins over shared data
    })
```

## Running

```bash
scafctl run solution -f examples/solutions/template-fanout/solution.yaml
```

## Output

After running, the `./output/` directory will contain one file per environment:

```
output/
└── envs/
    ├── dev/
    │   └── backend.tf
    ├── staging/
    │   └── backend.tf
    └── prod/
        └── backend.tf
```

Each `backend.tf` is rendered from the same template but with its own
environment values (bucket, region, prefix) plus the shared `platformAppName`.

## Key Concepts

| Step | Provider | Operation | Purpose |
|------|----------|-----------|---------|
| 1 | `static` | -- | Collection to fan out over |
| 2 | `static` | -- | Single shared template content |
| 3 | `go-template` | `render-tree` | One entry per item, each with per-entry `data` |
| 4 | `file` | `write-tree` | Write results preserving each entry's path |
