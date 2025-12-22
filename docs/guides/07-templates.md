# Templates Guide

## Overview

**Templates** are pure renderers that transform resolved data into artifacts. They have no side effects and no external dependencies—they only render text and files.

Templates are **optional**. You can build workflows with just resolvers and actions. But templates are powerful for generating configuration, documentation, and code.

## Core Principle

> **Templates render only. They never fetch data, execute logic, or mutate state.**

All template processing happens **in resolvers**, not in templates themselves. Templates are the final output stage.

## How Templates Work

Every template follows the same lifecycle:

```
1. Load         - Read template content from source
2. Normalize    - Convert to internal model
3. (Optional)   - Repeat via foreach
4. Render       - Apply resolver context ({{ _.variable }})
5. Emit         - Make output available
```

## Template Sources

Template content comes from exactly **one source**. All sources work identically after loading:

### 1. Inline Source

Embed template content directly in solution.yaml:

```yaml
templates:
  - name: dockerfile
    source:
      type: inline
      files:
        - path: Dockerfile
          content: |
            FROM golang:{{ _.goVersion }}
            WORKDIR /app
            COPY . .
            RUN go build -o bin/{{ _.appName }} ./cmd/main.go
        - path: .dockerignore
          content: |
            .git
            vendor/
            bin/
```

**Use inline when:**
- Template is small
- Tightly coupled to solution
- Readability matters

### 2. Filesystem Source

Load templates from a directory structure:

```yaml
templates:
  - name: app-config
    source:
      type: filesystem
      path: ./templates/app
```

Directory structure:
```
templates/
├── app/
│   ├── config.yaml
│   ├── dockerfile
│   └── nginx/
│       └── nginx.conf
```

All files are treated as templates. Paths are resolved relative to solution file.

**Use filesystem when:**
- Many template files
- Reusing existing structures
- Using editor tooling

### 3. Remote Source

Fetch templates from a catalog or repository:

```yaml
templates:
  - name: terraform-base
    source:
      type: remote
      uri: https://catalog.example.com/templates/terraform@1.2.0
```

Remote templates:
- Must be versioned
- Fetched once and cached
- Useful for reusable components

**Use remote when:**
- Sharing templates across solutions
- Versioning templates independently
- Publishing building blocks

## Template Variables

Templates use **Go templating syntax** with resolver context:

```yaml
# Access resolver values
{{ _.projectName }}
{{ _.version }}
{{ _.environment }}

# String operations
{{ .Name | upper }}
{{ .Name | lower }}
{{ .Name | quote }}

# Conditionals
{{ if eq _.environment "prod" }}
  Production deployment
{{ else }}
  Non-production deployment
{{ end }}

# Loops
{{ range _.regions }}
  Region: {{ . }}
{{ end }}

# Functions
{{ len _.services }}
{{ upper .Name }}
{{ lower .Name }}
```

## Templates as Resolver Sources

The most powerful pattern: **use templates in resolvers**.

A resolver can generate and use template output:

```yaml
resolvers:
  dockerfileContent:
    description: Generated Dockerfile content
    resolve:
      from:
        - provider: template
          name: dockerfile
          # Rendered Dockerfile becomes the value

templates:
  - name: dockerfile
    source:
      type: inline
      files:
        - path: Dockerfile
          content: |
            FROM golang:{{ _.goVersion }}
            WORKDIR /app
            RUN go build -o bin/{{ _.appName }}
```

Then use in actions:

```yaml
actions:
  write-dockerfile:
    provider: filesystem
    inputs:
      operation: write
      path: ./Dockerfile
      content: "{{ _.dockerfileContent }}"
```

**This pattern:** Generate content as resolver → Use in actions.

## Complete Examples

### Example 1: Multi-File Configuration

```yaml
templates:
  - name: app-config
    source:
      type: inline
      files:
        - path: config/development.yaml
          content: |
            server:
              port: 8080
              debug: true
            database:
              host: localhost
              name: {{ _.appName }}_dev

        - path: config/production.yaml
          content: |
            server:
              port: 80
              debug: false
            database:
              host: {{ _.dbHost }}
              name: {{ _.appName }}_prod

        - path: .env.example
          content: |
            # Development environment
            DB_HOST=localhost
            DB_NAME={{ _.appName }}
            REDIS_HOST=localhost
```

### Example 2: Conditional Files

```yaml
templates:
  - name: kubernetes
    source:
      type: inline
      files:
        - path: deployment.yaml
          content: |
            apiVersion: apps/v1
            kind: Deployment
            metadata:
              name: {{ _.appName }}
            spec:
              replicas: {{ if eq _.environment "prod" }}3{{ else }}1{{ end }}
              template:
                spec:
                  containers:
                  - name: {{ _.appName }}
                    image: {{ _.registry }}/{{ _.imageName }}:{{ _.version }}
                    {{ if eq _.environment "prod" }}
                    resources:
                      requests:
                        memory: "256Mi"
                        cpu: "250m"
                      limits:
                        memory: "512Mi"
                        cpu: "500m"
                    {{ end }}
```

### Example 3: Repeated Files (foreach)

```yaml
templates:
  - name: region-configs
    source:
      type: inline
      files:
        - path: config/{{ . }}.yaml
          content: |
            region: {{ . }}
            endpoint: https://{{ . }}.api.example.com
            environment: {{ _.environment }}
    foreach:
      over: _.regions
      as: __region
```

With `regions: ["us-east", "us-west", "eu-central"]`, generates:
- `config/us-east.yaml`
- `config/us-west.yaml`
- `config/eu-central.yaml`

### Example 4: Template as Resolver Source

```yaml
resolvers:
  configContent:
    description: Generated application configuration
    resolve:
      from:
        - provider: template
          name: app-config

templates:
  - name: app-config
    source:
      type: inline
      files:
        - path: app.yaml
          content: |
            name: {{ _.appName }}
            version: {{ _.version }}
            environment: {{ _.environment }}
            debug: {{ if eq _.environment "dev" }}true{{ else }}false{{ end }}

actions:
  generate-config:
    description: Write generated config to file
    provider: filesystem
    inputs:
      operation: write
      path: ./app.yaml
      content: "{{ _.configContent }}"
```

### Example 5: Complex Templating with Filesystem Source

Directory structure:
```
templates/
├── terraform/
│   ├── main.tf
│   ├── variables.tf
│   ├── outputs.tf
│   └── environments/
│       ├── dev.tfvars
│       ├── staging.tfvars
│       └── prod.tfvars
```

Solution file:
```yaml
templates:
  - name: terraform-config
    source:
      type: filesystem
      path: ./templates/terraform

actions:
  copy-terraform:
    description: Copy Terraform files to build directory
    provider: filesystem
    inputs:
      operation: copy
      source: ./templates/terraform
      destination: ./build/terraform
```

## Rendering Context

Templates have access to all resolved values via `_`:

```yaml
resolvers:
  projectName:
    resolve:
      from:
        - provider: cli
          key: project

  version:
    resolve:
      from:
        - provider: cli
          key: version

  environment:
    resolve:
      from:
        - provider: cli
          key: env
        - provider: static
          value: development

templates:
  - name: config
    source:
      type: inline
      files:
        - path: config.yaml
          content: |
            # {{ _.projectName }}
            version: {{ _.version }}
            env: {{ _.environment }}
```

All resolvers are available. Templates wait for all resolvers to complete.

## Best Practices

1. **Keep templates simple** - Logic belongs in resolvers, not templates
2. **Use descriptive names** - Show what template produces
3. **Document template variables** - List all `_.variable` references
4. **Use filesystem for many files** - Easier than inline
5. **Use remote for sharing** - Don't duplicate templates
6. **Version remote templates** - Always specify version
7. **Use inline for examples** - Shows complete working example
8. **Validate template output** - Use resolvers to validate generated content
9. **Keep template files small** - Easier to maintain
10. **Use consistent naming** - `{{ _.appName }}` vs `{{ _.app_name }}`

## Common Patterns

### Pattern 1: Generate and Validate

```yaml
resolvers:
  dockerfileContent:
    resolve:
      from:
        - provider: template
          name: dockerfile

    validate:
      - expr: __self.contains("FROM")
        message: "Invalid Dockerfile generated"

templates:
  - name: dockerfile
    source:
      type: inline
      files:
        - path: Dockerfile
          content: |
            FROM golang:{{ _.goVersion }}
            ...
```

### Pattern 2: Environment-Specific Templates

```yaml
templates:
  - name: config-dev
    source:
      type: inline
      files:
        - path: config.yaml
          content: |
            debug: true
            log_level: debug

  - name: config-prod
    source:
      type: inline
      files:
        - path: config.yaml
          content: |
            debug: false
            log_level: error

actions:
  setup-config:
    provider: filesystem
    inputs:
      # Use resolver to select template
      content: "{{ if eq _.environment \"prod\" }}{{ _.configProd }}{{ else }}{{ _.configDev }}{{ end }}"
```

### Pattern 3: Template Composition

```yaml
resolvers:
  baseConfig:
    resolve:
      from:
        - provider: template
          name: base

  envOverrides:
    resolve:
      from:
        - provider: template
          name: env-overrides

templates:
  - name: base
    source:
      type: inline
      files:
        - path: config.yaml
          content: |
            server:
              port: 8080
            database:
              pool: 10

  - name: env-overrides
    source:
      type: inline
      files:
        - path: overrides.yaml
          content: |
            {{ if eq _.environment "prod" }}
            server:
              port: 80
            database:
              pool: 50
            {{ end }}
```

## Troubleshooting

### Issue: Template variable not rendering

Check:
1. Is resolver defined?
2. Has resolver been emitted?
3. Use correct syntax: `{{ _.resolverName }}`

```yaml
# WRONG: Missing _ or wrong syntax
{{ projectName }}
{{ $.projectName }}
{{ _.project_name }}

# CORRECT:
{{ _.projectName }}
```

### Issue: Template files not found (filesystem source)

Check:
1. Is path relative to solution file?
2. Do files actually exist?
3. Check for typos in path

```yaml
# If solution.yaml is at ./solutions/app/solution.yaml
# Then ./templates refers to ./solutions/app/templates/
path: ./templates/app
```

### Issue: Conditional not working

Check:
1. Are values strings or booleans?
2. Use correct function: `eq`, `ne`, `lt`, `gt`
3. Quote string values

```yaml
# WRONG: Comparing boolean and string
{{ if _.enabled }}       # Works
{{ if eq _.environment "prod" }}  # Correct

# CORRECT:
{{ if eq _.environment "prod" }}
  Production config
{{ end }}
```

## Go Template Functions

Common template functions:

| Function | Example | Result |
|----------|---------|--------|
| `upper` | `{{ .Name \| upper }}` | Uppercase |
| `lower` | `{{ .Name \| lower }}` | Lowercase |
| `len` | `{{ len .Items }}` | Length |
| `quote` | `{{ .Name \| quote }}` | Add quotes |
| `add` | `{{ add 1 2 }}` | Addition |
| `sub` | `{{ sub 5 2 }}` | Subtraction |
| `mul` | `{{ mul 3 4 }}` | Multiplication |
| `div` | `{{ div 10 2 }}` | Division |
| `eq` | `{{ eq .A .B }}` | Equal |
| `ne` | `{{ ne .A .B }}` | Not equal |
| `lt` | `{{ lt .A .B }}` | Less than |
| `gt` | `{{ gt .A .B }}` | Greater than |
| `and` | `{{ and .A .B }}` | Logical AND |
| `or` | `{{ or .A .B }}` | Logical OR |
| `not` | `{{ not .Bool }}` | Logical NOT |

## Next Steps

- **Templates in resolvers** → [Resolver Pipeline](./02-resolver-pipeline.md#templates-as-resolver-sources)
- **Action integration** → [Action Orchestration](./04-action-orchestration.md)
- **File generation** → [Providers Guide](./06-providers.md#example-2-configuration-generation)
- **Template schema** → [Template Schema](../schemas/template-schema.md)

---

Templates are pure, deterministic renderers. Use them to generate configuration, documentation, and code with confidence!
