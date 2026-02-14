// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCommandBuilder creates a mock root command for testing.
// It returns a command that echoes stdout/stderr and sets an exit code.
func mockCommandBuilder(stdout, stderr string, exitCode int) CommandBuilder {
	return func(ioStreams *terminal.IOStreams, exitFunc func(code int)) *cobra.Command {
		cmd := &cobra.Command{
			Use:          "scafctl",
			SilenceUsage: true,
		}

		// Add a catch-all subcommand that accepts any args
		runCmd := &cobra.Command{
			Use:                "run",
			DisableFlagParsing: true,
			RunE: func(_ *cobra.Command, _ []string) error {
				if stdout != "" {
					ioStreams.Out.Write([]byte(stdout))
				}
				if stderr != "" {
					ioStreams.ErrOut.Write([]byte(stderr))
				}
				if exitCode != 0 {
					exitFunc(exitCode)
				}
				return nil
			},
		}
		lintCmd := &cobra.Command{
			Use:                "lint",
			DisableFlagParsing: true,
			RunE: func(_ *cobra.Command, _ []string) error {
				if stdout != "" {
					ioStreams.Out.Write([]byte(stdout))
				}
				if stderr != "" {
					ioStreams.ErrOut.Write([]byte(stderr))
				}
				if exitCode != 0 {
					exitFunc(exitCode)
				}
				return nil
			},
		}
		renderCmd := &cobra.Command{
			Use:                "render",
			DisableFlagParsing: true,
			RunE: func(_ *cobra.Command, _ []string) error {
				if stdout != "" {
					ioStreams.Out.Write([]byte(stdout))
				}
				if stderr != "" {
					ioStreams.ErrOut.Write([]byte(stderr))
				}
				if exitCode != 0 {
					exitFunc(exitCode)
				}
				return nil
			},
		}

		cmd.AddCommand(runCmd, lintCmd, renderCmd)
		return cmd
	}
}

// createTestSolution creates a minimal solution YAML file for testing.
func createTestSolution(t *testing.T, dir, name string) string {
	t.Helper()
	content := `metadata:
  name: ` + name + `
spec:
  tests:
    basic-test:
      command: ["run"]
      assertions:
        - expression: "__exitCode == 0"
`
	path := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestRunnerDryRun(t *testing.T) {
	ctx := context.Background()
	exitCodeZero := 0
	runner := &Runner{
		DryRun:     true,
		IOStreams:  &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		NewCommand: mockCommandBuilder("", "", 0),
	}

	solutions := []SolutionTests{
		{
			SolutionName: "test-solution",
			FilePath:     "/tmp/solution.yaml",
			TestConfig:   &TestConfig{SkipBuiltins: SkipBuiltinsValue{All: true}},
			Tests: map[string]*TestCase{
				"my-test": {
					Name:     "my-test",
					Command:  []string{"run"},
					ExitCode: &exitCodeZero,
					Assertions: []Assertion{
						{Expression: "__exitCode == 0"},
					},
				},
			},
		},
	}

	results, err := runner.Run(ctx, solutions)
	require.NoError(t, err)

	// dry-run should produce skip results
	require.Len(t, results, 1)
	assert.Equal(t, StatusSkip, results[0].Status)
	assert.Equal(t, "dry run", results[0].Message)
}

func TestRunnerSkipTest(t *testing.T) {
	ctx := context.Background()
	runner := &Runner{
		IOStreams:  &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		NewCommand: mockCommandBuilder("", "", 0),
	}

	solutions := []SolutionTests{
		{
			SolutionName: "test-solution",
			FilePath:     "/tmp/solution.yaml",
			TestConfig:   &TestConfig{SkipBuiltins: SkipBuiltinsValue{All: true}},
			Tests: map[string]*TestCase{
				"skipped-test": {
					Name:       "skipped-test",
					Command:    []string{"run"},
					Skip:       true,
					SkipReason: "not ready yet",
					Assertions: []Assertion{
						{Expression: "__exitCode == 0"},
					},
				},
			},
		},
	}

	results, err := runner.Run(ctx, solutions)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, StatusSkip, results[0].Status)
	assert.Equal(t, "not ready yet", results[0].Message)
}

func TestRunnerPassingTest(t *testing.T) {
	tmpDir := t.TempDir()
	solutionPath := createTestSolution(t, tmpDir, "passing-sol")

	ctx := context.Background()
	runner := &Runner{
		IOStreams:  &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		NewCommand: mockCommandBuilder("hello world", "", 0),
	}

	solutions := []SolutionTests{
		{
			SolutionName: "passing-sol",
			FilePath:     solutionPath,
			TestConfig:   &TestConfig{SkipBuiltins: SkipBuiltinsValue{All: true}},
			Tests: map[string]*TestCase{
				"pass-test": {
					Name:    "pass-test",
					Command: []string{"run"},
					Assertions: []Assertion{
						{Expression: `__exitCode == 0`},
					},
				},
			},
		},
	}

	results, err := runner.Run(ctx, solutions)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, StatusPass, results[0].Status)
}

func TestRunnerFailingTest(t *testing.T) {
	tmpDir := t.TempDir()
	solutionPath := createTestSolution(t, tmpDir, "failing-sol")

	ctx := context.Background()
	exitCodeOne := 1
	runner := &Runner{
		IOStreams:  &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		NewCommand: mockCommandBuilder("", "error happened", 1),
	}

	solutions := []SolutionTests{
		{
			SolutionName: "failing-sol",
			FilePath:     solutionPath,
			TestConfig:   &TestConfig{SkipBuiltins: SkipBuiltinsValue{All: true}},
			Tests: map[string]*TestCase{
				"fail-test": {
					Name:     "fail-test",
					Command:  []string{"run"},
					ExitCode: &exitCodeOne,
					Assertions: []Assertion{
						{Contains: "success"},
					},
				},
			},
		},
	}

	results, err := runner.Run(ctx, solutions)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, StatusFail, results[0].Status)
}

func TestRunnerExpectFailure(t *testing.T) {
	tmpDir := t.TempDir()
	solutionPath := createTestSolution(t, tmpDir, "expect-fail-sol")

	ctx := context.Background()
	runner := &Runner{
		IOStreams:  &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		NewCommand: mockCommandBuilder("", "error", 1),
	}

	solutions := []SolutionTests{
		{
			SolutionName: "expect-fail-sol",
			FilePath:     solutionPath,
			TestConfig:   &TestConfig{SkipBuiltins: SkipBuiltinsValue{All: true}},
			Tests: map[string]*TestCase{
				"expect-fail": {
					Name:          "expect-fail",
					Command:       []string{"run"},
					ExpectFailure: true,
					Assertions: []Assertion{
						{Expression: "__exitCode != 0"},
					},
				},
			},
		},
	}

	results, err := runner.Run(ctx, solutions)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, StatusPass, results[0].Status)
}

func TestRunnerRetry(t *testing.T) {
	tmpDir := t.TempDir()
	solutionPath := createTestSolution(t, tmpDir, "retry-sol")

	// Track call count to succeed on retry
	callCount := 0
	builder := func(ioStreams *terminal.IOStreams, exitFunc func(code int)) *cobra.Command {
		cmd := &cobra.Command{Use: "scafctl", SilenceUsage: true}
		runCmd := &cobra.Command{
			Use:                "run",
			DisableFlagParsing: true,
			RunE: func(_ *cobra.Command, _ []string) error {
				callCount++
				if callCount < 3 {
					exitFunc(1)
				}
				return nil
			},
		}
		cmd.AddCommand(runCmd)
		return cmd
	}

	ctx := context.Background()
	runner := &Runner{
		IOStreams:  &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		NewCommand: builder,
	}

	exitCodeZero := 0
	solutions := []SolutionTests{
		{
			SolutionName: "retry-sol",
			FilePath:     solutionPath,
			TestConfig:   &TestConfig{SkipBuiltins: SkipBuiltinsValue{All: true}},
			Tests: map[string]*TestCase{
				"retry-test": {
					Name:     "retry-test",
					Command:  []string{"run"},
					ExitCode: &exitCodeZero,
					Retries:  3,
					Assertions: []Assertion{
						{Expression: "__exitCode == 0"},
					},
				},
			},
		},
	}

	results, err := runner.Run(ctx, solutions)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, StatusPass, results[0].Status)
	assert.Equal(t, 2, results[0].RetryAttempt) // 3rd attempt (0-indexed: 2)
}

func TestRunnerFailFast(t *testing.T) {
	tmpDir := t.TempDir()
	solutionPath := createTestSolution(t, tmpDir, "failfast-sol")

	ctx := context.Background()
	runner := &Runner{
		FailFast:   true,
		IOStreams:  &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		NewCommand: mockCommandBuilder("", "", 1),
	}

	exitCodeZero := 0
	solutions := []SolutionTests{
		{
			SolutionName: "failfast-sol",
			FilePath:     solutionPath,
			TestConfig:   &TestConfig{SkipBuiltins: SkipBuiltinsValue{All: true}},
			Tests: map[string]*TestCase{
				"aaa-first": {
					Name:     "aaa-first",
					Command:  []string{"run"},
					ExitCode: &exitCodeZero,
					Assertions: []Assertion{
						{Expression: "__exitCode == 0"},
					},
				},
				"bbb-second": {
					Name:     "bbb-second",
					Command:  []string{"run"},
					ExitCode: &exitCodeZero,
					Assertions: []Assertion{
						{Expression: "__exitCode == 0"},
					},
				},
			},
		},
	}

	results, err := runner.Run(ctx, solutions)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// First should fail, second should be skipped due to fail-fast
	assert.Equal(t, StatusFail, results[0].Status)
	assert.Equal(t, StatusSkip, results[1].Status)
	assert.Contains(t, results[1].Message, "fail-fast")
}

func TestRunnerConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	solutionPath := createTestSolution(t, tmpDir, "concurrent-sol")

	ctx := context.Background()
	runner := &Runner{
		Concurrency: 4,
		IOStreams:   &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		NewCommand:  mockCommandBuilder("ok", "", 0),
	}

	exitCodeZero := 0
	tests := make(map[string]*TestCase)
	for i := range 8 {
		name := "test-" + string(rune('a'+i))
		tests[name] = &TestCase{
			Name:     name,
			Command:  []string{"run"},
			ExitCode: &exitCodeZero,
			Assertions: []Assertion{
				{Expression: "__exitCode == 0"},
			},
		}
	}

	solutions := []SolutionTests{
		{
			SolutionName: "concurrent-sol",
			FilePath:     solutionPath,
			TestConfig:   &TestConfig{SkipBuiltins: SkipBuiltinsValue{All: true}},
			Tests:        tests,
		},
	}

	results, err := runner.Run(ctx, solutions)
	require.NoError(t, err)
	assert.Len(t, results, 8)

	for _, r := range results {
		assert.Equal(t, StatusPass, r.Status, "test %s should pass", r.Test)
	}
}

func TestRunnerNoCommand(t *testing.T) {
	tmpDir := t.TempDir()
	solutionPath := createTestSolution(t, tmpDir, "nocmd-sol")

	ctx := context.Background()
	runner := &Runner{
		IOStreams:  &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		NewCommand: mockCommandBuilder("", "", 0),
	}

	solutions := []SolutionTests{
		{
			SolutionName: "nocmd-sol",
			FilePath:     solutionPath,
			TestConfig:   &TestConfig{SkipBuiltins: SkipBuiltinsValue{All: true}},
			Tests: map[string]*TestCase{
				"no-command": {
					Name: "no-command",
					// No Command field — fails validation
				},
			},
		},
	}

	results, err := runner.Run(ctx, solutions)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, StatusError, results[0].Status)
	assert.Contains(t, results[0].Message, "command is required")
}

func TestRunnerBuiltinParse(t *testing.T) {
	tmpDir := t.TempDir()
	solutionPath := createTestSolution(t, tmpDir, "parse-sol")

	ctx := context.Background()
	runner := &Runner{
		IOStreams:  &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		NewCommand: mockCommandBuilder("", "", 0),
	}

	solutions := []SolutionTests{
		{
			SolutionName: "parse-sol",
			FilePath:     solutionPath,
			TestConfig:   &TestConfig{SkipBuiltins: SkipBuiltinsValue{Names: []string{"lint", "resolve-defaults", "render-defaults"}}},
			Tests:        map[string]*TestCase{},
		},
	}

	results, err := runner.Run(ctx, solutions)
	require.NoError(t, err)

	// Should have only the parse builtin
	var parseResult *TestResult
	for i, r := range results {
		if r.Test == BuiltinName(BuiltinParse) {
			parseResult = &results[i]
		}
	}
	require.NotNil(t, parseResult, "should have builtin:parse result")
	assert.Equal(t, StatusPass, parseResult.Status)
}

func TestRunnerKeepSandbox(t *testing.T) {
	tmpDir := t.TempDir()
	solutionPath := createTestSolution(t, tmpDir, "sandbox-sol")

	ctx := context.Background()
	runner := &Runner{
		KeepSandbox: true,
		IOStreams:   &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		NewCommand:  mockCommandBuilder("ok", "", 0),
	}

	exitCodeZero := 0
	solutions := []SolutionTests{
		{
			SolutionName: "sandbox-sol",
			FilePath:     solutionPath,
			TestConfig:   &TestConfig{SkipBuiltins: SkipBuiltinsValue{All: true}},
			Tests: map[string]*TestCase{
				"keep-test": {
					Name:     "keep-test",
					Command:  []string{"run"},
					ExitCode: &exitCodeZero,
					Assertions: []Assertion{
						{Expression: "__exitCode == 0"},
					},
				},
			},
		},
	}

	results, err := runner.Run(ctx, solutions)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.NotEmpty(t, results[0].SandboxPath)

	// Sandbox should still exist
	_, err = os.Stat(results[0].SandboxPath)
	assert.NoError(t, err, "sandbox directory should still exist")

	// Cleanup
	os.RemoveAll(results[0].SandboxPath)
}

func TestCheckExitCode(t *testing.T) {
	r := &Runner{}
	exitCodeZero := 0
	exitCodeTwo := 2

	tests := []struct {
		name     string
		tc       *TestCase
		output   *CommandOutput
		expected bool
	}{
		{
			name:     "default success",
			tc:       &TestCase{},
			output:   &CommandOutput{ExitCode: 0},
			expected: true,
		},
		{
			name:     "default failure",
			tc:       &TestCase{},
			output:   &CommandOutput{ExitCode: 1},
			expected: false,
		},
		{
			name:     "expect failure with non-zero",
			tc:       &TestCase{ExpectFailure: true},
			output:   &CommandOutput{ExitCode: 1},
			expected: true,
		},
		{
			name:     "expect failure with zero",
			tc:       &TestCase{ExpectFailure: true},
			output:   &CommandOutput{ExitCode: 0},
			expected: false,
		},
		{
			name:     "explicit exit code match",
			tc:       &TestCase{ExitCode: &exitCodeTwo},
			output:   &CommandOutput{ExitCode: 2},
			expected: true,
		},
		{
			name:     "explicit exit code mismatch",
			tc:       &TestCase{ExitCode: &exitCodeZero},
			output:   &CommandOutput{ExitCode: 1},
			expected: false,
		},
		{
			name:     "assertions present bypass default check",
			tc:       &TestCase{Assertions: []Assertion{{Expression: "true"}}},
			output:   &CommandOutput{ExitCode: 42},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.checkExitCode(tt.tc, tt.output)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestBuildEnvMap(t *testing.T) {
	r := &Runner{}

	tc := &TestCase{
		Env: map[string]string{
			"TEST_VAR": "test-value",
			"OVERRIDE": "from-test",
		},
	}
	config := &TestConfig{
		Env: map[string]string{
			"CONFIG_VAR": "config-value",
			"OVERRIDE":   "from-config",
		},
	}

	env := r.buildEnvMap(tc, config, "/tmp/sandbox")

	assert.Equal(t, "test-value", env["TEST_VAR"])
	assert.Equal(t, "config-value", env["CONFIG_VAR"])
	assert.Equal(t, "from-test", env["OVERRIDE"]) // test overrides config
	assert.Equal(t, "/tmp/sandbox", env["SCAFCTL_SANDBOX_DIR"])
}

func TestMergeEnvForStep(t *testing.T) {
	envMap := map[string]any{
		"A": "1",
		"B": "2",
	}
	stepEnv := map[string]string{
		"B": "overridden",
		"C": "3",
	}

	result := mergeEnvForStep(envMap, stepEnv)
	assert.NotEmpty(t, result)

	// Check that all vars are present in KEY=VALUE format
	found := make(map[string]bool)
	for _, v := range result {
		if strings.HasPrefix(v, "B=overridden") {
			found["B"] = true
		}
		if strings.HasPrefix(v, "C=3") {
			found["C"] = true
		}
	}
	assert.True(t, found["B"], "should have overridden B")
	assert.True(t, found["C"], "should have C")
}

func TestMergeEnvForStepEmpty(t *testing.T) {
	result := mergeEnvForStep(nil, nil)
	assert.Nil(t, result)
}

// Reporter tests

func TestSummarize(t *testing.T) {
	results := []TestResult{
		{Status: StatusPass, Duration: 100 * time.Millisecond},
		{Status: StatusPass, Duration: 200 * time.Millisecond},
		{Status: StatusFail, Duration: 150 * time.Millisecond},
		{Status: StatusError, Duration: 50 * time.Millisecond},
		{Status: StatusSkip, Duration: 10 * time.Millisecond},
	}

	s := Summarize(results)
	assert.Equal(t, 5, s.Total)
	assert.Equal(t, 2, s.Passed)
	assert.Equal(t, 1, s.Failed)
	assert.Equal(t, 1, s.Errors)
	assert.Equal(t, 1, s.Skipped)
	assert.Equal(t, 510*time.Millisecond, s.Duration)
}

func TestReportTableEmpty(t *testing.T) {
	var buf strings.Builder
	err := reportTable(nil, &buf, false, 0)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No tests found")
}

func TestReportTableBasic(t *testing.T) {
	results := []TestResult{
		{Solution: "my-solution", Test: "pass-test", Status: StatusPass, Duration: 150 * time.Millisecond},
		{Solution: "my-solution", Test: "fail-test", Status: StatusFail, Duration: 200 * time.Millisecond, Message: "exit code mismatch"},
	}

	var buf strings.Builder
	err := reportTable(results, &buf, false, 0)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "SOLUTION")
	assert.Contains(t, output, "TEST")
	assert.Contains(t, output, "STATUS")
	assert.Contains(t, output, "PASS")
	assert.Contains(t, output, "FAIL")
	assert.Contains(t, output, "1 passed, 1 failed")
	assert.Contains(t, output, "Failures:")
	assert.Contains(t, output, "exit code mismatch")
}

func TestReportTableVerbose(t *testing.T) {
	results := []TestResult{
		{
			Solution: "sol",
			Test:     "test1",
			Status:   StatusPass,
			Duration: 100 * time.Millisecond,
			Assertions: []AssertionResult{
				{Passed: true},
				{Passed: true},
			},
		},
	}

	var buf strings.Builder
	err := reportTable(results, &buf, true, 0)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "ASSERTIONS")
	assert.Contains(t, output, "(2/2)")
}

func TestStatusIcon(t *testing.T) {
	assert.Equal(t, "PASS", statusIcon(StatusPass))
	assert.Equal(t, "FAIL", statusIcon(StatusFail))
	assert.Equal(t, "SKIP", statusIcon(StatusSkip))
	assert.Equal(t, "ERROR", statusIcon(StatusError))
}

func TestFormatDuration(t *testing.T) {
	assert.Equal(t, "500µs", formatDuration(500*time.Microsecond))
	assert.Equal(t, "150ms", formatDuration(150*time.Millisecond))
	assert.Equal(t, "1.50s", formatDuration(1500*time.Millisecond))
}

func TestFormatAssertionCounts(t *testing.T) {
	assert.Equal(t, "", formatAssertionCounts(nil))
	assert.Equal(t, "(2/3)", formatAssertionCounts([]AssertionResult{
		{Passed: true},
		{Passed: false},
		{Passed: true},
	}))
}

func TestReportListTable(t *testing.T) {
	entries := []listEntry{
		{Solution: "sol1", Test: "test-a", Command: "run", Tags: "smoke", Skip: "false"},
		{Solution: "sol1", Test: "test-b", Command: "lint", Tags: "", Skip: "true"},
	}

	var buf strings.Builder
	err := reportListTable(entries, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "SOLUTION")
	assert.Contains(t, output, "TEST")
	assert.Contains(t, output, "COMMAND")
	assert.Contains(t, output, "TAGS")
	assert.Contains(t, output, "SKIP")
	assert.Contains(t, output, "sol1")
	assert.Contains(t, output, "test-a")
}

func TestReportListTableEmpty(t *testing.T) {
	var buf strings.Builder
	err := reportListTable(nil, &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No tests found")
}

// JUnit tests

func TestWriteJUnitReport(t *testing.T) {
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.xml")

	results := []TestResult{
		{Solution: "sol1", Test: "pass-test", Status: StatusPass, Duration: 100 * time.Millisecond},
		{
			Solution: "sol1", Test: "fail-test", Status: StatusFail, Duration: 200 * time.Millisecond, Message: "assertion failed",
			Assertions: []AssertionResult{
				{Type: "expression", Input: "__exitCode == 0", Passed: false, Message: "got 1"},
			},
		},
		{Solution: "sol1", Test: "error-test", Status: StatusError, Duration: 50 * time.Millisecond, Message: "init failed"},
		{Solution: "sol2", Test: "skip-test", Status: StatusSkip, Duration: 0, Message: "not ready"},
	}

	err := WriteJUnitReport(results, reportPath)
	require.NoError(t, err)

	// Read and parse XML
	data, err := os.ReadFile(reportPath)
	require.NoError(t, err)

	assert.Contains(t, string(data), "<?xml")

	var suites junitTestSuites
	err = xml.Unmarshal(data, &suites)
	require.NoError(t, err)

	assert.Equal(t, 4, suites.Tests)
	assert.Equal(t, 1, suites.Failures)
	assert.Equal(t, 1, suites.Errors)
	assert.Equal(t, 1, suites.Skipped)
	assert.Len(t, suites.TestSuites, 2)

	// First suite (sol1)
	sol1 := suites.TestSuites[0]
	assert.Equal(t, "sol1", sol1.Name)
	assert.Equal(t, 3, sol1.Tests)
	assert.Len(t, sol1.TestCases, 3)

	// Check failure element
	var failCase *junitTestCase
	for i, tc := range sol1.TestCases {
		if tc.Name == "fail-test" {
			failCase = &sol1.TestCases[i]
		}
	}
	require.NotNil(t, failCase)
	require.NotNil(t, failCase.Failure)
	assert.Equal(t, "assertion failed", failCase.Failure.Message)
	assert.Contains(t, failCase.Failure.Body, "__exitCode == 0")

	// Check error element
	var errCase *junitTestCase
	for i, tc := range sol1.TestCases {
		if tc.Name == "error-test" {
			errCase = &sol1.TestCases[i]
		}
	}
	require.NotNil(t, errCase)
	require.NotNil(t, errCase.Error)
	assert.Equal(t, "init failed", errCase.Error.Message)

	// Second suite (sol2)
	sol2 := suites.TestSuites[1]
	assert.Equal(t, "sol2", sol2.Name)
	assert.Len(t, sol2.TestCases, 1)
	require.NotNil(t, sol2.TestCases[0].Skipped)
	assert.Equal(t, "not ready", sol2.TestCases[0].Skipped.Message)
}

func TestWriteJUnitReportCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	deepPath := filepath.Join(tmpDir, "a", "b", "c", "report.xml")

	results := []TestResult{
		{Solution: "sol", Test: "t1", Status: StatusPass, Duration: 10 * time.Millisecond},
	}

	err := WriteJUnitReport(results, deepPath)
	require.NoError(t, err)

	_, err = os.Stat(deepPath)
	assert.NoError(t, err)
}

func TestBuildReportData(t *testing.T) {
	results := []TestResult{
		{Solution: "sol", Test: "t1", Status: StatusPass, Duration: 100 * time.Millisecond},
		{Solution: "sol", Test: "t2", Status: StatusFail, Duration: 200 * time.Millisecond, Message: "failed"},
	}

	data := buildReportData(results, 0)
	assert.Len(t, data.Results, 2)
	assert.Equal(t, 2, data.Summary.Total)
	assert.Equal(t, 1, data.Summary.Passed)
	assert.Equal(t, 1, data.Summary.Failed)

	// Verify it serializes to valid JSON
	jsonBytes, err := json.Marshal(data)
	require.NoError(t, err)
	assert.Contains(t, string(jsonBytes), `"status":"pass"`)
	assert.Contains(t, string(jsonBytes), `"status":"fail"`)
}

func TestBuildFailureBody(t *testing.T) {
	r := TestResult{
		Message: "one or more assertions failed",
		Assertions: []AssertionResult{
			{Type: "expression", Input: "__exitCode == 0", Passed: true},
			{Type: "contains", Input: "hello", Passed: false, Message: "not found in stdout"},
		},
	}

	body := buildFailureBody(r)
	assert.Contains(t, body, "one or more assertions failed")
	assert.Contains(t, body, "Failed assertions:")
	assert.Contains(t, body, "[contains] hello: not found in stdout")
	// Passing assertion should not appear in failure body
	assert.NotContains(t, body, "__exitCode == 0")
}

func TestRunnerGlobalTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	solutionPath := createTestSolution(t, tmpDir, "timeout-sol")

	// Create a builder that blocks until context is cancelled
	builder := func(ioStreams *terminal.IOStreams, exitFunc func(code int)) *cobra.Command {
		cmd := &cobra.Command{Use: "scafctl", SilenceUsage: true}
		runCmd := &cobra.Command{
			Use:                "run",
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, _ []string) error {
				<-cmd.Context().Done()
				return cmd.Context().Err()
			},
		}
		cmd.AddCommand(runCmd)
		return cmd
	}

	ctx := context.Background()
	runner := &Runner{
		GlobalTimeout: 100 * time.Millisecond,
		TestTimeout:   50 * time.Millisecond,
		IOStreams:     &terminal.IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
		NewCommand:    builder,
	}

	exitCodeZero := 0
	solutions := []SolutionTests{
		{
			SolutionName: "timeout-sol",
			FilePath:     solutionPath,
			TestConfig:   &TestConfig{SkipBuiltins: SkipBuiltinsValue{All: true}},
			Tests: map[string]*TestCase{
				"slow-test": {
					Name:     "slow-test",
					Command:  []string{"run"},
					ExitCode: &exitCodeZero,
					Assertions: []Assertion{
						{Expression: "__exitCode == 0"},
					},
				},
			},
		},
	}

	results, err := runner.Run(ctx, solutions)
	require.NoError(t, err)
	require.Len(t, results, 1)
	// Test should fail or error due to timeout
	assert.NotEqual(t, StatusPass, results[0].Status)
}
