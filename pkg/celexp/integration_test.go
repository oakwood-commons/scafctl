// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_MultiStepWorkflow tests a complete workflow with multiple CEL expressions.
func TestIntegration_MultiStepWorkflow(t *testing.T) {
	cache := NewProgramCache(50)

	// Step 1: User validation
	validateUserExpr := Expression("user.age >= 18 && has(user.email) && user.email.contains('@')")
	validateUser, err := validateUserExpr.Compile(
		[]cel.EnvOption{
			cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
		},
		WithCache(cache),
	)
	require.NoError(t, err)

	userData := map[string]any{
		"user": map[string]any{
			"age":   int64(25),
			"email": "user@example.com",
			"name":  "John Doe",
		},
	}

	isValid, err := validateUser.Eval(userData)
	require.NoError(t, err)
	assert.True(t, isValid.(bool), "user should be valid")

	// Step 2: Calculate discount based on user attributes
	discountExpr := Expression("user.age < 25 ? 0.10 : user.age < 65 ? 0.05 : 0.15")
	calculateDiscount, err := discountExpr.Compile(
		[]cel.EnvOption{
			cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
		},
		WithCache(cache),
	)
	require.NoError(t, err)

	discount, err := calculateDiscount.Eval(userData)
	require.NoError(t, err)
	assert.Equal(t, 0.05, discount.(float64))

	// Step 3: Apply discount to cart total
	applyDiscountExpr := Expression("cartTotal * (1.0 - discount)")
	applyDiscount, err := applyDiscountExpr.Compile(
		[]cel.EnvOption{
			cel.Variable("cartTotal", cel.DoubleType),
			cel.Variable("discount", cel.DoubleType),
		},
		WithCache(cache),
	)
	require.NoError(t, err)

	finalPrice, err := applyDiscount.Eval(map[string]any{
		"cartTotal": 100.0,
		"discount":  discount,
	})
	require.NoError(t, err)
	assert.Equal(t, 95.0, finalPrice.(float64))

	// Verify cache efficiency
	stats := cache.Stats()
	assert.Greater(t, stats.Misses, uint64(0), "should have cache misses for first compilations")
}

// TestIntegration_ConcurrentEvaluation tests concurrent evaluation with shared cache.
func TestIntegration_ConcurrentEvaluation(t *testing.T) {
	cache := NewProgramCache(100, WithASTBasedCaching(true))
	expr := Expression("x * y + z")

	compiled, err := expr.Compile(
		[]cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
			cel.Variable("z", cel.IntType),
		},
		WithCache(cache),
	)
	require.NoError(t, err)

	// Perform 100 concurrent evaluations
	const numGoroutines = 100
	var wg sync.WaitGroup
	results := make([]any, numGoroutines)
	errors := make([]error, numGoroutines)

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()

			result, err := compiled.Eval(map[string]any{
				"x": int64(index),
				"y": int64(2),
				"z": int64(10),
			})

			results[index] = result
			errors[index] = err
		}(i)
	}

	wg.Wait()

	// Verify all evaluations succeeded
	for i := 0; i < numGoroutines; i++ {
		assert.NoError(t, errors[i], "evaluation %d should succeed", i)
		expected := int64(i*2 + 10)
		assert.Equal(t, expected, results[i], "evaluation %d result mismatch", i)
	}
}

// TestIntegration_ConcurrentCompilation tests concurrent compilation with shared cache.
func TestIntegration_ConcurrentCompilation(t *testing.T) {
	cache := NewProgramCache(100)

	const numGoroutines = 50
	const numExpressions = 10
	var wg sync.WaitGroup

	// Use same expressions across goroutines to test cache contention
	expressions := make([]string, numExpressions)
	for i := 0; i < numExpressions; i++ {
		expressions[i] = fmt.Sprintf("x + %d", i)
	}

	errors := make(chan error, numGoroutines*numExpressions)

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			for _, exprStr := range expressions {
				expr := Expression(exprStr)
				_, err := expr.Compile(
					[]cel.EnvOption{
						cel.Variable("x", cel.IntType),
					},
					WithCache(cache),
				)
				if err != nil {
					errors <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Verify no errors occurred
	errorCount := 0
	for err := range errors {
		t.Errorf("Compilation error: %v", err)
		errorCount++
	}
	assert.Equal(t, 0, errorCount, "no compilation errors should occur")

	// Verify cache efficiency
	stats := cache.Stats()
	expectedMisses := uint64(numExpressions)
	// Due to concurrent access and race conditions, we might have more misses.
	// In the worst case every goroutine can miss on every expression before the
	// cache is populated, so the upper bound is numGoroutines * numExpressions.
	// We use numExpressions * numGoroutines as the ceiling to avoid flaky failures.
	assert.LessOrEqual(t, expectedMisses, stats.Misses, "should have at least %d misses", numExpressions)
	assert.LessOrEqual(t, stats.Misses, uint64(numGoroutines*numExpressions), "misses should not exceed total compilations")
	assert.Greater(t, stats.Hits, uint64(0), "should have cache hits from concurrent access")
}

// TestIntegration_CacheWarming tests warming up a cache with common expressions.
func TestIntegration_CacheWarming(t *testing.T) {
	cache := NewProgramCache(100, WithASTBasedCaching(true))

	// Warm up cache with common expressions
	commonExpressions := []string{
		"user.age >= 18",
		"user.verified == true",
		"items.size() > 0",
		"price * quantity",
		"status == 'active'",
	}

	envOpts := []cel.EnvOption{
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("items", cel.ListType(cel.DynType)),
		cel.Variable("price", cel.DoubleType),
		cel.Variable("quantity", cel.DoubleType), // Changed from IntType
		cel.Variable("status", cel.StringType),
	}

	// Warm up phase
	for _, exprStr := range commonExpressions {
		expr := Expression(exprStr)
		_, err := expr.Compile(envOpts, WithCache(cache))
		require.NoError(t, err)
	}

	initialStats := cache.Stats()
	assert.Equal(t, uint64(len(commonExpressions)), initialStats.Misses)
	assert.Equal(t, uint64(0), initialStats.Hits)

	// Usage phase - all should be cache hits
	for _, exprStr := range commonExpressions {
		expr := Expression(exprStr)
		_, err := expr.Compile(envOpts, WithCache(cache))
		require.NoError(t, err)
	}

	finalStats := cache.Stats()
	assert.Equal(t, uint64(len(commonExpressions)), finalStats.Hits, "all expressions should hit cache")
	assert.Equal(t, initialStats.Misses, finalStats.Misses, "no additional misses")
}

// TestIntegration_ContextCancellation tests context cancellation during compilation.
func TestIntegration_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Start a goroutine that will cancel the context
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	// Attempt compilation with cancellable context
	expr := Expression("x + y")
	_, err := expr.Compile(
		[]cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		},
		WithContext(ctx),
	)

	// Note: CEL compilation may complete before cancellation
	// This test primarily ensures no panics occur
	_ = err // May or may not be cancelled depending on timing
}

// TestIntegration_ContextTimeout tests context timeout during compilation.
func TestIntegration_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	expr := Expression("x * y + z")
	compiled, err := expr.Compile(
		[]cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
			cel.Variable("z", cel.IntType),
		},
		WithContext(ctx),
	)

	require.NoError(t, err)

	result, err := compiled.Eval(map[string]any{
		"x": int64(10),
		"y": int64(20),
		"z": int64(5),
	})

	require.NoError(t, err)
	assert.Equal(t, int64(205), result)
}

// TestIntegration_MemoryPressure tests cache behavior under memory pressure.
func TestIntegration_MemoryPressure(t *testing.T) {
	// Small cache to force evictions
	cache := NewProgramCache(5)

	// Compile more expressions than cache capacity
	for i := 0; i < 20; i++ {
		expr := Expression(fmt.Sprintf("x + %d", i))
		_, err := expr.Compile(
			[]cel.EnvOption{
				cel.Variable("x", cel.IntType),
			},
			WithCache(cache),
		)
		require.NoError(t, err)
	}

	// Cache should have evicted entries
	stats := cache.Stats()
	assert.Equal(t, uint64(20), stats.Misses, "should have 20 misses (all unique)")
	assert.Equal(t, uint64(0), stats.Hits, "no cache hits due to evictions")
	assert.LessOrEqual(t, int(stats.Size), 5, "cache size should not exceed capacity")
}

// TestIntegration_LargeDataset tests evaluation with large datasets.
func TestIntegration_LargeDataset(t *testing.T) {
	expr := Expression("items.filter(x, x > threshold).size()")
	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("items", cel.ListType(cel.IntType)),
		cel.Variable("threshold", cel.IntType),
	})
	require.NoError(t, err)

	// Create large dataset
	largeList := make([]any, 10000)
	for i := 0; i < 10000; i++ {
		largeList[i] = int64(i)
	}

	result, err := compiled.Eval(map[string]any{
		"items":     largeList,
		"threshold": int64(5000),
	})

	require.NoError(t, err)
	assert.Equal(t, int64(4999), result.(int64)) // 5001 to 9999
}

// TestIntegration_ComplexNesting tests deeply nested expressions.
func TestIntegration_ComplexNesting(t *testing.T) {
	expr := Expression(`
		has(user.profile) && has(user.profile.settings) && has(user.profile.settings.notifications)
			? user.profile.settings.notifications.enabled
			: false
	`)

	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		data     map[string]any
		expected bool
	}{
		{
			name: "full_nested_structure",
			data: map[string]any{
				"user": map[string]any{
					"profile": map[string]any{
						"settings": map[string]any{
							"notifications": map[string]any{
								"enabled": true,
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "missing_nested_field",
			data: map[string]any{
				"user": map[string]any{
					"profile": map[string]any{},
				},
			},
			expected: false,
		},
		{
			name: "missing_user",
			data: map[string]any{
				"user": map[string]any{}, // Empty user object
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compiled.Eval(tt.data)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.(bool))
		})
	}
}

// TestIntegration_ValidationPipeline tests a complete validation pipeline.
func TestIntegration_ValidationPipeline(t *testing.T) {
	cache := NewProgramCache(50)

	// Define validation rules
	rules := []struct {
		name string
		expr string
	}{
		{"email_valid", "has(user.email) && user.email.contains('@')"},
		{"age_valid", "has(user.age) && user.age >= 18 && user.age <= 120"},
		{"name_valid", "has(user.name) && user.name.size() > 0"},
		{"terms_accepted", "has(user.termsAccepted) && user.termsAccepted == true"},
	}

	envOpts := []cel.EnvOption{
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
	}

	// Compile all rules
	compiled := make([]*CompileResult, len(rules))
	for i, rule := range rules {
		expr := Expression(rule.expr)
		c, err := expr.Compile(envOpts, WithCache(cache))
		require.NoError(t, err, "rule %s should compile", rule.name)
		compiled[i] = c
	}

	// Test with valid user
	validUser := map[string]any{
		"user": map[string]any{
			"email":         "user@example.com",
			"age":           int64(25),
			"name":          "John Doe",
			"termsAccepted": true,
		},
	}

	for i, rule := range rules {
		result, err := compiled[i].Eval(validUser)
		require.NoError(t, err, "rule %s should evaluate", rule.name)
		assert.True(t, result.(bool), "rule %s should pass for valid user", rule.name)
	}

	// Test with invalid user
	invalidUser := map[string]any{
		"user": map[string]any{
			"email":         "invalid-email",
			"age":           int64(15),
			"name":          "",
			"termsAccepted": false,
		},
	}

	failedRules := 0
	for i, rule := range rules {
		result, err := compiled[i].Eval(invalidUser)
		require.NoError(t, err, "rule %s should evaluate", rule.name)
		if !result.(bool) {
			failedRules++
		}
	}

	assert.Equal(t, len(rules), failedRules, "all rules should fail for invalid user")
}

// TestIntegration_RealWorldScenario tests a real-world e-commerce scenario.
func TestIntegration_RealWorldScenario(t *testing.T) {
	cache := NewProgramCache(100, WithASTBasedCaching(true))

	// Scenario: Calculate shipping cost based on multiple factors
	shippingExpr := Expression(`
		items.size() == 0 ? 0.0 :
		orderTotal >= freeShippingThreshold ? 0.0 :
		isPremiumMember ? baseShippingCost * 0.5 :
		destination == 'domestic' ? baseShippingCost :
		baseShippingCost * 2.0
	`)

	compiled, err := shippingExpr.Compile(
		[]cel.EnvOption{
			cel.Variable("items", cel.ListType(cel.DynType)),
			cel.Variable("orderTotal", cel.DoubleType),
			cel.Variable("freeShippingThreshold", cel.DoubleType),
			cel.Variable("isPremiumMember", cel.BoolType),
			cel.Variable("baseShippingCost", cel.DoubleType),
			cel.Variable("destination", cel.StringType),
		},
		WithCache(cache),
	)
	require.NoError(t, err)

	tests := []struct {
		name     string
		vars     map[string]any
		expected float64
	}{
		{
			name: "empty_cart",
			vars: map[string]any{
				"items":                 []any{},
				"orderTotal":            0.0,
				"freeShippingThreshold": 50.0,
				"isPremiumMember":       false,
				"baseShippingCost":      10.0,
				"destination":           "domestic",
			},
			expected: 0.0,
		},
		{
			name: "free_shipping_qualified",
			vars: map[string]any{
				"items":                 []any{"item1", "item2"},
				"orderTotal":            75.0,
				"freeShippingThreshold": 50.0,
				"isPremiumMember":       false,
				"baseShippingCost":      10.0,
				"destination":           "domestic",
			},
			expected: 0.0,
		},
		{
			name: "premium_member_discount",
			vars: map[string]any{
				"items":                 []any{"item1"},
				"orderTotal":            25.0,
				"freeShippingThreshold": 50.0,
				"isPremiumMember":       true,
				"baseShippingCost":      10.0,
				"destination":           "domestic",
			},
			expected: 5.0,
		},
		{
			name: "international_shipping",
			vars: map[string]any{
				"items":                 []any{"item1"},
				"orderTotal":            25.0,
				"freeShippingThreshold": 50.0,
				"isPremiumMember":       false,
				"baseShippingCost":      10.0,
				"destination":           "international",
			},
			expected: 20.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compiled.Eval(tt.vars)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.(float64))
		})
	}
}
