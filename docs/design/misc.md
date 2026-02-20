---
title: "Miscellaneous"
weight: 11
---

# Miscellaneous Design Considerations

This document captures cross-cutting concepts that are intentionally not core primitives, but are required to make scafctl safe, predictable, and operable at scale.

These concerns apply across resolvers, actions, providers, plugins, and solutions.

---

## Implementation Status

| Concern | Status | Notes |
|---------|--------|-------|
| Schema and Typing Model | ✅ Implemented | Provider `Descriptor` with `Schema` and `OutputSchemas` |
| Provider Contracts | ✅ Implemented | Full descriptor with input/output schemas, capabilities |
| Provider Capabilities | ✅ Implemented | `from`, `transform`, `validation`, `authentication`, `action` |
| Secrets and Sensitive Data | ✅ Implemented | `pkg/secrets` with AES-256-GCM, keychain integration |
| Secret Redaction | ✅ Implemented | `RedactedError`, `--redact` flag on snapshots |
| Stateless Core | ✅ Implemented | Providers are stateless, no persistent state |
| Error Model | ✅ Implemented | Rich error types with location, cause, context |
| Determinism Rules | ✅ Implemented | Resolver purity enforced, `MockBehavior` declared |
| Resolver DAG Visualization | ✅ Implemented | ASCII, DOT, Mermaid, JSON formats |
| Action DAG Visualization | ✅ Implemented | ASCII, DOT, Mermaid, JSON formats via `--action-graph` |
| Validation | ✅ Implemented | Schema, dependency, type validation |
| Linting | ✅ Implemented | `scafctl lint` with error/warning/info severities |
| Extensibility (Providers/Plugins) | ✅ Implemented | Plugin system for external providers |
| Render vs Run Separation | ✅ Implemented | `scafctl render` vs `scafctl run` |
| Dry-run Mode | ✅ Implemented | `--dry-run` flag, `DryRunFromContext()` |

---

## Schema and Typing Model

> **Status**: ✅ Implemented in `pkg/provider/provider.go`

### Purpose

A strong schema and typing model ensures early validation, clear errors, and predictable execution.

### Principles

- All provider inputs must be schema-defined
- All resolver outputs have a known type
- All action inputs are fully materialized before execution or render
- Type errors are detected before provider execution

### Enforcement Points

- Solution load time
- Resolver execution
- Action render phase
- Provider invocation

Schemas are used for validation only. They do not imply runtime coercion.

---

## Provider Contracts and Capabilities

> **Status**: ✅ Implemented - See `pkg/provider/provider.go` `Descriptor` struct

### Provider Contracts

Each provider must declare:

- Input schema ✅ (`Schema` field)
- Output shape ✅ (`OutputSchemas` field)
- Supported operations (if applicable) ✅ (`Capabilities` field)
- Determinism expectations ✅ (`MockBehavior` field)
- Side-effect behavior ✅ (capability determines this)
- A way to mock so solutions can be tested ✅ (`MockBehavior` field)

Providers are treated as black boxes with explicit contracts.

### Capabilities

Providers declare capabilities that determine where they can be used:

- `from` - Provider can be used in resolver `resolve.with` section ✅
- `transform` - Provider can be used in resolver `transform.with` section ✅
- `validation` - Provider can be used in resolver `validate.with` section ✅
- `authentication` - Provider handles authentication ✅
- `action` - Provider can be invoked as an action (side effects) ✅

Capabilities allow scafctl and external executors to reason about safety and execution constraints.

---

## Secrets and Sensitive Data

> **Status**: ✅ Implemented in `pkg/secrets/`

### Design Goals

- Prevent accidental leakage ✅
- Avoid rendering secrets into artifacts ✅ (`--redact` flag)
- Keep secrets out of logs and plans ✅ (`RedactedError` type)

### Rules

- Secrets may be resolved by resolvers ✅ (`secret` provider)
- Secrets may be passed to actions ✅
- Secrets must not be rendered in cleartext during render mode ✅ (`--redact`)
- Providers must explicitly declare secret-handling behavior ✅

Render mode supports secret redaction via `--redact` flag on snapshots.

### Implementation Details

- **Storage**: AES-256-GCM encryption with OS keychain for master key
- **CLI**: `scafctl secrets list/get/set/delete/exists/export/import/rotate`
- **Internal secrets**: Auth tokens use `scafctl.*` prefix; visible via `--all` flag
- **Platform paths** (XDG Base Directory Specification):
  - Linux: `~/.local/share/scafctl/secrets/`
  - macOS: `~/.local/share/scafctl/secrets/`
  - Windows: `%LOCALAPPDATA%\scafctl\secrets\`

---

## Lifecycle and State

> **Status**: ✅ Implemented - Core is stateless by design

### Stateless Core

scafctl is stateless by design:

- No persistent state between runs
- No implicit caching
- No hidden execution memory

### External State

If state is required, it must live outside scafctl, for example:

- Remote APIs
- Infrastructure systems
- External state stores

State is accessed only through providers.

---

## Error Model

> **Status**: ✅ Implemented in `pkg/resolver/errors.go`, `pkg/action/errors.go`

### Principles

- Fail fast ✅
- Fail early ✅
- Fail explicitly ✅

### Error Categories

- Schema validation errors ✅
- Resolver evaluation errors ✅ (`ExecutionError`)
- Render-time errors ✅
- Provider execution errors ✅

Errors include:

- Location in the solution ✅ (resolver name, phase, step)
- Provider or resolver name ✅
- Clear cause ✅
- Suggested remediation where possible ✅ (validation messages)

---

## Determinism and Reproducibility

> **Status**: ✅ Implemented

### Determinism Rules

- Resolvers must be pure ✅ (enforced by design - no side effects)
- Render mode must be deterministic ✅
- Action graphs must be reproducible given the same inputs ✅

### Non-Determinism

If a provider is non-deterministic, it must declare this via `MockBehavior`.
Providers like `exec` that have side effects are restricted to `CapabilityAction`.

---

## Validation, Linting, and Tooling

### Validation

> **Status**: ✅ Implemented

scafctl supports:

- Schema validation ✅ (provider inputs, config files)
- Dependency validation ✅ (cycle detection in DAG)
- Type validation ✅ (CEL type checking)
- Capability validation ✅ (providers checked for required capabilities)

### Linting

> **Status**: ✅ Implemented in `pkg/cmd/scafctl/lint/lint.go`

Linting rules include:

**Errors**: empty solutions, reserved names, missing providers, invalid expressions/templates, invalid dependencies, finally-with-forEach, workflow validation, unbundled test files, invalid test names, undefined required properties, invalid result schemas, **schema violations** (unknown fields, type mismatches against JSON Schema), **unknown provider inputs**, **invalid provider input types**

**Warnings**: unused resolvers, empty workflows, unused templates

**Info**: missing descriptions, long timeouts, unused finally actions, permissive result schemas

Linting is advisory, not blocking. Output supports table, JSON, YAML, and quiet formats via `-o` flag.

---

## Visualization and Introspection

> **Status**: ✅ Implemented

### Goals

- Make execution graphs understandable ✅
- Make data flow visible ✅
- Aid debugging and review ✅

### Outputs

- Resolver DAG visualization ✅ (`--graph` with ASCII, DOT, Mermaid, JSON)
- Action DAG visualization ✅ (`--action-graph` with ASCII, DOT, Mermaid, JSON)
- Rendered action graph inspection ✅ (`scafctl render solution`)
- Dependency summaries ✅ (`scafctl explain solution`)

Visualization operates on rendered graphs, not runtime execution.

### CLI Commands

```bash
# Resolver graph visualization
scafctl render solution -f solution.yaml --graph
scafctl render solution -f solution.yaml --graph --graph-format=dot
scafctl render solution -f solution.yaml --graph --graph-format=mermaid

# Standalone resolver graph
scafctl resolver graph -f solution.yaml --format=mermaid
```

---

## Extensibility Boundaries

> **Status**: ✅ Implemented

### What Can Be Extended

- Providers (via plugins) ✅
- Schemas ✅
- Validation rules ✅

### What Must Remain Fixed

- Resolver purity ✅ (enforced)
- Action side-effect boundaries ✅ (enforced via capabilities)
- Render vs run separation ✅ (enforced)
- Dependency rules ✅ (DAG prevents cycles)

Extensibility does not compromise determinism or analyzability.

---

## UX and Command-Line Experience

> **Status**: ✅ Implemented

### Expectations

- Clear error messages ✅
- Predictable commands ✅ (kubectl-style verb-kind-name)
- Explicit modes (run vs render) ✅
- No hidden behavior ✅

Commands are composable and script-friendly.

### Additional UX Features Implemented

- `--quiet` / `--no-color` flags for CI/CD
- `-o json/yaml/table` output formats
- `--interactive` mode for TUI
- Progress callbacks during execution

---

## Design Guardrails

> **Status**: ✅ All guardrails enforced

The following always hold true:

- Resolvers never perform side effects ✅ (enforced by capability system)
- Actions never feed resolvers ✅ (separate execution phases)
- Providers are the only execution mechanism ✅
- Rendering never executes providers ✅ (`scafctl render` is safe)
- Execution never mutates resolver outputs ✅ (immutable context)

Violating these rules breaks the mental model.

---

## Summary

These concerns define the operational and safety envelope of scafctl. While not core primitives, they are essential to making the system reliable, extensible, and understandable. Treat this document as the set of non-negotiable guardrails that protect the core design as the system evolves.
