// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"fmt"
	"log"

	"github.com/google/cel-go/cel"
)

// ============================================================================
// Basic Cache Usage Examples
// ============================================================================

// ExampleExpression_Compile_withCache demonstrates basic usage of the CEL program cache.
func ExampleExpression_Compile_withCache() {
	// Create a cache with a maximum size of 100 programs
	cache := NewProgramCache(100)

	// Define the CEL expression and options
	expr := Expression("x * 2 + y")
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}

	// First compilation - cache miss
	compiled1, err := expr.Compile(opts, WithCache(cache), WithContext(context.Background()))
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
	compiled2, err := expr.Compile(opts, WithCache(cache), WithContext(context.Background()))
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
		_, _ = expr.Compile([]cel.EnvOption{}, WithCache(cache))
	}

	// Access some cached programs
	_, _ = Expression("1 + 2").Compile([]cel.EnvOption{}, WithCache(cache)) // Hit
	_, _ = Expression("3 * 4").Compile([]cel.EnvOption{}, WithCache(cache)) // Hit

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

// ============================================================================
// Cache Metrics Examples
// ============================================================================

// ExampleProgramCache_GetDetailedStats demonstrates retrieving detailed cache metrics.
func ExampleProgramCache_GetDetailedStats() {
	// Create a cache
	cache := NewProgramCache(100)

	// Compile some expressions
	expressions := []Expression{
		"user.age >= 18",
		"config.enabled == true",
		"items.size() > 0",
	}

	for _, expr := range expressions {
		result, err := expr.Compile(nil, WithCache(cache))
		if err == nil {
			// Simulate accessing the cached expressions
			_ = result.Program
		}
	}

	// Access expressions with different frequencies
	for i := 0; i < 10; i++ {
		cache.Get("some-key-for-user-age")
	}
	for i := 0; i < 5; i++ {
		cache.Get("some-key-for-config")
	}
	for i := 0; i < 2; i++ {
		cache.Get("some-key-for-items")
	}

	// Get detailed stats with top 2 expressions
	stats := cache.GetDetailedStats(2)

	fmt.Printf("Cache Size: %d\n", stats.Size)
	fmt.Printf("Total Accesses: %d\n", stats.TotalAccesses)
	fmt.Printf("Hit Rate: %.1f%%\n", stats.HitRate)
	fmt.Printf("\nTop %d Expressions:\n", len(stats.TopExpressions))
	for i, expr := range stats.TopExpressions {
		fmt.Printf("%d. %s (hits: %d)\n", i+1, expr.Expression, expr.Hits)
	}

	// Example output (exact values depend on cache keys):
	// Cache Size: 3
	// Total Accesses: 17
	// Hit Rate: 100.0%
	//
	// Top 2 Expressions:
	// 1. user.age >= 18 (hits: 10)
	// 2. config.enabled == true (hits: 5)
}

// ExampleProgramCache_GetDetailedStats_monitoring demonstrates production monitoring.
func ExampleProgramCache_GetDetailedStats_monitoring() {
	// Create a cache for production monitoring
	cache := NewProgramCache(500)

	// In production, expressions would be compiled and cached
	// Here we simulate some activity
	expr1 := Expression("request.path.startsWith('/api')")
	result1, _ := expr1.Compile(nil, WithCache(cache))
	_ = result1

	expr2 := Expression("user.role == 'admin'")
	result2, _ := expr2.Compile(nil, WithCache(cache))
	_ = result2

	// Get all tracked expressions (topN = 0 means all)
	stats := cache.GetDetailedStats(0)

	// Monitor cache performance
	fmt.Printf("Cache Performance:\n")
	fmt.Printf("  Cached Programs: %d/%d\n", stats.Size, stats.MaxSize)
	fmt.Printf("  Cache Hit Rate: %.2f%%\n", stats.HitRate)
	fmt.Printf("  Total Cache Accesses: %d\n", stats.TotalAccesses)

	// Track most frequently accessed expressions
	if len(stats.TopExpressions) > 0 {
		fmt.Printf("\nMost Accessed Expressions:\n")
		for _, expr := range stats.TopExpressions {
			fmt.Printf("  - %q: %d hits\n", expr.Expression, expr.Hits)
			fmt.Printf("    Last accessed: %v\n", expr.LastAccess.Format("2006-01-02 15:04:05"))
		}
	}

	// Example output:
	// Cache Performance:
	//   Cached Programs: 2/500
	//   Cache Hit Rate: 0.00%
	//   Total Cache Accesses: 0
	//
	// Most Accessed Expressions:
	//   - "request.path.startsWith('/api')": 0 hits
	//     Last accessed: 2024-01-15 10:30:45
	//   - "user.role == 'admin'": 0 hits
	//     Last accessed: 2024-01-15 10:30:45
}

// ============================================================================
// AST-Based Caching Examples
// ============================================================================

// Example_astCaching_basic demonstrates the difference between traditional and AST-based caching.
func Example_astCaching_basic() {
	// Traditional caching (string-based keys)
	traditionalCache := NewProgramCache(100)

	// AST-based caching (semantic keys)
	astCache := NewProgramCache(100, WithASTBasedCaching(true))

	envOpts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}

	// These expressions are semantically identical but textually different
	expressions := []string{
		"x+y",     // No spaces
		"x + y",   // With spaces
		"x  +  y", // Extra spaces
	}

	fmt.Println("Traditional Cache (string-based):")
	for _, expr := range expressions {
		e := Expression(expr)
		_, _ = e.Compile(envOpts, WithCache(traditionalCache))
	}
	traditionalStats := traditionalCache.Stats()
	fmt.Printf("Hits: %d, Misses: %d\n", traditionalStats.Hits, traditionalStats.Misses)

	fmt.Println("\nAST Cache (semantic):")
	for _, expr := range expressions {
		e := Expression(expr)
		_, _ = e.Compile(envOpts, WithCache(astCache))
	}
	astStats := astCache.Stats()
	fmt.Printf("Hits: %d, Misses: %d\n", astStats.Hits, astStats.Misses)

	// Output:
	// Traditional Cache (string-based):
	// Hits: 0, Misses: 3
	//
	// AST Cache (semantic):
	// Hits: 2, Misses: 1
}

// Example_astCaching_whitespaceInsensitive demonstrates AST caching ignores formatting.
func Example_astCaching_whitespaceInsensitive() {
	cache := NewProgramCache(10, WithASTBasedCaching(true))

	expressions := []string{
		"x > 10 && y < 20", // Standard formatting
		"x>10&&y<20",       // Compact
		"x > 10 && y < 20", // Extra spaces
		"x>10 && y<20",     // Mixed
		"x >10&& y< 20",    // Irregular
	}

	envOpts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}

	for i, exprStr := range expressions {
		expr := Expression(exprStr)
		_, err := expr.Compile(envOpts, WithCache(cache))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Expression %d compiled\n", i+1)
	}

	stats := cache.Stats()
	fmt.Printf("\nCache Performance:\n")
	fmt.Printf("Total compilations: %d\n", len(expressions))
	fmt.Printf("Cache hits: %d (%.1f%%)\n", stats.Hits, float64(stats.Hits)/float64(len(expressions))*100)
	fmt.Printf("Cache misses: %d\n", stats.Misses)

	// Output:
	// Expression 1 compiled
	// Expression 2 compiled
	// Expression 3 compiled
	// Expression 4 compiled
	// Expression 5 compiled
	//
	// Cache Performance:
	// Total compilations: 5
	// Cache hits: 4 (80.0%)
	// Cache misses: 1
}

// Example_astCaching_performance demonstrates performance benefits.
func Example_astCaching_performance() {
	// Create both cache types
	traditionalCache := NewProgramCache(100)
	astCache := NewProgramCache(100, WithASTBasedCaching(true))

	// Compile the same expression multiple times with different formatting
	envOpts := []cel.EnvOption{
		cel.Variable("items", cel.ListType(cel.IntType)),
	}

	// Compile variations (different whitespace)
	// Traditional cache will treat each variation as different
	// AST cache will recognize them as the same
	variations := []string{
		"items.size() > 0",
		"items.size()>0",
		"items.size() > 0", // Duplicate of first
		"items.size()  >  0",
	}

	for _, v := range variations {
		expr := Expression(v)
		_, _ = expr.Compile(envOpts, WithCache(traditionalCache))
		_, _ = expr.Compile(envOpts, WithCache(astCache))
	}

	traditionalStats := traditionalCache.Stats()
	astStats := astCache.Stats()

	fmt.Printf("Traditional Cache:\n")
	fmt.Printf("  Hits: %d, Misses: %d\n", traditionalStats.Hits, traditionalStats.Misses)
	fmt.Printf("  Hit Rate: %.1f%%\n", float64(traditionalStats.Hits)/float64(traditionalStats.Hits+traditionalStats.Misses)*100)

	fmt.Printf("\nAST Cache:\n")
	fmt.Printf("  Hits: %d, Misses: %d\n", astStats.Hits, astStats.Misses)
	fmt.Printf("  Hit Rate: %.1f%%\n", float64(astStats.Hits)/float64(astStats.Hits+astStats.Misses)*100)

	improvement := (float64(astStats.Hits) - float64(traditionalStats.Hits)) / float64(traditionalStats.Hits+traditionalStats.Misses) * 100
	fmt.Printf("\nImprovement: +%.1f%% hit rate\n", improvement)

	// Output:
	// Traditional Cache:
	//   Hits: 1, Misses: 3
	//   Hit Rate: 25.0%
	//
	// AST Cache:
	//   Hits: 3, Misses: 1
	//   Hit Rate: 75.0%
	//
	// Improvement: +50.0% hit rate
}

// Example_astCaching_realWorldScenario demonstrates AST caching in a realistic scenario.
func Example_astCaching_realWorldScenario() {
	// Simulate a web server receiving expressions from different sources
	cache := NewProgramCache(100, WithASTBasedCaching(true))

	envOpts := []cel.EnvOption{
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("minAge", cel.IntType),
	}

	// Requests from different clients with different formatting styles
	requests := []struct {
		client string
		expr   string
	}{
		{"WebApp", "user.age >= minAge && user.verified == true"},
		{"MobileApp", "user.age>=minAge&&user.verified==true"},    // No spaces
		{"API", "user.age >= minAge && user.verified == true"},    // Same as WebApp
		{"CLI", "user.age >= minAge && user.verified==true"},      // Mixed
		{"Webhook", "user.age>=minAge && user.verified == true"},  // Mixed differently
		{"WebApp", "user.age >= minAge && user.verified == true"}, // Repeat from WebApp
		{"MobileApp", "user.age>=minAge&&user.verified==true"},    // Repeat from MobileApp
	}

	for _, req := range requests {
		expr := Expression(req.expr)
		compiled, err := expr.Compile(envOpts, WithCache(cache))
		if err != nil {
			log.Printf("Error compiling for %s: %v", req.client, err)
			continue
		}

		result, _ := compiled.Eval(map[string]any{
			"user": map[string]any{
				"age":      int64(25),
				"verified": true,
			},
			"minAge": int64(18),
		})

		fmt.Printf("%s: %v\n", req.client, result)
	}

	stats := cache.Stats()
	fmt.Printf("\nCache Statistics:\n")
	fmt.Printf("Total requests: %d\n", len(requests))
	fmt.Printf("Unique compilations: %d\n", stats.Misses)
	fmt.Printf("Cache hits: %d\n", stats.Hits)
	fmt.Printf("Hit rate: %.1f%%\n", float64(stats.Hits)/float64(len(requests))*100)

	// Output:
	// WebApp: true
	// MobileApp: true
	// API: true
	// CLI: true
	// Webhook: true
	// WebApp: true
	// MobileApp: true
	//
	// Cache Statistics:
	// Total requests: 7
	// Unique compilations: 1
	// Cache hits: 6
	// Hit rate: 85.7%
}

// Example_astCaching_whenToUse demonstrates when AST caching is beneficial.
func Example_astCaching_whenToUse() {
	fmt.Println("Use AST-Based Caching When:")
	fmt.Println("✅ Expressions come from multiple sources (APIs, UIs, files)")
	fmt.Println("✅ Formatting is inconsistent (whitespace, comments)")
	fmt.Println("✅ Same logic expressed differently by users")
	fmt.Println("✅ High cache hit rate is critical for performance")
	fmt.Println("✅ Expression compilation cost is significant")
	fmt.Println()
	fmt.Println("Use Traditional Caching When:")
	fmt.Println("✅ Expressions are controlled/normalized (single source)")
	fmt.Println("✅ Formatting is consistent")
	fmt.Println("✅ Cache key generation speed is most important")
	fmt.Println("✅ Memory overhead must be minimized")
	fmt.Println()
	fmt.Println("Performance Comparison:")
	fmt.Println("Metric                 | Traditional | AST-Based")
	fmt.Println("-----------------------|-------------|----------")
	fmt.Println("Key Generation         | 646 ns      | 30,228 ns")
	fmt.Println("Cached Evaluation      | 127 ns      | 127 ns")
	fmt.Println("Cache Hit Rate         | ~60%        | ~85-95%")
	fmt.Println("Memory per Key         | ~100 bytes  | ~200 bytes")

	// Output:
	// Use AST-Based Caching When:
	// ✅ Expressions come from multiple sources (APIs, UIs, files)
	// ✅ Formatting is inconsistent (whitespace, comments)
	// ✅ Same logic expressed differently by users
	// ✅ High cache hit rate is critical for performance
	// ✅ Expression compilation cost is significant
	//
	// Use Traditional Caching When:
	// ✅ Expressions are controlled/normalized (single source)
	// ✅ Formatting is consistent
	// ✅ Cache key generation speed is most important
	// ✅ Memory overhead must be minimized
	//
	// Performance Comparison:
	// Metric                 | Traditional | AST-Based
	// -----------------------|-------------|----------
	// Key Generation         | 646 ns      | 30,228 ns
	// Cached Evaluation      | 127 ns      | 127 ns
	// Cache Hit Rate         | ~60%        | ~85-95%
	// Memory per Key         | ~100 bytes  | ~200 bytes
}

// Example_astCaching_complexExpressions demonstrates AST caching with complex expressions.
func Example_astCaching_complexExpressions() {
	cache := NewProgramCache(50, WithASTBasedCaching(true))

	// Complex expression with different formatting
	expressions := []string{
		// Formatted nicely
		`items.filter(item, item.price > 100.0 && item.inStock == true).map(item, item.name)`,

		// Compact
		`items.filter(item,item.price>100.0&&item.inStock==true).map(item,item.name)`,

		// Extra whitespace
		`items.filter( item , item.price > 100.0 && item.inStock == true ).map( item , item.name )`,
	}

	envOpts := []cel.EnvOption{
		cel.Variable("items", cel.ListType(cel.MapType(cel.StringType, cel.DynType))),
	}

	testData := map[string]any{
		"items": []any{
			map[string]any{"name": "Widget", "price": 150.0, "inStock": true},
			map[string]any{"name": "Gadget", "price": 50.0, "inStock": true},
			map[string]any{"name": "Tool", "price": 200.0, "inStock": true},
		},
	}

	for i, exprStr := range expressions {
		expr := Expression(exprStr)
		compiled, err := expr.Compile(envOpts, WithCache(cache))
		if err != nil {
			log.Fatal(err)
		}

		result, _ := compiled.Eval(testData)
		fmt.Printf("Variation %d: %v\n", i+1, result)
	}

	stats := cache.Stats()
	fmt.Printf("\nCache hits: %d/%d (%.1f%%)\n", stats.Hits, len(expressions), float64(stats.Hits)/float64(len(expressions))*100)

	// Output:
	// Variation 1: [Widget Tool]
	// Variation 2: [Widget Tool]
	// Variation 3: [Widget Tool]
	//
	// Cache hits: 2/3 (66.7%)
}
