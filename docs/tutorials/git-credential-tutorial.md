---
title: "Git HTTPS Credential Helper Tutorial"
weight: 42
---

# Git HTTPS Credential Helper Tutorial

This tutorial shows how to use `scafctl git-credential` to authenticate `git clone`, `git fetch`, and `git push` over HTTPS using `scafctl`'s authentication handlers and AES-256-GCM encrypted secrets store -- with zero plaintext Personal Access Tokens in `.gitconfig` or `.git-credentials`.

## Prerequisites

- `scafctl` installed and available on your `PATH`
- `git` installed
- Familiarity with the [Authentication Tutorial](auth-tutorial.md) and [Auth Profiles Tutorial](auth-profiles-tutorial.md)

## Overview

When Git accesses an HTTPS remote, it invokes configured credential helpers over a line-oriented `key=value` protocol on stdin and stdout. `scafctl git-credential` bridges Git directly to your active `scafctl` identities:

- **Dynamic OAuth token resolution** -- mints and refreshes short-lived tokens on demand from `github` (`github.com`, `gist.github.com`, and configured GitHub Enterprise Server hosts), `entra` (`dev.azure.com`, `*.visualstudio.com`), `gcp` (`source.developers.google.com`), and `auth.customOAuth2` handlers with `gitHost`
- **Multi-org and multi-profile routing** -- sets `useHttpPath = true` in `.gitconfig` so repositories under `/work-org/...` and `/public-org/...` on the same host automatically route to separate auth profiles via `auth.git.rules`
- **Safe `store` and `erase` semantics** -- `store` and `erase` are strict no-ops for auth-handler-backed hosts so `git fetch` never overwrites OAuth tokens and a `403` push rejection never wipes your login session; non-handler HTTPS hosts fall back to `scafctl`'s encrypted secrets store

## Quick Start

### 1. Authenticate with GitHub (or another handler)

~~~bash
scafctl auth login github
~~~

By default, `scafctl auth login github` requests `repo`, `read:org`, and `read:packages` scopes so both private `git clone`/`git push` and OCI catalog access work from a single login.

### 2. Configure Git

~~~bash
scafctl git-credential install
~~~

This updates `~/.gitconfig` with per-host entries for `https://github.com`, `https://gist.github.com`, `https://dev.azure.com`, `https://source.developers.google.com`, and any additional hosts declared in your `scafctl` configuration:

~~~ini
[credential "https://github.com"]
	helper = 
	helper = !"/usr/local/bin/scafctl" git-credential
	useHttpPath = true
~~~

Notice two details in the generated configuration:

1. The leading empty `helper =` resets any inherited system-level helper for that host so Git calls `scafctl` deterministically without prompting a keychain dialog first.
2. `useHttpPath = true` instructs Git to include `path=<owner>/<repo>.git` in credential requests so path-prefix rules can select different profiles per organization.

Existing foreign helpers (such as `!gh auth git-credential` or `osxkeychain` configured on the same host section) are never overwritten unless you pass `--force`.

### 3. Clone and Push over HTTPS

~~~bash
git clone https://github.com/acme-corp/platform-service.git
cd platform-service
git push origin main
~~~

## Multi-Account Routing (`auth.git.rules`)

If you work across multiple GitHub organizations using separate accounts (for example `work-org` under a work profile and `public-org` under a personal profile), authenticate both profiles and add `auth.git.rules` to `~/.config/scafctl/config.yaml`:

~~~bash
scafctl auth login github --profile work
scafctl auth login github --profile personal
~~~

~~~yaml
auth:
  git:
    rules:
      - host: github.com
        pathPrefix: work-org
        handler: github
        profile: work
      - host: github.com
        pathPrefix: public-org
        handler: github
        profile: personal
~~~

Routing precedence:

- **Handler**: `--handler` flag > `auth.git.rules` (exact lowercase host + longest `/`-segment-boundary `pathPrefix` after trimming `/` and `.git`) > built-in host inference (`github.com`, `gist.github.com`, configured GHES hostnames, `dev.azure.com`, `*.visualstudio.com`, `source.developers.google.com`, `auth.customOAuth2[].gitHost`)
- **Profile**: `--profile` flag > `--auth-profile` / `SCAFCTL_AUTH_PROFILE` > matched rule `profile` > single matching GHES profile > `auth.<handler>.activeProfile`
- **Scope**: matched rule `scope` > handler Git default (`499b84ac-1321-427f-aa17-267ca6975798/.default` for `entra`, `https://www.googleapis.com/auth/cloud-platform` for `gcp`, login-time scopes for `github`)
- **Username**: matched rule `username` > handler convention (`x-access-token` for `github`, `oauth2accesstoken` for `gcp`, `oauth2` for `entra`, or `gitUsername` on `customOAuth2`) > request `username` > `oauth2`

## Custom Self-Hosted Git Servers (`auth.customOAuth2`)

Register any OAuth2-capable self-hosted Git service (such as GitLab, Gitea, or Forgejo) with `--git-host` and `--git-username`:

~~~bash
scafctl auth oauth2 add gitlab-internal \
  --display-name "Internal GitLab" \
  --authorize-url https://git.example.com/oauth/authorize \
  --token-url https://git.example.com/oauth/token \
  --client-id your-gitlab-app-id \
  --default-flow interactive \
  --scopes read_repository,write_repository \
  --git-host git.example.com \
  --git-username oauth2

scafctl auth login gitlab-internal
scafctl git-credential install
~~~

## Install Scope Options

~~~bash
# Configure only specific hosts or repository sub-paths
scafctl git-credential install --host github.com --host https://git.example.com/acme-corp

# Configure only the current repository (.git/config)
scafctl git-credential install --local

# Write to an explicit gitconfig file and emit structured JSON
scafctl git-credential install --file ~/.gitconfig.work -o json

# Configure global credential.helper for all HTTPS hosts (including static encrypted fallback)
scafctl git-credential install --global-helper

# Also install a git-credential-scafctl symlink or shim in ~/.local/bin
scafctl git-credential install --shim --bin-dir ~/.local/bin
~~~

## Verification and Troubleshooting

Test credential resolution directly from the terminal:

~~~bash
printf 'protocol=https\nhost=github.com\npath=work-org/repo.git\n\n' | scafctl git-credential get
~~~

### Re-authentication Required

Because `git` runs credential helpers without an interactive TTY, `scafctl git-credential get` cannot launch a browser login mid-clone. When a session is missing or expired, it prints an actionable hint to stderr:

~~~text
no valid credentials for https://github.com/work-org/repo.git: run 'scafctl auth login github --profile work' to re-authenticate (interactive login cannot run inside a credential-helper subprocess)
~~~

### Plain HTTP Remotes

`scafctl git-credential` refuses to send credentials over unencrypted `http://` except to loopback addresses (`localhost`, `127.0.0.1`, `::1`) unless `auth.git.allowInsecureHttp: true` is explicitly set in `config.yaml`.

## Embedding in Custom CLIs

CLIs built via `scafctl.Root(&scafctl.RootOptions{BinaryName: "mycli", ...})` automatically inherit `mycli git-credential`, `mycli.gitcred.*` secret isolation, `# mycli-managed forwarding shim` verification, and `git-credential-mycli` alias dispatch via `gitcredential.RewriteAliasArgs(os.Args)`.

## Uninstall

~~~bash
# Remove all scafctl-managed credential entries from ~/.gitconfig (preserves foreign helpers)
scafctl git-credential uninstall

# Also remove a managed symlink or forwarding shim
scafctl git-credential uninstall --shim
~~~
