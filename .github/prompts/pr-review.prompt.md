---
description: "Fetch and triage PR review comments for the current branch. Analyzes comments, fixes issues, and responds/resolves threads via gh CLI."
agent: "pr-reviewer"
argument-hint: "Optional: 'resolve' to also respond and resolve addressed threads"
---
Fetch review comments from the PR for the current branch and triage them:

1. Get current branch and find the matching PR via `gh pr view`
2. Fetch all unresolved review threads via GitHub GraphQL API
3. Classify each thread: actionable, question, nit, already addressed, disagree, outdated
4. Present the triage summary and wait for approval
5. Fix actionable items, then respond to and resolve threads

**Always waits for approval before making changes or responding to comments.**
