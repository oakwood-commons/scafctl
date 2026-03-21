---
title: "Template Directory Rendering"
weight: 97
---

# Template Directory Rendering Tutorial

This tutorial walks you through the recommended pattern for rendering a directory
tree of Go templates into output files while preserving the directory structure.
You'll learn how to combine three providers — `directory`, `go-template`, and
`file` — in a single solution to scaffold entire projects from templates.

## Prerequisites

- scafctl installed and available in your PATH
- Basic familiarity with solution files and Go template syntax

## Table of Contents

1. [Overview](#overview)
2. [Setting Up Templates](#setting-up-templates)
3. [Reading Templates with the Directory Provider](#reading-templates-with-the-directory-provider)
4. [Batch Rendering with render-tree](#batch-rendering-with-render-tree)
5. [Writing Files with write-tree](#writing-files-with-write-tree)
6. [Path Transformation with outputPath](#path-transformation-with-outputpath)
7. [Putting It All Together](#putting-it-all-together)
8. [Advanced Patterns](#advanced-patterns)

---

## Overview

A common scaffolding task is:

1. You have a directory of **template files** (`.tpl`, `.tmpl`, `.gotmpl`, etc.)
2. You want to **render** each template with shared variables
3. You want to **write** the rendered output preserving the directory structure

scafctl solves this with three operations:

| Step | Provider | Operation | Purpose |
|------|----------|-----------|---------|
| 1 | `directory` | `list` | Read template files with their content |
| 2 | `go-template` | `render-tree` | Batch-render all templates at once |
| 3 | `file` | `write-tree` | Write rendered files preserving structure |

---

## Setting Up Templates

Create a `templates/` directory with your Go template files:

```
templates/
├── README.md.tpl
├── config/
│   └── app.yaml.tpl
└── k8s/
    ├── deployment.yaml.tpl
    └── service.yaml.tpl
```

Each `.tpl` file is a standard Go template. For example, `k8s/deployment.yaml.tpl`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .appName }}
  namespace: {{ .namespace }}
spec:
  replicas: {{ .replicas }}
  selector:
    matchLabels:
      app: {{ .appName }}
  template:
    spec:
      containers:
        - name: {{ .appName }}
          image: {{ .registry }}/{{ .appName }}:{{ .appVersion }}
          ports:
            - containerPort: {{ .containerPort }}
```

---

## Reading Templates with the Directory Provider

Use the `directory` provider with `includeContent: true` to read all templates recursively:

```yaml
resolvers:
  templateFiles:
    type: any
    resolve:
      with:
        - provider: directory
          inputs:
            operation: list
            path: ./templates
            recursive: true
            filterGlob: "*.tpl"
            includeContent: true
```

This returns an object with an `entries` array. Each entry has:
- `path` — the relative path (e.g. `k8s/deployment.yaml.tpl`)
- `content` — the file's raw content (the unrendered template text)
- Plus metadata like `name`, `size`, `extension`, etc.

---

## Batch Rendering with render-tree

The `go-template` provider's `render-tree` operation takes an array of
`{path, content}` entries and renders each `content` as a Go template:

```yaml
resolvers:
  rendered:
    type: any
    resolve:
      with:
        - provider: go-template
          inputs:
            operation: render-tree
            entries:
              expr: '_.templateFiles.entries'
            data:
              appName: myapp
              namespace: production
              replicas: 3
              registry: ghcr.io/myorg
              appVersion: "1.0.0"
              containerPort: 8080
```

The output is an array of `{path, content}` where each `content` is now the
**rendered** result. The `path` is passed through unchanged from the input.

### Using a Separate Vars Resolver

For cleaner solutions, define variables in their own resolver and reference them:

```yaml
resolvers:
  vars:
    type: any
    resolve:
      with:
        - provider: static
          inputs:
            value:
              appName: myapp
              namespace: production
              # ... more vars

  rendered:
    type: any
    resolve:
      with:
        - provider: go-template
          inputs:
            operation: render-tree
            entries:
              expr: '_.templateFiles.entries'
            data:
              rslvr: vars
```

---

## Writing Files with write-tree

The `file` provider's `write-tree` operation takes an array of `{path, content}`
entries and writes them under a `basePath`:

```yaml
workflow:
  actions:
    write-output:
      provider: file
      inputs:
        operation: write-tree
        basePath: ./output
        entries:
          rslvr: rendered
```

This writes each entry as a file relative to `basePath`, automatically creating
subdirectories as needed. Without `outputPath`, files keep their original paths:

```
./output/k8s/deployment.yaml.tpl    # Still has .tpl extension!
./output/k8s/service.yaml.tpl
./output/config/app.yaml.tpl
./output/README.md.tpl
```

To strip the `.tpl` extension, use `outputPath`.

---

## Path Transformation with outputPath

The `outputPath` field is a Go template that transforms each entry's path before
writing. It receives these variables:

| Variable | Description | Example for `k8s/deployment.yaml.tpl` |
|----------|-------------|---------------------------------------|
| `__filePath` | Original relative path | `k8s/deployment.yaml.tpl` |
| `__fileName` | File name only | `deployment.yaml.tpl` |
| `__fileStem` | File name without last extension | `deployment.yaml` |
| `__fileExtension` | Last extension including dot | `.tpl` |
| `__fileDir` | Directory part (empty for root files) | `k8s` |

### Stripping the `.tpl` Extension

The most common pattern — reconstruct the path using the stem (which drops `.tpl`):

```yaml
outputPath: >-
  {{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}
```

Result: `k8s/deployment.yaml.tpl` → `k8s/deployment.yaml`

### Flattening All Files into One Directory

Ignore the directory structure, keeping only file names:

```yaml
outputPath: "{{ .__fileName }}"
```

Result: `k8s/deployment.yaml.tpl` → `deployment.yaml.tpl`

### Using Sprig Functions

All [Sprig](http://masterminds.github.io/sprig/) template functions are available:

```yaml
outputPath: "{{ lower .__filePath }}"
```

Result: `SRC/MyFile.TXT` → `src/myfile.txt`

### Replacing Extensions

Change `.tpl` to `.generated.yaml`:

```yaml
outputPath: >-
  {{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ trimSuffix ".tpl" .__fileName }}.generated.yaml
```

---

## Putting It All Together

Here is a complete solution that reads templates, renders them, and writes the
output:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: template-directory-rendering
  version: 1.0.0
  description: Render a directory of Go templates into output files

spec:
  resolvers:
    # Variables shared across all templates
    vars:
      type: any
      resolve:
        with:
          - provider: static
            inputs:
              value:
                appName: myapp
                appVersion: "1.2.0"
                namespace: production
                environment: prod
                replicas: 3
                registry: ghcr.io/myorg
                containerPort: 8080
                servicePort: 80
                serviceType: ClusterIP
                logLevel: info

    # Read all template files
    templateFiles:
      type: any
      resolve:
        with:
          - provider: directory
            inputs:
              operation: list
              path: ./templates
              recursive: true
              filterGlob: "*.tpl"
              includeContent: true

    # Render all templates with vars
    rendered:
      type: any
      resolve:
        with:
          - provider: go-template
            inputs:
              operation: render-tree
              entries:
                expr: '_.templateFiles.entries'
              data:
                rslvr: vars

  workflow:
    actions:
      # Write rendered results, stripping .tpl extension
      write-output:
        provider: file
        inputs:
          operation: write-tree
          basePath: ./output
          entries:
            rslvr: rendered
          outputPath: >-
            {{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}
```

Run it:

{{< tabs "template-directory-rendering-cmd-1" >}}
{{% tab "Bash" %}}
```bash
scafctl run solution -f solution.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run solution -f solution.yaml
```
{{% /tab %}}
{{< /tabs >}}

Inspect the output:

```bash
find ./output -type f
# output/README.md
# output/config/app.yaml
# output/k8s/deployment.yaml
# output/k8s/service.yaml

cat ./output/k8s/deployment.yaml
# apiVersion: apps/v1
# kind: Deployment
# metadata:
#   name: myapp
#   namespace: production
# ...
```

---

## Advanced Patterns

### Combining with CEL Filtering

Use a `cel` transform to filter entries before rendering — for instance, skip
files larger than 10KB:

```yaml
resolvers:
  small-templates:
    type: any
    resolve:
      with:
        - provider: directory
          inputs:
            operation: list
            path: ./templates
            recursive: true
            filterGlob: "*.tpl"
            includeContent: true
      transforms:
        - provider: cel
          inputs:
            expression: >-
              {"entries": data.entries.filter(e, e.size < 10000)}
```

### Multiple Template Sets

Render different template directories with different variables by defining
separate resolver chains:

```yaml
resolvers:
  frontendTemplates:
    type: any
    resolve:
      with:
        - provider: directory
          inputs:
            operation: list
            path: ./templates/frontend
            recursive: true
            filterGlob: "*.tpl"
            includeContent: true

  backendTemplates:
    type: any
    resolve:
      with:
        - provider: directory
          inputs:
            operation: list
            path: ./templates/backend
            recursive: true
            filterGlob: "*.tpl"
            includeContent: true

  renderedFrontend:
    type: any
    resolve:
      with:
        - provider: go-template
          inputs:
            operation: render-tree
            entries:
              expr: '_.frontendTemplates.entries'
            data:
              framework: react
              # ...

  renderedBackend:
    type: any
    resolve:
      with:
        - provider: go-template
          inputs:
            operation: render-tree
            entries:
              expr: '_.backendTemplates.entries'
            data:
              framework: gin
              # ...
```

Then write each set to a different output directory:

```yaml
workflow:
  actions:
    write-frontend:
      provider: file
      inputs:
        operation: write-tree
        basePath: ./output/frontend
        entries:
          rslvr: renderedFrontend
        outputPath: >-
          {{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}

    write-backend:
      provider: file
      inputs:
        operation: write-tree
        basePath: ./output/backend
        entries:
          rslvr: renderedBackend
        outputPath: >-
          {{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}
```

### Dry Run

Use `--dry-run` to preview what would be written without touching the filesystem:

{{< tabs "template-directory-rendering-cmd-2" >}}
{{% tab "Bash" %}}
```bash
scafctl run solution -f solution.yaml --dry-run
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run solution -f solution.yaml --dry-run
```
{{% /tab %}}
{{< /tabs >}}

The `write-tree` action will report all paths that *would* be written, including
paths transformed by `outputPath`, without creating any files.
