# Bundling Example

This example demonstrates advanced bundle configuration, including:

- Explicit `bundle.include` patterns for files not auto-discovered
- `bundle.exclude` patterns to filter out test files
- Catalog dependency vendoring
- Dynamic path handling

## Directory Structure

```
bundling-example/
├── solution.yaml          # Main solution with bundle configuration
├── configs/
│   ├── dev.yaml           # Environment-specific config
│   ├── staging.yaml
│   └── prod.yaml
└── templates/
    ├── deployment.yaml    # Kubernetes template
    └── service.yaml
```

## Running the Example

### 1. Preview the Bundle

See what files would be included:

```bash
scafctl build solution examples/catalog/bundling-example/solution.yaml --dry-run
```

### 2. Build to Catalog

```bash
scafctl build solution examples/catalog/bundling-example/solution.yaml --version 1.0.0
```

### 3. Verify the Bundle

```bash
scafctl bundle verify bundling-example@1.0.0
```

### 4. List Bundle Contents

```bash
scafctl bundle extract bundling-example@1.0.0 --list
```

### 5. Run the Solution

```bash
scafctl run solution bundling-example -r environment=dev
```

## Key Concepts Demonstrated

### Dynamic Paths

The `config-file` resolver uses a CEL expression to compute the file path:

```yaml
inputs:
  path:
    expr: "'configs/' + _.environment + '.yaml'"
```

Because this path is dynamic, scafctl cannot auto-discover the config files. The `bundle.include` pattern ensures all configs are bundled:

```yaml
bundle:
  include:
    - "configs/**/*.yaml"
```

### Exclude Patterns

Any files matching `*_test.yaml` are excluded from the bundle:

```yaml
bundle:
  exclude:
    - "**/*_test.yaml"
```

### What Gets Bundled

When you build this solution:

1. `solution.yaml` — the solution itself
2. `templates/*.yaml` — auto-discovered from the static `file` provider reference
3. `configs/*.yaml` — explicitly included via `bundle.include`
4. Any `*_test.yaml` files — excluded by `bundle.exclude`

## Next Steps

- See the [Catalog Tutorial](../../../docs/tutorials/catalog-tutorial.md#advanced-bundling) for more details
- Try modifying `bundle.include`/`bundle.exclude` and rebuilding
- Use `scafctl bundle diff` to compare versions after changes
