---
description: "scafctl: Run the issue definition-of-done gate -- code review (go-reviewer) then completeness/artifact audit (artifact-auditor) over the current changes, resolve findings, and summarize. Runs automatically at the end of issue work; can also be invoked manually."
agent: build
---
Run the local "Issue Definition of Done" quality gate over the current
uncommitted changes. This wraps the steps that must run automatically at the end
of any issue-driven change, before proposing a commit. Do NOT ask the user for
permission to run these steps -- just run them and report.

$ARGUMENTS

## Phase 1: Preconditions

1. Confirm code changes exist: `git status --short` and `git diff --stat`.
   If there are no changes, report that and stop.
2. Ensure the build and targeted tests pass before reviewing. Run
   `go build ./...` and `go test -race` on the changed packages. If they fail,
   fix them first (or hand off to `go-fixer`) before continuing.

## Phase 2: Code review (go-reviewer)

Dispatch the `go-reviewer` subagent over the current diff (this is the
`/go-review` flow). Require it to complete ALL phases -- automated checks,
systematic per-file review, adversarial analysis, cross-file consistency,
coverage analysis, and self-review. Collect findings by severity.

## Phase 3: Completeness / artifact audit (artifact-auditor)

Dispatch the `artifact-auditor` subagent (this is the `/completeness-check`
flow) over the staged-or-changed set. Confirm the change has:
- Tests in the corresponding `*_test.go` files (70%+ patch coverage target)
- Benchmarks for performance-sensitive code
- Examples in `examples/` if user-facing
- Updated docs / doc comments on exported types
- Integration tests (CLI, solution, or API per the change type)
- MCP tool / provider descriptor updates if applicable

## Phase 4: Resolve findings

- Address every CRITICAL and HIGH finding, and any real MEDIUM, before proposing
  a commit. For substantial fixes, hand off to the `go-fixer` subagent.
- After fixing, re-validate: `go build ./...`, `go test -race` on changed
  packages, and any targeted integration tests. Run the full `task integration`
  (or `task test:e2e`) once if the change is broad.
- Note any accepted LOW/INFO items with a short justification.
- **Ask when a fix is not obvious.** Fix routine, unambiguous findings silently,
  but pause and prompt the user when a finding involves a real judgment call --
  a design tradeoff, a scope decision, a breaking-change choice, conflicting
  conventions, or two reasonable approaches with different implications. Offer
  concrete options and a recommendation. When in doubt whether it is "obvious,"
  ask.

## Phase 4.5: Lint the final diff (mandatory before commit)

Run `task lint:changed` on the FINAL diff and confirm **no new issues** in the
changed code. Do this every time, right before proposing a commit -- an earlier
lint run does not count, because the review/fix edits above can introduce new
findings. `task lint` always exits non-zero due to pre-existing issues in
untouched files; `task lint:changed` reports only issues this branch introduced
(vs. the merge base), so a clean result there is the gate. Fix anything it
reports before committing; never rely on CI to catch a lint failure.

## Phase 5: Summarize

Report: review outcome (findings by severity), what was fixed, coverage status,
and test results.

Prompt the user whenever a genuine judgment call surfaces -- an ambiguous or
unresolved point in the plan/issue, or a review/completeness finding without an
obvious fix (design tradeoff, scope, breaking change, conflicting conventions).
The "no questions" rule applies ONLY to asking permission to run this gate; it
does NOT suppress real decisions. Do not commit or push (that remains a separate,
approval-gated step).

Note: `/pr-review` is intentionally NOT part of this gate. The user runs it
themselves after the branch is pushed and a GitHub PR exists, because it fetches
PR review threads, CI status, and Codecov data. Do not auto-run it here.
