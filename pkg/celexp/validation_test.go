// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"reflect"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileWithVarDecls(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		varDecls   []VarDecl
		wantError  bool
	}{
		{
			name:       "simple arithmetic with two int variables",
			expression: "x + y",
			varDecls: []VarDecl{
				NewVarDecl("x", cel.IntType),
				NewVarDecl("y", cel.IntType),
			},
			wantError: false,
		},
		{
			name:       "string concatenation",
			expression: "firstName + ' ' + lastName",
			varDecls: []VarDecl{
				NewVarDecl("firstName", cel.StringType),
				NewVarDecl("lastName", cel.StringType),
			},
			wantError: false,
		},
		{
			name:       "boolean expression",
			expression: "enabled && active",
			varDecls: []VarDecl{
				NewVarDecl("enabled", cel.BoolType),
				NewVarDecl("active", cel.BoolType),
			},
			wantError: false,
		},
		{
			name:       "list operations",
			expression: "items.size() > 0",
			varDecls: []VarDecl{
				NewVarDecl("items", cel.ListType(cel.StringType)),
			},
			wantError: false,
		},
		{
			name:       "map operations",
			expression: "config['key']",
			varDecls: []VarDecl{
				NewVarDecl("config", cel.MapType(cel.StringType, cel.AnyType)),
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expression)
			result, err := expr.CompileWithVarDecls(tt.varDecls)

			if tt.wantError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, result)
			assert.NotNil(t, result.Program)
			assert.Equal(t, expr, result.Expression)

			// Verify variable declarations were stored
			decls := result.GetDeclaredVariables()
			assert.Equal(t, len(tt.varDecls), len(decls))
			for _, vd := range tt.varDecls {
				assert.Contains(t, decls, vd.name)
			}
		})
	}
}

func TestValidateVars_Success(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		varDecls   []VarDecl
		vars       map[string]any
	}{
		{
			name:       "int variables",
			expression: "x + y",
			varDecls: []VarDecl{
				NewVarDecl("x", cel.IntType),
				NewVarDecl("y", cel.IntType),
			},
			vars: map[string]any{
				"x": int64(10),
				"y": int64(20),
			},
		},
		{
			name:       "string variables",
			expression: "name",
			varDecls: []VarDecl{
				NewVarDecl("name", cel.StringType),
			},
			vars: map[string]any{
				"name": "Alice",
			},
		},
		{
			name:       "bool variables",
			expression: "enabled",
			varDecls: []VarDecl{
				NewVarDecl("enabled", cel.BoolType),
			},
			vars: map[string]any{
				"enabled": true,
			},
		},
		{
			name:       "double variables",
			expression: "price * 1.1",
			varDecls: []VarDecl{
				NewVarDecl("price", cel.DoubleType),
			},
			vars: map[string]any{
				"price": 99.99,
			},
		},
		{
			name:       "list variables",
			expression: "items.size()",
			varDecls: []VarDecl{
				NewVarDecl("items", cel.ListType(cel.StringType)),
			},
			vars: map[string]any{
				"items": []string{"a", "b", "c"},
			},
		},
		{
			name:       "map variables",
			expression: "config['key']",
			varDecls: []VarDecl{
				NewVarDecl("config", cel.MapType(cel.StringType, cel.IntType)),
			},
			vars: map[string]any{
				"config": map[string]int{"key": 42},
			},
		},
		{
			name:       "mixed types",
			expression: "count > 0 && name != ''",
			varDecls: []VarDecl{
				NewVarDecl("count", cel.IntType),
				NewVarDecl("name", cel.StringType),
			},
			vars: map[string]any{
				"count": int64(5),
				"name":  "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expression)
			result, err := expr.CompileWithVarDecls(tt.varDecls)
			require.NoError(t, err)

			err = result.ValidateVars(tt.vars)
			assert.NoError(t, err)
		})
	}
}

func TestValidateVars_MissingVariable(t *testing.T) {
	expr := Expression("x + y")
	varDecls := []VarDecl{
		NewVarDecl("x", cel.IntType),
		NewVarDecl("y", cel.IntType),
	}

	result, err := expr.CompileWithVarDecls(varDecls)
	require.NoError(t, err)

	// Missing variable y
	err = result.ValidateVars(map[string]any{
		"x": int64(10),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing required variable")
	assert.Contains(t, err.Error(), "y")
}

func TestValidateVars_TypeMismatch(t *testing.T) {
	tests := []struct {
		name         string
		varDecls     []VarDecl
		vars         map[string]any
		expectedVar  string
		expectedType string
		actualType   string
	}{
		{
			name: "int expected, string provided",
			varDecls: []VarDecl{
				NewVarDecl("x", cel.IntType),
			},
			vars: map[string]any{
				"x": "string",
			},
			expectedVar:  "x",
			expectedType: "int",
			actualType:   "string",
		},
		{
			name: "string expected, int provided",
			varDecls: []VarDecl{
				NewVarDecl("name", cel.StringType),
			},
			vars: map[string]any{
				"name": int64(123),
			},
			expectedVar:  "name",
			expectedType: "string",
			actualType:   "int64",
		},
		{
			name: "bool expected, string provided",
			varDecls: []VarDecl{
				NewVarDecl("enabled", cel.BoolType),
			},
			vars: map[string]any{
				"enabled": "true",
			},
			expectedVar:  "enabled",
			expectedType: "bool",
			actualType:   "string",
		},
		{
			name: "double expected, int provided",
			varDecls: []VarDecl{
				NewVarDecl("price", cel.DoubleType),
			},
			vars: map[string]any{
				"price": int64(100),
			},
			expectedVar:  "price",
			expectedType: "double",
			actualType:   "int64",
		},
		{
			name: "list expected, string provided",
			varDecls: []VarDecl{
				NewVarDecl("items", cel.ListType(cel.StringType)),
			},
			vars: map[string]any{
				"items": "not a list",
			},
			expectedVar:  "items",
			expectedType: "list",
			actualType:   "string",
		},
		{
			name: "map expected, list provided",
			varDecls: []VarDecl{
				NewVarDecl("config", cel.MapType(cel.StringType, cel.AnyType)),
			},
			vars: map[string]any{
				"config": []string{"a", "b"},
			},
			expectedVar:  "config",
			expectedType: "map",
			actualType:   "[]string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use the actual variable names from varDecls in the expression
			var exprStr string
			if len(tt.varDecls) > 0 {
				exprStr = tt.varDecls[0].name
			} else {
				exprStr = "true"
			}
			expr := Expression(exprStr)
			result, err := expr.CompileWithVarDecls(tt.varDecls)
			require.NoError(t, err)

			err = result.ValidateVars(tt.vars)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedVar)
			assert.Contains(t, err.Error(), "expected")
			assert.Contains(t, err.Error(), tt.expectedType)
		})
	}
}

func TestValidateVars_NilValue(t *testing.T) {
	expr := Expression("x")
	varDecls := []VarDecl{
		NewVarDecl("x", cel.IntType),
	}

	result, err := expr.CompileWithVarDecls(varDecls)
	require.NoError(t, err)

	err = result.ValidateVars(map[string]any{
		"x": nil,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil value")
}

func TestValidateVars_ExtraVariablesIgnored(t *testing.T) {
	expr := Expression("x")
	varDecls := []VarDecl{
		NewVarDecl("x", cel.IntType),
	}

	result, err := expr.CompileWithVarDecls(varDecls)
	require.NoError(t, err)

	// Extra variable y should be ignored (not declared)
	err = result.ValidateVars(map[string]any{
		"x": int64(10),
		"y": "extra",
	})
	assert.NoError(t, err)
}

func TestValidateVars_NoDeclarations(t *testing.T) {
	// When using Compile() instead of CompileWithVarDecls(),
	// ValidateVars() should skip validation gracefully
	expr := Expression("x + y")
	result, err := expr.Compile([]cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	})
	require.NoError(t, err)

	// Should not error - validation is skipped when no declarations tracked
	err = result.ValidateVars(map[string]any{
		"x": "wrong type", // Would normally fail, but skipped
		"y": int64(20),
	})
	assert.NoError(t, err)
}

func TestGetDeclaredVariables(t *testing.T) {
	expr := Expression("x + y")
	varDecls := []VarDecl{
		NewVarDecl("x", cel.IntType),
		NewVarDecl("y", cel.IntType),
		NewVarDecl("name", cel.StringType),
	}

	result, err := expr.CompileWithVarDecls(varDecls)
	require.NoError(t, err)

	decls := result.GetDeclaredVariables()
	assert.Equal(t, 3, len(decls))
	assert.Equal(t, "int", decls["x"])
	assert.Equal(t, "int", decls["y"])
	assert.Equal(t, "string", decls["name"])
}

func TestGetDeclaredVariables_EmptyWhenUsingCompile(t *testing.T) {
	expr := Expression("x + y")
	result, err := expr.Compile([]cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	})
	require.NoError(t, err)

	decls := result.GetDeclaredVariables()
	assert.Empty(t, decls)
}

func TestValidateVars_WithEvaluation(t *testing.T) {
	// Integration test: validate then evaluate
	expr := Expression("x * 2 + y")
	varDecls := []VarDecl{
		NewVarDecl("x", cel.IntType),
		NewVarDecl("y", cel.IntType),
	}

	result, err := expr.CompileWithVarDecls(varDecls)
	require.NoError(t, err)

	vars := map[string]any{
		"x": int64(10),
		"y": int64(5),
	}

	// Validate before evaluation
	err = result.ValidateVars(vars)
	require.NoError(t, err)

	// Evaluate
	value, err := result.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, int64(25), value)
}

func TestVarDecl_ToEnvOption(t *testing.T) {
	vd := NewVarDecl("test", cel.StringType)
	envOpt := vd.ToEnvOption()
	assert.NotNil(t, envOpt)

	// Test that it works with cel.NewEnv
	env, err := cel.NewEnv(envOpt)
	require.NoError(t, err)
	assert.NotNil(t, env)
}

func TestIsCompatibleType(t *testing.T) {
	// nil CEL type - use string as go type, nil as cel type
	compat, _ := isCompatibleType(reflect.TypeOf(""), nil)
	assert.True(t, compat)

	tests := []struct {
		name       string
		goValue    any
		celType    *cel.Type
		wantCompat bool
	}{
		{"int64 matches int", int64(1), cel.IntType, true},
		{"int32 does not match int", int32(1), cel.IntType, false},
		{"uint64 matches uint", uint64(1), cel.UintType, true},
		{"float64 matches double", float64(1.0), cel.DoubleType, true},
		{"bool matches bool", true, cel.BoolType, true},
		{"string matches string", "hello", cel.StringType, true},
		{"bytes matches bytes", []byte("hi"), cel.BytesType, true},
		{"int does not match bytes", int64(1), cel.BytesType, false},
		{"slice matches list", []string{"a"}, cel.ListType(cel.StringType), true},
		{"int not list", int64(1), cel.ListType(cel.StringType), false},
		{"map matches map", map[string]any{}, cel.MapType(cel.StringType, cel.AnyType), true},
		{"int not map", int64(1), cel.MapType(cel.StringType, cel.AnyType), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goType := reflect.TypeOf(tt.goValue)
			compat, _ := isCompatibleType(goType, tt.celType)
			assert.Equal(t, tt.wantCompat, compat)
		})
	}
}

func TestIsCompatibleType_DefaultBranches(t *testing.T) {
	// OpaqueType creates a type string that doesn't match list/map prefix → hits default branches
	opaqueType := cel.OpaqueType("myproto", cel.StringType)

	t.Run("struct matches complex type", func(t *testing.T) {
		type myStruct struct{ Field string }
		goType := reflect.TypeOf(myStruct{})
		compat, _ := isCompatibleType(goType, opaqueType)
		assert.True(t, compat)
	})

	t.Run("map matches complex type", func(t *testing.T) {
		goType := reflect.TypeOf(map[string]any{})
		compat, _ := isCompatibleType(goType, opaqueType)
		assert.True(t, compat)
	})

	t.Run("interface matches complex type", func(t *testing.T) {
		goType := reflect.TypeOf((*interface{ Read([]byte) (int, error) })(nil)).Elem()
		compat, _ := isCompatibleType(goType, opaqueType)
		assert.True(t, compat)
	})

	t.Run("unsupported type returns false", func(t *testing.T) {
		goType := reflect.TypeOf(int(0)) // int is not struct/map/interface
		compat, msg := isCompatibleType(goType, opaqueType)
		assert.False(t, compat)
		assert.NotEmpty(t, msg)
	})
}

func TestCelTypeToString(t *testing.T) {
	assert.Equal(t, "any", celTypeToString(nil))
	assert.Equal(t, "string", celTypeToString(cel.StringType))
	assert.Equal(t, "int", celTypeToString(cel.IntType))
}

func BenchmarkValidateVars_Simple(b *testing.B) {
	expr := Expression("x + y")
	varDecls := []VarDecl{
		NewVarDecl("x", cel.IntType),
		NewVarDecl("y", cel.IntType),
	}
	result, _ := expr.CompileWithVarDecls(varDecls)
	vars := map[string]any{
		"x": int64(10),
		"y": int64(20),
	}

	b.ResetTimer()
	for b.Loop() {
		_ = result.ValidateVars(vars)
	}
}

func BenchmarkValidateVars_Complex(b *testing.B) {
	expr := Expression("a + b + c + d + e")
	varDecls := []VarDecl{
		NewVarDecl("a", cel.IntType),
		NewVarDecl("b", cel.IntType),
		NewVarDecl("c", cel.IntType),
		NewVarDecl("d", cel.IntType),
		NewVarDecl("e", cel.IntType),
	}
	result, _ := expr.CompileWithVarDecls(varDecls)
	vars := map[string]any{
		"a": int64(1),
		"b": int64(2),
		"c": int64(3),
		"d": int64(4),
		"e": int64(5),
	}

	b.ResetTimer()
	for b.Loop() {
		_ = result.ValidateVars(vars)
	}
}
