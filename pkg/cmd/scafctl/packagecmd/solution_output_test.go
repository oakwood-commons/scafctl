// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package packagecmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/builder"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const minimalSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: report-test
  version: 1.0.0
spec: {}
`

// failWriter always fails, used to exercise write-error propagation.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, assert.AnError }

// solutionFromMinimal parses the minimal solution fixture into a Solution.
func solutionFromMinimal(t *testing.T) *solution.Solution {
	t.Helper()
	var sol solution.Solution
	require.NoError(t, sol.LoadFromBytes([]byte(minimalSolution)))
	return &sol
}

// writeSolutionFile writes the minimal solution to a temp dir and returns its path.
func writeSolutionFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(minimalSolution), 0o600))
	return solFile
}

// dryRunOpts builds SolutionOptions for a dry-run report test with separate
// stdout/stderr buffers so we can assert the report lands on stdout and human
// progress lands on stderr.
func dryRunOpts(t *testing.T, solFile, output string) (*SolutionOptions, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, outBuf, errBuf, false)
	cliParams := settings.NewCliParams()
	opts := &SolutionOptions{
		File:          solFile,
		Output:        output,
		IOStreams:     ioStreams,
		CliParams:     cliParams,
		DryRun:        true,
		BundleMaxSize: "50MB",
	}
	return opts, outBuf, errBuf
}

func TestRunPackageSolution_DryRunJSONReport(t *testing.T) {
	solFile := writeSolutionFile(t)
	opts, outBuf, errBuf := dryRunOpts(t, solFile, "json")

	w := writer.New(opts.IOStreams, opts.CliParams)
	ctx := writer.WithWriter(t.Context(), w)

	require.NoError(t, runPackageSolution(ctx, opts))

	// stdout must be valid JSON: the machine-readable report only.
	var report builder.PackageReport
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &report),
		"stdout must be clean JSON, got: %s", outBuf.String())

	assert.Equal(t, "report-test", report.Name)
	assert.Equal(t, "1.0.0", report.Version)
	assert.Equal(t, "report-test@1.0.0", report.Reference)
	assert.True(t, report.DryRun)
	assert.Empty(t, report.Digest, "dry-run stores nothing, so no digest")
	require.NotNil(t, report.Solution, "composed solution is embedded")

	// Human progress goes to stderr, never stdout.
	assert.Contains(t, errBuf.String(), "Dry run:")
	assert.NotContains(t, outBuf.String(), "Dry run:")
}

func TestRunPackageSolution_DryRunYAMLReport(t *testing.T) {
	solFile := writeSolutionFile(t)
	opts, outBuf, _ := dryRunOpts(t, solFile, "yaml")

	w := writer.New(opts.IOStreams, opts.CliParams)
	ctx := writer.WithWriter(t.Context(), w)

	require.NoError(t, runPackageSolution(ctx, opts))

	out := outBuf.String()
	assert.Contains(t, out, "name: report-test")
	assert.Contains(t, out, "dryRun: true")
}

func TestRunPackageSolution_DryRunNoOutputStaysHuman(t *testing.T) {
	solFile := writeSolutionFile(t)
	outBuf := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, outBuf, outBuf, false)
	cliParams := settings.NewCliParams()
	opts := &SolutionOptions{
		File:          solFile,
		IOStreams:     ioStreams,
		CliParams:     cliParams,
		DryRun:        true,
		BundleMaxSize: "50MB",
	}
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	require.NoError(t, runPackageSolution(ctx, opts))

	// Without -o, the human dry-run summary is printed and no JSON is emitted.
	assert.Contains(t, outBuf.String(), "Dry run:")
	assert.NotContains(t, outBuf.String(), `"dryRun"`)
}

func TestRunPackageSolution_ComposedOutYAML(t *testing.T) {
	solFile := writeSolutionFile(t)
	composedOut := filepath.Join(t.TempDir(), "composed.yaml")

	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, outBuf, errBuf, false)
	cliParams := settings.NewCliParams()
	opts := &SolutionOptions{
		File:          solFile,
		ComposedOut:   composedOut,
		IOStreams:     ioStreams,
		CliParams:     cliParams,
		DryRun:        true,
		BundleMaxSize: "50MB",
	}
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	require.NoError(t, runPackageSolution(ctx, opts))

	data, err := os.ReadFile(composedOut)
	require.NoError(t, err)
	assert.Contains(t, string(data), "name: report-test")
}

func TestRunPackageSolution_ComposedOutJSON(t *testing.T) {
	solFile := writeSolutionFile(t)
	composedOut := filepath.Join(t.TempDir(), "composed.json")

	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, outBuf, errBuf, false)
	cliParams := settings.NewCliParams()
	opts := &SolutionOptions{
		File:          solFile,
		ComposedOut:   composedOut,
		IOStreams:     ioStreams,
		CliParams:     cliParams,
		DryRun:        true,
		BundleMaxSize: "50MB",
	}
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	require.NoError(t, runPackageSolution(ctx, opts))

	data, err := os.ReadFile(composedOut)
	require.NoError(t, err)

	// .json extension yields a valid JSON document.
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc), "composed .json must be valid JSON")
	assert.Equal(t, "report-test", doc["metadata"].(map[string]any)["name"])
}

func TestCommandPackageSolution_RejectsInvalidOutput(t *testing.T) {
	solFile := writeSolutionFile(t)

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandPackageSolution(cliParams, ioStreams, "build")
	w := writer.New(ioStreams, cliParams)
	cmd.SetContext(writer.WithWriter(t.Context(), w))
	cmd.SetArgs([]string{"-f", solFile, "-o", "xml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "xml")
}

func TestCommandPackageSolution_OutputFlags(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandPackageSolution(cliParams, ioStreams, "build")

	outFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outFlag)
	require.NotNil(t, cmd.Flags().ShorthandLookup("o"))
	assert.Equal(t, "output", cmd.Flags().ShorthandLookup("o").Name)

	composedFlag := cmd.Flags().Lookup("composed-out")
	require.NotNil(t, composedFlag)
}

func TestEmitPackageReport_NoOutputIsNoOp(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	opts := &SolutionOptions{Output: "", IOStreams: ioStreams}
	err := emitPackageReport(t.Context(), opts, builder.PackageReportInput{Name: "n", Version: "1.0.0"})
	require.NoError(t, err)
}

func TestEmitPackageReport_WriteError(t *testing.T) {
	t.Parallel()

	// A failing Out writer makes output.WriteOutput return a write error, which
	// emitPackageReport must surface (not swallow) as a non-nil error.
	ioStreams := terminal.NewIOStreams(nil, failWriter{}, &bytes.Buffer{}, false)
	opts := &SolutionOptions{Output: "json", IOStreams: ioStreams}
	err := emitPackageReport(t.Context(), opts, builder.PackageReportInput{Name: "n", Version: "1.0.0"})
	require.Error(t, err)
}

func TestWriteComposedSolution_WriteError(t *testing.T) {
	t.Parallel()

	// A path inside a nonexistent directory makes os.WriteFile fail.
	badPath := filepath.Join(t.TempDir(), "nope", "composed.yaml")

	errBuf := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, errBuf, errBuf, false)
	w := writer.New(ioStreams, settings.NewCliParams())

	sol := solutionFromMinimal(t)
	err := writeComposedSolution(badPath, sol, w)
	require.Error(t, err)
	assert.Contains(t, errBuf.String(), "failed to write composed solution")
}
