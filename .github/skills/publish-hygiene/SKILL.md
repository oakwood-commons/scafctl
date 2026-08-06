---
name: publish-hygiene
description: Use before posting anything to this public repository's external surfaces -- GitHub issues, PR titles/bodies/comments, commit messages, or committed docs/examples -- to check for and scrub any employer- or organization-specific references (internal tool names, internal hostnames/domains, internal project codenames, employee identifiers, internal infrastructure names, or other non-public organizational details) that a contributor's local environment or notes might otherwise leak into a public artifact.
---

# Publish Hygiene

This repository is public. Contributors work from many different employers,
personal setups, and local tooling configurations. None of that context
belongs in anything this repo publishes externally: GitHub issues, PR titles
and bodies, PR/issue comments, commit messages, or any content committed to
the repo (docs, examples, code comments, test fixtures).

This skill is intentionally **organization-agnostic** -- it names no specific
company, product, or internal tool. Apply the same checklist regardless of
which employer or environment you're working from.

## When to run this check

Before any of these actions:

- Creating or commenting on a GitHub issue
- Opening a PR or editing its title/body
- Writing a commit message
- Adding or editing a file that will be committed (docs, examples, fixtures,
  skills, agent/config files)

## What to scrub

Look for anything that identifies a specific non-public organization or
reveals its internal details, including:

- **Organization names** -- the literal name of an employer or internal team,
  in any casing or as part of a compound word (e.g. a company name fused into
  a tool, package, or hostname).
- **Internal tool/product names** -- proprietary or internal-only systems,
  platforms, or codenames that only make sense inside one organization.
- **Internal infrastructure** -- internal domains (anything other than
  clearly public services), internal hostnames, cluster/environment names,
  internal registries, VPN/network names, or internal IP ranges.
- **Identity details** -- corporate email domains, employee/user IDs, SSO
  tenant names, internal usernames, or anything that maps back to a specific
  person's employer.
- **Credentials and secrets** -- tokens, keys, connection strings, or
  anything that should never appear in a public artifact regardless of whose
  it is.
- **Screenshots, logs, or pasted output** -- these often carry hostnames,
  usernames, or tool names baked into the image/text that a text scrub alone
  won't catch. Review these visually, not just with grep.

## How to scrub

Replace with neutral, clearly-fictional placeholders instead of deleting
context wholesale:

- A specific company/org name -> `example-corp`, `Acme Corp`, or similar
- An internal hostname/domain -> `example.com`, `internal.example.com`
- An internal cluster/environment name -> `cluster-a`, `staging-env`
- An internal registry -> `registry.example.com`
- A corporate email address -> a generic placeholder such as name-at-example-dot-com
- An internal tool/codename -> a generic functional description of what it
  does, not its internal name (e.g. "the internal CI system" rather than a
  proper name)

Keep the technical substance of the report/issue/PR intact -- only the
identifying details change. A reader should be able to reproduce or
understand the issue using only public information after the scrub.

## Quick self-check before publishing

1. Would this text make sense to someone with zero knowledge of my employer?
2. Does it name, or allow inference of, a specific non-public organization?
3. Does it contain a hostname, domain, or identifier that isn't clearly a
   public, well-known service?
4. Are there credentials, tokens, or internal identifiers anywhere in the
   text, including inside code blocks, logs, or pasted command output?

If any answer raises doubt, scrub before publishing rather than after.

## Note on personal/local tooling

Some contributors' local AI-assistant or editor configurations enforce this
kind of check automatically as part of their own environment (for example, a
pre-publish hook that blocks a commit or PR containing a specific employer's
references). That kind of enforcement is personal machine configuration and
deliberately lives outside this repository -- it is not something this repo
should assume, require, or reference by name. This skill exists so the same
discipline is available to every contributor, regardless of their local
setup.
