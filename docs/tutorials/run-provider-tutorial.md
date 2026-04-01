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

The `scafctl run provider` command takes a provider name as its first argument. Provider inputs can be passed as positional `key=value` arguments or via the traditional `--input` flag:

{{< tabs "run-provider-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
```bash
# Positional key=value (recommended)
scafctl run provider <provider-name> <key>=<value>

# Explicit --input flag
scafctl run provider <provider-name> --input <key>=<value>
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Positional key=value (recommended)
scafctl run provider <provider-name> <key>=<value>

# Explicit --input flag
scafctl run provider <provider-name> --input <key>=<value>
```
{{% /tab %}}
{{< /tabs >}}

Both forms can be mixed freely. When the same key appears multiple times, later values override earlier ones.

### Your First Provider Execution

Run the `static` provider to return a simple value:

{{< tabs "run-provider-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider static value=hello
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider static value=hello
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "data": "hello"
}
```

The output always contains a `data` field with the provider's return value. Additional fields like `warnings` and `metadata` appear when relevant.

---

## Passing Inputs

Inputs can be passed as positional `key=value` arguments or via the `--input` flag. Both forms can be combined.

### Simple Key-Value Pairs

{{< tabs "run-provider-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
# Positional key=value (recommended)
scafctl run provider http url=https://httpbin.org/get method=GET

# Explicit --input flag
scafctl run provider http --input url=https://httpbin.org/get --input method=GET
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Positional key=value (recommended)
scafctl run provider http url=https://httpbin.org/get method=GET

# Explicit --input flag
scafctl run provider http --input url=https://httpbin.org/get --input method=GET
```
{{% /tab %}}
{{< /tabs >}}

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

{{< tabs "run-provider-tutorial-cmd-4" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider exec command=echo args=hello,world
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider exec shell=pwsh command=Write-Output "args=hello,world"
```
{{% /tab %}}
{{< /tabs >}}

### Values Containing Commas

If a value itself contains commas (not intended as array separators), wrap it in
quotes inside the flag value. Use single quotes around the entire flag to prevent
shell interpretation:

{{< tabs "run-provider-tutorial-cmd-5" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider cel 'expression="[1,2,3].map(x, x * 2)"'
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider cel 'expression="[1,2,3].map(x, x * 2)"'
```
{{% /tab %}}
{{< /tabs >}}

### Multiple Values for the Same Key

Repeating the same key creates an array:

{{< tabs "run-provider-tutorial-cmd-6" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider exec command=echo args=hello args=world
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider exec shell=pwsh command=Write-Output "args=hello args=world"
```
{{% /tab %}}
{{< /tabs >}}

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

{{< tabs "runprov-file-input" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider http --input @inputs.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Wrap @file in single quotes to avoid splatting operator
scafctl run provider http --input '@inputs.yaml'
```
{{% /tab %}}
{{< /tabs >}}

### Read Inputs from Stdin

Use `@-` to pipe inputs from stdin as YAML or JSON:

{{< tabs "runprov-stdin-input" >}}
{{% tab "Bash" %}}
```bash
echo '{"url": "https://api.example.com", "method": "GET"}' | scafctl run provider http --input @-
cat inputs.yaml | scafctl run provider http @-
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
'{"url": "https://api.example.com", "method": "GET"}' | scafctl run provider http --input '@-'
Get-Content inputs.yaml | scafctl run provider http '@-'
```
{{% /tab %}}
{{< /tabs >}}

### Pipe Raw Content into a Single Input

Use `key=@-` to read raw stdin into a specific input key, or `key=@file` to read a file's content:

{{< tabs "runprov-raw-stdin" >}}
{{% tab "Bash" %}}
```bash
# Pipe raw text into the message input
echo hello | scafctl run provider message message=@-

# Pipe a request body from stdin
cat body.json | scafctl run provider http url=https://api.example.com body=@-

# Read a file's raw content into an input
scafctl run provider message message=@greeting.txt
scafctl run provider http url=https://api.example.com body=@request.json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Pipe raw text into the message input
'hello' | scafctl run provider message 'message=@-'

# Pipe a request body from stdin
Get-Content body.json | scafctl run provider http 'url=https://api.example.com' 'body=@-'

# Read a file's raw content into an input
scafctl run provider message 'message=@greeting.txt'
scafctl run provider http 'url=https://api.example.com' 'body=@request.json'
```
{{% /tab %}}
{{< /tabs >}}

> **Note:** A single trailing newline is trimmed automatically. `key=@-` reads raw text — it does not parse YAML/JSON.

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

{{< tabs "runprov-mixed-input" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider http --input @inputs.yaml timeout=30
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider http --input '@inputs.yaml' timeout=30
```
{{% /tab %}}
{{< /tabs >}}

This loads all values from `inputs.yaml` and adds `timeout=30`.

---

## Input Key Validation

When you pass inputs, scafctl validates the input keys against the provider's
schema. Unknown keys are rejected early with a helpful error message. If the
key is close to a valid one (a likely typo), a suggestion is included:

{{< tabs "run-provider-tutorial-cmd-7" >}}
{{% tab "Bash" %}}
```bash
# Typo in key name
scafctl run provider http urll=https://example.com
# Error: provider "http" does not accept input "urll" — did you mean "url"? (valid inputs: body, headers, method, timeout, url)

# Completely unknown key
scafctl run provider http unknown=value
# Error: provider "http" does not accept input "unknown" (valid inputs: body, headers, method, timeout, url)
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Typo in key name
scafctl run provider http urll=https://example.com
# Error: provider "http" does not accept input "urll" — did you mean "url"? (valid inputs: body, headers, method, timeout, url)

# Completely unknown key
scafctl run provider http unknown=value
# Error: provider "http" does not accept input "unknown" (valid inputs: body, headers, method, timeout, url)
```
{{% /tab %}}
{{< /tabs >}}

This validation also applies to resolver and solution parameters:

{{< tabs "run-provider-tutorial-cmd-8" >}}
{{% tab "Bash" %}}
```bash
# Typo in parameter name
scafctl run resolver -f solution.yaml envrionment=prod
# Error: solution does not accept input "envrionment" — did you mean "environment"? (valid inputs: environment, region)
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Typo in parameter name
scafctl run resolver -f solution.yaml envrionment=prod
# Error: solution does not accept input "envrionment" — did you mean "environment"? (valid inputs: environment, region)
```
{{% /tab %}}
{{< /tabs >}}

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

{{< tabs "run-provider-tutorial-cmd-9" >}}
{{% tab "Bash" %}}
```bash
# Run a provider with a specific capability
scafctl run provider cel expression="1 + 2" --capability transform
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Run a provider with a specific capability
scafctl run provider cel expression="1 + 2" --capability transform
```
{{% /tab %}}
{{< /tabs >}}

### Viewing Available Capabilities

Use `scafctl get provider <name>` to see which capabilities a provider supports:

{{< tabs "run-provider-tutorial-cmd-10" >}}
{{% tab "Bash" %}}
```bash
scafctl get provider cel
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl get provider cel
```
{{% /tab %}}
{{< /tabs >}}

---

## Dry Run

Use `--dry-run` to see what would be executed without actually running the provider:

{{< tabs "run-provider-tutorial-cmd-11" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider http url=https://example.com --dry-run
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider http url=https://example.com --dry-run
```
{{% /tab %}}
{{< /tabs >}}

The provider will return simulated output without performing any side effects (no HTTP request, no command execution, etc.).

> [!NOTE]
> **Provider vs solution dry-run**: `run provider --dry-run` invokes the provider's `Execute()` method with a dry-run context flag, so the provider returns mock data. `run solution --dry-run` is different — it uses the WhatIf model where resolvers run normally and actions are never executed. See [Dry-Run & WhatIf design]({{< relref "/docs/design/dryrun-whatif" >}}) for details.

---

## Output Formats

The default output format is JSON. Use `-o` to change it:

{{< tabs "run-provider-tutorial-cmd-12" >}}
{{% tab "Bash" %}}
```bash
# JSON output (default)
scafctl run provider static value=hello -o json

# YAML output
scafctl run provider static value=hello -o yaml

# Table output
scafctl run provider static value=hello -o table

# Quiet mode (exit code only)
scafctl run provider static value=hello -o quiet
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# JSON output (default)
scafctl run provider static value=hello -o json

# YAML output
scafctl run provider static value=hello -o yaml

# Table output
scafctl run provider static value=hello -o table

# Quiet mode (exit code only)
scafctl run provider static value=hello -o quiet
```
{{% /tab %}}
{{< /tabs >}}

### Interactive Mode

Explore complex output in an interactive TUI:

{{< tabs "runprov-interactive" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider http --input url=https://httpbin.org/get -i
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider http --input url=https://httpbin.org/get -i
```
{{% /tab %}}
{{< /tabs >}}

### CEL Expressions

Filter or transform output using CEL expressions:

{{< tabs "runprov-cel-expr" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider http url=https://httpbin.org/get -e "_.data"
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider http url=https://httpbin.org/get -e '_.data'
```
{{% /tab %}}
{{< /tabs >}}

### Execution Metrics

Use `--show-metrics` to display timing information:

{{< tabs "run-provider-tutorial-cmd-13" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider http url=https://httpbin.org/get --show-metrics
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider http url=https://httpbin.org/get --show-metrics
```
{{% /tab %}}
{{< /tabs >}}

---

## Plugin Providers

Load providers from plugin executables using `--plugin-dir`:

{{< tabs "run-provider-tutorial-cmd-14" >}}
{{% tab "Bash" %}}
```bash
# Load plugins from a directory
scafctl run provider echo message=hello --plugin-dir ./plugins

# Multiple plugin directories
scafctl run provider my-plugin key=value --plugin-dir ./plugins --plugin-dir /opt/plugins
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Load plugins from a directory
scafctl run provider Write-Output "message=hello --plugin-dir ./plugins"

# Multiple plugin directories
scafctl run provider my-plugin key=value --plugin-dir ./plugins --plugin-dir /opt/plugins
```
{{% /tab %}}
{{< /tabs >}}

See the [Provider Development Guide](provider-development.md) for creating custom providers (including [plugin delivery](provider-development.md#delivering-as-a-plugin)).

---

## Discovering Providers

### List All Providers

{{< tabs "run-provider-tutorial-cmd-15" >}}
{{% tab "Bash" %}}
```bash
scafctl get providers
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl get providers
```
{{% /tab %}}
{{< /tabs >}}

### View Provider Details

{{< tabs "run-provider-tutorial-cmd-16" >}}
{{% tab "Bash" %}}
```bash
scafctl get provider http
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl get provider http
```
{{% /tab %}}
{{< /tabs >}}

This shows the provider's schema, capabilities, examples, and auto-generated CLI usage examples.

### Dynamic Help for Provider Inputs

When you run `--help` with a specific provider name, the help output automatically includes the provider's input parameters with types, required/optional status, defaults, and descriptions:

{{< tabs "run-provider-tutorial-cmd-17" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider http --help
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider http --help
```
{{% /tab %}}
{{< /tabs >}}

At the end of the standard help text, you'll see a section like:

```
Provider Inputs (http):
  body     string               Request body for POST/PUT/PATCH requests
  headers  any                  HTTP headers as key-value pairs
  method   string               HTTP method
  timeout  integer              Request timeout in seconds
  url      string   (required)  The URL to request
```

This works for any provider — just include the provider name before `--help`:

{{< tabs "run-provider-tutorial-cmd-18" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider env --help
scafctl run provider static --help
scafctl run provider file --help
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider env --help
scafctl run provider static --help
scafctl run provider file --help
```
{{% /tab %}}
{{< /tabs >}}

### Filter by Capability

{{< tabs "run-provider-tutorial-cmd-19" >}}
{{% tab "Bash" %}}
```bash
scafctl get providers --capability=validation
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl get providers --capability=validation
```
{{% /tab %}}
{{< /tabs >}}

### Browse Interactively

{{< tabs "run-provider-tutorial-cmd-20" >}}
{{% tab "Bash" %}}
```bash
scafctl get providers -i
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl get providers -i
```
{{% /tab %}}
{{< /tabs >}}

---

## Common Examples

### Read an Environment Variable

{{< tabs "run-provider-tutorial-cmd-21" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider env operation=get name=HOME
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider env operation=get name=HOME
```
{{% /tab %}}
{{< /tabs >}}

### Execute a Shell Command

{{< tabs "run-provider-tutorial-cmd-22" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider exec command=date
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider exec command=date
```
{{% /tab %}}
{{< /tabs >}}

### Read a File

{{< tabs "run-provider-tutorial-cmd-23" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider file operation=read path=README.md
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider file operation=read path=README.md
```
{{% /tab %}}
{{< /tabs >}}

### List a Directory

{{< tabs "run-provider-tutorial-cmd-24" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider directory operation=list path=./pkg
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider directory operation=list path=./pkg
```
{{% /tab %}}
{{< /tabs >}}

### Evaluate a CEL Expression

{{< tabs "run-provider-tutorial-cmd-25" >}}
{{% tab "Bash" %}}
```bash
# Simple expression (no commas)
scafctl run provider cel expression="1 + 2"

# Expressions with commas must be quoted to avoid CSV splitting
scafctl run provider cel 'expression="[1,2,3].map(x, x * 2)"'
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Simple expression (no commas)
scafctl run provider cel expression="1 + 2"

# Expressions with commas must be quoted to avoid CSV splitting
scafctl run provider cel 'expression="[1,2,3].map(x, x * 2)"'
```
{{% /tab %}}
{{< /tabs >}}

### Make an HTTP Request

{{< tabs "run-provider-tutorial-cmd-26" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider http url=https://httpbin.org/get method=GET
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider http url=https://httpbin.org/get method=GET
```
{{% /tab %}}
{{< /tabs >}}

### Get Git Repository Status

{{< tabs "run-provider-tutorial-cmd-27" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider git operation=status path=.
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider git operation=status path=.
```
{{% /tab %}}
{{< /tabs >}}

### Render a Go Template

{{< tabs "run-provider-tutorial-cmd-28" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider go-template name=greeting 'template=Hello World'
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider go-template name=greeting 'template=Hello World'
```
{{% /tab %}}
{{< /tabs >}}

### Redact Sensitive Output

Use `--redact` to mask sensitive values in the output. This example retrieves
a secret (which must already exist in the scafctl secrets store):

{{< tabs "run-provider-tutorial-cmd-29" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider secret operation=get name=my-secret --redact
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider secret operation=get name=my-secret --redact
```
{{% /tab %}}
{{< /tabs >}}

---

## Next Steps

- [Actions Tutorial](actions-tutorial.md) — Learn about workflows
- [Provider Reference](provider-reference.md) — Full provider documentation
- [Resolver Tutorial](resolver-tutorial.md) — Using providers within resolvers
- [Provider Development](provider-development.md) — Creating custom providers
