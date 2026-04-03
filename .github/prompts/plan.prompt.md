---
description: "scafctl: Create an implementation plan for a scafctl feature. Produces a structured blueprint with architecture decisions, task breakdown, interface design, and testing strategy."
agent: "planner"
argument-hint: "Describe the feature to plan (e.g., 'Add OAuth auth handler')"
---
Create a structured implementation blueprint for the described feature:

1. **Summary** — What and why
2. **Architecture decisions** — Layers affected, new types, interface changes
3. **Task breakdown** — Ordered steps with files, complexity, dependencies
4. **Interface design** — Define contracts first
5. **Error handling** — Sentinel errors, wrapping strategy
6. **Testing strategy** — Unit tests, benchmarks, integration tests (CLI, solution, API)
7. **Documentation** — Docs, examples, MCP tools, tutorials
8. **Risks & edge cases** — What could go wrong

Follow scafctl conventions: provider-based architecture, CEL/Go templates, Writer for output, kvx for data.
