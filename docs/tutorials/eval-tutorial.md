---
title: "Eval Tutorial"
weight: 42
---

# Eval Tutorial

This tutorial covers using the `scafctl eval` command group to test CEL expressions, render Go templates, and validate solution files — all without running a full solution.

## Overview

The `eval` commands are developer tools for quick iteration:

- **`eval cel`** — Evaluate CEL expressions with inline or file-based data
- **`eval template`** — Render Go templates against JSON/YAML data

For solution file validation, use the separate **`scafctl lint`** command (covered in [Section 3](#3-validating-solution-files-with-scafctl-lint) below).

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

{{< tabs "eval-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
```bash
scafctl eval cel --expression "1 + 2"
# 3
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl eval cel --expression "1 + 2"
# 3
```
{{% /tab %}}
{{< /tabs >}}

String operations:

{{< tabs "eval-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
```bash
scafctl eval cel --expression '"hello".upperAscii()'
# HELLO
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl eval cel --expression '"hello".upperAscii()'
# HELLO
```
{{% /tab %}}
{{< /tabs >}}

### Using Inline Data

Pass JSON data to the expression via `--data`:

{{< tabs "eval-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
scafctl eval cel --expression '_.name + " is " + string(_.age)' \
  --data '{"name": "Alice", "age": 30}'
# Alice is 30
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl eval cel --expression '_.name + " is " + string(_.age)' `
  --data '{"name": "Alice", "age": 30}'
# Alice is 30
```
{{% /tab %}}
{{< /tabs >}}

The data is available under `_`, just like in solution resolvers.

### Using Data Files

For larger datasets, use `--data-file`:

{{< tabs "eval-tutorial-cmd-4" >}}
{{% tab "Bash" %}}
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
scafctl eval cel --expression '_.items.filter(i, i.active).map(i, i.name)' --file data.json
# [item-a item-c]
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Create a data file
@'
{
  "items": [
    {"name": "item-a", "active": true},
    {"name": "item-b", "active": false},
    {"name": "item-c", "active": true}
  ]
}
'@ | Set-Content -Path data.json

# Filter active items
scafctl eval cel --expression '_.items.filter(i, i.active).map(i, i.name)' --file data.json
# [item-a item-c]
```
{{% /tab %}}
{{< /tabs >}}

### Output Formats

Use `-o` to control output format:

{{< tabs "eval-tutorial-cmd-5" >}}
{{% tab "Bash" %}}
```bash
# JSON output
scafctl eval cel --expression '{"greeting": "hello", "count": 42}' -o json

# YAML output
scafctl eval cel --expression '{"greeting": "hello", "count": 42}' -o yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# JSON output
scafctl eval cel --expression '{"greeting": "hello", "count": 42}' -o json

# YAML output
scafctl eval cel --expression '{"greeting": "hello", "count": 42}' -o yaml
```
{{% /tab %}}
{{< /tabs >}}

### Built-in Functions

Test scafctl's custom CEL extension functions:

{{< tabs "eval-tutorial-cmd-6" >}}
{{% tab "Bash" %}}
```bash
# String functions
scafctl eval cel --expression '"hello".upperAscii()'
# HELLO

scafctl eval cel --expression '"HELLO".lowerAscii()'
# hello

scafctl eval cel --expression '"hello-world".replace("-", " ")'
# hello world
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# String functions
scafctl eval cel --expression '"hello".upperAscii()'
# HELLO

scafctl eval cel --expression '"HELLO".lowerAscii()'
# hello

scafctl eval cel --expression '"hello-world".replace("-", " ")'
# hello world
```
{{% /tab %}}
{{< /tabs >}}

> [!NOTE]
> **Tip:** Use `eval cel` to prototype resolver expressions before adding them to a solution. This is much faster than running the entire solution each time.

## 2. Evaluating Go Templates

### Basic Usage

Render a template with inline data:

{{< tabs "eval-tutorial-cmd-7" >}}
{{% tab "Bash" %}}
```bash
scafctl eval template --template '{{.name}} has {{.count}} items' \
  --data '{"name": "project", "count": 5}'
# project has 5 items
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl eval template --template '{{.name}} has {{.count}} items' `
  --data '{"name": "project", "count": 5}'
# project has 5 items
```
{{% /tab %}}
{{< /tabs >}}

### Template Files

For multi-line templates, use `--template-file`:

{{< tabs "eval-tutorial-cmd-8" >}}
{{% tab "Bash" %}}
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
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
@'
Hello, {{.name}}!
You have {{len .items}} items:
{{range .items}}- {{.}}
{{end}}
'@ | Set-Content -Path greeting.tmpl

scafctl eval template --template-file greeting.tmpl `
  --data '{"name": "Alice", "items": ["task-1", "task-2", "task-3"]}'
```
{{% /tab %}}
{{< /tabs >}}

### Data Files

Combine template files with data files:

{{< tabs "eval-tutorial-cmd-9" >}}
{{% tab "Bash" %}}
```bash
scafctl eval template --template-file config.tmpl --file values.json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl eval template --template-file config.tmpl --file values.json
```
{{% /tab %}}
{{< /tabs >}}

### Writing Output to File

Save rendered output to a file:

{{< tabs "eval-tutorial-cmd-10" >}}
{{% tab "Bash" %}}
```bash
scafctl eval template --template-file config.tmpl \
  --file values.json \
  -o json > generated-config.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl eval template --template-file config.tmpl `
  --file values.json `
  -o json > generated-config.yaml
```
{{% /tab %}}
{{< /tabs >}}

> [!NOTE]
> **Tip:** Use `eval template` to preview scaffold/render actions before running the full solution workflow.

## 3. Validating Solution Files with `scafctl lint`

While `eval cel` and `eval template` test individual expressions and templates, use the **`scafctl lint`** command to validate entire solution files against the schema and lint rules.

> [!WARNING]
> **Note:** `scafctl lint` is a separate top-level command, not part of `eval`. It's included here because validation is a natural part of the expression development workflow.

### Basic Validation

Check a solution file for schema errors and lint violations:

{{< tabs "eval-lint-basic" >}}
{{% tab "Bash" %}}
```bash
scafctl lint -f solution.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl lint -f solution.yaml
```
{{% /tab %}}
{{< /tabs >}}

Get structured validation results:

{{< tabs "eval-lint-json" >}}
{{% tab "Bash" %}}
```bash
scafctl lint -f solution.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl lint -f solution.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

### Filter by Severity

Only show findings at or above a minimum severity level:

{{< tabs "eval-tutorial-cmd-11" >}}
{{% tab "Bash" %}}
```bash
# Show only warnings and errors (skip info)
scafctl lint -f solution.yaml --severity warning
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Show only warnings and errors (skip info)
scafctl lint -f solution.yaml --severity warning
```
{{% /tab %}}
{{< /tabs >}}

### Quick Validation in Scripts

Use quiet mode for CI/CD pipelines — returns exit code only (0 = clean, non-zero = errors):

{{< tabs "eval-lint-quiet" >}}
{{% tab "Bash" %}}
```bash
if scafctl lint -f solution.yaml -o quiet; then
  echo "Valid!"
else
  echo "Validation failed"
  exit 1
fi
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl lint -f solution.yaml -o quiet
if ($LASTEXITCODE -eq 0) { Write-Host "Valid!" } else { Write-Host "Validation failed"; exit 1 }
```
{{% /tab %}}
{{< /tabs >}}

### Exploring Lint Rules

List all available lint rules:

{{< tabs "eval-tutorial-cmd-12" >}}
{{% tab "Bash" %}}
```bash
scafctl lint rules
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl lint rules
```
{{% /tab %}}
{{< /tabs >}}

Get a detailed explanation of a specific rule:

{{< tabs "eval-tutorial-cmd-13" >}}
{{% tab "Bash" %}}
```bash
scafctl lint explain <rule-id>
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl lint explain <rule-id>
```
{{% /tab %}}
{{< /tabs >}}

See the [Linting Tutorial](linting-tutorial.md) for a deep dive into lint rules and custom validation workflows.

## 4. Common Workflows

### Prototyping Resolver Expressions

When building a new resolver, iterate quickly with `eval cel`:

{{< tabs "eval-tutorial-cmd-14" >}}
{{% tab "Bash" %}}
```bash
# 1. Start with simple expression
scafctl eval cel --expression '"Hello, " + _.name' --data '{"name": "World"}'

# 2. Add complexity
scafctl eval cel --expression '_.items.filter(i, i.enabled).map(i, i.name).join(", ")' \
  --file real-data.json

# 3. Once working, copy to solution YAML
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# 1. Start with simple expression
scafctl eval cel --expression '"Hello, " + _.name' --data '{"name": "World"}'

# 2. Add complexity
scafctl eval cel --expression '_.items.filter(i, i.enabled).map(i, i.name).join(", ")' `
  --file real-data.json

# 3. Once working, copy to solution YAML
```
{{% /tab %}}
{{< /tabs >}}

### Testing Template Rendering

Preview template output before integrating into actions:

{{< tabs "eval-tutorial-cmd-15" >}}
{{% tab "Bash" %}}
```bash
# Test with known data
scafctl eval template --template-file deploy.tmpl --data '{"env": "staging", "replicas": 3}'

# Test with real resolver output (from a snapshot)
scafctl eval template --template-file deploy.tmpl --file snapshot.json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Test with known data
scafctl eval template --template-file deploy.tmpl --data '{"env": "staging", "replicas": 3}'

# Test with real resolver output (from a snapshot)
scafctl eval template --template-file deploy.tmpl --file snapshot.json
```
{{% /tab %}}
{{< /tabs >}}

### Pre-commit Validation

Add to your pre-commit hooks or CI pipeline:

{{< tabs "eval-precommit" >}}
{{% tab "Bash" %}}
```bash
# Validate all solution files in a directory
find . -name 'solution.yaml' -exec scafctl lint -f {} -o quiet \;
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
Get-ChildItem -Recurse -Filter 'solution.yaml' | ForEach-Object { scafctl lint -f $_.FullName -o quiet }
```
{{% /tab %}}
{{< /tabs >}}

## Next Steps

- [CEL Expressions Tutorial](cel-tutorial.md) — Deep dive into CEL expression syntax and custom functions
- [Go Templates Tutorial](go-templates-tutorial.md) — Go template syntax and scafctl template functions
- [Resolver Tutorial](resolver-tutorial.md) — Use expressions in resolvers
- [Functional Testing Tutorial](functional-testing.md) — Automated solution testing
