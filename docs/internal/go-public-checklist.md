# Go Public Checklist

Comprehensive checklist for making the scafctl repository public and ready for community contributions. Items are grouped by priority and category.

---

## Priority Definitions

| Priority | Meaning |
|----------|---------|
| **P0** | Must be done before making the repo public. Blockers. |
| **P1** | Should be done before or immediately after going public. Critical for contributor experience. |
| **P2** | Nice to have for launch. Can be done in the first few weeks after going public. |

---

## P0 - Required Before Going Public

### Security & Secrets Audit

- [x] **Scan full git history for leaked secrets and credentials** *(Done — gitleaks found 19 hits, all false positives: test fixture JWTs, example placeholder keys in docs, and a UUID tenant ID. No real secrets. History is clean.)*

### SECURITY.md

- [x] **Create `.github/SECURITY.md`** *(Done — uses GitHub Security Advisories for reporting instead of email)*

### License Review

- [x] **Add copyright notice to LICENSE file** *(Done — added "Copyright 2025-2026 Oakwood Commons" to top of LICENSE, and SPDX headers to all 451 .go files)*

### README Overhaul

- [x] **Add installation instructions** *(Done — added binary download and `go install` sections)*
- [x] **Add project maturity notice at the top of the README** *(Done — Alpha notice added)*
- [x] **Add badges** *(Done — Go Report Card, License, Release, CI)*
- [x] **Add link to CONTRIBUTING.md** *(Done)*
- [x] **Add link to CODE_OF_CONDUCT.md** *(Done)*
- [x] **Review all documentation links** *(Done — fixed tutorial links to point to `docs/tutorials/`)*

### CLI Polish

- [ ] **Systematic walkthrough of every CLI command**
  - Run every subcommand with `--help` and verify output is clear
  - Run the happy path for each command
  - Verify error messages are user-friendly (no stack traces, no internal paths)
  - Check that `--quiet`, `--no-color`, `-o json`, `-o yaml`, `-o table` all work consistently
- [ ] **Distribute to team members for feedback** using pre-built binaries + tutorials
- [x] **Verify `scafctl version`** shows correct info and the TODO for "latest version check" either works or is gracefully skipped *(Done — implemented `GetLatestVersion` using GitHub Releases API)*

### Repo Cleanup

- [~] **Review `docs/internal/`** — decide what stays public *(Reviewed — keeping files until repo goes public)*
  - `going-public.md` and this checklist: **remove or move** before going public (internal planning docs)
  - `TODO.md`: 6 unchecked items remain to convert to GitHub Issues before removal:
    1. Dependency resolution (recursive, circular detection, version constraints)
    2. Multi-platform support (OCI image index)
    3. Cache management (TTL, `--no-cache` flag)
    4. Plugin discovery mechanism
    5. Help text generation (dynamic per kind)
    6. Artifact caching
  - Implementation plans / decision records (22 files): safe to keep public (shows project maturity)
- [x] **Resolve or convert TODOs in code to GitHub Issues**
  - ~~`pkg/celexp/validation.go:236` — "Could check element type..."~~ *(Deleted — speculative, current code handles parameterized types)*
  - ~~`pkg/celexp/validation.go:244` — "Could check key/value types..."~~ *(Deleted — same reason)*
  - `pkg/provider/executor.go:156` — "Consider passing capability context..." *(Kept as-is — valid architectural note)*
  - ~~`pkg/solution/get/get.go:126` — "This will need to support the scafctl repository"~~ *(Removed — issue to be filed on GitHub)*
  - ~~`pkg/cmd/scafctl/version/version.go:119` — "Need to implement getting the latest version"~~ *(Implemented)*
- [x] **Remove placeholder values in taskfile.yaml** *(Done — replaced `????` with empty defaults and added descriptive comments)*
- [x] **Review `.goreleaser.yaml`** *(Done — removed GCS `blobs` section, added grouped changelog with conventional commits)*
- [x] **Review AI config** — `.github/copilot-instructions.md` is fine to keep (many public repos have this). It helps contributors using Copilot *(Done — keep as-is)*

### Builtin Actions & Providers Review

- [x] **Verify all builtin actions work correctly** with current examples *(Done — all 12 action examples pass including complex-workflow and result-schema-validation after fixes)*
- [x] **Verify all builtin providers work correctly** with current examples *(Done — exec, cel, static, parameter, identity providers verified. Note: parameter provider returns hard errors when params are missing instead of triggering fallback chain — potential bug affecting solution examples that use parameter→static fallback)*
- [x] **Check that example files in `examples/` all run successfully** *(Done — all resolver examples (10/10) and action examples (12/12) pass. Solution examples fail due to parameter provider fallback bug noted above. Two bugs fixed: `complex-workflow.yaml` unescaped parentheses in deploy commands, `result-schema-validation.yaml` rewritten to use CEL provider instead of exec with unsupported `output: json`)*

---

## P1 - Should Be Done Before or Immediately After

### CODE_OF_CONDUCT.md

- [x] **Create `CODE_OF_CONDUCT.md` using the Contributor Covenant v2.1** *(Done — full Contributor Covenant v2.1 with GitHub Security Advisories for enforcement contact)*

### GitHub Issue & PR Templates

- [x] **Create `.github/ISSUE_TEMPLATE/bug_report.md`** *(Done)*

  ```markdown
  ---
  name: Bug Report
  about: Report a bug to help us improve scafctl
  labels: ["bug"]
  ---

  ## Describe the Bug

  A clear and concise description of the bug.

  ## Steps to Reproduce

  1. Run `scafctl ...`
  2. With this input file: ...
  3. See error

  ## Expected Behavior

  What you expected to happen.

  ## Actual Behavior

  What actually happened. Include full error output.

  ## Environment

  - scafctl version (`scafctl version`):
  - OS:
  - Go version (if building from source):

  ## Additional Context

  Any other context, screenshots, or log output.
  ```

- [x] **Create `.github/ISSUE_TEMPLATE/feature_request.md`** *(Done)*

  ```markdown
  ---
  name: Feature Request
  about: Suggest an idea for scafctl
  labels: ["enhancement"]
  ---

  ## Problem Statement

  What problem does this feature solve? What use case does it address?

  ## Proposed Solution

  Describe the solution you'd like.

  ## Alternatives Considered

  Any alternative solutions or features you've considered.

  ## Additional Context

  Any other context, examples, or mockups.
  ```

- [x] **Create `.github/PULL_REQUEST_TEMPLATE.md`** *(Done)*

  ```markdown
  ## Description

  Brief description of the changes.

  ## Related Issues

  Closes #

  ## Type of Change

  - [ ] Bug fix
  - [ ] New feature
  - [ ] Breaking change
  - [ ] Documentation update
  - [ ] Refactoring

  ## Checklist

  - [ ] I have read the [CONTRIBUTING](CONTRIBUTING.md) guide
  - [ ] My commits follow conventional commit format
  - [ ] I have added/updated tests for my changes
  - [ ] `go test ./...` passes
  - [ ] `golangci-lint run` passes
  - [ ] I have signed off my commits (`git commit -s`)
  ```

### DCO (Developer Certificate of Origin)

- [x] **Add DCO requirement to CONTRIBUTING.md** *(Done)*
  - Add a section explaining the sign-off requirement:

    ```markdown
    ## Developer Certificate of Origin (DCO)

    All contributions must be signed off per the
    [Developer Certificate of Origin](https://developercertificate.org/).
    This certifies that you have the right to submit the contribution
    under the project's license.

    Sign your commits with the `-s` flag:

    ```bash
    git commit -s -m "feat(provider): add new provider"
    ```

    This adds a `Signed-off-by: Your Name <your@email.com>` line to
    the commit message.

    If you forget, you can amend the last commit:

    ```bash
    git commit --amend -s --no-edit
    ```
    ```

- [x] **Install the DCO GitHub App** on the repo *(Done — installed)*

### CONTRIBUTING.md Updates

- [x] **Add DCO section** *(Done)*
- [x] **Add support expectations section** *(Done — added to CONTRIBUTING.md)*

  ```markdown
  ## Getting Help & Support Expectations

  scafctl is maintained on a best-effort, community-driven basis. We aim to:

  - **Triage new issues** within 1-2 weeks
  - **Review pull requests** within 2 weeks
  - **Respond to security reports** within 48 hours (see [SECURITY.md](.github/SECURITY.md))

  For questions and discussions, use
  [GitHub Discussions](https://github.com/oakwood-commons/scafctl/discussions)
  or open an issue.

  We appreciate your patience and contributions!
  ```

- [x] **Review Go version prerequisite** *(Done — updated to Go 1.25.4+)*
- [x] **Add link to Code of Conduct** *(Done)*
- [x] **Add link to Security Policy** *(Done)*

### GitHub Org & Repo Settings

- [x] **Make `oakwood-commons` org public** *(Done — org is public)*
- [ ] **Add org profile**: description, avatar, README at `.github/profile/README.md`
- [x] **Enable GitHub Discussions** on the repo for Q&A and general discussion *(Done — enabled)*
- [ ] **Configure repository topics/tags** (e.g., `go`, `cli`, `scaffolding`, `cel`, `devtools`, `configuration`)
- [ ] **Set repository description** and website URL (if docs site exists)

### Branch Protection

- [x] **Enable branch protection on `main`** *(Done — configured)*
  - Require pull request reviews (at least 1 reviewer)
  - Require status checks to pass (lint-and-test)
  - Require conversation resolution
  - Require signed commits (optional, but recommended for OSS)
  - Disallow force pushes
  - Disallow deletions

---

## P2 - Nice to Have for Launch

### CODEOWNERS

- [x] **Create `.github/CODEOWNERS`** *(Done)*

  ```
  # Default owners for everything
  * @oakwood-commons/scafctl-maintainers

  # Specific areas (adjust as team grows)
  /pkg/provider/   @oakwood-commons/scafctl-maintainers
  /pkg/celexp/     @oakwood-commons/scafctl-maintainers
  /docs/           @oakwood-commons/scafctl-maintainers
  ```

### Installation Enhancements (Post-Launch)

- [ ] **Homebrew tap** — create `oakwood-commons/homebrew-tap` repo with a formula
  - Add to goreleaser config:
    ```yaml
    brews:
      - name: scafctl
        repository:
          owner: oakwood-commons
          name: homebrew-tap
        homepage: "https://github.com/oakwood-commons/scafctl"
        description: "A configuration discovery and scaffolding tool"
        license: "Apache-2.0"
        install: |
          bin.install "scafctl"
        test: |
          system "#{bin}/scafctl", "version"
    ```
  - Enables `brew install oakwood-commons/tap/scafctl`
- [x] **Shell completion** — ensure `scafctl completion bash/zsh/fish/powershell` works and document it *(Done — documented in README.md)*
- [ ] **AUR package** (Arch Linux) — community can contribute this

### GoReleaser Cleanup

- [x] **Review GCS blob config** *(Done — removed `blobs` section)*
- [x] **Add GitHub release notes generation** to goreleaser if not present *(Done — `generate_release_notes: true` added)*
- [ ] **Verify GPG signing works** or remove signing config if not desired for initial release
- [x] **Consider adding a `changelog` section** using conventional commits: *(Done — added grouped changelog)*
  ```yaml
  changelog:
    use: github
    groups:
      - title: Features
        regexp: '^.*?feat(\([[:word:]]+\))??!?:.+$'
      - title: Bug Fixes
        regexp: '^.*?fix(\([[:word:]]+\))??!?:.+$'
      - title: Documentation
        regexp: '^.*?docs(\([[:word:]]+\))??!?:.+$'
      - title: Others
        order: 999
    filters:
      exclude:
        - '^chore:'
  ```

### CI Enhancements

- [x] **Add CodeQL / security scanning** workflow *(Done — `.github/workflows/codeql.yml` created)*
  ```yaml
  # .github/workflows/codeql.yml
  name: CodeQL
  on:
    push:
      branches: [main]
    pull_request:
      branches: [main]
    schedule:
      - cron: '0 6 * * 1'  # Weekly
  jobs:
    analyze:
      runs-on: ubuntu-latest
      permissions:
        security-events: write
      steps:
        - uses: actions/checkout@v4
        - uses: github/codeql-action/init@v3
          with:
            languages: go
        - uses: github/codeql-action/autobuild@v3
        - uses: github/codeql-action/analyze@v3
  ```
- [x] **Add DCO check** to CI (if using DCO GitHub App, this is automatic) *(Done — added DCO workflow; app installed)*
- [x] **Consider adding `go vet` and `staticcheck`** to PR checks *(Already covered — `staticcheck` is explicitly enabled in `.golangci.yml` line 32, and `govet` is a default linter in golangci-lint v2. PR checks run `task lint` → `golangci-lint run`)*

### Testing Namespace

- [x] **Document how contributors can run integration tests** that require registry access, or provide mocks *(Done — noted integration tests run locally without external registry access)*

### Documentation Site

- [ ] **Review Hugo site** (`hugo.yaml`, `public/`) — decide if you want to publish a docs site (GitHub Pages, Netlify, etc.)
- [ ] **Add a GitHub Pages deployment workflow** if publishing docs

---

## Execution Order

Recommended order for executing the above:

### Phase 1: Security & Cleanup (Do First)
1. Run secrets scan on full git history
2. Resolve: squash history or keep (based on scan results)
3. Remove/relocate `docs/internal/going-public.md`, this checklist, and `TODO.md`
4. Clean up taskfile.yaml placeholder values
5. Resolve or convert code TODOs to issues

### Phase 2: Community Files
6. Create SECURITY.md
7. Create CODE_OF_CONDUCT.md
8. Create issue templates and PR template
9. Update CONTRIBUTING.md (DCO, support expectations, correct Go version)
10. Add copyright notice to LICENSE

### Phase 3: README & First Impressions
11. Overhaul README (installation, badges, maturity notice, links)
12. Configure repo settings (topics, description, discussions)
13. Set up branch protection

### Phase 4: CLI & Content Validation
14. Full CLI command walkthrough
15. Verify all examples work
16. Team testing with pre-built binaries

### Phase 5: Release & Launch
17. Review and clean up goreleaser config
18. Install DCO GitHub App
19. Create CODEOWNERS
20. Tag initial public release (e.g., `v0.1.0`)
21. Announce

---

## Post-Launch

- Monitor issues and discussions for the first few weeks
- Set up a regular triage cadence
- Consider writing a blog post or announcement
- Submit to awesome-go list once stable
- Track contributor onboarding friction and update docs accordingly
