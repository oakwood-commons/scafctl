package soltesting_test

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateAssertions_ExpressionPass(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout:   "hello world",
		Stderr:   "",
		ExitCode: 0,
		Output:   map[string]any{"status": "ok"},
		Files:    map[string]soltesting.FileInfo{},
	}
	assertions := []soltesting.Assertion{
		{Expression: "__exitCode == 0"},
	}
	results := soltesting.EvaluateAssertions(context.Background(), assertions, cmdOutput)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)
	assert.Equal(t, "expression", results[0].Type)
}

func TestEvaluateAssertions_ExpressionFail(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout:   "hello world",
		ExitCode: 1,
		Output:   map[string]any{"status": "error"},
		Files:    map[string]soltesting.FileInfo{},
	}
	assertions := []soltesting.Assertion{
		{Expression: "__exitCode == 0"},
	}
	results := soltesting.EvaluateAssertions(context.Background(), assertions, cmdOutput)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed)
	assert.Contains(t, results[0].Message, "Comparison")
}

func TestEvaluateAssertions_RegexPass(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout: "Error: file not found at line 42",
		Files:  map[string]soltesting.FileInfo{},
	}
	assertions := []soltesting.Assertion{
		{Regex: `line \d+`},
	}
	results := soltesting.EvaluateAssertions(context.Background(), assertions, cmdOutput)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)
	assert.Equal(t, "regex", results[0].Type)
}

func TestEvaluateAssertions_RegexFail(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout: "all good",
		Files:  map[string]soltesting.FileInfo{},
	}
	assertions := []soltesting.Assertion{
		{Regex: `error\d+`},
	}
	results := soltesting.EvaluateAssertions(context.Background(), assertions, cmdOutput)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed)
	assert.Contains(t, results[0].Message, "did not match")
}

func TestEvaluateAssertions_ContainsPass(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout: "operation completed successfully",
		Files:  map[string]soltesting.FileInfo{},
	}
	assertions := []soltesting.Assertion{
		{Contains: "successfully"},
	}
	results := soltesting.EvaluateAssertions(context.Background(), assertions, cmdOutput)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)
}

func TestEvaluateAssertions_ContainsFail(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout: "operation failed",
		Files:  map[string]soltesting.FileInfo{},
	}
	assertions := []soltesting.Assertion{
		{Contains: "successfully"},
	}
	results := soltesting.EvaluateAssertions(context.Background(), assertions, cmdOutput)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed)
	assert.Contains(t, results[0].Message, "not found")
}

func TestEvaluateAssertions_NotRegex(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout: "all good",
		Files:  map[string]soltesting.FileInfo{},
	}
	results := soltesting.EvaluateAssertions(context.Background(),
		[]soltesting.Assertion{{NotRegex: "ERROR"}}, cmdOutput)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)

	cmdOutput.Stdout = "ERROR: something broke"
	results = soltesting.EvaluateAssertions(context.Background(),
		[]soltesting.Assertion{{NotRegex: "ERROR"}}, cmdOutput)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed)
	assert.Contains(t, results[0].Message, "unexpectedly matched")
}

func TestEvaluateAssertions_NotContains(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout: "all good",
		Files:  map[string]soltesting.FileInfo{},
	}
	results := soltesting.EvaluateAssertions(context.Background(),
		[]soltesting.Assertion{{NotContains: "ERROR"}}, cmdOutput)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)

	cmdOutput.Stdout = "ERROR: something broke"
	results = soltesting.EvaluateAssertions(context.Background(),
		[]soltesting.Assertion{{NotContains: "ERROR"}}, cmdOutput)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed)
	assert.Contains(t, results[0].Message, "unexpectedly found")
}

func TestEvaluateAssertions_TargetStderr(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout: "normal output",
		Stderr: "warning: something happened",
		Files:  map[string]soltesting.FileInfo{},
	}
	results := soltesting.EvaluateAssertions(context.Background(),
		[]soltesting.Assertion{{Contains: "warning", Target: "stderr"}}, cmdOutput)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)
}

func TestEvaluateAssertions_TargetCombined(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout: "stdout line",
		Stderr: "stderr line",
		Files:  map[string]soltesting.FileInfo{},
	}
	results := soltesting.EvaluateAssertions(context.Background(),
		[]soltesting.Assertion{{Contains: "stderr line", Target: "combined"}}, cmdOutput)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)

	results = soltesting.EvaluateAssertions(context.Background(),
		[]soltesting.Assertion{{Contains: "stdout line", Target: "combined"}}, cmdOutput)
	require.Len(t, results, 1)
	assert.True(t, results[0].Passed)
}

func TestEvaluateAssertions_AllEvaluatedEvenOnFailure(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout: "hello",
		Files:  map[string]soltesting.FileInfo{},
	}
	assertions := []soltesting.Assertion{
		{Contains: "hello"},
		{Contains: "missing"},
		{Contains: "hello"},
	}
	results := soltesting.EvaluateAssertions(context.Background(), assertions, cmdOutput)
	require.Len(t, results, 3)
	assert.True(t, results[0].Passed)
	assert.False(t, results[1].Passed)
	assert.True(t, results[2].Passed)
}

func TestEvaluateAssertions_OutputNilError(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout: "some output",
		Files:  map[string]soltesting.FileInfo{},
	}
	assertions := []soltesting.Assertion{
		{Expression: `__output.status == "ok"`},
	}
	results := soltesting.EvaluateAssertions(context.Background(), assertions, cmdOutput)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed)
	assert.Contains(t, results[0].Message, "nil")
}

func TestEvaluateAssertions_CustomMessage(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout: "bad output",
		Files:  map[string]soltesting.FileInfo{},
	}
	assertions := []soltesting.Assertion{
		{Contains: "expected", Message: "Output should contain expected string"},
	}
	results := soltesting.EvaluateAssertions(context.Background(), assertions, cmdOutput)
	require.Len(t, results, 1)
	assert.False(t, results[0].Passed)
	assert.Contains(t, results[0].Message, "Output should contain expected string")
}

func TestBuildAssertionContext_NilOutput(t *testing.T) {
	ctx := soltesting.BuildAssertionContext(nil)
	assert.Equal(t, "", ctx["__stdout"])
	assert.Equal(t, "", ctx["__stderr"])
	assert.Equal(t, 0, ctx["__exitCode"])
	assert.Nil(t, ctx["__output"])
}

func TestBuildAssertionContext_WithOutput(t *testing.T) {
	cmdOutput := &soltesting.CommandOutput{
		Stdout:   "hello",
		Stderr:   "warn",
		ExitCode: 0,
		Output:   map[string]any{"key": "value"},
		Files: map[string]soltesting.FileInfo{
			"new.txt": {Exists: true, Content: "content"},
		},
	}
	ctx := soltesting.BuildAssertionContext(cmdOutput)
	assert.Equal(t, "hello", ctx["__stdout"])
	assert.Equal(t, "warn", ctx["__stderr"])
	assert.Equal(t, 0, ctx["__exitCode"])
	assert.NotNil(t, ctx["__output"])

	filesMap, ok := ctx["__files"].(map[string]any)
	require.True(t, ok)
	fileEntry, ok := filesMap["new.txt"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, fileEntry["exists"])
	assert.Equal(t, "content", fileEntry["content"])
}
