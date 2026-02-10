---
title: "Go Templates Tutorial"
weight: 60
---

# Go Templates Tutorial

This tutorial covers using Go templates in scafctl for generating structured text output like configuration files, documentation, and code scaffolding.

## Overview

scafctl supports Go templates in two ways:

1. **`go-template` provider** — Render templates in resolvers and actions
2. **`tmpl` field** — Use Go template syntax in provider inputs (alternative to `expr`)

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│  Resolver    │ ──► │  Go Template │ ──► │  Rendered    │
│  Data (_)    │     │  Provider    │     │  Output      │
└──────────────┘     └──────────────┘     └──────────────┘
```

**When to use Go templates vs CEL:**
- **Go templates** — Multi-line text generation, file rendering, structured output
- **CEL expressions** — Data transformation, conditionals, single-value computation

## Quick Start

### 1. Simple Template

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: template-basics
  version: 1.0.0

spec:
  resolvers:
    name:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "World"

    greeting:
      type: string
      dependsOn: [name]
      resolve:
        with:
          - provider: go-template
            inputs:
              name: "greeting"
              template: "Hello, {{ .name }}!"
```

```bash
scafctl run solution -f template-basics.yaml -o json
# Output: {"greeting": "Hello, World!", "name": "World"}
```

The `go-template` provider receives all resolver values as template data. Access them with dot notation: `{{ .resolverName }}`.

### 2. Template from File

For larger templates, read them from the filesystem:

```yaml
spec:
  resolvers:
    template_content:
      type: any
      resolve:
        with:
          - provider: file
            inputs:
              operation: "read"
              path: "templates/config.yaml.tmpl"

    rendered:
      type: string
      dependsOn: [template_content, app_name, environment]
      resolve:
        with:
          - provider: go-template
            inputs:
              name: "config"
              template:
                expr: "_.template_content.content"
              missingKey: "error"
```

See [template-render.yaml](../../examples/actions/template-render.yaml) for a complete working example.

## Template Syntax

### Variables

All resolver values are available at the root level:

```yaml
template: |
  App: {{ .app_name }}
  Version: {{ .version }}
  Port: {{ .server.port }}
```

### Conditionals

```yaml
template: |
  {{ if eq .environment "production" }}
  replicas: 5
  logging: warn
  {{ else }}
  replicas: 1
  logging: debug
  {{ end }}
```

### Loops

```yaml
template: |
  {{ range .services }}
  - name: {{ .name }}
    port: {{ .port }}
  {{ end }}
```

With index:
```yaml
template: |
  {{ range $i, $svc := .services }}
  {{ $i }}. {{ $svc.name }}
  {{ end }}
```

### Maps

```yaml
template: |
  {{ range $key, $value := .features }}
  {{ $key }}: {{ $value }}
  {{ end }}
```

### Whitespace Control

Use `-` to trim whitespace:

```yaml
template: |
  services:
  {{- range .services }}
    - {{ .name }}
  {{- end }}
```

## Provider Reference

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `template` | string | ✅ | The Go template string (max 65536 chars) |
| `name` | string | ✅ | Template name (max 255 chars) |
| `missingKey` | string | ❌ | How to handle missing keys: `default`, `zero`, `error` |
| `leftDelim` | string | ❌ | Custom left delimiter (default: `{{`) |
| `rightDelim` | string | ❌ | Custom right delimiter (default: `}}`) |
| `data` | any | ❌ | Additional data merged into template context |

### Missing Key Behavior

| Mode | Behavior |
|------|----------|
| `default` | Missing keys render as `<no value>` |
| `zero` | Missing keys render as the zero value for the type |
| `error` | Missing keys cause an error (recommended for production) |

```yaml
# Fail if any referenced value doesn't exist
- provider: go-template
  inputs:
    name: "strict"
    template: "Hello, {{ .name }}"
    missingKey: "error"
```

### Custom Delimiters

Useful when generating files that contain `{{` (e.g., Helm charts, Jinja templates):

```yaml
- provider: go-template
  inputs:
    name: "helm-values"
    leftDelim: "<%"
    rightDelim: "%>"
    template: |
      # Helm values ({{ and }} are literal here)
      image: <% .image %>
      tag: <% .tag %>
      annotations:
        checksum: "{{ include \"mychart.checksum\" . }}"
```

### Additional Data

Merge extra data into the template context without creating resolvers:

```yaml
- provider: go-template
  inputs:
    name: "with-data"
    template: "{{ .app }} v{{ .build_number }}"
    data:
      build_number: "42"
```

The `data` values are merged with resolver values, with `data` taking precedence on conflicts.

## Using `tmpl` in Provider Inputs

As an alternative to `expr` (CEL), you can use `tmpl` for dynamic provider inputs:

```yaml
workflow:
  actions:
    deploy:
      provider: exec
      inputs:
        command:
          tmpl: "kubectl apply -f {{ .output_dir }}/{{ .app_name }}.yaml"
```

**Comparison:**

| Field | Syntax | Best For |
|-------|--------|----------|
| `expr` | `"'kubectl apply -f ' + _.output_dir + '/' + _.app_name + '.yaml'"` | Computed values, conditionals |
| `tmpl` | `"kubectl apply -f {{ .output_dir }}/{{ .app_name }}.yaml"` | String interpolation, readability |

Note: In `tmpl`, resolver values are accessed as `{{ .resolverName }}` (no `_` prefix needed).

## ForEach with Templates

Templates have access to iteration context variables:

```yaml
workflow:
  actions:
    notify:
      provider: exec
      forEach:
        in:
          expr: "_.services"
        item: service
      inputs:
        command:
          tmpl: "echo 'Deploying {{ .service.name }} to {{ .environment }}'"
```

## Common Patterns

### Generate a Config File

```yaml
rendered_config:
  type: string
  dependsOn: [app, db, features]
  resolve:
    with:
      - provider: go-template
        inputs:
          name: "app-config"
          missingKey: "error"
          template: |
            # Auto-generated by scafctl
            app:
              name: {{ .app.name }}
              port: {{ .app.port }}

            database:
              host: {{ .db.host }}
              port: {{ .db.port }}
              name: {{ .db.name }}

            features:
            {{- range $feature, $enabled := .features }}
              {{ $feature }}: {{ $enabled }}
            {{- end }}
```

### Generate a Dockerfile

```yaml
dockerfile:
  type: string
  dependsOn: [language, version, port]
  resolve:
    with:
      - provider: go-template
        inputs:
          name: "dockerfile"
          template: |
            FROM {{ .language }}:{{ .version }}
            WORKDIR /app
            COPY . .
            {{ if eq .language "go" }}
            RUN go build -o /app/server .
            {{ else if eq .language "node" }}
            RUN npm install && npm run build
            {{ end }}
            EXPOSE {{ .port }}
            CMD ["/app/server"]
```

### Multi-File Generation

Combine with `forEach` to generate multiple files:

```yaml
resolvers:
  services:
    type: array
    resolve:
      with:
        - provider: static
          inputs:
            value:
              - name: api
                port: 8080
              - name: worker
                port: 9090

workflow:
  actions:
    generate-configs:
      provider: file
      forEach:
        in:
          expr: "_.services"
        item: svc
      inputs:
        operation: "write"
        path:
          tmpl: "/tmp/configs/{{ .svc.name }}.yaml"
        content:
          tmpl: |
            name: {{ .svc.name }}
            port: {{ .svc.port }}
        createDirs: true
```

## Examples

| Example | Description | Run |
|---------|-------------|-----|
| [template-render.yaml](../../examples/actions/template-render.yaml) | File-based template rendering | `scafctl run solution -f examples/actions/template-render.yaml` |
| [go-template-inline.yaml](../../examples/actions/go-template-inline.yaml) | Inline template with loops and conditionals | `scafctl run solution -f examples/actions/go-template-inline.yaml` |

## Next Steps

- [Actions Tutorial](actions-tutorial.md) — Use templates in action workflows
- [Resolver Tutorial](resolver-tutorial.md) — Learn about resolver dependencies and transforms
- [Provider Reference](provider-reference.md) — Full `go-template` provider documentation
