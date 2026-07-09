// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readJUnitReport writes results to a temp file and parses the resulting XML.
func readJUnitReport(t *testing.T, results []TestResult) junitTestSuites {
	t.Helper()
	path := filepath.Join(t.TempDir(), "junit.xml")
	require.NoError(t, WriteJUnitReport(results, path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var parsed junitTestSuites
	require.NoError(t, xml.Unmarshal(data, &parsed))
	return parsed
}

func TestWriteJUnitReport_StatusMapping(t *testing.T) {
	results := []TestResult{
		{Solution: "sol", Test: "passing", Status: StatusPass, Duration: 10 * time.Millisecond},
		{Solution: "sol", Test: "failing", Status: StatusFail, Duration: 20 * time.Millisecond, Message: "boom"},
		{Solution: "sol", Test: "erroring", Status: StatusError, Duration: 5 * time.Millisecond, Message: "setup failed"},
		{Solution: "sol", Test: "skipping", Status: StatusSkip, Duration: 0, Message: "skipped"},
	}

	parsed := readJUnitReport(t, results)

	require.Equal(t, 4, parsed.Tests)
	require.Equal(t, 1, parsed.Failures)
	require.Equal(t, 1, parsed.Errors)
	require.Equal(t, 1, parsed.Skipped)
	require.Len(t, parsed.TestSuites, 1)

	suite := parsed.TestSuites[0]
	assert.Equal(t, "sol", suite.Name)
	require.Len(t, suite.TestCases, 4)

	byName := map[string]junitTestCase{}
	for _, tc := range suite.TestCases {
		byName[tc.Name] = tc
	}

	assert.Nil(t, byName["passing"].Failure)
	assert.Nil(t, byName["passing"].Error)
	assert.Nil(t, byName["passing"].Skipped)

	require.NotNil(t, byName["failing"].Failure)
	assert.Equal(t, "boom", byName["failing"].Failure.Message)
	assert.Equal(t, "AssertionFailure", byName["failing"].Failure.Type)

	require.NotNil(t, byName["erroring"].Error)
	assert.Equal(t, "setup failed", byName["erroring"].Error.Message)
	assert.Equal(t, "TestError", byName["erroring"].Error.Type)

	require.NotNil(t, byName["skipping"].Skipped)
	assert.Equal(t, "skipped", byName["skipping"].Skipped.Message)
}

func TestWriteJUnitReport_RelaxedSystemOut(t *testing.T) {
	tests := []struct {
		name     string
		result   TestResult
		wantOut  string
		wantHave bool
	}{
		{
			name: "relaxed with mask counts",
			result: TestResult{
				Solution:    "sol",
				Test:        "golden",
				Status:      StatusPass,
				Relaxed:     true,
				MaskMatches: map[string]int{"greeting": 1, "uuid": 2},
			},
			wantOut:  "snapshot fidelity relaxed via masks: greeting=1, uuid=2",
			wantHave: true,
		},
		{
			name: "relaxed with no matches",
			result: TestResult{
				Solution: "sol",
				Test:     "golden-empty",
				Status:   StatusPass,
				Relaxed:  true,
			},
			wantOut:  "snapshot fidelity relaxed via masks",
			wantHave: true,
		},
		{
			name: "not relaxed omits system-out",
			result: TestResult{
				Solution: "sol",
				Test:     "plain",
				Status:   StatusPass,
			},
			wantHave: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := readJUnitReport(t, []TestResult{tt.result})
			require.Len(t, parsed.TestSuites, 1)
			require.Len(t, parsed.TestSuites[0].TestCases, 1)

			got := parsed.TestSuites[0].TestCases[0].SystemOut
			if tt.wantHave {
				assert.Equal(t, tt.wantOut, got)
			} else {
				assert.Empty(t, got)
			}
		})
	}
}

func TestWriteJUnitReport_GroupsBySolution(t *testing.T) {
	results := []TestResult{
		{Solution: "alpha", Test: "a1", Status: StatusPass, Duration: time.Millisecond},
		{Solution: "beta", Test: "b1", Status: StatusPass, Duration: time.Millisecond},
		{Solution: "alpha", Test: "a2", Status: StatusFail, Duration: time.Millisecond, Message: "nope"},
	}

	parsed := readJUnitReport(t, results)

	require.Len(t, parsed.TestSuites, 2)
	// Suites preserve first-seen order.
	assert.Equal(t, "alpha", parsed.TestSuites[0].Name)
	assert.Equal(t, "beta", parsed.TestSuites[1].Name)
	assert.Equal(t, 2, parsed.TestSuites[0].Tests)
	assert.Equal(t, 1, parsed.TestSuites[0].Failures)
	assert.Equal(t, 1, parsed.TestSuites[1].Tests)
}

func TestWriteJUnitReport_CreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "reports", "junit.xml")
	err := WriteJUnitReport([]TestResult{
		{Solution: "sol", Test: "t", Status: StatusPass, Duration: time.Millisecond},
	}, path)
	require.NoError(t, err)

	_, statErr := os.Stat(path)
	require.NoError(t, statErr)
}
