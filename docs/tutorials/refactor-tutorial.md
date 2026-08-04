---
title: "Refactoring Tutorial"
weight: 46
---

# Refactoring Tutorial

This tutorial covers `scafctl refactor`, which applies source-preserving edits to
a solution file. It can rename a **resolver** (`refactor rename resolver`), a
**workflow action** (`refactor rename action`), a reusable **call**
(`refactor rename call`), or an author-defined **function**
(`refactor rename function`), rewriting every reference to the renamed symbol.

## Overview

Renaming a resolver by hand is error-prone: a resolver can be referenced in
several different ways, and missing one leaves a dangling reference that breaks
the solution. `refactor rename resolver` finds and rewrites all of them in a
single, comment-preserving edit.

```mermaid
flowchart LR
  A["Solution<br/>YAML"] --> B["scafctl refactor<br/>rename resolver"] --> C["Same file,<br/>all references renamed"]
```

It updates every form of reference:

- the resolver definition (the map key under `spec.resolvers`)
- `dependsOn` entries
- `rslvr:` references
- CEL `_.name` uses (in inputs and in `when`/`until` conditions)
- explicit Go-template `._.name` uses

Only the exact bytes of each reference are replaced, so comments, key order, and
formatting are preserved -- there is no YAML round-trip.

## A solution to rename

The example solution at `examples/refactor/solution.yaml` references the
`environment` resolver four different ways:

~~~yaml
spec:
  resolvers:
    environment:                       # the definition
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    appName:
      dependsOn:
        - environment                  # dependsOn reference
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: '_.environment == "dev" ? "app-dev" : "app-prod"'  # CEL reference
    greeting:
      resolve:
        with:
          - provider: go-template
            inputs:
              template:
                tmpl: "Deploying {{ ._.appName }} to {{ ._.environment }}"  # template reference
    envAlias:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                rslvr: environment      # rslvr reference
~~~

## Preview a rename with --dry-run

Always preview first. `--dry-run` lists every occurrence that would change and
leaves the file untouched:

```bash
scafctl refactor rename resolver environment env \
  -f examples/refactor/solution.yaml --dry-run
```

```text
Would rename resolver "environment" to "env" (5 occurrence(s)):
  examples/refactor/solution.yaml:13:5   environment -> env
  examples/refactor/solution.yaml:23:11  environment -> env
  examples/refactor/solution.yaml:30:26  environment -> env
  examples/refactor/solution.yaml:39:60  environment -> env
  examples/refactor/solution.yaml:48:24  environment -> env
```

## Apply the rename

Drop `--dry-run` to write the change in place:

```bash
scafctl refactor rename resolver environment env -f examples/refactor/solution.yaml
```

```text
Renamed resolver "environment" to "env" (5 occurrence(s)) in examples/refactor/solution.yaml
```

The definition, the `dependsOn` entry, the CEL expression, the template, and the
`rslvr` reference are all updated; every comment and blank line is preserved.

If you omit `-f`, the command auto-discovers a solution file in the current
directory. Because rename modifies a file, discovery is strict: if more than one
solution file matches, the command errors and asks you to pass `-f`.

## The safety guarantee: it refuses partial renames

A rename either updates every reference or does nothing. If the tool cannot
locate a reference to the target resolver byte-exact, it aborts with a non-zero
exit code rather than performing a partial (and potentially breaking) rewrite.

References that cannot be positioned today include:

- a context-dependent bare template accessor, `{{ .name }}`
- a `$`-rooted template accessor, `{{ $.name }}`
- an inline reference nested inside a literal value (for example a `rslvr:`
  buried in a literal map or list)

If any such reference targets the resolver you are renaming, you will see:

```text
rename resolver: N reference(s) to "environment" could not be located; aborting to avoid a partial rename
```

This check is name-scoped: an unlocatable reference to a *different* resolver
does not block your rename. When a rename is refused, update the ambiguous
reference to an explicit form (`._.name` in templates, `_.name` in CEL) and try
again.

## Renaming actions

`refactor rename action <old> <new>` renames a workflow action defined under
`spec.workflow.actions` (or `spec.workflow.finally`) and rewrites every
reference to it:

- the action definition (the map key)
- `dependsOn` entries
- CEL `__actions.name` uses (in inputs and in `when`/`until` conditions)
- explicit Go-template `.__actions.name` uses

```bash
# Preview, then apply (see examples/refactor/action-solution.yaml)
scafctl refactor rename action build compile \
  -f examples/refactor/action-solution.yaml --dry-run
scafctl refactor rename action build compile -f examples/refactor/action-solution.yaml
```

The example solution at `examples/refactor/action-solution.yaml` references the
action `build` three ways (a `dependsOn` entry, a CEL `__actions.build` use, and
a template `.__actions.build` use) plus its definition, so the rename reports
four occurrences. The action also carries `alias: b`, which is left unchanged.

An action's `alias` is a **separate** top-level name, not a reference to the
action, so renaming the action leaves the `alias` untouched. The rename is
kind-scoped: renaming an action never rewrites a resolver that happens to share
its name (and vice versa). Every other guarantee -- byte-exact edits,
comment/formatting preservation, and the all-or-nothing fail-safe below --
applies identically to actions.

## Renaming calls

`refactor rename call <old> <new>` renames a reusable call defined under
`spec.calls` and rewrites every `call:` reference to it -- in resolver
`with`/`transform`/`validate` steps and in workflow actions -- plus the
definition key. Calls are only referenced structurally through the `call:`
field, never from CEL or templates, so this is the simplest rename.

```bash
scafctl refactor rename call fetch download --dry-run
scafctl refactor rename call fetch download
```

The rename is kind-scoped: a resolver or action that happens to share the call's
name is not touched. See `examples/refactor/call-function-solution.yaml`, where
`fetch` is referenced from a resolve step and a workflow action.

## Renaming functions

`refactor rename function <old> <new>` renames an author-defined function under
`spec.functions` and rewrites every `{{ name ... }}` invocation of it across all
templates -- including invocations inside **other function bodies** -- plus the
definition key.

```bash
scafctl refactor rename function greet salute --dry-run
scafctl refactor rename function greet salute
```

Only author-defined functions are renamed. Built-in and extension helpers
(`printf`, `upper`, sprig functions, ...) that share the new name are left
untouched, because the rename is scoped to the names declared in
`spec.functions`. See `examples/refactor/call-function-solution.yaml`, where
`greet` is invoked from a resolver template and from inside the `loud` function's
body.

## Exit codes

| Code | Meaning |
| ---- | ------- |
| 0    | Rename applied (or previewed with `--dry-run`) |
| 2    | Validation error: invalid new name, name collision, undefined symbol, or an unlocatable reference blocked the rename |
| 4    | The solution file could not be resolved, read, or parsed |

## Summary

- `refactor rename resolver <old> <new>` renames a resolver and every reference
  to it, preserving comments and formatting.
- `refactor rename action <old> <new>` does the same for a workflow action
  (rewriting `dependsOn`, CEL `__actions.name`, and template `.__actions.name`
  uses); the action's `alias` is left unchanged.
- `refactor rename call <old> <new>` renames a reusable call (`spec.calls`),
  rewriting every `call:` reference.
- `refactor rename function <old> <new>` renames an author-defined function
  (`spec.functions`), rewriting every `{{ name ... }}` invocation (including in
  other function bodies); built-in/extension helpers are left unchanged.
- Use `--dry-run` to preview; use `-f` to target a specific file.
- The rename refuses rather than performing a partial, solution-breaking rewrite.

## See also

- [Resolver Tutorial](resolver-tutorial.md) -- how resolvers and references work
- [Linting Tutorial](linting-tutorial.md) -- validate a solution after refactoring
