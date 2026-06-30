---
description: "Go coding conventions for scafctl: struct tags, Huma validation, error handling, design principles, functional options, context/timeouts, and formatting. Use when writing or editing Go code."
applyTo: "**/*.go"
---

# Go Conventions

## Struct Tags

Always add JSON/YAML tags. Use [Huma validation tags](https://huma.rocks/features/request-validation/#validation-tags):
- All fields: `doc`
- Strings: `maxLength`, `example`, `pattern`, `patternDescription`
- Integers: `maximum`, `example`
- Arrays: `maxItems` (no `example`)
- Objects/maps: no `example` tag

## Special Field Types

| Field | Type | Package |
|-------|------|---------|
| `expr` | `Expression` | `pkg/celexp` |
| `tmpl` | `GoTemplatingContent` | `pkg/gotmpl` |

## Deprecating a Field

scafctl has a reusable, struct-tag-driven deprecation mechanism. Deprecating a
spec field requires no per-field lint code -- you tag the old field, keep it
working, and add the replacement. The worked example is the resolver/action
`onError` enum being superseded by `continueOnError`.

Follow these steps:

1. Tag the OLD field as deprecated and name its replacement:

~~~go
OnError ErrorBehavior `json:"onError,omitempty" yaml:"onError,omitempty" deprecated:"true" deprecatedReplacement:"continueOnError" doc:"DEPRECATED: use continueOnError instead. ..."`
~~~

   - `deprecated:"true"` -- required; marks the field deprecated.
   - `deprecatedReplacement:"<newYamlKey>"` -- the replacement's YAML key; drives
     lint messaging and the schema `[DEPRECATED] (use <replacement>)` marker.
   - `deprecatedMessage:"<extra guidance>"` -- optional free-form note appended to
     the lint warning.

2. Keep the old field fully functional (parse + translate) for at least one major
   version. Never delete a field in the same release you deprecate it. Provide a
   translation path (e.g. `onError: continue` -> `continueOnError: true`,
   `onError: fail` -> `continueOnError: false`) and document the mapping in the
   new field's `doc` tag.

3. Add the NEW field. If it is a `*Condition`, do NOT add an `example` tag (Huma
   validates examples against the object schema and a scalar like `true` will
   panic schema generation).

4. The plumbing is automatic:
   - `pkg/schema/introspect.go` reads the tags into `FieldInfo`.
   - `pkg/schema/format.go` renders `[DEPRECATED] (use <replacement>)`.
   - Huma emits `deprecated: true` in the JSON schema from the `deprecated` tag.
   - The generic `deprecated-field` lint rule (`pkg/lint/deprecated.go`) emits a
     WARNING when the old field is set. Add a traversal call there for the new
     struct location if it is not already walked (resolvers, workflow actions,
     and forEach are covered).

5. If the old and new fields are mutually exclusive, the `deprecated-field-conflict`
   rule emits an ERROR when both are set on the same object (runtime: the new
   field wins). This is the default behavior of `emitDeprecated`.

6. Escalate by SEMVER, not wall-clock time: warn now -> error in the next major ->
   remove in the major after that. CI can opt into strict mode by treating lint
   warnings as failures.

7. Migrate first-party docs and examples to the new field, but keep ONE example of
   the deprecated field to demonstrate the warning, and keep a test asserting the
   old field still works and warns. Cross-reference the `deprecated-field` rule's
   explain text so `explain_lint_rule` and these instructions tell the same story.

## Error Handling

Always wrap errors with context:

```go
if err != nil {
    return fmt.Errorf("failed to create user: %w", err)
}
```

## Design Principles

- Accept interfaces, return structs
- Keep interfaces small (1-3 methods)
- Define interfaces where they are used, not where they are implemented
- Use constructor functions for dependency injection
- Use functional options pattern (`WithX(val) Option`) for configurable constructors
- Always pass `context.Context` as first parameter for timeout/cancellation control
- No package-level mutable state

## Secret Management

Read secrets from environment variables -- never hardcode.

## Formatting

- **gofmt** and **goimports** are mandatory -- no style debates
- Never use magic strings or numbers; always define constants or use settings

## Embedder-Safe Design

scafctl is consumed as a library by external CLIs. Code must not assume the binary name.

- Use `settings.CliBinaryName` or `settings.Run.BinaryName` instead of hardcoding `"scafctl"`
- Functions producing paths, cache keys, or env vars must accept a name parameter or read from context
- When adding `settings.*For(binaryName)` helpers, guard against empty `binaryName` by falling back to `CliBinaryName`
