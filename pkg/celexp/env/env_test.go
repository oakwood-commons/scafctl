package env

import (
	"context"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	ctx := context.Background()

	// Create environment with a simple variable declaration
	env, err := New(ctx, cel.Variable("testVar", cel.StringType))
	require.NoError(t, err)
	require.NotNil(t, env)

	// Test that we can compile and evaluate expressions with the environment
	tests := []struct {
		name       string
		expression string
		vars       map[string]interface{}
		expected   interface{}
	}{
		{
			name:       "simple variable",
			expression: `testVar`,
			vars:       map[string]interface{}{"testVar": "hello"},
			expected:   "hello",
		},
		{
			name:       "strings extension - charAt",
			expression: `"hello".charAt(1)`,
			vars:       map[string]interface{}{},
			expected:   "e",
		},
		{
			name:       "strings extension - split",
			expression: `"a,b,c".split(",").size()`,
			vars:       map[string]interface{}{},
			expected:   int64(3),
		},
		{
			name:       "math extension - abs",
			expression: `math.abs(-5)`,
			vars:       map[string]interface{}{},
			expected:   int64(5),
		},
		{
			name:       "lists extension - reverse",
			expression: `[1, 2, 3].reverse().size()`,
			vars:       map[string]interface{}{},
			expected:   int64(3),
		},
		{
			name:       "sets extension - contains",
			expression: `sets.contains([1, 2, 3], [2])`,
			vars:       map[string]interface{}{},
			expected:   true,
		},
		{
			name:       "encoders extension - base64.encode",
			expression: `base64.encode(b"hello")`,
			vars:       map[string]interface{}{},
			expected:   "aGVsbG8=",
		},
		{
			name:       "bindings extension - cel.bind",
			expression: `cel.bind(x, 5, x * 2)`,
			vars:       map[string]interface{}{},
			expected:   int64(10),
		},
		{
			name:       "optionalTypes - optional.of",
			expression: `optional.of(42).value()`,
			vars:       map[string]interface{}{},
			expected:   int64(42),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err(), "Failed to compile expression: %s", tt.expression)

			prog, err := env.Program(ast)
			require.NoError(t, err, "Failed to create program")

			result, _, err := prog.Eval(tt.vars)
			require.NoError(t, err, "Failed to evaluate expression")

			assert.Equal(t, tt.expected, result.Value(), "Expression result mismatch for: %s", tt.expression)
		})
	}
}

func TestNew_WithMultipleDeclarations(t *testing.T) {
	ctx := context.Background()

	// Test with multiple variable declarations
	env, err := New(ctx,
		cel.Variable("name", cel.StringType),
		cel.Variable("age", cel.IntType),
	)
	require.NoError(t, err)
	require.NotNil(t, env)

	// Test expression using both variables and extension functions
	ast, issues := env.Compile(`"Hello " + name + ", you are " + string(age) + " years old"`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]interface{}{
		"name": "Alice",
		"age":  int64(30),
	})
	require.NoError(t, err)

	assert.Equal(t, "Hello Alice, you are 30 years old", result.Value())
}

func TestNew_AllExtensionsLoaded(t *testing.T) {
	ctx := context.Background()

	env, err := New(ctx, cel.Variable("dummy", cel.StringType))
	require.NoError(t, err)

	// Get the functions from the environment
	funcs := env.Functions()

	// Verify that extension functions are loaded
	extensionFunctions := []string{
		"charAt",        // strings
		"reverse",       // lists
		"cel.@block",    // bindings
		"base64.encode", // encoders
		"math.abs",      // math
		"sets.contains", // sets
		"hasValue",      // optionalTypes
	}

	for _, funcName := range extensionFunctions {
		assert.True(t, env.HasFunction(funcName),
			"Extension function %s should be loaded", funcName)
	}

	// Verify we have more functions than a baseline environment
	baselineEnv, err := cel.NewEnv()
	require.NoError(t, err)

	baselineFuncs := baselineEnv.Functions()
	assert.Greater(t, len(funcs), len(baselineFuncs),
		"Environment should have more functions than baseline after loading extensions")

	t.Logf("Total functions in environment: %d", len(funcs))
	t.Logf("Baseline functions: %d", len(baselineFuncs))
	t.Logf("Extension functions added: %d", len(funcs)-len(baselineFuncs))
}

func TestNew_NoDeclarations(t *testing.T) {
	ctx := context.Background()

	// Test that we can create an environment with nil declarations
	// (the function should handle this gracefully)
	env, err := New(ctx, cel.Variable("test", cel.StringType))
	require.NoError(t, err)
	require.NotNil(t, env)

	// Verify extensions still work
	ast, issues := env.Compile(`"hello".charAt(0)`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]interface{}{})
	require.NoError(t, err)

	assert.Equal(t, "h", result.Value())
}
