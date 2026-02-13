// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// JUnit XML types following the JUnit 4 schema.

// junitTestSuites is the root element containing all test suites.
type junitTestSuites struct {
	XMLName    xml.Name         `xml:"testsuites"`
	Tests      int              `xml:"tests,attr"`
	Failures   int              `xml:"failures,attr"`
	Errors     int              `xml:"errors,attr"`
	Skipped    int              `xml:"skipped,attr"`
	Time       string           `xml:"time,attr"`
	TestSuites []junitTestSuite `xml:"testsuite"`
}

// junitTestSuite represents a single test suite (one per solution).
type junitTestSuite struct {
	XMLName   xml.Name        `xml:"testsuite"`
	Name      string          `xml:"name,attr"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Errors    int             `xml:"errors,attr"`
	Skipped   int             `xml:"skipped,attr"`
	Time      string          `xml:"time,attr"`
	TestCases []junitTestCase `xml:"testcase"`
}

// junitTestCase represents a single test case.
type junitTestCase struct {
	XMLName   xml.Name      `xml:"testcase"`
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Error     *junitError   `xml:"error,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
}

// junitFailure records an assertion failure.
type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",chardata"`
}

// junitError records an infrastructure error.
type junitError struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",chardata"`
}

// junitSkipped records a skipped test.
type junitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

// WriteJUnitReport generates a JUnit XML report from test results and writes it to path.
func WriteJUnitReport(results []TestResult, path string) error {
	// Group results by solution
	suiteMap := make(map[string][]TestResult)
	var suiteOrder []string
	for _, r := range results {
		if _, exists := suiteMap[r.Solution]; !exists {
			suiteOrder = append(suiteOrder, r.Solution)
		}
		suiteMap[r.Solution] = append(suiteMap[r.Solution], r)
	}

	summary := Summarize(results)

	root := junitTestSuites{
		Tests:    summary.Total,
		Failures: summary.Failed,
		Errors:   summary.Errors,
		Skipped:  summary.Skipped,
		Time:     fmt.Sprintf("%.3f", summary.Duration.Seconds()),
	}

	for _, solutionName := range suiteOrder {
		suiteResults := suiteMap[solutionName]
		suiteSummary := Summarize(suiteResults)

		suite := junitTestSuite{
			Name:     solutionName,
			Tests:    suiteSummary.Total,
			Failures: suiteSummary.Failed,
			Errors:   suiteSummary.Errors,
			Skipped:  suiteSummary.Skipped,
			Time:     fmt.Sprintf("%.3f", suiteSummary.Duration.Seconds()),
		}

		for _, r := range suiteResults {
			tc := junitTestCase{
				Name:      r.Test,
				ClassName: r.Solution,
				Time:      fmt.Sprintf("%.3f", r.Duration.Seconds()),
			}

			switch r.Status {
			case StatusFail:
				tc.Failure = &junitFailure{
					Message: r.Message,
					Type:    "AssertionFailure",
					Body:    buildFailureBody(r),
				}
			case StatusError:
				tc.Error = &junitError{
					Message: r.Message,
					Type:    "TestError",
					Body:    r.Message,
				}
			case StatusSkip:
				tc.Skipped = &junitSkipped{
					Message: r.Message,
				}
			case StatusPass:
				// No additional elements needed
			}

			suite.TestCases = append(suite.TestCases, tc)
		}

		root.TestSuites = append(root.TestSuites, suite)
	}

	// Ensure output directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating JUnit report directory: %w", err)
	}

	data, err := xml.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling JUnit XML: %w", err)
	}

	content := []byte(xml.Header)
	content = append(content, data...)
	content = append(content, '\n')

	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("writing JUnit report to %q: %w", path, err)
	}

	return nil
}

// buildFailureBody creates a detailed failure description from assertion results.
func buildFailureBody(r TestResult) string {
	var sb strings.Builder
	sb.WriteString(r.Message)

	failedAssertions := 0
	for _, a := range r.Assertions {
		if !a.Passed {
			failedAssertions++
		}
	}

	if failedAssertions > 0 {
		sb.WriteString("\n\nFailed assertions:\n")
		for _, a := range r.Assertions {
			if !a.Passed {
				fmt.Fprintf(&sb, "  [%s] %s: %s\n", a.Type, a.Input, a.Message)
			}
		}
	}

	return sb.String()
}
