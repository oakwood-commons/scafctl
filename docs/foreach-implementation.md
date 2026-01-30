# ForEach Implementation Plan for Resolver Transform Phase

> **Status: ✅ IMPLEMENTED**
> 
> This feature has been implemented. See [pkg/resolver/executor.go](../pkg/resolver/executor.go) for the core implementation and [pkg/resolver/foreach_test.go](../pkg/resolver/foreach_test.go) for tests.

This document outlines the implementation plan for adding `forEach` support to resolver transform steps, enabling iteration over arrays with parallel provider execution.

---

## Executive Summary

Add a `forEach` clause to transform steps that allows mapping provider operations over arrays. This preserves the resolver model (one resolver = one value) while enabling powerful iteration patterns within the transform phase.

---

## Design Decisions

| Topic | Decision |
|-------|----------|
| Location | Transform phase only (not resolve or validate) |
| Resolver model | Preserved - resolver emits one value (an array) |
| Dependency graph | Unchanged - still one node per resolver |
| Concurrency | Default unlimited, optional `concurrency` limit |
| Order preservation | Yes - output array maintains input order |
| Empty array input | Returns empty array `[]` |
| Non-array input | Error with clear message |
| Variable naming | Alias mechanism - creates both `__item`/`__index` AND custom names |
| Variable scope | Available in `inputs`, `when`, and all nested templates/expressions |
| `in` field | Optional, defaults to `__self` |
| Error handling | `onError: continue` includes error metadata in output array |
| Filter support | Deferred - future enhancement |

---

## Syntax

### Basic Usage

```yaml
spec:
  resolvers:
    regionConfigs:
      type: array
      resolve:
        with:
          - provider: static
            inputs:
              value: ["us-east1", "us-west1", "eu-west1"]
      transform:
        with:
          - forEach:
              item: region     # Creates both __item AND region
            provider: http
            inputs:
              url:
                tmpl: "https://api.example.com/config/{{ .region }}"
```

**Result:** `_.regionConfigs` = `[{config1}, {config2}, {config3}]`

### With Index

```yaml
transform:
  with:
    - forEach:
        item: region
        index: i           # Creates both __index AND i
      provider: cel
      inputs:
        expression: "{'name': region, 'position': i + 1}"
```

### With Custom Source

```yaml
transform:
  with:
    - forEach:
        item: endpoint
        in:
          rslvr: apiEndpoints   # Iterate over different resolver's value
      provider: http
      inputs:
        url:
          rslvr: endpoint
```

### With Concurrency Limit

```yaml
transform:
  with:
    - forEach:
        item: url
        concurrency: 5    # Max 5 parallel HTTP requests
      provider: http
      inputs:
        url:
          rslvr: url
```

### With Conditional Execution

```yaml
transform:
  with:
    - forEach:
        item: region
      when:
        expr: region.enabled == true   # Skip disabled regions
      provider: http
      inputs:
        url:
          tmpl: "https://{{ .region.endpoint }}/api"
```

### With Error Handling

```yaml
transform:
  with:
    - forEach:
        item: url
      provider: http
      inputs:
        url:
          rslvr: url
      onError: continue    # Continue on failure, include error in output
```

**Output with `onError: continue`:**
```json
[
  {"data": {"status": "ok"}},
  {"error": "connection refused", "index": 1, "item": "https://bad.url"},
  {"data": {"status": "ok"}}
]
```

---

## Variables Available During Iteration

| Variable | Description |
|----------|-------------|
| `__item` | Current array element (always available) |
| `__index` | Current 0-based index (always available) |
| `{custom_item}` | Alias for `__item` when `item` field specified |
| `{custom_index}` | Alias for `__index` when `index` field specified |
| `__self` | Original array being iterated (NOT current item) |
| `_` | Resolver context as usual |

---

## Type Definitions

### ForEachClause

**File:** `pkg/resolver/resolver.go`

```go
// ForEachClause defines iteration over an array in a transform step.
// When present, the provider is executed once per array element and results
// are collected into an output array preserving order.
type ForEachClause struct {
    // Item is the variable name alias for the current element.
    // Creates both __item (always) and this custom name.
    // Optional - if not specified, only __item is available.
    Item string `json:"item,omitempty" yaml:"item,omitempty" doc:"Variable name alias for current array element" maxLength:"50" pattern:"^[a-zA-Z_][a-zA-Z0-9_]*$" patternDescription:"Must be a valid identifier" example:"region"`

    // Index is the variable name alias for the current 0-based index.
    // Creates both __index (always) and this custom name.
    // Optional - if not specified, only __index is available.
    Index string `json:"index,omitempty" yaml:"index,omitempty" doc:"Variable name alias for current index" maxLength:"50" pattern:"^[a-zA-Z_][a-zA-Z0-9_]*$" patternDescription:"Must be a valid identifier" example:"i"`

    // In specifies the array to iterate over.
    // Optional - defaults to __self (current transform value).
    In *ValueRef `json:"in,omitempty" yaml:"in,omitempty" doc:"Array to iterate over (default: __self)"`

    // Concurrency limits parallel execution.
    // 0 (default) means unlimited parallelism.
    Concurrency int `json:"concurrency,omitempty" yaml:"concurrency,omitempty" doc:"Maximum parallel iterations (0=unlimited)" minimum:"0" example:"5"`
}
```

### Updated ProviderTransform

**File:** `pkg/resolver/resolver.go`

```go
// ProviderTransform represents a single transform step
type ProviderTransform struct {
    Provider string               `json:"provider" yaml:"provider" doc:"Provider name" example:"cel" maxLength:"100" pattern:"^[a-zA-Z][a-zA-Z0-9_-]*$" patternDescription:"Must start with a letter, followed by letters, numbers, underscores, or hyphens"`
    Inputs   map[string]*ValueRef `json:"inputs" yaml:"inputs" doc:"Provider inputs"`
    When     *Condition           `json:"when,omitempty" yaml:"when,omitempty" doc:"Step-level condition"`
    OnError  ErrorBehavior        `json:"onError,omitempty" yaml:"onError,omitempty" doc:"Behavior when provider fails (continue, fail)" example:"fail" default:"fail"`
    ForEach  *ForEachClause       `json:"forEach,omitempty" yaml:"forEach,omitempty" doc:"Iterate over array, executing provider for each element"`
}
```

### ForEachIterationError

**File:** `pkg/resolver/errors.go`

```go
// ForEachIterationError represents a failed iteration within a forEach transform.
// Used when onError: continue allows partial results.
type ForEachIterationError struct {
    Index int    `json:"index" yaml:"index" doc:"Index of failed iteration" minimum:"0" example:"1"`
    Item  any    `json:"item" yaml:"item" doc:"The item that caused the failure"`
    Error string `json:"error" yaml:"error" doc:"Error message" maxLength:"1000" example:"connection refused"`
}

// ForEachTypeError represents an error when forEach input is not an array.
type ForEachTypeError struct {
    ResolverName string `json:"resolverName" yaml:"resolverName" doc:"Name of the resolver" example:"regionConfigs"`
    Step         int    `json:"step" yaml:"step" doc:"Transform step number (0-indexed)" minimum:"0" example:"0"`
    ActualType   string `json:"actualType" yaml:"actualType" doc:"Actual type received" example:"string"`
}

func (e *ForEachTypeError) Error() string {
    return fmt.Sprintf("resolver %q transform step %d: forEach requires array input, got %s",
        e.ResolverName, e.Step, e.ActualType)
}
```

---

## Implementation Tasks

### Phase 1: Type Definitions & Validation

| Task | File | Description |
|------|------|-------------|
| 1.1 | `pkg/resolver/resolver.go` | Add `ForEachClause` struct |
| 1.2 | `pkg/resolver/resolver.go` | Add `ForEach` field to `ProviderTransform` |
| 1.3 | `pkg/resolver/errors.go` | Add `ForEachIterationError` and `ForEachTypeError` |
| 1.4 | `pkg/resolver/resolver_test.go` | Add YAML unmarshalling tests for forEach |
| 1.5 | `pkg/resolver/validation.go` | Add validation for forEach clause (variable names, concurrency >= 0) |

### Phase 2: Executor Implementation

| Task | File | Description |
|------|------|-------------|
| 2.1 | `pkg/resolver/executor.go` | Add `executeForEachTransform()` method |
| 2.2 | `pkg/resolver/executor.go` | Modify `executeTransformPhase()` to detect and route forEach steps |
| 2.3 | `pkg/resolver/executor.go` | Implement parallel execution with semaphore for concurrency limit |
| 2.4 | `pkg/resolver/executor.go` | Implement order-preserving result collection |
| 2.5 | `pkg/resolver/executor.go` | Implement error handling with `onError: continue` support |
| 2.6 | `pkg/resolver/executor.go` | Add variable injection (`__item`, `__index`, custom aliases) |

### Phase 3: ValueRef Updates

| Task | File | Description |
|------|------|-------------|
| 3.1 | `pkg/resolver/valueref.go` | Update `Resolve()` to accept forEach variables |
| 3.2 | `pkg/celexp/context.go` | Ensure CEL evaluation supports forEach variables |
| 3.3 | `pkg/gotmpl/gotmpl.go` | Ensure template execution supports forEach variables |

### Phase 4: Testing

| Task | File | Description |
|------|------|-------------|
| 4.1 | `pkg/resolver/executor_test.go` | Test basic forEach iteration |
| 4.2 | `pkg/resolver/executor_test.go` | Test with custom item/index names |
| 4.3 | `pkg/resolver/executor_test.go` | Test with custom `in` source |
| 4.4 | `pkg/resolver/executor_test.go` | Test concurrency limiting |
| 4.5 | `pkg/resolver/executor_test.go` | Test order preservation under parallel execution |
| 4.6 | `pkg/resolver/executor_test.go` | Test empty array input |
| 4.7 | `pkg/resolver/executor_test.go` | Test non-array input error |
| 4.8 | `pkg/resolver/executor_test.go` | Test `onError: continue` with partial failures |
| 4.9 | `pkg/resolver/executor_test.go` | Test `when` condition with forEach variables |
| 4.10 | `pkg/resolver/integration_test.go` | End-to-end forEach scenarios |

### Phase 5: Documentation

| Task | File | Description |
|------|------|-------------|
| 5.1 | `docs/design/resolvers.md` | Add forEach section to transform documentation |
| 5.2 | `docs/design/resolvers.md` | Add future enhancement note for filter support |
| 5.3 | `docs/design/actions.md` | Add future enhancement note for filter support |
| 5.4 | `examples/` | Create forEach example solution files |

---

## Execution Flow

```
executeTransformPhase()
    │
    ├── for each transform step:
    │       │
    │       ├── if step.ForEach != nil:
    │       │       │
    │       │       ├── Resolve 'in' (default: __self)
    │       │       │
    │       │       ├── Validate input is array
    │       │       │       └── If not array → ForEachTypeError
    │       │       │
    │       │       ├── If empty array → return []
    │       │       │
    │       │       ├── executeForEachTransform():
    │       │       │       │
    │       │       │       ├── Create result slice (preserve order)
    │       │       │       │
    │       │       │       ├── Create semaphore (if concurrency > 0)
    │       │       │       │
    │       │       │       ├── For each item (parallel):
    │       │       │       │       │
    │       │       │       │       ├── Build iteration context:
    │       │       │       │       │       __item = current element
    │       │       │       │       │       __index = current index
    │       │       │       │       │       {custom_item} = current element
    │       │       │       │       │       {custom_index} = current index
    │       │       │       │       │
    │       │       │       │       ├── Evaluate when condition (if present)
    │       │       │       │       │       └── If false → skip, store nil marker
    │       │       │       │       │
    │       │       │       │       ├── Execute provider
    │       │       │       │       │
    │       │       │       │       └── Store result at index position
    │       │       │       │               └── On error + continue: store error object
    │       │       │       │
    │       │       │       └── Return collected results array
    │       │       │
    │       │       └── Set __self = results array for next step
    │       │
    │       └── else: normal transform execution (existing code)
    │
    └── return final __self
```

---

## Example: Complete forEach Transform

```yaml
spec:
  resolvers:
    # Source of regions to process
    regions:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                - name: us-east1
                  endpoint: https://us-east1.api.example.com
                  enabled: true
                - name: us-west1
                  endpoint: https://us-west1.api.example.com
                  enabled: true
                - name: eu-west1
                  endpoint: https://eu-west1.api.example.com
                  enabled: false

    # Fetch config from each enabled region
    regionConfigs:
      type: array
      resolve:
        with:
          - provider: static
            inputs:
              value:
                rslvr: regions
      transform:
        with:
          # First, filter to enabled regions using CEL
          - provider: cel
            inputs:
              expression: __self.filter(r, r.enabled)

          # Then fetch config from each region
          - forEach:
              item: region
              index: i
              concurrency: 3
            provider: http
            inputs:
              url:
                tmpl: "{{ .region.endpoint }}/config"
              headers:
                expr: "{'X-Region': region.name, 'X-Request-Index': string(i)}"
            onError: continue

          # Finally, add metadata to each result
          - forEach:
              item: config
              index: i
            provider: cel
            inputs:
              expression: |
                config.has('error') 
                  ? config 
                  : config.merge({'fetchedAt': now(), 'index': i})
```

---

## Future Enhancements

### Filter Support (Deferred)

Both resolvers and actions may benefit from a `filter` clause to conditionally include/exclude items:

```yaml
# Future syntax (not implemented)
forEach:
  item: region
  filter:
    expr: region.enabled == true
```

For now, users can achieve filtering with a CEL transform step before the forEach:

```yaml
transform:
  with:
    - provider: cel
      inputs:
        expression: __self.filter(r, r.enabled)
    - forEach:
        item: region
      # ...
```

---

## Testing Strategy

### Unit Tests

1. **Basic iteration** - forEach with static array
2. **Variable availability** - `__item`, `__index`, custom aliases accessible in inputs
3. **Custom `in` source** - iterate over different resolver's value
4. **Empty array** - returns `[]`
5. **Non-array error** - clear error message
6. **Concurrency limit** - verify semaphore behavior
7. **Order preservation** - parallel execution maintains input order
8. **Error handling** - `onError: continue` includes error objects
9. **When condition** - skip iterations based on condition
10. **Chained forEach** - multiple forEach steps in sequence

### Integration Tests

1. **HTTP foreach** - fetch from multiple endpoints
2. **Nested data** - forEach over complex objects
3. **Mixed transforms** - forEach combined with regular transforms
4. **Large arrays** - performance with 100+ items
5. **Concurrent failures** - multiple items fail simultaneously

---

## Acceptance Criteria

- [ ] `forEach` clause parses correctly from YAML
- [ ] Type validation rejects non-array inputs with clear error
- [ ] Empty arrays return `[]` without errors
- [ ] Iterations execute in parallel by default
- [ ] `concurrency` limit restricts parallelism correctly
- [ ] Output array preserves input order regardless of execution order
- [ ] `__item` and `__index` available in all expression contexts
- [ ] Custom `item`/`index` names create additional aliases
- [ ] `in` defaults to `__self` when not specified
- [ ] `when` condition can reference forEach variables
- [ ] `onError: continue` includes error metadata in output
- [ ] Documentation updated with forEach examples
- [ ] Example solution files demonstrate forEach usage
