# CEL Global Cache Implementation

## Overview

This document describes the implementation of the global CEL program cache for scafctl, completed as part of the CEL integration design (see [design/cel-integration.md](design/cel-integration.md)).

**Note**: The original design included a global CEL environment singleton, but this was removed after recognizing that CEL environments are immutable and cannot have variables added after creation. Since the CEL provider needs different variables per request, it must create fresh environments each time using `env.New()`.

## Architecture

### Global Program Cache

**Location**: `pkg/celexp/env/global.go`

A thread-safe singleton cache for compiled CEL programs, shared across the application:

- **Function**: `GlobalCache() *celexp.ProgramCache`
- **Initialization**: Uses `sync.Once` to ensure single initialization on first call
- **Size**: 10,000 entries (configurable via `GlobalCacheSize` constant)
- **Thread-Safety**: Uses `sync.Map` and `sync.RWMutex` internally
- **Metrics**: Tracks hits, misses, evictions, and per-expression statistics

### Why No Global Environment Singleton?

CEL environments are **immutable** - you cannot add variables to an existing environment. The CEL provider has different variables on each request (from resolver data), so it must create a fresh environment each time via `env.New()`.

**What IS cached**:
- ✅ **Extension options** - Cached in `env.New()` via `getBaseEnvOptions()` with `sync.Once`
- ✅ **Compiled programs** - Cached in `GlobalCache()` and reused across different variable values

**What is NOT needed**:
- ❌ **Environment singleton** - Must create fresh environments with different variable declarations

### Factory Pattern for Circular Dependency Resolution

**Location**: `pkg/celexp/celexp.go`

To avoid circular dependencies between `celexp` and `env` packages, a factory pattern was implemented:

- **Registration**: `SetEnvFactory(factory func(context.Context, ...cel.EnvOption) (*cel.Env, error))`
- **Usage**: `celexp.Compile()` uses factory if available, falls back to `cel.NewEnv()`
- **Thread-Safety**: Uses `sync.Once` and `sync.RWMutex` for safe concurrent access
- **Initialization**: Factory is registered at app startup in `cmd/scafctl/scafctl.go`

## Application Startup Flow

**Location**: `cmd/scafctl/scafctl.go`

The `run()` function initializes CEL components:

1. **Register Environment Factory**: `celexp.SetEnvFactory(env.New)`
   - Enables celexp package to use environments with all extensions
   - Breaks circular dependency between packages

2. **Continue with CLI Initialization**: `scafctl.Root()`

The global cache is initialized lazily on first call to `env.GlobalCache()`.

## CEL Provider Implementation

**Location**: `pkg/provider/builtin/celprovider/cel.go`

The CEL provider uses manual caching for compiled programs:

### Cache Hit Path
```go
cacheKey := generateCacheKey(expression, envOpts)
if prog, found := cache.Get(cacheKey); found {
    // Evaluate immediately with cached program
    result, _, err := prog.Eval(celVars)
    // ...
}
```

### Cache Miss Path
```go
// Create environment with all extensions
celEnv, err := env.New(ctx, envOpts...)

// Compile expression
ast, issues := celEnv.Compile(expression)

// Create program
prog, err := celEnv.Program(ast)

// Store in global cache
cache.Put(cacheKey, prog, expression)

// Evaluate
result, _, err := prog.Eval(celVars)
```

### Cache Key Generation

Cache keys are generated using SHA256 hashing of:
- Expression string
- Variable declarations (sorted for consistency)
- Environment options

This ensures consistent cache keys for semantically identical expressions.

## Benefits

### Performance
- **Compiled Program Reuse**: Same expressions use cached compiled programs
- **Extension Caching**: `env.New()` caches extension options via `sync.Once`
- **No Redundant Environment Creation**: Fresh environments only when variable declarations change

### Reliability
- **Thread-Safety**: All caching structures use proper synchronization
- **Context Support**: `env.New()` respects timeouts and cancellation

### Maintainability
- **Factory Pattern**: Clean separation between packages without circular dependencies
- **Centralized Configuration**: Global cache size in one place
- **Comprehensive Testing**: Unit tests verify singleton, concurrency, and caching behavior

## Testing

### Global Cache Tests
**Location**: `pkg/celexp/env/global_test.go`

- Singleton initialization and behavior
- Concurrent access (100 goroutines)
- Performance benchmarks

### Cache Utilization Tests
**Location**: `pkg/provider/builtin/celprovider/cache_test.go`

- Cache hit/miss tracking
- Multiple evaluations of same expression
- Different expressions create different cache entries
- Cache statistics verification

### All Tests Passing
```bash
go test ./pkg/celexp/... ./pkg/provider/builtin/celprovider/...
# Result: PASS (all tests)
```

### Linter Clean
```bash
golangci-lint run ./pkg/celexp/... ./pkg/provider/builtin/celprovider/... ./cmd/scafctl/...
# Result: 0 issues
```

## Usage Examples

### Getting Global Cache
```go
import "github.com/oakwood-commons/scafctl/pkg/celexp/env"

cache := env.GlobalCache()
if cache != nil {
    stats := cache.Stats()
    fmt.Printf("Cache: %d hits, %d misses (%.2f%% hit rate)\n",
        stats.Hits, stats.Misses, stats.HitRate)
}
```

### Creating Environment with Variables
```go
// Each request creates fresh environment with specific variables
celEnv, err := env.New(ctx,
    cel.Variable("x", cel.IntType),
    cel.Variable("y", cel.StringType),
)
```

### Why Programs Are Reusable Across Variable Values

The key insight: A compiled CEL program is parameterized by variable **types**, not values.

```go
// First call: compile "x * 2" with x as int
celEnv1, _ := env.New(ctx, cel.Variable("x", cel.IntType))
prog1, _ := celEnv1.Program(ast)  // Compiles program
result1, _ := prog1.Eval(map[string]any{"x": 10})  // Returns 20

// Second call: SAME program works with different value!
result2, _ := prog1.Eval(map[string]any{"x": 50})  // Returns 100

// Third call: different type = different cache entry
celEnv2, _ := env.New(ctx, cel.Variable("x", cel.StringType))
prog2, _ := celEnv2.Program(ast)  // Different program (string type)
```

## Configuration

### Global Cache Size
**Location**: `pkg/celexp/env/global.go`

```go
const GlobalCacheSize = 10000 // Configurable constant
```

To adjust cache size, modify this constant and rebuild.

## Future Enhancements

1. **Dynamic Cache Size**: Configuration via environment variable or config file
2. **Cache Eviction Policy**: LRU is current default, could support other policies
3. **Cache Metrics Export**: Export cache stats to monitoring systems
4. **Distributed Caching**: Support for shared cache in multi-instance deployments

## Related Documentation

- [CEL Integration Design](design/cel-integration.md) - Overall architecture and phases
- [pkg/celexp/README.md](../pkg/celexp/README.md) - CEL expression package documentation
- [pkg/celexp/env/README.md](../pkg/celexp/env/README.md) - Environment creation documentation

## Completion Status

✅ **Phase 1 Complete**: Global CEL Caching Infrastructure

- Global 10K program cache with metrics
- Factory pattern to avoid circular dependencies
- Manual caching in CEL provider for compiled programs
- Extension caching in `env.New()` via `sync.Once`
- Comprehensive test coverage with all tests passing
- Linter clean (0 issues)
- **Removed**: Global environment singleton (not needed due to CEL immutability)

**Key Insight**: CEL environments are immutable, so we create fresh environments per request with `env.New()` but cache the compiled programs which are reusable across different variable values.

**Next Phase**: Resolver Dependency Extraction (see design document)
