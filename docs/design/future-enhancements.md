---
title: "Future Enhancements"
weight: 13
---

# Future Enhancements

Consolidated index of planned features and future enhancements across all design docs. Each entry links back to the source design doc for full context.

Items marked ✅ have been implemented since this document was originally written.

---

## Actions

Source: [actions.md](actions.md)

### ✅ Result Schema Validation

**Implemented.** Actions can declare a `resultSchema` with JSON Schema and a `resultSchemaMode` (`error`, `warn`, `ignore`). Validated at runtime in the action executor. Supports workflow-level defaults. See `pkg/action/result_validation.go` and `pkg/action/executor.go`.

### Conditional Retry

Retry policies could support a `retryIf` condition to retry only on specific error types (e.g., rate limits, server errors). Introduces an `__error` namespace with `message`, `statusCode`, `code`, `retryable`, and `attempt` fields.

### Matrix Strategy

Parallel expansion across multiple dimensions (e.g., region × environment). Supports `exclude` and `include` modifiers for fine-grained control. Only available in `workflow.actions`.

### Action Alias

Actions could declare a short alias for more readable expression references (e.g., `config.results.endpoint` instead of `__actions.fetchConfiguration.results.endpoint`). Supports refactoring without updating all expressions.

### ✅ Exclusive Actions (Mutual Exclusion)

**Implemented.** Actions can declare `exclusive: [<other-action>]` to prevent concurrent execution. One-way declaration, causes DAG phase splitting. See `pkg/action/validation.go` and `pkg/action/exclusive_test.go`.

### ✅ Action Concurrency Limit

**Implemented.** The `--max-action-concurrency` flag on `scafctl run solution` limits the maximum number of actions executing concurrently. Default is 0 (unlimited). See `pkg/cmd/scafctl/run/solution.go`.

---

## Auth

Source: [auth.md](auth.md)

### Auth Claims Provider

A dedicated provider to expose authentication claims (tenant ID, subject, scopes, etc.) for use in expressions and conditions. Enables conditional logic based on the authenticated identity without exposing raw tokens.

---

## Catalog Build Bundling

Source: [catalog-build-bundling.md](catalog-build-bundling.md)

✅ The bundling design is now fully implemented. Key features:

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
- **`scafctl bundle verify`** — Validate built artifacts
- **Bundle diffing between versions** — `scafctl bundle diff`
- **Selective extraction** — `scafctl bundle extract`

---

## Catalog

Source: [catalog.md](catalog.md)

### Future Artifact Types

Additional artifact types beyond solutions, providers, and auth handlers — TBD as the system evolves.

### ✅ Catalog Lock File

**Implemented.** The `solution.lock` file records resolved versions and digests for vendored catalog dependencies and plugin dependencies. Generated during `scafctl build solution`, replayed on subsequent builds for reproducibility. Use `--update-lock` to re-resolve. See `pkg/solution/bundler/lock.go`.

---

## CLI

Source: [cli.md](cli.md)

### ✅ Publishing Artifacts (`push`)

**Implemented.** `scafctl catalog push` pushes artifacts to a remote catalog. See `pkg/cmd/scafctl/catalog/push.go`.

### ✅ Pulling Artifacts (`pull`)

**Implemented.** `scafctl catalog pull` pulls artifacts from a remote catalog. See `pkg/cmd/scafctl/catalog/pull.go`.

### ✅ Tagging Artifacts (`tag`)

**Implemented.** `scafctl catalog tag` creates version aliases. See `pkg/cmd/scafctl/catalog/tag.go`.

### ✅ Catalog Resolution (`--catalog` flag)

**Implemented.** The `--catalog` flag targets a specific configured catalog. Falls back to config-based default. See `pkg/cmd/scafctl/catalog/resolve.go`.

### ✅ Version Constraints

**Implemented.** Semver constraint resolution (`^`, `~`, `>=`, etc.) is supported in catalog vendoring and plugin dependencies. See `pkg/solution/bundler/plugin.go` and `pkg/solution/bundler/vendor.go`.

---

## Misc

Source: [misc.md](misc.md)

### ✅ Linting

**Implemented.** `scafctl lint` provides advisory linting with `scafctl lint rules` and `scafctl lint explain`. See `pkg/cmd/scafctl/lint/`.

### ✅ Action DAG Visualization (ASCII/DOT/Mermaid)

**Implemented.** Action DAG visualization supports ASCII, DOT, Mermaid, JSON, and YAML formats. See `pkg/action/graph_visualization.go`.

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

## Functional Testing

Source: [functional-testing.md](functional-testing.md)

### ✅ Auto-Generated Tests (`-o test`)

**Implemented.** The `-o test` output type captures command execution and generates test definitions with assertions and golden files. See `pkg/solution/soltesting/generate.go`.

### Catalog Regression Testing (`scafctl pipeline`)

A future command that executes functional tests across solutions in a remote catalog, enabling validation that scafctl changes don't break existing solutions. Fetches matching solutions, extracts bundled test files, runs `test functional`, and reports aggregate results.

### ✅ Test Scaffolding (`scafctl test init`)

**Implemented.** Generates a starter test suite for an existing solution by analyzing its structure — parsing resolvers with defaults, validation rules, and workflow actions, then outputting skeleton test YAML. See `pkg/solution/soltesting/scaffold.go` and `pkg/cmd/scafctl/test/init.go`.

### ✅ Watch Mode (`--watch`)

**Implemented.** The `--watch` / `-w` flag for `scafctl test functional` monitors solution files for changes and automatically re-runs affected tests. Uses `fsnotify` with debounce. See `pkg/solution/soltesting/watch.go`.

---

## Resolvers

Source: [resolvers.md](resolvers.md)

### ✅ ForEach Filter Property

**Implemented** (with inverted API). Rather than an opt-in `filter: true`, the default behavior filters out `nil` results from forEach with `when` conditions. Use `keepSkipped: true` to retain them. See `pkg/spec/foreach.go` and `pkg/resolver/executor.go`.

---

## Solutions

Source: [solutions.md](solutions.md)

### ✅ Plugin Dependencies

**Implemented.** Solutions can declare plugin dependencies in `bundle.plugins` with kind, version constraints, and ValueRef-aware defaults. Vendored during `scafctl build solution` and tracked in `solution.lock`. See `pkg/solution/solution.go` and `pkg/solution/bundler/vendor_plugins.go`.

> **Note:** Dynamic plugin auto-fetching from remote catalogs at runtime (without a prior build step) is not yet implemented. Plugins must be vendored at build time.
