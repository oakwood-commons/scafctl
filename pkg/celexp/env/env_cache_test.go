// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package env

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentCaching(t *testing.T) {
	t.Run("getBaseEnvOptions caches results", func(t *testing.T) {
		// Reset for clean test
		baseEnvOnce = sync.Once{}
		baseEnvOpts = nil
		baseEnvErr = nil

		ctx := context.Background()

		// First call
		opts1, err1 := getBaseEnvOptions(ctx)
		require.NoError(t, err1)
		require.NotNil(t, opts1)

		// Second call should return cached result (same slice)
		opts2, err2 := getBaseEnvOptions(ctx)
		require.NoError(t, err2)
		assert.Same(t, &opts1[0], &opts2[0], "should return same cached slice")
	})

	t.Run("New uses cached base options", func(t *testing.T) {
		// Reset for clean test
		baseEnvOnce = sync.Once{}
		baseEnvOpts = nil
		baseEnvErr = nil

		ctx := context.Background()

		// Create multiple environments - should reuse base options
		env1, err := New(ctx, cel.Variable("x", cel.IntType))
		require.NoError(t, err)
		require.NotNil(t, env1)

		env2, err := New(ctx, cel.Variable("y", cel.IntType))
		require.NoError(t, err)
		require.NotNil(t, env2)

		// Both should have access to extension functions
		// Test by compiling an expression using a custom extension
		ast1, issues := env1.Compile("guid.new()")
		require.Nil(t, issues)
		require.NotNil(t, ast1)

		ast2, issues := env2.Compile("guid.new()")
		require.Nil(t, issues)
		require.NotNil(t, ast2)
	})

	t.Run("concurrent New calls are safe", func(t *testing.T) {
		// Reset for clean test
		baseEnvOnce = sync.Once{}
		baseEnvOpts = nil
		baseEnvErr = nil

		ctx := context.Background()
		var wg sync.WaitGroup
		errors := make(chan error, 10)

		// Create multiple environments concurrently
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := New(ctx, cel.Variable("x", cel.IntType))
				if err != nil {
					errors <- err
				}
			}()
		}

		wg.Wait()
		close(errors)

		// No errors should occur
		for err := range errors {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestContextCancellation(t *testing.T) {
	t.Run("context cancellation before getBaseEnvOptions", func(t *testing.T) {
		// Reset for clean test
		baseEnvOnce = sync.Once{}
		baseEnvOpts = nil
		baseEnvErr = nil

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := getBaseEnvOptions(ctx)
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("context cancellation before New", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := New(ctx, cel.Variable("x", cel.IntType))
		assert.Error(t, err)
	})

	t.Run("context timeout during New", func(t *testing.T) {
		// Reset to force slow initialization
		baseEnvOnce = sync.Once{}
		baseEnvOpts = nil
		baseEnvErr = nil

		// Very short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		time.Sleep(2 * time.Millisecond) // Ensure timeout occurs

		_, err := New(ctx, cel.Variable("x", cel.IntType))
		// May or may not error depending on timing, but shouldn't panic
		if err != nil {
			assert.Contains(t, err.Error(), "context")
		}
	})

	t.Run("New respects context after cache initialization", func(t *testing.T) {
		// First call to initialize cache
		ctx1 := context.Background()
		env1, err := New(ctx1, cel.Variable("x", cel.IntType))
		require.NoError(t, err)
		require.NotNil(t, env1)

		// Second call with cancelled context
		ctx2, cancel := context.WithCancel(context.Background())
		cancel()

		// Should still check context even though cache is initialized
		_, err = New(ctx2, cel.Variable("y", cel.IntType))
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}

func TestNewEnvironmentWithExtensions(t *testing.T) {
	t.Run("environment has all extensions loaded", func(t *testing.T) {
		ctx := context.Background()
		env, err := New(ctx)
		require.NoError(t, err)

		// Test various extension functions
		tests := []struct {
			name string
			expr string
		}{
			{"guid", "guid.new()"},
			{"strings", "strings.clean('Hello-World')"},
			{"debug", "debug.sleep(1)"},
			{"filepath", "filepath.normalize('/a//b/c')"},
			{"map", "map.add({}, 'key', 'value')"},
			{"arrays", "arrays.strings.unique(['a', 'b', 'a'])"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ast, issues := env.Compile(tt.expr)
				if issues != nil && issues.Err() != nil {
					t.Errorf("failed to compile %s: %v", tt.expr, issues.Err())
				}
				assert.NotNil(t, ast)
			})
		}
	})

	t.Run("environment accepts additional declarations", func(t *testing.T) {
		ctx := context.Background()
		env, err := New(ctx,
			cel.Variable("myVar", cel.StringType),
			cel.Variable("myInt", cel.IntType),
		)
		require.NoError(t, err)

		// Should be able to use declared variables
		ast, issues := env.Compile("myVar + string(myInt)")
		require.Nil(t, issues)
		require.NotNil(t, ast)

		prog, err := env.Program(ast)
		require.NoError(t, err)

		result, _, err := prog.Eval(map[string]any{
			"myVar": "test",
			"myInt": int64(123),
		})
		require.NoError(t, err)
		assert.Equal(t, "test123", result.Value())
	})
}

func BenchmarkNewWithCache(b *testing.B) {
	// Reset to ensure cache is built
	baseEnvOnce = sync.Once{}
	baseEnvOpts = nil
	baseEnvErr = nil

	ctx := context.Background()

	// Prime the cache
	_, _ = New(ctx, cel.Variable("x", cel.IntType))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = New(ctx, cel.Variable("y", cel.IntType))
	}
}

func BenchmarkNewConcurrent(b *testing.B) {
	// Reset
	baseEnvOnce = sync.Once{}
	baseEnvOpts = nil
	baseEnvErr = nil

	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = New(ctx, cel.Variable("x", cel.IntType))
		}
	})
}
