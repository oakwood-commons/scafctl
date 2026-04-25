// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeFileEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		config    *TestConfig
		testFiles []string
		want      []string
	}{
		{
			name:      "nil config",
			config:    nil,
			testFiles: []string{"a.txt"},
			want:      []string{"a.txt"},
		},
		{
			name:      "empty config files",
			config:    &TestConfig{},
			testFiles: []string{"b.txt"},
			want:      []string{"b.txt"},
		},
		{
			name:      "config files only",
			config:    &TestConfig{Files: []string{"shared.txt"}},
			testFiles: nil,
			want:      []string{"shared.txt"},
		},
		{
			name:      "both merged config first",
			config:    &TestConfig{Files: []string{"shared.txt"}},
			testFiles: []string{"local.txt"},
			want:      []string{"shared.txt", "local.txt"},
		},
		{
			name:      "both empty",
			config:    &TestConfig{},
			testFiles: nil,
			want:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mergeFileEntries(tc.config, tc.testFiles)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAttachCommandOutput(t *testing.T) {
	t.Parallel()

	t.Run("nil output", func(t *testing.T) {
		t.Parallel()
		result := &TestResult{}
		attachCommandOutput(result, nil)
		assert.Empty(t, result.Stdout)
		assert.Empty(t, result.Stderr)
	})

	t.Run("populated output", func(t *testing.T) {
		t.Parallel()
		result := &TestResult{}
		attachCommandOutput(result, &CommandOutput{
			Stdout: "hello",
			Stderr: "oops",
		})
		assert.Equal(t, "hello", result.Stdout)
		assert.Equal(t, "oops", result.Stderr)
	})
}

func TestReportFailures_IncludesStderr(t *testing.T) {
	t.Parallel()

	results := []TestResult{
		{
			Solution: "sol-a",
			Test:     "test-1",
			Status:   StatusFail,
			Message:  "exit code 1",
			Stderr:   "something went wrong",
		},
	}

	var buf bytes.Buffer
	reportFailures(results, &buf, false)
	output := buf.String()

	require.Contains(t, output, "Failures:")
	assert.Contains(t, output, "sol-a/test-1: exit code 1")
	assert.Contains(t, output, "stderr: something went wrong")
}

func TestReportFailures_NoStderrWhenEmpty(t *testing.T) {
	t.Parallel()

	results := []TestResult{
		{
			Solution: "sol-a",
			Test:     "test-1",
			Status:   StatusFail,
			Message:  "exit code 1",
		},
	}

	var buf bytes.Buffer
	reportFailures(results, &buf, false)
	output := buf.String()

	assert.NotContains(t, output, "stderr:")
}

func TestBuildReportData_IncludesStdoutStderr(t *testing.T) {
	t.Parallel()

	results := []TestResult{
		{
			Solution: "sol-a",
			Test:     "test-1",
			Status:   StatusFail,
			Message:  "exit code 1",
			Stdout:   "output text",
			Stderr:   "error text",
		},
	}

	data := buildReportData(results, 0)

	require.Len(t, data.Results, 1)
	assert.Equal(t, "output text", data.Results[0].Stdout)
	assert.Equal(t, "error text", data.Results[0].Stderr)
}
