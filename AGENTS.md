# AGENTS.md

Primary AI guidance for this repo lives in the GitHub Copilot files and is
auto-loaded by opencode via `opencode.json`:

- `.github/copilot-instructions.md` -- project overview, key patterns, conventions.
- `.github/instructions/*.instructions.md` -- path-scoped rules (Go, CLI, providers,
  MCP, testing, docs). Read the one matching the file you are editing.

## Git safety & commit signing

The AI **may** create commits, push, and open/merge PRs (with approval), but
must follow these rules:

1. **Publishing is approval-gated; amend is blocked.** Creating commits,
   pushing, and opening/merging PRs all require the user's explicit approval
   FIRST. A **single approval may cover the whole `commit` -> `push` -> PR
   sequence** -- the AI asks once (presenting the consolidated summary and the
   exact commands it intends to run), and on approval may run that sequence
   without stopping again for each step. If anything material changes after
   approval (different files, a failing gate), re-ask. `git commit --amend` is
   **denied** outright -- the user rewrites history manually.
   - **Enforcement is opencode's `permission` `ask` rules** (in `opencode.json`:
     `git commit*`, `git push*`, `gh pr create*`, `gh pr merge*` -> `ask`;
     `git commit --amend*` -> `deny`). The `ask` prompt is the real consent
     gate: it is surfaced to the user and cannot be self-approved by the agent.
     (There is deliberately no in-band "approval marker" -- a marker in the
     command text would be asserted by the agent, not the user, so it proves
     nothing.)
   - **Never push or open a PR without asking.** The AI must not chain
     branch/commit/push/PR autonomously. Present the summary, ask, and only
     then run the approved sequence.
2. **Do not `sleep` to wait for CI; ask first, then loop-poll.** After
   pushing or opening a PR, do not silently idle on a long `sleep` waiting for
   pipeline results, and do not silently move on either -- **ask the user
   whether to wait**. If the user wants to wait, poll in a **bounded loop**
   (e.g. repeated `gh pr checks` with a short interval) rather than a single
   long sleep, and hand control back promptly once there is a result or the
   user says to stop.
3. **Commits must be signed AND DCO signed-off.**
   - The repo sets `commit.gpgsign=true` (SSH signing).
   - The DCO check requires a `Signed-off-by:` trailer, so always commit with
     `-s` (or ensure the trailer is present).
4. **Verify the signing key is available before committing.** The SSH signing
   key is passphrase-protected; if it is not loaded in the ssh-agent, the commit
   fails mid-operation and no commit object is written. Check with:

   ```
   ssh-add -l
   ```

5. **If no key is loaded, prompt the user to load it** (per their local machine
   setup) instead of retrying a doomed commit. Then have them confirm with
   `ssh-add -l` before you retry the commit. (The
   `.opencode/plugins/git-signing-guard.ts` plugin enforces this by blocking
   commits when no key is loaded.)

### This is a squash-merge repo

PRs are merged with **squash**, so every PR becomes a single commit on `main`
and the individual commit messages on the branch are discarded (GitHub uses the
**PR title and body** as the final squash commit message). Commit accordingly:

- **Do not split a branch into multiple commits for history's sake** -- it buys
  nothing under squash and the messages are thrown away. Prefer a single,
  well-formed commit per PR (or however many is convenient while working;
  they collapse anyway).
- **Put the real effort into the PR title and body**, not per-commit messages.
  The PR title must be a valid conventional-commit subject (`feat:`, `fix:`,
  `docs:`, `!` for breaking) because it becomes the squash subject.
- **One logical change per PR.** Since everything in a PR squashes together,
  keep unrelated concerns in **separate PRs** rather than separate commits --
  e.g. a bug fix and unrelated tooling/docs changes should be two PRs, not one
  PR with two commits.
- The branch still must be signed and DCO signed-off (the checks run on the
  branch commits, and the DCO trailer must survive into the squash).

**The PR body IS the final commit message -- keep it in sync with every commit.**
The repo is configured with `squash_merge_commit_title=PR_TITLE` and
`squash_merge_commit_message=PR_BODY`, so the squash commit on `main` is exactly
the **PR title + PR body** (the per-commit messages are discarded entirely).
Because the AI cannot amend (`git commit --amend` is denied) and each review
fix is a **new commit**, a PR routinely accumulates many noisy fix commits --
none of which reach `main`. The discipline that makes this clean:

- **Whenever a commit changes what the PR does, update the PR body to match.**
  Treat the PR body as the living commit message. If a review fix changes
  behavior, flags, or scope, edit the PR body (via `gh pr edit`) in the same
  step so the eventual squash commit is accurate and complete -- never a stale
  description of an earlier version.
- **Before proposing a merge, do a final pass on the PR title and body** so they
  read as one coherent change (folding in anything that shifted during review),
  and show them to the user for approval. The messy fix-commit log is invisible
  on `main`; only the title/body matter.

**Reviewer-loop judgment: know when to stop.** This repo has Copilot configured
to **re-review on every push** (`review_on_push: true`), so each new commit --
including every review fix -- triggers a fresh review that may surface new
threads. To avoid an endless fix -> push -> new-review loop, classify each review
round:

- **Substantive** (correctness bugs, fail-open/guarantee-defeating logic, silent
  failures, security, missing error handling, 0%-covered new logic): **fix it**,
  accept that pushing triggers another review.
- **Nitpick** (comment wording, naming, doc phrasing, display-only issues,
  "consider..." suggestions with no behavioral impact, defensive guards for
  non-existent callers): **do NOT push another fix**. Instead **reply to the
  thread acknowledging it as a non-blocking nit and resolve it** -- replying and
  resolving does not create a commit, so it does not trigger a new review.
- **The stop signal:** when a review round is **all nits** (no substantive
  findings), state that explicitly with the reasoning, reply-and-resolve the
  nit threads, and -- once **CI is green AND the PR's `reviewDecision` is not
  blocking** -- recommend merging. Do not keep pushing. Note that resolving
  threads does NOT clear a `CHANGES_REQUESTED` review decision on GitHub, and
  `main` requires an approving review (`required_approving_review_count: 1`,
  `require_last_push_approval: true`): so if the decision is still
  `CHANGES_REQUESTED` or `REVIEW_REQUIRED`, the merge stays blocked on green CI
  alone. In that case say so and ask the user to approve (or request a
  re-review) rather than implying the PR is mergeable.


### Identity for this worktree

Commits in this repo must be **SSH-signed** and carry a **DCO `Signed-off-by:`**
trailer (see the Git safety rules above). Contributors configure their own
signing identity locally (git `user.name` / `user.email` / `user.signingkey`);
do not hardcode a specific person's key or email here.

## Conventions (summary; see .github for full detail)

- **Conventional commits** (`feat:`, `fix:`, `docs:`, `!` for breaking).
- **Squash-merge repo:** PRs squash to one commit from the PR title/body -- don't
  split for history, keep unrelated concerns in separate PRs (see Git section).
- **Errors:** wrap with `fmt.Errorf("context: %w", err)`; never panic.
- **Business logic** never in `pkg/cmd/scafctl/...`, `pkg/mcp/tools_*.go`, or API
  packages -- put it in shared domain packages.
- **Tests** for every new/changed file (target 70%+ patch coverage).
- **Scratch files** go in the gitignored `temp/`; never commit them.
- **Build/lint** via `task` (use `task lint`, not raw golangci-lint). Before
  every commit, run `task lint:changed` and confirm no new issues -- it filters
  out pre-existing findings in untouched files (which make plain `task lint`
  always exit non-zero) and reports only what this branch introduced.
