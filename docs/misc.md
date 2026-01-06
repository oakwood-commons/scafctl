# Miscellaneous Design Considerations

This document captures cross-cutting concepts that are intentionally not core primitives, but are required to make scafctl safe, predictable, and operable at scale.

These concerns apply across resolvers, actions, providers, plugins, and solutions.

---

## Schema and Typing Model

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

### Provider Contracts

Each provider must declare:

- Input schema
- Output shape
- Supported operations (if applicable)
- Determinism expectations
- Side-effect behavior

Providers are treated as black boxes with explicit contracts.

### Capabilities

Providers may declare capabilities such as:

- Requires network access
- Requires filesystem access
- Supports render-only mode
- Supports dry-run or plan mode

Capabilities allow scafctl and external executors to reason about safety and execution constraints.

---

## Secrets and Sensitive Data

### Design Goals

- Prevent accidental leakage
- Avoid rendering secrets into artifacts
- Keep secrets out of logs and plans

### Rules

- Secrets may be resolved by resolvers
- Secrets may be passed to actions
- Secrets must not be rendered in cleartext during render mode
- Providers must explicitly declare secret-handling behavior

Render mode must support secret redaction or placeholder substitution.

---

## Lifecycle and State

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

### Principles

- Fail fast
- Fail early
- Fail explicitly

### Error Categories

- Schema validation errors
- Resolver evaluation errors
- Render-time errors
- Provider execution errors

Errors must include:
- Location in the solution
- Provider or resolver name
- Clear cause
- Suggested remediation where possible

---

## Determinism and Reproducibility

### Determinism Rules

- Resolvers must be pure
- Render mode must be deterministic
- Action graphs must be reproducible given the same inputs

### Non-Determinism

If a provider is non-deterministic, it must declare this explicitly.

---

## Validation, Linting, and Tooling

### Validation

scafctl should support:

- Schema validation
- Dependency validation
- Type validation
- Capability validation

### Linting

Linting may include:

- Unused resolvers
- Unreachable actions
- Missing dependencies
- Anti-pattern detection

Linting is advisory, not blocking.

---

## Visualization and Introspection

### Goals

- Make execution graphs understandable
- Make data flow visible
- Aid debugging and review

### Outputs

- Resolver DAG visualization
- Action DAG visualization
- Rendered action graph inspection
- Dependency summaries

Visualization operates on rendered graphs, not runtime execution.

---

## Extensibility Boundaries

### What Can Be Extended

- Providers (via plugins)
- Schemas
- Validation rules

### What Must Remain Fixed

- Resolver purity
- Action side-effect boundaries
- Render vs run separation
- Dependency rules

Extensibility must not compromise determinism or analyzability.

---

## UX and Command-Line Experience

### Expectations

- Clear error messages
- Predictable commands
- Explicit modes (run vs render)
- No hidden behavior

Commands should be composable and script-friendly.

---

## Design Guardrails

The following must always hold true:

- Resolvers never perform side effects
- Actions never feed resolvers
- Providers are the only execution mechanism
- Rendering never executes providers
- Execution never mutates resolver outputs

Violating these rules breaks the mental model.

---

## Summary

These concerns define the operational and safety envelope of scafctl. While not core primitives, they are essential to making the system reliable, extensible, and understandable. Treat this document as the set of non-negotiable guardrails that protect the core design as the system evolves.
