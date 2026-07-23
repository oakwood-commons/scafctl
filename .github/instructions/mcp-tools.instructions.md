---
description: "MCP tool handler rules for scafctl. Handlers are thin wrappers -- no business logic. Parse inputs, call domain packages, format output. Use when editing MCP tool files."
applyTo: "pkg/mcp/tools_*.go"
---

# MCP Tool Handlers

MCP tool handlers are **thin wrappers** -- they parse tool inputs, call domain packages, and format results.

## Rules

- **No business logic** -- delegate to packages in `pkg/`
- Register tools in `register*Tools()` methods on `*Server`
- Use `mcp.NewTool()` with descriptive names, descriptions, and typed parameters
- Use `mcp.With*HintAnnotation()` for tool metadata (read-only, destructive, idempotent)
- Return `mcp.NewToolResultText()` or `mcp.NewToolResultError()` -- never panic
- Always add Huma-style parameter descriptions and constraints
- Tool descriptions and server name must use the configured binary name (`s.name`), not hardcoded "scafctl"

## Before Adding a Tool

Confirm the capability is not already covered. The server already exposes broad
reference tools -- `explain_concepts`, `explain_kind`, `get_solution_schema`,
`list_providers` / `get_provider_schema`, `list_cel_functions` /
`list_go_template_functions`, `list_lint_rules` / `explain_lint_rule`, and
`list_context_variables`. If a new tool would surface spec / CEL / template /
provider knowledge, prefer extending or pointing at those existing tools over a
bespoke handler.
