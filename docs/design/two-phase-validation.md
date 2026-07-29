---
title: "Two-Phase Validation"
weight: 51
---

# Two-Phase Validation

## Overview

Resolver validation runs in two phases so that a validation rule which
references another resolver does not distort the resolution dependency graph:

1. **Inline validation** (phase 1) runs during resolution, immediately after a
   resolver's `resolve`/`transform` phases complete. Inline rules reference
   only their own resolver (via `__self`). They fail fast: a failing inline rule
   stops dependent resolvers just like a resolve error.
2. **Deferred validation** (phase 2) runs after *every* resolver has resolved,
   just before actions execute. Deferred rules reference one or more *other*
   resolvers (via `_.other`, in the rule expression, its inputs, its `when:`
   condition, or its message template).

The classification is automatic. Authors do not choose the phase; scafctl
inspects each rule's references and assigns it to the phase that can satisfy
them.

## Why Two Phases

Before two-phase validation, the resolver dependency graph was built from *all*
references found in a resolver -- including references inside `validate` blocks.
A message-only reference such as
`message: "tmpl: {{ .region }} must differ from backup"` was enough to add a
resolution edge. When two resolvers validated against each other, those edges
formed a **false ordering cycle**, and the entire solution failed to load with a
`circular dependency detected` error -- even though neither resolver actually
needed the other's value to *resolve*.

The historical workaround was to extract cross-resolver validation into a
dedicated "sink" resolver that depended on both. That workaround is no longer
necessary and is retired.

This pattern -- separating "compute the value" from "assert across values" --
mirrors established tools: Terraform evaluates resource dependencies to build its
graph, then runs cross-resource `precondition`/`postcondition` checks against the
already-computed plan; Kubernetes admission validation runs after an object is
otherwise resolved. scafctl follows the same principle.

## Classification Rules

A validation rule is **deferred** when any of the following reference a resolver
other than the owner:

- the rule expression or `match`/`notMatch` inputs
- any provider input on the rule
- the rule-level `when:` condition
- the failure `message`

When the phase-level `validate.when:` condition references a foreign resolver,
the entire validate phase for that resolver is deferred.

A rule that references only `__self` (or nothing) is **inline**.

Because inline rules have, by definition, an empty foreign-reference set, the
`validate` block contributes **zero edges** to the resolution dependency graph.
Resolution ordering is derived solely from `resolve`, `transform`, and `when:`
references.

## Execution Order

```text
resolve  ->  inline validate  ->  (all resolvers done)
         ->  deferred validate  ->  commit immutable locks
         ->  actions            ->  commit merged parameters
```

Immutable-lock state is committed **after** deferred validation passes and
**before** actions run, so an action never executes against a value that failed a
cross-resolver check. Resolvers whose deferred validation failed are excluded
from the immutable commit. Merged-parameter state is persisted after actions.

## Skipped and Errored Resolvers

Deferred validation is **fail-closed**. If a deferred rule references a resolver
that produced no usable value (it was skipped by its own `when:` condition, or it
errored during resolve/transform), the rule fails with a clear message:

```text
deferred validation references resolver "backupRegion" which did not produce a value
```

Two refinements keep this predictable:

- **Own-skip exemption**: if the resolver that *owns* a deferred rule was itself
  skipped, its deferred rules are skipped too (there is nothing to validate).
- **Cascade suppression**: if a deferred rule references a resolver whose *own*
  deferred validation already failed, the dependent rule is suppressed rather
  than reported as an independent failure, so the root cause is surfaced once.

For genuinely conditional cross-checks, guard the rule with a rule-level `when:`
so it only runs when the referenced value is present.

## Reporting Graph Cycle Tolerance

Deferred rules can reference each other freely -- including mutually -- because
they run after resolution. To order the *failure report* deterministically (and
surface root causes first), scafctl builds a separate reporting graph over the
deferred units and runs Tarjan SCC over it. Cycles in this reporting graph are
**tolerated**: they only affect report ordering, never execution. This is the
key difference from the resolution graph, where a cycle is a hard error.

## Failure Modes

The behavior on a deferred validation failure depends on the execution mode:

| Mode | Flag | Behavior |
| --- | --- | --- |
| Fatal | `--on-validation-error error` (default for `run solution` / `run action` / `validate resolver`) | Execution stops; an `AggregatedDeferredValidationError` is returned before actions run. |
| Non-fatal | `--on-validation-error warn` (default for `run resolver`) | Values are still emitted; failures are reported as validation diagnostics on stderr; state persistence is skipped; the process exits `0`. |
| Validate-all | `--validate-all` | Failures are folded into the aggregated execution error alongside inline failures. |
| Skip validation | `--on-validation-error ignore` | Both phases are skipped entirely. |

## Linting

The `resolver-cycle` rule now only fires on genuine `resolve`/`transform`/`when`
cycles; cross-resolver validation references never trigger it.

A new advisory, `deferred-validation-not-fail-fast` (severity `info`), fires for
each resolver that has a deferred validation rule. It is informational -- it
reminds authors that the rule runs late (after all resolvers resolve) rather than
failing fast during resolution.

## Related

- [Resolvers](resolvers.md) -- resolver phases and the validate phase
- [Providers](providers.md) -- the `validation` provider
