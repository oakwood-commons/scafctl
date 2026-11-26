---
name: "{{SKILL_NAME}}"
description: Author, validate, test, and package {{BINARY_NAME}} solutions and actions.yaml workflows using MCP schema and verification tools.
---

# Authoring {{BINARY_NAME}} Solutions

Always query the `{{BINARY_NAME}}` MCP server before writing YAML -- never guess spec fields, provider inputs, or expression functions from memory.

## 1. Schema and Provider Lookup First

- Inspect spec shapes with `get_solution_schema` and `explain_kind` (`solution`, `resolver`, `action`, `workflow`, `spec`, `retry`).
- Discover providers and exact I/O schemas via `list_providers`, `get_provider_schema`, and `get_provider_output_shape`.
- Check expression/template helpers and phase variables with `list_cel_functions`, `list_go_template_functions`, and `list_context_variables`.

## 2. File Archetypes: `actions.yaml` vs `solution.yaml`

- **`actions.yaml` (task runner)**: Repo-local build/test/lint/release tasks discovered automatically by `{{BINARY_NAME}} run action <name>`. Keep focused on `spec.resolvers` + `spec.workflow.actions`.
- **`solution.yaml` (reusable package)**: Distributable scaffolding or automation with `metadata.name`, `metadata.version` (semver), `bundle.include` / `bundle.plugins`, `spec.resolvers`, `spec.workflow`, and `spec.testing.cases`.
- **`explicit` action gating**: Actions default to `explicit: false` and run together during `run solution` or `run action` with no target names. Set `explicit: true` on opt-in tasks (such as `release`, `clean`, or `deploy`) so they run only when explicitly named on the CLI or pulled in via `dependsOn`.

## 3. ValueRef, CEL, and Shell Idioms

- **ValueRefs**: Use literals for static values; `{rslvr: myResolver}` for raw values; `{expr: "_.myResolver.trim()"}` for CEL data logic; `{tmpl: "..."}` for multi-line text; `{scrt: name}` and `{tkn: handler}` for credentials so outputs, dry-runs, and snapshots stay redacted.
- **Naming and error flow**: Name resolvers in `camelCase` so CEL reads `_.myResolver` cleanly. Use `continueOnError` on actions (never deprecated `onError`).
- **Portable `exec` shell (`mvdan/sh`)**: The `exec` provider runs an embedded cross-OS POSIX shell by default. Stick to POSIX `sh` syntax -- avoid bashisms (`[[`, arrays, `set -o pipefail`) unless `shell: bash` or `shell: pwsh` is explicitly set. Set `failOnError: true` on non-final `exec` steps in resolver fallback chains.

## 4. Mandatory Verification Before Finishing

1. Run `lint_solution` (or `{{BINARY_NAME}} lint`) and resolve all errors and warnings.
2. Preview behavior safely with `preview_resolvers` and `dry_run_solution`.
3. Add or update `spec.testing.cases` (a map keyed by test name) and run `run_solution_tests` (or `{{BINARY_NAME}} test`).
4. For packaged `solution.yaml` bundles, verify bundle file inclusion and plugin lock readiness with `{{BINARY_NAME}} package solution --dry-run` (or `package_solution` with `dry_run: true` when available).
