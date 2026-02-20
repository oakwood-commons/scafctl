// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
)

// ResultSummary holds aggregated counts for reporting.
type ResultSummary struct {
	Passed       int           `json:"passed"`
	Failed       int           `json:"failed"`
	Errors       int           `json:"errors"`
	Skipped      int           `json:"skipped"`
	Total        int           `json:"total"`
	Duration     time.Duration `json:"duration"`
	WallDuration time.Duration `json:"wallDuration,omitempty"`
}

// ElapsedDuration returns WallDuration if set, otherwise falls back to
// the summed individual Duration. Use this for summary display so that
// parallel runs show wall-clock time instead of cumulative CPU time.
func (s ResultSummary) ElapsedDuration() time.Duration {
	if s.WallDuration > 0 {
		return s.WallDuration
	}
	return s.Duration
}

// Summarize computes a ResultSummary from a slice of TestResults.
func Summarize(results []TestResult) ResultSummary {
	var s ResultSummary
	s.Total = len(results)
	for _, r := range results {
		s.Duration += r.Duration
		switch r.Status {
		case StatusPass:
			s.Passed++
		case StatusFail:
			s.Failed++
		case StatusError:
			s.Errors++
		case StatusSkip:
			s.Skipped++
		}
	}
	return s
}

// ReportResults formats and writes test results using the given output options.
// For table format it writes a human-readable table with summary.
// For JSON/YAML it delegates to kvx.OutputOptions.Write.
// For quiet format it writes nothing.
// The elapsed parameter, when > 0, overrides the summed individual durations
// in the summary line with the actual wall-clock time.
func ReportResults(results []TestResult, opts *kvx.OutputOptions, verbose bool, elapsed time.Duration) error {
	switch {
	case kvx.IsQuietFormat(opts.Format):
		return nil
	case kvx.IsKvxFormat(opts.Format):
		return reportTable(results, opts.IOStreams.Out, verbose, elapsed)
	default:
		// JSON / YAML
		return opts.Write(buildReportData(results, elapsed))
	}
}

// reportData is the structured output for JSON/YAML.
type reportData struct {
	Results []testResultOutput `json:"results"`
	Summary ResultSummary      `json:"summary"`
}

// testResultOutput is the JSON/YAML representation of a single test result.
type testResultOutput struct {
	Solution     string            `json:"solution"`
	Test         string            `json:"test"`
	Status       Status            `json:"status"`
	Duration     string            `json:"duration"`
	Message      string            `json:"message,omitempty"`
	Assertions   []assertionOutput `json:"assertions,omitempty"`
	RetryAttempt int               `json:"retryAttempt,omitempty"`
	SandboxPath  string            `json:"sandboxPath,omitempty"`
}

// assertionOutput is the JSON/YAML representation of an assertion result.
type assertionOutput struct {
	Type    string `json:"type"`
	Input   string `json:"input"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

func buildReportData(results []TestResult, elapsed time.Duration) reportData {
	summary := Summarize(results)
	summary.WallDuration = elapsed
	outputs := make([]testResultOutput, 0, len(results))
	for _, r := range results {
		out := testResultOutput{
			Solution:     r.Solution,
			Test:         r.Test,
			Status:       r.Status,
			Duration:     formatDuration(r.Duration),
			Message:      r.Message,
			RetryAttempt: r.RetryAttempt,
			SandboxPath:  r.SandboxPath,
		}
		for _, a := range r.Assertions {
			out.Assertions = append(out.Assertions, assertionOutput{
				Type:    a.Type,
				Input:   a.Input,
				Passed:  a.Passed,
				Message: a.Message,
			})
		}
		outputs = append(outputs, out)
	}
	return reportData{
		Results: outputs,
		Summary: summary,
	}
}

// reportTable writes a human-readable table to w.
func reportTable(results []TestResult, w io.Writer, verbose bool, elapsed time.Duration) error {
	if len(results) == 0 {
		fmt.Fprintln(w, "No tests found.")
		return nil
	}

	// Compute column widths
	solW, testW := 8, 4 // min widths for "SOLUTION" and "TEST"
	for _, r := range results {
		if len(r.Solution) > solW {
			solW = len(r.Solution)
		}
		if len(r.Test) > testW {
			testW = len(r.Test)
		}
	}

	// Header
	if verbose {
		fmt.Fprintf(w, "%-*s  %-*s  %-8s  %-10s  %s\n", solW, "SOLUTION", testW, "TEST", "STATUS", "DURATION", "ASSERTIONS")
	} else {
		fmt.Fprintf(w, "%-*s  %-*s  %-8s  %s\n", solW, "SOLUTION", testW, "TEST", "STATUS", "DURATION")
	}

	// Rows
	for _, r := range results {
		status := statusIcon(r.Status)
		dur := formatDuration(r.Duration)

		if verbose {
			assertions := formatAssertionCounts(r.Assertions)
			fmt.Fprintf(w, "%-*s  %-*s  %-8s  %-10s  %s\n", solW, r.Solution, testW, r.Test, status, dur, assertions)
		} else {
			fmt.Fprintf(w, "%-*s  %-*s  %-8s  %s\n", solW, r.Solution, testW, r.Test, status, dur)
		}
	}

	// Failure/error details
	var failures []TestResult
	for _, r := range results {
		if r.Status == StatusFail || r.Status == StatusError {
			failures = append(failures, r)
		}
	}

	if len(failures) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Failures:")
		for _, f := range failures {
			fmt.Fprintf(w, "  %s/%s: %s\n", f.Solution, f.Test, f.Message)
			for _, a := range f.Assertions {
				if !a.Passed {
					fmt.Fprintf(w, "    [%s] %s: %s\n", a.Type, a.Input, a.Message)
				}
			}
		}
	}

	// Summary
	summary := Summarize(results)
	summary.WallDuration = elapsed
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%d passed, %d failed, %d errors, %d skipped (%s)\n",
		summary.Passed, summary.Failed, summary.Errors, summary.Skipped,
		formatDuration(summary.ElapsedDuration()))

	return nil
}

// listEntry holds test information for list display.
type listEntry struct {
	Solution string `json:"solution"`
	Test     string `json:"test"`
	Command  string `json:"command"`
	Tags     string `json:"tags"`
	Skip     string `json:"skip"`
}

// ReportList formats and writes test discovery results.
func ReportList(solutions []SolutionTests, opts *kvx.OutputOptions, includeBuiltins bool) error {
	var entries []listEntry
	for _, st := range solutions {
		if includeBuiltins {
			builtins := BuiltinTests(st.Config)
			for _, b := range builtins {
				entries = append(entries, listEntry{
					Solution: st.SolutionName,
					Test:     b.Name,
					Command:  strings.Join(b.Command, " "),
					Skip:     "false",
				})
			}
		}

		names := SortedTestNames(st)
		for _, name := range names {
			tc := st.Cases[name]
			if tc.IsTemplate() {
				continue
			}
			if IsBuiltin(name) {
				continue // already handled above
			}

			skip := "false"
			if tc.Skip {
				skip = "true"
			}
			if tc.SkipExpression != "" {
				skip = string(tc.SkipExpression)
			}

			entries = append(entries, listEntry{
				Solution: st.SolutionName,
				Test:     name,
				Command:  strings.Join(tc.Command, " "),
				Tags:     strings.Join(tc.Tags, ", "),
				Skip:     skip,
			})
		}
	}

	switch {
	case kvx.IsQuietFormat(opts.Format):
		return nil
	case kvx.IsKvxFormat(opts.Format):
		return reportListTable(entries, opts.IOStreams.Out)
	default:
		return opts.Write(entries)
	}
}

func reportListTable(entries []listEntry, w io.Writer) error {
	if len(entries) == 0 {
		fmt.Fprintln(w, "No tests found.")
		return nil
	}

	// Compute column widths
	solW, testW, cmdW, tagW := 8, 4, 7, 4
	for _, le := range entries {
		if len(le.Solution) > solW {
			solW = len(le.Solution)
		}
		if len(le.Test) > testW {
			testW = len(le.Test)
		}
		if len(le.Command) > cmdW {
			cmdW = len(le.Command)
		}
		if len(le.Tags) > tagW {
			tagW = len(le.Tags)
		}
	}

	fmt.Fprintf(w, "%-*s  %-*s  %-*s  %-*s  %s\n", solW, "SOLUTION", testW, "TEST", cmdW, "COMMAND", tagW, "TAGS", "SKIP")
	for _, le := range entries {
		fmt.Fprintf(w, "%-*s  %-*s  %-*s  %-*s  %s\n", solW, le.Solution, testW, le.Test, cmdW, le.Command, tagW, le.Tags, le.Skip)
	}
	return nil
}

// statusIcon returns a human-readable status string.
func statusIcon(s Status) string {
	switch s {
	case StatusPass:
		return "PASS"
	case StatusFail:
		return "FAIL"
	case StatusSkip:
		return "SKIP"
	case StatusError:
		return "ERROR"
	default:
		return string(s)
	}
}

// formatDuration formats a duration as a human-readable string.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

// formatAssertionCounts returns a string like "(3/5)" showing passed/total assertions.
func formatAssertionCounts(assertions []AssertionResult) string {
	if len(assertions) == 0 {
		return ""
	}
	passed := 0
	for _, a := range assertions {
		if a.Passed {
			passed++
		}
	}
	return fmt.Sprintf("(%d/%d)", passed, len(assertions))
}
