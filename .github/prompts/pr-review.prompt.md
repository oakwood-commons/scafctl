---
description: "scafctl: Fetch and triage PR review comments for the current branch. Analyzes comments, fixes issues, and responds/resolves threads via gh CLI."
agent: "pr-reviewer"
argument-hint: "Optional: PR number or leave blank to use current branch"
---
Address unresolved PR review comments. Use `gh` CLI and the **GitHub GraphQL API (v4)** to fetch review threads -- the REST API does not expose the `isResolved` field.

Follow these phases **in order** -- do not skip ahead:

1. **Fetch**: Fetch all review threads via GraphQL; **skip comments that are already resolved**
2. **Triage**: For each unresolved comment, assess whether it's a legit problem with the code. Present the triage summary with recommendations and **stop here** -- the user will click "Apply fixes" to approve
3. **Apply fixes**: Fix the code, verify build (`go build ./...`, `go vet ./...`), run tests (`task test:e2e`), then respond to and resolve each addressed thread
4. If you disagree with a comment: **discuss it with me before deciding** -- don't resolve or dismiss it
5. **Do not commit** -- I will handle that
