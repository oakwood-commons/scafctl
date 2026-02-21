---
title: "MCP Server Tutorial"
weight: 55
---

# MCP Server Tutorial

This tutorial walks you through setting up scafctl's Model Context Protocol (MCP) server so that AI agents — GitHub Copilot, Claude, Cursor, Windsurf, and others — can discover and invoke scafctl tools programmatically.

## Prerequisites

- scafctl installed and on your `$PATH` (`scafctl version` should work)
- An MCP-compatible AI client (VS Code with Copilot, Claude Desktop, Cursor, or Windsurf)

## What Is the MCP Server?

The MCP server is a **local process** that translates between the AI agent's JSON-RPC protocol and scafctl's Go library functions. When an AI agent decides to use a tool (e.g., "lint this solution"), it sends a structured request to the MCP server, which calls the same code that the CLI commands use and returns structured JSON results.

Most tools exposed by the MCP server are **read-only** — they inspect, validate, evaluate, and list. A few tools (`preview_resolvers`, `run_solution_tests`, `render_solution`) execute solution code which may have side effects depending on the providers used (e.g., exec, http).

```
AI Agent (Copilot, Claude, etc.)
    │
    │  JSON-RPC 2.0 over stdio
    ▼
scafctl mcp serve
    │
    │  Direct Go function calls
    ▼
scafctl libraries (solution, provider, CEL, catalog, auth)
```

## 1. Getting Started

### Verify the MCP Server

Run the `--info` flag to confirm the server is built and can list its tools:

```bash
scafctl mcp serve --info
```

You should see JSON output listing all available tools:

```json
{
  "name": "scafctl",
  "version": "dev",
  "tools": [
    { "name": "auth_status", "description": "Report which auth handlers (e.g. entra, gcp, github) are configured..." },
    { "name": "catalog_list", "description": "List entries in the local catalog..." },
    { "name": "evaluate_cel", "description": "Evaluate a CEL expression..." },
    { "name": "get_provider_schema", "description": "Get the full JSON Schema for a provider..." },
    { "name": "inspect_solution", "description": "Get full solution metadata..." },
    { "name": "lint_solution", "description": "Validate a solution file..." },
    { "name": "list_cel_functions", "description": "List all available CEL functions..." },
    { "name": "list_providers", "description": "List all available providers..." },
    { "name": "list_solutions", "description": "List available solutions from the local catalog..." },
    { "name": "render_solution", "description": "Render a solution's action graph..." }
  ]
}
```

If this works, the MCP server is ready.

### Start the Server Manually (Optional)

You can start the server interactively for testing:

```bash
scafctl mcp serve
```

The server listens on stdin/stdout for JSON-RPC 2.0 messages. Press `Ctrl+C` to stop. Normally, your AI client starts and stops the server automatically.

## 2. VS Code / GitHub Copilot Setup

### Option A: Project-Level Configuration (Recommended)

Create `.vscode/mcp.json` in your project root:

```json
{
  "servers": {
    "scafctl": {
      "type": "stdio",
      "command": "scafctl",
      "args": ["mcp", "serve"]
    }
  }
}
```

### Option B: User-Level Configuration

Add to your VS Code `settings.json` (`Cmd+Shift+P` → "Preferences: Open User Settings (JSON)"):

```json
{
  "mcp": {
    "servers": {
      "scafctl": {
        "type": "stdio",
        "command": "scafctl",
        "args": ["mcp", "serve"]
      }
    }
  }
}
```

### Verify in VS Code

1. Reload the VS Code window (`Cmd+Shift+P` → "Developer: Reload Window")
2. Open the Copilot chat panel
3. The scafctl tools should appear in the tool picker (click the tool icon in the chat input)
4. Try asking Copilot: *"List my available providers"*

## 3. Claude Desktop Setup

Add to your Claude Desktop configuration file:

- **macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "scafctl": {
      "command": "scafctl",
      "args": ["mcp", "serve"]
    }
  }
}
```

Restart Claude Desktop. The scafctl tools will appear in the tool menu (hammer icon).

## 4. Cursor Setup

Create `.cursor/mcp.json` in your project root:

```json
{
  "mcpServers": {
    "scafctl": {
      "command": "scafctl",
      "args": ["mcp", "serve"]
    }
  }
}
```

Restart Cursor. The tools will be available in the AI chat.

## 5. Windsurf Setup

Create `.windsurf/mcp.json` in your project root:

```json
{
  "mcpServers": {
    "scafctl": {
      "command": "scafctl",
      "args": ["mcp", "serve"]
    }
  }
}
```

Restart Windsurf to pick up the configuration.

## 6. Using the Tools

Once connected, your AI agent can use scafctl tools through natural conversation. This section walks through each tool with a concrete example you can follow along with.

### Setup: Create a Sample Solution

Most examples below reference a solution file. Create `mcp-demo/solution.yaml` in your project:

```bash
mkdir -p mcp-demo
```

You can copy the pre-built example or create it from scratch:

```bash
# Option A: Copy the example file
cp examples/mcp/tutorial-solution.yaml mcp-demo/solution.yaml

# Option B: Create it manually (paste the YAML below)
```

Paste this into `mcp-demo/solution.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: greeting-service
  version: 1.0.0
  description: A simple greeting service that demonstrates resolvers, validation, and actions.
  maintainers:
    - name: Tutorial User
      email: tutorial@example.com
  tags:
    - tutorial
    - demo

spec:
  resolvers:
    username:
      description: Name of the person to greet
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: username
          - provider: static
            inputs:
              value: World
      validate:
        with:
          - provider: validation
            inputs:
              match: '^[A-Za-z ]{1,50}$'
            message: "Username must be 1-50 alphabetic characters"

    style:
      description: Greeting style
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: style
          - provider: static
            inputs:
              value: friendly
      transform:
        with:
          - provider: cel
            inputs:
              expression: '__self.lowerAscii()'
      validate:
        with:
          - provider: validation
            inputs:
              expression: '__self in ["friendly", "formal", "casual"]'
            message: "Style must be friendly, formal, or casual"

    message:
      description: Composed greeting message
      type: string
      dependsOn:
        - username
        - style
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                _.style == "formal" ? "Good day, " + _.username + "."
                : _.style == "casual" ? "Hey " + _.username + "!"
                : "Hello, " + _.username + "! Welcome!"

  workflow:
    actions:
      greet:
        description: Print the greeting to the console
        provider: exec
        inputs:
          command:
            expr: '"echo " + _.message'

      log:
        description: Log the greeting event
        provider: static
        dependsOn:
          - greet
        inputs:
          value:
            event: greeting_sent
            user:
              rslvr: username
            style:
              rslvr: style
```

### List Available Providers

No files needed — this tool lists the built-in providers.

> **You:** "What solution providers are available?"

The AI calls `list_providers` and returns something like:

```
Here are the available solution providers:

| Name        | Category    | Capabilities       |
|-------------|-------------|--------------------|
| cel         | transform   | from, transform    |
| debug       | utility     | action             |
| directory   | data        | from               |
| env         | data        | from               |
| exec        | action      | from, action       |
| file        | data        | from               |
| git         | data        | from               |
| go-template | transform   | from, transform    |
| http        | data        | from, action       |
| identity    | auth        | from               |
| parameter   | data        | from               |
| secret      | data        | from               |
| sleep       | utility     | action             |
| static      | data        | from, action       |
| validation  | validation  | validation         |

15 providers available. Use `get_provider_schema` with a specific
provider name to see its full input schema and examples.
```

### Inspect a Solution

Uses the `mcp-demo/solution.yaml` file created above.

> **You:** "Explain what the greeting-service solution does"

The AI calls `inspect_solution` with `path: "mcp-demo/solution.yaml"` and returns something like:

```
## greeting-service v1.0.0

**Description:** A simple greeting service that demonstrates resolvers,
validation, and actions.

**Maintainers:** Tutorial User <tutorial@example.com>
**Tags:** tutorial, demo

### Resolvers (3)

| Resolver | Type   | Description                  | Providers              |
|----------|--------|------------------------------|------------------------|
| username | string | Name of the person to greet  | parameter → static     |
| style    | string | Greeting style               | parameter → static     |
| message  | string | Composed greeting message    | cel                    |

- `message` depends on: username, style
- `username` validates via regex: `^[A-Za-z ]{1,50}$`
- `style` validates via CEL: `__self in ["friendly", "formal", "casual"]`

### Actions (2)

| Action | Provider | Description                      | Depends On |
|--------|----------|----------------------------------|------------|
| greet  | exec     | Print the greeting to the console|            |
| log    | static   | Log the greeting event           | greet      |
```

### Validate a Solution

Uses the `mcp-demo/solution.yaml` file created above.

> **You:** "Validate my mcp-demo/solution.yaml file"

The AI calls `lint_solution` with `file: "mcp-demo/solution.yaml"` and returns something like:

```
✅ Validation passed — no errors or warnings found in mcp-demo/solution.yaml.

The solution "greeting-service" (v1.0.0) is well-formed with 3 resolvers
and 2 actions.
```

To see what lint findings look like, you can also try validating the intentionally broken example:

> **You:** "Lint the file examples/solutions/bad-solution-yaml/solution.yaml"

The AI calls `lint_solution` and returns structured findings with severity, location, and descriptions for each issue.

### Evaluate a CEL Expression

No files needed — you provide the expression and data inline.

> **You:** "Evaluate this CEL expression with sample data: `_.items.filter(i, i.enabled).map(i, i.name)`"
>
> Use this data: `{"items": [{"name": "alpha", "enabled": true}, {"name": "beta", "enabled": false}, {"name": "gamma", "enabled": true}]}`

The AI calls `evaluate_cel` with the expression and data, and returns something like:

```
Expression: _.items.filter(i, i.enabled).map(i, i.name)

Result: ["alpha", "gamma"]
Type:   list(string)
```

You can also test expressions used in your solution:

> **You:** "Evaluate this: `_.style == "formal" ? "Good day, " + _.username + "." : "Hello, " + _.username + "!"` with data `{"style": "formal", "username": "Alice"}`"

```
Result: "Good day, Alice."
Type:   string
```

### Discover CEL Functions

No files needed.

> **You:** "What custom CEL functions can I use for string manipulation?"

The AI calls `list_cel_functions` with `custom_only: true` and returns something like:

```
Here are the custom scafctl CEL functions for string manipulation:

- **lowerAscii(string) → string** — Convert to lowercase
- **upperAscii(string) → string** — Convert to uppercase
- **trim(string) → string** — Remove leading/trailing whitespace
- **contains(string, string) → bool** — Check substring
- **replace(string, string, string) → string** — Replace occurrences
- **split(string, string) → list(string)** — Split by delimiter
- **join(list(string), string) → string** — Join with delimiter
...

Use `evaluate_cel` to test any of these interactively.
```

### Browse the Catalog

This tool queries your local catalog. If you haven't added solutions to the catalog yet, the result will be empty.

> **You:** "What solutions are in my local catalog?"

The AI calls `catalog_list` with `kind: "solution"` and returns something like:

```
Your local catalog contains 0 solutions.

To add solutions to the catalog, see the Catalog Tutorial.
```

Or, if you have entries:

```
Found 2 solutions in your local catalog:

| Name             | Version | Description                     |
|------------------|---------|---------------------------------|
| greeting-service | 1.0.0   | A simple greeting service       |
| deploy-gcp       | 2.1.0   | Deploy services to GCP          |
```

### Check Authentication

No files needed — this inspects your current auth state.

> **You:** "Am I authenticated for any services?"

The AI calls `auth_status` and returns something like:

```
You have 2 authenticated auth handlers:

✅ gcp (Google Cloud Platform)
   Identity: user | Expires: May 18, 2026
   Flows: interactive, service_principal, workload_identity, metadata

✅ github (GitHub)
   Identity: user | Username: kcloutie
   Flows: device_code, pat

❌ entra (Microsoft Entra ID)
   Not authenticated
   Flows: device_code, service_principal, workload_identity

Run `scafctl auth login <handler>` to authenticate.
```

### Render a Solution Graph

Uses the `mcp-demo/solution.yaml` file created above.

> **You:** "Show me the action graph for mcp-demo/solution.yaml"

The AI calls `render_solution` with `path: "mcp-demo/solution.yaml"` and `graph_type: "action"` and returns something like:

```
Action execution graph for greeting-service:

  greet
    └── log

Execution order:
  1. greet (exec) — Print the greeting to the console
  2. log (static) — Log the greeting event [depends on: greet]
```

You can also request the resolver dependency graph:

> **You:** "Show me the resolver dependency graph for mcp-demo/solution.yaml"

```
Resolver dependency graph:

  username ──┐
             ├──► message
  style ─────┘

Resolution order:
  Phase 1: username, style (parallel — no dependencies)
  Phase 2: message (depends on: username, style)
```

### Get Provider Schema

No files needed — you specify the provider by name.

> **You:** "What inputs does the `exec` provider accept?"

The AI calls `get_provider_schema` with `name: "exec"` and returns structured JSON with:

- **Input schema** — each property has `type`, `description`, `required` (true/false), `default`, `example`, and `enum` where applicable
- **Output schemas** — per capability (action, from, transform)
- **Examples** — YAML usage examples for common patterns
- **CLI usage** — auto-generated `scafctl run provider` commands

```json
{
  "name": "exec",
  "description": "Executes shell commands using an embedded cross-platform POSIX shell interpreter...",
  "capabilities": ["action", "from", "transform"],
  "schema": {
    "properties": {
      "command": { "type": "string", "required": true, "description": "Command to execute...", "example": "echo hello | tr a-z A-Z" },
      "shell":   { "type": "string", "default": "auto", "enum": ["auto", "sh", "bash", "pwsh", "cmd"] },
      "timeout": { "type": "integer", "description": "Timeout in seconds (0 or omit for no timeout)" },
      "workingDir": { "type": "string", "description": "Working directory for command execution" },
      "env":     { "type": "", "description": "Environment variables to set (key-value pairs)" },
      "args":    { "type": "array", "description": "Additional arguments appended to the command" },
      "stdin":   { "type": "string", "description": "Standard input to provide to the command" }
    }
  },
  "outputSchemas": { "action": { "properties": { "stdout": {...}, "stderr": {...}, "exitCode": {...}, "success": {...} } } },
  "examples": [{ "name": "Simple command execution", "yaml": "..." }],
  "cliUsage": ["scafctl run provider exec --input command=echo hello | tr a-z A-Z"]
}
```

The `required: true` field on each property makes it immediately clear which inputs are mandatory — the AI doesn't need to cross-reference a separate `required` array. The `provider://reference` resource also provides a compact overview of all providers' required inputs at once.

### Example: Schema and Examples (AI-Assisted Authoring)

These tools help AI agents write correct solution YAML by giving them access to the full schema and real examples.

#### Get the Solution Schema

> **You:** "What fields are valid in a solution YAML file?"

The AI calls `get_solution_schema` and receives the full JSON Schema for the solution format — every field, type, validation rule, and description. This prevents the AI from inventing fields that don't exist.

You can also ask about a specific section:

> **You:** "What goes in the metadata section of a solution?"

The AI calls `get_solution_schema` with `field: "metadata"` and returns just the metadata schema, showing fields like `name`, `version`, `description`, `tags`, etc. with their validation constraints.

#### Explain a Kind

> **You:** "What fields does a resolver have?"

The AI calls `explain_kind` with `kind: "resolver"` and returns a structured breakdown of all resolver fields, including nested types, documentation, and validation tags.

#### Browse Examples

> **You:** "Show me example action configurations"

The AI calls `list_examples` with `category: "actions"` to see all available action examples, then calls `get_example` with a specific path to read the example content. This gives it real, tested patterns to reference when writing new YAML.

> **You:** "Show me how to set up a solution with parameters and validation"

The AI calls `get_example` with `path: "solutions/email-notifier/solution.yaml"` to get a practical reference, then uses `get_solution_schema` to verify the structure.

## 7. Available Tools Reference

| Tool | Description |
|------|-------------|
| `auth_status` | Check auth handler status (configured, token validity, expiry). Auth handlers manage authentication identity — they are not solution providers |
| `catalog_list` | List catalog entries filtered by kind (`solution`, `provider`, `auth-handler`) and name |
| `diff_solution` | Compare two solution files structurally — shows added, removed, and changed resolvers, actions, metadata, and tests |
| `evaluate_cel` | Evaluate a CEL expression with inline data, variables, or file-based context |
| `evaluate_go_template` | Evaluate a Go template against provided data, returning rendered output and referenced fields |
| `explain_kind` | Explain any registered kind (solution, resolver, action, etc.) — shows all fields, types, descriptions, and validation tags |
| `explain_lint_rule` | Get a detailed explanation of a lint rule — description, severity, category, why it matters, how to fix it, and examples |
| `get_example` | Read the contents of a scafctl example file. Use `list_examples` first to find available examples |
| `get_provider_schema` | Get comprehensive provider info: input schema (with per-property required), output schemas, examples, CLI usage |
| `get_solution_schema` | Get the full JSON Schema for the solution YAML file format. Optionally drill into a specific field (e.g., `metadata`, `spec`) |
| `get_run_command` | Get the exact CLI command to run a solution (determines run solution vs run resolver) |
| `inspect_solution` | Full solution metadata — resolvers, actions, tags, links, maintainers |
| `lint_solution` | Validate a solution YAML file and return structured findings |
| `list_cel_functions` | List CEL functions — custom scafctl functions, built-in, or by name |
| `list_examples` | List available scafctl example files with category filtering (solutions, resolvers, actions, providers, etc.) |
| `list_providers` | List all providers with capability and category filtering |
| `list_solutions` | List solutions from the local catalog with name filtering |
| `preview_action` | Preview what each action in a workflow would do WITHOUT executing — shows materialized inputs, deferred values, phases, and dependencies |
| `preview_resolvers` | Execute a solution's resolver chain and return each resolver's resolved value. Use `resolver` param to focus on a single resolver and its dependencies |
| `render_solution` | Render action, resolver, or action-deps graphs as structured JSON |
| `run_solution_tests` | Execute functional tests defined in a solution and return structured results. Use `verbose=true` for full assertion details |
| `scaffold_solution` | Generate a complete skeleton solution YAML from parameters — name, description, features, and providers |
| `validate_expression` | Syntax-check a CEL expression or Go template without executing it — returns validity, errors, and referenced fields |

## 8. Available Resources

MCP resources are read-only data endpoints that AI agents can fetch on demand:

| Resource URI | Description |
|-------------|-------------|
| `solution://{name}` | Raw YAML content of a solution |
| `solution://{name}/schema` | JSON Schema for a solution's input parameters |
| `solution://{name}/graph` | Resolver dependency graph with execution tiers, ASCII diagram, and Mermaid diagram |
| `provider://{name}` | Detailed provider info: input schema (with required/optional per property), output schemas, examples, CLI usage |
| `provider://reference` | Compact reference of all providers with required/optional inputs, capabilities, and descriptions |

## 9. Available Prompts

MCP prompts are predefined templates that guide AI agents through common workflows:

| Prompt | Description | Required Arguments |
|--------|-------------|-------------------|
| `create_solution` | Step-by-step guide for creating a new solution YAML file. Instructs the AI to fetch the schema and examples before writing any YAML. | `name`, `description` |
| `debug_solution` | Systematic debugging workflow that inspects, lints, checks schema, and renders dependency graphs. | `path` |
| `add_resolver` | Guide for adding a resolver using a specific provider. Fetches provider schema and resolver examples. | `provider` |
| `add_action` | Guide for adding an action to a solution's workflow. Shows all action features (retry, forEach, when, etc.). | `provider` |
| `update_solution` | Guide for modifying an existing solution. Follows inspect → edit → lint → preview → test workflow. | `path`, `change` |
| `add_tests` | Guide for writing functional tests for a solution. Covers test schema, assertions, and test patterns. | `path` |
| `compose_solution` | Guide for designing a multi-file composed solution with partial YAML files that get merged at build time. | `path`, `goal` |

### Using Prompts

In most AI clients, prompts appear as suggested conversation starters or can be invoked explicitly. For example, in VS Code with Copilot, you might see a prompt suggestion like "Create a new scafctl solution" that fills in the context automatically.

The prompts instruct the AI to:
1. **Fetch the schema first** — ensuring it uses correct field names and structure
2. **Check available providers** — so it picks real providers, not invented ones
3. **Reference examples** — for practical patterns and best practices
4. **Follow validation rules** — respecting field constraints like `maxLength`, `pattern`, etc.

## 10. Debugging

### Check Server Capabilities

Print the full tool list and verify the server starts correctly:

```bash
scafctl mcp serve --info
```

### Enable File Logging

When the MCP server runs over stdio, application logs go to stderr by default. For persistent logs, use `--log-file`:

```bash
scafctl mcp serve --log-file /tmp/scafctl-mcp.log
```

Then tail the log in another terminal:

```bash
tail -f /tmp/scafctl-mcp.log
```

### Test with Raw JSON-RPC

You can test the server manually by sending JSON-RPC messages:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}' | scafctl mcp serve
```

### Debug Logging

Enable verbose logging to see tool calls and responses:

```bash
scafctl mcp serve --log-level debug --log-file /tmp/scafctl-mcp-debug.log
```

## 11. Troubleshooting

### "scafctl: command not found"

The AI client cannot find the `scafctl` binary. Ensure it's on your `$PATH`:

```bash
which scafctl
```

If using a non-standard install location, use the full path in your MCP configuration:

```json
{
  "servers": {
    "scafctl": {
      "type": "stdio",
      "command": "/usr/local/bin/scafctl",
      "args": ["mcp", "serve"]
    }
  }
}
```

### Tools Not Appearing in AI Client

1. Verify the server starts correctly: `scafctl mcp serve --info`
2. Check your MCP config file syntax (valid JSON, correct file location)
3. Reload/restart the AI client after configuration changes
4. Check the AI client's output panel for MCP connection errors

### "solution has no workflow defined"

If you see this error when running `scafctl run solution`:

```
Error: solution "my-solution" has no workflow defined; use 'scafctl run resolver' to execute resolvers without actions
```

This means the solution contains only resolvers and no `spec.workflow` section. Use `scafctl run resolver` instead:

```bash
# For solutions with ONLY resolvers (no spec.workflow):
scafctl run resolver -f ./my-solution.yaml -r key=value

# For solutions WITH actions (spec.workflow.actions):
scafctl run solution -f ./my-solution.yaml -r key=value
```

The `create_solution` MCP prompt instructs the AI to suggest the correct command based on whether the generated solution includes a workflow.

### Authentication Errors

If a tool returns an auth error, set up authentication before starting the server:

```bash
# Authenticate first
scafctl auth login <provider>

# Then start the server (or let the AI client start it)
scafctl mcp serve
```

The MCP server inherits the same auth context as the CLI. If `scafctl run solution` works with your credentials, the MCP server will too.

### Config Not Found

The MCP server uses the same config file as the CLI (`~/.scafctl/config.yaml` by default). If you use a custom config location:

```json
{
  "servers": {
    "scafctl": {
      "type": "stdio",
      "command": "scafctl",
      "args": ["mcp", "serve", "--config", "/path/to/config.yaml"]
    }
  }
}
```

### Proxy Configuration

If you are behind a corporate proxy, ensure proxy environment variables are available to the MCP server process. In VS Code, you can pass environment variables in the MCP configuration:

```json
{
  "servers": {
    "scafctl": {
      "type": "stdio",
      "command": "scafctl",
      "args": ["mcp", "serve"],
      "env": {
        "HTTP_PROXY": "http://proxy.example.com:8080",
        "HTTPS_PROXY": "http://proxy.example.com:8080",
        "NO_PROXY": "localhost,127.0.0.1,.local"
      }
    }
  }
}
```

## Next Steps

- [Provider Reference](provider-reference.md) — Complete documentation for all built-in providers
- [CEL Expressions Tutorial](cel-tutorial.md) — Master CEL expressions used in solutions
- [Catalog Tutorial](catalog-tutorial.md) — Manage your local solution catalog
- [Authentication Tutorial](auth-tutorial.md) — Set up auth for cloud providers
