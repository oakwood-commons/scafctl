// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetDeclaredVars tests the GetDeclaredVars method
func TestGetDeclaredVars(t *testing.T) {
	t.Run("basic functionality", func(t *testing.T) {
		expr := Expression("x + y")
		decls := []VarDecl{
			NewVarDecl("x", cel.IntType),
			NewVarDecl("y", cel.IntType),
		}

		result, err := expr.CompileWithVarDecls(decls)
		require.NoError(t, err)
		require.NotNil(t, result)

		vars := result.GetDeclaredVars()
		require.NotNil(t, vars)
		require.Len(t, vars, 2)

		// Check that variables are sorted by name
		assert.Equal(t, "x", vars[0].Name)
		assert.Equal(t, "int", vars[0].Type)
		assert.Equal(t, "y", vars[1].Name)
		assert.Equal(t, "int", vars[1].Type)
	})

	t.Run("empty variables", func(t *testing.T) {
		expr := Expression("1 + 2")
		decls := []VarDecl{}

		result, err := expr.CompileWithVarDecls(decls)
		require.NoError(t, err)
		require.NotNil(t, result)

		vars := result.GetDeclaredVars()
		assert.Nil(t, vars)
	})

	t.Run("complex types", func(t *testing.T) {
		expr := Expression("items.size() > 0 && config.enabled")
		decls := []VarDecl{
			NewVarDecl("items", cel.ListType(cel.StringType)),
			NewVarDecl("config", cel.MapType(cel.StringType, cel.AnyType)),
		}

		result, err := expr.CompileWithVarDecls(decls)
		require.NoError(t, err)
		require.NotNil(t, result)

		vars := result.GetDeclaredVars()
		require.NotNil(t, vars)
		require.Len(t, vars, 2)

		// Check complex type formatting
		assert.Equal(t, "config", vars[0].Name)
		assert.Contains(t, vars[0].Type, "map")
		assert.Equal(t, "items", vars[1].Name)
		assert.Contains(t, vars[1].Type, "list")
	})

	t.Run("sorted output", func(t *testing.T) {
		expr := Expression("z + y + x + a")
		decls := []VarDecl{
			NewVarDecl("z", cel.IntType),
			NewVarDecl("y", cel.IntType),
			NewVarDecl("x", cel.IntType),
			NewVarDecl("a", cel.IntType),
		}

		result, err := expr.CompileWithVarDecls(decls)
		require.NoError(t, err)
		require.NotNil(t, result)

		vars := result.GetDeclaredVars()
		require.NotNil(t, vars)
		require.Len(t, vars, 4)

		// Verify alphabetical order
		assert.Equal(t, "a", vars[0].Name)
		assert.Equal(t, "x", vars[1].Name)
		assert.Equal(t, "y", vars[2].Name)
		assert.Equal(t, "z", vars[3].Name)
	})

	t.Run("nil compile result", func(t *testing.T) {
		var result *CompileResult
		vars := result.GetDeclaredVars()
		assert.Nil(t, vars)
	})
}

// TestValidateVars_ImprovedErrorMessages tests the improved error messages
func TestValidateVars_ImprovedErrorMessages(t *testing.T) {
	t.Run("missing variable with available vars", func(t *testing.T) {
		expr := Expression("x + y")
		decls := []VarDecl{
			NewVarDecl("x", cel.IntType),
			NewVarDecl("y", cel.IntType),
		}

		result, err := expr.CompileWithVarDecls(decls)
		require.NoError(t, err)

		// Only provide x, not y
		err = result.ValidateVars(map[string]any{
			"x": int64(10),
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing required variable")
		assert.Contains(t, err.Error(), "y")
		assert.Contains(t, err.Error(), "declared type: int")
		assert.Contains(t, err.Error(), "Available variables: [x]")
	})

	t.Run("missing variable with no vars provided", func(t *testing.T) {
		expr := Expression("x + y")
		decls := []VarDecl{
			NewVarDecl("x", cel.IntType),
			NewVarDecl("y", cel.IntType),
		}

		result, err := expr.CompileWithVarDecls(decls)
		require.NoError(t, err)

		err = result.ValidateVars(map[string]any{})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing required variable")
		assert.Contains(t, err.Error(), "No variables provided")
	})

	t.Run("type mismatch with value", func(t *testing.T) {
		expr := Expression("x + y")
		decls := []VarDecl{
			NewVarDecl("x", cel.IntType),
			NewVarDecl("y", cel.IntType),
		}

		result, err := expr.CompileWithVarDecls(decls)
		require.NoError(t, err)

		// Provide string instead of int for x
		err = result.ValidateVars(map[string]any{
			"x": "not an int",
			"y": int64(20),
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "type mismatch")
		assert.Contains(t, err.Error(), "x")
		assert.Contains(t, err.Error(), "expected int")
		assert.Contains(t, err.Error(), "got string")
		assert.Contains(t, err.Error(), "actual value: not an int")
	})

	t.Run("nil value error", func(t *testing.T) {
		expr := Expression("x + y")
		decls := []VarDecl{
			NewVarDecl("x", cel.IntType),
			NewVarDecl("y", cel.IntType),
		}

		result, err := expr.CompileWithVarDecls(decls)
		require.NoError(t, err)

		err = result.ValidateVars(map[string]any{
			"x": nil,
			"y": int64(20),
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "type mismatch")
		assert.Contains(t, err.Error(), "nil value provided")
		assert.Contains(t, err.Error(), "expected int")
	})
}

// TestEval_ImprovedErrorMessages tests improved error messages in evaluation
func TestEval_ImprovedErrorMessages(t *testing.T) {
	t.Run("eval error with type info", func(t *testing.T) {
		expr := Expression("x.startsWith('hello')")
		decls := []VarDecl{
			NewVarDecl("x", cel.StringType),
		}

		result, err := expr.CompileWithVarDecls(decls)
		require.NoError(t, err)

		// Provide int instead of string
		_, err = result.Eval(map[string]any{
			"x": int64(123),
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to evaluate expression")
		assert.Contains(t, err.Error(), "Variable types provided:")
		assert.Contains(t, err.Error(), "int64")
		assert.Contains(t, err.Error(), "Declared types:")
		assert.Contains(t, err.Error(), "string")
	})

	t.Run("eval error without declared types", func(t *testing.T) {
		expr := Expression("x.startsWith('hello')")

		// Use Compile instead of CompileWithVarDecls
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("x", cel.StringType),
		})
		require.NoError(t, err)

		// Provide int instead of string
		_, err = result.Eval(map[string]any{
			"x": int64(123),
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to evaluate expression")
		assert.Contains(t, err.Error(), "Variable types:")
		assert.Contains(t, err.Error(), "int64")
		// Should not contain "Declared types" since we used Compile
	})

	t.Run("eval with context error message", func(t *testing.T) {
		expr := Expression("x + y")
		decls := []VarDecl{
			NewVarDecl("x", cel.IntType),
			NewVarDecl("y", cel.IntType),
		}

		result, err := expr.CompileWithVarDecls(decls)
		require.NoError(t, err)

		// Provide string instead of int
		ctx := context.Background()
		_, err = result.EvalWithContext(ctx, map[string]any{
			"x": "wrong",
			"y": int64(20),
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to evaluate expression")
		assert.Contains(t, err.Error(), "Variable types provided:")
	})
}

// TestResetDefaultCache tests the cache reset functionality
func TestResetDefaultCache(t *testing.T) {
	t.Run("cache reset clears entries", func(t *testing.T) {
		// Reset to ensure clean state
		ResetDefaultCache()

		// Compile an expression
		expr := Expression("1 + 2")
		result1, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)
		require.NotNil(t, result1)

		// Check cache has one entry
		stats := GetDefaultCacheStats()
		assert.Equal(t, 1, stats.Size)

		// Reset the cache
		ResetDefaultCache()

		// Cache should be empty
		stats = GetDefaultCacheStats()
		assert.Equal(t, 0, stats.Size)
		assert.Equal(t, uint64(0), stats.Hits)
		assert.Equal(t, uint64(0), stats.Misses)
	})

	t.Run("cache reset allows new compilations", func(t *testing.T) {
		ResetDefaultCache()

		expr := Expression("x * 2")
		result1, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)})
		require.NoError(t, err)

		value, err := result1.Eval(map[string]any{"x": int64(5)})
		require.NoError(t, err)
		assert.Equal(t, int64(10), value)

		// Reset and compile again
		ResetDefaultCache()

		result2, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)})
		require.NoError(t, err)

		value, err = result2.Eval(map[string]any{"x": int64(5)})
		require.NoError(t, err)
		assert.Equal(t, int64(10), value)
	})

	t.Run("multiple resets", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			ResetDefaultCache()

			stats := GetDefaultCacheStats()
			assert.Equal(t, 0, stats.Size, "iteration %d", i)

			// Add an entry
			expr := Expression("1 + 1")
			_, err := expr.Compile([]cel.EnvOption{})
			require.NoError(t, err)
		}
	})
}

// TestFormatCelType tests the formatCelType helper function
func TestFormatCelType(t *testing.T) {
	tests := []struct {
		name     string
		celType  *cel.Type
		expected string
	}{
		{
			name:     "nil type",
			celType:  nil,
			expected: "any",
		},
		{
			name:     "int type",
			celType:  cel.IntType,
			expected: "int",
		},
		{
			name:     "string type",
			celType:  cel.StringType,
			expected: "string",
		},
		{
			name:     "bool type",
			celType:  cel.BoolType,
			expected: "bool",
		},
		{
			name:     "double type",
			celType:  cel.DoubleType,
			expected: "double",
		},
		{
			name:     "bytes type",
			celType:  cel.BytesType,
			expected: "bytes",
		},
		{
			name:     "list type",
			celType:  cel.ListType(cel.StringType),
			expected: "list(string)",
		},
		{
			name:     "map type",
			celType:  cel.MapType(cel.StringType, cel.IntType),
			expected: "map(string, int)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatCelType(tt.celType)
			// For parameterized types, check if the result contains the expected string
			if strings.Contains(tt.expected, "(") {
				assert.Contains(t, result, tt.expected)
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestCompileWithVarDecls_EnvOptsStored tests that envOpts are stored
func TestCompileWithVarDecls_EnvOptsStored(t *testing.T) {
	expr := Expression("x + y")
	decls := []VarDecl{
		NewVarDecl("x", cel.IntType),
		NewVarDecl("y", cel.IntType),
	}

	result, err := expr.CompileWithVarDecls(decls)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check that envOpts are stored
	assert.NotNil(t, result.envOpts)
	assert.Len(t, result.envOpts, 2)

	// Check that declaredVars are populated
	assert.NotNil(t, result.declaredVars)
	assert.Len(t, result.declaredVars, 2)
}

// TestCompile_EnvOptsStored tests that envOpts are stored in regular Compile
func TestCompile_EnvOptsStored(t *testing.T) {
	expr := Expression("x + y")
	envOpts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}

	result, err := expr.Compile(envOpts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check that envOpts are stored
	assert.NotNil(t, result.envOpts)
	assert.Len(t, result.envOpts, 2)
}
