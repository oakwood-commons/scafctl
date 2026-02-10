// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"testing"

	"github.com/google/cel-go/cel"
)

// BenchmarkCurrentCacheKeyGeneration benchmarks the current cache key generation approach.
func BenchmarkCurrentCacheKeyGeneration(b *testing.B) {
	expr := "x + y * z"
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
		cel.Variable("z", cel.IntType),
	}

	// Use cache with AST mode disabled (traditional approach)
	cache := NewProgramCache(10)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = generateCacheKeyWithAST(ctx, cache, expr, opts, 0)
	}
}

// BenchmarkASTBasedCacheKeyGeneration benchmarks the AST-based cache key generation.
func BenchmarkASTBasedCacheKeyGeneration(b *testing.B) {
	expr := "x + y * z"
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
		cel.Variable("z", cel.IntType),
	}

	// Pre-compile the AST (in real usage, this would come from the cache lookup)
	env, _ := cel.NewEnv(opts...)
	ast, _ := env.Compile(expr)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = generateNormalizedASTKey(ast)
	}
}

// BenchmarkASTBasedFullCompileAndKey benchmarks the full compilation + AST key generation.
func BenchmarkASTBasedFullCompileAndKey(b *testing.B) {
	expr := "x + y * z"
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
		cel.Variable("z", cel.IntType),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env, _ := cel.NewEnv(opts...)
		ast, _ := env.Compile(expr)
		_ = generateNormalizedASTKey(ast)
	}
}

// BenchmarkCacheKeyComparison compares both approaches side-by-side.
func BenchmarkCacheKeyComparison(b *testing.B) {
	expressions := []string{
		"x + y",
		"a * b / c",
		"user.name == 'Alice'",
		"items.size() > 0 && enabled == true",
	}

	b.Run("Current", func(b *testing.B) {
		cache := NewProgramCache(10)
		ctx := context.Background()
		for i := 0; i < b.N; i++ {
			for _, expr := range expressions {
				opts := []cel.EnvOption{
					cel.Variable("x", cel.IntType),
					cel.Variable("y", cel.IntType),
					cel.Variable("a", cel.IntType),
					cel.Variable("b", cel.IntType),
					cel.Variable("c", cel.IntType),
					cel.Variable("user", cel.MapType(cel.StringType, cel.StringType)),
					cel.Variable("items", cel.ListType(cel.IntType)),
					cel.Variable("enabled", cel.BoolType),
				}
				_ = generateCacheKeyWithAST(ctx, cache, expr, opts, 0)
			}
		}
	})

	b.Run("AST-Based", func(b *testing.B) {
		// Pre-compile all expressions
		compiled := make([]*cel.Ast, len(expressions))
		for i, expr := range expressions {
			opts := []cel.EnvOption{
				cel.Variable("x", cel.IntType),
				cel.Variable("y", cel.IntType),
				cel.Variable("a", cel.IntType),
				cel.Variable("b", cel.IntType),
				cel.Variable("c", cel.IntType),
				cel.Variable("user", cel.MapType(cel.StringType, cel.StringType)),
				cel.Variable("items", cel.ListType(cel.IntType)),
				cel.Variable("enabled", cel.BoolType),
			}
			env, _ := cel.NewEnv(opts...)
			compiled[i], _ = env.Compile(expr)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, ast := range compiled {
				_ = generateNormalizedASTKey(ast)
			}
		}
	})
}

// BenchmarkCacheHitScenario simulates realistic cache hit scenarios.
func BenchmarkCacheHitScenario(b *testing.B) {
	// Simulate a workload where 80% of requests use the same structural pattern
	// but with different variable names
	commonStructure := []string{
		"x + y",
		"a + b",
		"num1 + num2",
		"val1 + val2",
	}

	b.Run("Current-NoSharing", func(b *testing.B) {
		// Current approach: each expression is compiled separately
		cache := NewProgramCache(10)
		ctx := context.Background()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			expr := commonStructure[i%len(commonStructure)]
			opts := []cel.EnvOption{
				cel.Variable("x", cel.IntType),
				cel.Variable("y", cel.IntType),
				cel.Variable("a", cel.IntType),
				cel.Variable("b", cel.IntType),
				cel.Variable("num1", cel.IntType),
				cel.Variable("num2", cel.IntType),
				cel.Variable("val1", cel.IntType),
				cel.Variable("val2", cel.IntType),
			}
			result := generateCacheKeyWithAST(ctx, cache, expr, opts, 0)
			if result.err == nil && result.ast != nil {
				_, _ = result.env.Program(result.ast)
			}
		}
	})

	b.Run("AST-Based-WithSharing", func(b *testing.B) {
		// AST-based: expressions with same structure share compiled program
		// Simulate cache by compiling once and reusing
		opts := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
			cel.Variable("a", cel.IntType),
			cel.Variable("b", cel.IntType),
			cel.Variable("num1", cel.IntType),
			cel.Variable("num2", cel.IntType),
			cel.Variable("val1", cel.IntType),
			cel.Variable("val2", cel.IntType),
		}

		// Compile once (this would be cached)
		env, _ := cel.NewEnv(opts...)
		ast, _ := env.Compile("x + y")
		prog, _ := env.Program(ast)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// In AST-based caching, we just reuse the program
			// The only cost is variable binding at eval time
			_, _, _ = prog.Eval(map[string]interface{}{
				"x": int64(1),
				"y": int64(2),
			})
		}
	})
}
