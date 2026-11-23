# Copilot code review: how to review this repo

You are reviewing as an **independent final reviewer**, not a re-run of this
repo's own development process. The change you're looking at has already gone
through this repo's own AI-assisted (or human) implementation and local
checks -- your job is to find what that process missed, not to redo it or
invent a parallel standard.

## Sources of truth

Do not restate this repo's engineering process here -- read it directly:

- `AGENTS.md` -- canonical repo guidance: build/test/lint commands, key
  patterns, business-logic placement, embedder contract, Go/testing
  conventions, per-area rules.
- `.github/skills/` -- on-demand deep dives: `kvx-rendering` (CLI array output
  and the `-i` TUI), `cel-extensions` (writing CEL extension functions),
  `cel-patterns`, `go-template-patterns`, `provider-authoring`,
  `solution-spec`, `publish-hygiene`.

Where this file's guidance and one of the sources above conflict on a
repo-specific convention, the source above wins; this file only governs *how*
to conduct the review, not the repo's engineering standards.

## Method: review the whole change before writing any findings

1. **Read the full diff and the PR description together**, not comment by
   comment as if triaging a stream. Understand what changed and why before
   judging any single hunk.
2. **Inventory every changed interface**: functions, exported types,
   HTTP/RPC endpoints, MCP tools, schemas, config keys/flags, CLI commands,
   or documented behaviors. For each one, check its callers/consumers and
   existing tests elsewhere in the repo -- not just the lines the diff
   touched. A change that looks correct in isolation can still break a
   caller's assumptions, an existing test's expectations, or documented
   compatibility.
3. **Check test coverage against the actual change**, not against its own
   claims: do the new/changed tests exercise the new behavior, the edge
   cases, and the failure paths -- or only the happy path? Run or read the
   test command below if you need to confirm what's actually asserted:
   `go test ./...` (unit/integration; `task lint:changed` and `task test:e2e`
   are the repo-gate variants the author runs locally).

## What to weigh heavily

Correctness, security, and basic code quality are already what Copilot's
own review is optimized for -- restating them adds noise, not signal
(GitHub's own guidance: vague instructions like "don't miss any issues" add
noise without improving effectiveness). The items below are the ones a
diff-scoped review is most likely to under-weight, not a general checklist:

- **Regressions and edge cases, concretely**: empty/zero/nil inputs, values
  at a boundary or capacity limit, and the failure path of each new external
  call (network, filesystem, subprocess, downstream API) -- not just the
  happy path the diff's own tests exercise.
- **API or behavioral compatibility for existing callers** -- found via the
  interface inventory above, not visible from the diff alone. (Breaking
  changes ARE allowed in this pre-production repo -- flag them only when
  they are missing a note in the PR body, not because they break.)
- **Concurrency and synchronization** (races, deadlocks, shared mutable
  state), **resource cleanup** (files, connections, goroutines/threads,
  locks), and **context/timeout/cancellation handling** -- classes of bug
  that read as correct in isolation and only fail under conditions the diff
  itself doesn't show.
- **Test coverage against the actual change**, not against the tests' own
  claims (Method step 3).
- **Trust boundaries**: a new input, endpoint, or config value that reaches
  an authz check, a database query, a shell/exec call, a file path, or a
  log statement (secrets) elsewhere in the repo -- trace it to that sink,
  not just the diff's own lines.
- **Comment hygiene and accuracy**: verify touched or adjacent comments and
  docstrings accurately match current code behavior (identifiers,
  parameters, return values, error sentinels, callers), and flag
  development or review narration in code or test comments (PR/review-round
  references, issue/ticket IDs, phase/item scaffolding, pre-change
  comparisons, calling out people by name/handle, or duplicated
  multi-paragraph docblocks across pass-through layers).
- **Violations of this repo's own stated architecture or conventions** (see
  Sources of truth above) -- repo-specific, not inferable from the code
  alone.

### Breadth and depth are different axes

Cover the whole diff (breadth) -- but before writing findings, identify
which parts of the change are highest-risk (the interface inventory from
Method step 2 is a proxy: more callers/consumers, more state, more
existing tests likely to break -> higher risk) and spend deeper review
effort there. Treating every hunk as equally worth the same attention
produces shallow coverage everywhere instead of real coverage where it
matters.

Repo-specific:

- **Business-logic placement**: no business logic in CLI command packages
  (`pkg/cmd/scafctl/...`), MCP handler files (`pkg/mcp/tools_*.go`), or API
  packages -- it belongs in shared domain packages (`pkg/...`).
- **Embedder contract**: no hardcoded binary name -- use
  `settings.CliBinaryName` / `settings.Run.BinaryName`, `SafeEnvPrefix()`
  for env vars, new CLI capabilities exposed as `RootOptions` fields with
  documented defaults (also applies to the API server via `ServerOption` and
  to MCP tool descriptions).
- **Output pipeline**: CLI output goes through `writer.FromContext(ctx)`,
  never raw `fmt.Fprintf`; structured data goes through `kvx.OutputOptions`;
  commands emitting an **array of objects** attach an interactive display
  schema (`kvx.WithOutputDisplaySchemaJSON`), and structured/quiet formats
  emit a parseable document (`[]`, not `null`) with human "no results" text
  on stderr. Diff subcommands reuse the one shared `writeDiffOutput` /
  diffreport pipeline rather than hand-rolled formatting. Before claiming a
  kvx rendering bug, load the `kvx-rendering` skill -- piped output shows
  only the flat text fallback, never the TUI.
- **Coverage**: 70%+ patch coverage per PR, no new file at 0%, and tests
  must exercise the actual change (not just restate its claims) --
  including integration tests in the right file (CLI -> `cli_test.go`,
  providers/solutions -> `tests/integration/solutions/`, API -> and only ->
  `api_test.go`).
- **No magic values**: configuration values, timeout defaults, and
  conflict-strategy defaults must be exported constants or settings, not
  literal numbers/strings.
- **Spec-field deprecation**: a deprecated field must use the struct-tag
  deprecation mechanism (`deprecated:"true"` +
  `deprecatedReplacement:`, keep the old field working, doc the mapping) --
  never a bare delete or a one-off lint rule.
- **Publish hygiene**: this repo is public -- flag any employer- or
  organization-specific reference (internal tool names, hostnames/domains,
  codenames, employee/user IDs, credentials) in the diff, including in docs,
  examples, and test fixtures.
- **Secret and credential redaction**: whenever a change touches `sensitive:
  true` resolvers, `scrt:`/`tkn:` `ValueRef`s, secrets, or auth tokens, verify
  redaction holds across all sibling output surfaces (`run`, `dryrun`/`WhatIf`,
  `render`, `snapshot` with `0600` permissions preserved on overwrite, REST
  endpoints, MCP tools, `forEach` metadata, and short/numeric values) and never
  re-evaluates pre-resolved untrusted strings as templates or expressions.
- **PR title/body accuracy**: the repo squash-merges with
  `squash_merge_commit_title=PR_TITLE` / `squash_merge_commit_message=PR_BODY`
  -- the PR title must be a valid conventional-commit subject and the body
  must describe the change as it stands at HEAD, not an earlier version.
  Flag a stale body or non-conventional title.

## What not to spend the review on

Purely stylistic comments, subjective refactoring suggestions, or low-value
formatting nitpicks should not dominate the review, and should not be raised
at all unless they violate an explicit convention from one of the sources of
truth above. If you have nothing substantive to say about a hunk, say
nothing about it.

Concretely, do not flag:

- **Style nits** that gofmt/goimports or the pinned linter already govern.
- **"Consider ..." suggestions with no behavioral impact** -- speculative
  abstractions, renames of private helpers, or alternate phrasings of docs.
- **Defensive guards for non-existent callers** -- input validation on code
  paths with no external input, or nil checks where the value provably
  cannot be nil.

## Follow the implication

If one finding suggests a class of mistake (a missing nil check, an
unhandled error path, a repeated copy-paste of the same logic), **search the
rest of the diff for other occurrences of the same class before finishing**.
Report it as one finding covering every location, not one comment per
occurrence. Likewise, if several symptoms trace back to one underlying
defect, identify the underlying defect as the finding rather than raising
each symptom separately.

## Before finishing: a completeness pass

Before submitting the review, reconsider the whole diff once more against:
the interface inventory from Method step 2, any patterns surfaced by your
own findings above (did you actually check every occurrence?), and whether
the tests you found in Method step 3 would actually catch a regression in
what you just found. The goal is one comprehensive review, not the first
issue you noticed.

This does not require finding every possible issue in one pass -- review is
inherently non-deterministic, and later changes can expose new problems.
The goal is to avoid *avoidable* serial discovery of issues that were
already present and reasonably detectable right now.

## Re-reviews (after a fix is pushed)

Stick to what changed since your last review (`<last_reviewed_commit>..HEAD`)
rather than re-reviewing the entire pull request from scratch:

1. **Verify prior findings and their fixes**: confirm each previously reported
   issue is resolved, and check the immediate implications of that fix (e.g.
   whether the fix introduced a regression or missed a sibling occurrence of
   the same defect in adjacent paths).
2. **Scope file inspection to changed files + newly affected callers**: review
   only the files modified since `<last_reviewed_commit>`, plus callers or
   consumers of any interface whose signature or behavior changed in that
   delta. **Do not re-read or re-audit untouched files** that already passed an
   earlier review round.
3. **Check PR body sync**: confirm the PR title and body still match `HEAD`.
