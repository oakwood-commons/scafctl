// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	pkglint "github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandLint(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLint(cliParams, ioStreams, "scafctl")
	require.NotNil(t, cmd)
	assert.Equal(t, "lint [name[@version]]", cmd.Use)
	assert.Contains(t, cmd.Aliases, "l")
	assert.Contains(t, cmd.Aliases, "check")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE, "lint command should have RunE")
	subCmds := cmd.Commands()
	require.Len(t, subCmds, 2, "should have 2 subcommands: rules, explain")
	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Name()
	}
	assert.Contains(t, cmdNames, "rules")
	assert.Contains(t, cmdNames, "explain")
}

func TestCommandLint_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLint(cliParams, ioStreams, "scafctl")
	tests := []struct {
		name     string
		flagName string
		defVal   string
	}{
		{"file", "file", ""},
		{"output", "output", "table"},
		{"expression", "expression", ""},
		{"severity", "severity", "info"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, f, "flag %q should exist", tt.flagName)
			assert.Equal(t, tt.defVal, f.DefValue, "flag %q default value", tt.flagName)
		})
	}
}

func TestCommandRules(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandRules(cliParams, ioStreams, "scafctl/lint")
	require.NotNil(t, cmd)
	assert.Equal(t, "rules", cmd.Use)
	assert.Contains(t, cmd.Aliases, "r")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	sf := cmd.Flags().Lookup("severity")
	require.NotNil(t, sf, "--severity flag should exist")
	cf := cmd.Flags().Lookup("category")
	require.NotNil(t, cf, "--category flag should exist")
}

func TestCommandExplainRule(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExplainRule(cliParams, ioStreams, "scafctl/lint")
	require.NotNil(t, cmd)
	assert.Equal(t, "explain <rule-name>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandExplainRule_RequiresArg(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExplainRule(cliParams, ioStreams, "scafctl/lint")
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "should fail without rule-name argument")
}

func BenchmarkCommandLint(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandLint(cliParams, ioStreams, "scafctl")
	}
}

// ── findingsColumnHints tests ────────────────────────────────────────────────

func TestFindingsColumnHints_ReturnsAllColumns(t *testing.T) {
	t.Parallel()
	hints := findingsColumnHints(nil)
	for _, col := range []string{"severity", "location", "message", "ruleName"} {
		assert.Contains(t, hints, col, "column %q must be present", col)
	}
	assert.Equal(t, 8, hints["severity"].MaxWidth)
	assert.Equal(t, maxLocationWidth, hints["location"].MaxWidth)
	assert.Equal(t, maxRuleWidth, hints["ruleName"].MaxWidth)
}

func TestFindingsColumnHints_DefaultMessageWidth(t *testing.T) {
	t.Parallel()
	// With a nil writer, termWidth returns 0, so message stays at default.
	hints := findingsColumnHints(nil)
	assert.Equal(t, maxMessageWidth, hints["message"].MaxWidth)
}

// ── projectFindings tests ────────────────────────────────────────────────────

func TestProjectFindings_ConvertsFields(t *testing.T) {
	t.Parallel()
	findings := []*pkglint.Finding{
		{Severity: pkglint.SeverityError, Location: "file.yaml:10", Message: "bad thing", RuleName: "test-rule"},
		{Severity: pkglint.SeverityWarning, Location: "file.yaml:20", Message: "warn", RuleName: "warn-rule"},
	}
	rows := projectFindings(findings)
	require.Len(t, rows, 2)
	row0 := rows[0].(map[string]any)
	assert.Equal(t, "error", row0["severity"])
	assert.Equal(t, "file.yaml:10", row0["location"])
	assert.Equal(t, "bad thing", row0["message"])
	assert.Equal(t, "test-rule", row0["ruleName"])
}

func TestProjectFindings_Empty(t *testing.T) {
	t.Parallel()
	rows := projectFindings(nil)
	assert.Empty(t, rows)
}

// ── truncate tests ───────────────────────────────────────────────────────────

func TestTruncate_Short(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "abc", truncate("abc", 10))
}

func TestTruncate_Exact(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "abcde", truncate("abcde", 5))
}

func TestTruncate_Long(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "abcdefghij...", truncate("abcdefghijklmnop", 13))
}

func TestTruncate_MaxLenZero(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", truncate("abc", 0))
}

func TestTruncate_MaxLenOne(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a", truncate("abc", 1))
}

func TestTruncate_MaxLenTwo(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "ab", truncate("abc", 2))
}

func TestTruncate_MaxLenThree(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "abc", truncate("abcde", 3))
}

// ── termWidth tests ──────────────────────────────────────────────────────────

func TestTermWidth_NilWriter(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0, termWidth(nil))
}

func TestTermWidth_NonFileWriter(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	assert.Equal(t, 0, termWidth(&buf))
}

// ── runLint integration tests ────────────────────────────────────────────────

func testCliParams() *settings.Run {
	p := settings.NewCliParams()
	p.ExitOnError = false
	return p
}

func testContext(ioStreams *terminal.IOStreams) context.Context {
	ctx := context.Background()
	lgr := logger.GetNoopLogger()
	ctx = logger.WithLogger(ctx, lgr)
	w := writer.New(ioStreams, testCliParams())
	ctx = writer.WithWriter(ctx, w)
	return ctx
}

func TestRunLint_CleanSolution(t *testing.T) {
	t.Parallel()

	sol := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-clean
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`
	tmpDir := t.TempDir()
	solPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solPath, []byte(sol), 0o600))

	ioStreams, _, _ := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)
	opts := &Options{
		File:       solPath,
		Output:     "table",
		Severity:   "info",
		CliParams:  testCliParams(),
		IOStreams:  ioStreams,
		BinaryName: "scafctl",
	}
	err := runLint(ctx, opts)
	assert.NoError(t, err)
}

func TestRunLint_WithFindings_TableOutput(t *testing.T) {
	t.Parallel()

	// Solution with a null resolver — triggers the null-resolver lint rule.
	sol := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-findings
  version: 1.0.0
spec:
  resolvers:
    empty_resolver:
`
	tmpDir := t.TempDir()
	solPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solPath, []byte(sol), 0o600))

	ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)
	opts := &Options{
		File:       solPath,
		Output:     "table",
		Severity:   "info",
		CliParams:  testCliParams(),
		IOStreams:  ioStreams,
		BinaryName: "scafctl",
	}
	err := runLint(ctx, opts)
	// The null resolver should trigger lint findings. The error may come from
	// load failure or from lint errors — either way we expect an error.
	assert.Error(t, err, "should return error for findings with errors")
	combined := outBuf.String() + errBuf.String()
	assert.NotEmpty(t, combined, "should produce some output")
}

func TestRunLint_JSONOutput(t *testing.T) {
	t.Parallel()

	sol := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-json
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`
	tmpDir := t.TempDir()
	solPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solPath, []byte(sol), 0o600))

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)
	opts := &Options{
		File:       solPath,
		Output:     "json",
		Severity:   "info",
		CliParams:  testCliParams(),
		IOStreams:  ioStreams,
		BinaryName: "scafctl",
	}
	err := runLint(ctx, opts)
	assert.NoError(t, err)
	assert.Contains(t, outBuf.String(), "findings", "JSON output should contain findings key")
}

func TestRunLint_QuietOutput_NoErrors(t *testing.T) {
	t.Parallel()

	sol := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-quiet
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`
	tmpDir := t.TempDir()
	solPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solPath, []byte(sol), 0o600))

	ioStreams, _, _ := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)
	opts := &Options{
		File:       solPath,
		Output:     "quiet",
		Severity:   "info",
		CliParams:  testCliParams(),
		IOStreams:  ioStreams,
		BinaryName: "scafctl",
	}
	err := runLint(ctx, opts)
	assert.NoError(t, err)
}

func TestRunLint_FileNotFound(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)
	opts := &Options{
		File:       "/nonexistent/solution.yaml",
		Output:     "table",
		Severity:   "info",
		CliParams:  testCliParams(),
		IOStreams:  ioStreams,
		BinaryName: "scafctl",
	}
	err := runLint(ctx, opts)
	assert.Error(t, err)
}

// ── Delegate re-export tests ─────────────────────────────────────────────────

func TestSolutionDelegate(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "test"},
	}
	result := Solution(sol, "test.yaml", nil)
	require.NotNil(t, result)
}

func TestFilterBySeverityDelegate(t *testing.T) {
	t.Parallel()
	result := &pkglint.Result{}
	filtered := FilterBySeverity(result, "error")
	require.NotNil(t, filtered)
}
