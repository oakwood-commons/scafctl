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
