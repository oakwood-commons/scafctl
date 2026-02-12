---
title: "Future Enhancements"
weight: 13
---

# Future Enhancements

Consolidated index of planned features and future enhancements across all design docs. Each entry links back to the source design doc for full context.

---

## Actions

Source: [actions.md](actions.md)

### Result Schema Validation

Actions could optionally declare an expected result schema for validation and documentation. This would enable runtime validation of provider output, self-documenting result structures, and better CEL/template autocomplete. Schema uses standard JSON Schema format.

### Conditional Retry

Retry policies could support a `retryIf` condition to retry only on specific error types (e.g., rate limits, server errors). Introduces an `__error` namespace with `message`, `statusCode`, `code`, `retryable`, and `attempt` fields.

### Matrix Strategy

Parallel expansion across multiple dimensions (e.g., region × environment). Supports `exclude` and `include` modifiers for fine-grained control. Only available in `workflow.actions`.

### Action Alias

Actions could declare a short alias for more readable expression references (e.g., `config.results.endpoint` instead of `__actions.fetchConfiguration.results.endpoint`). Supports refactoring without updating all expressions.

### Exclusive Actions (Mutual Exclusion)

Actions could declare mutual exclusion constraints to prevent concurrent execution of actions that share resources. One-way declaration, does not imply dependency ordering.

### Action Concurrency Limit

A `--max-action-concurrency` CLI parameter to limit the maximum number of actions executing concurrently. Allows runtime tuning without modifying the solution.

---

## Auth

Source: [auth.md](auth.md)

### Auth Claims Provider

A dedicated provider to expose authentication claims (tenant ID, subject, scopes, etc.) for use in expressions and conditions. Enables conditional logic based on the authenticated identity without exposing raw tokens.

---

## Catalog Build Bundling

Source: [catalog-build-bundling.md](catalog-build-bundling.md)

The bundling design is now fully specified. Key features:

- **Multi-file composition (`compose`)** — Split solutions across YAML files, merged at build time
- **Static analysis + explicit `bundle.include`** — Auto-discover literal file paths; declare globs for dynamic paths
- **File bundling as OCI layers** — Multi-layer OCI artifacts (solution YAML + bundle tar)
- **Catalog vendoring** — Remote catalog dependencies fetched and embedded at build time for offline execution
- **`solution.lock`** — Lock file for reproducible builds (catalog deps + plugin versions)
- **Plugin dependencies (`bundle.plugins`)** — Declare plugins with `kind`, version constraints, and ValueRef-aware defaults
- **`.scafctlignore`** — Purpose-specific file exclusion (independent of `.gitignore`)
- **Nested bundle support** — Bundled sub-solutions can themselves contain bundles
- **Semver constraint resolution** — Catalog refs support full semver constraints (`^`, `~`, `>=`, etc.) resolved at build time to the highest matching version
- **Version conflict detection** — Conflicting transitive dependency versions are detected and rejected at build time

Future enhancements tracked in the bundling design doc:
- `scafctl bundle verify` — validate built artifacts
- Bundle diffing between versions
- Per-provider defaults within a plugin

---

## Catalog

Source: [catalog.md](catalog.md)

### Future Artifact Types

Additional artifact types beyond solutions, providers, and auth handlers — TBD as the system evolves.

### Catalog Lock File

Source: [catalog-build-bundling.md](catalog-build-bundling.md)

The `solution.lock` file is now part of the bundling design. It records resolved versions and digests for both vendored catalog dependencies and plugin dependencies. Generated during `scafctl build solution`, replayed on subsequent builds for reproducibility. Use `--update-lock` to re-resolve.

---

## CLI

Source: [cli.md](cli.md)

### Publishing Artifacts (`push`)

Push artifacts to a remote catalog (analogous to `docker push`). Supports pushing solutions and plugins to named catalogs.

### Pulling Artifacts (`pull`)

Pull artifacts from a remote catalog to local (analogous to `docker pull`).

### Tagging Artifacts (`tag`)

Create version aliases for artifacts (e.g., `my-solution:latest`, `aws-provider:stable`).

### Catalog Resolution (`--catalog` flag)

Target a specific configured catalog for commands like `run`, `get`, `push`, `pull`.

### Version Constraints

Support constraint-based version resolution in run commands (e.g., `example@^1.2`, `example@>=1.0 <2.0`). Requires catalog.

---

## Misc

Source: [misc.md](misc.md)

### Linting

Advisory linting for solutions: unused resolvers, unreachable actions, missing dependencies, anti-pattern detection. Non-blocking.

### Action DAG Visualization (ASCII/DOT/Mermaid)

Action DAG visualization currently supports JSON/YAML only. ASCII, DOT, and Mermaid output formats are planned to match resolver DAG visualization.

---

## Plugins

Source: [plugins.md](plugins.md)

### Extended Plugin Capabilities

Plugins currently expose providers only. Future capability types may include provider sets, schemas, and validation helpers.

---

## Providers

Source: [providers.md](providers.md)

### Future Provider Capabilities

The capability model is designed for extension. Future capabilities may include:

- `caching` — Provider supports result caching
- `streaming` — Provider supports streaming data
- `batch` — Provider supports batch operations
- `webhook` — Provider can receive webhook notifications

---

## Resolvers

Source: [resolvers.md](resolvers.md)

### ForEach Filter Property

A `filter` property to automatically remove `nil` results from the output array when using `when` conditions. Provides a more ergonomic alternative to a separate transform step.

---

## Solutions

Source: [solutions.md](solutions.md)

### Plugin Dependencies

Solutions will be able to declare dependencies on plugins that provide custom providers, with semver version constraints. scafctl will check, pull, validate, and dynamically load required plugins.
