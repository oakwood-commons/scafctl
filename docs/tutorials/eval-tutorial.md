---
title: "Eval Tutorial"
weight: 52
---

# Eval Tutorial

This tutorial covers using the `scafctl eval` command group to test CEL expressions, render Go templates, and validate solution files — all without running a full solution.

## Overview

The `eval` commands are developer tools for quick iteration:

- **`eval cel`** — Evaluate CEL expressions with inline or file-based data
- **`eval template`** — Render Go templates against JSON/YAML data
- **`eval validate`** — Validate a solution file against the schema and lint rules

```
┌──────────────┐         ┌──────────────┐         ┌──────────────┐
│  Expression  │  ────►  │  scafctl     │  ────►  │   Result     │
│  or Template │         │    eval      │         │   (stdout)   │
└──────────────┘         └──────────────┘         └──────────────┘
```

These commands are especially useful when writing or debugging resolver expressions, action templates, and solution configurations.

## 1. Evaluating CEL Expressions

### Basic Usage

Evaluate a simple expression:

```bash
scafctl eval cel "1 + 2"
# 3
```

String operations:

```bash
scafctl eval cel '"hello".upperAscii()'
# HELLO
```

### Using Inline Data

Pass JSON data to the expression via `--data`:

```bash
scafctl eval cel '_.name + " is " + string(_.age)' \
  --data '{"name": "Alice", "age": 30}'
# Alice is 30
```

The data is available under `_`, just like in solution resolvers.

### Using Data Files

For larger datasets, use `--data-file`:

```bash
# Create a data file
cat > data.json << 'EOF'
{
  "items": [
    {"name": "item-a", "active": true},
    {"name": "item-b", "active": false},
    {"name": "item-c", "active": true}
  ]
}
EOF

# Filter active items
scafctl eval cel '_.items.filter(i, i.active).map(i, i.name)' --data-file data.json
# ["item-a", "item-c"]
```

### Output Formats

Use `-o` to control output format:

```bash
# JSON output
scafctl eval cel '{"greeting": "hello", "count": 42}' -o json

# YAML output
scafctl eval cel '{"greeting": "hello", "count": 42}' -o yaml
```

### Built-in Functions

Test scafctl's custom CEL extension functions:

```bash
# String functions
scafctl eval cel '"hello-world".toCamelCase()'
# helloWorld

scafctl eval cel '"hello-world".toPascalCase()'
# HelloWorld

# List the available CEL functions
scafctl eval cel 'true' --list-functions
```

> **Tip:** Use `eval cel` to prototype resolver expressions before adding them to a solution. This is much faster than running the entire solution each time.

## 2. Evaluating Go Templates

### Basic Usage

Render a template with inline data:

```bash
scafctl eval template '{{.name}} has {{.count}} items' \
  --data '{"name": "project", "count": 5}'
# project has 5 items
```

### Template Files

For multi-line templates, use `--template-file`:

```bash
cat > greeting.tmpl << 'EOF'
Hello, {{.name}}!
You have {{len .items}} items:
{{range .items}}- {{.}}
{{end}}
EOF

scafctl eval template --template-file greeting.tmpl \
  --data '{"name": "Alice", "items": ["task-1", "task-2", "task-3"]}'
```

### Data Files

Combine template files with data files:

```bash
scafctl eval template --template-file config.tmpl --data-file values.json
```

### Writing Output to File

Save rendered output to a file:

```bash
scafctl eval template --template-file config.tmpl \
  --data-file values.json \
  --output generated-config.yaml
```

> **Tip:** Use `eval template` to preview scaffold/render actions before running the full solution workflow.

## 3. Validating Solution Files

### Basic Validation

Check a solution file for schema errors and lint violations:

```bash
scafctl eval validate -f solution.yaml
```

### JSON Output

Get structured validation results:

```bash
scafctl eval validate -f solution.yaml -o json
```

This returns:

```json
{
  "file": "solution.yaml",
  "errorCount": 0,
  "warnCount": 1,
  "infoCount": 0,
  "findings": [
    {
      "rule": "missing-description",
      "severity": "warning",
      "message": "Solution metadata should include a description",
      "location": "metadata"
    }
  ]
}
```

### Quick Validation in Scripts

Use quiet mode for CI/CD pipelines:

```bash
if scafctl eval validate -f solution.yaml -o quiet; then
  echo "Valid!"
else
  echo "Validation failed"
  exit 1
fi
```

## 4. Common Workflows

### Prototyping Resolver Expressions

When building a new resolver, iterate quickly with `eval cel`:

```bash
# 1. Start with simple expression
scafctl eval cel '"Hello, " + _.name' --data '{"name": "World"}'

# 2. Add complexity
scafctl eval cel '_.items.filter(i, i.enabled).map(i, i.name).join(", ")' \
  --data-file real-data.json

# 3. Once working, copy to solution YAML
```

### Testing Template Rendering

Preview template output before integrating into actions:

```bash
# Test with known data
scafctl eval template --template-file deploy.tmpl --data '{"env": "staging", "replicas": 3}'

# Test with real resolver output (from a snapshot)
scafctl eval template --template-file deploy.tmpl --data-file snapshot.json
```

### Pre-commit Validation

Add to your pre-commit hooks or CI pipeline:

```bash
# Validate all solution files in a directory
find . -name 'solution.yaml' -exec scafctl eval validate -f {} -o quiet \;
```

## Next Steps

- [CEL Expressions Tutorial](cel-tutorial.md) — Deep dive into CEL expression syntax and custom functions
- [Go Templates Tutorial](go-templates-tutorial.md) — Go template syntax and scafctl template functions
- [Resolver Tutorial](resolver-tutorial.md) — Use expressions in resolvers
- [Functional Testing Tutorial](functional-testing.md) — Automated solution testing
