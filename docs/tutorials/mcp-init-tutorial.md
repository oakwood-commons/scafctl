---
title: "MCP Harness Setup (mcp init)"
weight: 66
---

# MCP Harness Setup (`scafctl mcp init`)

`scafctl mcp init` wires `scafctl mcp serve` and an embedded Agent Skills authoring guide (`SKILL.md`) into AI coding harnesses in a single idempotent command.

Pairing both artifacts solves two setup problems at once:

1. **Zero-hand-editing MCP registration**: Updates JSON and JSONC configuration files in place while preserving existing comments, key order, and neighbor MCP servers.
2. **Turn-1 authoring guidance**: Because modern AI harnesses lazy-load MCP tool schemas, installing a concise `SKILL.md` gives the assistant upfront design judgment right from turn 1 -- when to author `actions.yaml` vs `solution.yaml`, `explicit: false` vs `explicit: true` actions, cross-platform `mvdan/sh` rules, and running lint, tests, and bundle dry-run checks before finishing.

## Quick Start

Run `scafctl mcp init` in the root of your repository:

~~~shell
scafctl mcp init
~~~

When `--target` and `--skill-dir` are omitted, `scafctl` inspects workspace markers (`.vscode`, `.github/skills`, `.github/copilot-instructions.md`, `.mcp.json`, `.claude`, `CLAUDE.md`, `.cursor`, `.cursorrules`, `opencode.jsonc`, `opencode.json`, `.opencode`, `.agents`) and configures every detected harness, defaulting to `vscode` when none are present.

Re-running `scafctl mcp init` is safe and idempotent: files whose desired MCP entry and `SKILL.md` content already match are reported as `unchanged` and skipped.

## Supported Harnesses and Paths

| Target (`--target`) | Project MCP Config (`--scope project`) | Project Skill | User MCP Config (`--scope user`) | User Skill |
| --- | --- | --- | --- | --- |
| `vscode` | `.vscode/mcp.json` | `.github/skills/scafctl/SKILL.md` | macOS: `~/Library/Application Support/Code/User/mcp.json`<br>Windows: `%APPDATA%\Code\User\mcp.json`<br>Linux: `$XDG_CONFIG_HOME/Code/User/mcp.json` | `~/.copilot/skills/scafctl/SKILL.md` |
| `claude` | `.mcp.json` | `.claude/skills/scafctl/SKILL.md` | `~/.claude.json` | `~/.claude/skills/scafctl/SKILL.md` |
| `cursor` | `.cursor/mcp.json` | `.cursor/skills/scafctl/SKILL.md` | `~/.cursor/mcp.json` | `~/.cursor/skills/scafctl/SKILL.md` |
| `opencode` | `opencode.jsonc` (or existing `opencode.json`) | `.opencode/skills/scafctl/SKILL.md` | `$XDG_CONFIG_HOME/opencode/opencode.jsonc` (default `~/.config/opencode/opencode.jsonc`, or existing `.json`) | `$XDG_CONFIG_HOME/opencode/skills/scafctl/SKILL.md` (default `~/.config/opencode/skills/scafctl/SKILL.md`) |
| `agy` | `.agents/mcp_config.json` | `.agents/skills/scafctl/SKILL.md` | `~/.gemini/config/mcp_config.json` (or existing `~/.gemini/antigravity/mcp_config.json`) | `~/.gemini/config/skills/scafctl/SKILL.md` (or existing `~/.gemini/antigravity/skills/scafctl/SKILL.md`) |

## Common Workflows

### Configure Specific Harnesses or All Harnesses

~~~shell
# Configure VS Code Copilot and Claude Code in the current repository
scafctl mcp init --target vscode --target claude

# Configure every supported harness at once
scafctl mcp init --target all
~~~

### Configure User-Level Profile (`--scope user`)

Install the MCP server and authoring skill once in your user profile so they are available across every workspace:

~~~shell
scafctl mcp init --target cursor --scope user
~~~

### Preview Changes and Create Backups (`--dry-run`, `--backup`)

~~~shell
# Preview which files would be created or updated without touching disk
scafctl mcp init --dry-run

# Save a .bak backup alongside any existing file before updating it
scafctl mcp init --backup

# Emit structured JSON or YAML output for automation
scafctl mcp init --dry-run -o json
~~~

### MCP Config Only (`--no-skill`)

If you want to register `scafctl mcp serve` without installing `SKILL.md`:

~~~shell
scafctl mcp init --target vscode --no-skill
~~~

### Other Editors (`--print` and `--skill-dir`)

For editors outside the built-in target list (such as Windsurf, Zed, Cline, or Claude Desktop), use `--print` to emit the standard `mcpServers` stdio JSON block and `SKILL.md` to stdout, and `--skill-dir` to install `SKILL.md` into any custom Agent Skills directory:

~~~shell
# Print standard MCP JSON config + SKILL.md to stdout
scafctl mcp init --print

# Print only the raw JSON config block
scafctl mcp init --print --no-skill

# Install SKILL.md under .windsurf/skills/scafctl/SKILL.md
scafctl mcp init --skill-dir .windsurf/skills
~~~

## Safety Guarantees

- **Comment-preserving JSONC updates**: Existing `//` and `/* */` comments, URLs (`https://...`), trailing commas, property ordering, and neighbor MCP servers remain intact.
- **Fail-closed on malformed configs**: If an existing JSON/JSONC file has syntax errors, `scafctl mcp init` aborts without modifying the file.
- **Scoped atomic writes**: Files are written atomically with `0600` permissions (`0700` for new directories) and symbolic links are rejected.

## Embedder Customization

CLIs embedding `scafctl` automatically substitute their configured `RootOptions.BinaryName` into the MCP server key, command (`["<binary>", "mcp", "serve"]`), and skill instructions, and derive a normalized Agent Skills identifier (`initconfig.SkillName`: lowercase letters, digits, and single hyphens, up to 64 characters) for the `SKILL.md` frontmatter `name` and `skills/<skill-name>/SKILL.md` directory. Embedders can also append custom markdown guidance via `RootOptions.MCPSkillSupplement`:

~~~go
rootCmd, cleanup := scafctl.Root(&scafctl.RootOptions{
	BinaryName: "mycli",
	MCPSkillSupplement: "## Platform Conventions\n\nRun `mycli auth status` before deploying.",
})
defer cleanup()
~~~

## Related Documentation

- [MCP Server Tutorial](mcp-server-tutorial.md) -- Explore MCP tools, prompts, transports, and interactive workflows
- [MCP Examples](https://github.com/oakwood-commons/scafctl/blob/main/examples/mcp/README.md) -- Ready-to-copy MCP configuration snippets
