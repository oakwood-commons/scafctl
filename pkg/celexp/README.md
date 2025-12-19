# CEL Expression Package

A Go package for compiling and evaluating [Common Expression Language (CEL)](https://github.com/google/cel-spec) expressions with optional caching for improved performance.

## Table of Contents

- [Overview](#overview)
- [Basic Usage](#basic-usage)
- [Caching](#caching)
  - [Why Cache?](#why-cache)
  - [When to Use Caching](#when-to-use-caching)
  - [Cache Usage](#cache-usage)
  - [Performance Comparison](#performance-comparison)
- [Cache Configuration](#cache-configuration)
- [Cache Statistics](#cache-statistics)
- [Thread Safety](#thread-safety)
- [Best Practices](#best-practices)
- [Examples](#examples)

## Overview

This package provides a simple API for working with CEL expressions in Go:

- **Compile**: Parse and validate CEL expressions
- **Eval**: Execute compiled programs with variables
- **CompileWithCache**: Compile with automatic caching for performance

## Basic Usage

### Without Caching

```go
import (
    "fmt"
    "github.com/google/cel-go/cel"
    "github.com/kcloutie/scafctl/pkg/celexp"
)

func main() {
    // Define the expression and variables
    expr := celexp.CelExpression("user.age >= 18 && user.country == 'US'")
    
    // Compile the expression
    compiled, err := expr.Compile(
        cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
    )
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
    "github.com/google/cel-go/cel"
    "github.com/kcloutie/scafctl/pkg/celexp"
)

func main() {
    // Create a cache (holds up to 100 compiled programs)
    cache := celexp.NewProgramCache(100)
    
    // Define a reusable expression
    expr := celexp.CelExpression("price * quantity * (1 - discount)")
    opts := []cel.EnvOption{
        cel.Variable("price", cel.DoubleType),
        cel.Variable("quantity", cel.IntType),
        cel.Variable("discount", cel.DoubleType),
    }
    
    // First compilation - cache MISS (~40,000 ns)
    compiled1, err := celexp.CompileWithCache(cache, expr, opts...)
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
    compiled2, err := celexp.CompileWithCache(cache, expr, opts...)
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
        expr := celexp.CelExpression("x + y")
        compiled, _ := celexp.CompileWithCache(cache, expr,
            cel.Variable("x", cel.IntType),
            cel.Variable("y", cel.IntType),
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

## Best Practices

### 1. Reuse Cache Instances

**❌ Don't create new caches repeatedly:**

```go
// BAD - Creates new cache each time
func processRequest(expr string) {
    cache := celexp.NewProgramCache(100) // New cache every call!
    prog, _ := celexp.CompileWithCache(cache, expr)
    // ...
}
```

**✅ Create once, reuse everywhere:**

```go
// GOOD - Single cache for the application
var globalCache = celexp.NewProgramCache(1000)

func processRequest(exprStr string) {
    expr := celexp.CelExpression(exprStr)
    compiled, _ := celexp.CompileWithCache(globalCache, expr)
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
expr := celexp.CelExpression(invalidExpr)
compiled, err := celexp.CompileWithCache(cache, expr)
if err != nil {
    // This error won't be cached - the expression will be
    // re-compiled on the next call
    return fmt.Errorf("invalid expression: %w", err)
}
```

### 5. Clear Cache When Needed

If you need to reset the cache (e.g., configuration reload):

```go
cache.Clear() // Removes all entries and resets statistics
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
        expr := celexp.CelExpression(rule.Expression)
        compiled, err := celexp.CompileWithCache(e.cache, expr, opts...)
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
    expr := celexp.CelExpression(expression)
    compiled, err := celexp.CompileWithCache(p.cache, expr, opts...)
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
        expr := celexp.CelExpression(rule)
        compiled, err := celexp.CompileWithCache(v.cache, expr, opts...)
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

prog, err := celexp.CompileWithCache(cache, 
    "input.matches('[0-9]+') && int(input) > 100", 
    opts...)
```

### Cache with Multiple Option Sets

Different option sets create different cache entries:

```go
// These will be cached separately
prog1, _ := celexp.CompileWithCache(cache, "x + y",
    cel.Variable("x", cel.IntType),
    cel.Variable("y", cel.IntType),
)

prog2, _ := celexp.CompileWithCache(cache, "x + y",
    cel.Variable("x", cel.DoubleType), // Different type!
    cel.Variable("y", cel.DoubleType),
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
    expr := "your.expression.here"
    opts := []cel.EnvOption{ /* your options */ }
    
    // Prime the cache
    celexp.CompileWithCache(cache, expr, opts...)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = celexp.CompileWithCache(cache, expr, opts...)
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
