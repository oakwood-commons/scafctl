// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"testing"

	"github.com/google/cel-go/cel"
)

// BenchmarkCompile_CacheMiss tests compilation performance on cache misses
func BenchmarkCompile_CacheMiss(b *testing.B) {
	expr := Expression("x * 2 + y * 3")
	envOpts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create a new cache each time to force cache miss
		cache := NewProgramCache(10)
		_, err := expr.Compile(envOpts, WithContext(ctx), WithCache(cache))
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompile_CacheHit tests compilation performance on cache hits
func BenchmarkCompile_CacheHit(b *testing.B) {
	expr := Expression("x * 2 + y * 3")
	cache := NewProgramCache(10)
	envOpts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}
	ctx := context.Background()

	// Warm up the cache
	_, err := expr.Compile(envOpts, WithContext(ctx), WithCache(cache))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := expr.Compile(envOpts, WithContext(ctx), WithCache(cache))
		if err != nil {
			b.Fatal(err)
		}
	}
}
