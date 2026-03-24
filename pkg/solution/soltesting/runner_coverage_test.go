// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"context"
	"os"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/shellexec"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting/mockexec"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateSkipExpression_TrueExpression(t *testing.T) {
	runner := &Runner{
		IOStreams: &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
	}
	ctx := context.Background()

	result, err := runner.evaluateSkipExpression(ctx, "true")
	require.NoError(t, err)
	assert.True(t, result)
}

func TestEvaluateSkipExpression_FalseExpression(t *testing.T) {
	runner := &Runner{
		IOStreams: &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
	}
	ctx := context.Background()

	result, err := runner.evaluateSkipExpression(ctx, "false")
	require.NoError(t, err)
	assert.False(t, result)
}

func TestEvaluateSkipExpression_SubprocessVariable(t *testing.T) {
	runner := &Runner{
		BinaryPath: "/usr/bin/scafctl",
		IOStreams:  &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
	}
	ctx := context.Background()

	result, err := runner.evaluateSkipExpression(ctx, "subprocess")
	require.NoError(t, err)
	assert.True(t, result)
}

func TestEvaluateSkipExpression_SubprocessFalseWhenEmpty(t *testing.T) {
	runner := &Runner{
		BinaryPath: "",
		IOStreams:  &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
	}
	ctx := context.Background()

	result, err := runner.evaluateSkipExpression(ctx, "subprocess")
	require.NoError(t, err)
	assert.False(t, result)
}

func TestEvaluateSkipExpression_InvalidExpression(t *testing.T) {
	runner := &Runner{
		IOStreams: &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
	}
	ctx := context.Background()

	_, err := runner.evaluateSkipExpression(ctx, "invalid_func()")
	assert.Error(t, err)
}

func TestEvaluateSkipExpression_NonBoolResult(t *testing.T) {
	runner := &Runner{
		IOStreams: &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
	}
	ctx := context.Background()

	_, err := runner.evaluateSkipExpression(ctx, `"string_value"`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must return bool")
}

func TestComposeExecMocks_MatchingRule(t *testing.T) {
	mock, err := mockexec.New([]mockexec.Rule{
		{
			Command:  "echo hello",
			Stdout:   "hello\n",
			ExitCode: 0,
		},
	})
	require.NoError(t, err)

	composed := composeExecMocks([]*mockexec.MockExec{mock})
	require.NotNil(t, composed)
}

func TestComposeExecMocks_NoMatchingRule(t *testing.T) {
	mock, err := mockexec.New([]mockexec.Rule{
		{
			Command:  "echo hello",
			Stdout:   "hello\n",
			ExitCode: 0,
		},
	})
	require.NoError(t, err)

	composed := composeExecMocks([]*mockexec.MockExec{mock})
	require.NotNil(t, composed)

	// Call with a non-matching command — should return an error
	ctx := context.Background()
	runFn := composed
	_, runErr := runFn(ctx, &shellexec.RunOptions{
		Command: "ls",
		Args:    []string{"-la"},
	})
	assert.Error(t, runErr)
}

func TestComposeExecMocks_MultipleMocks(t *testing.T) {
	mock1, err := mockexec.New([]mockexec.Rule{
		{Command: "echo hello", Stdout: "hello\n", ExitCode: 0},
	})
	require.NoError(t, err)

	mock2, err := mockexec.New([]mockexec.Rule{
		{Command: "echo world", Stdout: "world\n", ExitCode: 0},
	})
	require.NoError(t, err)

	composed := composeExecMocks([]*mockexec.MockExec{mock1, mock2})
	require.NotNil(t, composed)
}

func TestRunnerEmitTestStart_WithCallback(t *testing.T) {
	var started []string
	runner := &Runner{
		IOStreams: &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		Progress: &testProgressTracker{
			onTestStart: func(solution, test string) {
				started = append(started, solution+"/"+test)
			},
		},
	}
	runner.emitTestStart("my-solution", "my-test")
	assert.Equal(t, []string{"my-solution/my-test"}, started)
}

func TestRunnerEmitTestStart_WithoutCallback(t *testing.T) {
	runner := &Runner{
		IOStreams: &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		Progress:  nil,
	}
	// Should not panic
	runner.emitTestStart("my-solution", "my-test")
}

func TestRunnerEmitTestComplete_WithCallback(t *testing.T) {
	var completed []TestResult
	runner := &Runner{
		IOStreams: &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		Progress: &testProgressTracker{
			onTestComplete: func(result TestResult) {
				completed = append(completed, result)
			},
		},
	}
	result := TestResult{
		Solution: "my-solution",
		Test:     "my-test",
		Status:   StatusPass,
	}
	runner.emitTestComplete(result)
	require.Len(t, completed, 1)
	assert.Equal(t, StatusPass, completed[0].Status)
}

func TestRunnerEmitTestComplete_WithoutCallback(t *testing.T) {
	runner := &Runner{
		IOStreams: &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		Progress:  nil,
	}
	// Should not panic
	runner.emitTestComplete(TestResult{})
}

// testProgressTracker implements TestProgressCallback for testing.
type testProgressTracker struct {
	onTestStart    func(solution, test string)
	onTestComplete func(result TestResult)
}

func (t *testProgressTracker) OnTestStart(solution, test string) {
	if t.onTestStart != nil {
		t.onTestStart(solution, test)
	}
}

func (t *testProgressTracker) OnTestComplete(result TestResult) {
	if t.onTestComplete != nil {
		t.onTestComplete(result)
	}
}

func (t *testProgressTracker) Wait() {}

// Benchmarks

func BenchmarkEvaluateSkipExpression(b *testing.B) {
	runner := &Runner{
		IOStreams: &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = runner.evaluateSkipExpression(ctx, "true")
	}
}

func BenchmarkComposeExecMocks(b *testing.B) {
	mock, err := mockexec.New([]mockexec.Rule{
		{Command: "echo hello", Stdout: "hello\n", ExitCode: 0},
	})
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		composeExecMocks([]*mockexec.MockExec{mock})
	}
}
