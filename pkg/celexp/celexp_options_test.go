package celexp

import (
	"context"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompile_NewAPI tests the new functional options API
func TestCompile_NewAPI(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		envOpts    []cel.EnvOption
		opts       []Option
		wantErr    bool
	}{
		{
			name:       "simple with no options",
			expression: "1 + 2",
			envOpts:    []cel.EnvOption{},
			opts:       nil,
			wantErr:    false,
		},
		{
			name:       "with variable",
			expression: "x * 2",
			envOpts:    []cel.EnvOption{cel.Variable("x", cel.IntType)},
			opts:       nil,
			wantErr:    false,
		},
		{
			name:       "with context option",
			expression: "x + y",
			envOpts:    []cel.EnvOption{cel.Variable("x", cel.IntType), cel.Variable("y", cel.IntType)},
			opts:       []Option{WithContext(context.Background())},
			wantErr:    false,
		},
		{
			name:       "with cost limit option",
			expression: "x + 1",
			envOpts:    []cel.EnvOption{cel.Variable("x", cel.IntType)},
			opts:       []Option{WithCostLimit(50000)},
			wantErr:    false,
		},
		{
			name:       "with no cost limit option",
			expression: "x + 1",
			envOpts:    []cel.EnvOption{cel.Variable("x", cel.IntType)},
			opts:       []Option{WithNoCostLimit()},
			wantErr:    false,
		},
		{
			name:       "with custom cache option",
			expression: "x + 1",
			envOpts:    []cel.EnvOption{cel.Variable("x", cel.IntType)},
			opts:       []Option{WithCache(NewProgramCache(10))},
			wantErr:    false,
		},
		{
			name:       "with multiple options",
			expression: "x + y",
			envOpts:    []cel.EnvOption{cel.Variable("x", cel.IntType), cel.Variable("y", cel.IntType)},
			opts: []Option{
				WithContext(context.Background()),
				WithCostLimit(100000),
				WithCache(NewProgramCache(10)),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expression)
			result, err := expr.Compile(tt.envOpts, tt.opts...)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.NotNil(t, result.Program)
				assert.Equal(t, expr, result.Expression)
			}
		})
	}
}

// TestCompile_WithContextOption tests context cancellation via WithContext option
func TestCompile_WithContextOption(t *testing.T) {
	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		expr := Expression("x + 1")
		_, err := expr.Compile(
			[]cel.EnvOption{cel.Variable("x", cel.IntType)},
			WithContext(ctx),
		)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("timeout context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		expr := Expression("x + 1")
		result, err := expr.Compile(
			[]cel.EnvOption{cel.Variable("x", cel.IntType)},
			WithContext(ctx),
		)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
}

// TestCompile_WithCache tests custom cache usage
func TestCompile_WithCache(t *testing.T) {
	cache := NewProgramCache(10)
	expr := Expression("x + 1")
	envOpts := []cel.EnvOption{cel.Variable("x", cel.IntType)}

	// First compilation - cache miss
	result1, err := expr.Compile(envOpts, WithCache(cache))
	require.NoError(t, err)
	assert.NotNil(t, result1)

	stats := cache.Stats()
	assert.Equal(t, 1, stats.Size)
	assert.Equal(t, uint64(0), stats.Hits)
	assert.Equal(t, uint64(1), stats.Misses)

	// Second compilation - cache hit
	result2, err := expr.Compile(envOpts, WithCache(cache))
	require.NoError(t, err)
	assert.NotNil(t, result2)

	stats = cache.Stats()
	assert.Equal(t, 1, stats.Size)
	assert.Equal(t, uint64(1), stats.Hits)
	assert.Equal(t, uint64(1), stats.Misses)
}

// TestCompile_WithCostLimit tests cost limit configuration
func TestCompile_WithCostLimit(t *testing.T) {
	t.Run("custom cost limit", func(t *testing.T) {
		expr := Expression("x * 2")
		result, err := expr.Compile(
			[]cel.EnvOption{cel.Variable("x", cel.IntType)},
			WithCostLimit(1000),
		)
		require.NoError(t, err)
		assert.NotNil(t, result)

		// Evaluation should respect cost limit
		_, err = result.Eval(map[string]any{"x": int64(10)})
		assert.NoError(t, err)
	})

	t.Run("no cost limit", func(t *testing.T) {
		expr := Expression("[1,2,3,4,5].map(x, x * 2)")
		result, err := expr.Compile(
			[]cel.EnvOption{},
			WithNoCostLimit(),
		)
		require.NoError(t, err)
		assert.NotNil(t, result)

		// Should work without cost limiting
		_, err = result.Eval(nil)
		assert.NoError(t, err)
	})
}

// TestCompile_OptionCombinations tests various option combinations
func TestCompile_OptionCombinations(t *testing.T) {
	expr := Expression("x + y")
	envOpts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}

	tests := []struct {
		name string
		opts []Option
	}{
		{
			name: "context only",
			opts: []Option{WithContext(context.Background())},
		},
		{
			name: "cache only",
			opts: []Option{WithCache(NewProgramCache(10))},
		},
		{
			name: "cost limit only",
			opts: []Option{WithCostLimit(50000)},
		},
		{
			name: "context and cache",
			opts: []Option{
				WithContext(context.Background()),
				WithCache(NewProgramCache(10)),
			},
		},
		{
			name: "context and cost limit",
			opts: []Option{
				WithContext(context.Background()),
				WithCostLimit(50000),
			},
		},
		{
			name: "cache and cost limit",
			opts: []Option{
				WithCache(NewProgramCache(10)),
				WithCostLimit(50000),
			},
		},
		{
			name: "all options",
			opts: []Option{
				WithContext(context.Background()),
				WithCache(NewProgramCache(10)),
				WithCostLimit(50000),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := expr.Compile(envOpts, tt.opts...)
			assert.NoError(t, err)
			assert.NotNil(t, result)

			// Verify it can evaluate
			val, err := result.Eval(map[string]any{"x": int64(10), "y": int64(20)})
			assert.NoError(t, err)
			assert.Equal(t, int64(30), val)
		})
	}
}

// TestCompile_OptionOrder tests that option order doesn't matter
func TestCompile_OptionOrder(t *testing.T) {
	expr := Expression("x * 2")
	envOpts := []cel.EnvOption{cel.Variable("x", cel.IntType)}
	ctx := context.Background()
	cache := NewProgramCache(10)

	// Try different orders
	orders := [][]Option{
		{WithContext(ctx), WithCache(cache), WithCostLimit(50000)},
		{WithCache(cache), WithCostLimit(50000), WithContext(ctx)},
		{WithCostLimit(50000), WithContext(ctx), WithCache(cache)},
	}

	for i, opts := range orders {
		t.Run("order_"+string(rune('A'+i)), func(t *testing.T) {
			result, err := expr.Compile(envOpts, opts...)
			assert.NoError(t, err)
			assert.NotNil(t, result)

			val, err := result.Eval(map[string]any{"x": int64(5)})
			assert.NoError(t, err)
			assert.Equal(t, int64(10), val)
		})
	}
}
