# CEL Expression Package

A Go package for compiling and evaluating [Common Expression Language (CEL)](https://github.com/google/cel-spec) expressions with optional caching for improved performance.

## Table of Contents

- [Overview](#overview)
- [⚠️ Type Safety](#️-type-safety)
- [Basic Usage](#basic-usage)
- [Common Patterns](#common-patterns)
  - [Conditional Expressions](#conditional-expressions)
  - [String Interpolation](#string-interpolation)
  - [Null Coalescing](#null-coalescing)
- [Caching](#caching)
  - [Why Cache?](#why-cache)
  - [When to Use Caching](#when-to-use-caching)
  - [Cache Usage](#cache-usage)
  - [AST-Based Caching](#ast-based-caching)
  - [Performance Comparison](#performance-comparison)
- [Cache Configuration](#cache-configuration)
- [Cache Statistics](#cache-statistics)
- [Performance Tuning](#performance-tuning)
- [Thread Safety](#thread-safety)
- [Troubleshooting](#troubleshooting)
- [Best Practices](#best-practices)
- [Examples](#examples)

## Overview

This package provides a simple API for working with CEL expressions in Go:

- **Compile**: Parse and validate CEL expressions with optional caching
- **Eval**: Execute compiled programs with variables
- **Functional Options**: Use `WithCache()`, `WithContext()`, and `WithCostLimit()` for configuration

## ⚠️ Type Safety

**IMPORTANT**: `CompileResult` is bound to the variable types declared during compilation. The CEL runtime will produce errors if you provide mismatched types at evaluation time.

### Correct Usage ✅

```go
expr := celexp.Expression("x + 10")
compiled, _ := expr.Compile([]cel.EnvOption{
    cel.Variable("x", cel.IntType),
})

// ✅ Correct - x is int64 (CEL's int type)
result, _ := compiled.Eval(map[string]any{"x": int64(5)})
fmt.Println(result) // 15
```

### Incorrect Usage ❌

```go
// ❌ WRONG - x is string, not int64
result, err := compiled.Eval(map[string]any{"x": "hello"})
// Error: no such overload: add_int64_int64 applied to (string, int)

// ❌ WRONG - x is int (Go int), not int64 (CEL int)
result, err := compiled.Eval(map[string]any{"x": 5})
// Error: no matching overload for '_+_' applied to (int, int64)
```

### Type Mapping

CEL uses specific types that must match your Go values:

| CEL Type | Go Type | Example |
|----------|---------|---------|
| `cel.IntType` | `int64` | `int64(42)` |
| `cel.UintType` | `uint64` | `uint64(42)` |
| `cel.DoubleType` | `float64` | `float64(3.14)` |
| `cel.BoolType` | `bool` | `true` |
| `cel.StringType` | `string` | `"hello"` |
| `cel.BytesType` | `[]byte` | `[]byte("data")` |
| `cel.ListType(T)` | `[]T` | `[]any{int64(1), int64(2)}` |
| `cel.MapType(K,V)` | `map[K]V` | `map[string]any{"key": "value"}` |

### Prevention: Validation

Use `ValidateVars()` before `Eval()` for better error messages:

```go
vars := map[string]any{"x": "hello"} // Wrong type

if err := compiled.ValidateVars(vars); err != nil {
    log.Printf("Validation failed: %v", err)
    // Error: variable "x" type mismatch: expected int, got string (actual value: hello)
}
```

## Basic Usage

### Simple Compilation

```go
import (
    "fmt"
    "github.com/google/cel-go/cel"
    "github.com/oakwood-commons/scafctl/pkg/celexp"
)

func main() {
    // Define the expression and variables
    expr := celexp.Expression("user.age >= 18 && user.country == 'US'")
    
    // Compile the expression (automatically uses global cache if registered)
    compiled, err := expr.Compile([]cel.EnvOption{
        cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
    })
    if err != nil {
        panic(err)
    }
    
    // Evaluate with actual data
    result, err := compiled.Eval(map[string]any{
        "user": map[string]any{
            "age": 25,
            "country": "US",
        },
    })
    if err != nil {
        panic(err)
    }
    
    fmt.Println(result) // true
}
```

**Note**: After calling `celexp.SetCacheFactory(env.GlobalCache)` at application startup, the global cache is automatically used. No need to explicitly pass `WithCache()`!

### With Options

The new functional options API provides a clean, flexible way to customize compilation:

```go
import (
    "context"
    "time"
    "github.com/google/cel-go/cel"
    "github.com/oakwood-commons/scafctl/pkg/celexp"
)

func main() {
    expr := celexp.Expression("x + y")
    
    // With context timeout
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    compiled, err := expr.Compile(
        []cel.EnvOption{
            cel.Variable("x", cel.IntType),
            cel.Variable("y", cel.IntType),
        },
        celexp.WithContext(ctx),
        celexp.WithCostLimit(50000),
        celexp.WithCache(celexp.NewProgramCache(100)),
    )
    if err != nil {
        panic(err)
    }
    
    result, _ := compiled.Eval(map[string]any{"x": int64(10), "y": int64(20)})
    fmt.Println(result) // 30
}
```

## Common Patterns

The package provides helper functions for common CEL expression patterns.

### Conditional Expressions

Use `NewConditional()` for simple if/then/else logic:

```go
// Create a ternary expression
expr := celexp.NewConditional("age >= 18", `"adult"`, `"minor"`)
// Equivalent to: age >= 18 ? "adult" : "minor"

compiled, _ := expr.Compile([]cel.EnvOption{
    cel.Variable("age", cel.IntType),
})

result, _ := compiled.Eval(map[string]any{"age": int64(25)})
fmt.Println(result) // "adult"
```

### String Interpolation

Use `NewStringInterpolation()` to embed variables in strings:

```go
// ${var} placeholders are converted to CEL string concatenation
expr := celexp.NewStringInterpolation("Hello, ${name}! You are ${age} years old.")
// Equivalent to: "Hello, " + name + "! You are " + string(age) + " years old."

compiled, _ := expr.Compile([]cel.EnvOption{
    cel.Variable("name", cel.StringType),
    cel.Variable("age", cel.IntType),
})

result, _ := compiled.Eval(map[string]any{
    "name": "Alice",
    "age":  int64(30),
})
fmt.Println(result) // "Hello, Alice! You are 30 years old."
```

**Nested properties** are supported:

```go
expr := celexp.NewStringInterpolation("User: ${user.name} (${user.email})")

compiled, _ := expr.Compile([]cel.EnvOption{
    cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
})

result, _ := compiled.Eval(map[string]any{
    "user": map[string]any{
        "name":  "Bob",
        "email": "bob@example.com",
    },
})
// "User: Bob (bob@example.com)"
```

**Escaping**: Use `\${` to include a literal `${` in the output:

```go
expr := celexp.NewStringInterpolation(`Price: \${price}`)
// Output: "Price: ${price}" (literal, not interpolated)
```

### Null Coalescing

Use `NewCoalesce()` for fallback values (similar to SQL COALESCE or JavaScript `??`):

```go
// Returns first non-null value
expr := celexp.NewCoalesce("user.nickname", "user.name", `"Guest"`)
// Returns user.nickname if not null, else user.name if not null, else "Guest"

compiled, _ := expr.Compile([]cel.EnvOption{
    cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
})

// Case 1: nickname exists
result, _ := compiled.Eval(map[string]any{
    "user": map[string]any{
        "nickname": "Bobby",
        "name":     "Robert",
    },
})
fmt.Println(result) // "Bobby"

// Case 2: only name exists
result, _ = compiled.Eval(map[string]any{
    "user": map[string]any{
        "name": "Robert",
    },
})
fmt.Println(result) // "Robert"

// Case 3: neither exists
result, _ = compiled.Eval(map[string]any{
    "user": map[string]any{},
})
fmt.Println(result) // "Guest"
```

## Caching

### Why Cache?

CEL compilation involves several expensive operations:

1. **Parsing**: Converting the expression string into an Abstract Syntax Tree (AST)
2. **Type Checking**: Validating types and resolving function calls
3. **Program Generation**: Creating executable bytecode

These operations can take **~40-50 microseconds** per compilation. For applications that evaluate the same expressions repeatedly, this overhead adds up quickly.

**With caching, subsequent compilations take only ~200 nanoseconds** - approximately **200x faster**!

### When to Use Caching

✅ **Use caching when:**

- Evaluating the same expressions with different data (e.g., rule engines)
- Processing templates that use CEL expressions
- Building API servers with CEL-based validation rules
- Running expressions in loops or hot paths
- Working with configuration-driven logic

❌ **Skip caching when:**

- Each expression is unique and won't be reused
- Memory is extremely constrained
- Expression strings are dynamically generated and rarely repeat

### Cache Usage

```go
import (
    "context"
    "github.com/google/cel-go/cel"
    "github.com/oakwood-commons/scafctl/pkg/celexp"
)

func main() {
    // Create a cache (holds up to 100 compiled programs)
    cache := celexp.NewProgramCache(100)
    
    // Define a reusable expression
    expr := celexp.Expression("price * quantity * (1 - discount)")
    opts := []cel.EnvOption{
        cel.Variable("price", cel.DoubleType),
        cel.Variable("quantity", cel.IntType),
        cel.Variable("discount", cel.DoubleType),
    }
    
    // First compilation - cache MISS (~40,000 ns)
    compiled1, err := expr.Compile(opts,
        celexp.WithCache(cache),
        celexp.WithContext(context.Background()),
        celexp.WithCostLimit(celexp.GetDefaultCostLimit()),
    )
    if err != nil {
        panic(err)
    }
    
    // Calculate for first order
    result1, _ := compiled1.Eval(map[string]any{
        "price": 29.99,
        "quantity": int64(2),
        "discount": 0.10,
    })
    fmt.Printf("Order 1 total: $%.2f\n", result1)
    
    // Second compilation - cache HIT (~200 ns) ⚡
    compiled2, err := expr.Compile(opts,
        celexp.WithCache(cache),
        celexp.WithContext(context.Background()),
        celexp.WithCostLimit(celexp.GetDefaultCostLimit()),
    )
    if err != nil {
        panic(err)
    }
    
    // Calculate for second order (same expression, different values)
    result2, _ := compiled2.Eval(map[string]any{
        "price": 49.99,
        "quantity": int64(3),
        "discount": 0.15,
    })
    fmt.Printf("Order 2 total: $%.2f\n", result2)
    
    // Check cache performance
    stats := cache.Stats()
    fmt.Printf("Cache efficiency: %.1f%% hit rate\n", stats.HitRate)
}
```

### AST-Based Caching

**NEW**: The package now supports AST-based cache keys that ignore variable names, allowing structurally identical expressions to share cache entries.

#### The Problem with Traditional Caching

Traditional caching creates separate entries for expressions with different variable names, even if they're structurally identical:

```go
cache := celexp.NewProgramCache(100)

// These create 4 SEPARATE cache entries (0% cache sharing):
expr1 := celexp.Expression("x + y")
expr1.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType), cel.Variable("y", cel.IntType)}, celexp.WithCache(cache))

expr2 := celexp.Expression("a + b")
expr2.Compile([]cel.EnvOption{cel.Variable("a", cel.IntType), cel.Variable("b", cel.IntType)}, celexp.WithCache(cache))

expr3 := celexp.Expression("num1 + num2")
expr3.Compile([]cel.EnvOption{cel.Variable("num1", cel.IntType), cel.Variable("num2", cel.IntType)}, celexp.WithCache(cache))

expr4 := celexp.Expression("val1 + val2")
expr4.Compile([]cel.EnvOption{cel.Variable("val1", cel.IntType), cel.Variable("val2", cel.IntType)}, celexp.WithCache(cache))
```

#### The Solution: AST-Based Keys

Enable AST-based caching to share entries based on expression structure and types:

```go
// Enable AST-based caching
cache := celexp.NewProgramCache(100, celexp.WithASTBasedCaching(true))

// These now share 1 cache entry (75% cache hit rate improvement):
expr1.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType), cel.Variable("y", cel.IntType)}, celexp.WithCache(cache))  // Cache MISS
expr2.Compile([]cel.EnvOption{cel.Variable("a", cel.IntType), cel.Variable("b", cel.IntType)}, celexp.WithCache(cache))  // Cache HIT ✅
expr3.Compile([]cel.EnvOption{cel.Variable("num1", cel.IntType), cel.Variable("num2", cel.IntType)}, celexp.WithCache(cache))  // Cache HIT ✅
expr4.Compile([]cel.EnvOption{cel.Variable("val1", cel.IntType), cel.Variable("val2", cel.IntType)}, celexp.WithCache(cache))  // Cache HIT ✅
```

#### Performance Benefits

Real benchmark results:

| Metric | Traditional Caching | AST-Based Caching | Improvement |
|--------|-------------------|-------------------|-------------|
| **Cache Hit Rate** | 25% | **100%** | **+75%** |
| **Key Generation** | 30,228 ns | **646 ns** | **47x faster** |
| **Cached Eval Time** | 45,310 ns | **127 ns** | **356x faster** |
| **Memory per Eval** | 51,008 B | **336 B** | **152x less** |

#### Type Safety Preserved

AST-based caching still maintains type safety - different types produce different cache keys:

```go
cache := celexp.NewProgramCache(100, celexp.WithASTBasedCaching(true))

// Int addition
expr1 := celexp.Expression("x + y")
expr1.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType), cel.Variable("y", cel.IntType)}, celexp.WithCache(cache))

// String concatenation - DIFFERENT cache entry (different operation)
expr2 := celexp.Expression("a + b")
expr2.Compile([]cel.EnvOption{cel.Variable("a", cel.StringType), cel.Variable("b", cel.StringType)}, celexp.WithCache(cache))
```

#### When to Use AST-Based Caching

✅ **Best for:**
- Template engines with variable placeholders
- Dynamic rule evaluation systems
- Generated expressions with varying variable names
- Multi-tenant applications with per-user expressions

⚠️ **May not help:**
- Expressions with mostly unique structures
- Simple literal-only expressions
- Very small cache sizes (< 10 entries)

### Performance Comparison

Here are real benchmark results from the package:

| Operation | Time per Operation | Memory | Allocations |
|-----------|-------------------|--------|-------------|
| **Cache Hit** | **210 ns** | 152 B | 5 allocs |
| Cache Miss | 40,256 ns | 44,676 B | 670 allocs |
| No Cache | 41,856 ns | 47,998 B | 726 allocs |

**Performance Improvements:**

- **~200x faster** when cache hits
- **~300x less memory** per operation
- **~145x fewer allocations**

For an application evaluating 10,000 expressions per second:

- **Without cache**: ~420ms of CPU time
- **With cache (50% hit rate)**: ~210ms of CPU time (50% reduction)
- **With cache (90% hit rate)**: ~42ms of CPU time (90% reduction) 🚀

## Cache Configuration

### Creating a Cache

```go
// Default cache (100 entries)
cache := celexp.NewProgramCache(0) // or NewProgramCache(100)

// Small cache for memory-constrained environments
smallCache := celexp.NewProgramCache(10)

// Large cache for high-throughput applications
largeCache := celexp.NewProgramCache(1000)
```

### How the Cache Works

1. **LRU Eviction**: When the cache is full, the least recently used entry is automatically removed
2. **Thread-Safe**: Safe for concurrent use across goroutines
3. **Cache Key**: Generated from a hash of the expression and environment options
4. **Automatic Management**: No manual cleanup needed

### Cache Key Generation

The cache key is a SHA-256 hash of:

- The CEL expression string
- The environment options (variables, functions, etc.)

This ensures that:

- Identical expressions with identical options share the same cache entry
- Different expressions or options get separate cache entries
- The cache is deterministic and predictable

#### ⚠️ Important: Cache Key Includes Variable Names and Types

**The cache key includes the complete environment configuration**, which means the same expression with different variable names or types will create separate cache entries:

```go
cache := celexp.NewProgramCache(100)
expr := celexp.Expression("x + 1")

// These create SEPARATE cache entries (different variable names):
expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)}, celexp.WithCache(cache))  // Cache entry 1
expr.Compile([]cel.EnvOption{cel.Variable("y", cel.IntType)}, celexp.WithCache(cache))  // Cache entry 2

// These also create SEPARATE cache entries (different variable types):
expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)}, celexp.WithCache(cache))    // Cache entry 3
expr.Compile([]cel.EnvOption{cel.Variable("x", cel.StringType)}, celexp.WithCache(cache)) // Cache entry 4
```

**Impact on Cache Hit Rate:**

- ✅ **High hit rate**: Same expressions with consistent variable declarations (e.g., template engines, rule engines)
- ⚠️ **Lower hit rate**: Same expressions but variable names/types change dynamically
- 💡 **Tip**: Use consistent variable names across your application to maximize cache effectiveness

**Example - Good Cache Usage:**

```go
// Consistent variable naming pattern across all rules
rules := []string{
    "user.age >= 18",
    "user.country == 'US'",
    "user.verified == true",
}

// All rules use the same variable declaration = high cache reuse potential
opts := []cel.EnvOption{cel.Variable("user", cel.MapType(cel.StringType, cel.DynType))}
for _, rule := range rules {
    expr := celexp.Expression(rule)
    compiled, _ := expr.Compile(opts, celexp.WithCache(cache))
    // ... evaluate ...
}
```

**Example - Poor Cache Usage:**

```go
// Different variable names for each similar operation
expr1 := celexp.Expression("x + 1")
expr1.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)}, celexp.WithCache(cache))

expr2 := celexp.Expression("x + 1") // Same expression!
expr2.Compile([]cel.EnvOption{cel.Variable("y", cel.IntType)}, celexp.WithCache(cache)) // Different var name = cache miss
```

**Monitoring Cache Effectiveness:**

Use cache statistics to understand if your variable naming strategy is effective:

```go
stats := cache.Stats()
if stats.HitRate < 50.0 {
    // Low hit rate might indicate:
    // - Variable names/types changing frequently
    // - Unique expressions (expected)
    // - Need to increase cache size
    log.Printf("Cache hit rate: %.1f%% - consider standardizing variable names", stats.HitRate)
}
```

## Cache Statistics

Monitor cache performance with the `Stats()` method:

```go
cache := celexp.NewProgramCache(100)

// ... compile some expressions ...

stats := cache.Stats()
fmt.Printf("Cache Statistics:\n")
fmt.Printf("  Size: %d/%d entries\n", stats.Size, stats.MaxSize)
fmt.Printf("  Hits: %d\n", stats.Hits)
fmt.Printf("  Misses: %d\n", stats.Misses)
fmt.Printf("  Evictions: %d\n", stats.Evictions)
fmt.Printf("  Hit Rate: %.1f%%\n", stats.HitRate)
```

**Key Metrics:**

- **Hit Rate**: Percentage of cache hits vs. total requests (higher is better)
- **Evictions**: Number of entries removed due to size limits
- **Size**: Current number of cached programs

**Target Hit Rates:**

- **60-80%**: Good for varied workloads
- **80-95%**: Excellent for repeated patterns
- **95%+**: Optimal for template-driven applications

### Managing Cache Statistics

The package provides flexible methods for managing cache statistics:

```go
cache := celexp.NewProgramCache(100)

// ... use the cache ...

// Clear cache entries but preserve statistics for monitoring
cache.Clear()

// Clear both cache entries AND reset statistics to zero
cache.ClearWithStats()

// Reset only statistics without clearing cached entries
cache.ResetStats()
```

**Use Cases:**
- `Clear()` - Free memory during low-usage periods while keeping performance metrics
- `ClearWithStats()` - Complete reset for testing or when starting a new monitoring period
- `ResetStats()` - Reset metrics to measure performance over a new time window

## Cost Limit Configuration

CEL expressions are evaluated with a cost limit to prevent denial-of-service attacks from expensive operations. The default limit is 1,000,000 cost units.

### Setting the Default Cost Limit

```go
import "github.com/oakwood-commons/scafctl/pkg/celexp"

func init() {
    // Set a lower limit for security-sensitive environments
    celexp.SetDefaultCostLimit(500000)
    
    // Or disable cost limiting entirely (not recommended for untrusted input)
    celexp.SetDefaultCostLimit(0)
}

// Get the current default
currentLimit := celexp.GetDefaultCostLimit()
```

### Per-Expression Cost Limits

```go
// Custom cost limit for a specific expression
lowLimit := uint64(1000)
result, err := expr.Compile(
    []cel.EnvOption{cel.Variable("x", cel.IntType)},
    celexp.WithCostLimit(lowLimit),
)
```

## Context Support

All evaluation methods now support context for cancellation and timeouts:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

expr := celexp.Expression(\"heavy_computation(data)\")
result, _ := expr.Compile(cel.Variable(\"data\", cel.DynType))

// Evaluate with context - can be cancelled or time out
value, err := result.EvalWithContext(ctx, map[string]any{\"data\": bigData})
if err == context.DeadlineExceeded {
    fmt.Println(\"Evaluation timed out\")
}

// Type-specific context-aware evaluation
boolVal, err := result.EvalAsBoolWithContext(ctx, vars)
intVal, err := result.EvalAsInt64WithContext(ctx, vars)
strVal, err := result.EvalAsStringWithContext(ctx, vars)
```

## Thread Safety

The cache is fully thread-safe and can be used concurrently:

```go
cache := celexp.NewProgramCache(100)

// Safe to use from multiple goroutines
var wg sync.WaitGroup
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        expr := celexp.Expression("x + y")
        compiled, _ := expr.Compile(
            []cel.EnvOption{
                cel.Variable("x", cel.IntType),
                cel.Variable("y", cel.IntType),
            },
            celexp.WithCache(cache),
            celexp.WithContext(context.Background()),
            celexp.WithCostLimit(celexp.GetDefaultCostLimit()),
        )
        result, _ := compiled.Eval(map[string]any{
            "x": int64(10),
            "y": int64(20),
        })
        fmt.Println(result)
    }()
}
wg.Wait()
```

## Performance Tuning

### Cache Sizing

Choose the right cache size for your workload:

| Application Type | Unique Expressions | Recommended Cache Size |
|-----------------|-------------------|----------------------|
| Simple API | 10-50 | 50-100 |
| Rule Engine | 50-500 | 500-1000 |
| Template System | 100-1000 | 1000-2000 |
| Multi-tenant SaaS | 1000+ | 2000-5000 |

**Formula**: `cache_size = unique_expressions × 2 (to account for growth)`

### Cost Limits

Set appropriate cost limits to prevent resource exhaustion:

```go
// Default (no limit) - use for trusted expressions
celexp.WithCostLimit(0)

// Conservative (10K) - good for user-provided expressions
celexp.WithCostLimit(10000)

// Permissive (100K) - for complex internal logic
celexp.WithCostLimit(100000)
```

**Cost examples:**
- Simple arithmetic: ~5-20 cost
- String operations: ~10-50 cost
- List comprehensions: ~100-1000+ cost
- Nested maps/objects: ~50-500 cost

### Memory Optimization

**For memory-constrained environments:**

```go
// Small cache
cache := celexp.NewProgramCache(10)

// Disable AST caching if not beneficial
cache := celexp.NewProgramCache(100) // Default: AST caching OFF

// Use cost limits to bound execution
compiled, _ := expr.Compile(envOpts, 
    celexp.WithCostLimit(5000),
    celexp.WithCache(cache),
)
```

### Concurrency Tuning

The cache is thread-safe but uses locks. For high-concurrency scenarios:

1. **Use larger cache sizes** to reduce eviction overhead
2. **Pre-warm the cache** at startup
3. **Consider sharding** for 1000+ requests/second

```go
// Pre-warm cache at startup
func warmCache(cache *celexp.ProgramCache, expressions []string, envOpts []cel.EnvOption) {
    for _, exprStr := range expressions {
        expr := celexp.Expression(exprStr)
        _, _ = expr.Compile(envOpts, celexp.WithCache(cache))
    }
}
```

### Profiling

Monitor cache performance in production:

```go
func logCacheStats(cache *celexp.ProgramCache) {
    stats := cache.Stats()
    log.Printf("Cache stats: size=%d/%d, hits=%d, misses=%d, hit_rate=%.1f%%",
        stats.Size, stats.MaxSize, stats.Hits, stats.Misses, stats.HitRate)
}

// Log periodically
ticker := time.NewTicker(1 * time.Minute)
go func() {
    for range ticker.C {
        logCacheStats(globalCache)
    }
}()
```

## Troubleshooting

### Common Errors and Solutions

#### 1. Type Mismatch Errors

**Error**: `no such overload` or `no matching overload`

**Cause**: Variable type doesn't match declaration

**Solution**:
```go
// ❌ Wrong
vars := map[string]any{"x": 5}  // int, not int64

// ✅ Correct
vars := map[string]any{"x": int64(5)}
```

#### 2. Missing Variable Errors

**Error**: `undeclared reference to 'x'`

**Cause**: Variable not included in compilation

**Solution**:
```go
// ❌ Missing variable declaration
compiled, _ := expr.Compile([]cel.EnvOption{})

// ✅ Declare all variables
compiled, _ := expr.Compile([]cel.EnvOption{
    cel.Variable("x", cel.IntType),
    cel.Variable("y", cel.IntType),
})
```

#### 3. Cost Limit Exceeded

**Error**: `evaluation cost exceeded`

**Cause**: Expression too complex or cost limit too low

**Solution**:
```go
// Increase cost limit
compiled, _ := expr.Compile(envOpts, celexp.WithCostLimit(50000))

// Or remove limit (trusted expressions only)
compiled, _ := expr.Compile(envOpts, celexp.WithCostLimit(0))
```

#### 4. nil Dereference Errors

**Error**: `no such key: 'field'`

**Cause**: Accessing property on nil/missing value

**Solution**:
```go
// ❌ No null safety
expr := celexp.Expression("user.name")

// ✅ Add null checks
expr := celexp.Expression("has(user) && has(user.name) ? user.name : 'Unknown'")

// ✅ Use NewCoalesce helper
expr := celexp.NewCoalesce("user.name", `"Unknown"`)
```

#### 5. Low Cache Hit Rate

**Problem**: Cache hit rate below 50%

**Possible causes**:
- Cache size too small (eviction happening)
- Expressions not reused enough
- Variable names/types changing

**Solutions**:
```go
// 1. Increase cache size
cache := celexp.NewProgramCache(1000)  // was 100

// 2. Enable AST-based caching for variable name variations
cache := celexp.NewProgramCache(100, celexp.WithASTBasedCaching(true))

// 3. Standardize variable names across expressions
// Use consistent naming: "user", "item", "config" etc.
```

#### 6. Context Timeout Errors

**Error**: `context deadline exceeded`

**Cause**: Compilation/evaluation took too long

**Solution**:
```go
// Increase timeout
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

compiled, _ := expr.Compile(envOpts, celexp.WithContext(ctx))

// Or use background context for no timeout
compiled, _ := expr.Compile(envOpts, celexp.WithContext(context.Background()))
```

### Debugging Tips

**1. Enable verbose logging:**
```go
import "log"

vars := map[string]any{"x": "hello"}
if err := compiled.ValidateVars(vars); err != nil {
    log.Printf("Validation error: %v", err)
    log.Printf("Provided vars: %+v", vars)
    // Validation error: variable "x" type mismatch: expected int, got string
}
```

**2. Check declared variables:**
```go
// See what variables are expected
info := compiled.GetDeclaredVars()
for _, v := range info {
    log.Printf("Expected variable: %s (type: %s)", v.Name, v.Type)
}
```

**3. Test expressions in isolation:**
```go
func TestExpression(t *testing.T) {
    expr := celexp.Expression("x + y")
    compiled, err := expr.Compile([]cel.EnvOption{
        cel.Variable("x", cel.IntType),
        cel.Variable("y", cel.IntType),
    })
    require.NoError(t, err)
    
    result, err := compiled.Eval(map[string]any{
        "x": int64(10),
        "y": int64(20),
    })
    require.NoError(t, err)
    assert.Equal(t, int64(30), result)
}
```

## Best Practices

### 1. Reuse Cache Instances

**❌ Don't create new caches repeatedly:**

```go
// BAD - Creates new cache each time
func processRequest(exprStr string) {
    cache := celexp.NewProgramCache(100) // New cache every call!
    expr := celexp.Expression(exprStr)
    prog, _ := expr.Compile(nil, celexp.WithCache(cache))
    // ...
}
```

**✅ Create once, reuse everywhere:**

```go
// GOOD - Single cache for the application
var globalCache = celexp.NewProgramCache(1000)

func processRequest(exprStr string) {
    expr := celexp.Expression(exprStr)
    compiled, _ := expr.Compile(nil, celexp.WithCache(globalCache))
    // ...
}
```

### 2. Choose Appropriate Cache Size

- **Small applications**: 10-50 entries
- **Medium applications**: 100-500 entries
- **Large applications**: 1000+ entries
- **Rule of thumb**: Set to 2-3x your unique expression count

### 3. Monitor Hit Rates

Regularly check cache statistics to ensure effectiveness:

```go
if stats := cache.Stats(); stats.HitRate < 50.0 {
    log.Warnf("Low cache hit rate: %.1f%% - consider increasing cache size", stats.HitRate)
}
```

### 4. Handle Compilation Errors

Compilation errors are **not cached** (by design):

```go
expr := celexp.Expression(invalidExpr)
compiled, err := expr.Compile(nil, celexp.WithCache(cache))
if err != nil {
    // This error won't be cached - the expression will be
    // re-compiled on the next call
    return fmt.Errorf("invalid expression: %w", err)
}
```

### 5. Clear Cache When Needed

If you need to reset the cache (e.g., configuration reload):

```go
cache.Clear()          // Removes all entries, preserves statistics
cache.ClearWithStats() // Removes all entries AND resets statistics
cache.ResetStats()     // Resets statistics without clearing entries
```

## Examples

### Example 1: Rule Engine

```go
type RuleEngine struct {
    cache *celexp.ProgramCache
    rules []Rule
}

type Rule struct {
    Name       string
    Expression string
    Priority   int
}

func NewRuleEngine(rules []Rule) *RuleEngine {
    return &RuleEngine{
        cache: celexp.NewProgramCache(len(rules) * 2),
        rules: rules,
    }
}

func (e *RuleEngine) Evaluate(ctx map[string]any) ([]string, error) {
    var matches []string
    
    opts := []cel.EnvOption{
        cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
        cel.Variable("request", cel.MapType(cel.StringType, cel.DynType)),
    }
    
    for _, rule := range e.rules {
        expr := celexp.Expression(rule.Expression)
        compiled, err := expr.Compile(opts,
            celexp.WithCache(e.cache),
            celexp.WithCostLimit(celexp.GetDefaultCostLimit()),
        )
        if err != nil {
            return nil, fmt.Errorf("rule %s: %w", rule.Name, err)
        }
        
        result, err := compiled.Eval(ctx)
        if err != nil {
            return nil, fmt.Errorf("rule %s evaluation: %w", rule.Name, err)
        }
        
        if result.(bool) {
            matches = append(matches, rule.Name)
        }
    }
    
    return matches, nil
}
```

### Example 2: Template Processing

```go
type TemplateProcessor struct {
    cache *celexp.ProgramCache
}

func NewTemplateProcessor() *TemplateProcessor {
    return &TemplateProcessor{
        cache: celexp.NewProgramCache(500),
    }
}

func (p *TemplateProcessor) RenderField(expression string, data map[string]any) (string, error) {
    opts := []cel.EnvOption{
        cel.Variable("data", cel.MapType(cel.StringType, cel.DynType)),
    }
    
    // Expression like: data.user.firstName + " " + data.user.lastName
    expr := celexp.Expression(expression)
    compiled, err := expr.Compile(opts,
        celexp.WithCache(p.cache),
        celexp.WithCostLimit(celexp.GetDefaultCostLimit()),
    )
    if err != nil {
        return "", err
    }
    
    result, err := compiled.Eval(map[string]any{"data": data})
    if err != nil {
        return "", err
    }
    
    return fmt.Sprint(result), nil
}

func (p *TemplateProcessor) Stats() celexp.CacheStats {
    return p.cache.Stats()
}
```

### Example 3: API Validation

```go
type Validator struct {
    cache *celexp.ProgramCache
}

func NewValidator() *Validator {
    return &Validator{
        cache: celexp.NewProgramCache(100),
    }
}

func (v *Validator) ValidateRequest(rules map[string]string, req map[string]any) error {
    opts := []cel.EnvOption{
        cel.Variable("request", cel.MapType(cel.StringType, cel.DynType)),
    }
    
    for field, rule := range rules {
        expr := celexp.Expression(rule)
        compiled, err := expr.Compile(opts,
            celexp.WithCache(v.cache),
            celexp.WithCostLimit(celexp.GetDefaultCostLimit()),
        )
        if err != nil {
            return fmt.Errorf("invalid validation rule for %s: %w", field, err)
        }
        
        result, err := compiled.Eval(map[string]any{"request": req})
        if err != nil {
            return fmt.Errorf("validation error for %s: %w", field, err)
        }
        
        if !result.(bool) {
            return fmt.Errorf("validation failed for field: %s", field)
        }
    }
    
    return nil
}
```

## Advanced Topics

### Custom Environment Options

You can cache programs with custom CEL functions:

```go
import "github.com/google/cel-go/ext"

opts := []cel.EnvOption{
    cel.Variable("input", cel.StringType),
    ext.Strings(), // Built-in string extensions
}

expr := celexp.Expression("input.matches('[0-9]+') && int(input) > 100")
prog, err := expr.Compile(opts, celexp.WithCache(cache))
```

### Cache with Multiple Option Sets

Different option sets create different cache entries:

```go
// These will be cached separately
expr := celexp.Expression("x + y")
prog1, _ := expr.Compile(
    []cel.EnvOption{
        cel.Variable("x", cel.IntType),
        cel.Variable("y", cel.IntType),
    },
    celexp.WithCache(cache),
)

prog2, _ := expr.Compile(
    []cel.EnvOption{
        cel.Variable("x", cel.DoubleType), // Different type!
        cel.Variable("y", cel.DoubleType),
    },
    celexp.WithCache(cache),
)
```

### Memory Considerations

Each cached program consumes approximately:

- **~10-50 KB** for simple expressions
- **~50-200 KB** for complex expressions with many variables
- **~200-500 KB** for very complex expressions with custom functions

A cache of 100 entries typically uses **1-5 MB** of memory.

## Benchmarking Your Use Case

To benchmark caching in your specific scenario:

```go
func BenchmarkYourExpression(b *testing.B) {
    cache := celexp.NewProgramCache(100)
    expr := celexp.Expression("your.expression.here")
    opts := []cel.EnvOption{ /* your options */ }
    
    // Prime the cache
    expr.Compile(opts, celexp.WithCache(cache), celexp.WithCostLimit(celexp.GetDefaultCostLimit()))
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = expr.Compile(opts, celexp.WithCache(cache), celexp.WithCostLimit(celexp.GetDefaultCostLimit()))
    }
}
```

Run with: `go test -bench=BenchmarkYourExpression -benchmem`

## Troubleshooting

### Low Hit Rates

If you're seeing low cache hit rates:

1. **Check expression uniqueness**: Are your expressions actually repeating?
2. **Verify options consistency**: Different options = different cache entries
3. **Increase cache size**: May need more entries for your workload
4. **Log cache keys**: Add debug logging to see what's being cached

### High Memory Usage

If cache is using too much memory:

1. **Reduce cache size**: Lower the max entries
2. **Clear periodically**: Call `cache.Clear()` when appropriate
3. **Profile memory**: Use Go's pprof to analyze actual usage

### Thread Contention

If you see contention on the cache lock:

1. **Use multiple caches**: Shard by expression hash
2. **Reduce cache operations**: Batch compilations if possible
3. **Profile**: Confirm the cache is actually the bottleneck

## Related Resources

- [CEL Specification](https://github.com/google/cel-spec)
- [CEL Go Implementation](https://github.com/google/cel-go)
- [CEL Language Guide](https://github.com/google/cel-spec/blob/master/doc/langdef.md)

## License

This package is part of the scafctl project. See the repository LICENSE for details.
