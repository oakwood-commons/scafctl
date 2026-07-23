# scafctl - AI Agent Instructions

## Overview
Go-based CLI tool using CEL (Common Expression Language) for dynamic configuration evaluation and template processing.

## Key Patterns

- **CLI Output**: Use `writer.FromContext(ctx)` -- never `fmt.Fprintf` directly. See `pkg/terminal/writer/`
- **Data Output**: Use `kvx.OutputOptions` for structured table/json/yaml/quiet output. See `pkg/terminal/kvx/`. Commands that emit an **array of objects** should attach an interactive display schema via `kvx.WithOutputDisplaySchemaJSON` (a `go:embed`-ed `<cmd>_schema.json` with `x-kvx-list`/`x-kvx-detail` extensions) so `-i` renders a card/detail view; single-object output does not need one. See `.github/instructions/cli-commands.instructions.md`.
- **HTTP Client**: See `pkg/httpc/README.md`
- **Paths**: Use xdg paths via `pkg/paths`
- **Configuration**: `pkg/settings` for defaults, `pkg/config/` for app configuration
- **CEL**: Use `celexp.EvaluateExpression()`. Prefer `expression` field over `expr`

## MCP Tools Are the Source of Truth

scafctl ships an MCP server whose tools return live, version-accurate facts about
the solution spec, functions, providers, and lint rules. When authoring or
reasoning about solutions -- or writing docs/skills -- **consult these tools
instead of relying on (or hand-maintaining) inline catalogs**, which drift:

- **Spec / fields**: `get_solution_schema`, `explain_kind` (solution, resolver,
  action, workflow, spec, provider, schema, retry).
- **Functions**: `list_cel_functions`, `list_go_template_functions`.
- **Providers**: `list_providers`, `list_official_providers`, `get_provider_schema`.
- **Lint**: `list_lint_rules`, `explain_lint_rule`.
- **Concepts / runtime**: `explain_concepts` -- including the `context` category
  (`context-variables`, `phase-execution`, `cel-cost-model`,
  `template-dependency-inference`, `snapshot-masking`, `authoring-workflow`) --
  and `list_context_variables` for the injected-variable-per-phase matrix.
- **Validate before shipping**: `validate_expression`, `evaluate_cel`,
  `evaluate_go_template`, `lint_solution`, `extract_resolver_refs`.

Prose in the skills (`.github/skills/`) should POINT at these tools, not mirror
their output.

## Conventions

- **Commits**: Use [conventional commits](https://www.conventionalcommits.org/en/v1.0.0/#specification)
- **Squash-merge**: PRs are squash-merged, so a PR becomes one commit on `main` built from the **PR title and body** (branch commit messages are discarded; the repo sets `squash_merge_commit_title=PR_TITLE`, `squash_merge_commit_message=PR_BODY`). Do not split a branch into multiple commits for history's sake, put the effort into the PR title/body, and keep unrelated concerns in **separate PRs**. The PR title must be a valid conventional-commit subject. **The PR body IS the final commit message: whenever a new commit changes what the PR does (e.g. a review fix), update the PR body via `gh pr edit` in the same step so the squash commit stays accurate**, and do a final title/body pass before proposing a merge. See `AGENTS.md` for detail.
- **Reviewer loop (Copilot re-reviews every push)**: this repo runs Copilot review on every push (`review_on_push: true`), so each new commit -- including every review fix -- triggers a fresh review. Classify each round: **substantive** findings (correctness, fail-open/guarantee-defeating logic, silent failures, security, missing error handling, 0%-covered new logic) get **fixed** (accept another review); **nitpicks** (comment/naming/doc wording, display-only issues, "consider..." with no behavioral impact, guards for non-existent callers) get a **reply-and-resolve, NOT a new fix commit** (replying does not trigger a new review). When a round is **all nits**, say so with the reasoning, reply-and-resolve them, and -- once CI is green AND the PR's `reviewDecision` is non-blocking (not `CHANGES_REQUESTED`/`REVIEW_REQUIRED`; `main` requires an approving review) -- recommend merging; otherwise ask the user to approve or request re-review rather than implying it is mergeable. Do not keep pushing. See `AGENTS.md`.
- **Signing**: All commits must be GPG/SSH signed (`-S`) and include DCO sign-off (`-s`)
- **Errors**: Return errors with `fmt.Errorf("context: %w", err)`, don't panic
- **Breaking changes**: Allowed -- this app is not in production. Note when doing so.
- **Backward compatibility**: Do not do it, see Breaking changes above.
- **Scratch files**: Use `temp/` for throwaway files (experiments, debug output, drafts). This directory is gitignored and must never be committed.

## Build & Test Commands

```bash
# Build
go build -ldflags "-w -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.BuildVersion=dev -X main.Commit=$(git rev-parse HEAD)" -o dist/scafctl ./cmd/scafctl/scafctl.go

# Test
go test ./...                    # Run all tests

# Linting
task lint                        # Run Linter (uses pinned golangci-lint version)
task lint:fix                    # Run Linter and auto-fix issues
```

The project uses `task` (go-task/task) for builds and linting. **Always use `task lint` instead of running `golangci-lint` directly** to ensure the correct pinned version is used.

## Critical Rules

- **Before implementing an issue, validate it first**: An open issue is not a mandate. Confirm the bug/gap is real against the current code (cite `file:line`) and worth doing before writing any solution. Treat the reporter's "Proposed Fix"/"Files to Modify" as an unverified hint -- reporters may lack codebase access, so validate the actual best location and approach in the code. Aim for the BEST solution, not the easy one (breaking changes are allowed -- see below). Reuse idiomatic scafctl patterns before inventing, and take cues from best-in-class CLIs (`git`, `docker`, `kubectl`, `gh`, `terraform`, `brew`). The `planner` agent codifies this triage gate -- use it for non-trivial issues.
- **Business logic placement**: Never in CLI command packages (`pkg/cmd/scafctl/...`), MCP handler files (`pkg/mcp/tools_*.go`), or API packages -- put it in shared domain packages (`pkg/...`)
- **After any change**: Run `task test:e2e` to ensure everything passes. E2E is slow -- run it **once**, redirect output to a file (`task test:e2e 2>&1 | tee /tmp/e2e-results.txt`), and grep the file to check results. Never re-run just to read output differently.
- **Test coverage**: Every new or changed file must have tests. Target 70%+ patch coverage. Never submit a new file with 0% test coverage
- **No magic values**: Always define constants or use settings for configuration values
- **Git safety**: The AI may commit, push, and open/merge PRs, but must **ask for approval first**; a **single approval covers the whole commit -> push -> PR sequence** (never publish any step without having asked). `git commit --amend` is **denied** and run manually by the user. Before committing, verify a signing key is loaded (`ssh-add -l`); if none is loaded, prompt the user to load their signing key into the ssh-agent per their local setup rather than attempting a doomed commit. All commits must be signed (`-S`) and DCO signed-off (`-s`). **Do not `sleep` to wait for CI**; ask the user whether to wait, and if so loop-poll (`gh pr checks`). See `AGENTS.md` for details.

## Issue Definition of Done (automatic quality gate)

When work originates from a GitHub issue (via `/issue`, the `planner`, or a
direct "fix issue N" request), the following review gate runs **automatically**
as the last phase -- do not ask the user whether to run it, and do not wait to
be asked. Treat it as part of "done," equivalent to a pre-commit hook.

**Checkpoint policy: one stop before commit.** After the plan is authorized,
implement the change, run the automatic gate below, and apply fixes in one
continuous pass. Do NOT pause between implementation, review, and fixing.
Interrupt only to ask a genuine judgment-call question (see "Ask vs. proceed").
Then present a **single consolidated summary** (what changed, review findings and
how they were resolved, coverage, test results) and **ask once for approval to
publish**. That one approval covers the whole `commit` -> `push` -> open-PR
sequence: on approval the AI may run all three without stopping again, unless
something material changed (different files, a failing gate), in which case it
re-asks. It must **never** push or open a PR without having asked
(`/pr-review` is run by the user afterward).

**Do not `sleep` to wait for CI.** After pushing/opening a PR, do not idle on a
long `sleep` waiting for pipeline results -- but do not silently move on either.
**Ask the user whether to wait.** If they want to wait, loop-poll in a bounded
loop (e.g. repeated `gh pr checks` with a short interval) rather than a single
long sleep, and hand control back promptly.

Once the code changes for the issue are complete and local tests pass, and
**before proposing a commit**:

1. **Code review** -- Dispatch the `go-reviewer` agent (equivalent to the
   `/go-review` command) over the current diff. Complete every phase; do not
   stop at the first few findings.
2. **Completeness/artifact audit** -- Dispatch the `artifact-auditor` agent
   (equivalent to `/completeness-check`) to confirm the change has its required
   tests, benchmarks, examples, docs, integration tests, and MCP/tool coverage.
3. **Resolve findings** -- Address every CRITICAL/HIGH finding and any real
   MEDIUM before proposing a commit (hand off to `go-fixer` if substantial).
   Validate the fixes (build, `-race` tests, targeted integration) and note any
   accepted LOW/INFO items with justification.
4. **Lint before commit (mandatory)** -- Immediately before proposing a commit,
   run `task lint:changed` and confirm it reports **no new issues** in the
   changed code. This is required on the FINAL diff every time -- a lint run from
   earlier in the session does not count, since later edits (including test
   changes) can introduce new findings. Note: plain `task lint` always exits
   non-zero because of pre-existing issues in untouched files; `task lint:changed`
   filters to only issues this branch introduced (vs. the merge base), so a
   clean result there is the gate. If it reports anything, fix it before
   committing -- never push a lint-failing change and rely on CI to catch it.
5. **Summarize** -- Report the review outcome and what was fixed. Surface a
   question to the user only if a finding requires a genuine product/design
   decision -- never merely to ask permission to run the gate.

**Ask vs. proceed.** The rule is about not asking *permission to run the gate* --
it is NOT a rule to suppress genuine questions. Prompt the user whenever a real
judgment call surfaces, for example:

- The plan or issue has an ambiguity, an unresolved assumption, or something that
  needs the user's attention before or during implementation.
- A review or completeness finding does not have an obvious fix -- e.g. a design
  tradeoff, a scope decision, a breaking-change choice, conflicting conventions,
  or two reasonable approaches with different implications.
- The correct behavior/UX is unclear, or the fix would expand scope beyond the
  issue.

In these cases, pause and ask (offer concrete options and a recommendation).
Only routine, unambiguous findings should be fixed silently. When in doubt about
whether something is "obvious," ask.

`/pr-review` is **not** part of this local gate: it operates on an open GitHub
PR (fetching review threads, CI status, and Codecov) and therefore runs only
**after push**, once a PR exists. **The user runs `/pr-review` themselves** after
the local gate is done and the branch is pushed -- do not auto-run it as part of
issue work.

The `/finish-issue` command wraps steps 1-4 as a single callable; the flow above
must happen even if that command is never invoked explicitly.

## Embedder Contract

scafctl is used as a **library by external CLIs**. Every new feature must be consumable by embedders via `RootOptions` or domain package APIs.

- **No hardcoded "scafctl"**: Use `settings.CliBinaryName` or `settings.Run.BinaryName` in context
- **`RootOptions` is the embedder API surface**: New CLI-level capabilities must be exposed as fields with sensible defaults
- **Test embedder scenarios**: Include a test with a non-default binary name (e.g., `"mycli"`)

## Security Scanning

```bash
gosec ./...
```

## Additional Conventions

Go coding conventions (struct tags, error handling, design patterns), testing rules, integration test scoping, and documentation requirements are in `.github/instructions/*.instructions.md` files -- they load automatically when editing relevant files.

## Auto-Discovery and Resolution

When no `-f` flag is provided, all CLI commands use the unified `Resolve()` function from `pkg/solution/get` which:

1. Returns the explicit `-f` path if provided.
2. Returns the positional argument if provided (catalog reference).
3. Auto-discovers solution files by searching folder prefixes (`scafctl/`, `.scafctl/`, `.`) combined with file names (`solution.yaml`, `solution.yml`, `scafctl.yaml`, `scafctl.yml`, `solution.json`, `scafctl.json`, `taskfile.yaml`, `taskfile.yml`). Action file names (`actions.yaml`, `actions.yml`) are only searched in `DiscoveryModeAction` (used by `run action`).

**Multi-match ambiguity handling** uses risk levels:
- **Low-risk** (`DiscoveryRiskLow`): uses first match, emits a warning about other matches. Used by `run`, `lint`, `test`.
- **High-risk** (`DiscoveryRiskHigh`): returns an error requiring `-f`. Used by `build`.

The MCP server's `list_solutions` tool also uses `FindAllSolutions()` and returns all discovered paths.