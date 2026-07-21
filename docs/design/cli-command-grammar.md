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
(`docs/design/information-command-taxonomy.md`), which introduced
`get`/`explain`/`inspect`; this document supersedes the `inspect` verb by
folding instance detail into `get` (Rule 3).

## Problem

scafctl's command surface mixes command shapes with no documented rule, so users
cannot predict how to invoke a command, and new features have no obvious home.
A full survey (issue #649) found:

- **Two orderings coexist with no stated rule.** Workflow/inspection commands are
  verb-first (`run solution`, `render solution`, `get solution`), while
  subsystem-management commands are noun-first (`config get`, `auth login`,
  `catalog list`). A mixed grammar is fine -- best-in-class CLIs do it -- but the
  split was never defined, so the seams are inconsistent.
- **Artifact nouns straddle both lanes.** `solution`, `bundle`, and `snapshot`
  are user artifacts, yet they appear inconsistently: `solution` is `<verb>
  solution` under six verbs AND a top-level `solution diff` group; `bundle` and
  `snapshot` are top-level noun groups (`bundle diff/verify/extract`,
  `snapshot diff/show`). So `diff` is spelled three different ways
  (`solution diff` vs `bundle diff` vs `snapshot diff`) and a user cannot predict
  whether the artifact comes first or second.
- **Overlapping check-verbs proliferate.** `validate` (root), `verify` (under
  `bundle`), and `lint` all "check something" and read as synonyms, so a user
  cannot tell which one gates their build; the surface was never rationalized.
- **Inconsistent terminology.** Go templates are called `template` in one place
  (`eval template`) and `go-template` in another (`get go-template-functions`).
- **Inconsistent verb ergonomics.** Some operation verbs act directly
  (`lint [name]`); others require an explicit sub-verb or noun.
- **Hyphenated command tokens** (`get cel-functions`,
  `get go-template-functions`) pack two concepts into one token instead of
  expressing hierarchy by nesting.
- **Inconsistent no-args UX.** Some commands show help on bare invocation, some
  error (`run provider`), some act on an implicit target.

## Prior art

The verb-first / noun-first split is standard and tracks command *class*:

| Tool | Operations on objects | Subsystem management |
|------|------------------------|----------------------|
| kubectl | `get`/`describe`/`explain`/`diff <resource>` (verb-first) | `config view`, `auth can-i` (noun-first) |
| docker | `container run`, `image ls` (noun-first, migrated with aliases) | same |
| gh | `pr create`, `repo view` (noun-first) | `auth login` |
| helm | `install`/`upgrade` (verb-first) | `repo add` (noun-first) |
| git | `commit`/`diff` (flat verbs) | `remote add`, `stash list` |

The takeaway: a mixed grammar is fine **when the split is by command class and is
consistent within each class**. The fix is to codify the split with a test that
predicts which lane any noun belongs in -- and repair the seams -- not to force a
single global ordering (massive, low-value churn).

## Decision

### The lane test: is the noun an OBJECT or a SUBSYSTEM?

Every noun in the CLI is either an **object** you act on or a **subsystem** you
operate within. This single distinction chooses the lane:

- **Object -> verb-first.** A noun is an *object* when you *do things to it*: it
  is typically an artifact the user made (a solution, a bundle, a snapshot, a
  provider definition), usually a single instance, and the verbs that act on it
  (`diff`, `validate`, `get`, `extract`) **generalize across several objects**.
  The operation is the point; the object is its target. Read it as `<verb>
  <object>` -- "diff solution", "validate solution", "get snapshot".

- **Subsystem -> noun-first.** A noun is a *subsystem* when it is a bounded
  management context / a place with its own vocabulary: *the* catalog, *the*
  cache, *the* config -- a standing store or service, not an artifact -- whose
  verbs (`login`, `push`, `prune`, `set`) are **specific to it and do not
  generalize** to other nouns. You are not "doing X to the config"; you are
  "using config's own commands." Read it as `<subsystem> <verb>` -- "auth login",
  "catalog push".

**The tests, applied:**

| Test | Object (verb-first) | Subsystem (noun-first) |
|------|---------------------|------------------------|
| Is it a file/artifact the user made? | solution, bundle, snapshot -- yes | config, auth, catalog -- no |
| One instance, or a standing store/service? | one bundle, one snapshot | *the* catalog, *the* cache, *the* config |
| Do its verbs generalize to other nouns? | `diff`/`validate`/`get`/`extract` do | `catalog push`, `auth login` do not |
| Does `<verb> <it>` read naturally? | "diff solution" yes | "login auth" no, "list catalog" no |
| Fundamentally a management/admin surface? | no | yes |

`solution`, `bundle`, and `snapshot` all pass the object tests -- they are
artifacts, single instances, and share the same verbs -- so they are **objects,
verb-first**, and the top-level `solution`/`bundle`/`snapshot` groups are retired.
`catalog`, `config`, `auth`, `kube`, `cache`, `secrets`, `state`, `plugins`,
`mcp`, `eval`, and `vendor` all pass the subsystem tests -- standing surfaces with
bespoke verbs -- so they stay **noun-first**. You would never say "diff catalog"
or "login auth"; you *use* those subsystems, you do not act *on* them.

### Lane 1 -- Operation verbs on objects: `<verb> <object> [name] [flags]`

Verbs that *do something to an object* are verb-first. The same verb works across
many objects (it is polymorphic), which is what makes the grammar predictable:
learn the verb once, apply it to any object.

- **Polymorphic operation verbs:** `run`, `render`, `get`, `explain`,
  `validate`, `diff`, `extract`, `new`, `package`.
- **Objects:** `solution`, `bundle`, `snapshot`, `provider`, `resolver`,
  `action`, `schema`, `plugin`, ... A new object type slots straight in
  (`diff <newthing>`, `validate <newthing>`).
- **Single-target verbs** default to the solution -- the way `go test`,
  `cargo build`, and `git commit` work. These are `lint` and `test`:
  `scafctl test` tests the solution, `scafctl lint` lints it. A positional names
  a specific target; `-f` always points at a file. A single-target verb may host
  closely-related sub-verbs (`test list`, `lint rules`), git-stash-style.
- **Multi-noun verbs** span several objects, so the object is **explicit** and a
  bare invocation shows help (it lists the objects): `scafctl run` shows help;
  `scafctl run solution` runs a solution; `scafctl diff snapshot <a> <b>` diffs
  two snapshots.

Examples of the polymorphism this buys:

- `diff solution|bundle|snapshot` -- one verb, one spelling, every artifact.
- `validate solution|bundle|schema` -- one gate verb; each object defines what
  "correct" means for it (see Rule 2).
- `get solution|snapshot|bundle` -- list many, or one in full via arg + `-o`/
  `--detail`; a single verb for instances at any depth (see Rule 3).
- `extract bundle` (and future `extract <object>`) -- pull contents out of any
  extractable artifact.

### Lane 2 -- Subsystem management: `<subsystem> <verb> [args] [flags]`

Subsystems -- bounded management contexts with no single primary action -- are
noun-first. The subsystem is a group; its children are verbs.

- Subsystems: `config`, `auth`, `catalog`, `secrets`, `state`, `plugins`,
  `cache`, `mcp`, `kube`, `eval`, `vendor`, `credential-helper`.
- Children are verbs: `list`, `get`, `set`, `delete`, `login`, `push`, `pull`,
  `install`, `update`, `prune`, ...
- A bare subsystem shows help (it has no default action).

### Lane 3 -- Meta / self commands: `<verb> [flags]` (flat, top-level)

Commands that operate on *the tool itself or the user's environment* -- not on an
object (Lane 1), not on a subsystem (Lane 2) -- are flat top-level verbs.

- Verbs: `version`, `update` (self-update), `completion`, `options`, and future
  diagnostics such as `doctor` / `env` / `status`.
- Prior art: `brew doctor`, `gh extension upgrade`, `rustup update`,
  `kubectl completion`.
- Keep this lane small; if a command grows sub-verbs or a managed resource, it
  belongs in Lane 2.

**Deciding the lane:** run the object-vs-subsystem test. If the noun is an object
you act on, it is Lane 1 (verb-first). If it is a subsystem you operate within, it
is Lane 2 (noun-first). If the command targets the tool/environment itself, it is
Lane 3. Within Lane 1, a verb with a single natural target defaults to the
solution (`lint`, `test`); a verb spanning several objects requires the object
explicitly (`run`, `get`, `diff`, ...).

### Rules that apply across lanes

1. **Verbs act; be natural and discoverable.** Prefer what a developer would
   naturally type (`scafctl test`, not `scafctl test run`). When the exact
   command is not obvious, it must be discoverable by walking the help tree.
2. **One gate verb: `validate`. `lint` is its advisory subset. There is no
   `verify`.** "validate", "verify", and "lint" are near-synonyms in plain
   English; shipping all three would force users to guess which one gates their
   CI. So the checking surface is deliberately minimal, organized by *when in the
   lifecycle* a check happens, not by synonym:
   - **`lint <object>`** answers "am I *authoring this well*?" -- style, smells,
     deprecated fields, best-practice **warnings**. Advisory, fast, for the inner
     authoring loop. Standalone and optional; you never *need* to run it
     separately.
   - **`validate <object>`** is **the** gate: "is this *good*?" It is pass/fail,
     and it **runs lint internally** (lint findings surface as warnings; errors
     fail). Each object defines what "good" means for it -- `validate solution`
     (parses, schema-conforms, refs resolve), `validate schema` (data vs schema).
     This is the single command a user or CI runs to know their source is sound.
   - **Built-artifact completeness is not a verb.** Checking that a *built bundle*
     is complete (all files, globs, plugins present) rides on the two lifecycle
     transitions where it matters, automatically:
     - **Producer side -- `package`:** building an artifact completeness-checks
       its output; the build **fails** if the bundle is incomplete.
     - **Consumer side -- `install` / `pull`:** acquiring a bundle
       completeness-checks it before caching/running and **refuses or warns** on a
       broken artifact -- the way `npm install`, `apt`, and `docker pull` verify
       what they fetch. The consumer never runs a separate check verb.
     A `--strict` flag on `package` / `install` exposes deep checks for CI.
   - **Why this is enough.** To know an object is good, a user runs **one**
     command at the stage they are at: `validate` on source; nothing extra on a
     bundle (it is checked when produced and when acquired). A typical CI gate is
     two lines that both do real work -- `validate solution` then
     `package solution` -- with no dedicated "checking" commands to learn.
   - **Help-text contract:** `validate` Short must read as the gate (e.g.
     `Check a definition is correct and ready (runs lint; fails on errors)`);
     `lint` Short must state it is the advisory subset (e.g.
     `Report authoring warnings (a subset of 'validate'; advisory, non-fatal)`).
     This is enforced by review so users never read this file to pick a verb.
3. **Two information verbs, not three: `get` (data) and `explain` (schema); no
   `inspect`.** "get", "inspect", and "describe" all read as "show me the thing,"
   so scafctl keeps only the two that answer genuinely different questions:
   - **`get <object>`** returns *instances* -- at any depth. `get solution` lists
     many; `get solution my-app` shows that one; `get solution my-app -o yaml`
     (or `--detail`) shows it in full. "List vs. detail" is a *depth* axis
     expressed by the positional arg and output flags, not a separate verb -- the
     way `kubectl get pods` / `kubectl get pod x -o yaml` and `gh pr list` /
     `gh pr view` already work.
   - **`explain <kind>`** documents the *schema/type* of a kind (its fields and
     docs), independent of any instance (kubectl `explain`). This is a different
     question -- definition, not data.
   - There is no `inspect`/`describe` verb: it would duplicate `get`'s job on a
     depth axis `get` already covers. If a "resolved/computed" view is needed
     (effective config, resolved resolvers), it is a **flag on `get`**
     (`get solution --resolved`), not a new verb.
4. **One term per concept.** Go templates are `template` everywhere
   (`eval template`, `get template functions`); help text may clarify "Go
   template," but the command word is `template`. CEL is `cel`. Do not mix
   `template` and `go-template` as command tokens.
5. **`package` builds; `plugins` manages.** `package solution` and
   `package plugin` are the polymorphic *build-into-catalog* verb. The `plugins`
   subsystem manages the *local plugin cache* (`plugins install/update/prune/
   list/search`). They are different concerns (build vs. cache management), so
   both are correct; use `plugin` (singular) for the object, `plugins` for the
   management group.
6. **Ambiguity rule for sub-verbs.** A positional argument to an operation verb is
   a **target** unless it exactly matches a known subcommand; `-f` always forces a
   file target. So `scafctl test list` runs the `list` sub-verb, while
   `scafctl test -f ./list` tests the file `./list`.
7. **Do not hyphenate two separately-meaningful tokens; nest instead.**
   `get cel functions`, not `get cel-functions`. A hyphen is acceptable only for a
   single established compound term (`credential-helper`). Hyphenated *flag* names
   are always fine.
8. **Bare invocation never errors with a raw cobra message.** A subsystem, a
   multi-noun verb, or a single-target verb with no discoverable target shows help
   or a clear, actionable error -- not `requires at least 1 arg`.

## Seam fixes (bringing existing commands into line)

| Seam | Today | Target | Rationale |
|------|-------|--------|-----------|
| artifact straddle -- diff | `solution diff` / `bundle diff` / `snapshot diff` (three spellings) | `diff solution` / `diff bundle` / `diff snapshot` | `diff` is one polymorphic operation verb; retires the top-level `solution`/`bundle`/`snapshot` groups. |
| artifact straddle -- detail | `snapshot show <f>` | `get snapshot <f>` (with `-o`/`--detail`) | Snapshot is an object; showing its detail is `get` at depth, not a separate verb (Rule 3). |
| artifact straddle -- completeness | `bundle verify <ref>` (standalone check-verb) | folded into `package` (producer) and `install`/`pull` (consumer); `verify` retired | Completeness is a guarantee of building/acquiring an artifact, not a verb users invoke; kills the `validate`/`verify` synonym clash (Rule 2). |
| artifact straddle -- extract | `bundle extract <ref>` | `extract bundle <ref>` | Extraction as a polymorphic verb, ready for `extract <other object>`. |
| snapshot creation | `snapshot save` | `render solution --snapshot [file]` | Creating a snapshot is a *render output*, not an action on an existing snapshot; render already executes-and-captures. |
| lint ergonomics | `lint [name]` + `lint rules` + `lint explain <rule>` | `lint [solution]` (acts) + `lint rules` (list) + `lint rule <name>` (detail) | `lint` acts by default; rule-metadata stay as sub-verbs; de-hyphenate `explain <rule>` -> `rule <name>`. |
| test ergonomics | `test [ref]` + `test functional/init/list` | `test [solution]` (acts, runs functional) + `test list` + `test init` | `test` acts by default like `go test`; `functional` folds into the default (aliased). |
| `auth handlers` dual-mode | `auth handlers [name]` (lists) + install/remove | `auth handlers list` / `install <h>` / `remove <h>` | `auth` is a subsystem; `handlers` is a sub-subsystem whose children are verbs. |
| `serve` + openapi | `serve` + `serve openapi` | `serve` (leaf, starts server); export moves to `get openapi` | Exporting the spec is a `get` of an artifact, not a sub-mode of the server. |
| terminology | `eval template` vs `get go-template-functions` | `eval template` + `get template functions` | One term (`template`) per concept (Rule 4); de-hyphenate (Rule 7). |
| hyphenated `get cel-functions` | verb + hyphenated token | `get cel functions` (nested) | Rule 7. |
| `run provider` no-args | `requires at least 1 arg` | Show help (or a clear error) | Rule 8. |
| New: schema/data validation | (does not exist) | `validate schema --schema <s> --data <d>` | The concrete driver for #649; lands in the operation-verb lane. |

Note the two "validate" surfaces are reconciled by the lanes: `validate <object>`
(solution/resolver/schema) is the operation-verb form; `eval validate` (CEL/
template syntax) stays under the `eval` sandbox subsystem because it validates
ad-hoc expressions, not domain artifacts.

## Future fit

New commands must slot in without debate. Likely capabilities map onto the lanes
via the object-vs-subsystem test. Illustrative, not a commitment to build.

| Anticipated capability | Lane / form | Prior art |
|------------------------|-------------|-----------|
| Self-update (#239) | Lane 3: `update` (alias `upgrade`) | `brew upgrade`, `rustup update` |
| Setup diagnostics | Lane 3: `doctor` / `env` | `brew doctor`, `npm doctor` |
| Inspect a provider (#319) | Lane 1: `get provider <name>` (with `--detail`) | `kubectl get -o yaml` |
| Drift / replay (#242) | Lane 1: `diff solution` (against a snapshot), `replay solution` | `terraform plan` |
| Format a solution file | Lane 1 single-target: `fmt` (defaults to solution) | `gofmt`, `terraform fmt` |
| Edit in `$EDITOR` | Lane 1 single-target: `edit` | `kubectl edit` |
| Live-follow output | Flag, not a verb: `-w`/`--watch` on operation verbs | `kubectl get -w` |
| Plugins search (#535) | Lane 2: `plugins search` | `brew search` |
| MCP inspector (#419) | Lane 2: `mcp tools` / `mcp call` / `mcp ping` | -- |
| Schema/UI generation (#243) | Lane 1: `get schema` / `generate schema` | -- |

**Open question -- `pipeline` (#60):** catalog-regression testing does not fit
cleanly. Options: a `test` sub-verb (`test catalog`), a Lane 2 subsystem
(`pipeline run`/`test`), or fold into `catalog` (`catalog test`). Decide when that
feature is designed.

## Consequences

- **Breaking CLI-path changes** (pre-production, squash-merge -- acceptable, noted
  per change). Each moved command ships with a **deprecated alias** forwarding to
  the new path for one release where practical; aliases are removed in a later
  major.
- New commands have an unambiguous home: run the object-vs-subsystem test, then
  pick the verb (Lane 1) or subsystem+verb (Lane 2).
- The top-level `solution`, `bundle`, and `snapshot` groups are retired; their
  operations become polymorphic verbs (`diff`/`get`/`extract <object>`), and
  bundle completeness folds into `package`/`install` (`verify` retired). This
  resolves the #648 placement question.
- Operation verbs are natural to type and consistent across objects -- learn
  `diff` once, use it on every artifact.

## Migration (phased PRs)

Too large for one PR. Suggested sequence, each its own PR. This project is
pre-production, so phases that move commands choose a hard cutover (remove the
old path outright) over aliasing.

1. **Grammar doc** (this document) -- ratify the rule set. No code.
2. **`diff <object>`** -- add the polymorphic `diff` verb; hard-remove
   `solution diff` / `bundle diff` / `snapshot diff` (no aliases); retire those
   top-level groups. Resolves #648.
3. **`extract` / `get` for artifacts** -- move `bundle extract` ->
   `extract bundle`, `snapshot show` -> `get snapshot` (detail via `-o`/
   `--detail`); retire `inspect` (folded into `get`); hard-remove old paths
   (no aliases).
4. **Retire `verify`; fold completeness into `package` + `install`** -- remove the
   standalone `bundle verify`; make `package` fail on an incomplete build and
   `install`/`pull` refuse/warn on a broken bundle (`--strict` for deep checks).
5. **`validate` as the single gate** -- implement `validate schema` (the driver);
   make `validate` run `lint` internally; align help text per Rule 2.
6. **Tidy `lint`** -- `lint [solution]` acts; `lint explain <rule>` ->
   `lint rule <name>`; keep `lint rules`; apply the ambiguity rule. Alias.
7. **Tidy `test`** -- `test [solution]` acts (functional); fold `test functional`
   into the default; keep `test list`/`test init`. De-dual-mode `auth handlers`;
   move `serve openapi` -> `get openapi`.
8. **Terminology + de-hyphenate** -- `template` everywhere;
   `get cel functions` / `get template functions` (nested). Alias old paths.
9. **Snapshot creation** -- `snapshot save` -> `render solution --snapshot`.
10. **No-args UX sweep** -- every bare command shows help or acts on its default
    target; fix `run provider` and audit the rest.

Each PR updates `docs/design/cli.md`, integration tests, and MCP tool
descriptions that reference the moved paths, and notes the breaking change and
alias in its title/body.
