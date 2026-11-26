# MCP Server Examples

Example configurations for connecting AI clients to scafctl's MCP server.

## Automated Setup (`scafctl mcp init`)

For supported AI harnesses (`vscode`, `claude`, `cursor`, `opencode`, `agy`), run `scafctl mcp init` to register `scafctl mcp serve` (preserving existing JSON/JSONC comments and neighbor MCP servers) and install the official `scafctl` authoring `SKILL.md` in one step:

~~~bash
# Auto-detect active harnesses in the current repository
scafctl mcp init

# Configure specific harnesses or your user-level profile
scafctl mcp init --target vscode --target claude
scafctl mcp init --target cursor --scope user

# Print generic MCP JSON + SKILL.md for any other editor, or write a custom skill path
scafctl mcp init --print
scafctl mcp init --skill-dir .windsurf/skills
~~~

See the [MCP Harness Setup Tutorial](../../docs/tutorials/mcp-init-tutorial.md) for all flags and target paths, or use the manual snippets below.

## Manual Quick Start

1. Copy the appropriate config file for your AI client
2. Ensure `scafctl` is on your `$PATH`
3. Restart the AI client
4. Start chatting -- the AI will discover and use scafctl tools automatically

## VS Code / GitHub Copilot

Run `scafctl mcp init --target vscode`, or copy `vscode-mcp.json` to `.vscode/mcp.json` in your project root:

```bash
mkdir -p .vscode
cp examples/mcp/vscode-mcp.json .vscode/mcp.json
```

Reload VS Code and open Copilot chat. See [vscode-mcp.json](vscode-mcp.json).

## Claude Code & Claude Desktop

For Claude Code CLI, run `scafctl mcp init --target claude` (writes `.mcp.json` + `.claude/skills/scafctl/SKILL.md`, or `~/.claude.json` with `--scope user`).

For Claude Desktop, merge the content of `claude-desktop-config.json` into your Claude Desktop config:

- **macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

See [claude-desktop-config.json](claude-desktop-config.json).

## Cursor

Run `scafctl mcp init --target cursor`, or copy `cursor-mcp.json` to `.cursor/mcp.json` in your project root:

```bash
mkdir -p .cursor
cp examples/mcp/cursor-mcp.json .cursor/mcp.json
```

Restart Cursor. See [cursor-mcp.json](cursor-mcp.json).

## OpenCode

Run `scafctl mcp init --target opencode` to configure `opencode.jsonc` (or update an existing `opencode.json`) and `.opencode/skills/scafctl/SKILL.md`.

## Antigravity (AGY)

Run `scafctl mcp init --target agy` (or `scafctl mcp init --target agy --scope user` for `~/.gemini/config/mcp_config.json`, falling back to an existing `~/.gemini/antigravity/mcp_config.json`).

## Windsurf

Copy `windsurf-mcp.json` to `.windsurf/mcp.json` in your project root and optionally install the authoring skill with `scafctl mcp init --skill-dir .windsurf/skills`:

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
| `list_auth_handlers` | List registered auth handlers with config status |
| `auth_list_tokens` | List cached access tokens (metadata only, no secrets) |
| `auth_purge_expired` | Remove expired tokens from the cache |
| `render_solution` | Render action/resolver graphs |
| `get_openapi_spec` | Generate full OpenAPI specification for the REST API |
| `list_api_endpoints` | List all REST API endpoints with method, path, and summary |

For the full tutorials, see the [MCP Harness Setup Tutorial](../../docs/tutorials/mcp-init-tutorial.md) and [MCP Server Tutorial](../../docs/tutorials/mcp-server-tutorial.md).
