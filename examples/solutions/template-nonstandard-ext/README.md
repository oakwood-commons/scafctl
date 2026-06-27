# Rendering Non-Standard Template Extensions

Demonstrates that scafctl template rendering is **role-based, not
extension-based**. A file reached via a `directory` provider (or
`bundle.include`) is treated as a Go-template source regardless of its
extension -- Terraform `.tf`, Kubernetes `.yaml`, and others render exactly like
`.tpl`/`.tmpl`/`.gotmpl`.

## What It Does

1. **Reads** the `.tf` files from `templates/` using the `directory` provider
   (`filterGlob: "*.tf"`)
2. **Renders** them with resolver values using `go-template` `render-tree`
3. **Writes** the rendered files under `./output/` using `file` `write-tree`

## Directory Layout

```
template-nonstandard-ext/
├── solution.yaml     # The solution definition
├── README.md         # This file
└── templates/
    └── main.tf       # A Terraform file that is also a Go template
```

## Running

```bash
scafctl run solution -f examples/solutions/template-nonstandard-ext/solution.yaml
cat ./output/main.tf
```

## Lint Discovery

Because rendering is role-based, the `missing-template-dependency` lint rule
also scans non-`.tpl` files for resolver references. It reads `templates/main.tf`,
finds the `{{ .appName }}` and `{{ .region }}` references, and verifies that both
resolvers are reachable in the `rendered` resolver's dependency graph. If a
referenced resolver were missing from that graph, lint would warn -- even though
the file is a `.tf`, not a `.tpl`:

```bash
scafctl lint -f examples/solutions/template-nonstandard-ext/solution.yaml
```
