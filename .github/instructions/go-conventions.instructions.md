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
