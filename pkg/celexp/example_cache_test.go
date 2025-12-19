package celexp

import (
	"fmt"

	"github.com/google/cel-go/cel"
)

// ExampleCompileWithCache demonstrates basic usage of the CEL program cache.
func ExampleCompileWithCache() {
	// Create a cache with a maximum size of 100 programs
	cache := NewProgramCache(100)

	// Define the CEL expression and options
	expr := Expression("x * 2 + y")
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}

	// First compilation - cache miss
	compiled1, err := CompileWithCache(cache, expr, opts...)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Evaluate the program
	result1, err := compiled1.Eval(map[string]any{"x": int64(10), "y": int64(5)})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Result 1: %v\n", result1)

	// Second compilation - cache hit (much faster)
	compiled2, err := CompileWithCache(cache, expr, opts...)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Evaluate again with different values
	result2, err := compiled2.Eval(map[string]any{"x": int64(20), "y": int64(3)})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Result 2: %v\n", result2)

	// Check cache statistics
	stats := cache.Stats()
	fmt.Printf("Cache hits: %d, misses: %d, hit rate: %.1f%%\n", stats.Hits, stats.Misses, stats.HitRate)

	// Output:
	// Result 1: 25
	// Result 2: 43
	// Cache hits: 1, misses: 1, hit rate: 50.0%
}

// ExampleProgramCache_Stats demonstrates monitoring cache performance.
func ExampleProgramCache_Stats() {
	cache := NewProgramCache(10)

	// Compile several expressions
	expressions := []Expression{"1 + 2", "3 * 4", "5 - 1"}
	for _, expr := range expressions {
		_, _ = CompileWithCache(cache, expr)
	}

	// Access some cached programs
	_, _ = CompileWithCache(cache, Expression("1 + 2")) // Hit
	_, _ = CompileWithCache(cache, Expression("3 * 4")) // Hit

	// Get statistics
	stats := cache.Stats()
	fmt.Printf("Size: %d/%d\n", stats.Size, stats.MaxSize)
	fmt.Printf("Hits: %d, Misses: %d\n", stats.Hits, stats.Misses)
	fmt.Printf("Hit Rate: %.1f%%\n", stats.HitRate)
	fmt.Printf("Evictions: %d\n", stats.Evictions)

	// Output:
	// Size: 3/10
	// Hits: 2, Misses: 3
	// Hit Rate: 40.0%
	// Evictions: 0
}

// ExampleNewProgramCache demonstrates creating a cache with different sizes.
func ExampleNewProgramCache() {
	// Create a small cache for limited memory environments
	smallCache := NewProgramCache(10)
	fmt.Printf("Small cache max size: %d\n", smallCache.Stats().MaxSize)

	// Create a larger cache for high-throughput scenarios
	largeCache := NewProgramCache(1000)
	fmt.Printf("Large cache max size: %d\n", largeCache.Stats().MaxSize)

	// Default size (100) is used for invalid sizes
	defaultCache := NewProgramCache(0)
	fmt.Printf("Default cache max size: %d\n", defaultCache.Stats().MaxSize)

	// Output:
	// Small cache max size: 10
	// Large cache max size: 1000
	// Default cache max size: 100
}
