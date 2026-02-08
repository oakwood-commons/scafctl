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

- [ ] **Scan full git history for leaked secrets and credentials**
  - Run a secrets scanner against the full history:
    ```bash
    # Option 1: trufflehog (recommended)
    brew install trufflehog
    trufflehog git file://. --since-commit HEAD~1000 --only-verified

    # Option 2: gitleaks
    brew install gitleaks
    gitleaks detect --source . -v
    ```
  - Manually grep for internal domains, API keys, tokens:
    ```bash
    git log --all -p | grep -iE 'secret|password|token|api[_-]?key|private[_-]?key|AZURE_CLIENT_SECRET' | head -50
    ```
  - Check for any hardcoded internal URLs (e.g., internal registries, Slack webhooks, corp domains)
  - If anything is found: rotate the credential immediately, then either rewrite history with `git filter-repo` or squash to a clean init commit
  - **Decision needed**: Squash to single init commit vs. keep history. If secrets are found, squash. If history is clean, keeping it builds trust with contributors.

### SECURITY.md

- [ ] **Create `.github/SECURITY.md`**
  - GitHub surfaces this in the Security tab of the repo
  - Content:

    ```markdown
    # Security Policy

    ## Supported Versions

    | Version | Supported |
    |---------|-----------|
    | latest  | Yes       |

    ## Reporting a Vulnerability

    **Do NOT open a public GitHub issue for security vulnerabilities.**

    Please report security vulnerabilities by emailing [SECURITY_EMAIL].

    Include:
    - Description of the vulnerability
    - Steps to reproduce
    - Impact assessment
    - Any suggested fixes (optional)

    ### Response Timeline

    - **Acknowledgment**: Within 48 hours
    - **Initial assessment**: Within 1 week
    - **Fix timeline**: Communicated after assessment

    We will coordinate disclosure with you and credit you in the advisory
    (unless you prefer to remain anonymous).
    ```

  - Replace `[SECURITY_EMAIL]` with a real contact

### License Review

- [ ] **Add copyright notice to LICENSE file**
  - The Apache 2.0 LICENSE file should have a copyright line. Add to the top or appendix:
    ```
    Copyright 2025-2026 Oakwood Commons
    ```
  - Decide whether to add the Apache 2.0 boilerplate header to source files. Recommendation: add a short header to `.go` files (can be automated with `addlicense` tool):
    ```go
    // Copyright 2025-2026 Oakwood Commons
    // SPDX-License-Identifier: Apache-2.0
    ```
    ```bash
    # Automate with:
    go install github.com/google/addlicense@latest
    addlicense -c "Oakwood Commons" -l apache -s only .
    ```

### README Overhaul

- [ ] **Add installation instructions** (most important section for newcomers)

  ```markdown
  ## Installation

  ### From Release Binaries (Recommended)

  Download the latest binary for your platform from the
  [GitHub Releases](https://github.com/oakwood-commons/scafctl/releases) page.

  #### macOS / Linux

  ```bash
  # Download (replace VERSION and OS/ARCH as needed)
  curl -LO https://github.com/oakwood-commons/scafctl/releases/latest/download/scafctl_VERSION_OS_ARCH.tar.gz
  tar xzf scafctl_*.tar.gz
  sudo mv scafctl /usr/local/bin/
  ```

  #### From Source

  ```bash
  go install github.com/oakwood-commons/scafctl/cmd/scafctl@latest
  ```
  ```

- [ ] **Add project maturity notice at the top of the README**

  ```markdown
  > **Alpha** — scafctl is under active development. APIs and CLI commands may
  > change between releases. Breaking changes are documented in release notes.
  ```

- [ ] **Add badges**

  ```markdown
  [![Go Report Card](https://goreportcard.com/badge/github.com/oakwood-commons/scafctl)](https://goreportcard.com/report/github.com/oakwood-commons/scafctl)
  [![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
  [![Release](https://img.shields.io/github/v/release/oakwood-commons/scafctl)](https://github.com/oakwood-commons/scafctl/releases)
  [![CI](https://github.com/oakwood-commons/scafctl/actions/workflows/pr-checks.yml/badge.svg)](https://github.com/oakwood-commons/scafctl/actions/workflows/pr-checks.yml)
  ```

- [ ] **Add link to CONTRIBUTING.md**
- [ ] **Add link to CODE_OF_CONDUCT.md** (once created)
- [ ] **Review all documentation links** — make sure tutorial/doc links in the README resolve correctly

### CLI Polish

- [ ] **Systematic walkthrough of every CLI command**
  - Run every subcommand with `--help` and verify output is clear
  - Run the happy path for each command
  - Verify error messages are user-friendly (no stack traces, no internal paths)
  - Check that `--quiet`, `--no-color`, `-o json`, `-o yaml`, `-o table` all work consistently
- [ ] **Distribute to team members for feedback** using pre-built binaries + tutorials
- [ ] **Verify `scafctl version`** shows correct info and the TODO for "latest version check" either works or is gracefully skipped

### Repo Cleanup

- [ ] **Review `docs/internal/`** — decide what stays public
  - `going-public.md` and this checklist: **remove or move** before going public (internal planning docs)
  - `TODO.md`: convert remaining items to GitHub Issues, then remove the file
  - Implementation plans / decision records: generally fine to keep public (shows project maturity)
- [ ] **Resolve or convert TODOs in code to GitHub Issues**
  - `pkg/celexp/validation.go:236` — "Could check element type..."
  - `pkg/celexp/validation.go:244` — "Could check key/value types..."
  - `pkg/provider/executor.go:156` — "Consider passing capability context..."
  - `pkg/solution/get/get.go:126` — "This will need to support the scafctl repository"
  - `pkg/cmd/scafctl/version/version.go:119` — "Need to implement getting the latest version"
- [ ] **Remove placeholder values in taskfile.yaml**
  - `CONTAINER_REGISTRY` defaults to `????`
  - `PREPROD_URL` and `PROD_URL` are `https://?????`
  - Either set real values, remove them, or add comments explaining they're optional/environment-specific
- [ ] **Review `.goreleaser.yaml`** — the GCS blob upload (`scafctl-assets` bucket) may be internal. Decide if you want GitHub Releases only (remove the `blobs` section) or keep GCS
- [ ] **Review AI config** — `.github/copilot-instructions.md` is fine to keep (many public repos have this). It helps contributors using Copilot

### Builtin Actions & Providers Review

- [ ] **Verify all builtin actions work correctly** with current examples
- [ ] **Verify all builtin providers work correctly** with current examples
- [ ] **Check that example files in `examples/` all run successfully**

---

## P1 - Should Be Done Before or Immediately After

### CODE_OF_CONDUCT.md

- [ ] **Create `CODE_OF_CONDUCT.md` using the Contributor Covenant v2.1**
  - The industry standard: https://www.contributor-covenant.org/version/2/1/code_of_conduct/
  - Replace placeholder contact info with a real enforcement email
  - GitHub provides a wizard: Settings > Code and automation > Community standards

### GitHub Issue & PR Templates

- [ ] **Create `.github/ISSUE_TEMPLATE/bug_report.md`**

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

- [ ] **Create `.github/ISSUE_TEMPLATE/feature_request.md`**

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

- [ ] **Create `.github/PULL_REQUEST_TEMPLATE.md`**

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

- [ ] **Add DCO requirement to CONTRIBUTING.md**
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

- [ ] **Install the DCO GitHub App** on the repo
  - https://github.com/apps/dco — automatically checks PRs for sign-off
  - Zero config, just install it on the repo/org

### CONTRIBUTING.md Updates

- [ ] **Add DCO section** (see above)
- [ ] **Add support expectations section**

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

- [ ] **Review Go version prerequisite** — currently says "Go 1.21+" but `go.mod` requires `go 1.25.4`. Update to match.
- [ ] **Add link to Code of Conduct** (once created)
- [ ] **Add link to Security Policy** (once created)

### GitHub Org & Repo Settings

- [ ] **Make `oakwood-commons` org public** (if not already)
- [ ] **Add org profile**: description, avatar, README at `.github/profile/README.md`
- [ ] **Enable GitHub Discussions** on the repo for Q&A and general discussion
- [ ] **Configure repository topics/tags** (e.g., `go`, `cli`, `scaffolding`, `cel`, `devtools`, `configuration`)
- [ ] **Set repository description** and website URL (if docs site exists)

### Branch Protection

- [ ] **Enable branch protection on `main`**
  - Require pull request reviews (at least 1 reviewer)
  - Require status checks to pass (lint-and-test)
  - Require conversation resolution
  - Require signed commits (optional, but recommended for OSS)
  - Disallow force pushes
  - Disallow deletions

---

## P2 - Nice to Have for Launch

### CODEOWNERS

- [ ] **Create `.github/CODEOWNERS`**

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
- [ ] **Shell completion** — ensure `scafctl completion bash/zsh/fish/powershell` works and document it
- [ ] **AUR package** (Arch Linux) — community can contribute this

### GoReleaser Cleanup

- [ ] **Review GCS blob config** — remove `blobs` section if not needed for public releases
- [ ] **Add GitHub release notes generation** to goreleaser if not present
- [ ] **Verify GPG signing works** or remove signing config if not desired for initial release
- [ ] **Consider adding a `changelog` section** using conventional commits:
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

- [ ] **Add CodeQL / security scanning** workflow
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
- [ ] **Add DCO check** to CI (if using DCO GitHub App, this is automatic)
- [ ] **Consider adding `go vet` and `staticcheck`** to PR checks (if not already in golangci-lint config)

### Testing Namespace

- [ ] **Spin up a testing namespace in Quay** (or your OCI registry) for integration tests
- [ ] **Document how contributors can run integration tests** that require registry access, or provide mocks

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
