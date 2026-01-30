# CEL Integration Architecture

## Overview

This document describes the architecture and best practices for integrating Common Expression Language (CEL) into scafctl. The design emphasizes performance through intelligent caching, thread safety through isolated execution contexts, and correctness through dependency-based execution ordering.

---

## Architecture Principles

### Single Global CEL Environment

- **One CEL environment per application instance**
- Created at application startup
- Contains all built-in and custom extension functions
- **Never recreated during runtime**
- Shared across all requests and commands
- Enables effective AST caching

**Why?**
- CEL environment creation is expensive
- Extension functions are static and never change
- AST cache must be shared across executions for maximum benefit
- Immutable environment is thread-safe by design

### Per-Request Execution Context

- **Each API request or CLI command gets its own isolated execution context**
- Execution context contains a `sync.Map` for storing resolver results
- Lifecycle: created at request/command start, discarded after completion
- No shared state between concurrent executions
- Thread-safe by design using `sync.Map`

**Why?**
- Prevents data leakage between concurrent requests
- Simplifies reasoning about resolver state
- Automatic cleanup when execution completes
- Safe concurrent resolver execution within a phase

---

## Component Responsibilities

### CEL Environment (Global, Singleton)

**Created:** Application startup  
**Lifecycle:** Lives for entire application runtime  
**Contains:**
- All built-in CEL functions
- All custom extension functions (from `pkg/celexp/ext/`)
- AST cache (shared across all executions)
- Variable type declarations (updated per evaluation)

**Thread Safety:** Immutable after creation, inherently thread-safe

### Execution Context (Per-Request/Command)

**Created:** Start of API request or CLI command execution  
**Lifecycle:** Lives only for the duration of one request/command  
**Contains:**
- `sync.Map` storing resolver results
- Maps resolver name → resolved value
- Only successful resolver results (failed resolvers do not store values)

**Thread Safety:** Uses `sync.Map` for concurrent read/write operations

### AST Cache

**Location:** Part of the global CEL environment  
**Scope:** Shared across all requests and commands  
**Cache Key:** Expression string + variable declarations  
**Benefits:**
- Each unique CEL expression is compiled once
- Subsequent evaluations reuse the compiled AST
- Dramatic performance improvement for repeated expressions
- See `pkg/celexp/README.md` for benchmarks

**Usage Pattern:**
```go
// First execution: cache miss, compiles and caches
// Subsequent executions: cache hit, reuses compiled AST
expr := celexp.Expression("_.port + 1000")
compiled, err := expr.Compile(envOptions, celexp.WithCache(cache))
```

---

## Variable Lifecycle

### The `_` Variable (Resolver Context)

**Type:** `map[string]any`  
**Purpose:** Provides access to resolver results  
**Lifecycle:** Updated after each resolver phase completes

#### Phase-by-Phase Lifecycle

**Phase 0 (Initial State):**
- `_` is empty or undefined
- No resolvers have executed yet
- First-phase resolvers cannot reference `_`

**After Phase 1 Completes:**
- `_` is updated with all successful resolver results from Phase 1
- Failed resolvers do not store values in `_`
- CEL environment's `_` variable is recreated/updated before Phase 2 begins

**After Phase N Completes:**
- `_` is updated with cumulative results from Phase 1 through Phase N
- CEL environment's `_` variable is recreated/updated before Phase N+1 begins
- All resolvers in Phase N+1 see consistent `_` state

**Example:**
```yaml
# Phase 1: No dependencies
resolvers:
  basePort:
    resolve:
      with:
        - provider: static
          inputs:
            value: 8000

# Phase 2: Depends on Phase 1
  apiPort:
    resolve:
      with:
        - provider: cel
          inputs:
            expression: "_.basePort + 80"  # _.basePort available after Phase 1
```

#### Storage Details

- Stored in `sync.Map` in execution context
- Exposed to CEL as `map[string]any`
- Keys: resolver names (e.g., `"basePort"`, `"apiEndpoint"`)
- Values: resolver output (any type)
- Only successful resolver results are stored

### The `__self` Variable (Context-Specific)

**Purpose:** Refers to the current value being processed  
**Contexts:**
- **Resolve phase:** Value from previous source (available in `until:` conditions)
- **Transform phase:** Value from previous transform step
- **Validate phase:** Final transformed value being validated

**Example:**
```yaml
resolvers:
  port:
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
```

### The `__item` Variable (Context-Specific)

**Purpose:** Refers to the current item in iteration contexts  
**Contexts:** Set during array operations, loops, or collection processing  
**Lifecycle:** Scoped to the specific iteration operation

---

## Dependency Analysis and DAG Execution

### Dependency Collection

Dependencies between resolvers are automatically detected by analyzing CEL expressions:

1. **Static Analysis:** Use `pkg/celexp/refs.go` to extract variable references from CEL AST
2. **Dependency Graph:** Build graph where edges represent "depends on" relationships
3. **Phase Assignment:** Group resolvers into phases using topological sort
4. **Validation:** Detect circular dependencies and report errors

**Example:**
```yaml
resolvers:
  a:  # Phase 1
    resolve:
      with:
        - provider: static
          inputs:
            value: 10
  
  b:  # Phase 2 (depends on 'a')
    resolve:
      with:
        - provider: cel
          inputs:
            expression: "_.a + 5"  # References _.a
  
  c:  # Phase 2 (depends on 'a', concurrent with 'b')
    resolve:
      with:
        - provider: cel
          inputs:
            expression: "_.a * 2"  # References _.a
  
  d:  # Phase 3 (depends on 'b' and 'c')
    resolve:
      with:
        - provider: cel
          inputs:
            expression: "_.b + _.c"  # References _.b and _.c
```

**Resulting DAG:**
```
Phase 1:  [a]
Phase 2:  [b, c]  ← Execute concurrently
Phase 3:  [d]
```

### Phase Execution Model

**Within a Phase:**
- All resolvers execute concurrently (goroutines)
- Order within phase is non-deterministic
- Each resolver writes to `sync.Map` immediately after emit
- No dependencies between resolvers in same phase (guaranteed by DAG)

**Between Phases:**
- Phase N must complete entirely before Phase N+1 begins
- `_` variable is updated with Phase N results before Phase N+1 starts
- Sequential execution ensures dependency correctness

**Thread Safety:**
- `sync.Map` handles concurrent writes within a phase
- CEL environment is read-only during execution (thread-safe)
- No locking required for resolver execution

---

## Error Handling

### Phase-Level Error Collection

When a resolver fails during execution:

1. **Continue Phase:** Allow all other resolvers in the current phase to complete
2. **Collect Errors:** Aggregate errors from all failed resolvers in the phase
3. **Exclude Failed Values:** Failed resolvers do NOT store their results in `_`
4. **Return Errors:** Return all collected errors together
5. **Halt Execution:** Do not proceed to subsequent phases

**Rationale:**
- Maximizes useful error information (multiple failures reported together)
- Prevents cascading failures
- Keeps `_` clean (only successful results)
- Respects phase boundaries (don't start dependent work with incomplete data)

**Example Scenario:**
```yaml
# Phase 2: Three resolvers executing concurrently
resolvers:
  config:      # ✅ Succeeds
    resolve: ...
  
  database:    # ❌ Fails (invalid connection string)
    resolve: ...
  
  credentials: # ❌ Fails (missing secret)
    resolve: ...
```

**Execution Flow:**
1. All three resolvers start concurrently
2. `config` succeeds, writes to `_`
3. `database` fails, error collected
4. `credentials` fails, error collected
5. Phase 2 completes
6. Return both errors to user
7. `_` contains only `config` (successful resolver)
8. Phase 3 never executes

### Error Reporting

Errors should include:
- Resolver name
- Phase number
- Error message
- Context (which CEL expression failed, etc.)

---

## Performance Considerations

### AST Caching Best Practices

**Do:**
- ✅ Reuse the same CEL environment across all executions
- ✅ Enable AST caching on the global environment
- ✅ Use consistent expression strings (cache key includes expression)
- ✅ Declare variables consistently (cache key includes variable types)

**Don't:**
- ❌ Create new CEL environments per request
- ❌ Disable caching for production workloads
- ❌ Dynamically generate expressions with unique strings (defeats caching)

### Execution Context Optimization

**Do:**
- ✅ Create execution context once per request/command
- ✅ Discard context after completion (automatic garbage collection)
- ✅ Use `sync.Map` for concurrent resolver writes
- ✅ Execute independent resolvers concurrently within phases

**Don't:**
- ❌ Share execution contexts between requests
- ❌ Manually lock/unlock around `sync.Map` operations (it's already thread-safe)
- ❌ Serialize resolver execution when parallel execution is safe

### Variable Updates

**Strategy:** Recreate `_` variable after each phase
- CEL requires type-safe variable declarations
- Updating `_` ensures correct types for newly added resolver results
- Minimal overhead compared to resolver execution time

---

## Implementation Checklist

### Application Initialization

- [x] Create global CEL environment with all extensions
  - **Status**: ✅ Completed in `pkg/celexp/env/global.go`
  - Uses `sync.Once` for singleton pattern
  - Loads all extensions from `pkg/celexp/ext/`
- [x] Initialize AST cache for the environment
  - **Status**: ✅ Completed in `pkg/celexp/env/global.go`
  - 10,000 entry cache via `celexp.NewProgramCache(10000)`
- [x] Configure cache size limits (see `pkg/celexp/README.md`)
  - **Status**: ✅ Completed - set to 10,000 entries
  - Constant: `GlobalCacheSize = 10000`
- [x] Store environment in application context or singleton
  - **Status**: ✅ Completed - singleton via `env.Global(ctx)` and `env.GlobalCache()`
  - Eager initialization in `cmd/scafctl/scafctl.go` at startup

### Request/Command Handling

- [ ] Create new `sync.Map` for execution context
- [ ] Initialize `_` variable (empty initially)
- [ ] Analyze resolver dependencies to build DAG
- [ ] Execute resolvers phase by phase
- [ ] Update `_` variable after each phase completes
- [ ] Collect and return errors if any resolver fails
- [ ] Discard execution context after completion

### Resolver Execution

- [ ] Compile CEL expressions using global environment + AST cache
- [ ] Inject `_` variable with current resolver context
- [ ] Inject `__self` or `__item` variables when applicable
- [ ] Execute resolvers in current phase concurrently
- [ ] Write successful results to `sync.Map` immediately after emit
- [ ] Collect errors for failed resolvers
- [ ] Wait for all resolvers in phase to complete before proceeding

### Type Safety

- [ ] Validate variable types before CEL evaluation (use `ValidateVars()`)
- [ ] Ensure `_` is always `map[string]any` type
- [ ] Use `int64` for integers, `float64` for floats (CEL types)
- [ ] See `pkg/celexp/README.md` Type Safety section

---

## Best Practices

### 1. Environment Reuse

**Always reuse the global CEL environment:**
```go
// Good: Create once at startup
var globalCELEnv *cel.Env
var globalASTCache *celexp.Cache

func init() {
    globalCELEnv, _ = cel.NewEnv(
        cel.Lib(customExtensions),
    )
    globalASTCache = celexp.NewCache(10000)
}

// Use in request handler
func handleRequest() {
    expr := celexp.Expression("_.port + 1000")
    compiled, _ := expr.Compile(
        []cel.EnvOption{cel.Variable("_", cel.MapType(cel.StringType, cel.AnyType))},
        celexp.WithCache(globalASTCache),
    )
}
```

### 2. Execution Isolation

**Always create isolated execution context per request:**
```go
// Good: One sync.Map per request
func handleRequest(w http.ResponseWriter, r *http.Request) {
    resolverResults := &sync.Map{}
    ctx := context.WithValue(r.Context(), resolverContextKey, resolverResults)
    
    // Execute resolvers with isolated context
    executeResolvers(ctx)
}

// Bad: Sharing sync.Map across requests
var globalResults sync.Map  // ❌ Don't do this
```

### 3. Error Handling

**Collect all phase errors before halting:**
```go
func executePhase(resolvers []Resolver) error {
    var wg sync.WaitGroup
    errChan := make(chan error, len(resolvers))
    
    for _, resolver := range resolvers {
        wg.Add(1)
        go func(r Resolver) {
            defer wg.Done()
            if err := r.Execute(); err != nil {
                errChan <- err
            }
        }(resolver)
    }
    
    wg.Wait()
    close(errChan)
    
    // Collect all errors
    var errors []error
    for err := range errChan {
        errors = append(errors, err)
    }
    
    if len(errors) > 0 {
        return combineErrors(errors)
    }
    return nil
}
```

### 4. Variable Updates

**Update `_` after each phase:**
```go
func executePhasedResolvers(phases [][]Resolver, resolverResults *sync.Map) error {
    for i, phase := range phases {
        // Execute all resolvers in current phase
        if err := executePhase(phase); err != nil {
            return fmt.Errorf("phase %d failed: %w", i+1, err)
        }
        
        // Update _ variable with current results
        celVars := syncMapToMap(resolverResults)
        updateCELVariable("_", celVars)
        
        // Proceed to next phase
    }
    return nil
}
```

### 5. Dependency Analysis

**Use AST analysis for accurate dependency detection:**
```go
import "github.com/oakwood-commons/scafctl/pkg/celexp/refs"

func extractDependencies(expression string) ([]string, error) {
    // Parse expression to AST
    expr := celexp.Expression(expression)
    compiled, err := expr.Compile(envOptions)
    if err != nil {
        return nil, err
    }
    
    // Extract variable references using AST
    deps := refs.ExtractVariableReferences(compiled.AST)
    
    // Filter for resolver references (starts with "_.")
    var resolverDeps []string
    for _, dep := range deps {
        if strings.HasPrefix(dep, "_.") {
            resolverName := strings.TrimPrefix(dep, "_.")
            resolverDeps = append(resolverDeps, resolverName)
        }
    }
    
    return resolverDeps, nil
}
```

### 6. Thread Safety

**Leverage `sync.Map` correctly:**
```go
// Good: Direct use, no additional locking needed
resolverResults.Store("port", int64(8080))
value, ok := resolverResults.Load("port")

// Good: Concurrent writes within a phase
var wg sync.WaitGroup
for _, resolver := range phase {
    wg.Add(1)
    go func(r Resolver) {
        defer wg.Done()
        result, _ := r.Execute()
        resolverResults.Store(r.Name(), result)  // Thread-safe
    }(resolver)
}
wg.Wait()
```

---

## Reference Materials

- **CEL Package Documentation:** [`pkg/celexp/README.md`](../../pkg/celexp/README.md)
- **Resolver Design:** [`docs/design/resolvers.md`](./resolvers.md)
- **CEL Specification:** [github.com/google/cel-spec](https://github.com/google/cel-spec)
- **CEL Go Implementation:** [github.com/google/cel-go](https://github.com/google/cel-go)

---

## Summary

**Global CEL Environment:**
- One environment per application instance
- Created at startup, never recreated
- Contains all extension functions
- Shared AST cache for maximum performance

**Per-Request Execution Context:**
- One `sync.Map` per API request or CLI command
- Isolated, no shared state between executions
- Stores only successful resolver results
- Automatically discarded after completion

**Variable Lifecycle:**
- `_` updated after each resolver phase completes
- Contains cumulative results from all completed phases
- Failed resolvers do not store values in `_`
- `__self` and `__item` injected in specific contexts

**Execution Model:**
- Dependencies extracted from CEL expressions using AST analysis
- DAG determines phase assignment
- Resolvers within a phase execute concurrently
- Phases execute sequentially
- Errors collected per phase, halt execution before next phase

This architecture ensures correctness, performance, and thread safety for CEL integration in scafctl.
