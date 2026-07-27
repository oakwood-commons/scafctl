---
description: "scafctl: File a GitHub issue with codebase exploration and feasibility assessment."
agent: scafctl-issue-creator
---
Create a GitHub issue for the described change. Follow this process:

1. **Explore** the codebase for relevant files, patterns, and interfaces
2. **Assess** feasibility, scope (XS/S/M/L/XL), risks, and affected areas
3. **Wait** for user confirmation before creating anything
4. **Create** the issue via `gh issue create` with appropriate title and structured body

When an issue is later implemented, the "Issue Definition of Done" gate runs
automatically before any commit: `go-reviewer` then `artifact-auditor` (the
`/finish-issue` flow), resolving findings without asking the user for permission.
See `.github/copilot-instructions.md`.
