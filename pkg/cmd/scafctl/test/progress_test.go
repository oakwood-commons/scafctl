// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── LineTestProgress tests ───────────────────────────────────────────────────

func newTestWriter(tb testing.TB) (*writer.Writer, *bytes.Buffer) {
	tb.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	return w, &buf
}

func TestNewLineTestProgress(t *testing.T) {
	t.Parallel()
	w, _ := newTestWriter(t)
	p := NewLineTestProgress(w)
	require.NotNil(t, p)
}

func TestLineTestProgress_OnTestStart_IsNoop(t *testing.T) {
	t.Parallel()
	w, buf := newTestWriter(t)
	p := NewLineTestProgress(w)

	// OnTestStart should produce no output
	p.OnTestStart("my-solution", "my-test")
	assert.Empty(t, buf.String(), "OnTestStart should not produce any output")
}

func TestLineTestProgress_OnTestComplete_Pass(t *testing.T) {
	t.Parallel()
	w, buf := newTestWriter(t)
	p := NewLineTestProgress(w)

	result := soltesting.TestResult{
		Solution: "my-solution",
		Test:     "my-test",
		Status:   soltesting.StatusPass,
		Duration: 150 * time.Millisecond,
	}
	p.OnTestComplete(result)

	output := buf.String()
	assert.Contains(t, output, "my-solution")
	assert.Contains(t, output, "my-test")
	assert.Contains(t, output, "pass")
	assert.Contains(t, output, "✓")
	assert.Contains(t, output, "150ms")
}

func TestLineTestProgress_OnTestComplete_Fail(t *testing.T) {
	t.Parallel()
	w, buf := newTestWriter(t)
	p := NewLineTestProgress(w)

	result := soltesting.TestResult{
		Solution: "sol",
		Test:     "test-fail",
		Status:   soltesting.StatusFail,
		Duration: 0,
	}
	p.OnTestComplete(result)

	output := buf.String()
	assert.Contains(t, output, "✗")
	assert.Contains(t, output, "fail")
}

func TestLineTestProgress_OnTestComplete_Error(t *testing.T) {
	t.Parallel()
	w, buf := newTestWriter(t)
	p := NewLineTestProgress(w)

	result := soltesting.TestResult{
		Solution: "sol",
		Test:     "test-err",
		Status:   soltesting.StatusError,
		Duration: 200 * time.Millisecond,
	}
	p.OnTestComplete(result)

	output := buf.String()
	assert.Contains(t, output, "!")
	assert.Contains(t, output, "error")
}

func TestLineTestProgress_OnTestComplete_Skip(t *testing.T) {
	t.Parallel()
	w, buf := newTestWriter(t)
	p := NewLineTestProgress(w)

	result := soltesting.TestResult{
		Solution: "sol",
		Test:     "test-skip",
		Status:   soltesting.StatusSkip,
		Duration: 0,
	}
	p.OnTestComplete(result)

	output := buf.String()
	assert.Contains(t, output, "⊘")
	assert.Contains(t, output, "skip")
}

func TestLineTestProgress_OnTestComplete_NoDurationWhenZero(t *testing.T) {
	t.Parallel()
	w, buf := newTestWriter(t)
	p := NewLineTestProgress(w)

	result := soltesting.TestResult{
		Solution: "sol",
		Test:     "test",
		Status:   soltesting.StatusPass,
		Duration: 0,
	}
	p.OnTestComplete(result)

	output := buf.String()
	// When duration is 0, no parenthetical duration should appear
	assert.NotContains(t, output, "(")
}

func TestLineTestProgress_Wait_IsNoop(t *testing.T) {
	t.Parallel()
	w, _ := newTestWriter(t)
	p := NewLineTestProgress(w)

	// Should not block or panic
	p.Wait()
}

// ── fmtDuration tests ────────────────────────────────────────────────────────

func TestFmtDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    time.Duration
		expected string
	}{
		{
			name:     "microseconds",
			input:    500 * time.Microsecond,
			expected: "500µs",
		},
		{
			name:     "milliseconds",
			input:    250 * time.Millisecond,
			expected: "250ms",
		},
		{
			name:     "seconds",
			input:    2500 * time.Millisecond,
			expected: "2.50s",
		},
		{
			name:     "exact millisecond boundary",
			input:    1 * time.Millisecond,
			expected: "1ms",
		},
		{
			name:     "exact second boundary",
			input:    1 * time.Second,
			expected: "1.00s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := fmtDuration(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ── MPBTestProgress tests ────────────────────────────────────────────────────

func TestNewMPBTestProgress(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	p := NewMPBTestProgress(&buf)
	require.NotNil(t, p)
	// Wait should not block since no bars were added
	p.Wait()
}

func TestMPBTestProgress_BarKey(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	p := NewMPBTestProgress(&buf)

	key := p.barKey("my-solution", "my-test")
	assert.Equal(t, "my-solution :: my-test", key)
	p.Wait()
}

func TestMPBTestProgress_OnTestComplete_WithoutStart(t *testing.T) {
	t.Parallel()
	// completing a test without calling OnTestStart should not panic
	var buf bytes.Buffer
	p := NewMPBTestProgress(&buf)

	result := soltesting.TestResult{
		Solution: "sol",
		Test:     "no-start-test",
		Status:   soltesting.StatusPass,
		Duration: 10 * time.Millisecond,
	}
	p.OnTestComplete(result)
	p.Wait()
}

func TestMPBTestProgress_OnTestComplete_FailWithoutStart(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	p := NewMPBTestProgress(&buf)

	result := soltesting.TestResult{
		Solution: "sol",
		Test:     "fail-no-start",
		Status:   soltesting.StatusFail,
		Duration: 5 * time.Millisecond,
	}
	p.OnTestComplete(result)
	p.Wait()
}

func TestMPBTestProgress_OnTestComplete_ErrorWithoutStart(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	p := NewMPBTestProgress(&buf)

	result := soltesting.TestResult{
		Solution: "sol",
		Test:     "error-no-start",
		Status:   soltesting.StatusError,
		Duration: 3 * time.Millisecond,
	}
	p.OnTestComplete(result)
	p.Wait()
}

func TestMPBTestProgress_OnTestComplete_SkipWithoutStart(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	p := NewMPBTestProgress(&buf)

	result := soltesting.TestResult{
		Solution: "sol",
		Test:     "skip-no-start",
		Status:   soltesting.StatusSkip,
		Duration: 0,
	}
	p.OnTestComplete(result)
	p.Wait()
}

func TestMPBTestProgress_OnTestStart_ThenComplete(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	p := NewMPBTestProgress(&buf)

	p.OnTestStart("solution-a", "test-1")
	p.OnTestComplete(soltesting.TestResult{
		Solution: "solution-a",
		Test:     "test-1",
		Status:   soltesting.StatusPass,
		Duration: 50 * time.Millisecond,
	})
	p.Wait()
}

func TestMPBTestProgress_OnTestStart_ThenFail(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	p := NewMPBTestProgress(&buf)

	p.OnTestStart("solution-b", "test-fail")
	p.OnTestComplete(soltesting.TestResult{
		Solution: "solution-b",
		Test:     "test-fail",
		Status:   soltesting.StatusFail,
		Duration: 25 * time.Millisecond,
	})
	p.Wait()
}

// ── Concurrent safety tests ───────────────────────────────────────────────────

func TestLineTestProgress_ConcurrentWrites(t *testing.T) {
	t.Parallel()
	w, buf := newTestWriter(t)
	p := NewLineTestProgress(w)

	// Fire multiple concurrent completions to test mutex safety
	done := make(chan struct{})
	for i := range 5 {
		go func(i int) {
			p.OnTestComplete(soltesting.TestResult{
				Solution: "sol",
				Test:     "test",
				Status:   soltesting.StatusPass,
				Duration: time.Duration(i) * time.Millisecond,
			})
			done <- struct{}{}
		}(i)
	}

	for range 5 {
		<-done
	}

	// All writes should have gone through — just verify no panic occurred.
	assert.NotEmpty(t, buf.String())
}

// ── Helper to verify line test progress output format ────────────────────────

func TestLineTestProgress_OutputFormat(t *testing.T) {
	t.Parallel()
	w, buf := newTestWriter(t)
	p := NewLineTestProgress(w)

	p.OnTestComplete(soltesting.TestResult{
		Solution: "my-sol",
		Test:     "my-test",
		Status:   soltesting.StatusPass,
		Duration: 1 * time.Second,
	})

	output := buf.String()
	// Verify it contains both the solution and test with separator
	assert.True(t, strings.Contains(output, "my-sol") && strings.Contains(output, "my-test"),
		"output should contain solution and test names")
}

func BenchmarkLineTestProgress_OnTestComplete(b *testing.B) {
	w, _ := newTestWriter(b)
	p := NewLineTestProgress(w)
	result := soltesting.TestResult{
		Solution: "sol",
		Test:     "bench-test",
		Status:   soltesting.StatusPass,
		Duration: 10 * time.Millisecond,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.OnTestComplete(result)
	}
}

func BenchmarkFmtDuration(b *testing.B) {
	durations := []time.Duration{
		500 * time.Microsecond,
		250 * time.Millisecond,
		2500 * time.Millisecond,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, d := range durations {
			fmtDuration(d)
		}
	}
}
