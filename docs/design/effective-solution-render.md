---
title: "Effective Solution Rendering"
weight: 19
---

# Effective Solution Rendering

## Overview

`scafctl render solution --effective` emits the **effective** solution document:
the fully-composed, canonicalized solution as it exists after `compose:` partials
are merged, serialized deterministically to YAML or JSON. It performs **no
resolver execution and no provider calls** -- it is a pure document transform.

This gives users a stable, diffable view of "what solution am I actually running"
when a solution is split across multiple files via the `compose:` field. It is the
scafctl analogue of `docker compose config`, `helm template`, and
`kustomize build`: tools that render the merged/effective configuration document
without applying it.

## Problem Statement

The `compose:` feature (see [Solutions](solutions.md)) lets authors split a
solution across multiple partial YAML files that are merged at load time. This is
good for organization but creates a visibility and fidelity gap:

1. **No canonical view of the merged result.** Nothing prints the post-compose
   document. `inspect solution` is a lossy projection (metadata summary),
   `render solution` (default/`--action-graph`/`--snapshot`) all **execute
   resolvers** and emit an execution plan -- not the source document.
2. **No fidelity check for composition.** When a solution is refactored (e.g.
   inlining a partial, or reorganizing which file a resolver lives in), there is
   no cheap, deterministic way to prove the *effective* solution did not change.
   `diff solution` compares actions shallowly (name + provider + description) and
   still requires two solution files rather than a golden artifact.
3. **Hard to review composition in CI.** Reviewers cannot see the net effect of a
   compose change without mentally merging the partials.

## Design

### Command surface

```
scafctl render solution --effective [--section all|workflow|resolvers] [-o yaml|json] [--compact]
```

- `--effective` -- select effective-document mode. Mutually exclusive with
  `--action-graph` and `--snapshot`.
- `--section` -- scope the output (only valid with `--effective`):
  - `all` (default) -- the whole composed solution document.
  - `workflow` -- only `spec.workflow`.
  - `resolvers` -- only `spec.resolvers`.
- `-o yaml` (default) or `-o json`. `-o test` is rejected in this mode.
- `--compact` -- single-line JSON (JSON only).

### Domain package

Business logic lives in the domain package `pkg/solution/effective`, not in the
CLI command (per the repository's layering rules). The CLI handler and the MCP
tool are both thin wrappers over it.

```go
// pkg/solution/effective
func Render(sol *solution.Solution, opts Options) ([]byte, error)

type Options struct {
    Section Section // all | workflow | resolvers
    Format  Format  // yaml | json
    Compact bool
}
```

`Render` projects the requested section out of the already-loaded (already
composed) `*solution.Solution` and marshals it. It never resolves, never dials a
provider, and never touches the filesystem beyond the initial load performed by
the caller.

### Why no execution

The value of this feature is fidelity, and fidelity requires determinism.
Executing resolvers would make output depend on environment, time, network, and
provider state -- defeating golden-file comparison. Compose merging is a pure,
deterministic transform, so the effective document is a pure function of the
input files.

### Determinism guarantee

Both `gopkg.in/yaml.v3` and `encoding/json` marshal Go maps in sorted-key order
and structs in field-declaration order, so the serialized output is byte-stable
for a given input. `compose:` merges resolvers and actions **by name** (rejecting
duplicates) into maps, and the merged result clears its own `compose:` field
(the document is self-contained). The `Deterministic` test asserts repeated
renders of every section/format combination are byte-identical.

## Fidelity workflow

The intended use is golden-file diffing in CI:

```bash
# Capture the effective document as a golden artifact (once, reviewed).
scafctl render solution -f ./solution.yaml --effective -o yaml > golden.effective.yaml

# In CI, regenerate and diff. A non-empty diff means composition changed.
scafctl render solution -f ./solution.yaml --effective -o yaml \
  | diff -u golden.effective.yaml -
```

Because the output is deterministic, any change to the merged solution --
including one introduced by editing a `compose:` partial -- shows up as a diff,
while pure reorganizations that do not change the effective document produce no
diff.

## MCP tool

The `render_effective_solution` MCP tool exposes the same capability to agents:
inputs `path`, `section` (`all|workflow|resolvers`), `format` (`yaml|json`), and
`cwd`; it returns the rendered document as text. It is marked read-only and
non-open-world (no network, no execution).

## Alternatives considered

- **Deepen `diff solution` instead.** Making `diff solution` compare full action
  bodies would help two-file comparisons but still would not produce a stable
  golden artifact, and it conflates "compare two solutions" with "snapshot one
  solution." Deepening the diff is a worthwhile but **separate** change.
- **Add a `mode: effective` to the existing action-graph renderer.** Rejected:
  the action-graph renderer executes resolvers by construction; bolting a
  no-execution path onto it would blur its contract. A dedicated flag and domain
  package keep the two behaviors clearly separated.

## Out of scope

- Deepening `diff solution` field-level comparison.
- Resolving/normalizing expression values (the effective document keeps
  expressions verbatim; it is a source-fidelity view, not an evaluated one).
