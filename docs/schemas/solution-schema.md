# Solution Schema

## Overview

The **solution** is the top-level resource. It's a versioned, declarative unit that defines a complete workflow.

Follows Kubernetes conventions: `apiVersion`, `kind`, `metadata`, `spec`.

## Full Schema

```yaml
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: solution-identifier
  version: 1.0.0
  displayName: Human-Readable Name
  description: What this solution does
  category: infrastructure|application|automation|utility
  tags:
    - tag1
    - tag2
  maintainers:
    - name: Person Name
      email: person@example.com

spec:
  # Optional: Define providers once
  providers:
    - name: provider-name
      type: provider-type
      config: {}

  # Optional: Define templates
  templates:
    - name: template-name
      source:
        type: inline|filesystem|remote
        files: [...]  # for inline
        path: ./path  # for filesystem
        url: https://... # for remote

  # Required: Define resolvers
  resolvers:
    resolverName:
      description: What this resolver produces
      resolve:
        from: [...]
      transform:
        when: condition
        until: condition
        into: [...]
      validate:
        - expr: condition
          message: error message

  # Optional: Define actions
  actions:
    actionName:
      description: What this action does
      provider: provider-name
      when: condition
      foreach:
        over: _.arrayResolver
        as: item
      dependsOn:
        - other-action
      inputs: {}
      outputs: {}

# Optional: Catalog settings
catalog:
  visibility: public|private
  beta: true|false
  disabled: true|false

# Optional: Testing configuration
testing:
  tests:
    - name: test-name
      type: engine|cli|api
      synopsis: What this test validates
      # Test-specific fields...
```

## Field Descriptions

### `apiVersion`

**Type:** `string`
**Required:** Yes
**Value:** `scafctl.io/v1`

API version for this resource type. Enables future schema evolution.

### `kind`

**Type:** `string`
**Required:** Yes
**Value:** `Solution`

Resource type. Always `Solution` for this schema.

## Metadata Section

### `metadata.name`

**Type:** `string`
**Required:** Yes
**Pattern:** `^[a-z0-9-]+$`

Unique identifier for the solution. Used in commands:
```bash
scafctl run solution:my-app-name
```

### `metadata.version`

**Type:** `string`
**Required:** Yes
**Format:** Semantic versioning recommended: `MAJOR.MINOR.PATCH`

Version of this solution. Affects catalog publishing.

### `metadata.displayName`

**Type:** `string`
**Required:** Yes

Human-readable name. Shown in documentation and UIs.

### `metadata.description`

**Type:** `string`
**Required:** Yes

What this solution does. Multi-line supported with `|`:

```yaml
description: |
  Handles multi-step deployment workflow.
  Includes validation, build, test, and release.
```

### `metadata.category`

**Type:** `string`
**Required:** No
**Values:** `infrastructure`, `application`, `automation`, `utility`, custom

Categorizes solutions for organization.

### `metadata.tags`

**Type:** `array[string]`
**Required:** No

Labels for discovery. Examples: `["golang", "docker", "ci", "kubernetes"]`

### `metadata.maintainers`

**Type:** `array[object]`
**Required:** No

People responsible for this solution:

```yaml
maintainers:
  - name: John Doe
    email: john@example.com
  - name: Jane Smith
    email: jane@example.com
```

## Spec Section

### `spec.providers`

**Type:** `array[object]`
**Required:** No

Define reusable provider configurations:

```yaml
providers:
  - name: my-api
    type: api
    config:
      baseUrl: https://api.example.com
      defaultHeaders:
        Authorization: Bearer token
```

### `spec.templates`

**Type:** `array[object]`
**Required:** No

Define templates used by resolvers:

```yaml
templates:
  - name: config-template
    source:
      type: inline
      files:
        - path: Dockerfile
          content: |
            FROM golang:1.21
            RUN go build
```

### `spec.resolvers`

**Type:** `object`
**Required:** Yes (but can be empty)

Define all data sources. Keys are resolver names:

```yaml
resolvers:
  projectName:
    description: Project name from input
    resolve:
      from:
        - provider: cli
          key: name

  version:
    description: Application version
    resolve:
      from:
        - provider: static
          value: "1.0.0"
```

### `spec.actions`

**Type:** `object`
**Required:** No

Define side effects. Keys are action names:

```yaml
actions:
  build:
    description: Build the application
    provider: shell
    inputs:
      cmd: [go, build]

  deploy:
    description: Deploy to production
    provider: api
    dependsOn: [build]
    inputs:
      endpoint: https://deploy.example.com
      method: POST
```

## Catalog Section

### `catalog.visibility`

**Type:** `string`
**Values:** `public`, `private`
**Default:** `public`

Whether solution is visible in public catalogs.

### `catalog.beta`

**Type:** `boolean`
**Default:** `false`

Indicates solution is in beta. May change.

### `catalog.disabled`

**Type:** `boolean`
**Default:** `false`

Prevents solution from being used. Useful for deprecation.

## Testing Section

### `testing.tests`

**Type:** `array[object]`
**Required:** No

Define tests for this solution:

```yaml
testing:
  tests:
    - name: resolve-defaults
      type: engine
      synopsis: Validate defaults without input
      want:
        resolvers:
          projectName: my-app

    - name: dry-run
      type: cli
      synopsis: Verify dry-run works
      command: [run, solution:my-solution]
      parameters: [--dry-run]
      want:
        status: pass

    - name: fresh-run
      type: cli
      synopsis: Skip caches to validate first-run behavior
      command: [run, solution:my-solution]
      parameters: [--dry-run, --no-cache]
      want:
        status: pass
```

See [Testing Guide](../guides/testing.md) for details.

## Examples

### Minimal Solution

```yaml
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: hello-world
  version: 1.0.0
  displayName: Hello World
  description: Simple example

spec:
  resolvers:
    greeting:
      description: A greeting
      resolve:
        from:
          - provider: static
            value: "Hello, World!"
```

### Complete Solution

```yaml
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: go-pipeline
  version: 1.0.0
  displayName: Go Project Pipeline
  description: Build, test, and deploy Go applications
  category: automation
  tags: [golang, ci, docker]
  maintainers:
    - name: DevOps Team
      email: devops@example.com

spec:
  providers:
    - name: docker-registry
      type: api
      config:
        baseUrl: https://registry.example.com

  resolvers:
    projectName:
      description: Project name
      resolve:
        from:
          - provider: cli
            key: project
          - provider: static
            value: my-app

    version:
      description: Application version
      resolve:
        from:
          - provider: cli
            key: version
          - provider: git
            field: tag
          - provider: static
            value: "0.0.0-dev"

  actions:
    lint:
      description: Run linters
      provider: shell
      inputs:
        cmd: [golangci-lint, run, ./...]

    test:
      description: Run tests
      provider: shell
      dependsOn: [lint]
      inputs:
        cmd: [go, test, -v, ./...]

    build:
      description: Build binary
      provider: shell
      dependsOn: [test]
      inputs:
        cmd: [go, build, -o, bin/{{ _.projectName }}, ./cmd]

catalog:
  visibility: public
  beta: false

testing:
  tests:
    - name: resolve-defaults
      type: engine
      synopsis: Validate resolvers with defaults
      want:
        resolvers:
          projectName: my-app
          version: "0.0.0-dev"

    - name: dry-run
      type: cli
      synopsis: Verify dry-run works
      command: [run, solution:go-pipeline]
      parameters: [--dry-run]
      want:
        status: pass
```

## Best Practices

1. **Use semantic versioning** - Makes versioning clear
2. **Be descriptive** - Good descriptions help users
3. **Add maintainers** - Users know who to contact
4. **Use tags** - Help users discover solutions
5. **Categorize solutions** - Organize by purpose
6. **Include tests** - Validate solution works
7. **Keep metadata consistent** - Name matches displayName pattern
8. **Document examples** - Show common use cases
9. **Mark beta carefully** - Indicates potential changes
10. **Use categories** - Help organize growing catalogs

## Next Steps

- **Resolver schema** → [Resolver Schema](./resolver-schema.md)
- **Action schema** → [Action Schema](./action-schema.md)
- **Testing guide** → [Testing Guide](../guides/testing.md)

---

Solutions are the top-level unit of scafctl. They represent complete, versioned workflows.
