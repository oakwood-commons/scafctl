// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package debug

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
)

// newTestWriter creates a Writer for testing that writes to the provided buffer.
// Returns the Writer and the error output buffer (where DebugOut writes).
func newTestWriter() (*writer.Writer, *bytes.Buffer) {
	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	cliParams := &settings.Run{
		NoColor:     true,
		IsQuiet:     false,
		MinLogLevel: "debug", // Enable debug output
	}
	w := writer.New(ioStreams, cliParams)
	return w, errBuf
}

func TestDebugOutFunc_Metadata(t *testing.T) {
	w, _ := newTestWriter()
	debugFunc := DebugOutFunc(w)

	assert.Equal(t, "debug.out", debugFunc.Name)
	assert.Equal(t, "Outputs a debug message to the console. Use debug.out(message) to print a message (returns null), or debug.out(message, value) to print a message and return a value for inline debugging", debugFunc.Description)
	assert.NotEmpty(t, debugFunc.EnvOptions)
}

func TestDebugOutFunc_OutputCapture(t *testing.T) {
	w, outBuf := newTestWriter()
	debugFunc := DebugOutFunc(w)

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name           string
		expression     string
		expectedOutput string
	}{
		{
			name:           "debug string value",
			expression:     `debug.out("test value")`,
			expectedOutput: "CEL DEBUG OUTPUT: test value",
		},
		{
			name:           "debug integer value",
			expression:     `debug.out(42)`,
			expectedOutput: "CEL DEBUG OUTPUT: 42",
		},
		{
			name:           "debug boolean value",
			expression:     `debug.out(true)`,
			expectedOutput: "CEL DEBUG OUTPUT: true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outBuf.Reset()

			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation failed: %v", issues)

			prog, err := env.Program(ast)
			require.NoError(t, err)

			_, _, err = prog.Eval(map[string]interface{}{})
			require.NoError(t, err)

			output := strings.TrimSpace(outBuf.String())
			assert.Contains(t, output, tt.expectedOutput)
		})
	}
}

func TestDebugOutFunc_TwoArgumentOverload(t *testing.T) {
	w, outBuf := newTestWriter()
	debugFunc := DebugOutFunc(w)

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name           string
		expression     string
		expectedOutput string
		expectedValue  interface{}
	}{
		{
			name:           "debug message with string value",
			expression:     `debug.out("checking value", "returned value")`,
			expectedOutput: "CEL DEBUG OUTPUT: checking value",
			expectedValue:  "returned value",
		},
		{
			name:           "debug message with integer value",
			expression:     `debug.out("the answer", 42)`,
			expectedOutput: "CEL DEBUG OUTPUT: the answer",
			expectedValue:  int64(42),
		},
		{
			name:           "debug message with boolean value",
			expression:     `debug.out("is valid?", true)`,
			expectedOutput: "CEL DEBUG OUTPUT: is valid?",
			expectedValue:  true,
		},
		{
			name:           "inline usage with two arguments",
			expression:     `debug.out("length of string", "hello").size()`,
			expectedOutput: "CEL DEBUG OUTPUT: length of string",
			expectedValue:  int64(5),
		},
		{
			name:           "inline usage with calculation",
			expression:     `debug.out("result", 10 + 5) * 2`,
			expectedOutput: "CEL DEBUG OUTPUT: result",
			expectedValue:  int64(30),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outBuf.Reset()

			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation failed: %v", issues)

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)

			output := strings.TrimSpace(outBuf.String())
			assert.Contains(t, output, tt.expectedOutput)
			assert.Equal(t, tt.expectedValue, result.Value())
		})
	}
}

func TestDebugOutFunc_CELIntegration(t *testing.T) {
	w, _ := newTestWriter()
	debugFunc := DebugOutFunc(w)

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
	}{
		{
			name:       "debug string value returns null",
			expression: `debug.out("test value")`,
		},
		{
			name:       "debug integer value returns null",
			expression: `debug.out(42)`,
		},
		{
			name:       "debug boolean value returns null",
			expression: `debug.out(true)`,
		},
		{
			name:       "debug list value returns null",
			expression: `debug.out(["a", "b", "c"])`,
		},
		{
			name:       "debug map value returns null",
			expression: `debug.out({"key": "value"})`,
		},
		{
			name:       "debug empty string returns null",
			expression: `debug.out("")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation failed: %v", issues)

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			// Single-arg debug.out returns structpb.NullValue(0), not Go nil
			assert.Equal(t, int(0), int(result.Value().(structpb.NullValue)))
		})
	}
}

func TestDebugOutFunc_InlineUsage(t *testing.T) {
	w, _ := newTestWriter()
	debugFunc := DebugOutFunc(w)

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name          string
		expression    string
		expectedValue interface{}
	}{
		{
			name:          "debug and use string length with two-arg",
			expression:    `debug.out("checking", "hello").size()`,
			expectedValue: int64(5),
		},
		{
			name:          "debug and use string concatenation with two-arg",
			expression:    `debug.out("value", "hello") + " world"`,
			expectedValue: "hello world",
		},
		{
			name:          "debug and perform arithmetic with two-arg",
			expression:    `debug.out("num", 10) + 5`,
			expectedValue: int64(15),
		},
		{
			name:          "debug in conditional with two-arg",
			expression:    `debug.out("flag", true) ? "yes" : "no"`,
			expectedValue: "yes",
		},
		{
			name:          "single-arg returns null",
			expression:    `debug.out("test") == null`,
			expectedValue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation failed: %v", issues)

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, result.Value())
		})
	}
}

func TestDebugOutFunc_WithVariables(t *testing.T) {
	w, _ := newTestWriter()
	debugFunc := DebugOutFunc(w)

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	env, err = env.Extend(
		cel.Variable("myValue", cel.AnyType),
	)
	require.NoError(t, err)

	// Single-arg returns null
	ast, issues := env.Compile(`debug.out(myValue)`)
	if issues != nil {
		t.Logf("Compilation issues: %v", issues)
	}
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	testCases := []struct {
		value interface{}
	}{
		{"test string"},
		{int64(123)},
		{true},
		{[]string{"a", "b"}},
	}

	for _, tc := range testCases {
		result, _, err := prog.Eval(map[string]interface{}{
			"myValue": tc.value,
		})
		require.NoError(t, err)
		// Single-arg debug.out returns structpb.NullValue(0), not Go nil
		assert.Equal(t, int(0), int(result.Value().(structpb.NullValue)))
	}
}

func TestDebugOutFunc_OutputFormat(t *testing.T) {
	w, _ := newTestWriter()
	debugFunc := DebugOutFunc(w)

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`debug.out("test")`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	_, _, err = prog.Eval(map[string]interface{}{})
	require.NoError(t, err)

	// The actual output goes to os.Stdout, which is hard to capture in unit tests
	// This test mainly verifies the function executes without errors
}

func TestDebugOutFunc_ComplexExpressions(t *testing.T) {
	w, _ := newTestWriter()
	debugFunc := DebugOutFunc(w)

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name        string
		expression  string
		checkValue  func(t *testing.T, val interface{})
		description string
	}{
		{
			name:       "debug in list operation",
			expression: `["a", "b", "c"].map(x, debug.out("item", x))`,
			checkValue: func(t *testing.T, val interface{}) {
				// CEL map returns a list, just verify it's not nil
				assert.NotNil(t, val)
			},
			description: "debug each element in list using two-arg form",
		},
		{
			name:       "debug in filter",
			expression: `[1, 2, 3, 4, 5].filter(x, debug.out("checking", x) > 2)`,
			checkValue: func(t *testing.T, val interface{}) {
				// CEL filter returns a list, just verify it's not nil
				assert.NotNil(t, val)
			},
			description: "debug during filtering using two-arg form",
		},
		{
			name:       "debug intermediate calculation",
			expression: `debug.out("result", 5 * 5) + 10`,
			checkValue: func(t *testing.T, val interface{}) {
				assert.Equal(t, int64(35), val)
			},
			description: "debug intermediate value in calculation using two-arg form",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation failed: %v", issues)

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			tt.checkValue(t, result.Value())
		})
	}
}

func TestDebugOutFunc_EdgeCases(t *testing.T) {
	w, _ := newTestWriter()
	debugFunc := DebugOutFunc(w)

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		checkValue func(t *testing.T, val interface{})
	}{
		{
			name:       "debug null value",
			expression: `debug.out(null)`,
			checkValue: func(t *testing.T, val interface{}) {
				// Single-arg returns null
				assert.Equal(t, int(0), int(val.(structpb.NullValue)))
			},
		},
		{
			name:       "debug empty list",
			expression: `debug.out([])`,
			checkValue: func(t *testing.T, val interface{}) {
				// Single-arg returns null
				assert.Equal(t, int(0), int(val.(structpb.NullValue)))
			},
		},
		{
			name:       "debug empty map",
			expression: `debug.out({})`,
			checkValue: func(t *testing.T, val interface{}) {
				// Single-arg returns null
				assert.Equal(t, int(0), int(val.(structpb.NullValue)))
			},
		},
		{
			name:       "debug zero",
			expression: `debug.out(0)`,
			checkValue: func(t *testing.T, val interface{}) {
				// Single-arg returns null
				assert.Equal(t, int(0), int(val.(structpb.NullValue)))
			},
		},
		{
			name:       "debug negative number",
			expression: `debug.out(-42)`,
			checkValue: func(t *testing.T, val interface{}) {
				// Single-arg returns null
				assert.Equal(t, int(0), int(val.(structpb.NullValue)))
			},
		},
		{
			name:       "debug special characters",
			expression: `debug.out("hello\nworld\t!")`,
			checkValue: func(t *testing.T, val interface{}) {
				// Single-arg returns null
				assert.Equal(t, int(0), int(val.(structpb.NullValue)))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation failed: %v", issues)

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)

			tt.checkValue(t, result.Value())
		})
	}
}

func BenchmarkDebugOutFunc_CEL(b *testing.B) {
	w, _ := newTestWriter()
	debugFunc := DebugOutFunc(w)
	env, _ := cel.NewEnv(debugFunc.EnvOptions...)
	ast, _ := env.Compile(`debug.out("benchmark test")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}

// DebugThrowFunc Tests

func TestDebugThrowFunc_Metadata(t *testing.T) {
	debugFunc := DebugThrowFunc()

	assert.Equal(t, "debug.throw", debugFunc.Name)
	assert.Equal(t, "Throws an error with the provided message, immediately halting CEL expression evaluation. Use debug.throw(message) to stop execution and return an error with the specified message", debugFunc.Description)
	assert.NotEmpty(t, debugFunc.EnvOptions)
}

func TestDebugThrowFunc_ThrowsError(t *testing.T) {
	debugFunc := DebugThrowFunc()

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name          string
		expression    string
		expectedError string
		errorContains string
	}{
		{
			name:          "throw string error",
			expression:    `debug.throw("custom error message")`,
			errorContains: "custom error message",
		},
		{
			name:          "throw integer error",
			expression:    `debug.throw(404)`,
			errorContains: "404",
		},
		{
			name:          "throw boolean error",
			expression:    `debug.throw(false)`,
			errorContains: "false",
		},
		{
			name:          "throw error in expression",
			expression:    `1 + debug.throw("error in calculation")`,
			errorContains: "error in calculation",
		},
		{
			name:          "throw error with concatenated message",
			expression:    `debug.throw("Error: " + "something went wrong")`,
			errorContains: "Error: something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, _ := prog.Eval(map[string]interface{}{})

			// The result should be an error
			require.NotNil(t, result)
			resultStr := fmt.Sprintf("%v", result)
			assert.True(t, strings.Contains(resultStr, tt.errorContains),
				"Expected error to contain '%s', got: %s", tt.errorContains, resultStr)
		})
	}
}

func TestDebugThrowFunc_InConditional(t *testing.T) {
	debugFunc := DebugThrowFunc()

	env, err := cel.NewEnv(
		debugFunc.EnvOptions[0],
		cel.Variable("value", cel.IntType),
	)
	require.NoError(t, err)

	tests := []struct {
		name          string
		expression    string
		variables     map[string]interface{}
		shouldError   bool
		errorContains string
		expectedValue interface{}
	}{
		{
			name:          "throw in true condition",
			expression:    `value > 10 ? debug.throw("value too large") : value`,
			variables:     map[string]interface{}{"value": 15},
			shouldError:   true,
			errorContains: "value too large",
		},
		{
			name:          "no throw in false condition",
			expression:    `value > 10 ? debug.throw("value too large") : value`,
			variables:     map[string]interface{}{"value": 5},
			shouldError:   false,
			expectedValue: int64(5),
		},
		{
			name:          "throw in false condition branch",
			expression:    `value < 10 ? value : debug.throw("value not less than 10")`,
			variables:     map[string]interface{}{"value": 15},
			shouldError:   true,
			errorContains: "value not less than 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast, cel.EvalOptions(cel.OptTrackState))
			require.NoError(t, err)

			result, _, _ := prog.Eval(tt.variables)
			require.NotNil(t, result)

			if tt.shouldError {
				resultStr := fmt.Sprintf("%v", result)
				assert.True(t, strings.Contains(resultStr, tt.errorContains),
					"Expected error to contain '%s', got: %s", tt.errorContains, resultStr)
			} else {
				assert.Equal(t, tt.expectedValue, result.Value())
			}
		})
	}
}

func TestDebugThrowFunc_WithVariables(t *testing.T) {
	debugFunc := DebugThrowFunc()

	env, err := cel.NewEnv(
		debugFunc.EnvOptions[0],
		cel.Variable("errorMsg", cel.StringType),
	)
	require.NoError(t, err)

	tests := []struct {
		name          string
		expression    string
		variables     map[string]interface{}
		errorContains string
	}{
		{
			name:          "throw with variable",
			expression:    `debug.throw(errorMsg)`,
			variables:     map[string]interface{}{"errorMsg": "variable error"},
			errorContains: "variable error",
		},
		{
			name:          "throw with concatenated variable",
			expression:    `debug.throw("Error: " + errorMsg)`,
			variables:     map[string]interface{}{"errorMsg": "file not found"},
			errorContains: "Error: file not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, _ := prog.Eval(tt.variables)
			require.NotNil(t, result)
			resultStr := fmt.Sprintf("%v", result)
			assert.True(t, strings.Contains(resultStr, tt.errorContains),
				"Expected error to contain '%s', got: %s", tt.errorContains, resultStr)
		})
	}
}

func BenchmarkDebugThrowFunc_CEL(b *testing.B) {
	debugFunc := DebugThrowFunc()
	env, _ := cel.NewEnv(debugFunc.EnvOptions...)
	ast, _ := env.Compile(`debug.throw("benchmark error")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}

// DebugSleepFunc Tests

func TestDebugSleepFunc_Metadata(t *testing.T) {
	debugFunc := DebugSleepFunc()

	assert.Equal(t, "debug.sleep", debugFunc.Name)
	assert.Equal(t, "Pauses execution for the specified duration in milliseconds and returns the value for inline debugging. Use debug.sleep(duration) to sleep and return the duration value, or debug.sleep(duration, value) to sleep and return a different value", debugFunc.Description)
	assert.NotEmpty(t, debugFunc.EnvOptions)
}

func TestDebugSleepFunc_Sleep(t *testing.T) {
	debugFunc := DebugSleepFunc()

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name          string
		expression    string
		expectedValue int64
	}{
		{
			name:          "sleep 10ms",
			expression:    `debug.sleep(10)`,
			expectedValue: 10,
		},
		{
			name:          "sleep 0ms",
			expression:    `debug.sleep(0)`,
			expectedValue: 0,
		},
		{
			name:          "sleep with calculation",
			expression:    `debug.sleep(5 + 5)`,
			expectedValue: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			require.NotNil(t, result)

			// Should return the duration value
			assert.Equal(t, tt.expectedValue, result.Value())
		})
	}
}

func TestDebugSleepFunc_NegativeValue(t *testing.T) {
	debugFunc := DebugSleepFunc()

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`debug.sleep(-100)`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Negative values are clamped to 0, but the original value is returned
	assert.Equal(t, int64(-100), result.Value())
}

func TestDebugSleepFunc_TwoArgumentOverload(t *testing.T) {
	debugFunc := DebugSleepFunc()

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		checkValue func(t *testing.T, val interface{})
	}{
		{
			name:       "sleep with string value",
			expression: `debug.sleep(10, "test value")`,
			checkValue: func(t *testing.T, val interface{}) {
				assert.Equal(t, "test value", val)
			},
		},
		{
			name:       "sleep with integer value",
			expression: `debug.sleep(10, 42)`,
			checkValue: func(t *testing.T, val interface{}) {
				assert.Equal(t, int64(42), val)
			},
		},
		{
			name:       "sleep with boolean value",
			expression: `debug.sleep(10, true)`,
			checkValue: func(t *testing.T, val interface{}) {
				assert.Equal(t, true, val)
			},
		},
		{
			name:       "sleep with list value",
			expression: `debug.sleep(10, [1, 2, 3])`,
			checkValue: func(t *testing.T, val interface{}) {
				// CEL returns lists as []ref.Val, just verify it's not nil
				assert.NotNil(t, val)
			},
		},
		{
			name:       "sleep with map value",
			expression: `debug.sleep(10, {"key": "value"})`,
			checkValue: func(t *testing.T, val interface{}) {
				// CEL returns maps with ref.Val keys/values, just verify it's not nil
				assert.NotNil(t, val)
			},
		},
		{
			name:       "sleep with calculated duration",
			expression: `debug.sleep(5 + 5, "calculated")`,
			checkValue: func(t *testing.T, val interface{}) {
				assert.Equal(t, "calculated", val)
			},
		},
		{
			name:       "sleep with expression value",
			expression: `debug.sleep(10, 5 + 10)`,
			checkValue: func(t *testing.T, val interface{}) {
				assert.Equal(t, int64(15), val)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			require.NotNil(t, result)

			// Should return the second argument value
			tt.checkValue(t, result.Value())
		})
	}
}

func TestDebugSleepFunc_InlineUsage(t *testing.T) {
	debugFunc := DebugSleepFunc()

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		checkValue func(t *testing.T, val interface{})
	}{
		{
			name:       "sleep in string concatenation",
			expression: `"prefix-" + debug.sleep(10, "middle") + "-suffix"`,
			checkValue: func(t *testing.T, val interface{}) {
				assert.Equal(t, "prefix-middle-suffix", val)
			},
		},
		{
			name:       "sleep in arithmetic",
			expression: `debug.sleep(10, 5) * 2`,
			checkValue: func(t *testing.T, val interface{}) {
				assert.Equal(t, int64(10), val)
			},
		},
		{
			name:       "sleep in ternary",
			expression: `true ? debug.sleep(10, "yes") : "no"`,
			checkValue: func(t *testing.T, val interface{}) {
				assert.Equal(t, "yes", val)
			},
		},
		{
			name:       "multiple sleeps in chain",
			expression: `debug.sleep(5, debug.sleep(5, "nested"))`,
			checkValue: func(t *testing.T, val interface{}) {
				assert.Equal(t, "nested", val)
			},
		},
		{
			name:       "sleep in list construction",
			expression: `[debug.sleep(5, 1), debug.sleep(5, 2), debug.sleep(5, 3)]`,
			checkValue: func(t *testing.T, val interface{}) {
				// CEL returns lists as []ref.Val, just verify it's not nil
				assert.NotNil(t, val)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			require.NotNil(t, result)

			tt.checkValue(t, result.Value())
		})
	}
}

func TestDebugSleepFunc_TwoArgumentWithVariables(t *testing.T) {
	debugFunc := DebugSleepFunc()

	env, err := cel.NewEnv(
		debugFunc.EnvOptions[0],
		cel.Variable("duration", cel.IntType),
		cel.Variable("value", cel.AnyType),
	)
	require.NoError(t, err)

	tests := []struct {
		name          string
		expression    string
		variables     map[string]interface{}
		expectedValue interface{}
	}{
		{
			name:          "sleep with variable duration and value",
			expression:    `debug.sleep(duration, value)`,
			variables:     map[string]interface{}{"duration": 10, "value": "test"},
			expectedValue: "test",
		},
		{
			name:          "sleep with calculated duration and variable value",
			expression:    `debug.sleep(duration * 2, value)`,
			variables:     map[string]interface{}{"duration": 5, "value": 42},
			expectedValue: int64(42),
		},
		{
			name:          "sleep with variable duration and expression value",
			expression:    `debug.sleep(duration, value + " suffix")`,
			variables:     map[string]interface{}{"duration": 10, "value": "prefix"},
			expectedValue: "prefix suffix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(tt.variables)
			require.NoError(t, err)
			require.NotNil(t, result)

			assert.Equal(t, tt.expectedValue, result.Value())
		})
	}
}

func TestDebugSleepFunc_InExpression(t *testing.T) {
	debugFunc := DebugSleepFunc()

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name          string
		expression    string
		expectedValue int64
	}{
		{
			name:          "sleep in addition",
			expression:    `debug.sleep(10) + 5`,
			expectedValue: 15,
		},
		{
			name:          "sleep in multiplication",
			expression:    `debug.sleep(10) * 2`,
			expectedValue: 20,
		},
		{
			name:          "sleep in comparison",
			expression:    `debug.sleep(10) > 5 ? 100 : 0`,
			expectedValue: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			require.NotNil(t, result)

			assert.Equal(t, tt.expectedValue, result.Value())
		})
	}
}

func TestDebugSleepFunc_WithVariables(t *testing.T) {
	debugFunc := DebugSleepFunc()

	env, err := cel.NewEnv(
		debugFunc.EnvOptions[0],
		cel.Variable("duration", cel.IntType),
	)
	require.NoError(t, err)

	tests := []struct {
		name          string
		expression    string
		variables     map[string]interface{}
		expectedValue int64
	}{
		{
			name:          "sleep with variable",
			expression:    `debug.sleep(duration)`,
			variables:     map[string]interface{}{"duration": 25},
			expectedValue: 25,
		},
		{
			name:          "sleep with calculated variable",
			expression:    `debug.sleep(duration * 2)`,
			variables:     map[string]interface{}{"duration": 10},
			expectedValue: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(tt.variables)
			require.NoError(t, err)
			require.NotNil(t, result)

			assert.Equal(t, tt.expectedValue, result.Value())
		})
	}
}

func TestDebugSleepFunc_InvalidType(t *testing.T) {
	debugFunc := DebugSleepFunc()

	env, err := cel.NewEnv(debugFunc.EnvOptions...)
	require.NoError(t, err)

	// This should fail at compile time since we require IntType
	_, issues := env.Compile(`debug.sleep("not a number")`)
	require.Error(t, issues.Err())
	assert.Contains(t, issues.Err().Error(), "found no matching overload")
}

func BenchmarkDebugSleepFunc_CEL(b *testing.B) {
	debugFunc := DebugSleepFunc()
	env, _ := cel.NewEnv(debugFunc.EnvOptions...)
	ast, _ := env.Compile(`debug.sleep(1)`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}
