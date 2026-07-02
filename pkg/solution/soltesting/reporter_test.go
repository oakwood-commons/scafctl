// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"bytes"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProgress implements TestProgressCallback for testing.
type stubProgress struct{}

func (s *stubProgress) OnTestStart(_, _ string)     {}
func (s *stubProgress) OnTestComplete(_ TestResult) {}
func (s *stubProgress) Wait()                       {}

func newTestOutputOpts(buf *bytes.Buffer, format kvx.OutputFormat) *kvx.OutputOptions {
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	opts := kvx.NewOutputOptions(ioStreams)
	opts.Format = format
	return opts
}

func sampleResults() []TestResult {
	return []TestResult{
		{Solution: "sol-a", Test: "test-1", Status: StatusPass, Duration: 100 * time.Millisecond},
		{Solution: "sol-a", Test: "test-2", Status: StatusFail, Duration: 50 * time.Millisecond, Message: "assertion failed"},
		{Solution: "sol-b", Test: "test-3", Status: StatusSkip, Duration: 0},
	}
}

func TestReportResults_QuietFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := newTestOutputOpts(&buf, kvx.OutputFormatQuiet)

	err := ReportResults(sampleResults(), opts, false, time.Second, nil)
	require.NoError(t, err)
	assert.Empty(t, buf.String(), "quiet format should produce no output")
}

func TestReportResults_RelaxedStatus(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := newTestOutputOpts(&buf, kvx.OutputFormatTable)

	results := []TestResult{
		{
			Solution: "sol-a", Test: "golden", Status: StatusPass,
			Duration:    10 * time.Millisecond,
			Relaxed:     true,
			MaskMatches: map[string]int{"entra-group": 6, "uuid": 2},
		},
	}

	err := ReportResults(results, opts, false, time.Second, nil)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "PASS*", "relaxed pass should be marked in the table")
	assert.Contains(t, output, "Relaxed (snapshot fidelity loosened):")
	assert.Contains(t, output, "entra-group=6")
	assert.Contains(t, output, "uuid=2")
	assert.Contains(t, output, "1 relaxed")
}

func TestSummarize_CountsRelaxed(t *testing.T) {
	t.Parallel()
	results := []TestResult{
		{Status: StatusPass, Relaxed: true},
		{Status: StatusPass},
		{Status: StatusFail},
	}
	s := Summarize(results)
	assert.Equal(t, 2, s.Passed)
	assert.Equal(t, 1, s.Failed)
	assert.Equal(t, 1, s.Relaxed)
}

func TestReportResults_TableFormat_NoProgress(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := newTestOutputOpts(&buf, kvx.OutputFormatTable)

	err := ReportResults(sampleResults(), opts, false, time.Second, nil)
	require.NoError(t, err)
	output := buf.String()
	// Per-test rows should be present when no progress was shown
	assert.Contains(t, output, "sol-a")
	assert.Contains(t, output, "test-1")
	assert.Contains(t, output, "test-2")
}

func TestReportResults_TableFormat_WithProgress(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := newTestOutputOpts(&buf, kvx.OutputFormatTable)

	err := ReportResults(sampleResults(), opts, false, time.Second, &stubProgress{})
	require.NoError(t, err)
	output := buf.String()
	// Per-test rows should be suppressed, but summary and failures should be present.
	assert.Contains(t, output, "Failures:", "failures section header should be shown")
	assert.Contains(t, output, "test-2", "specific failing test should be shown in failures section")
	assert.Contains(t, output, "passed", "summary line should be shown")

	// The normal per-test table output should be suppressed when progress was shown.
	assert.NotContains(t, output, "test-1", "passing per-test row should be suppressed")
	assert.NotContains(t, output, "test-3", "skipped per-test row should be suppressed")
	assert.NotContains(t, output, "sol-b", "solution identifier from per-test rows should be suppressed")
}

func TestReportResults_JSONFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := newTestOutputOpts(&buf, kvx.OutputFormatJSON)

	err := ReportResults(sampleResults(), opts, false, 500*time.Millisecond, nil)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, `"status"`)
	assert.Contains(t, output, `"summary"`)
}

func TestReportResults_YAMLFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := newTestOutputOpts(&buf, kvx.OutputFormatYAML)

	err := ReportResults(sampleResults(), opts, false, 500*time.Millisecond, nil)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "status:")
	assert.Contains(t, output, "summary:")
}

func TestReportResults_AutoFormat_NoProgress(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	opts := newTestOutputOpts(&buf, kvx.OutputFormatAuto)

	err := ReportResults(sampleResults(), opts, false, time.Second, nil)
	require.NoError(t, err)
	output := buf.String()
	// Auto format on non-TTY should behave like table
	assert.Contains(t, output, "sol-a")
}

func BenchmarkReportResults_Table(b *testing.B) {
	results := sampleResults()
	var buf bytes.Buffer
	opts := newTestOutputOpts(&buf, kvx.OutputFormatTable)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		_ = ReportResults(results, opts, false, time.Second, nil)
	}
}
