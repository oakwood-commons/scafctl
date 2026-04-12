// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPreflight_LintErrors_BlockBuild(t *testing.T) {
	t.Parallel()

	// A solution with no resolvers and no workflow triggers the
	// "empty-solution" lint error.
	sol := &solution.Solution{}
	sol.ApplyDefaults()

	result, err := RunPreflight(context.Background(), sol, "test.yaml", PreflightOptions{
		SkipTests: true,
		Logger:    logr.Discard(),
	})

	require.NoError(t, err)
	assert.True(t, result.Blocked, "build should be blocked when lint has errors")
	assert.NotNil(t, result.LintResult)
	assert.NotEmpty(t, result.LintResult.Findings, "lint should report at least one finding")
}

func TestRunPreflight_LintErrors_IgnorePreflight(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{}
	sol.ApplyDefaults()

	result, err := RunPreflight(context.Background(), sol, "test.yaml", PreflightOptions{
		SkipTests:       true,
		IgnorePreflight: true,
		Logger:          logr.Discard(),
	})

	require.NoError(t, err)
	assert.False(t, result.Blocked, "build should not be blocked when --ignore-preflight is set")
	assert.NotEmpty(t, result.LintResult.Findings,
		"lint errors should still be reported")
	assert.Contains(t, result.Messages[0], "ignored, preflight errors overridden")
}

func TestRunPreflight_SkipLint(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{}
	sol.ApplyDefaults()

	result, err := RunPreflight(context.Background(), sol, "test.yaml", PreflightOptions{
		SkipLint:  true,
		SkipTests: true,
		Logger:    logr.Discard(),
	})

	require.NoError(t, err)
	assert.False(t, result.Blocked)
	assert.Nil(t, result.LintResult)
	assert.Contains(t, result.Messages[0], "lint: skipped")
}

func TestRunPreflight_SkipTests(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{}
	sol.ApplyDefaults()

	result, err := RunPreflight(context.Background(), sol, "test.yaml", PreflightOptions{
		SkipLint:  true,
		SkipTests: true,
		Logger:    logr.Discard(),
	})

	require.NoError(t, err)
	assert.True(t, result.TestsPassed)
	assert.Contains(t, result.Messages[1], "tests: skipped")
}

func TestRunPreflight_NoTests_Defined(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{}
	sol.ApplyDefaults()

	result, err := RunPreflight(context.Background(), sol, "test.yaml", PreflightOptions{
		SkipLint: true,
		Logger:   logr.Discard(),
	})

	require.NoError(t, err)
	assert.True(t, result.TestsPassed)
	assert.Contains(t, result.Messages[1], "tests: no tests defined")
}

func TestRunPreflight_LintClean(t *testing.T) {
	t.Parallel()

	// Create a minimal valid solution to pass lint.
	sol := buildMinimalValidSolution(t)

	result, err := RunPreflight(context.Background(), sol, "test.yaml", PreflightOptions{
		SkipTests: true,
		Logger:    logr.Discard(),
	})

	require.NoError(t, err)
	assert.False(t, result.Blocked)
	assert.NotNil(t, result.LintResult)
	assert.Equal(t, 0, result.LintResult.ErrorCount)
}

func TestRunPreflight_AllSkipped_NotBlocked(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{}
	sol.ApplyDefaults()

	result, err := RunPreflight(context.Background(), sol, "test.yaml", PreflightOptions{
		SkipLint:  true,
		SkipTests: true,
		Logger:    logr.Discard(),
	})

	require.NoError(t, err)
	assert.False(t, result.Blocked)
	assert.Len(t, result.Messages, 2)
}

func TestRunPreflight_LintWarnings_NotBlocked(t *testing.T) {
	t.Parallel()

	// Construct a result scenario: warnings only don't block.
	// We test the logic by running with a valid solution that may produce warnings.
	sol := buildMinimalValidSolution(t)

	result, err := RunPreflight(context.Background(), sol, "test.yaml", PreflightOptions{
		SkipTests: true,
		Logger:    logr.Discard(),
	})

	require.NoError(t, err)
	assert.False(t, result.Blocked, "warnings should not block the build")
}

func TestRunPreflight_NilLogger(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{}
	sol.ApplyDefaults()

	// Zero-value logr.Logger (no sink) should not panic.
	result, err := RunPreflight(context.Background(), sol, "test.yaml", PreflightOptions{
		SkipTests: true,
	})

	require.NoError(t, err)
	assert.True(t, result.Blocked, "empty solution should still block")
}

func TestRunPreflight_EmptyBinaryPath_TestsDefined(t *testing.T) {
	t.Parallel()

	yaml := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec:
  resolvers:
    greeting:
      value: hello
  testing:
    cases:
      basic:
        resolvers:
          greeting: world
`
	var sol solution.Solution
	err := sol.LoadFromBytes([]byte(yaml))
	require.NoError(t, err)

	result, err := RunPreflight(context.Background(), &sol, "test.yaml", PreflightOptions{
		SkipLint:   true,
		BinaryPath: "", // Empty -- should report "skipped (no binary path)"
		Logger:     logr.Discard(),
	})

	require.NoError(t, err)
	assert.True(t, result.TestsPassed)
	assert.Contains(t, result.Messages[1], "tests: skipped (no binary path)")
}

func TestCountBySeverity_Empty(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, countBySeverity(nil, lint.SeverityError))
	assert.Equal(t, 0, countBySeverity([]*lint.Finding{}, lint.SeverityWarning))
}

func TestCountBySeverity_Mixed(t *testing.T) {
	t.Parallel()

	findings := []*lint.Finding{
		{Severity: lint.SeverityError},
		{Severity: lint.SeverityWarning},
		{Severity: lint.SeverityError},
		{Severity: lint.SeverityWarning},
		{Severity: lint.SeverityWarning},
	}

	assert.Equal(t, 2, countBySeverity(findings, lint.SeverityError))
	assert.Equal(t, 3, countBySeverity(findings, lint.SeverityWarning))
}

// buildMinimalValidSolution creates a solution that passes lint with zero errors.
func buildMinimalValidSolution(t *testing.T) *solution.Solution {
	t.Helper()

	yaml := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec:
  resolvers:
    greeting:
      value: hello
`
	var sol solution.Solution
	err := sol.LoadFromBytes([]byte(yaml))
	require.NoError(t, err)

	// Verify it passes lint so the test is meaningful.
	lintResult := lint.Solution(&sol, "test.yaml", nil)
	require.Equal(t, 0, lintResult.ErrorCount,
		"test fixture should pass lint; findings: %v", lintResult.Findings)

	return &sol
}

func BenchmarkRunPreflight(b *testing.B) {
	yaml := `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bench-solution
  version: 1.0.0
spec:
  resolvers:
    greeting:
      value: hello
`
	var sol solution.Solution
	if err := sol.LoadFromBytes([]byte(yaml)); err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	opts := PreflightOptions{
		SkipTests: true,
		Logger:    logr.Discard(),
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = RunPreflight(ctx, &sol, "bench.yaml", opts)
	}
}
