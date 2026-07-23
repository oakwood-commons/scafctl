---
name: cel-patterns
description: "CEL expression patterns, context variables, built-in functions, and pitfalls for scafctl. Use when working on CEL evaluation, expression compilation, resolver expressions, or the celexp package."
---

# CEL Patterns in scafctl

> Function reference is not duplicated here. For the authoritative, version-accurate
> list of CEL functions (built-in and custom scafctl functions), call the MCP tool
> **`list_cel_functions`** (filter with `custom_only: true` for scafctl extensions).
> Test any expression with **`evaluate_cel`** / **`validate_expression`**. This skill
> covers the knowledge those tools do not: context variables, patterns, pitfalls,
> and the cost model.

## Context Variables

Context variables are the special values injected into CEL evaluation (they are
**not** functions, so `list_cel_functions` does not cover them). For the
authoritative per-phase matrix -- which variable is available in resolve vs.
transform vs. validate vs. forEach vs. action vs. state-backend vs. error
contexts -- call the MCP tool **`list_context_variables`** (optionally filtered
by `phase`), or read the narrative in `explain_concepts name=context-variables`.

Quick orientation (see the tool for the full, correct scoping):

- `_` -- map of all resolved values (`_.my_resolver`, `_.config.database.host`).
  Referencing `_.other` also creates an implicit dependency edge.
- `__self` -- the current resolver's in-progress value (transform, validate, a
  resolve-phase `until` condition, and forEach).
- `__item` / `__index` -- current element / 0-based index inside `forEach`.
- `__plan` -- pre-execution resolver topology (`__plan["name"].phase`).
- `__execution` / `__actions` / `__cwd` -- available to actions.
- `__params` -- raw CLI params, state-backend inputs only.
- `__error` -- failure contexts (a string in resolvers; a structured map in
  actions -- use `__error.message`, `__error.statusCode`).

Most of these are injected into **both** CEL and Go templates. See
`explain_concepts name=context-variables` for the exact language/phase details.

## Common Patterns

### Conditional Values (Ternary)

```cel
_.env == "prod" ? "https://api.example.com" : "http://localhost:8080"
```

### Default Values

```cel
has(_.optional) ? _.optional : "default"
```

### String Building

```cel
_.prefix + "-" + _.name + "-" + _.suffix
```

### List Transformation Pipeline

```cel
_.items.filter(x, x.enabled).map(x, x.name).sort().join(", ")
```

### Type Coercion

```cel
int(_.port)           // String to int
string(_.count)       // Int to string
double(_.value)       // To float
```

### Nested Has Checks

```cel
has(_.config) && has(_.config.database) && has(_.config.database.host)
```

## Design Patterns from Solution Authors

- **Prefer `when` clauses over ternaries** for conditional resolvers -- cleaner DAG
- **Use `transform` phase** for reshaping instead of complex inline CEL
- **Keep resolvers small and focused** -- one value per resolver
- **Use `cel.bind()`** to avoid repeating long expressions

## Pitfalls

| Pitfall | Wrong | Right |
|---------|-------|-------|
| Trim function | `trimSpace()` | `trim()` |
| Null check | `_.x != null` | `has(_.x)` |
| Type mismatch | `_.port + 1` (if port is string) | `int(_.port) + 1` |
| Empty string | `_.name == ""` | `size(_.name) == 0` or `_.name == ""` (both work) |
| Missing field | `_.missing.field` (runtime error) | `has(_.missing) ? _.missing.field : default` |
| List on non-list | `_.value.filter(...)` (if scalar) | Check type first or ensure resolver type is `array` |

## Reference Extraction

To see which resolver references an expression produces (for dependency
inference), use the MCP tool **`extract_resolver_refs`**. The underlying static
analysis lives in `pkg/celexp/refs.go`:

- `RequiredVariables(ctx)` / `GetUnderscoreVariables(ctx)` -- extract variable
  and `_.` resolver references.
- `MapLiteralKeys(ctx) ([]string, bool)` -- when the expression's top-level node
  is a **map literal** with constant string keys (e.g.
  `{"appName": _.appName}`), returns those keys and `true`. Returns `nil, false`
  for anything else (function calls like `map.merge(...)`, identifiers, lists,
  or non-string/non-constant keys). This lets a go-template `data:` input's key
  set be determined statically so bare `{{ .field }}` template accessors are not
  mis-inferred as resolver dependencies (see the go-template-patterns skill).

## Key Packages

- `pkg/celexp/`: Expression type, Compile/Eval, EvaluateExpression convenience
- `pkg/celexp/env/`: CEL environment setup, extension loading, caching
- `pkg/celexp/ext/`: Custom function registration (regex, arrays, map, guid, time, sort, out)
- `pkg/celexp/refs.go`: static reference extraction (RequiredVariables, MapLiteralKeys)
- `pkg/provider/builtin/celprovider/`: CEL provider for transform phase

## Cost Limits

CEL expressions run under a cost limit (default 1,000,000) to prevent runaway
computations, enforced by cel-go's cost tracker. For the full model -- the
`spec.options.cel.costLimit` override, the `min(solution, global)` semantics,
and the high-cost anti-patterns -- see `explain_concepts name=cel-cost-model`.

Quick reference: a solution can only *lower* the limit
(`spec.options.cel.costLimit`), never raise it above the operator global.

### Avoiding High-Cost Patterns

| Pattern | Cost | Alternative |
|---------|------|-------------|
| `list.filter(x, list2.exists(y, ...))` | O(n*m) | Use `arrays.groupBy` + lookup |
| `list.map(x, list.filter(...))` | O(n^2) | Pre-group with `arrays.groupBy` |
| Nested `map` + `filter` | Multiplicative | Single-pass with custom extension |

## See Also

- MCP tools: `list_cel_functions`, `evaluate_cel`, `validate_expression`,
  `list_context_variables`, `extract_resolver_refs`.
- Concepts (`explain_concepts name=<...>`): `context-variables`, `cel-cost-model`,
  `phase-execution`.
- The `go-template-patterns` skill for the Go-template side.
