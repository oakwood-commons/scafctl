// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/shellexec"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandBuilder is a function that creates a cobra.Command for in-process execution.
// It receives IOStreams and an exit function and returns a configured root command.
// This indirection avoids an import cycle between soltesting and cmd/scafctl.
type CommandBuilder func(ioStreams *terminal.IOStreams, exitFunc func(code int)) *cobra.Command

// Runner is the main test execution engine for functional tests.
type Runner struct {
	// Concurrency is the number of tests to run in parallel. 0 or 1 = sequential.
	Concurrency int
	// FailFast stops remaining tests for a solution on first failure.
	FailFast bool
	// UpdateSnapshots writes actual output to golden files instead of comparing.
	UpdateSnapshots bool
	// Verbose enables extra output (assertion counts, etc.).
	Verbose bool
	// KeepSandbox prevents cleanup of sandbox directories after tests.
	KeepSandbox bool
	// TestTimeout is the default timeout per test.
	TestTimeout time.Duration
	// GlobalTimeout is the overall timeout for all tests.
	GlobalTimeout time.Duration
	// DryRun validates tests without executing commands.
	DryRun bool
	// IOStreams provides input/output streams.
	IOStreams *terminal.IOStreams
	// Filter contains filter options to apply.
	Filter FilterOptions
	// NewCommand builds a root cobra.Command for in-process execution.
	// Must be set before calling Run(). Typically wraps scafctl.Root().
	NewCommand CommandBuilder
}

// Run orchestrates functional test execution across all solutions.
// It returns the results and a non-nil error only for infrastructure failures
// (not test failures — those are reflected in the results).
func (r *Runner) Run(ctx context.Context, solutions []SolutionTests) ([]TestResult, error) {
	if r.GlobalTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.GlobalTimeout)
		defer cancel()
	}

	var allResults []TestResult

	for i := range solutions {
		st := &solutions[i]

		// Generate builtins
		builtins := BuiltinTests(st.TestConfig)
		for _, b := range builtins {
			st.Tests[b.Name] = b
		}

		// Resolve extends
		if err := ResolveExtends(st.Tests); err != nil {
			// All tests in this solution get error status
			for name := range st.Tests {
				allResults = append(allResults, TestResult{
					Solution: st.SolutionName,
					Test:     name,
					Status:   StatusError,
					Message:  fmt.Sprintf("extends resolution failed: %s", err),
				})
			}
			continue
		}

		// Validate all tests (skip builtins — they are generated internally)
		for name, tc := range st.Tests {
			if IsBuiltin(name) {
				continue
			}
			if err := tc.Validate(); err != nil {
				allResults = append(allResults, TestResult{
					Solution: st.SolutionName,
					Test:     name,
					Status:   StatusError,
					Message:  fmt.Sprintf("validation failed: %s", err),
				})
				delete(st.Tests, name)
			}
		}

		// Filter tests
		filtered := FilterTests([]SolutionTests{*st}, r.Filter)
		if len(filtered) == 0 {
			continue
		}
		*st = filtered[0]

		// Sort tests: builtins first, then alphabetical
		testNames := SortedTestNames(*st)

		if r.DryRun {
			for _, name := range testNames {
				tc := st.Tests[name]
				if tc.IsTemplate() {
					continue
				}
				allResults = append(allResults, TestResult{
					Solution: st.SolutionName,
					Test:     name,
					Status:   StatusSkip,
					Message:  "dry run",
				})
			}
			continue
		}

		results, err := r.runSolution(ctx, st, testNames)
		if err != nil {
			return allResults, fmt.Errorf("running solution %q: %w", st.SolutionName, err)
		}
		allResults = append(allResults, results...)
	}

	return allResults, nil
}

// runSolution executes all tests for a single solution.
func (r *Runner) runSolution(ctx context.Context, st *SolutionTests, testNames []string) ([]TestResult, error) {
	solutionDir := filepath.Dir(st.FilePath)

	// Run suite-level setup if configured
	if st.TestConfig != nil && len(st.TestConfig.Setup) > 0 {
		if err := r.runInitSteps(ctx, st.TestConfig.Setup, solutionDir, nil); err != nil {
			// All tests become error status
			results := make([]TestResult, 0, len(testNames))
			for _, name := range testNames {
				results = append(results, TestResult{
					Solution: st.SolutionName,
					Test:     name,
					Status:   StatusError,
					Message:  fmt.Sprintf("suite setup failed: %s", err),
				})
			}
			return results, nil
		}
	}

	// Ensure suite-level cleanup runs after all tests
	defer func() {
		if st.TestConfig != nil && len(st.TestConfig.Cleanup) > 0 {
			_ = r.runInitSteps(context.Background(), st.TestConfig.Cleanup, solutionDir, nil)
		}
	}()

	concurrency := r.Concurrency
	if concurrency <= 1 {
		concurrency = 1
	}

	results := make([]TestResult, 0, len(testNames))
	var mu sync.Mutex
	var failFastTriggered bool

	if concurrency == 1 {
		// Sequential execution
		for _, name := range testNames {
			if err := ctx.Err(); err != nil {
				results = append(results, TestResult{
					Solution: st.SolutionName,
					Test:     name,
					Status:   StatusError,
					Message:  "context cancelled",
				})
				continue
			}

			if failFastTriggered {
				results = append(results, TestResult{
					Solution: st.SolutionName,
					Test:     name,
					Status:   StatusSkip,
					Message:  "skipped due to --fail-fast",
				})
				continue
			}

			result := r.runTestWithRetries(ctx, st, name, solutionDir)
			results = append(results, result)

			if r.FailFast && (result.Status == StatusFail || result.Status == StatusError) {
				failFastTriggered = true
			}
		}
	} else {
		// Concurrent execution with semaphore
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup

		for _, name := range testNames {
			if err := ctx.Err(); err != nil {
				mu.Lock()
				results = append(results, TestResult{
					Solution: st.SolutionName,
					Test:     name,
					Status:   StatusError,
					Message:  "context cancelled",
				})
				mu.Unlock()
				continue
			}

			mu.Lock()
			skip := failFastTriggered
			mu.Unlock()
			if skip {
				mu.Lock()
				results = append(results, TestResult{
					Solution: st.SolutionName,
					Test:     name,
					Status:   StatusSkip,
					Message:  "skipped due to --fail-fast",
				})
				mu.Unlock()
				continue
			}

			wg.Add(1)
			sem <- struct{}{}
			go func(testName string) {
				defer wg.Done()
				defer func() { <-sem }()

				result := r.runTestWithRetries(ctx, st, testName, solutionDir)

				mu.Lock()
				results = append(results, result)
				if r.FailFast && (result.Status == StatusFail || result.Status == StatusError) {
					failFastTriggered = true
				}
				mu.Unlock()
			}(name)
		}

		wg.Wait()
	}

	return results, nil
}

// runTestWithRetries runs a test, retrying on failure up to tc.Retries times.
func (r *Runner) runTestWithRetries(ctx context.Context, st *SolutionTests, name, solutionDir string) TestResult {
	tc := st.Tests[name]
	if tc == nil {
		return TestResult{
			Solution: st.SolutionName,
			Test:     name,
			Status:   StatusError,
			Message:  "test case not found",
		}
	}

	maxAttempts := 1
	if tc.Retries > 0 {
		maxAttempts = tc.Retries + 1
	}

	var lastResult TestResult
	for attempt := range maxAttempts {
		result := r.executeTest(ctx, tc, st, solutionDir)
		result.RetryAttempt = attempt

		if result.Status == StatusPass || result.Status == StatusSkip {
			return result
		}

		lastResult = result
	}

	return lastResult
}

// executeTest runs a single test case in an isolated sandbox.
func (r *Runner) executeTest(ctx context.Context, tc *TestCase, st *SolutionTests, solutionDir string) TestResult {
	start := time.Now()

	result := TestResult{
		Solution: st.SolutionName,
		Test:     tc.Name,
	}

	// Check skip conditions
	if tc.Skip {
		result.Status = StatusSkip
		result.Message = tc.SkipReason
		if result.Message == "" {
			result.Message = "test is skipped"
		}
		result.Duration = time.Since(start)
		return result
	}

	if tc.SkipExpression != "" {
		skipped, err := r.evaluateSkipExpression(ctx, string(tc.SkipExpression))
		if err != nil {
			result.Status = StatusError
			result.Message = fmt.Sprintf("skip expression error: %s", err)
			result.Duration = time.Since(start)
			return result
		}
		if skipped {
			result.Status = StatusSkip
			result.Message = tc.SkipReason
			if result.Message == "" {
				result.Message = fmt.Sprintf("skip expression: %s", tc.SkipExpression)
			}
			result.Duration = time.Since(start)
			return result
		}
	}

	// Handle builtin:parse specially — no command execution needed
	if tc.Name == BuiltinName(BuiltinParse) {
		result.Status = StatusPass
		result.Duration = time.Since(start)
		return result
	}

	// Need at least a command to execute
	if len(tc.Command) == 0 {
		result.Status = StatusError
		result.Message = "no command specified"
		result.Duration = time.Since(start)
		return result
	}

	// Create sandbox
	sandbox, err := NewSandbox(st.FilePath, nil, tc.Files)
	if err != nil {
		result.Status = StatusError
		result.Message = fmt.Sprintf("sandbox creation failed: %s", err)
		result.Duration = time.Since(start)
		return result
	}
	if !r.KeepSandbox {
		defer sandbox.Cleanup()
	} else {
		result.SandboxPath = sandbox.Path()
	}

	// Run test init steps
	if len(tc.Init) > 0 {
		envMap := r.buildEnvMap(tc, st.TestConfig, sandbox.Path())
		if err := r.runInitSteps(ctx, tc.Init, sandbox.Path(), envMap); err != nil {
			result.Status = StatusError
			result.Message = fmt.Sprintf("init step failed: %s", err)
			result.Duration = time.Since(start)
			return result
		}
	}

	// Take pre-snapshot for file diff tracking
	if err := sandbox.PreSnapshot(); err != nil {
		result.Status = StatusError
		result.Message = fmt.Sprintf("pre-snapshot failed: %s", err)
		result.Duration = time.Since(start)
		return result
	}

	// Build and execute the scafctl command in-process
	cmdOutput, err := r.executeCommand(ctx, tc, st, sandbox)
	if err != nil {
		result.Status = StatusError
		result.Message = fmt.Sprintf("command execution failed: %s", err)
		result.Duration = time.Since(start)
		return result
	}

	// Collect file diffs
	fileDiffs, err := sandbox.PostSnapshot()
	if err != nil {
		result.Status = StatusError
		result.Message = fmt.Sprintf("post-snapshot failed: %s", err)
		result.Duration = time.Since(start)
		return result
	}
	cmdOutput.Files = fileDiffs

	// Check exit code
	if !r.checkExitCode(tc, cmdOutput) {
		result.Status = StatusFail
		switch {
		case tc.ExpectFailure:
			result.Message = fmt.Sprintf("expected non-zero exit code, got %d", cmdOutput.ExitCode)
		case tc.ExitCode != nil:
			result.Message = fmt.Sprintf("expected exit code %d, got %d", *tc.ExitCode, cmdOutput.ExitCode)
		default:
			result.Message = fmt.Sprintf("command failed with exit code %d", cmdOutput.ExitCode)
		}
		result.Duration = time.Since(start)
		return result
	}

	// Snapshot comparison
	if tc.Snapshot != "" {
		snapshotPath := filepath.Join(solutionDir, tc.Snapshot)
		if r.UpdateSnapshots {
			if err := UpdateSnapshot(cmdOutput.Stdout, snapshotPath, sandbox.Path()); err != nil {
				result.Status = StatusError
				result.Message = fmt.Sprintf("snapshot update failed: %s", err)
				result.Duration = time.Since(start)
				return result
			}
		} else {
			match, diff, err := CompareSnapshot(cmdOutput.Stdout, snapshotPath, sandbox.Path())
			if err != nil {
				result.Status = StatusError
				result.Message = fmt.Sprintf("snapshot comparison failed: %s", err)
				result.Duration = time.Since(start)
				return result
			}
			if !match {
				result.Status = StatusFail
				result.Message = fmt.Sprintf("snapshot mismatch:\n%s", diff)
				result.Duration = time.Since(start)
				return result
			}
		}
	}

	// Evaluate assertions
	if len(tc.Assertions) > 0 {
		assertResults := EvaluateAssertions(ctx, tc.Assertions, cmdOutput)
		result.Assertions = assertResults

		for _, ar := range assertResults {
			if !ar.Passed {
				result.Status = StatusFail
				result.Message = "one or more assertions failed"
				result.Duration = time.Since(start)
				return result
			}
		}
	}

	// Run test cleanup steps
	if len(tc.Cleanup) > 0 {
		envMap := r.buildEnvMap(tc, st.TestConfig, sandbox.Path())
		// Cleanup errors are not test failures, just log them
		_ = r.runInitSteps(ctx, tc.Cleanup, sandbox.Path(), envMap)
	}

	result.Status = StatusPass
	result.Duration = time.Since(start)
	return result
}

// executeCommand builds and runs a scafctl CLI command in-process.
func (r *Runner) executeCommand(ctx context.Context, tc *TestCase, st *SolutionTests, sandbox *Sandbox) (*CommandOutput, error) {
	timeout := r.TestTimeout
	if tc.Timeout != nil {
		timeout = tc.Timeout.Duration
	}

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Build command args
	args := make([]string, 0, len(tc.Command)+len(tc.Args)+4)
	args = append(args, tc.Command...)
	args = append(args, tc.Args...)

	// Auto-inject -f <sandbox-solution-path> unless injectFile is false
	if tc.GetInjectFile() {
		args = append(args, "-f", sandbox.SolutionPath())
	}

	// Capture stdout/stderr
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{
		In:           os.Stdin,
		Out:          &stdout,
		ErrOut:       &stderr,
		ColorEnabled: false,
	}

	// Capture exit code via ExitFunc
	var capturedExitCode int
	exitFunc := func(code int) {
		capturedExitCode = code
	}

	// Build environment
	envMap := r.buildEnvMap(tc, st.TestConfig, sandbox.Path())

	// Set env vars for the command
	for k, v := range envMap {
		strVal := fmt.Sprintf("%v", v)
		os.Setenv(k, strVal) //nolint:errcheck // env vars for test isolation
		defer os.Unsetenv(k) //nolint:errcheck // cleanup env vars after test
	}

	// Create root command with isolated IOStreams
	rootCmd := r.NewCommand(ioStreams, exitFunc)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)

	// Execute command
	cmdErr := rootCmd.ExecuteContext(ctx)

	// Determine exit code
	exitCode := capturedExitCode
	if cmdErr != nil && exitCode == 0 {
		exitCode = 1
	}

	cmdOutput := &CommandOutput{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}

	// Try to parse JSON from stdout
	var parsed map[string]any
	if err := json.Unmarshal([]byte(cmdOutput.Stdout), &parsed); err == nil {
		cmdOutput.Output = parsed
	}

	return cmdOutput, nil
}

// checkExitCode validates the exit code against expectations.
func (r *Runner) checkExitCode(tc *TestCase, output *CommandOutput) bool {
	switch {
	case tc.ExpectFailure:
		return output.ExitCode != 0
	case tc.ExitCode != nil:
		return output.ExitCode == *tc.ExitCode
	default:
		// Default: expect success unless assertions explicitly check exit code
		if len(tc.Assertions) > 0 {
			return true // let assertions handle it
		}
		return output.ExitCode == 0
	}
}

// evaluateSkipExpression evaluates a CEL skip expression.
func (r *Runner) evaluateSkipExpression(ctx context.Context, expr string) (bool, error) {
	envVars := map[string]any{
		"os":   os.Getenv("GOOS"),
		"arch": os.Getenv("GOARCH"),
	}

	result, err := celexp.EvaluateExpression(ctx, expr, nil, envVars)
	if err != nil {
		return false, err
	}

	boolResult, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("skip expression must return bool, got %T", result)
	}

	return boolResult, nil
}

// runInitSteps executes init or cleanup steps using shellexec.
func (r *Runner) runInitSteps(ctx context.Context, steps []InitStep, workDir string, envMap map[string]any) error {
	for i, step := range steps {
		timeout := time.Duration(step.Timeout) * time.Second
		if timeout == 0 {
			timeout = 30 * time.Second // default init step timeout
		}

		stepCtx, cancel := context.WithTimeout(ctx, timeout)

		shell := shellexec.ShellType(step.Shell)
		if shell == "" {
			shell = shellexec.ShellAuto
		}

		dir := workDir
		if step.WorkingDir != "" {
			if filepath.IsAbs(step.WorkingDir) {
				dir = step.WorkingDir
			} else {
				dir = filepath.Join(workDir, step.WorkingDir)
			}
		}

		// Merge environment
		mergedEnv := mergeEnvForStep(envMap, step.Env)

		opts := &shellexec.RunOptions{
			Command: step.Command,
			Args:    step.Args,
			Shell:   shell,
			Dir:     dir,
			Env:     mergedEnv,
		}

		if step.Stdin != "" {
			opts.Stdin = strings.NewReader(step.Stdin)
		}

		// Capture output for diagnostics
		var stepStdout, stepStderr bytes.Buffer
		opts.Stdout = &stepStdout
		opts.Stderr = &stepStderr

		result, err := shellexec.Run(stepCtx, opts)
		cancel()

		if err != nil {
			return fmt.Errorf("init step %d (%q) failed: %w (stderr: %s)", i, step.Command, err, stepStderr.String())
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("init step %d (%q) exited with code %d (stderr: %s)", i, step.Command, result.ExitCode, stepStderr.String())
		}
	}

	return nil
}

// buildEnvMap builds the environment variable map for a test.
// Precedence: process env → testConfig.env → testCase.env → SCAFCTL_SANDBOX_DIR.
func (r *Runner) buildEnvMap(tc *TestCase, testConfig *TestConfig, sandboxPath string) map[string]any {
	env := make(map[string]any)

	// testConfig.env
	if testConfig != nil {
		for k, v := range testConfig.Env {
			env[k] = v
		}
	}

	// testCase.env overrides
	for k, v := range tc.Env {
		env[k] = v
	}

	// Always set sandbox dir
	env["SCAFCTL_SANDBOX_DIR"] = sandboxPath

	return env
}

// mergeEnvForStep creates an env slice for shellexec combining the env map with step-level env.
func mergeEnvForStep(envMap map[string]any, stepEnv map[string]string) []string {
	combined := make(map[string]any, len(envMap)+len(stepEnv))
	for k, v := range envMap {
		combined[k] = v
	}
	for k, v := range stepEnv {
		combined[k] = v
	}

	if len(combined) == 0 {
		return nil
	}

	return shellexec.MergeEnv(combined)
}
