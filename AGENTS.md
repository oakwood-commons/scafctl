# AGENTS.md

Primary AI guidance for this repo lives in the GitHub Copilot files and is
auto-loaded by opencode via `opencode.json`:

- `.github/copilot-instructions.md` -- project overview, key patterns, conventions.
- `.github/instructions/*.instructions.md` -- path-scoped rules (Go, CLI, providers,
  MCP, testing, docs). Read the one matching the file you are editing.

## Git safety & commit signing

The AI **may** create commits and push (with approval), but must follow these rules:

1. **Commit and push are ask-gated; amend is blocked.** The AI may run
   `git commit` and `git push` only after explicit per-action user approval.
   `git commit --amend` is **denied** outright -- the user rewrites history
   manually. (Enforced in `opencode.json`: `git commit *` -> `ask`,
   `git push *` -> `ask`, `git commit --amend *` -> `deny`.)
2. **Commits must be signed AND DCO signed-off.**
   - The repo sets `commit.gpgsign=true` (SSH signing).
   - The DCO check requires a `Signed-off-by:` trailer, so always commit with
     `-s` (or ensure the trailer is present).
3. **Verify the signing key is available before committing.** The SSH signing
   key is passphrase-protected; if it is not loaded in the ssh-agent, the commit
   fails mid-operation and no commit object is written. Check with:

   ```
   ssh-add -l
   ```

4. **If no key is loaded, prompt the user to load it** instead of retrying a
   doomed commit. Give them these instructions:

   ```
   ssh-add --apple-use-keychain ~/.ssh/id_ecdsa_public_github
   ```

   On macOS, `--apple-use-keychain` stores the passphrase in the Keychain so it
   is not re-prompted on every ssh-agent restart. On non-macOS hosts, drop the
   flag and use `ssh-add ~/.ssh/id_ecdsa_public_github`.

   On Windows (OpenSSH), the ssh-agent is a service that is often stopped by
   default. Start it once (elevated PowerShell), set it to auto-start, then add
   the key:

   ```powershell
   Set-Service ssh-agent -StartupType Automatic
   Start-Service ssh-agent
   ssh-add $env:USERPROFILE\.ssh\id_ecdsa_public_github
   ```

   If `ssh-add` reports it cannot connect to the agent, the service is not
   running -- start it as above and retry.

   Then have them confirm with `ssh-add -l` before you retry the commit.
   (The `.opencode/plugins/git-signing-guard.ts` plugin enforces this by
   blocking commits when no key is loaded.)

### Identity for this worktree

Repos under the `Public/` folder are signed as **alice@example.com** using
`~/.ssh/id_ecdsa_public_github` / `~/.ssh/id_ecdsa_public_github.pub`. The
repo-local `user.signingkey` should point at the `.pub` file so signing goes
through the ssh-agent.

## Conventions (summary; see .github for full detail)

- **Conventional commits** (`feat:`, `fix:`, `docs:`, `!` for breaking).
- **Errors:** wrap with `fmt.Errorf("context: %w", err)`; never panic.
- **Business logic** never in `pkg/cmd/scafctl/...`, `pkg/mcp/tools_*.go`, or API
  packages -- put it in shared domain packages.
- **Tests** for every new/changed file (target 70%+ patch coverage).
- **Scratch files** go in the gitignored `temp/`; never commit them.
- **Build/lint** via `task` (use `task lint`, not raw golangci-lint).
