# Template Directory Rendering Example

Demonstrates the recommended pattern for rendering a directory tree of Go
templates into final output files, preserving the original directory structure.

## What It Does

1. **Reads** all `.tpl` files from `templates/` recursively using the `directory` provider
2. **Renders** every template with shared variables using `go-template` `render-tree`
3. **Writes** all rendered files under `./output/`, stripping the `.tpl` extension using `file` `write-tree` with `outputPath`

## Directory Layout

```
template-directory/
├── solution.yaml           # The solution definition
├── README.md               # This file
└── templates/
    ├── README.md.tpl       # Project readme template
    ├── config/
    │   └── app.yaml.tpl    # Application config template
    └── k8s/
        ├── deployment.yaml.tpl
        └── service.yaml.tpl
```

## Running

```bash
scafctl run solution -f examples/solutions/template-directory/solution.yaml
```

## Output

After running, the `./output/` directory will contain:

```
output/
├── README.md
├── config/
│   └── app.yaml
└── k8s/
    ├── deployment.yaml
    └── service.yaml
```

Note how the `.tpl` extension is stripped automatically via the `outputPath` template.

## Key Concepts

### Provider Pipeline

| Step | Provider | Operation | Purpose |
|------|----------|-----------|---------|
| 1 | `directory` | `list` | Read template files with content |
| 2 | `go-template` | `render-tree` | Batch-render all templates |
| 3 | `file` | `write-tree` | Write results preserving structure |

### `outputPath` Template

The `outputPath` field on the `file` provider's `write-tree` operation is a Go
template that controls where each file is written (relative to `basePath`).
Available variables:

| Variable | Example for `k8s/deployment.yaml.tpl` |
|----------|---------------------------------------|
| `__filePath` | `k8s/deployment.yaml.tpl` |
| `__fileName` | `deployment.yaml.tpl` |
| `__fileStem` | `deployment.yaml` |
| `__fileExtension` | `.tpl` |
| `__fileDir` | `k8s` |

The template `{{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}`
reconstructs the path without the `.tpl` extension.
