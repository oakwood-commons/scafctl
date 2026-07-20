---
title: "CLI Command Grammar"
weight: 10
---

# CLI Command Grammar Decision

## Status

Accepted (direction). Implementation is sequenced across phased PRs; see
"Migration" below. This document is the ratified rule set that new commands must
follow and that the seam fixes below will bring existing commands into line with.

Tracks issue #649. Builds on the information-command taxonomy
(`docs/design/information-command-taxonomy.md`, `get`/`explain`/`inspect`).

## Problem

scafctl's command surface mixes command shapes with no documented rule, so users
cannot predict how to invoke a command, and new features have no obvious home.
A full survey (issue #649) found:

- **Two orderings coexist.** Workflow/inspection commands are verb-first
  (`run solution`, `render solution`, `get solution`, `inspect solution`), while
  subsystem-management commands are noun-first (`config get`, `auth login`,
  `catalog list`, `secrets set`). This is not itself wrong -- best-in-class CLIs
  do the same -- but it was never stated as an intentional rule, so the seams are
  inconsistent.
- **The `solution` noun straddles both lanes.** It is `<verb> solution` under six
  verbs AND a top-level `solution` group (`solution diff`). A user cannot predict
  whether `solution` comes first or second.
- **Inconsistent verb ergonomics.** Some operation verbs act directly
  (`lint [name]`), but others require an explicit sub-verb or noun, so a
  developer cannot rely on "the verb does the obvious thing." `test` runs
  functional tests when bare, which is good, but the surface around it is
  uneven.
- **A meta subcommand can collide with a target.** `lint rules`/`lint explain`
  are auxiliary operations, but there is no stated rule for how a positional is
  interpreted (target vs. subcommand), so behavior is implicit.
- **Hyphenated command tokens** (`get cel-functions`,
  `get go-template-functions`) pack two concepts into one token instead of
  expressing hierarchy by nesting.
- **Inconsistent no-args UX.** Some commands show help on bare invocation, some
  error (`run provider`), some act on an implicit target.

## Prior art

The verb-first / noun-first split is standard and tracks command *class*:

| Tool | Lifecycle / operations | Subsystem management |
|------|------------------------|----------------------|
| kubectl | `get`/`create`/`delete`/`describe`/`explain <resource>` (verb-first) | `config view`, `auth can-i` (noun-first) |
| docker | `container run`, `image ls` (noun-first, migrated with aliases) | same |
| gh | `pr create`, `repo view`, `issue list` (noun-first) | `auth login` |
| helm | `install`/`upgrade` (verb-first) | `repo add` (noun-first) |
| git | `commit`/`diff` (flat verbs) | `remote add`, `stash list` |

The takeaway: a mixed grammar is fine **when the split is by command class and is
consistent within each class**. scafctl already has a clean verb-first spine for
the workflow nouns and a clean noun-first spine for admin subsystems; the fix is
to codify that and repair the seams -- not to force a single global ordering
(which would be massive, low-value churn).

## Decision

### Lanes, chosen by command class

**Lane 1 -- Operation verbs: `<verb> [noun] [name] [flags]`**

Commands that *do something to a domain object* are verb-first. Within Lane 1
there are two shapes, distinguished by how many nouns the verb operates on:

- **Single-target verbs** act directly, defaulting to the solution -- the way
  `go test`, `cargo build`, `git commit`, and `npm test` work. A developer types
  the verb and it does the obvious thing.
  - Verbs: `lint`, `test`.
  - `scafctl test` tests the solution, `scafctl lint` lints it. A positional
    names a specific target (`scafctl test my-app`); `-f` always points at a
    file (`scafctl lint -f ./sol.yaml`).
  - A single-target verb **may also host closely-related sub-verbs** for
    auxiliary operations on the same domain (e.g. `test list`, `test init`,
    `lint rules`). This mirrors `git stash` / `git stash list`.

- **Multi-noun verbs** operate on more than one kind of object, so the noun is
  **explicit** -- these are parent dispatchers, not solution-defaulting verbs. A
  bare invocation shows help (it lists the nouns), not a default action.
  - Verbs and their nouns: `run solution|resolver|provider|action`,
    `render solution`, `get solution|provider|example|...`,
    `inspect solution`, `explain <kind>`, `validate solution|resolver|schema`,
    `diff solution`, `new solution`, `package solution|plugin`.
  - `scafctl run` shows help; `scafctl run solution` runs a solution.
    `diff` takes an explicit noun (`diff solution <a> <b>`) so it can grow to
    other diffable nouns (e.g. `diff snapshot`) later.

A new operation slots in obviously: validating arbitrary data against a schema
is `validate schema` -- a sibling of `validate solution` under the existing
multi-noun `validate` verb, not a new top-level command.

Note: some multi-noun verbs already accept a positional solution ref as a
convenience on their solution subcommand (e.g. `inspect solution my-app`). That
is the *noun's* argument, not a verb-level default; the noun is still required.

**Lane 2 -- Subsystem management: `<subsystem> <verb> [args] [flags]`**

Commands that *manage a subsystem or a collection of resources* -- where there
is no single "primary action," just a set of admin operations -- are noun-first.
The subsystem is a group; its children are verbs.

- Subsystems: `config`, `auth`, `catalog`, `secrets`, `state`, `plugins`,
  `cache`, `mcp`, `kube`, `snapshot`, `bundle`, `vendor`, `credential-helper`.
- Children are verbs: `list`, `get`, `set`, `delete`, `login`, `push`, `pull`, ...
- A bare subsystem shows help (it has no default action).

**Lane 3 -- Meta / self commands: `<verb> [flags]` (flat, top-level)**

Commands that operate on *the tool itself or the user's environment* -- not on a
domain object (Lane 1) and not on a managed subsystem (Lane 2) -- are flat
top-level verbs. They take no solution and have no noun.

- Verbs: `version`, `update` (self-update), `completion`, `options`, and future
  diagnostics such as `doctor` / `env` / `status`.
- These are the "tool about the tool" commands every mature CLI has
  (`brew doctor`, `gh extension upgrade`, `rustup update`, `kubectl completion`).
- Keep this lane small and obvious; if a command starts needing sub-verbs or a
  managed resource, it belongs in Lane 2 instead.

**Deciding the lane:** if the command *does something to a domain object*
("test it", "lint it", "run a solution", "diff two solutions"), it is an
operation verb (Lane 1). If it is a bag of management operations with no single
primary action ("manage config", "manage auth"), it is a subsystem (Lane 2). If
it operates on the tool or the environment itself ("update scafctl", "print the
version", "diagnose my setup"), it is a meta command (Lane 3). Within Lane 1,
a verb that has a single natural target defaults to the solution (`lint`,
`test`); a verb that spans several nouns (or is kept extensible) requires the
noun explicitly (`run`, `get`, `validate`, `diff`, ...).

### Rules that apply to both lanes

1. **Verbs act; be natural and discoverable.** Prefer what a developer would
   naturally type (`scafctl test`, not `scafctl test run`), following go / cargo
   / git / npm conventions. When the exact command is not obvious, it must be
   discoverable by walking the help tree (`scafctl --help` ->
   `scafctl test --help` -> `scafctl test list --help`).
2. **Ambiguity rule for sub-verbs.** A positional argument to an operation verb
   is treated as a **target** unless it exactly matches a known subcommand name;
   `-f` always forces a file target. So `scafctl test list` runs the `list`
   sub-verb, while `scafctl test -f ./list` tests the file `./list`. Keep
   sub-verb names short and unlikely to collide with real solution names, and
   document them.
3. **Do not hyphenate two separately-meaningful command tokens; nest instead.**
   A hyphen that joins two concepts (a namespace + a thing, or a verb + an
   object) is hiding a hierarchy -- express it by nesting: `get cel functions`,
   not `get cel-functions`. A hyphen is acceptable only when it is part of a
   single established compound term (`credential-helper`, `go-template`) -- the
   same way git keeps `cherry-pick`. Hyphenated *flag* names are always fine.
4. **Bare invocation never errors with a raw cobra message.** A subsystem, a
   multi-noun verb (a parent that dispatches to nouns), or a single-target verb
   with no discoverable target shows help or a clear, actionable error (see the
   silent-failure fix in `inspect solution`, PR #646) -- not
   `requires at least 1 arg`.
5. **Schema/kind documentation lives under `explain`.** The schema of a kind and
   its field docs are `explain <kind>` (e.g. `explain solution`, `explain
   provider`). Sub-verbs that are auxiliary operations of a *specific verb* stay
   under that verb (e.g. `lint rules` lists the lint engine's rules; it belongs
   to `lint`, not to `explain`).

## Seam fixes (bringing existing commands into line)

| Seam | Today | Target | Rationale |
|------|-------|--------|-----------|
| `solution` straddle | `solution diff <a> <b>` (top-level noun group) | `diff solution <a> <b>` | `diff` becomes an operation verb; retires the top-level `solution` group. |
| lint ergonomics | `lint [name]` + `lint rules` + `lint explain <rule>` | `lint [solution]` (acts by default) + `lint rules` (list) + `lint rule <name>` (detail) | `lint` acts by default; its rule-metadata stay as natural sub-verbs under `lint` (git-stash-style). `lint explain <rule>` -> `lint rule <name>` (nested noun, no hyphen). Positional-vs-subcommand resolved by the ambiguity rule. |
| test ergonomics | `test [ref]` (runs functional) + `test functional/init/list` | `test [solution]` (acts by default, runs functional tests) + `test list` + `test init` | `test` acts by default like `go test`/`cargo test`; `functional` folds into the default (aliased); `list`/`init` remain natural sub-verbs. |
| `auth handlers` | `auth handlers [name]` (lists) + `install`/`remove` | `auth handlers list` / `auth handlers install <h>` / `auth handlers remove <h>` | `auth` is a subsystem (Lane 2), so `handlers` is a sub-subsystem whose children are verbs -- explicit, no bare-acts. |
| `serve` + openapi | `serve` (starts server) + `serve openapi` | `serve` stays a pure leaf (starts the server); OpenAPI export moves to `get openapi`. | Starting the server is `serve`'s one obvious action (leaf); exporting the generated spec is a `get` of an artifact, not a sub-mode of the running server. |
| Hyphenated `get cel-functions` / `get go-template-functions` | verb + hyphenated token | `get cel functions` / `get go-template functions` (nested) | Listing available functions is the `get` lane (like `get provider`, `get examples`). The hyphen joins a namespace (`cel`) and a thing (`functions`) -- nest it. `go-template` stays hyphenated as a single established term. |
| `run provider` no-args | `requires at least 1 arg` | Show help (or a clear error) | Rule 4. |
| New: schema/data validation | (does not exist) | `validate schema --schema <s> --data <d>` | The concrete driver for #649; lands cleanly in the operation-verb lane. |

Note the two existing "validate" surfaces are reconciled by the lanes:
`validate <noun>` (solution/resolver/schema) is the operation-verb form;
`eval validate` (CEL/template syntax) stays under the `eval` sandbox subsystem
because it validates ad-hoc expressions, not domain artifacts.

## Future fit

A grammar is only useful if new commands slot in without debate. The
capabilities scafctl is likely to grow -- several already requested -- map onto
the three lanes as follows. This is illustrative, not a commitment to build.

| Anticipated capability | Lane / form | Prior art |
|------------------------|-------------|-----------|
| Self-update (#239) | Lane 3: `update` (alias `upgrade`) | `brew upgrade`, `rustup update` |
| Setup diagnostics | Lane 3: `doctor` / `env` | `brew doctor`, `npm doctor` |
| Shell completion | Lane 3: `completion` (exists) | universal |
| Inspect a provider (#319) | Lane 1 multi-noun: `inspect provider <name>` | `kubectl describe` |
| Replay / verify / drift (#242) | Lane 1: `verify solution`, `replay solution` | `terraform plan` |
| Format a solution file | Lane 1 single-target: `fmt` (defaults to solution) | `gofmt`, `terraform fmt` |
| Edit in `$EDITOR` | Lane 1 single-target: `edit` | `kubectl edit` |
| Live-follow output | Flag, not a verb: `-w`/`--watch` on operation verbs | `kubectl get -w` |
| Execution logs / history | Lane 1 or `run` sub-verb: `logs` / `run logs` | `docker logs` |
| Graph / visualize | Extend existing `render solution -o mermaid` | `terraform graph` |
| Plugins search (#535) | Lane 2: `plugins search` | `brew search` |
| MCP inspector (#419) | Lane 2: `mcp tools` / `mcp call` / `mcp ping` | -- |
| Schema/UI generation (#243) | Lane 1 multi-noun: `get schema` / `generate schema` | -- |

**Open question -- `pipeline` (#60):** catalog-regression testing does not fit
cleanly. Options: a `test` sub-verb (`test catalog`), a Lane 2 subsystem
(`pipeline run`/`test`), or fold into `catalog` (`catalog test`). Decide when
that feature is designed; noting it here so the grammar is not assumed to have
already answered it.

## Consequences

- **Breaking CLI-path changes** (pre-production, squash-merge -- acceptable, noted
  per change). Each moved command ships with a **deprecated alias** forwarding to
  the new path for one release where practical, so scripts and muscle memory get
  a transition window; aliases are removed in a later major.
- New commands have an unambiguous home: pick the lane by class, then the verb
  (Lane 1) or subsystem+verb (Lane 2).
- Operation verbs are natural to type -- `scafctl test`, `scafctl lint`,
  `scafctl run` all do the obvious thing -- with auxiliary sub-verbs discoverable
  in each verb's help.
- The top-level `solution` group is retired (its sole child `diff` moves to
  `diff solution`); this resolves the #648 placement question.

## Migration (phased PRs)

This is too large for one PR. Suggested sequence, each its own PR with aliases:

1. **Grammar doc** (this document) -- ratify the rule set. No code.
2. **`diff solution`** -- add the operation verb, alias `solution diff` -> it,
   retire the top-level `solution` group. Resolves #648.
3. **`validate schema`** -- implement the schema/data validation command (the
   driver). Reconcile the `validate` lane.
4. **Tidy `lint`** -- keep `lint [solution]` acting by default; rename
   `lint explain <rule>` -> `lint rule <name>` and keep `lint rules` (list);
   apply the ambiguity rule. Alias old paths.
5. **Tidy `test`** -- keep `test [solution]` acting by default (running
   functional tests), fold `test functional` into the default (alias), keep
   `test list`/`test init`. De-dual-mode `auth handlers` (-> explicit verbs) and
   move `serve openapi` -> `get openapi`. One PR or split as needed.
6. **De-hyphenate** `get cel-functions` / `get go-template-functions` ->
   `get cel functions` / `get go-template functions` (nested). Alias old paths.
7. **No-args UX sweep** -- ensure every bare command shows help or acts on its
   default target; fix `run provider` and audit the rest.

Each PR updates `docs/design/cli.md`, integration tests, and MCP tool
descriptions that reference the moved paths, and notes the breaking change and
alias in its title/body.
