---
description: "Check if staged changes have corresponding docs, tutorials, examples, integration tests, and MCP tools."
agent: "agent"
argument-hint: "Optional: specific area to check"
---
Review staged changes and check if supporting artifacts exist:

1. Run `git diff --cached --stat` to identify what changed
2. For each feature, provider, or command, verify:
   - Docs in `docs/` or `pkg/docs/`
   - Tutorials in `docs/tutorials/` for user-facing features
   - Examples in `examples/`
   - Solution integration tests in `tests/integration/solutions/`
   - CLI integration tests in `tests/integration/cli_test.go`
   - API integration tests in `tests/integration/api_test.go`
   - MCP server tools in `pkg/mcp/tools_*.go`
3. Report present vs missing as a checklist
4. Do not create anything, just report the gaps
