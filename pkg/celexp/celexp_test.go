// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompile(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		opts       []cel.EnvOption
		wantErr    bool
	}{
		{
			name:       "simple arithmetic",
			expression: "1 + 2",
			opts:       []cel.EnvOption{},
			wantErr:    false,
		},
		{
			name:       "with variable",
			expression: "x * 2",
			opts:       []cel.EnvOption{cel.Variable("x", cel.IntType)},
			wantErr:    false,
		},
		{
			name:       "string concatenation",
			expression: `"hello " + name`,
			opts:       []cel.EnvOption{cel.Variable("name", cel.StringType)},
			wantErr:    false,
		},
		{
			name:       "syntax error",
			expression: "1 +",
			opts:       []cel.EnvOption{},
			wantErr:    true,
		},
		{
			name:       "undefined variable",
			expression: "unknown + 1",
			opts:       []cel.EnvOption{},
			wantErr:    true,
		},
		{
			name:       "type mismatch",
			expression: "x + y",
			opts:       []cel.EnvOption{cel.Variable("x", cel.IntType), cel.Variable("y", cel.StringType)},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expression)
			result, err := expr.Compile(tt.opts)
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

func TestCompile_ComplexExpressions(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		opts       []cel.EnvOption
	}{
		{
			name:       "conditional expression",
			expression: "x > 10 ? 'big' : 'small'",
			opts:       []cel.EnvOption{cel.Variable("x", cel.IntType)},
		},
		{
			name:       "list operations",
			expression: "[1, 2, 3].map(x, x * 2)",
			opts:       []cel.EnvOption{},
		},
		{
			name:       "map access",
			expression: "data['key']",
			opts:       []cel.EnvOption{cel.Variable("data", cel.MapType(cel.StringType, cel.StringType))},
		},
		{
			name:       "nested expressions",
			expression: "(a + b) * (c - d)",
			opts: []cel.EnvOption{
				cel.Variable("a", cel.IntType),
				cel.Variable("b", cel.IntType),
				cel.Variable("c", cel.IntType),
				cel.Variable("d", cel.IntType),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expression)
			result, err := expr.Compile(tt.opts)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, result.Program)
		})
	}
}

func TestEval(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		opts       []cel.EnvOption
		vars       map[string]any
		want       any
		wantErr    bool
	}{
		{
			name:       "simple arithmetic",
			expression: "1 + 2",
			opts:       []cel.EnvOption{},
			vars:       map[string]any{},
			want:       int64(3),
			wantErr:    false,
		},
		{
			name:       "with variable",
			expression: "x * 2",
			opts:       []cel.EnvOption{cel.Variable("x", cel.IntType)},
			vars:       map[string]any{"x": int64(5)},
			want:       int64(10),
			wantErr:    false,
		},
		{
			name:       "string concatenation",
			expression: `"hello " + name`,
			opts:       []cel.EnvOption{cel.Variable("name", cel.StringType)},
			vars:       map[string]any{"name": "world"},
			want:       "hello world",
			wantErr:    false,
		},
		{
			name:       "boolean expression",
			expression: "x > 5",
			opts:       []cel.EnvOption{cel.Variable("x", cel.IntType)},
			vars:       map[string]any{"x": int64(10)},
			want:       true,
			wantErr:    false,
		},
		{
			name:       "conditional expression",
			expression: "x > 10 ? 'big' : 'small'",
			opts:       []cel.EnvOption{cel.Variable("x", cel.IntType)},
			vars:       map[string]any{"x": int64(15)},
			want:       "big",
			wantErr:    false,
		},
		{
			name:       "missing variable",
			expression: "x + y",
			opts:       []cel.EnvOption{cel.Variable("x", cel.IntType), cel.Variable("y", cel.IntType)},
			vars:       map[string]any{"x": int64(5)}, // missing y
			want:       nil,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expression)
			compiled, err := expr.Compile(tt.opts)
			require.NoError(t, err)

			result, err := compiled.Eval(tt.vars)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, result)
			}
		})
	}
}

func TestEval_ComplexTypes(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		opts       []cel.EnvOption
		vars       map[string]any
	}{
		{
			name:       "list creation",
			expression: "[1, 2, 3]",
			opts:       []cel.EnvOption{},
			vars:       map[string]any{},
		},
		{
			name:       "list filter",
			expression: "[1, 2, 3, 4, 5].filter(x, x > 2)",
			opts:       []cel.EnvOption{},
			vars:       map[string]any{},
		},
		{
			name:       "map creation",
			expression: `{"key": "value", "number": 42}`,
			opts:       []cel.EnvOption{},
			vars:       map[string]any{},
		},
		{
			name:       "map access",
			expression: "data['name']",
			opts:       []cel.EnvOption{cel.Variable("data", cel.MapType(cel.StringType, cel.StringType))},
			vars:       map[string]any{"data": map[string]any{"name": "test", "age": int64(30)}},
		},
		{
			name:       "list size",
			expression: "[1, 2, 3].size()",
			opts:       []cel.EnvOption{},
			vars:       map[string]any{},
		},
		{
			name:       "string length",
			expression: `"hello".size()`,
			opts:       []cel.EnvOption{},
			vars:       map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expression)
			compiled, err := expr.Compile(tt.opts)
			require.NoError(t, err)

			result, err := compiled.Eval(tt.vars)
			require.NoError(t, err)
			assert.NotNil(t, result, "result should not be nil")
		})
	}
}

func TestCompileAndEval_Integration(t *testing.T) {
	// Test that we can compile once and evaluate multiple times
	expr := Expression("x * multiplier")
	compiled, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType), cel.Variable("multiplier", cel.IntType)})
	require.NoError(t, err)

	testCases := []struct {
		x          int64
		multiplier int64
		want       int64
	}{
		{5, 2, 10},
		{10, 3, 30},
		{7, 7, 49},
		{0, 100, 0},
	}

	for _, tc := range testCases {
		result, err := compiled.Eval(map[string]any{"x": tc.x, "multiplier": tc.multiplier})
		require.NoError(t, err)
		assert.Equal(t, tc.want, result)
	}
}

func TestEval_NilResult(t *testing.T) {
	// Test that passing nil result doesn't panic
	var result *CompileResult
	value, err := result.Eval(map[string]any{})
	assert.Error(t, err)
	assert.Nil(t, value)
	assert.Contains(t, err.Error(), "compile result or program is nil")
}

func BenchmarkCompile(b *testing.B) {
	expr := Expression("x * 2 + y * 3")
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = expr.Compile(opts)
	}
}

func BenchmarkEval(b *testing.B) {
	expr := Expression("x * 2 + y * 3")
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}
	compiled, _ := expr.Compile(opts)
	vars := map[string]any{"x": int64(5), "y": int64(10)}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compiled.Eval(vars)
	}
}

func BenchmarkCompileAndEval(b *testing.B) {
	expr := Expression("x * 2 + y * 3")
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}
	vars := map[string]any{"x": int64(5), "y": int64(10)}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compiled, _ := expr.Compile(opts)
		_, _ = compiled.Eval(vars)
	}
}

func TestSetDefaultCostLimit(t *testing.T) {
	orig := GetDefaultCostLimit()
	defer defaultCostLimit.Store(orig)

	SetDefaultCostLimit(999999)
	assert.Equal(t, uint64(999999), GetDefaultCostLimit())

	SetDefaultCostLimit(0)
	assert.Equal(t, uint64(0), GetDefaultCostLimit())
}

func TestExtFunction_GetName(t *testing.T) {
	f := ExtFunction{Name: "myFunc"}
	assert.Equal(t, "myFunc", f.GetName())
}

func TestExtFunction_GetSubNames(t *testing.T) {
	f := ExtFunction{FunctionNames: []string{"base64.encode", "base64.decode"}}
	assert.Equal(t, []string{"base64.encode", "base64.decode"}, f.GetSubNames())

	empty := ExtFunction{}
	assert.Nil(t, empty.GetSubNames())
}

func TestInitFromAppConfig(t *testing.T) {
	ResetForTesting()
	defer ResetForTesting()

	ctx := context.Background()
	cfg := CELConfigInput{
		CacheSize:          100,
		CostLimit:          500000,
		UseASTBasedCaching: true,
		EnableMetrics:      false,
	}
	InitFromAppConfig(ctx, cfg)

	cache := GetAppConfigCache()
	assert.NotNil(t, cache)

	// Calling again should be a no-op (idempotent)
	InitFromAppConfig(ctx, CELConfigInput{CacheSize: 999})
	cache2 := GetAppConfigCache()
	assert.Equal(t, cache, cache2)
}

func TestInitFromAppConfig_DisabledCostLimit(t *testing.T) {
	ResetForTesting()
	defer ResetForTesting()

	ctx := context.Background()
	cfg := CELConfigInput{
		CacheSize: 50,
		CostLimit: 0, // explicitly disable
	}
	InitFromAppConfig(ctx, cfg)
	assert.NotNil(t, GetAppConfigCache())
}

func TestResetForTesting(t *testing.T) {
	ctx := context.Background()
	InitFromAppConfig(ctx, CELConfigInput{CacheSize: 10})
	assert.NotNil(t, GetAppConfigCache())

	ResetForTesting()
	assert.Nil(t, GetAppConfigCache())
}

func TestSetEnvFactory(t *testing.T) {
	// Save and restore original state
	origFactory := envFactory
	origInitialized := envFactoryInitialized
	defer func() {
		envFactoryMu.Lock()
		envFactory = origFactory
		envFactoryInitialized = origInitialized
		envFactoryMu.Unlock()
	}()

	// Reset so SetEnvFactory will accept the new factory
	envFactoryMu.Lock()
	envFactory = nil
	envFactoryInitialized = false
	envFactoryMu.Unlock()

	called := false
	factory := func(ctx context.Context, opts ...cel.EnvOption) (*cel.Env, error) {
		called = true
		return cel.NewEnv(opts...)
	}
	SetEnvFactory(factory)

	// Verify the factory was set
	got := getEnvFactory()
	assert.NotNil(t, got)
	// Call it to verify it works
	_, err := got(context.Background())
	assert.NoError(t, err)
	assert.True(t, called)

	// Calling SetEnvFactory again should be a no-op (only-once semantics)
	called2 := false
	SetEnvFactory(func(ctx context.Context, opts ...cel.EnvOption) (*cel.Env, error) {
		called2 = true
		return cel.NewEnv(opts...)
	})
	assert.False(t, called2, "second SetEnvFactory call should be ignored")
}

func TestSetCacheFactory(t *testing.T) {
	// Reset state so we can set it fresh
	ResetForTesting()
	defer ResetForTesting()

	myCache := NewProgramCache(10)
	factory := func() *ProgramCache { return myCache }

	SetCacheFactory(factory)
	got := getCacheFactory()
	assert.NotNil(t, got)
	assert.Equal(t, myCache, got())

	// Second call should be a no-op
	otherCache := NewProgramCache(5)
	SetCacheFactory(func() *ProgramCache { return otherCache })
	got2 := getCacheFactory()
	assert.Equal(t, myCache, got2(), "second SetCacheFactory should be ignored")
}

func TestFormatCelType_Nil(t *testing.T) {
	result := formatCelType(nil)
	assert.Equal(t, "any", result)
}

func TestFormatCelType_KnownTypes(t *testing.T) {
	tests := []struct {
		t    *cel.Type
		want string
	}{
		{cel.IntType, "int"},
		{cel.UintType, "uint"},
		{cel.DoubleType, "double"},
		{cel.BoolType, "bool"},
		{cel.StringType, "string"},
		{cel.BytesType, "bytes"},
		{cel.NullType, "null"},
		{cel.TypeType, "type"},
		{cel.ListType(cel.StringType), "list(string)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, formatCelType(tt.t))
		})
	}
}
