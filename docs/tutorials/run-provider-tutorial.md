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

```shell
# Positional key=value (recommended)
scafctl run provider <provider-name> <key>=<value>

# Explicit --input flag
scafctl run provider <provider-name> --input <key>=<value>
```

Both forms can be mixed freely. When the same key appears multiple times, later values override earlier ones.

### Your First Provider Execution

Run the `static` provider to return a simple value:

```shell
scafctl run provider static value=hello
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

Inputs can be passed as positional `key=value` arguments or via the `--input` flag. Both forms can be combined.

### Simple Key-Value Pairs

```shell
# Positional key=value (recommended)
scafctl run provider http url=https://httpbin.org/get method=GET

# Explicit --input flag
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

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
scafctl run provider exec command=echo args=hello,world
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
```powershell
scafctl run provider exec shell=pwsh command=Write-Output "args=hello,world"
```
{{< /shell-pane >}}
{{< /shell-toggle >}}

### Values Containing Commas

If a value itself contains commas (not intended as array separators), wrap it in
quotes inside the flag value. Use single quotes around the entire flag to prevent
shell interpretation:

```shell
scafctl run provider cel 'expression="[1,2,3].map(x, x * 2)"'
```

### Multiple Values for the Same Key

Repeating the same key creates an array:

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
scafctl run provider exec command=echo args=hello args=world
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
```powershell
scafctl run provider exec shell=pwsh command=Write-Output "args=hello args=world"
```
{{< /shell-pane >}}
{{< /shell-toggle >}}

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

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
scafctl run provider http --input @inputs.yaml
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
```powershell
# Wrap @file in single quotes to avoid splatting operator
scafctl run provider http --input '@inputs.yaml'
```
{{< /shell-pane >}}
{{< /shell-toggle >}}

### Read Inputs from Stdin

Use `@-` to pipe inputs from stdin as YAML or JSON:

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
echo '{"url": "https://api.example.com", "method": "GET"}' | scafctl run provider http --input @-
cat inputs.yaml | scafctl run provider http @-
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
```powershell
'{"url": "https://api.example.com", "method": "GET"}' | scafctl run provider http --input '@-'
Get-Content inputs.yaml | scafctl run provider http '@-'
```
{{< /shell-pane >}}
{{< /shell-toggle >}}

### Pipe Raw Content into a Single Input

Use `key=@-` to read raw stdin into a specific input key, or `key=@file` to read a file's content:

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
# Pipe raw text into the message input
echo hello | scafctl run provider message message=@-

# Pipe a request body from stdin
cat body.json | scafctl run provider http url=https://api.example.com body=@-

# Read a file's raw content into an input
scafctl run provider message message=@greeting.txt
scafctl run provider http url=https://api.example.com body=@request.json
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
```powershell
# Pipe raw text into the message input
'hello' | scafctl run provider message 'message=@-'

# Pipe a request body from stdin
Get-Content body.json | scafctl run provider http 'url=https://api.example.com' 'body=@-'

# Read a file's raw content into an input
scafctl run provider message 'message=@greeting.txt'
scafctl run provider http 'url=https://api.example.com' 'body=@request.json'
```
{{< /shell-pane >}}
{{< /shell-toggle >}}

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

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
scafctl run provider http --input @inputs.yaml timeout=30
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
```powershell
scafctl run provider http --input '@inputs.yaml' timeout=30
```
{{< /shell-pane >}}
{{< /shell-toggle >}}

This loads all values from `inputs.yaml` and adds `timeout=30`.

---

## Input Key Validation

When you pass inputs, scafctl validates the input keys against the provider's
schema. Unknown keys are rejected early with a helpful error message. If the
key is close to a valid one (a likely typo), a suggestion is included:

```shell
# Typo in key name
scafctl run provider http urll=https://example.com
# Error: provider "http" does not accept input "urll" — did you mean "url"? (valid inputs: acceptableStatusCodes, authProvider, autoParseJson, body, data, headers, hostname, method, operation, pagination, poll, retry, scope, timeout, url)

# Completely unknown key
scafctl run provider http unknown=value
# Error: provider "http" does not accept input "unknown" (valid inputs: acceptableStatusCodes, authProvider, autoParseJson, body, data, headers, hostname, method, operation, pagination, poll, retry, scope, timeout, url)
```

This validation also applies to resolver and solution parameters:

```shell
# Typo in parameter name
scafctl run resolver -f solution.yaml envrionment=prod
# Error: solution does not accept input "envrionment" — did you mean "environment"? (valid inputs: environment, region)
```

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

```shell
# Run a provider with a specific capability
scafctl run provider cel expression="1 + 2" --capability transform
```

### Viewing Available Capabilities

Use `scafctl get provider <name>` to see which capabilities a provider supports:

```shell
scafctl get provider cel
```

---

## Dry Run

Use `--dry-run` to see what would be executed without actually running the provider:

```shell
scafctl run provider http url=https://example.com --dry-run
```

The provider will return simulated output without performing any side effects (no HTTP request, no command execution, etc.).

> [!NOTE]
> **Provider vs solution dry-run**: `run provider --dry-run` invokes the provider's `Execute()` method with a dry-run context flag, so the provider returns mock data. `run solution --dry-run` is different -- it uses the WhatIf model where resolvers run normally and actions are never executed. See [Dry-Run & WhatIf design](../design/dryrun-whatif.md) for details.

---

## Output Formats

The default output format is JSON. Use `-o` to change it:

```shell
# JSON output (default)
scafctl run provider static value=hello -o json

# YAML output
scafctl run provider static value=hello -o yaml

# Table output
scafctl run provider static value=hello -o table

# Quiet mode (exit code only)
scafctl run provider static value=hello -o quiet
```

### Interactive Mode

Explore complex output in an interactive TUI:

```shell
scafctl run provider http --input url=https://httpbin.org/get -i
```

### CEL Expressions

Filter or transform output using CEL expressions:

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
scafctl run provider http url=https://httpbin.org/get -e "_.data"
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
```powershell
scafctl run provider http url=https://httpbin.org/get -e '_.data'
```
{{< /shell-pane >}}
{{< /shell-toggle >}}

### Execution Metrics

Use `--show-metrics` to display timing information:

```shell
scafctl run provider http url=https://httpbin.org/get --show-metrics
```

---

## Plugin Providers

Load providers from plugin executables using `--plugin-dir`:

```shell
# Load plugins from a directory
scafctl run provider echo message=hello --plugin-dir ./plugins

# Multiple plugin directories
scafctl run provider my-plugin key=value --plugin-dir ./plugins --plugin-dir /opt/plugins
```

### Version Pinning

When multiple versions of a plugin provider are cached locally, you can pin a
specific version using `name@version` syntax or the `--plugin-version` flag:

```shell
# Pin via @version syntax (recommended)
scafctl run provider exec@0.5.0 command="echo hello"

# Pin via --plugin-version flag
scafctl run provider exec --plugin-version 0.5.0 command="echo hello"
```

Without version pinning, the latest cached version is used automatically.

See the [Provider Development Guide](provider-development.md) for creating custom providers (including [plugin delivery](provider-development.md#delivering-as-a-plugin)).

---

## Discovering Providers

### List All Providers

```shell
scafctl get providers
```

This lists all available providers with their source (`builtin` or `official`), category, capabilities, and version. Official providers are auto-fetched from the OCI catalog on first use -- no manual installation required.

> **Note:** When `--capability` or `--category` filters are active, only built-in providers are shown since official providers do not expose this metadata.

### View Provider Details

```shell
scafctl get provider http
```

This shows the provider's operations (when declared), schema, capabilities, examples, and auto-generated CLI usage examples. If the provider is not found in the built-in registry, scafctl falls back to the official provider registry and displays catalog information (or full schema and operations when the plugin is cached locally).

### Inspect Provider Operations

Multi-operation providers (such as plugin providers exposing an `operation` input) declare per-operation descriptions, capabilities, write indicators, schemas, and examples. Use `scafctl get provider <name>` to see the full operations table, or pass `--operation <op>` to inspect a single operation in detail:

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
# View all operations and schema for a cached plugin provider
scafctl get provider github

# Inspect a single operation's input/output schemas and examples
scafctl get provider github --operation create_issue

# Filter write operations as JSON using a CEL expression
scafctl get provider github -o json -e "_.operations.filter(o, o.isWrite)"
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
```powershell
# View all operations and schema for a cached plugin provider
scafctl get provider github

# Inspect a single operation's input/output schemas and examples
scafctl get provider github --operation create_issue

# Filter write operations as JSON using a CEL expression
scafctl get provider github -o json -e '_.operations.filter(o, o.isWrite)'
```
{{< /shell-pane >}}
{{< /shell-toggle >}}

### Dynamic Help for Provider Inputs

When you run `--help` with a specific provider name, the help output automatically includes the provider's input parameters with types, required/optional status, defaults, and descriptions:

```shell
scafctl run provider http --help
```

At the end of the standard help text, you'll see a section like:

```
Provider Inputs (http):
  acceptableStatusCodes  array         Status codes treated as successful (e.g. [200, 404], "2xx", "200-204")
  authProvider   string                Authentication provider to use for this request
  autoParseJson  boolean               When true and the response Content-Type is application/json, automatically parse the body into a structured object
  body           any                   Request body for POST/PUT/PATCH requests (string sent verbatim; object/array serialized to JSON)
  headers        any                   HTTP headers as key-value pairs
  hostname       string                Target endpoint URL or configured hostname alias for per-endpoint token routing
  method         string                HTTP method
  pagination     object                Pagination configuration for automatically following paginated API responses
  poll           object                Polling configuration for re-executing the request until a condition is met
  retry          any                   Retry configuration for transient failures
  scope          string                OAuth scope for authentication
  timeout        integer               Request timeout in seconds
  url            string   (required)   The URL to request
```

This works for any provider — just include the provider name before `--help`:

```shell
scafctl run provider env --help
scafctl run provider static --help
scafctl run provider file --help
```

### Filter by Capability

```shell
scafctl get providers --capability=validation
```

### Browse Interactively

```shell
scafctl get providers -i
```

---

## Common Examples

### Read an Environment Variable

```shell
scafctl run provider env operation=get name=HOME
```

### Execute a Shell Command

```shell
scafctl run provider exec command=date
```

### Read a File

```shell
scafctl run provider file operation=read path=README.md
```

### List a Directory

```shell
scafctl run provider directory operation=list path=./pkg
```

### Evaluate a CEL Expression

```shell
# Simple expression (no commas)
scafctl run provider cel expression="1 + 2"

# Expressions with commas must be quoted to avoid CSV splitting
scafctl run provider cel 'expression="[1,2,3].map(x, x * 2)"'
```

### Make an HTTP Request

```shell
scafctl run provider http url=https://httpbin.org/get method=GET
```

### Get Git Repository Status

```shell
scafctl run provider git operation=status path=.
```

### Render a Go Template

```shell
scafctl run provider go-template name=greeting 'template=Hello World'
```

### Redact Sensitive Output

Use `--redact` to mask sensitive values in the output. This example retrieves
a secret (which must already exist in the scafctl secrets store):

```shell
scafctl run provider secret operation=get name=my-secret --redact
```

---

## Next Steps

- [Actions Tutorial](actions-tutorial.md) — Learn about workflows
- [Provider Reference](provider-reference.md) — Full provider documentation
- [Resolver Tutorial](resolver-tutorial.md) — Using providers within resolvers
- [Provider Development](provider-development.md) — Creating custom providers
