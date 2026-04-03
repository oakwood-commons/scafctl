---
description: "Fetch and triage PR review comments for the current branch. Analyzes comments, fixes issues, and responds/resolves threads via gh CLI."
agent: "pr-reviewer"
argument-hint: "PR URL (e.g., https://github.com/oakwood-commons/scafctl/pull/123)"
---
Address unresolved PR review comments. Use `gh` CLI and the **GitHub GraphQL API (v4)** to fetch review threads — the REST API does not expose the `isResolved` field.

1. Fetch all review threads via GraphQL; **skip comments that are already resolved**
2. For each unresolved comment, assess whether it's a legit problem with the code
3. If it needs fixing: make the fix, reply to the thread confirming it's fixed, and mark it resolved
4. If you disagree with a comment: **discuss it with me before deciding** — don't resolve or dismiss it
5. After all changes are made, run `task test:e2e` and make sure everything passes
6. **Do not commit** — I will handle that
