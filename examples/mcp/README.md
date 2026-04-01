# MCP Server Examples

Example configurations for connecting AI clients to scafctl's MCP server.

## Quick Start

1. Copy the appropriate config file for your AI client
2. Ensure `scafctl` is on your `$PATH`
3. Restart the AI client
4. Start chatting — the AI will discover and use scafctl tools automatically

## VS Code / GitHub Copilot

Copy `vscode-mcp.json` to `.vscode/mcp.json` in your project root:

```bash
mkdir -p .vscode
cp examples/mcp/vscode-mcp.json .vscode/mcp.json
```

Reload VS Code and open Copilot chat. See [vscode-mcp.json](vscode-mcp.json).

## Claude Desktop

Merge the content of `claude-desktop-config.json` into your Claude Desktop config:

- **macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

See [claude-desktop-config.json](claude-desktop-config.json).

## Cursor

Copy `cursor-mcp.json` to `.cursor/mcp.json` in your project root:

```bash
mkdir -p .cursor
cp examples/mcp/cursor-mcp.json .cursor/mcp.json
```

Restart Cursor. See [cursor-mcp.json](cursor-mcp.json).

## Windsurf

Copy `windsurf-mcp.json` to `.windsurf/mcp.json` in your project root:

```bash
mkdir -p .windsurf
cp examples/mcp/windsurf-mcp.json .windsurf/mcp.json
```

Restart Windsurf. See [windsurf-mcp.json](windsurf-mcp.json).

## Tutorial Solution

The `tutorial-solution.yaml` is a sample solution used in the [MCP Server Tutorial](../../docs/tutorials/mcp-server-tutorial.md). Copy it to follow along:

```bash
mkdir -p mcp-demo
cp examples/mcp/tutorial-solution.yaml mcp-demo/solution.yaml
```

Then try asking your AI agent: *"Inspect mcp-demo/solution.yaml"* or *"Lint mcp-demo/solution.yaml"*.

## Verifying the Connection

After configuring your AI client, verify the MCP server is working:

1. Check that tools are available: `scafctl mcp serve --info`
2. Ask the AI: *"List my available providers"*
3. The AI should invoke the `list_providers` tool and return results

## Debugging

Enable file logging for troubleshooting:

```json
{
  "servers": {
    "scafctl": {
      "type": "stdio",
      "command": "scafctl",
      "args": ["mcp", "serve", "--log-file", "/tmp/scafctl-mcp.log"]
    }
  }
}
```

## Available Tools

| Tool | Description |
|------|-------------|
| `auth_status` | Check auth provider status |
| `catalog_list` | List catalog entries by kind and name |
| `evaluate_cel` | Evaluate CEL expressions with data |
| `get_provider_schema` | Get provider JSON Schema |
| `inspect_solution` | Full solution metadata |
| `lint_solution` | Validate solution files |
| `list_cel_functions` | List CEL functions |
| `list_providers` | List providers with filtering |
| `list_solutions` | List catalog solutions |
| `render_solution` | Render action/resolver graphs |
| `get_openapi_spec` | Generate full OpenAPI specification for the REST API |
| `list_api_endpoints` | List all REST API endpoints with method, path, and summary |

For the full tutorial, see the [MCP Server Tutorial](../../docs/tutorials/mcp-server-tutorial.md).
