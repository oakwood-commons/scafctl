---
title: "Information Command Taxonomy (get / explain / inspect)"
weight: 10
---

# Information Command Taxonomy Decision

## Status

Accepted and implemented. The cleanup (retire `explain solution`, delete dead
`explain provider`, remove empty `get authhandler/`, relocate `get resolver
refs` -> `eval refs`) and the `inspect solution --usage` view landed together.

## Problem Statement

scafctl accumulated several overlapping commands for "tell me about X". Auditing
the surface revealed genuine duplication and a few misfiled commands:

1. **`solution` is overloaded across four verbs**:
   - `get solution` -- lists the catalog / shows a light summary.
   - `explain solution` -- fixed-text dump of the solution's structure.
   - `inspect solution` -- kvx-rendered view of the **same** structure, plus the
     inferred run command.
   - `diff solution` -- pairwise structural comparison.

   `explain solution` and `inspect solution` build the **same** data object
   (`pkg/solution/inspect.BuildSolutionExplanation` -> `SolutionExplanation`)
   and differ only in renderer (fixed `Writer` text vs. the kvx `OutputOptions`
   pipeline). `inspect solution`'s own help text already states it supersedes
   `explain solution`. This is historical duplication.

2. **`explain provider` is dead code**: it is defined
   (`pkg/cmd/scafctl/explain/provider.go`) but deliberately never wired into the
   command tree, because "`explain provider`" (a provider *instance*) collides
   with "`explain provider`" (the schema-browser lookup of the *provider kind*).
   At runtime, `explain provider` resolves to the **schema** view, not the
   instance -- so the instance command is unreachable.

3. **`get resolver refs` is mis-verbed**: it extracts resolver references from a
   template/expression string. It neither lists nor shows a "resolver", so it
   does not belong under the `get` verb.

4. **`get authhandler/` is empty dead code** (no Go source).

The net effect: a user asking "how do I understand/consume a solution?" faces
three similar commands (`get`, `explain`, `inspect`), none of which is clearly
the user-facing "how do I run this?" view (the gap tracked by issue #300).

## Prior Art

Best-in-class CLIs separate these concerns into distinct verbs:

- **kubectl**: `explain TYPE` = schema/field docs for a *kind*; `describe NAME`
  = detailed state of an *instance*; `get` = list/retrieve instances.
- **docker**: `image inspect` / `container inspect` = instance structure.
- **helm**: `helm show values/chart/readme <chart>` = user-facing "how do I use
  this chart"; `helm get` = instance state.

The consistent pattern is: one verb for **schema-of-a-kind**, one for
**instance detail**, one for **listing**. scafctl already half-follows this --
the cleanup makes it consistent.

## Decision

Adopt a three-verb taxonomy for information/discovery commands, separated by
**what they operate on**:

| Verb | Meaning | Operates on | Output |
|------|---------|-------------|--------|
| `get <kind> [name]` | list many, or show one by name | a kind (list) or one instance (summary) | kvx |
| `explain <kind>[.field]` | schema documentation for a kind | a **kind** (Go-struct-tag schema) | kvx (target) |
| `inspect <resource>` | full structure/metadata of a specific instance | an **instance** | kvx |

Rules that follow from this split:

1. **`explain` means "schema of a kind" only.** Remove the instance misfits:
   retire `explain solution` and delete the dead `explain provider`. After this,
   `explain <kind>` has exactly one meaning and no naming collision.
2. **`inspect` is the instance view.** It absorbs what `explain solution` did
   (kvx-native, already the reference implementation). The user-facing "usage"
   view for issue #300 is a **projection of `inspect`** (e.g. an
   `inspect solution --usage` view or an `inspect` usage projection), **not** a
   new top-level command. This avoids inventing a new convention and avoids
   repurposing `explain`.
3. **`get` stays "list a kind, or show one by name."** It keeps the catalog
   listing role. Relocate `get resolver refs` out of `get` (it is an
   extraction/eval operation, not a get). Remove the empty `get authhandler`.

### Why the #300 usage view lives under `inspect`

- `inspect`'s charter is already "inspect the structure and metadata of
  resources with full kvx output support." A user-facing usage view *is*
  inspecting a solution's metadata + parameters + actions so it can be consumed
  correctly -- squarely within the existing intent, not a repurpose.
- A dedicated top-level `usage` verb would apply only to solutions today, which
  is too narrow to justify a new convention. The `inspect` projection pattern
  generalizes to other resources later.
- `explain` (schema) stays untouched and unambiguous.

## Consequences

**Removed / relocated (fewer commands, not more):**

- `explain solution` -- retired (its role is `inspect solution`). Breaking; a
  deprecation alias may bridge one release (see Sequencing).
- `explain provider` (instance) -- deleted as dead/unreachable code.
- `get authhandler/` -- removed (empty).
- `get resolver refs` -- relocated to a verb that matches its behavior
  (extraction), e.g. under `eval` or a `resolver` group.

**Added:**

- A user-facing usage projection under `inspect` for issue #300 (synopsis,
  parameters with defaults/allowed values, per-action run commands), rendered
  via the kvx pipeline so table/json/yaml/TUI come for free.

**Unchanged:**

- `get` listing/summary behavior and catalog integration.
- `explain <kind>` schema browser for kinds.
- `eval` (expression sandbox) and `snapshot` (execution artifacts) -- confirmed
  distinct, out of scope.
- `diff solution` -- distinct pairwise comparison.

## Sequencing

This decision spans more than issue #300. Suggested split into separate PRs:

1. **Taxonomy cleanup** (its own issue/PR): delete dead `explain provider` and
   empty `get authhandler`; relocate `get resolver refs`; retire (or deprecate
   -> alias) `explain solution` in favor of `inspect solution`. Migrate docs,
   examples, and tests. Update `docs/design/cli.md`.
2. **#300 usage view** (layered on the cleaned base): add the `inspect` usage
   projection with the data-building logic in `pkg/solution/inspect` (extend
   `BuildRunCommand`), an optional `metadata.usage` enrichment block, and
   graceful fallback for `when`-clause value discovery.

Breaking changes are acceptable in this pre-production project; note them
explicitly in the PR titles/bodies (squash-merge repo).
