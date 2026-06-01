// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package arrays

import (
	"encoding/json"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func evalGroupBy(t *testing.T, env *cel.Env, expression string) map[string]any {
	t.Helper()

	ast, issues := env.Compile(expression)
	require.Nil(t, issues, "compilation failed: %v", issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{})
	require.NoError(t, err)

	// Round-trip through JSON for stable comparison
	b, err := json.Marshal(result.Value())
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))

	return got
}

func TestGroupBy_CELIntegration(t *testing.T) {
	groupByFunc := GroupByFunc()

	env, err := cel.NewEnv(groupByFunc.EnvOptions...)
	require.NoError(t, err)

	t.Run("group by category", func(t *testing.T) {
		got := evalGroupBy(t, env, `arrays.groupBy([{"name": "a", "cat": "x"}, {"name": "b", "cat": "x"}, {"name": "c", "cat": "y"}], "cat")`)
		assert.Len(t, got, 2)
		assert.Len(t, got["x"], 2)
		assert.Len(t, got["y"], 1)
	})

	t.Run("single group", func(t *testing.T) {
		got := evalGroupBy(t, env, `arrays.groupBy([{"k": "a"}, {"k": "a"}], "k")`)
		assert.Len(t, got, 1)
		assert.Len(t, got["a"], 2)
	})

	t.Run("each item in its own group", func(t *testing.T) {
		got := evalGroupBy(t, env, `arrays.groupBy([{"k": "a"}, {"k": "b"}, {"k": "c"}], "k")`)
		assert.Len(t, got, 3)
	})

	t.Run("empty list", func(t *testing.T) {
		got := evalGroupBy(t, env, `arrays.groupBy([], "key")`)
		assert.Empty(t, got)
	})

	t.Run("extra fields preserved", func(t *testing.T) {
		got := evalGroupBy(t, env, `arrays.groupBy([{"env": "dev", "region": "us", "id": 1}, {"env": "prod", "region": "us", "id": 2}], "region")`)
		assert.Len(t, got, 1)
		items := got["us"].([]any)
		assert.Len(t, items, 2)
		first := items[0].(map[string]any)
		assert.Equal(t, "dev", first["env"])
	})
}

func TestGroupBy_Errors(t *testing.T) {
	groupByFunc := GroupByFunc()

	env, err := cel.NewEnv(groupByFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		errContain string
	}{
		{
			name:       "missing key field",
			expression: `arrays.groupBy([{"name": "a"}], "missing")`,
			errContain: "missing key field",
		},
		{
			name:       "non-string key value",
			expression: `arrays.groupBy([{"k": 123}], "k")`,
			errContain: "must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues)

			prog, err := env.Program(ast)
			require.NoError(t, err)

			_, _, err = prog.Eval(map[string]any{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContain)
		})
	}
}

func TestGroupBy_Metadata(t *testing.T) {
	f := GroupByFunc()
	assert.Equal(t, "arrays.groupBy", f.Name)
	assert.True(t, f.Custom)
	assert.NotEmpty(t, f.Description)
	assert.NotEmpty(t, f.Examples)
	assert.NotEmpty(t, f.EnvOptions)
}

func BenchmarkGroupBy(b *testing.B) {
	groupByFunc := GroupByFunc()

	env, err := cel.NewEnv(groupByFunc.EnvOptions...)
	require.NoError(b, err)

	ast, issues := env.Compile(`arrays.groupBy([{"k":"a","v":1},{"k":"b","v":2},{"k":"a","v":3},{"k":"c","v":4},{"k":"b","v":5}], "k")`)
	require.Nil(b, issues)

	prog, err := env.Program(ast)
	require.NoError(b, err)

	vars := map[string]any{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _, _ = prog.Eval(vars)
	}
}
