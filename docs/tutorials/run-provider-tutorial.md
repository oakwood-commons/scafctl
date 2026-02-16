---
title: "Run Provider Tutorial"
weight: 26
---

# Run Provider Tutorial

This tutorial covers the `scafctl run provider` command — a tool for executing individual providers directly without a solution or resolver file. This is useful for testing, debugging, and exploring providers in isolation.

## Prerequisites

- scafctl installed and available in your PATH
- Familiarity with [providers](provider-reference.md)

## Table of Contents

1. [Basic Usage](#basic-usage)
2. [Passing Inputs](#passing-inputs)
3. [File-Based Inputs](#file-based-inputs)
4. [Capabilities](#capabilities)
5. [Dry Run](#dry-run)
6. [Output Formats](#output-formats)
7. [Plugin Providers](#plugin-providers)
8. [Discovering Providers](#discovering-providers)
9. [Common Examples](#common-examples)

---

## Basic Usage

The `scafctl run provider` command takes a provider name as its only positional argument and provider inputs via `--input` flags.

```bash
scafctl run provider <provider-name> --input <key>=<value>
```

### Your First Provider Execution

Run the `static` provider to return a simple value:

```bash
scafctl run provider static --input value=hello
```

Output:

```json
{
  "data": "hello"
}
```

The output always contains a `data` field with the provider's return value. Additional fields like `warnings` and `metadata` appear when relevant.

---

## Passing Inputs

Inputs are passed using `--input` flags. Multiple inputs can be specified by repeating the flag.

### Simple Key-Value Pairs

```bash
scafctl run provider http --input url=https://httpbin.org/get --input method=GET
```

Output:

```json
{
  "data": {
    "body": "{...}",
    "headers": {
      "Content-Type": "application/json",
      "...": "..."
    },
    "statusCode": 200
  }
}
```

### Array Values

Comma-separated values are automatically converted to arrays:

```bash
scafctl run provider exec --input command=echo --input args=hello,world
```

### Values Containing Commas

If a value itself contains commas (not intended as array separators), wrap it in
quotes inside the flag value. Use single quotes around the entire flag to prevent
shell interpretation:

```bash
scafctl run provider cel --input 'expression="[1,2,3].map(x, x * 2)"'
```

### Multiple Values for the Same Key

Repeating the same key creates an array:

```bash
scafctl run provider exec --input command=echo --input args=hello --input args=world
```

---

## File-Based Inputs

For complex inputs, load them from a YAML or JSON file using the `@` prefix:

### Create an Input File

Create `inputs.yaml`:

```yaml
url: https://httpbin.org/post
method: POST
body: '{"message": "hello"}'
headers:
  Content-Type: application/json
```

### Run with File Input

```bash
scafctl run provider http --input @inputs.yaml
```

Output:

```json
{
  "data": {
    "body": "{...}",
    "headers": { "...": "..." },
    "statusCode": 200
  }
}
```

### Mix File and Inline Inputs

File and inline inputs are merged. When the same key appears in both,
the values are combined into an array. To override a file value, omit
that key from the file:

```bash
scafctl run provider http --input @inputs.yaml --input timeout=30
```

This loads all values from `inputs.yaml` and adds `timeout=30`.

---

## Capabilities

Providers declare capabilities that define what kind of operation they perform:

| Capability | Description |
|------------|-------------|
| `from` | Data sourcing (default for most providers) |
| `transform` | Data transformation |
| `validation` | Input validation |
| `authentication` | Authentication flows |
| `action` | Side-effecting operations |

By default, `scafctl run provider` uses the provider's first declared capability. Use `--capability` to select a specific one:

```bash
# Run a provider with a specific capability
scafctl run provider cel --input expression="1 + 2" --capability transform
```

### Viewing Available Capabilities

Use `scafctl get provider <name>` to see which capabilities a provider supports:

```bash
scafctl get provider cel
```

---

## Dry Run

Use `--dry-run` to see what would be executed without actually running the provider:

```bash
scafctl run provider http --input url=https://example.com --dry-run
```

The provider will return simulated output without performing any side effects (no HTTP request, no command execution, etc.).

---

## Output Formats

The default output format is JSON. Use `-o` to change it:

```bash
# JSON output (default)
scafctl run provider static --input value=hello -o json

# YAML output
scafctl run provider static --input value=hello -o yaml

# Table output
scafctl run provider static --input value=hello -o table

# Quiet mode (exit code only)
scafctl run provider static --input value=hello -o quiet
```

### Interactive Mode

Explore complex output in an interactive TUI:

```bash
scafctl run provider http --input url=https://api.example.com -i
```

### CEL Expressions

Filter or transform output using CEL expressions:

```bash
scafctl run provider http --input url=https://api.example.com -e "_.data"
```

### Execution Metrics

Use `--show-metrics` to display timing information:

```bash
scafctl run provider http --input url=https://httpbin.org/get --show-metrics
```

---

## Plugin Providers

Load providers from plugin executables using `--plugin-dir`:

```bash
# Load plugins from a directory
scafctl run provider echo --input message=hello --plugin-dir ./plugins

# Multiple plugin directories
scafctl run provider my-plugin --input key=value --plugin-dir ./plugins --plugin-dir /opt/plugins
```

See the [Plugin Development](plugin-development.md) tutorial for creating custom providers.

---

## Discovering Providers

### List All Providers

```bash
scafctl get providers
```

### View Provider Details

```bash
scafctl get provider http
```

This shows the provider's schema, capabilities, examples, and auto-generated CLI usage examples.

### Filter by Capability

```bash
scafctl get providers --capability=validation
```

### Browse Interactively

```bash
scafctl get providers -i
```

---

## Common Examples

### Read an Environment Variable

```bash
scafctl run provider env --input operation=get --input name=HOME
```

### Execute a Shell Command

```bash
scafctl run provider exec --input command=date
```

### Read a File

```bash
scafctl run provider file --input operation=read --input path=README.md
```

### List a Directory

```bash
scafctl run provider directory --input operation=list --input path=./pkg
```

### Evaluate a CEL Expression

```bash
# Simple expression (no commas)
scafctl run provider cel --input expression="1 + 2"

# Expressions with commas must be quoted to avoid CSV splitting
scafctl run provider cel --input 'expression="[1,2,3].map(x, x * 2)"'
```

### Make an HTTP Request

```bash
scafctl run provider http --input url=https://httpbin.org/get --input method=GET
```

### Get Git Repository Status

```bash
scafctl run provider git --input operation=status --input path=.
```

### Render a Go Template

```bash
scafctl run provider go-template --input name=greeting --input 'template=Hello World'
```

### Redact Sensitive Output

Use `--redact` to mask sensitive values in the output. This example retrieves
a secret (which must already exist in the scafctl secrets store):

```bash
scafctl run provider secret --input operation=get --input name=my-secret --redact
```

---

## Next Steps

- [Actions Tutorial](actions-tutorial.md) — Learn about workflows
- [Provider Reference](provider-reference.md) — Full provider documentation
- [Resolver Tutorial](resolver-tutorial.md) — Using providers within resolvers
- [Provider Development](provider-development.md) — Creating custom providers
