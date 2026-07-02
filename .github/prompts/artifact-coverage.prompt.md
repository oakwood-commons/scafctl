---
description: "scafctl: Verify staged changes have updated/added docs, tutorials, examples, solution integration tests, and MCP server tools."
agent: "agent"
argument-hint: "Optional: specific feature, provider, or command to scope the check"
---
Determine whether the staged changes have corresponding docs, tutorials,
examples, solution integration tests, and MCP server tools. Report gaps only --
do not create or modify any files.

## Steps

1. **Identify staged changes**
   - Run `git diff --cached --name-only` and `git diff --cached --stat`.
   - If nothing is staged, fall back to `git log origin/main..HEAD --stat` to
     inspect pushed commits on the current branch.
   - If an argument scopes the check (a feature, provider, or command), limit
     the analysis to files matching that scope.

2. **Classify each change** into what it needs. Map source changes to their
   expected supporting artifacts:
   - New/changed provider (`pkg/provider/**`) -> docs, example, solution
     integration test, MCP schema exposure.
   - New/changed CLI command (`pkg/cmd/scafctl/**`) -> docs, CLI integration
     test, `--help` coverage.
   - New/changed feature or behavior -> docs, tutorial (if user-facing),
     example.

3. **Check for supporting artifacts**
   - **Docs**: `docs/` and `pkg/docs/`.
   - **Tutorials**: `docs/tutorials/` (required for user-facing features).
   - **Examples**: `examples/` (matching solution YAML or usage example).
   - **Solution integration tests**: `tests/integration/solutions/`.
   - **CLI integration tests**: `tests/integration/cli_test.go`.
   - **API integration tests**: `tests/integration/api_test.go`.
   - **MCP server tools**: `pkg/mcp/tools_*.go` -- confirm new providers,
     commands, or capabilities are surfaced through the relevant MCP tool and
     its schema/output shape.

4. **Report** a checklist grouped by change, marking each artifact as
   Present, Updated, or Missing. Call out anything that is user-facing but
   lacks a tutorial or example. Keep it concise -- a table or bulleted
   checklist, not prose.

5. **Audit only by default.** Do not create or modify any files while
   producing the report.

6. **Offer to implement the gaps.** End the response with a short numbered
   list of the missing artifacts, then ask the user to confirm before making
   any changes, e.g.:

   > Reply **"implement gaps"** (or list specific numbers) and I'll create the
   > missing docs, tutorials, examples, integration tests, and MCP coverage.

   Phrase this as an explicit follow-up question so VS Code Chat surfaces it as
   a one-click suggestion. Only after the user confirms should you implement
   the selected gaps -- then re-run the checklist to verify each is resolved.

7. **Never stage or commit.** Do not run `git add`, `git commit`, `git stash`,
   or any command that modifies the git index or history -- neither during the
   audit nor when implementing gaps. Leave all new and modified files unstaged
   in the working tree so the user reviews and stages them. When re-running the
   checklist after implementing gaps, inspect the working tree (e.g.
   `git status`, `git diff`, or read the files directly) rather than staging
   changes to verify them.
