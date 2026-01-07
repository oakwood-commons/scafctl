package celexp_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// ExampleProduction_withValidation demonstrates production-ready pattern with validation.
func Example_production_withValidation() {
	// Define expression
	expr := celexp.Expression("user.age >= minAge && user.verified == true")

	// Compile with explicit variable declarations
	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("minAge", cel.IntType),
	}, celexp.WithCostLimit(10000)) // Prevent DoS
	if err != nil {
		log.Fatalf("Compilation failed: %v", err)
	}

	// Prepare evaluation data
	vars := map[string]any{
		"user": map[string]any{
			"age":      int64(25),
			"verified": true,
		},
		"minAge": int64(18),
	}

	// Validate before evaluation
	if err := compiled.ValidateVars(vars); err != nil {
		log.Fatalf("Validation failed: %v", err)
	}

	// Evaluate
	result, err := compiled.Eval(vars)
	if err != nil {
		log.Fatalf("Evaluation failed: %v", err)
	}

	fmt.Printf("User eligible: %v\n", result)
	// Output:
	// User eligible: true
}

// ExampleProduction_withTimeout demonstrates using context timeouts.
func Example_production_withTimeout() {
	expr := celexp.Expression("x * y + z")

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	compiled, err := expr.Compile(
		[]cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
			cel.Variable("z", cel.IntType),
		},
		celexp.WithContext(ctx),
		celexp.WithCostLimit(50000),
	)
	if err != nil {
		log.Fatalf("Compilation failed: %v", err)
	}

	result, err := compiled.Eval(map[string]any{
		"x": int64(10),
		"y": int64(20),
		"z": int64(5),
	})
	if err != nil {
		log.Fatalf("Evaluation failed: %v", err)
	}

	fmt.Printf("Result: %v\n", result)
	// Output:
	// Result: 205
}

// ExampleProduction_withCaching demonstrates production caching pattern.
func Example_production_withCaching() {
	// Create a global cache (typically singleton)
	cache := celexp.NewProgramCache(100, celexp.WithASTBasedCaching(true))

	// Simulate processing multiple requests with same expression
	expressions := []string{
		"user.age >= 18",
		"user.age >= 18", // Cache hit
		"user.age >= 18", // Cache hit
	}

	envOpts := []cel.EnvOption{
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
	}

	for i, exprStr := range expressions {
		expr := celexp.Expression(exprStr)
		compiled, err := expr.Compile(envOpts, celexp.WithCache(cache))
		if err != nil {
			log.Fatalf("Compilation failed: %v", err)
		}

		result, _ := compiled.Eval(map[string]any{
			"user": map[string]any{
				"age": int64(25),
			},
		})
		fmt.Printf("Request %d: %v\n", i+1, result)
	}

	// Check cache performance
	stats := cache.Stats()
	fmt.Printf("Cache hits: %d, misses: %d\n", stats.Hits, stats.Misses)

	// Output:
	// Request 1: true
	// Request 2: true
	// Request 3: true
	// Cache hits: 2, misses: 1
}

// ExampleProduction_errorHandling demonstrates comprehensive error handling.
func Example_production_errorHandling() {
	expr := celexp.Expression("double(items.size()) * price")

	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("items", cel.ListType(cel.StringType)),
		cel.Variable("price", cel.DoubleType),
	})
	if err != nil {
		// Handle compilation errors (syntax, type errors, etc.)
		log.Printf("❌ Compilation error: %v", err)
		return
	}

	vars := map[string]any{
		"items": []any{"apple", "banana", "orange"},
		"price": float64(1.50),
	}

	// Step 1: Validate types
	if err := compiled.ValidateVars(vars); err != nil {
		log.Printf("❌ Validation error: %v", err)
		return
	}

	// Step 2: Evaluate
	result, err := compiled.Eval(vars)
	if err != nil {
		log.Printf("❌ Evaluation error: %v", err)
		return
	}

	fmt.Printf("✅ Total: $%.2f\n", result)
	// Output:
	// ✅ Total: $4.50
}

// ExampleProduction_nullSafeAccess demonstrates safe access to potentially nil values.
func Example_production_nullSafeAccess() {
	// Use has() to check existence before accessing
	expr := celexp.Expression("has(user.profile) && has(user.profile.name) ? user.profile.name : 'Unknown'")

	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
	})
	if err != nil {
		log.Fatal(err)
	}

	// Case 1: Full profile exists
	result1, _ := compiled.Eval(map[string]any{
		"user": map[string]any{
			"profile": map[string]any{
				"name": "Alice",
			},
		},
	})
	fmt.Printf("With profile: %v\n", result1)

	// Case 2: No profile
	result2, _ := compiled.Eval(map[string]any{
		"user": map[string]any{},
	})
	fmt.Printf("Without profile: %v\n", result2)

	// Output:
	// With profile: Alice
	// Without profile: Unknown
}

// ExampleProduction_batchProcessing demonstrates efficient batch processing.
func Example_production_batchProcessing() {
	cache := celexp.NewProgramCache(10)
	expr := celexp.Expression("score >= threshold")

	compiled, err := expr.Compile(
		[]cel.EnvOption{
			cel.Variable("score", cel.IntType),
			cel.Variable("threshold", cel.IntType),
		},
		celexp.WithCache(cache),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Process batch of scores
	scores := []int64{85, 92, 78, 95, 88}
	threshold := int64(80)
	passed := 0

	for _, score := range scores {
		result, err := compiled.Eval(map[string]any{
			"score":     score,
			"threshold": threshold,
		})
		if err != nil {
			log.Printf("Error evaluating score %d: %v", score, err)
			continue
		}

		if result.(bool) {
			passed++
		}
	}

	fmt.Printf("Passed: %d/%d\n", passed, len(scores))
	// Output:
	// Passed: 4/5
}

// ExampleProduction_ruleEngine demonstrates a simple rule engine pattern.
func Example_production_ruleEngine() {
	cache := celexp.NewProgramCache(100)

	// Define rules
	rules := []struct {
		name string
		expr string
	}{
		{"age_check", "user.age >= 18"},
		{"country_check", "user.country == 'US'"},
		{"verified_check", "user.verified == true"},
	}

	envOpts := []cel.EnvOption{
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
	}

	userData := map[string]any{
		"user": map[string]any{
			"age":      int64(25),
			"country":  "US",
			"verified": true,
		},
	}

	// Evaluate all rules
	results := make(map[string]bool)
	for _, rule := range rules {
		expr := celexp.Expression(rule.expr)
		compiled, err := expr.Compile(envOpts, celexp.WithCache(cache))
		if err != nil {
			log.Printf("Rule %s failed to compile: %v", rule.name, err)
			continue
		}

		result, err := compiled.Eval(userData)
		if err != nil {
			log.Printf("Rule %s failed to evaluate: %v", rule.name, err)
			continue
		}

		results[rule.name] = result.(bool)
	}

	// Check if all rules passed
	allPassed := true
	for name, passed := range results {
		fmt.Printf("Rule '%s': %v\n", name, passed)
		if !passed {
			allPassed = false
		}
	}
	fmt.Printf("All rules passed: %v\n", allPassed)

	// Output:
	// Rule 'age_check': true
	// Rule 'country_check': true
	// Rule 'verified_check': true
	// All rules passed: true
}
