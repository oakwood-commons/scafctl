// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp_test

import (
	"context"
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// ExampleExpression_Compile demonstrates the simplest way to compile CEL expressions.
// This is the recommended method for most use cases.
func ExampleExpression_Compile() {
	// Simple compilation with default caching and cost limits
	expr := celexp.Expression("x * 2 + y")
	result, err := expr.Compile([]cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	})
	if err != nil {
		panic(err)
	}

	value, _ := result.Eval(map[string]any{"x": int64(10), "y": int64(5)})
	fmt.Println(value)
	// Output: 25
}

// ExampleEvalAs demonstrates type-safe evaluation with generics.
// Use EvalAs[T]() for compile-time type safety instead of runtime type assertions.
func ExampleEvalAs() {
	// String result
	expr := celexp.Expression("'Hello, ' + name")
	result, _ := expr.Compile([]cel.EnvOption{cel.Variable("name", cel.StringType)})
	str, _ := celexp.EvalAs[string](result, map[string]any{"name": "World"})
	fmt.Println(str)

	// Boolean result
	expr = celexp.Expression("age >= 18")
	result, _ = expr.Compile([]cel.EnvOption{cel.Variable("age", cel.IntType)})
	isAdult, _ := celexp.EvalAs[bool](result, map[string]any{"age": int64(21)})
	fmt.Println(isAdult)

	// Integer result
	expr = celexp.Expression("x + y")
	result, _ = expr.Compile([]cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	})
	sum, _ := celexp.EvalAs[int64](result, map[string]any{"x": int64(10), "y": int64(20)})
	fmt.Println(sum)

	// List result
	expr = celexp.Expression("[1, 2, 3].map(x, x * 2)")
	result, _ = expr.Compile([]cel.EnvOption{})
	list, _ := celexp.EvalAs[[]any](result, nil)
	fmt.Println(len(list))

	// Output:
	// Hello, World
	// true
	// 30
	// 3
}

// ExampleGetDefaultCacheStats shows how to monitor cache performance.
func ExampleGetDefaultCacheStats() {
	// Compile a few expressions
	expr1 := celexp.Expression("1 + 1")
	expr1.Compile([]cel.EnvOption{})

	expr2 := celexp.Expression("2 * 3")
	expr2.Compile([]cel.EnvOption{})

	// Compile the same expression again (cache hit)
	expr1.Compile([]cel.EnvOption{})

	// Get statistics
	stats := celexp.GetDefaultCacheStats()
	fmt.Printf("Cache size: %d/%d\n", stats.Size, stats.MaxSize)
	fmt.Printf("Hits: %d, Misses: %d\n", stats.Hits, stats.Misses)
	fmt.Printf("Hit rate: %.1f%%\n", stats.HitRate)
	// Output will vary, but shows cache is working
}

// ExampleClearDefaultCache shows how to clear the cache.
func ExampleClearDefaultCache() {
	// Add some entries
	expr := celexp.Expression("x + 1")
	expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)})

	// Clear cache (useful for testing or memory management)
	celexp.ClearDefaultCache()

	stats := celexp.GetDefaultCacheStats()
	fmt.Printf("Cache size after clear: %d\n", stats.Size)
	// Output: Cache size after clear: 0
}

// Example showing when to use functional options for different scenarios.
func Example_choosingCompilationMethod() {
	// 1. SIMPLE CASE - just pass env options
	simpleExpr := celexp.Expression("x + y")
	simpleExpr.Compile([]cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	})

	// 2. WITH TIMEOUT - add WithContext option
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	timeoutExpr := celexp.Expression("name.size() > 0")
	timeoutExpr.Compile(
		[]cel.EnvOption{cel.Variable("name", cel.StringType)},
		celexp.WithContext(ctx),
	)

	// 3. CUSTOM LIMITS - add WithCostLimit option
	customExpr := celexp.Expression("x * 2")
	customExpr.Compile(
		[]cel.EnvOption{cel.Variable("x", cel.IntType)},
		celexp.WithCostLimit(10000),
	)

	// 4. EVERYTHING - combine multiple options
	fullCtx := context.Background()
	fullCache := celexp.NewProgramCache(100)
	fullExpr := celexp.Expression("x > 0")
	fullExpr.Compile(
		[]cel.EnvOption{cel.Variable("x", cel.IntType)},
		celexp.WithContext(fullCtx),
		celexp.WithCostLimit(50000),
		celexp.WithCache(fullCache),
	)

	fmt.Println("All methods work!")
	// Output: All methods work!
}
