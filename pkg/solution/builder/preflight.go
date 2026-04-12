// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
)

// PreflightOptions controls which pre-flight checks run before a build.
type PreflightOptions struct {
	// SkipLint skips the lint pre-flight check entirely.
	SkipLint bool `json:"skipLint,omitempty" yaml:"skipLint,omitempty" doc:"Skip lint pre-flight check"`

	// SkipTests skips the functional test pre-flight check entirely.
	SkipTests bool `json:"skipTests,omitempty" yaml:"skipTests,omitempty" doc:"Skip functional test pre-flight check"`

	// IgnorePreflight runs checks but treats errors as warnings instead of blocking.
	IgnorePreflight bool `json:"ignorePreflight,omitempty" yaml:"ignorePreflight,omitempty" doc:"Run checks but treat errors as warnings"`

	// BinaryPath is the CLI executable path used for test execution.
	// When empty, functional test pre-flight checks are skipped.
	BinaryPath string `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty" doc:"Binary path for test execution"`

	// Registry is the provider registry for lint to validate against.
	// When nil, lint skips provider-specific checks.
	Registry *provider.Registry `json:"-" yaml:"-"`

	// Logger is used for structured logging.
	Logger logr.Logger
}

// PreflightResult holds the outcome of pre-flight validation.
type PreflightResult struct {
	// LintResult contains lint findings. Nil if lint was skipped.
	LintResult *lint.Result `json:"lintResult,omitempty" yaml:"lintResult,omitempty" doc:"Lint findings"`

	// TestsPassed is true if tests passed or were skipped.
	TestsPassed bool `json:"testsPassed" yaml:"testsPassed" doc:"Whether tests passed or were skipped"`

	// Blocked is true if the build should be aborted.
	Blocked bool `json:"blocked" yaml:"blocked" doc:"Whether the build should be aborted"`

	// Messages collects human-readable status lines for display.
	Messages []string `json:"messages,omitempty" yaml:"messages,omitempty" doc:"Human-readable status lines"`
}

// RunPreflight executes lint and (optionally) functional tests against the
// parsed solution. Returns a result indicating whether the build should proceed.
func RunPreflight(ctx context.Context, sol *solution.Solution, filePath string, opts PreflightOptions) (*PreflightResult, error) {
	if opts.Logger.GetSink() == nil {
		opts.Logger = logr.Discard()
	}

	result := &PreflightResult{
		TestsPassed: true,
	}

	// --- Lint ---
	if !opts.SkipLint {
		opts.Logger.V(1).Info("running pre-flight lint check")
		lintResult := lint.Solution(sol, filePath, opts.Registry)
		result.LintResult = lintResult

		// Count errors from findings (Result.ErrorCount may be zero for early-return paths).
		lintErrors := lintResult.ErrorCount
		if lintErrors == 0 {
			for _, f := range lintResult.Findings {
				if f.Severity == lint.SeverityError {
					lintErrors++
				}
			}
		}

		warnings := lintResult.WarnCount
		if warnings == 0 {
			warnings = countBySeverity(lintResult.Findings, lint.SeverityWarning)
		}

		switch {
		case lintErrors > 0:
			msg := fmt.Sprintf("lint: %d error(s), %d warning(s)", lintErrors, warnings)
			if opts.IgnorePreflight {
				result.Messages = append(result.Messages, msg+" (ignored, preflight errors overridden)")
			} else {
				result.Messages = append(result.Messages, msg)
				result.Blocked = true
			}
		case warnings > 0:
			result.Messages = append(result.Messages,
				fmt.Sprintf("lint: %d warning(s)", warnings))
		default:
			result.Messages = append(result.Messages, "lint: passed")
		}
	} else {
		result.Messages = append(result.Messages, "lint: skipped")
	}

	// --- Functional Tests ---
	switch {
	case opts.SkipTests:
		result.Messages = append(result.Messages, "tests: skipped")
	case !sol.Spec.HasTests():
		result.Messages = append(result.Messages, "tests: no tests defined")
	case opts.BinaryPath == "":
		result.Messages = append(result.Messages, "tests: skipped (no binary path)")
	default:
		opts.Logger.V(1).Info("running pre-flight functional tests")

		tests, err := soltesting.DiscoverFromFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("pre-flight test discovery: %w", err)
		}

		if tests != nil && len(tests.Cases) > 0 {
			ios := terminal.NewIOStreams(io.NopCloser(bytes.NewReader(nil)), io.Discard, io.Discard, false)
			runner := &soltesting.Runner{
				BinaryPath:  opts.BinaryPath,
				Concurrency: 1,
				FailFast:    true,
				IOStreams:   ios,
			}

			results, err := runner.Run(ctx, []soltesting.SolutionTests{*tests})
			if err != nil {
				return nil, fmt.Errorf("pre-flight tests: %w", err)
			}

			var failures int
			for _, tr := range results {
				if tr.Status == soltesting.StatusFail || tr.Status == soltesting.StatusError {
					failures++
				}
			}

			if failures > 0 {
				result.TestsPassed = false
				msg := fmt.Sprintf("tests: %d of %d failed", failures, len(results))
				if opts.IgnorePreflight {
					result.Messages = append(result.Messages, msg+" (ignored, preflight errors overridden)")
				} else {
					result.Messages = append(result.Messages, msg)
					result.Blocked = true
				}
			} else {
				result.Messages = append(result.Messages,
					fmt.Sprintf("tests: %d passed", len(results)))
			}
		} else {
			result.Messages = append(result.Messages, "tests: no test cases found")
		}
	}

	return result, nil
}

// countBySeverity counts findings at a given severity level.
func countBySeverity(findings []*lint.Finding, severity lint.SeverityLevel) int {
	count := 0
	for _, f := range findings {
		if f.Severity == severity {
			count++
		}
	}
	return count
}
