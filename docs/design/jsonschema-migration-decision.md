---
title: "JSON Schema Migration Decision"
weight: 17
---

# Provider Schema Migration: Custom SchemaDefinition → JSON Schema

**Status:** Proposed  
**Created:** 2026-02-07  
**Author:** (team discussion)

---

## Context

Provider input and output schemas are currently defined using custom types — `SchemaDefinition` and `PropertyDefinition` — in `pkg/provider/provider.go`. These types were purpose-built early in development and serve their role for simple, flat property definitions.

Meanwhile, the codebase already uses [`github.com/google/jsonschema-go`](https://github.com/google/jsonschema-go) (v0.4.2) for **action result validation** (`Action.ResultSchema`), where it provides full JSON Schema 2020-12 support including `$ref`, `allOf`/`anyOf`/`oneOf`, nested objects, and more.

This proposal evaluates replacing the custom `SchemaDefinition`/`PropertyDefinition` types with `jsonschema.Schema` for provider `Schema` (inputs) and `OutputSchemas` (outputs per capability).

### Current Custom Types

```go
type SchemaDefinition struct {
    Properties map[string]PropertyDefinition
}

type PropertyDefinition struct {
    Type        PropertyType   // "string" | "int" | "float" | "bool" | "array" | "any"
    Required    bool
    Description string
    Default     any
    Example     any
    MinLength   *int
    MaxLength   *int
    Pattern     string
    Minimum     *float64
    Maximum     *float64
    MinItems    *int
    MaxItems    *int
    Enum        []any
    Format      string
    Deprecated  bool
    IsSecret    bool
}
```

### Current Limitations

1. **No nested objects** — `PropertyTypeAny` is used as an escape hatch for maps, objects, and any complex structure. Providers like `http` (headers), `exec` (env), and `env` (list output) have opaque `any`-typed properties with no structural description.
2. **No `object` or `map` type** — only 6 property types exist (`string`, `int`, `float`, `bool`, `array`, `any`).
3. **No composition** — cannot express `allOf`, `anyOf`, `oneOf`, `if/then/else`, or `$ref`.
4. **No array item schemas** — `array` type has `minItems`/`maxItems` but no way to describe what's _in_ the array.
5. **`Required` is per-property** — JSON Schema convention is a `required` array on the parent object. Our approach works but diverges from ecosystem norms.
6. **Two schema systems** — action `ResultSchema` uses `jsonschema.Schema`, provider schemas use `SchemaDefinition`. Developers must learn both.
7. **No schema generation from Go structs** — `jsonschema.For[T]()` already works for config; providers with `Decode` functions could auto-generate schemas from their typed input structs.

---

## Proposed Change

Replace the `Descriptor` fields:

```go
// Before
Schema        SchemaDefinition                      // input schema
OutputSchemas map[Capability]SchemaDefinition        // output schemas per capability

// After
Schema        *jsonschema.Schema                    // input schema
OutputSchemas map[Capability]*jsonschema.Schema      // output schemas per capability
```

Remove `SchemaDefinition`, `PropertyDefinition`, and `PropertyType` from the provider package.

---

## Detailed Pros and Cons

### Pros

#### 1. Industry Standard — No Custom Learning Curve
JSON Schema is a widely adopted standard (RFC draft, used by OpenAPI, AsyncAPI, Kubernetes CRDs, GitHub Actions, VS Code settings, etc.). New contributors and plugin authors can use existing JSON Schema knowledge instead of learning a bespoke type system.

#### 2. Eliminates Dual Schema Systems
Today, action results validate against `jsonschema.Schema` while provider inputs/outputs validate against `SchemaDefinition`. Unifying on one schema system reduces cognitive overhead and eliminates the question "which schema type do I use here?"

#### 3. Full Expressiveness for Complex Types
JSON Schema natively supports:
- **Nested objects** — `properties` within `properties`, replacing opaque `any` types
- **Array item schemas** — `items` describes element shape
- **Composition** — `allOf`, `anyOf`, `oneOf` for polymorphic inputs
- **Conditional schemas** — `if`/`then`/`else` for context-dependent validation
- **References** — `$ref` for reusable schema fragments
- **`additionalProperties`** — control whether extra keys are allowed

Example: The `http` provider's `headers` could move from `type: any` to:
```json
{
  "type": "object",
  "additionalProperties": { "type": "string" },
  "description": "HTTP headers as key-value pairs"
}
```

#### 4. Richer Validation for Free
`jsonschema.Schema.Resolve(nil).Validate(data)` already works in the codebase for action results. Switching provider validation to the same mechanism gives us:
- `pattern` with full JSON Schema regex semantics
- `format` with standard format names (`uri`, `email`, `date-time`, `uuid`, `ipv4`, etc.)
- `multipleOf` for numeric constraints
- `dependentRequired` / `dependentSchemas` for conditional requirements
- `const` for fixed values
- `contentMediaType` / `contentEncoding` for binary data

This replaces our ~300-line custom `validation.go` with a battle-tested library.

#### 5. Auto-Generation from Go Structs
Providers with typed input structs (via `Descriptor.Decode`) could use `jsonschema.For[T]()` to auto-generate their input schema from struct tags, eliminating manual schema maintenance. This already works for `config.Config`.

#### 6. Ecosystem Tooling Compatibility
JSON Schema enables:
- **IDE autocompletion** — editors can validate YAML/JSON files against published schemas
- **Documentation generation** — tools like `json-schema-to-markdown` can render schemas
- **OpenAPI integration** — schemas are directly embeddable in OpenAPI specs
- **Schema registries** — schemas can be published and consumed by external tools

#### 7. Future-Proofing
As more artifact kinds are added (plugins, auth-handlers), a standard schema system scales better than a custom one. External plugin authors benefit from using a well-documented standard.

#### 8. Cleaner Descriptor Validation
`ValidateDescriptor()` currently manually checks that capability output schemas include required fields (e.g., `valid` + `errors` for validation capability). With JSON Schema, these requirements can be expressed as meta-schemas that validate output schemas themselves.

---

### Cons

#### 1. Breaking Change — All 14 Builtin Providers Must Be Rewritten
Every provider's `Descriptor()` method constructs `SchemaDefinition{Properties: map[string]PropertyDefinition{...}}`. All 14 must be migrated to `jsonschema.Schema` construction. This is mechanical but significant in scope.

**Affected providers:** `cel`, `debug`, `env`, `exec`, `file`, `git`, `gotmpl`, `http`, `identity`, `parameter`, `secret`, `sleep`, `static`, `validation`

#### 2. Verbose Schema Construction in Go
`jsonschema.Schema` construction in Go is more verbose than our current flat struct:

```go
// Current (compact)
"url": {
    Type:     provider.PropertyTypeString,
    Required: true,
    Example:  "https://example.com",
    Pattern:  `^https?://.*`,
}

// jsonschema.Schema (more verbose)
"url": {
    Type:        jsonschema.String,
    Examples:    []any{"https://example.com"},
    Pattern:     ptr(`^https?://.*`),
    Description: ptr("The URL to request"),
}
// Plus: "url" must be in parent schema's Required array
```

Many `jsonschema.Schema` fields are pointers (`*string`, `*float64`) requiring helper functions, and `Required` moves to the parent object.

#### 3. Loss of Custom Fields: `IsSecret` and `Deprecated` (per-property)
`PropertyDefinition` has:
- **`IsSecret`** — marks a property as containing sensitive data (used for redaction)
- **`Deprecated`** (per-property) — marks individual properties as deprecated

JSON Schema has no native equivalent for `IsSecret`. Options:
- Use `x-is-secret` extension keyword (non-standard)
- Use a separate `SensitiveFields []string` on the Descriptor
- Use JSON Schema's `writeOnly: true` as a partial semantic match

JSON Schema does support `deprecated: true` natively (added in 2019-09), so per-property deprecation can be preserved.

#### 4. gRPC Plugin Serialization Must Be Redesigned
`pkg/plugin/grpc.go` manually converts `PropertyDefinition` fields to/from protobuf messages. With `jsonschema.Schema`, the proto definition needs redesign. Options:
- Serialize the schema as JSON bytes in proto (simple but loses type safety)
- Create a comprehensive proto message mirroring jsonschema.Schema (complex)
- Use protobuf `Struct` type for flexible representation

#### 5. Display / Rendering Code Must Be Updated
Three rendering paths consume `SchemaDefinition` directly:
- **`explain` command** — `printSchemaProperties()` iterates `Properties` map, reads `Type`, `Enum`, `Required`, `Description`, `Default`, `Example`
- **`get provider` command** — detail view, JSON/YAML output, TUI all access the same fields
- **TUI** — generates example YAML snippets from schema properties

These must be rewritten to traverse `jsonschema.Schema`'s tree structure (which can be nested/recursive). The rendering logic becomes more complex but also more capable.

#### 6. Custom Validation Error Messages Are Lost
Our custom `ValidationError` type provides structured errors with `Field`, `Constraint`, `Actual`, `Expected`. The `jsonschema` library returns its own error types. We'd need to map these or accept the library's error format, which may not match our current UX.

#### 7. Type System Mismatch: `int` vs `float`
JSON Schema has `integer` and `number` but no `int` / `float` distinction like our `PropertyType`. Our `PropertyTypeInt` maps to JSON Schema `integer`, `PropertyTypeFloat` to `number`. This is a minor translation concern but could cause confusion for providers that currently distinguish int from float at the Go level.

#### 8. Increased Dependency Weight
While `jsonschema-go` is already in `go.mod`, making it the core of provider validation increases coupling to this library. Any breaking changes or bugs in the library directly affect all providers.

#### 9. Schema Authoring Complexity for Plugin Developers
External plugin authors currently define simple flat schemas. With `jsonschema.Schema`, they could write arbitrarily complex schemas — which is powerful but also means more ways to get it wrong. We'd need good documentation and possibly helper functions to keep things approachable.

#### 10. `CapabilityRequiredOutputFields` Validation Changes
The current `ValidateDescriptor()` checks that output schemas include required fields per capability (e.g., validation must have `valid: bool`). With JSON Schema, we'd need meta-schemas or custom validation logic to enforce these constraints, since `jsonschema.Schema.Properties` is `map[string]*jsonschema.Schema` and checking types requires inspecting nested schema objects.

---

## Impact Assessment

### Files Requiring Changes

| Area | Files | Effort |
|------|-------|--------|
| Core types | `pkg/provider/provider.go` | Medium — remove 3 types, update 2 fields |
| Validation | `pkg/provider/validation.go` | High — rewrite or remove ~300 lines |
| Executor | `pkg/provider/executor.go` | Low — update `ValidateInputs` call |
| 14 builtin providers | `pkg/provider/builtin/*/` | High — rewrite all `Descriptor()` schema construction |
| gRPC plugin | `pkg/plugin/grpc.go` | Medium — redesign proto conversion |
| Explain command | `pkg/cmd/scafctl/explain/provider.go` | Medium — rewrite `printSchemaProperties()` |
| Get provider command | `pkg/cmd/scafctl/get/provider/provider.go` | Medium — update detail/JSON/YAML rendering |
| Get provider TUI | `pkg/cmd/scafctl/get/provider/tui.go` | Medium — update TUI rendering + example generation |
| Schema registration | `pkg/schema/kinds.go` | Low — update `PropertyDefinition` registration |
| Descriptor validation | `pkg/provider/provider.go` | Medium — rewrite `ValidateDescriptor()` |
| Tests | Multiple test files | High — update all test schema constructions |
| Integration tests | `tests/integration/cli_test.go` | Low — likely no changes (tests CLI output, not schemas) |

### Backward Compatibility

This is a **breaking change** for:
- External plugin authors who define `SchemaDefinition` in their providers
- Any code that programmatically reads provider schemas
- gRPC plugin protocol (proto messages change)

Per project conventions, breaking changes are allowed since the app is not in production.

---

## Migration Strategy (if approved)

### Option M1: Big Bang
Migrate everything in one PR. Clean but large — estimated 30+ files changed.

### Option M2: Incremental with Adapter
1. Add `jsonschema.Schema` fields alongside existing ones (e.g., `JSONSchema *jsonschema.Schema`)
2. Update validation to prefer `JSONSchema` when present
3. Migrate providers one by one
4. Remove old types when all providers are migrated

### Option M3: Helper Functions
Create builder helpers to reduce verbosity:

```go
// Helper functions to make jsonschema construction less painful
func StringProp(desc string, opts ...PropOption) *jsonschema.Schema { ... }
func IntProp(desc string, opts ...PropOption) *jsonschema.Schema { ... }
func ObjectSchema(required []string, props map[string]*jsonschema.Schema) *jsonschema.Schema { ... }

// Usage
Schema: ObjectSchema([]string{"url", "method"}, map[string]*jsonschema.Schema{
    "url":    StringProp("The URL to request", WithExample("https://example.com"), WithPattern(`^https?://.*`)),
    "method": StringProp("HTTP method", WithEnum("GET", "POST", "PUT", "DELETE")),
    "headers": ObjectProp("HTTP headers", WithAdditionalProperties(StringProp(""))),
})
```

This approach preserves readability while using `jsonschema.Schema` under the hood.

---

## Alternatives Considered

### Keep Custom Types + Add Optional JSON Schema
Add an optional `JSONSchema *jsonschema.Schema` field to `Descriptor` for providers that need full expressiveness. Keep `SchemaDefinition` for simple cases. **Downside:** Three schema systems instead of two.

### Switch to OpenAPI Schema (subset)
Use an OpenAPI-aligned schema type. **Downside:** Still non-standard in Go ecosystem, and `jsonschema-go` is already available.

### Enhance SchemaDefinition Incrementally
Add `object` type, nested properties, array item schemas to the existing types. **Downside:** We'd be re-inventing JSON Schema piecemeal.

---

## Open Questions

1. **How do we handle `IsSecret`?** Use `x-is-secret` extension, `writeOnly`, or a separate mechanism on `Descriptor`?
2. **Should we build schema helpers?** (Option M3) Or accept the verbosity trade-off?
3. **What about the gRPC proto?** Serialize as JSON bytes or create a comprehensive proto message?
4. **Do we want auto-generation from Go structs?** If so, `jsonschema.For[T]()` becomes a strong motivator, but requires that all providers use `Decode` with typed structs.
5. **Should `CapabilityRequiredOutputFields` become meta-schemas?** Or keep as Go-level validation?
6. **Timeline?** Is this worth doing now, or defer until there's a concrete need for nested schemas?

---

## Recommendation

**Proceed with the migration.** Use **Option M1 (big bang)** combined with **Option M3 (helper functions)**.

### Rationale

The strongest argument for migrating is that **we already have two schema systems** and maintaining both is strictly worse than having one. Action result validation uses `jsonschema.Schema`; provider validation uses `SchemaDefinition`. Every developer touching both areas must context-switch between the two. Unifying eliminates that overhead permanently.

The cons are real but manageable:

1. **Breaking change** — not a concern since the app is not in production. This is the ideal time to make structural changes like this.
2. **Verbose Go construction** — solved by builder helpers (Option M3). A small `pkg/provider/schemahelper/` package with functions like `StringProp()`, `ObjectSchema()`, etc. keeps provider code as readable as today, possibly more so.
3. **`IsSecret`** — move to a `SensitiveFields []string` on `Descriptor`. This is cleaner than an extension keyword and keeps secret-handling as a provider-level concern rather than a schema concern. JSON Schema's `writeOnly: true` is a poor semantic fit.
4. **gRPC plugin proto** — serialize the schema as JSON bytes in the proto message. Avoids recreating the entire JSON Schema spec in protobuf, and `jsonschema.Schema` already marshals/unmarshals cleanly.
5. **Display code rewrite** — needed anyway if we ever want to show nested object structures in `explain` and `get provider`. The current renderers silently drop all structural information for `any`-typed properties.
6. **Validation error format** — wrap the `jsonschema` library's errors into our existing `ValidationError` type to preserve UX consistency.

The migration cost is a one-time investment. Leaving two schema systems in place is an ongoing cost paid on every new provider, every plugin, and every contributor onboarding.

### Recommended Implementation Plan

1. **Create `pkg/provider/schemahelper/`** — builder helpers for constructing `jsonschema.Schema` objects ergonomically. Include `StringProp`, `IntProp`, `BoolProp`, `ArrayProp`, `ObjectProp`, `ObjectSchema`, and option functions like `WithRequired`, `WithExample`, `WithPattern`, `WithEnum`, `WithDefault`, `WithMinLength`, `WithMaxLength`, `WithMinimum`, `WithMaximum`, etc.

2. **Update `Descriptor`** — replace `Schema` and `OutputSchemas` types, add `SensitiveFields []string`, remove `SchemaDefinition`, `PropertyDefinition`, `PropertyType`, and `CapabilityRequiredOutputFields`.

3. **Rewrite `validation.go`** — replace the custom validation pipeline with `schema.Resolve(nil).Validate(data)` (same pattern as `action.ValidateResult`). Map library errors to `ValidationError` for consistent UX. This should remove ~300 lines of hand-rolled validation logic.

4. **Migrate all 14 builtin providers** — rewrite `Descriptor()` using the helper functions. Mechanical but straightforward; each provider is independent so this can be split across reviewers.

5. **Update `ValidateDescriptor()`** — express capability required output fields as meta-schemas (e.g., a schema that validates that the validation capability's output schema has `valid: boolean` and `errors: array`).

6. **Update rendering** — `explain`, `get provider`, and TUI code. Teach the renderers to traverse `jsonschema.Schema` recursively for nested properties.

7. **Update gRPC plugin layer** — serialize `jsonschema.Schema` as JSON bytes in proto, deserialize on the other side.

8. **Update tests** — migrate all test schema constructions. Consider adding a shared test helpers file.

All of this fits in a single PR. Estimated scope: ~30-40 files, but the changes are repetitive and low-risk since the test suite will catch regressions.
