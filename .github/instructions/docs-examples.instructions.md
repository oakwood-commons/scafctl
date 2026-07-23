---
description: "Documentation and examples conventions for scafctl. Use when creating or updating docs, tutorials, or examples."
applyTo: "{docs,examples,pkg/docs,pkg/examples}/**"
---

# Documentation & Examples

- Use `pkg/docs/` for generating and managing documentation and tutorials
- Use `pkg/examples/` for storing example configurations and usage scenarios
- Always create documentation, tutorials, and examples for new features, providers, and commands
- Always update documentation, tutorials, and examples when features, providers, or commands change
- Always update MCP server tools (if applicable) when features, providers, or commands change
- Reader-facing docs must stand alone -- website and checkout readers cannot
  invoke MCP tools, so keep committed reference material complete. Use the MCP
  tools (`get_solution_schema` / `explain_kind`, `list_cel_functions` /
  `list_go_template_functions`, `list_providers` / `get_provider_schema`,
  `list_lint_rules` / `explain_lint_rule`, `explain_concepts`,
  `list_context_variables`) to **verify or
  generate** that reference rather than as the sole reader-facing source.
