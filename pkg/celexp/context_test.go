package celexp

import (
	"context"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCELContext_ResolverDataOnly(t *testing.T) {
	resolverData := map[string]any{
		"port":        int64(8080),
		"environment": "prod",
	}

	envOpts, vars := BuildCELContext(resolverData, nil)

	// Verify variables
	require.NotNil(t, vars)
	require.Contains(t, vars, "_")
	assert.Equal(t, resolverData, vars["_"])
	assert.Len(t, vars, 1) // Only _ variable

	// Verify environment options
	require.NotEmpty(t, envOpts)
	assert.Len(t, envOpts, 1) // Only _ variable

	// Verify CEL can use the variables
	expr := Expression("_.port + 100")
	compiled, err := expr.Compile(envOpts)
	require.NoError(t, err)

	result, err := compiled.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, int64(8180), result)
}

func TestBuildCELContext_WithSelf(t *testing.T) {
	resolverData := map[string]any{
		"multiplier": int64(10),
	}
	selfValue := int64(5)

	envOpts, vars := BuildCELContext(resolverData, map[string]any{VarSelf: selfValue})

	// Verify variables
	require.Contains(t, vars, "_")
	require.Contains(t, vars, VarSelf)
	assert.Equal(t, resolverData, vars["_"])
	assert.Equal(t, selfValue, vars[VarSelf])
	assert.Len(t, vars, 2)

	// Verify CEL can use both variables
	expr := Expression("__self * _.multiplier")
	compiled, err := expr.Compile(envOpts)
	require.NoError(t, err)

	result, err := compiled.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, int64(50), result)
}

func TestBuildCELContext_WithItem(t *testing.T) {
	resolverData := map[string]any{
		"prefix": "item",
	}
	itemValue := int64(42)

	envOpts, vars := BuildCELContext(resolverData, map[string]any{VarItem: itemValue})

	// Verify variables
	require.Contains(t, vars, "_")
	require.Contains(t, vars, VarItem)
	assert.Equal(t, resolverData, vars["_"])
	assert.Equal(t, itemValue, vars[VarItem])
	assert.Len(t, vars, 2)

	// Verify CEL can use both variables
	expr := Expression("_.prefix + '-' + string(__item)")
	compiled, err := expr.Compile(envOpts)
	require.NoError(t, err)

	result, err := compiled.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, "item-42", result)
}

func TestBuildCELContext_WithAdditionalVars(t *testing.T) {
	resolverData := map[string]any{
		"name": "world",
	}
	additionalVars := map[string]any{
		"prefix": "Hello",
		"suffix": "!",
	}

	envOpts, vars := BuildCELContext(resolverData, additionalVars)

	// Verify variables
	require.Contains(t, vars, "_")
	require.Contains(t, vars, "prefix")
	require.Contains(t, vars, "suffix")
	assert.Equal(t, resolverData, vars["_"])
	assert.Equal(t, "Hello", vars["prefix"])
	assert.Equal(t, "!", vars["suffix"])
	assert.Len(t, vars, 3)

	// Verify CEL can use all variables
	expr := Expression("prefix + ' ' + _.name + suffix")
	compiled, err := expr.Compile(envOpts)
	require.NoError(t, err)

	result, err := compiled.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, "Hello world!", result)
}

func TestBuildCELContext_AllOptions(t *testing.T) {
	resolverData := map[string]any{
		"multiplier": int64(2),
	}
	additionalVars := map[string]any{
		VarSelf:  int64(10),
		VarItem:  "test",
		"prefix": "Result:",
	}

	envOpts, vars := BuildCELContext(resolverData, additionalVars)

	// Verify all variables present
	require.Contains(t, vars, "_")
	require.Contains(t, vars, VarSelf)
	require.Contains(t, vars, VarItem)
	require.Contains(t, vars, "prefix")
	assert.Len(t, vars, 4)

	// Verify CEL can use all variables
	expr := Expression("prefix + ' ' + string(__self * _.multiplier) + ' ' + __item")
	compiled, err := expr.Compile(envOpts)
	require.NoError(t, err)

	result, err := compiled.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, "Result: 20 test", result)
}

func TestBuildCELContext_NilInputs(t *testing.T) {
	envOpts, vars := BuildCELContext(nil, nil)

	// Should return empty but valid structures
	assert.NotNil(t, vars)
	assert.NotNil(t, envOpts)
	assert.Empty(t, vars)
	assert.Empty(t, envOpts)

	// Should still be usable with CEL
	expr := Expression("1 + 1")
	compiled, err := expr.Compile(envOpts)
	require.NoError(t, err)

	result, err := compiled.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, int64(2), result)
}

func TestBuildCELContext_EmptyResolverData(t *testing.T) {
	resolverData := map[string]any{} // Empty but not nil

	envOpts, vars := BuildCELContext(resolverData, nil)

	// Should include _ variable even if empty
	require.Contains(t, vars, "_")
	assert.Empty(t, vars["_"])

	// Verify CEL can check for empty map
	expr := Expression("size(_) == 0")
	compiled, err := expr.Compile(envOpts)
	require.NoError(t, err)

	result, err := compiled.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, true, result)
}

func TestBuildCELContext_NestedResolverData(t *testing.T) {
	resolverData := map[string]any{
		"config": map[string]any{
			"database": map[string]any{
				"host": "localhost",
				"port": int64(5432),
			},
		},
	}

	envOpts, vars := BuildCELContext(resolverData, nil)

	// Verify CEL can navigate nested structure
	expr := Expression("_.config.database.host + ':' + string(_.config.database.port)")
	compiled, err := expr.Compile(envOpts)
	require.NoError(t, err)

	result, err := compiled.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, "localhost:5432", result)
}

func TestBuildCELContext_VariableTypesCorrect(t *testing.T) {
	resolverData := map[string]any{
		"test": "value",
	}

	envOpts, vars := BuildCELContext(resolverData, map[string]any{
		VarSelf: "self",
		VarItem: "item",
		"extra": "var",
	})

	// Create environment and verify variable types are declared correctly
	env, err := cel.NewEnv(envOpts...)
	require.NoError(t, err)

	// Compile a complex expression that uses all variables
	ast, issues := env.Compile("has(_.test) && __self == 'self' && __item == 'item' && extra == 'var'")
	require.Nil(t, issues)
	require.NotNil(t, ast)

	// Evaluate
	prg, err := env.Program(ast)
	require.NoError(t, err)

	out, _, err := prg.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, true, out.Value())
}

func TestEvaluateExpression_Basic(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"name": "world",
		"port": int64(8080),
	}

	result, err := EvaluateExpression(ctx, "'hello ' + _.name", resolverData, nil)
	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
}

func TestEvaluateExpression_WithAdditionalVars(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"name": "Smith",
	}
	additionalVars := map[string]any{
		"prefix": "Dr.",
		"suffix": "PhD",
	}

	result, err := EvaluateExpression(ctx, "prefix + ' ' + _.name + ' ' + suffix", resolverData, additionalVars)
	require.NoError(t, err)
	assert.Equal(t, "Dr. Smith PhD", result)
}

func TestEvaluateExpression_WithSelf(t *testing.T) {
	ctx := context.Background()
	selfValue := int64(10)

	result, err := EvaluateExpression(ctx, "__self * 2", nil, map[string]any{VarSelf: selfValue})
	require.NoError(t, err)
	assert.Equal(t, int64(20), result)
}

func TestEvaluateExpression_WithItem(t *testing.T) {
	ctx := context.Background()
	itemValue := map[string]any{
		"id":   int64(123),
		"name": "test",
	}

	result, err := EvaluateExpression(ctx, "__item.id + 100", nil, map[string]any{VarItem: itemValue})
	require.NoError(t, err)
	assert.Equal(t, int64(223), result)
}

func TestEvaluateExpression_WithAllParameters(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"base": int64(1000),
	}
	additionalVars := map[string]any{
		VarSelf:      int64(5),
		VarItem:      int64(3),
		"multiplier": int64(2),
	}

	result, err := EvaluateExpression(ctx, "_.base + (__self * __item * multiplier)", resolverData, additionalVars)
	require.NoError(t, err)
	assert.Equal(t, int64(1030), result) // 1000 + (5 * 3 * 2) = 1030
}

func TestEvaluateExpression_ComplexConversion(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"items": []any{
			map[string]any{"name": "item1", "value": int64(10)},
			map[string]any{"name": "item2", "value": int64(20)},
		},
	}

	result, err := EvaluateExpression(ctx, "size(_.items)", resolverData, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), result)
}

func TestEvaluateExpression_CompilationError(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"name": "test",
	}

	_, err := EvaluateExpression(ctx, "_.invalidSyntax(((", resolverData, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to compile expression")
}

func TestEvaluateExpression_EvaluationError(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"items": []any{int64(1), int64(2)},
	}

	// Try to access an index that doesn't exist
	_, err := EvaluateExpression(ctx, "_.items[10]", resolverData, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to evaluate expression")
}

func TestEvaluateExpression_WithCustomCompileOptions(t *testing.T) {
	ctx := context.Background()
	resolverData := map[string]any{
		"value": int64(42),
	}

	// WithContext is added automatically, but we can pass additional options
	result, err := EvaluateExpression(ctx, "_.value", resolverData, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(42), result)
}

func TestEvaluateExpression_NilResolverData(t *testing.T) {
	ctx := context.Background()
	additionalVars := map[string]any{
		"x": int64(10),
		"y": int64(20),
	}

	result, err := EvaluateExpression(ctx, "x + y", nil, additionalVars)
	require.NoError(t, err)
	assert.Equal(t, int64(30), result)
}

func TestEvaluateExpression_TypeConversion(t *testing.T) {
	ctx := context.Background()
	rootData := map[string]any{
		"data": map[string]any{
			"nested": map[string]any{
				"value": int64(100),
			},
		},
	}

	result, err := EvaluateExpression(ctx, "_.data.nested", rootData, nil)
	require.NoError(t, err)

	// Verify the nested map was properly converted
	resultMap, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, int64(100), resultMap["value"])
}

// Tests for non-map root data types (DynType support)

func TestBuildCELContext_SliceRootData(t *testing.T) {
	rootData := []any{"a", "b", "c"}
	envOpts, vars := BuildCELContext(rootData, nil)

	require.Contains(t, vars, "_")
	assert.Equal(t, rootData, vars["_"])
	assert.Len(t, vars, 1)

	// Verify CEL can work with slice
	expr := Expression("size(_)")
	compiled, err := expr.Compile(envOpts)
	require.NoError(t, err)

	result, err := compiled.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, int64(3), result)
}

func TestBuildCELContext_SliceIndexAccess(t *testing.T) {
	rootData := []any{"first", "second", "third"}
	envOpts, vars := BuildCELContext(rootData, nil)

	expr := Expression("_[0]")
	compiled, err := expr.Compile(envOpts)
	require.NoError(t, err)

	result, err := compiled.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, "first", result)
}

func TestBuildCELContext_StringRootData(t *testing.T) {
	rootData := "hello world"
	envOpts, vars := BuildCELContext(rootData, nil)

	require.Contains(t, vars, "_")
	assert.Equal(t, rootData, vars["_"])

	// Verify CEL can work with string (use size() which is a base CEL function)
	expr := Expression("size(_)")
	compiled, err := expr.Compile(envOpts)
	require.NoError(t, err)

	result, err := compiled.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, int64(11), result) // "hello world" has 11 characters
}

func TestBuildCELContext_IntRootData(t *testing.T) {
	rootData := int64(42)
	envOpts, vars := BuildCELContext(rootData, nil)

	require.Contains(t, vars, "_")
	assert.Equal(t, rootData, vars["_"])

	// Verify CEL can work with int
	expr := Expression("_ * 2")
	compiled, err := expr.Compile(envOpts)
	require.NoError(t, err)

	result, err := compiled.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, int64(84), result)
}

func TestBuildCELContext_BoolRootData(t *testing.T) {
	rootData := true
	envOpts, vars := BuildCELContext(rootData, nil)

	require.Contains(t, vars, "_")
	assert.Equal(t, rootData, vars["_"])

	// Verify CEL can work with bool
	expr := Expression("_ == true")
	compiled, err := expr.Compile(envOpts)
	require.NoError(t, err)

	result, err := compiled.Eval(vars)
	require.NoError(t, err)
	assert.Equal(t, true, result)
}

func TestEvaluateExpression_SliceRootData(t *testing.T) {
	ctx := context.Background()
	rootData := []any{
		map[string]any{"name": "item1"},
		map[string]any{"name": "item2"},
	}

	// Test identity expression returns raw value
	result, err := EvaluateExpression(ctx, "_", rootData, nil)
	require.NoError(t, err)

	resultSlice, ok := result.([]any)
	require.True(t, ok, "expected []any, got %T", result)
	assert.Len(t, resultSlice, 2)
}

func TestEvaluateExpression_SliceOperations(t *testing.T) {
	ctx := context.Background()
	rootData := []any{int64(1), int64(2), int64(3), int64(4), int64(5)}

	// Test size
	result, err := EvaluateExpression(ctx, "size(_)", rootData, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(5), result)

	// Test index access
	result, err = EvaluateExpression(ctx, "_[2]", rootData, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(3), result)

	// Test filter (if supported)
	result, err = EvaluateExpression(ctx, "_.filter(x, x > 2)", rootData, nil)
	require.NoError(t, err)
	resultSlice, ok := result.([]any)
	require.True(t, ok)
	assert.Len(t, resultSlice, 3) // 3, 4, 5
}

func TestEvaluateExpression_StringRootData(t *testing.T) {
	ctx := context.Background()
	rootData := "hello"

	result, err := EvaluateExpression(ctx, "_ + ' world'", rootData, nil)
	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
}

func TestEvaluateExpression_IntRootData(t *testing.T) {
	ctx := context.Background()
	rootData := int64(100)

	result, err := EvaluateExpression(ctx, "_ / 4", rootData, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(25), result)
}
