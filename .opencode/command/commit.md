---
description: "scafctl: Generate a conventional commit message from staged or recent changes. Outputs the message only -- does not run git commit."
agent: commit-message
---
Analyze the current changes and generate a conventional commit message. **Output the message only -- do not stage or commit code.**

1. Check `git diff --cached --stat` for staged changes (fall back to `git diff --stat`)
2. Read the actual diff to understand what changed
3. If asked to amend, check `git log -1` for the last commit
4. Generate a message: `<type>(<scope>): <description>` + body with bullet points summarizing key changes
5. Output the raw message in a code block for the user to copy
6. Also output the ready-to-run command: `git commit -s -S -m "<COMMIT_MESSAGE>"`
7. Check the current branch with `git rev-parse --abbrev-ref HEAD`. If the user is on the default branch (`main`), ask whether they would like to create a feature branch for these changes. If they say yes, create a new feature branch (`git checkout -b <branch-name>`) using a descriptive name derived from the change.

**Do not stage (`git add`) or commit (`git commit`) code.** Only creating a feature branch on request is permitted.

Always include a body unless the change is a single trivial file edit.
Types: feat, fix, docs, perf, refactor, style, test, chore, ci, revert
Add `!` and `BREAKING CHANGE:` footer for breaking changes.
