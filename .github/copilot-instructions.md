# scafctl - AI Agent Instructions

scafctl is a Go-based CLI tool using CEL (Common Expression Language) for
dynamic configuration evaluation and template processing.

`AGENTS.md` at the repo root is the canonical guidance for authors and tools:
build/test/lint commands, key patterns, business-logic placement, the
embedder contract, Go/testing conventions, and per-area rules. On-demand
skills live under `.github/skills/`. The MCP server shipped in the binary
(`./dist/scafctl mcp serve`, wired in `opencode.json` / `.vscode/mcp.json`)
exposes the authoritative spec, function, provider, and lint-rule catalogs.

<!-- copilot-review:start -->
For pull request code review, follow the review contract in [`REVIEW.md`](../REVIEW.md) at the repo root. It is the single source of truth; do not duplicate it here.
<!-- copilot-review:end -->
