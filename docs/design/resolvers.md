---
title: "Resolvers"
weight: 2
---

# Resolvers

## Purpose

Resolvers produce named data values. They exist to gather, normalize, validate, and emit data in a deterministic way so that actions and other resolvers can consume it without re-computation or implicit behavior.

Resolvers are the only mechanism for introducing data into a solution. Actions never fetch or derive data on their own.

Resolvers do not cause side effects. They only compute values.

---

## Implementation Status

| Feature | Status | Location |
|---------|--------|----------|
| Resolver struct (Name, Description, Type, When, etc.) | ✅ Implemented | `pkg/resolver/resolver.go` |
| Resolver Context (`sync.Map`, thread-safe) | ✅ Implemented | `pkg/resolver/context.go` |
| ValueRef (Literal, Resolver, Expr, Tmpl) | ✅ Implemented | `pkg/spec/valueref.go` |
| Phase-based execution (DAG ordering) | ✅ Implemented | `pkg/resolver/phase.go` |
| Dependency extraction (CEL, templates, `dependsOn`) | ✅ Implemented | `pkg/resolver/graph.go` |
| Cycle detection | ✅ Implemented | Uses `pkg/dag` |
| Type coercion (string, int, float, bool, array, object, any) | ✅ Implemented | `pkg/spec/types.go` |
| Additional types: `time`, `duration` | ✅ Implemented | `pkg/spec/types.go` |
| Special symbols (`__self`, `__item`, `__index`) | ✅ Implemented | `pkg/resolver/executor.go` |
| Iteration aliases (`item`, `index` in forEach) | ✅ Implemented | `pkg/spec/foreach.go` |
| Error handling (ExecutionError, AggregatedValidationError) | ✅ Implemented | `pkg/resolver/errors.go` |
| Redaction for sensitive values | ✅ Implemented | `RedactedError`, snapshots |
| Timeout configuration (resolver, phase, default) | ✅ Implemented | `ExecutorOption` functions |
| Concurrency control (`maxConcurrency`) | ✅ Implemented | `WithMaxConcurrency()` |
| Progress callbacks | ✅ Implemented | `ProgressCallback` interface |
| Snapshots | ✅ Implemented | `pkg/resolver/snapshot.go` |
| Graph visualization (DOT, Mermaid, ASCII, JSON) | ✅ Implemented | `pkg/resolver/graph.go` |
| Prometheus metrics | ✅ Implemented | `pkg/resolver/metrics.go` |
| forEach in transform | ✅ Implemented | `ForEachClause` |
| forEach `keepSkipped` (nil retention opt-in) | ✅ Implemented | `ForEachClause.KeepSkipped` |
| forEach nil filtering (default behavior) | ✅ Implemented | `pkg/resolver/executor.go`, `pkg/spec/foreach.go` (`KeepSkipped`) |
| `onError` behavior | ✅ Implemented | `ErrorBehavior` type |
| ValidateAll mode (`--validate-all`) | ✅ Implemented | `WithValidateAll()` |
| SkipValidation mode (`--skip-validation`) | ✅ Implemented | `WithSkipValidation()` |
| Value size limits | ✅ Implemented | `WarnValueSize`, `MaxValueSize` |
| Run resolver command | ✅ Implemented | `pkg/cmd/scafctl/run/resolver.go` |

---

## Responsibilities

A resolver is responsible for:

- Declaring how a value is obtained
- Normalizing or deriving the value using pure computation
- Validating the final value
- Emitting a named result into the resolver context

A resolver is not responsible for:

- Performing side effects
- Orchestrating execution
- Rendering output
- Mutating shared state

---

## Resolver Context

Resolvers evaluate expressions against a single resolver context object named `_`.

**Key principles:**

- `_` contains only resolver outputs
- Nothing appears in `_` unless explicitly emitted by a resolver
- If a solution needs metadata (version, environment, etc.), it must define a resolver (e.g., `meta`) and populate it using a provider

**Special symbols:**

- `__self` refers to the current value being transformed or validated
- `__item` refers to the current element in a forEach iteration
- `__index` refers to the current zero-based index in a forEach iteration
- In the resolve phase, `__self` represents the value from the previous source (available in `until:` conditions)
- In the transform phase, `__self` is the value from the previous transform step
- In the validate phase, `__self` is the final transformed value
- Resolver names cannot be prefixed with `__` (reserved for internal use)

**Implementation:**

Resolver results are stored in a thread-safe map (`sync.Map`) that is request-scoped (lives in the context). This map is exposed to CEL expressions as the `_` variable with type `map[string]any`.

**Thread Safety:**

- The resolver map uses `sync.Map` for concurrent read/write safety
- All resolver writes are atomic and happen immediately after the emit phase
- Reads from `_` within CEL expressions are safe during concurrent resolver execution
- Provider implementations must be thread-safe as they may execute concurrently within the same phase
- No additional locking is required when accessing resolver values via `_`

**Execution Model:**

Resolvers are executed in phases based on their dependency graph:

- Resolvers are grouped into phases where all resolvers in phase N have no dependencies on each other
- All resolvers in phase N must complete before any resolver in phase N+1 begins
- Within a phase, resolvers execute concurrently (order is non-deterministic)
- Each resolver writes to the `sync.Map` **immediately** after completing its emit phase
- This ensures that failed resolvers can emit partial values that are visible to dependent resolvers before they fail

---

## Type System

Resolvers support optional type declarations for validation and automatic type coercion.

### Supported Types

- `string` - Text values
- `int` - Integer numbers
- `float` - Floating-point numbers
- `bool` - Boolean true/false
- `array` - Ordered lists (coerces single values to single-element arrays)
- `object` - Key-value maps (`map[string]any`). Rejects non-map values.
- `time` - Time values (parses ISO 8601 strings like `2026-01-14T12:00:00Z`)
- `duration` - Duration values (parses Go duration strings like `5m`, `1h30m`, `500ms`)
- `any` - No type constraint (default). Accepts any value with no validation or coercion.

### Type Aliases

For convenience, the following aliases are supported:

- `timestamp`, `datetime` → `time`
- `integer` → `int`
- `number` → `float`
- `boolean` → `bool`
- `map` → `object`

### Type Declaration

Types are declared at the resolver level:

~~~yaml
spec:
  resolvers:
    port:
      type: int
      resolve:
        with:
          - provider: parameter
            inputs:
              key: port

    name:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: name

    config:
      type: any
      resolve:
        with:
          - provider: parameter
            inputs:
              key: config
~~~

### Type Coercion

When a type is explicitly declared, scafctl will attempt to coerce the resolved value to the declared type:

- `"8080"` → `8080` (string to int)
- `"3.14"` → `3.14` (string to float)
- `"true"` → `true` (string to bool)
- `123` → `"123"` (int to string)
- `"foo"` → `["foo"]` (single value to array)
- `123` → `[123]` (single value to array)
- `["a", "b"]` → `["a", "b"]` (array unchanged)
- `[[1, 2]]` → `[[1, 2]]` (already an array, passes through)
- `"2026-01-14T12:00:00Z"` → `time.Time` (string to time)
- `"5m30s"` → `time.Duration` (string to duration)
- `"-1h"` → `time.Duration` (negative duration)
- `map[string]any{"key": "val"}` → `map[string]any{"key": "val"}` (map to object, validated)

**Coercion rules:**

- Type coercion only occurs when a type is explicitly declared
- If coercion fails, the resolver fails with a type error
- Coercion happens **once**: on the final resolver value, after the last active phase (resolve or transform) completes and before validate begins. The `type` field describes the resolver's **output contract**, not the intermediate value between phases.
- This means transform steps can work with raw provider types (e.g., `map[string]interface{}`) and reshape them freely — only the final output must match the declared type.
- **Array coercion**: Non-array values are wrapped in a single-element array. Already-arrays pass through unchanged. This is useful when a field can accept either a single value or multiple values.
- **Object coercion**: Accepts any map with string keys. Rejects non-map values (strings, ints, arrays, etc.) with a clear error.

### Type Validation

Type validation occurs automatically when a type is declared:

~~~yaml
spec:
  resolvers:
    replicas:
      type: int
      resolve:
        with:
          - provider: parameter
            inputs:
              key: replicas  # Must be coercible to int
~~~

If the value cannot be coerced to the declared type, the resolver fails.

### Type Constraints

Additional constraints (min/max, length, pattern) should be enforced in the `validate` phase, not in the type declaration:

~~~yaml
spec:
  resolvers:
    port:
      type: int
      resolve:
        with:
          - provider: parameter
            inputs:
              key: port
      validate:
        with:
          - provider: validation
            inputs:
              expression: "__self >= 1 && __self <= 65535"
            message: "Port must be between 1 and 65535"
~~~

---

## Resolver Model

### Conceptual Flow

- resolve
  - fetch raw value using providers
- transform
  - apply provider-backed transformations
- validate
  - enforce constraints
- emit
  - publish value into context

Each resolver follows this sequence exactly and in order.

---

## Resolver Phases

Resolvers execute through four fixed phases.

### Resolver Execution Order Visualization

Resolvers are executed in phases based on their dependency graph. Here's a concrete example:

~~~yaml
spec:
  resolvers:
    # Phase 1: No dependencies - execute concurrently
    static_value:
      resolve:
        with:
          - provider: static
            inputs:
              value: "base"

    param_value:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: input

    # Phase 2: Depends only on Phase 1 resolvers - execute concurrently after Phase 1 completes
    computed_from_static:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.static_value + "-derived"

    computed_from_param:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.param_value.toUpperCase()

    # Phase 3: Depends on Phase 2 resolvers - executes after Phase 2 completes
    final_value:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.computed_from_static + "-" + _.computed_from_param
~~~

**Execution flow:**

1. **Phase 1**: `static_value` and `param_value` execute concurrently (no dependencies)
2. **Phase 1 completes**: Both values written to `_` before Phase 2 begins
3. **Phase 2**: `computed_from_static` and `computed_from_param` execute concurrently
4. **Phase 2 completes**: Both values written to `_` before Phase 3 begins
5. **Phase 3**: `final_value` executes (depends on Phase 2 results)

If any resolver in a phase fails, that phase completes its running resolvers, then execution terminates. Subsequent phases never execute.

---

## Resolver Naming Conventions

Resolver names must follow these rules:

**Restrictions:**

- Cannot start with `__` (double underscore) - reserved for internal use
- Cannot contain whitespace

**Allowed formats:**

- `camelCase` - **Recommended best practice**
- `snake_case` - Acceptable
- `kebab-case` - Acceptable
- Any combination of alphanumeric characters, underscores, and hyphens

**Examples:**

~~~yaml
spec:
  resolvers:
    # Recommended (camelCase)
    apiEndpoint:
      resolve: ...

    userName:
      resolve: ...

    # Acceptable (snake_case)
    api_endpoint:
      resolve: ...

    # Acceptable (kebab-case)
    api-endpoint:
      resolve: ...

    # INVALID - starts with __
    __internal:
      resolve: ...
~~~

**Reserved names:**

- `__self` - Used for current value in transform/validate contexts
- `__item` - Used for current element in forEach iterations
- `__index` - Used for current index in forEach iterations
- Any name starting with `__` is reserved for future internal use

---

## Empty and Null Value Handling

Null values are valid and have specific handling semantics in scafctl resolvers.

### Null as Valid Value

- A resolver can successfully emit `null` as its value
- `null` is treated as a valid emitted value and stored in `_`
- Dependent resolvers can access and reference `null` values

### Null Behavior in Resolve Phase

When using `until:` with null values:

~~~yaml
spec:
  resolvers:
    name:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: name
          - provider: env
            inputs:
              key: PROJECT_NAME
          - provider: static
            inputs:
              value: null
          - provider: static
            inputs:
              value: "fallback"
        until:
          expr: __self != null
~~~

**Evaluation with null:**

- `__self != null` evaluates to `false` when `__self` is `null` (standard boolean logic)
- When `until:` evaluates to `false`, processing continues to the next source
- In the example above, all four sources would be evaluated because the first three could return `null`

### Empty vs Null vs Missing

- **`null`**: Resolver executed and emitted `null` - accessible via `_.resolverName`, value is `null`
- **Empty string** (`""`): Valid string value, different from `null`
- **Missing**: Resolver did not execute (e.g., `when: false`) - resolver does not exist in `_`, must check with `has(_.resolverName)`

### Checking for Null Values

~~~yaml
spec:
  resolvers:
    optional_value:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: optional

    dependent:
      resolve:
        with:
          - provider: cel
            inputs:
              # Check if resolver exists AND is not null
              expression: has(_.optional_value) && _.optional_value != null ? _.optional_value : "default"
~~~

---

## 1. Resolve

The resolve phase selects an initial value from one or more sources.

~~~yaml
resolve:
  with:
    - provider: parameter
      inputs:
        key: name
    - provider: env
      inputs:
        key: PROJECT_NAME
    - provider: static
      inputs:
        value: my-app
~~~

Key properties:

- Uses providers explicitly
- Sources are evaluated in order
- **Default behavior** (when `until:` is not specified): All sources are evaluated until the first non-null value is found
- Providers return data via `ProviderOutput` structure (containing data, optional warnings, and metadata)
- **Error handling**: When a source fails, the next source is tried automatically (fallback chain semantics). Set `onError: fail` on a source to stop the chain on failure.

Optional controls:

- `when:` to conditionally skip the resolve phase or a source (must be a boolean)
- `until:` to stop evaluation early (must be a boolean)

### `until:` in Resolve

The `until:` control in the resolve phase stops source evaluation when a condition is met:

- **Evaluation timing**: Checked **after** each source completes
- **Early termination**: When the condition evaluates to `true`, processing stops and the current value is emitted
- **Default behavior**: When `until:` is not specified, all sources are evaluated until the first non-null value is encountered
- **Use case**: Stop at first non-null value or when a specific condition is satisfied

**Example:**

~~~yaml
resolve:
  with:
    - provider: parameter
      inputs:
        key: name
    - provider: env
      inputs:
        key: PROJECT_NAME
    - provider: static
      inputs:
        value: my-app
  until:
    expr: __self != null  # Stop at first non-null value
~~~

### `when:` Rule

`when:` in `resolve` (and per-source `when:`) may reference only previously emitted resolvers.

Example:

~~~yaml
spec:
  resolvers:
    foo:
      resolve:
        with:
          - provider: static
            inputs:
              value: hello

    name:
      resolve:
        with:
          - provider: env
            when:
              expr: _.foo == "hello"
            inputs:
              key: PROJECT_NAME
          - provider: static
            inputs:
              value: my-app
~~~

The resolve phase answers: where does the value come from?

---

### ForEach Filter Property

Resolvers that produce arrays from item-by-item resolution can use `forEach` at the resolve level. Each item is resolved independently using a nested `resolve` block, and the results are collected into an output array.

~~~yaml
resolvers:
  activeUsers:
    type: '[]object'
    resolve:
      forEach:
        items:
          expr: allUsers
        as: user
        filter: true  # Remove nil entries from output
        resolve:
          with:
            - provider: static
              when:
                expr: 'user.active == true'
              inputs:
                value:
                  expr: user
~~~

**ForEach Fields (resolve phase):**

| Field | Description | Required |
|-------|-------------|----------|
| `items` | ValueRef pointing to the source array | Yes |
| `as` | Variable alias for the current element | Yes |
| `filter` | When `true`, nil results are removed from the output array | No (default: `false`) |
| `resolve` | Nested resolve phase executed for each element | Yes |

**`filter: true` behavior:**

Without `filter: true`, items where the nested `resolve` returns `nil` (e.g., when a `when` condition is false) are included as `nil` entries in the output array, preserving index alignment with the input:

```
input:  [user1, user2, user3, user4]
output: [user1, nil,   user3, nil  ]   # user2 and user4 skipped by when
```

With `filter: true`, `nil` entries are removed:

```
input:  [user1, user2, user3, user4]
output: [user1, user3]                 # only matched items
```

This is more ergonomic than adding a separate transform step to strip `nil` entries when using `when` conditions inside `forEach`.

---

## 2. Transform

The transform phase derives a new value from the resolved value.

Transform steps are provider-backed, but differ from resolve in intent:

- Resolve selects a value
- Transform modifies an existing value

---

### Transform as Provider Execution

Each transform step is executed by a provider. Transform is not limited to expression evaluation. Any provider that is pure and side-effect-free may participate in transform.

The distinction is semantic:

- Resolve selects an initial value from sources
- Transform derives new values from an existing value

All phases now use the `with:` keyword for consistency and simplicity.

#### Example Using Multiple Providers

~~~yaml
transform:
  with:
    - provider: filesystem
      inputs:
        operation: read
        path: ./templates/base-name.txt

    - provider: cel
      inputs:
        expression: __self.trim()

    - provider: cel
      inputs:
        expression: __self.toLowerCase()

    - provider: cel
      inputs:
        expression: __self.replace("_", "-")
~~~

In this example:

- The filesystem provider supplies a value derived from local state
- Subsequent providers normalize and shape that value
- Each step receives the previous value as `__self`

Transform providers must be deterministic and free of externally visible side effects.

---

### forEach: Iterating Over Arrays

The `forEach` clause enables parallel iteration over array values during the transform phase. Each element is processed independently by the specified provider, and results are collected back into an array preserving the original order.

#### Basic Syntax

~~~yaml
transform:
  with:
    - provider: cel
      forEach:
        item: user    # Variable name for current element (optional, defaults to __item)
        index: i      # Variable name for current index (optional, defaults to __index)
      inputs:
        expression: |
          {
            "name": user.name.toUpperCase(),
            "index": i
          }
~~~

#### Iteration Variables

Within a forEach iteration, you have access to:

- `__item` - The current array element (built-in)
- `__index` - The current array index (built-in)
- Custom aliases via `item` and `index` fields in `forEach` clause

Both the built-in names and custom aliases are available simultaneously:

~~~yaml
forEach:
  item: user     # Access element as 'user' or '__item'
  index: idx     # Access index as 'idx' or '__index'
~~~

#### Custom Iteration Source

By default, forEach iterates over `__self` (the current resolver value). Use the `in` field to specify a different source:

~~~yaml
transform:
  with:
    - provider: http
      forEach:
        item: endpoint
        in:
          rslvr: endpoints  # Iterate over another resolver's value
      inputs:
        url:
          expr: endpoint.url
        method: GET
~~~

The `in` field accepts all ValueRef forms:

- `rslvr:` - Reference another resolver
- `expr:` - CEL expression
- `tmpl:` - Go template
- Literal array value

#### Concurrency Control

By default, forEach executes iterations in parallel without limits. Use `concurrency` to limit parallel executions:

~~~yaml
transform:
  with:
    - provider: http
      forEach:
        item: url
        concurrency: 5  # Max 5 concurrent HTTP requests
      inputs:
        url:
          expr: url
~~~

Setting `concurrency: 1` forces sequential execution.

#### Conditional Iteration with `when`

Use the `when` clause to conditionally execute iterations. The condition has access to iteration variables:

~~~yaml
transform:
  with:
    - provider: cel
      forEach:
        item: num
      when:
        expr: "num % 2 == 0"  # Only process even numbers
      inputs:
        expression: "num * 2"
~~~

When a `when` condition evaluates to `false`, the item is **automatically removed** from the output array. This means the output length equals the number of items that matched the condition — the most useful default for filtering patterns.

To retain index alignment with the input array (keeping `nil` placeholders for skipped items), set `keepSkipped: true` on the `forEach` clause:

~~~yaml
transform:
  with:
    - provider: cel
      forEach:
        item: num
        keepSkipped: true   # opt-in: preserves nil for skipped items
      when:
        expr: "num % 2 == 0"
      inputs:
        expression: "num * 2"
~~~

#### Error Handling with `onError`

Control error behavior during iteration:

- `onError: stop` (default) - Stop on first error, propagate error
- `onError: continue` - Continue processing remaining items, collect results with error metadata

With `onError: continue`, each result is wrapped in a `ForEachIterationResult`:

~~~yaml
transform:
  with:
    - provider: http
      forEach:
        item: url
      onError: continue
      inputs:
        url:
          expr: url
~~~

Result structure with `onError: continue`:

~~~json
[
  {"data": {"status": 200}, "error": ""},
  {"data": null, "error": "connection timeout"},
  {"data": {"status": 200}, "error": ""}
]
~~~

#### Chaining forEach Steps

Multiple forEach steps can be chained. Each step receives the previous step's output array:

~~~yaml
transform:
  with:
    # Step 1: Double each number
    - provider: cel
      forEach: {}
      inputs:
        expression: "__item * 2"
    
    # Step 2: Add 1 to each result
    - provider: cel
      forEach: {}
      inputs:
        expression: "__item + 1"
~~~

Input `[1, 2, 3]` → Step 1 → `[2, 4, 6]` → Step 2 → `[3, 5, 7]`

#### Empty Array Handling

If the input array is empty, forEach returns an empty array `[]` without executing the provider.

#### Non-Array Input Error

If the input is not an array/slice, forEach returns a `ForEachTypeError`. Ensure your resolve phase produces an array when using forEach.

#### Order Preservation

Results are always returned in the same order as the input array, regardless of the order in which parallel iterations complete.

---

> **Skipped item behavior**: When a `when` condition skips an item, the default is to remove it from the output (auto-filter). Use `keepSkipped: true` on the `forEach` clause to retain `nil` placeholders and preserve index alignment with the input array.

---

#### Complex Provider Chaining Examples

**Example 1: HTTP → JSON Parse → CEL → Base64**

~~~yaml
spec:
  resolvers:
    encodedApiData:
      resolve:
        with:
          - provider: static
            inputs:
              value: "https://api.example.com/config"
      transform:
        with:
          # Step 1: Fetch data from HTTP endpoint
          - provider: http
            inputs:
              url: 
                expr: __self
              method: GET
          # Step 2: Parse JSON response
          - provider: jq
            inputs:
              expression: __self.apiKey
          # Step 4: Encode result
          - provider: base64
            inputs:
              expression: |
                {
                  "name": __self.metadata.name,
                  "endpoint": __self.spec.host + ":" + string(__self.spec.port),
                  "timeout": __self.spec.timeout.seconds + "s"
                }
# Result: Structured config object with computed endpoint
~~~

**Example 3: Multi-stage data transformation**

~~~yaml
spec:
  resolvers:
    processedData:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: rawData
      transform:
        with:
          # Step 1: Parse CSV input
          - provider: csv
            inputs:
              expression: __self.filter(row, row.status == "active")
          # Step 3: Transform each row
          - provider: cel
            inputs:
              expression: |
                __self.map(row, {
                  "id": row.id,
                  "name": row.name.toUpperCase(),
                  "processedAt": now()
                })
          # Step 4: Convert to JSON
          - provider: json
            inputs:
              match: "^[a-z0-9-]+$"
            message: "Must be lowercase alphanumeric with hyphens"
          - provider: validation
            inputs:
              notMatch: "^test$"
            message: "Must not be 'test'"
          - provider: validation
            inputs:
              expression: "__self.length() >= 3"
            message: "Must be at least 3 characters"
~~~

**If user provides `name=A`:**

- Validation 1: ❌ fails (contains uppercase)
- Validation 2: ✅ passes (not "test")
- Validation 3: ❌ fails (only 1 character)

**Error output:**
```
Error: Resolver 'name' validation failed:
  - Must be lowercase alphanumeric with hyphens
  - Must be at least 3 characters
```

Only the failed validation messages are included in the error. The transformed value `"A"` is still emitted to `_` for use by error handlers or logging.

### Validation Messages

Validation messages support all four input forms:

- **Literal string**: Static error message
- **Template (`tmpl`)**: Dynamic message with Go templating
- **Expression (`expr`)**: Dynamic message computed via CEL
- **Resolver (`rslvr`)**: Message from another resolver

### Examples

Literal regex match:

~~~yaml
validate:
  with:
    - provider: validation
      inputs:
        match: "^[a-z0-9-]+$"
      message: "Must be lowercase alphanumeric with hyphens"
~~~

Regex not match:

~~~yaml
validate:
  with:
    - provider: validation
      inputs:
        notMatch: "^fff$"
      message: "Must not be fff"
~~~

Combining match and notMatch:

~~~yaml
validate:
  with:
    - provider: validation
      inputs:
        match: "^[a-z0-9-]+$"
        notMatch: "^fff$"
      message: "Must be lowercase alphanumeric and not fff"
~~~

Dynamic pattern using expression:

~~~yaml
validate:
  with:
    - provider: validation
      inputs:
        match:
          expr: "\"^\" + _.allowedPrefix + \"-[a-z0-9]+$\""
      message: "Must match allowed prefix pattern"
~~~

Pattern from resolver:

~~~yaml
validate:
  with:
    - provider: validation
      inputs:
        match:
          rslvr: namePattern
      message: "Must match naming convention"
~~~

CEL expression validation:

~~~yaml
validate:
  with:
    - provider: validation
      inputs:
        expression: "__self in [\"dev\", \"staging\", \"prod\"]"
      message: "Invalid environment"
~~~

Dynamic message using template:

~~~yaml
validate:
  with:
    - provider: validation
      inputs:
        match: "^[a-z0-9-]+$"
      message:
        tmpl: "Value '{{ .__self }}' must match pattern {{ _.namePattern }}"
~~~

Dynamic message using expression:

~~~yaml
validate:
  with:
    - provider: validation
      inputs:
        expression: "__self.length() >= 3"
      message:
        expr: "'Value must be at least 3 characters, got ' + string(__self.length())"
~~~

Message from resolver:

~~~yaml
validate:
  with:
    - provider: validation
      inputs:
        match: "^[a-z-]+$"
      message:
        rslvr: validationMessages.nameFormat
~~~

---

## 4. Emit

If resolve, transform, and validate succeed, the resolver emits its value.

- The value becomes available as `_.<resolverName>`
- Values are immutable after emission
- Emission is implicit and cannot be customized

### Value Immutability

**Important:** Resolver value immutability is enforced by **convention**, not by runtime checks.

**Behavior:**

- scafctl does **not** perform deep copying of resolver values
- Maps, arrays, and objects emitted by resolvers are stored by reference in the resolver context
- Mutating a resolver value from CEL or provider code may cause unexpected behavior
- Resolvers and actions should treat all values from `_` as read-only

**Safe practices:**

~~~yaml
spec:
  resolvers:
    baseConfig:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                timeout: 30
                retries: 3

    # Good: Create new object, don't mutate baseConfig
    extendedConfig:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                {
                  "timeout": _.baseConfig.timeout,
                  "retries": _.baseConfig.retries,
                  "newField": "value"
                }

    # Unsafe: Attempting to mutate (behavior undefined)
    # This pattern should be avoided
    mutatedConfig:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                _.baseConfig.timeout = 60  # DO NOT DO THIS
~~~

**Why no enforcement?**

- Performance: Deep copying every resolver value would add significant overhead
- CEL immutability: CEL expressions naturally create new values rather than mutating existing ones
- Provider responsibility: Providers should return new values, not mutate inputs

If you need to modify a resolver value, create a new value derived from the original.

---

## Provider Output Structure

Providers return data via a standardized structure that carries the value along with optional metadata.

### Structure Definition

~~~go
type ProviderOutput struct {
    Data     any                    // The actual value returned by the provider
    Warnings []string               // Optional warnings (logged but don't stop execution)
    Metadata map[string]any         // Provider-specific metadata (cache status, timing, etc.)
}
~~~

### Field Usage

**`Data` field:**
- Contains the actual value returned by the provider
- Type varies based on provider and context
- Validation providers must return a boolean in `Data`
- Resolve and transform providers can return any type

**`Warnings` field:**
- Contains non-fatal issues encountered during provider execution
- Warnings are logged but do not stop resolver execution
- Examples: cache misses, deprecated API usage, rate limit warnings

**`Metadata` field:**
- Provider-specific information about the operation
- Common keys: `cache_hit`, `request_duration_ms`, `source`, `retry_count`
- Used for observability and debugging
- Not accessible in CEL expressions (internal use only)

### Example

~~~go
// HTTP provider returning cached data
return &ProviderOutput{
    Data: responseBody,
    Warnings: []string{"Response time exceeded 1s"},
    Metadata: map[string]any{
        "cache_hit":           true,
        "request_duration_ms": 1250,
        "status_code":         200,
    },
}
~~~

---

## Feeding Resolver Values Into Providers

Resolvers emit typed values that can be consumed by providers in later resolvers or actions. This is enabled through custom unmarshalling, which allows provider inputs to accept multiple shapes while preserving strong typing.

### Supported Input Forms

Each provider input field may accept one of the following forms.

#### 1. Literal Value

Passed as-is with no evaluation.

~~~yaml
inputs:
  image: nginx:1.27
~~~

#### 2. Direct Resolver Binding (Canonical)

Copies the resolver value directly, preserving its type.

~~~yaml
inputs:
  image:
    rslvr: image
~~~

#### 3. Expression-Based Value (Explicit CEL)

Evaluated using CEL before provider execution.

~~~yaml
inputs:
  image:
    expr: _.org + "/" + _.repo + ":" + _.version
~~~

#### 4. Template-Based Value

Rendered using Go templating. Always produces a string.

~~~yaml
inputs:
  image:
    tmpl: "{{ _.org }}/{{ _.repo }}:{{ _.version }}"
~~~

### Exclusivity Rule

For a single input field, it is an error to specify more than one of:

- literal
- `rslvr`
- `expr`
- `tmpl`

---

### Go Representation (Custom Unmarshalling)

~~~go
import (
  "github.com/oakwood-commons/scafctl/pkg/celexp"
  "github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

type ValueRef struct {
  Literal  any
  Resolver *string
  Expr     *celexp.Expression
  Tmpl     *gotmpl.GoTemplatingContent
}

func (v *ValueRef) UnmarshalYAML(node *yaml.Node) error {
  switch node.Kind {

  case yaml.MappingNode:
    var raw struct {
      Resolver *string                      `yaml:"rslvr"`
      Expr     *celexp.Expression           `yaml:"expr"`
      Tmpl     *gotmpl.GoTemplatingContent  `yaml:"tmpl"`
    }
    if err := node.Decode(&raw); err != nil {
      return err
    }

    count := 0
    if raw.Resolver != nil {
      count++
    }
    if raw.Expr != nil {
      count++
    }
    if raw.Tmpl != nil {
      count++
    }

    if count != 1 {
      return fmt.Errorf("invalid value ref: expected exactly one of rslvr, expr, or tmpl")
    }

    v.Resolver = raw.Resolver
    v.Expr = raw.Expr
    v.Tmpl = raw.Tmpl
    return nil

  default:
    var anyVal any
    if err := node.Decode(&anyVal); err != nil {
      return err
    }
    v.Literal = anyVal
    return nil
  }
}
~~~

All inputs are resolved into concrete values before provider execution. Providers never see expressions, templates, or resolver references.

---

## Resolver Parameters (CLI Overrides)

### Purpose

Resolvers may receive values directly from the CLI using resolver parameters.

Resolver parameters are consumed by the `parameter` provider and participate in normal `resolve.from` ordering. They do not bypass the resolve phase.

---

## CLI Syntax

Resolver parameters are supplied using the `-r` or `--resolver` flag.

~~~bash
scafctl run solution example -r key=value

# Or with run resolver for debugging
scafctl run resolver -f example.yaml -r key=value
~~~

Multiple resolver parameters may be supplied:

~~~bash
scafctl run solution example \
  -r env=prod \
  -r regions=us-east1,us-west1

# Run specific resolvers only (with dependencies)
scafctl run resolver env region \
  -f example.yaml \
  -r env=prod
~~~

Each `-r` maps to a parameter key that may be read by the `parameter` provider.

---

## Supported Resolver Input Forms

Resolver parameters support multiple input forms.

### Literal Strings

~~~bash
-r name=my-app
~~~

### Numbers

~~~bash
-r replicas=3
-r timeout=1.5
~~~

### Booleans

~~~bash
-r dryRun=true
~~~

### CSV Lists

~~~bash
-r environments=dev,qa,prod
~~~

### JSON Values

~~~bash
-r config={"foo":"bar","count":3}
~~~

### Stdin Input

~~~bash
cat config.json | scafctl run solution example -r config=-
~~~

### File References

~~~bash
-r config=file://./config.json
~~~

### URL References

~~~bash
-r data=https://example.com/data.json
~~~

### Parsing Precedence

When multiple formats are ambiguous, scafctl applies the following parsing order:

1. **Stdin check**: If value is exactly `-`, read from stdin
2. **File protocol**: If value starts with `file://`, treat as file reference
3. **HTTP protocol**: If value starts with `http://` or `https://`, treat as URL reference
4. **JSON parse**: If value starts with `{` or `[`, attempt JSON parse
5. **Boolean parse**: If value is exactly `true` or `false` (case-insensitive), parse as boolean
6. **Number parse**: Attempt to parse as integer or float
7. **CSV detection**: If value contains `,` and not enclosed in quotes, split as CSV list
8. **Literal string**: Fallback to treating value as literal string

**Examples:**

- `-r url=https://example.com` → URL reference (rule 3)
- `-r url="https://example.com"` → Literal string (quotes override protocol detection)
- `-r count=42` → Integer (rule 6)
- `-r items=a,b,c` → Array `["a", "b", "c"]` (rule 7)
- `-r items=a -r items=b -r items=c` → Array `["a", "b", "c"]` (rule 7)
- `-r flag=true` → Boolean `true` (rule 5)
- `-r config={"key":"value"}` → JSON object (rule 4)

---

## Interaction with Cobra

scafctl uses Cobra for CLI parsing, but Cobra is intentionally limited to collecting raw resolver input strings.

Cobra responsibilities:

- Register `-r` / `--resolver` flags
- Accept repeated string values
- Preserve input ordering

Cobra does not:

- Parse types
- Decode JSON
- Read files or stdin
- Apply resolver semantics

All parsing, decoding, validation, and error reporting occurs in scafctl core, not in Cobra.

---

## Conditional Execution

Resolvers support conditional execution using the `when:` clause.

### Resolver-Level `when:`

A resolver can be conditionally skipped based on previously emitted resolver values:

~~~yaml
spec:
  resolvers:
    feature_flag:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: enableFeatureX
          - provider: static
            inputs:
              value: false

    feature_x_config:
      when:
        expr: _.feature_flag == true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: featureXConfig
~~~

### Behavior When `when:` is False

If the `when:` condition evaluates to `false`:

- The resolver is **completely absent** from `_`
- The resolver does not execute any phase (resolve, transform, validate)
- No value is emitted
- Dependent resolvers must handle the missing resolver

### Handling Missing Resolvers

Dependent resolvers must check for the existence of conditional resolvers using the `has()` CEL function:

~~~yaml
spec:
  resolvers:
    optional_feature:
      when:
        expr: _.flags.enableX == true
      resolve:
        with:
          - provider: static
            inputs:
              value: "feature-enabled"

    dependent:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: has(_.optional_feature) ? _.optional_feature : "feature-disabled"
~~~

### Phase-Level `when:`

There is **no** phase-level `when:` control. The `when:` clause only appears at:

- Resolver level (skip entire resolver)
- Source level in resolve phase (skip individual source)
- Step level in transform phase (skip individual transform step)

### Rules

- `when:` expressions can only reference previously emitted resolvers
- `when:` must evaluate to a boolean
- **Dependency graph vs execution**: All resolvers (including conditional ones) appear in the dependency graph to determine execution order. However, only resolvers whose `when:` condition evaluates to `true` will execute and emit values to the `sync.Map` (accessible via `_`)
- Resolvers with `when: false` are absent from `_` and must be checked with `has()`
- Circular dependencies involving `when:` are rejected during graph construction

---

## Resolver Dependencies

Resolvers form a directed acyclic graph inferred from references.

~~~yaml
spec:
  resolvers:
    name:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: name

    image:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.name + ":latest"
~~~

Rules:

- Dependencies are inferred from `_` references in the dependency graph
- Explicit dependencies can be declared with `dependsOn` (merged with auto-extracted)
- Execution order is computed automatically from the dependency graph
- Independent resolvers (no dependencies on each other) execute concurrently
- Cycles in the dependency graph are rejected

### Explicit Dependencies (dependsOn)

When dependencies cannot be auto-extracted (e.g., templates loaded from files), use `dependsOn`:

~~~yaml
spec:
  resolvers:
    config:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: config

    formatted-output:
      dependsOn:
        - config
        - credentials
      resolve:
        with:
          - provider: file
            inputs:
              path: "/path/to/template.tmpl"
      transform:
        with:
          - provider: go-template
            inputs:
              name: output-template
              template:
                rslvr: formatted-output
~~~

The `dependsOn` field:
- Accepts a list of resolver names that this resolver depends on
- Is merged with auto-extracted dependencies (not a replacement)
- Validates that referenced resolvers exist
- Cannot reference itself (self-dependency is an error)
- Participates in circular dependency detection

### Circular Dependency Detection

Circular dependencies are detected during graph construction and cause immediate failure.

**Example 1: Direct circular dependency (invalid)**

~~~yaml
spec:
  resolvers:
    a:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.b + "-suffix"
    b:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.a + "-suffix"
~~~

**Error message:**
```
Error: Circular dependency detected in resolvers: a → b → a
```

**Example 2: Indirect circular dependency (invalid)**

~~~yaml
spec:
  resolvers:
    a:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.c + "-a"
    b:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.a + "-b"
    c:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.b + "-c"
~~~

**Error message:**
```
Error: Circular dependency detected in resolvers: a → c → b → a
```

**Example 3: Circular dependency with `when:` clause (invalid)**

~~~yaml
spec:
  resolvers:
    a:
      when:
        expr: _.b == "trigger"  # Creates dependency on b
      resolve:
        with:
          - provider: static
            inputs:
              value: "a-value"
    b:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: has(_.a) ? _.a + "-b" : "default-b"  # Creates dependency on a
~~~

**Error message:**
```
Error: Circular dependency detected in resolvers: a → b → a
```

**Important:** Even though `a` is conditionally executed, it still creates a dependency on `b` in the `when:` clause. The dependency graph includes all resolvers and all references, regardless of conditional execution.

**Example 4: Valid conditional reference (no cycle)**

~~~yaml
spec:
  resolvers:
    feature_flag:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: enableFeature
          - provider: static
            inputs:
              value: false

    feature_config:
      when:
        expr: _.feature_flag == true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: featureConfig

    app_config:
      resolve:
        with:
          - provider: cel
            inputs:
              # No cycle: app_config depends on feature_config, but feature_config doesn't depend on app_config
              expression: has(_.feature_config) ? _.feature_config : {"default": true}
~~~

This is valid because the dependency flow is: `feature_flag` → `feature_config` → `app_config` (no cycles).

---

## Resolver Timeouts

Resolvers support configurable timeouts to prevent hung providers from blocking execution indefinitely.

### Timeout Configuration

Timeouts can be specified at three levels:

1. **Global default**: Applied to all resolvers unless overridden (default: 30 seconds)
2. **Resolver-level**: Specified in resolver definition
3. **Provider-level**: Some providers may have their own internal timeouts

**Timeout precedence:**

- The **resolver timeout is the hard limit** for all resolver execution (all phases combined)
- Provider-level timeouts should be **shorter than or equal to** the resolver timeout
- If a provider has a longer timeout than the resolver, the resolver timeout will cancel the provider's operation when reached
- Provider implementations receive a context with the resolver timeout and should respect context cancellation
- When both are configured, the resolver timeout takes precedence—the operation will be cancelled when the resolver timeout expires, regardless of provider timeout settings

### Resolver-Level Timeout

~~~yaml
spec:
  resolvers:
    external_data:
      timeout: 10s  # This resolver must complete within 10 seconds
      resolve:
        with:
          - provider: http
            inputs:
              url: https://slow-api.example.com/data
~~~

### Timeout Behavior

- Timeout applies to the entire resolver execution (all phases combined)
- On timeout, the resolver fails with a timeout error
- Partial results are emitted according to standard error handling rules
- The timeout context is propagated to the provider implementation
- Providers should respect context cancellation for proper timeout handling

### Best Practices

- Set shorter timeouts for external dependencies (HTTP, database queries)
- Use longer timeouts for complex transformations or validations
- Provider implementations should implement their own internal timeouts for granular control
- Consider retry logic in providers rather than relying solely on resolver timeouts

---

## Error Handling

When a resolver encounters an error during any phase, execution stops and an error is returned.

### Error Propagation

- **Current phase completion**: Resolvers in the current phase (same dependency level) are allowed to complete
- **Subsequent phase halt**: All resolvers in subsequent phases (higher dependency levels) are never executed
- **Execution termination**: Once the current phase completes, the entire resolver execution process terminates with an error

### Partial Emission

Resolvers emit the value from the last successful phase before failure:

- If **resolve** succeeds but **transform** fails → emit the resolved value (pre-transform)
- If **resolve** and **transform** succeed but **validate** fails → emit the transformed value
- The failed resolver's partial value is accessible in `_`

**Important notes about partial values:**

- Partial values are stored in `_` **identically to successful values**
- There is **no flag or marker** indicating a resolver failed
- Dependent resolvers cannot distinguish between successful and partial emission by inspecting `_` alone
- The resolver execution process terminates with an error after the current phase completes
- Partial emission exists primarily for error reporting and debugging

**Checking for resolver failures:**

You cannot programmatically detect resolver failures within the solution configuration because execution stops when a resolver fails. However, partial values are useful for:

- Error messages that reference the failed resolver's value
- Logging and debugging output
- Understanding what data was available before the failure

**Example:**

~~~yaml
spec:
  resolvers:
    userName:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: user  # Returns "ADMIN"
      transform:
        with:
          - provider: cel
            inputs:
              expression: __self.unknownFunc()  # Fails with error
      # Result: _.userName = "ADMIN" (resolved value, before transform)
      # Error is raised, but "ADMIN" is accessible in error context
~~~

In this example, the resolver fails during transform, but `_.userName` contains `"ADMIN"`. This allows error messages to reference the value that caused the problem.

### Example

~~~yaml
spec:
  resolvers:
    # Phase 1
    base:
      resolve:
        with:
          - provider: static
            inputs:
              value: "base-value"

    # Phase 1 (independent of base)
    name:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: name  # Returns "MyApp"
      transform:
        with:
          - provider: cel
            inputs:
              expression: __self.toLowerCase()  # Fails due to error
      # Result: _.name = "MyApp" (resolved value, not transformed)
      # Error occurs, but phase 1 completes (base resolver finishes)

    # Phase 2 (depends on name)
    image:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.name + "-image"  
      # This resolver is NEVER EXECUTED because name failed in phase 1

    # Phase 2 (depends on base)
    derived:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.base + "-suffix"
      # This resolver is NEVER EXECUTED because execution stopped after phase 1
~~~

In this example:

1. Phase 1 executes: `base` succeeds, `name` fails during transform but emits "MyApp"
2. Phase 1 completes (all resolvers in phase 1 finish)
3. Execution terminates with error
4. Phase 2 resolvers (`image`, `derived`) are never executed

### Retry Behavior

Retry logic is a provider concern, not a resolver concern. Providers may implement their own retry mechanisms.

### Validate-All Mode

> **Status**: ✅ Implemented via `--validate-all` flag

By default, resolver execution stops at the first error. Use `--validate-all` to collect all errors:

~~~bash
scafctl run solution myapp --validate-all
~~~

**Behavior in validate-all mode:**

- Execution continues even when resolvers fail
- Resolvers that depend on failed resolvers are skipped (marked as dependency-skipped)
- All errors are collected into an `AggregatedExecutionError`
- The final error contains all failure details for comprehensive reporting

This mode is useful for:
- CI/CD pipelines that need to report all validation failures at once
- IDE integrations that show all problems
- Debugging complex resolver graphs

### Skip Validation Mode

> **Status**: ✅ Implemented via `--skip-validation` flag

Skip the validation phase for all resolvers:

~~~bash
scafctl run solution myapp --skip-validation
~~~

This is useful during development when validation rules are being refined.

### Context Cancellation

Resolver execution respects context cancellation for graceful shutdown.

**Cancellation behavior:**

- User interruption (Ctrl+C) or timeout triggers context cancellation
- Cancellation propagates immediately to all running resolvers in the current phase
- Provider implementations must respect context cancellation and return promptly
- Resolvers that have not yet started are never executed
- Partial results from cancelled resolvers are **not** emitted to `_`
- Dependent resolvers in subsequent phases are never executed

**Provider requirements:**

- Providers should check context cancellation at appropriate intervals
- Long-running operations (HTTP requests, file I/O) should pass the context through
- On cancellation, providers should clean up resources and return a cancellation error

**Example of cancellation-aware provider:**

~~~go
func (p *HTTPProvider) Execute(ctx context.Context, inputs map[string]any) (*ProviderOutput, error) {
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }
    
    resp, err := p.client.Do(req)
    if err != nil {
        if ctx.Err() != nil {
            return nil, fmt.Errorf("request cancelled: %w", ctx.Err())
        }
        return nil, err
    }
    // ... rest of implementation
}
~~~

---

## Caching & Performance

### Intra-Execution Caching

Resolver results are automatically cached within a single solution execution:

- Once a resolver emits a value to `_`, it is not re-executed during that execution
- All references to `_.resolverName` return the same cached value
- The cache lifetime is scoped to a single solution run

### Provider-Level Caching

Caching of expensive operations (HTTP calls, file reads, etc.) is a provider concern:

- Providers may implement their own caching strategies
- Cache invalidation occurs when cache keys change
- Resolvers do not declare themselves as "expensive" - the execution model handles all resolvers uniformly

### Example

~~~yaml
spec:
  resolvers:
    api_config:
      resolve:
        with:
          - provider: http
            inputs:
              url: https://api.example.com/config
              # Provider handles caching based on URL and headers

    derived_value:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.api_config.setting  # Uses cached result from api_config
~~~

### Memory Considerations

Resolver values are stored in memory for the duration of solution execution. Consider the following when designing resolvers:

**Size limitations:**

- Avoid emitting very large values (multi-MB) from resolvers
- Large datasets should be processed incrementally rather than loaded entirely into resolver context
- The `sync.Map` storing resolver values is held in memory until solution execution completes

**Value size limits:**

> **Status**: ✅ Implemented via executor options

scafctl supports configurable value size limits:

- **Warn threshold** (`WarnValueSize`): Log a warning when a resolver value exceeds this size
- **Max threshold** (`MaxValueSize`): Fail the resolver when value exceeds this size

These can be configured via app configuration or executor options.

**Best practices for large data:**

~~~yaml
spec:
  resolvers:
    # Bad: Loading large file into resolver
    largeDataset:
      resolve:
        with:
          - provider: filesystem
            inputs:
              operation: read
              path: ./data/large-file.json  # 50MB file

    # Good: Store file reference, not content
    datasetPath:
      resolve:
        with:
          - provider: static
            inputs:
              value: ./data/large-file.json

    # Actions can then stream/process the file incrementally
~~~

**Memory optimization tips:**

- Use file paths or URLs as resolver values instead of loading full content
- Filter or transform large datasets to extract only needed fields in resolvers
- Consider whether data truly needs to be shared (via resolvers)

**Concurrent access:**

- The `sync.Map` used for resolver context is optimized for concurrent reads
- Multiple resolvers reading from `_` simultaneously do not create contention
- Memory is not duplicated when multiple resolvers reference the same value

---

## Minimal Resolver Execution

Resolvers run only when required.

~~~bash
scafctl run solution myapp --action deploy
~~~

Force all resolvers with:

~~~bash
scafctl run solution myapp --resolve-all
~~~

---

## Best Practices

### Resolver Granularity

**Keep resolvers focused and single-purpose:**

~~~yaml
spec:
  resolvers:
    # Good: Separate concerns
    apiHost:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: host

    apiPort:
      type: int
      resolve:
        with:
          - provider: parameter
            inputs:
              key: port

    apiEndpoint:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "https://" + _.apiHost + ":" + string(_.apiPort)
~~~

~~~yaml
spec:
  resolvers:
    # Avoid: One massive resolver doing too much
    apiConfig:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: config  # Contains host, port, protocol, auth, etc.
      transform:
        with:
          - provider: cel
            inputs:
              expression: |
                {
                  "endpoint": __self.protocol + "://" + __self.host + ":" + string(__self.port),
                  "auth": __self.auth_type + ":" + __self.auth_token,
                  "timeout": __self.timeout_ms,
                  # Too much logic in one place
                }
~~~

**When to split resolvers:**

- When different parts have different fallback strategies
- When different parts have different validation rules
- When intermediate values are useful to other resolvers
- When type coercion is needed for specific fields

**When to combine resolvers:**

- When the combined value is always used together
- When splitting creates unnecessary complexity
- When the data source naturally provides the complete structure

### Dependency Management

**Minimize dependency chains:**

~~~yaml
spec:
  resolvers:
    # Good: Flat dependency structure
    region:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: region

    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env

    clusterName:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.environment + "-" + _.region + "-cluster"
~~~

~~~yaml
spec:
  resolvers:
    # Avoid: Unnecessary chaining
    region:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: region

    regionPrefix:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.region + "-"

    clusterName:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: _.regionPrefix + "cluster"  # Unnecessary intermediate step
~~~

**Leverage concurrent execution:**

- Design resolvers to execute in parallel when possible
- Avoid creating artificial dependencies between independent resolvers
- Group related resolvers that depend on the same inputs

### Transform vs Validate

**Use transform for data shaping:**

~~~yaml
spec:
  resolvers:
    userName:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: user
      transform:
        with:
          - provider: cel
            inputs:
              expression: __self.trim().toLowerCase()
      validate:
        with:
          - provider: validation
            inputs:
              match: "^[a-z0-9-]+$"
            message: "Username must be lowercase alphanumeric with hyphens"
~~~

**Use validate for constraints:**

~~~yaml
spec:
  resolvers:
    port:
      type: int
      resolve:
        from:
          - provider: parameter
            inputs:
              key: port
      validate:
        with:
          - provider: validation
            inputs:
              expression: "__self >= 1 && __self <= 65535"
            message: "Port must be between 1 and 65535"
~~~

**Guidelines:**

- **Transform**: Normalizing, formatting, deriving, cleaning data
- **Validate**: Enforcing business rules, checking constraints, ensuring correctness
- Avoid using transform for validation (e.g., don't throw errors in CEL expressions)
- Avoid using validate for data transformation

### Conditional Resolvers

**Use `when:` for feature flags and optional configuration:**

~~~yaml
spec:
  resolvers:
    enableCaching:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: cache
          - provider: static
            inputs:
              value: false

    cacheConfig:
      when:
        expr: _.enableCaching == true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: cacheConfig

    dependent:
      resolve:
        with:
          - provider: cel
            inputs:
              # Always check with has()
              expression: has(_.cacheConfig) ? _.cacheConfig.ttl : 0
~~~

**Guidelines:**

- Always use `has()` when referencing conditional resolvers
- Document which resolvers are conditionally executed
- Consider providing safe defaults when conditional resolvers are absent

### Type Declarations

**Declare types for CLI parameters and external inputs:**

~~~yaml
spec:
  resolvers:
    # Good: Declare types for user input
    replicas:
      type: int
      resolve:
        with:
          - provider: parameter
            inputs:
              key: replicas

    enableDebug:
      type: bool
      resolve:
        with:
          - provider: parameter
            inputs:
              key: debug
~~~

**Use `any` for complex or dynamic structures:**

~~~yaml
spec:
  resolvers:
    # Good: Use any for complex JSON
    config:
      type: any
      resolve:
        with:
          - provider: parameter
            inputs:
              key: config
~~~

**Guidelines:**

- Always declare types for scalar CLI parameters
- Use `any` for maps/objects with dynamic keys
- Let type coercion handle string-to-type conversion
- Validate type constraints in the `validate` phase, not through type declarations

### Error Handling

**Design for graceful failure with fallbacks:**

~~~yaml
spec:
  resolvers:
    apiUrl:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: url
          - provider: env
            inputs:
              key: API_URL
          - provider: static
            inputs:
              value: "https://api.example.com"
~~~

**Document failure modes:**

~~~yaml
spec:
  resolvers:
    externalData:
      description: "Fetches configuration from external API. Falls back to local cache if API is unavailable."
      resolve:
        with:
          - provider: http
            inputs:
              url: "https://config-api.example.com/settings"
          - provider: filesystem
            inputs:
              operation: read
              path: "./cache/settings.json"
~~~

**Guidelines:**

- Provide sensible defaults using static providers
- Order sources from most specific to most general
- Use `until:` to short-circuit on successful retrieval
- Document expected failure scenarios in resolver descriptions

### Naming

**Use descriptive, consistent names:**

~~~yaml
spec:
  resolvers:
    # Good: Clear and descriptive
    databaseConnectionString:
      resolve:
        with: []

    kubernetesNamespace:
      resolve:
        with: []

    # Avoid: Ambiguous or too short
    db:
      resolve:
        with: []

    ns:
      resolve:
        with: []
~~~

**Establish naming conventions:**

- Use camelCase consistently (recommended)
- Group related resolvers with common prefixes (e.g., `api*`, `database*`)
- Use full words instead of abbreviations
- Make boolean resolver names clearly boolean (e.g., `enableFeatureX`, `isProduction`)

### Performance

**Avoid unnecessary dependencies:**

~~~yaml
spec:
  resolvers:
    # Good: No dependency on unrelated resolvers
    serviceA:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: serviceA

    serviceB:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: serviceB
    # serviceA and serviceB execute concurrently
~~~

~~~yaml
spec:
  resolvers:
    # Avoid: Artificial dependency
    serviceA:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: serviceA

    serviceB:
      resolve:
        with:
          - provider: cel
            inputs:
              # Unnecessary reference to serviceA
              expression: _.serviceA != null ? "serviceB-value" : "serviceB-value"
    # serviceB must wait for serviceA even though it doesn't need it
~~~

**Leverage provider-level caching:**

- Expensive operations (HTTP calls, file reads) are cached by providers
- Avoid duplicating expensive provider calls in multiple resolvers
- Extract shared expensive operations to a single resolver and reference it

---

## Resolver Metadata

Resolvers support metadata fields for documentation and operational purposes:

### Supported Metadata Fields

- **`description`**: Human-readable explanation of the resolver's purpose
- **`displayName`**: Friendly name for UI and logging (defaults to resolver key)
- **`sensitive`**: Boolean flag indicating the value should be redacted in table/interactive output (human-facing). JSON and YAML output reveals sensitive values for machine consumption, following the Terraform model. Use `--show-sensitive` to reveal values in all output formats
- **`example`**: Example value for documentation and testing

### Example

~~~yaml
spec:
  resolvers:
    api_token:
      description: API authentication token for external service
      displayName: API Token
      sensitive: true
      example: "sk_test_1234567890abcdef"
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: token
          - provider: env
            inputs:
              key: API_TOKEN
~~~

---

## Resolver Example

~~~yaml
spec:
  resolvers:
    environment:
      description: Deployment environment
      displayName: Environment
      example: dev
      type: string

      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
          - provider: static
            inputs:
              value: dev

      transform:
        with:
          - provider: cel
            inputs:
              expression: __self.toLowerCase()

      validate:
        with:
          - provider: validation
            inputs:
              expression: "__self in [\"dev\", \"staging\", \"prod\"]"
            message: "Invalid environment"
~~~

---

## Observability & Metrics

> **Status**: ✅ Implemented in `pkg/resolver/metrics.go`

Resolver execution is instrumented for observability via Prometheus metrics:

**`scafctl_resolver_execution_duration_seconds`** (Histogram)
- Labels: `resolver_name`, `status` (success/failed/skipped)
- Tracks total resolver execution time

**`scafctl_resolver_phase_duration_seconds`** (Histogram)
- Labels: `resolver_name`, `phase` (resolve/transform/validate)
- Tracks per-phase execution time

**`scafctl_resolver_executions_total`** (Counter)
- Labels: `resolver_name`, `status`
- Tracks total resolver execution count

**`scafctl_resolver_provider_calls_total`** (Counter)
- Labels: `resolver_name`, `provider`, `phase`
- Tracks provider invocations per resolver

**`scafctl_resolver_value_size_bytes`** (Histogram)
- Labels: `resolver_name`
- Tracks resolver value sizes

**`scafctl_resolver_concurrent_executions`** (Gauge)
- Tracks current number of concurrent resolver executions

Metrics are registered via `RegisterResolverMetrics()` and recorded automatically during execution.

### OpenTelemetry Traces

> **Status**: ✅ Implemented in `pkg/resolver/executor.go`

Resolver execution is also instrumented with OpenTelemetry spans for distributed tracing. Two spans are emitted per resolver invocation:

#### `resolver.Execute` span

Wraps the entire lifecycle of a single named resolver (resolve + transform + validate).

| Property | Value |
|---|---|
| Tracer name | `github.com/oakwood-commons/scafctl/resolver` |
| Span kind | `SpanKindInternal` |

| Attribute | Type | Description |
|---|---|---|
| `resolver.count` | int | Total number of resolvers in the current execution batch |

#### `resolver.executeResolver` span

Child span scoped to one resolver's execution inside `executeResolver()`.

| Attribute | Type | Description |
|---|---|---|
| `resolver.name` | string | Name of the resolver being executed |
| `resolver.phase` | string | Current phase: `resolve`, `transform`, or `validate` |
| `resolver.sensitive` | bool | Whether the resolver is marked sensitive |

#### Error Recording

If a resolver fails, the error is recorded on the innermost active span and the span status is set to `codes.Error` before the span ends. The parent `resolver.Execute` span propagates the error status upward.

#### Trace Hierarchy Example

```
solution.Get                         ← pkg/solution
  └─ resolver.Execute                ← full batch
        └─ resolver.executeResolver  ← one resolver
              └─ provider.Execute    ← one provider call
                    └─ HTTP GET ...  ← otelhttp transport
```

---

## Design Constraints

- Resolve uses providers to select values
- Transform uses providers to derive values
- Validate uses boolean-emitting providers
- Resolver values are fed into providers via typed input binding
- All resolver phases are provider-backed
- Resolvers must remain pure and deterministic

---

## Summary

Resolvers define how data is sourced, derived, checked, and shared in scafctl. Resolve chooses data, transform shapes data, validate enforces correctness, and emit publishes results. Resolver parameters feed the `parameter` provider and coexist with Cobra by keeping Cobra responsible only for raw flag collection while scafctl core owns parsing and typing.
