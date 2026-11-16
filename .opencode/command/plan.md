---
description: "scafctl: Create an implementation plan for a scafctl feature. Produces a structured blueprint with architecture decisions, task breakdown, interface design, and testing strategy."
agent: planner
---
Create a structured implementation blueprint for the described feature:

1. **Summary** -- What and why
2. **Architecture decisions** -- Layers affected, new types, interface changes
3. **Task breakdown** -- Ordered steps with files, complexity, dependencies
4. **Interface design** -- Define contracts first
5. **Error handling** -- Sentinel errors, wrapping strategy
6. **Testing strategy** -- Unit tests, benchmarks, integration tests (CLI, solution, API)
7. **Documentation** -- Docs, examples, MCP tools, tutorials
8. **Risks & edge cases** -- What could go wrong

Follow scafctl conventions (see `AGENTS.md`): provider-based architecture,
CEL/Go templates, Writer for output, kvx for data, thin CLI/MCP/API layers,
embedder contract.

Before planning, run the issue triage/validation gate (confirm the issue is
legitimate and still relevant, treat the reporter's suggested implementation
as an unverified hint, and choose the best solution -- breaking changes
allowed).
