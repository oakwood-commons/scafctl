// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
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
	require.Len(t, subCmds, 3, "should have 3 subcommands: rules, rule, explain")
	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Name()
	}
	assert.Contains(t, cmdNames, "rules")
	assert.Contains(t, cmdNames, "rule")
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
		{"output", "output", "auto"},
		{"expression", "expression", ""},
		{"severity", "severity", "info"},
		{"interactive", "interactive", "false"},
		{"where", "where", ""},
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

func TestRulesRun_JSONOutput(t *testing.T) {
	t.Parallel()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	ctx := testContext(ioStreams)

	opts := &RulesOptions{
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
	}

	err := opts.Run(ctx)
	require.NoError(t, err)

	var rules []pkglint.RuleMeta
	err = json.Unmarshal(outBuf.Bytes(), &rules)
	require.NoError(t, err, "output should be valid JSON")
	assert.NotEmpty(t, rules, "should contain at least one rule")
	assert.NotEmpty(t, rules[0].Rule, "rule name should be populated")
}

func TestRulesRun_TableOutput(t *testing.T) {
	t.Parallel()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	ctx := testContext(ioStreams)

	opts := &RulesOptions{
		IOStreams: ioStreams,
		CliParams: cliParams,
	}

	err := opts.Run(ctx)
	require.NoError(t, err)

	output := outBuf.String()
	assert.NotEmpty(t, output)
	// KVX table output should contain rule names
	assert.True(t, strings.Contains(output, "unused-resolver") || strings.Contains(output, "empty-solution"),
		"table output should contain at least one known rule name")
}

func TestRulesRun_EmptyResults(t *testing.T) {
	t.Parallel()
	ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	ctx := testContext(ioStreams)

	opts := &RulesOptions{
		IOStreams: ioStreams,
		CliParams: cliParams,
		Severity:  "nonexistent",
	}

	err := opts.Run(ctx)
	require.NoError(t, err)

	combined := outBuf.String() + errBuf.String()
	assert.Contains(t, combined, "No rules match")
}

func TestRulesRun_EmptyResults_JSON(t *testing.T) {
	t.Parallel()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	ctx := testContext(ioStreams)

	opts := &RulesOptions{
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		Severity:       "nonexistent",
	}

	err := opts.Run(ctx)
	require.NoError(t, err)

	var rules []pkglint.RuleMeta
	err = json.Unmarshal(outBuf.Bytes(), &rules)
	require.NoError(t, err, "output should be valid JSON")
	assert.Empty(t, rules, "should be an empty array")
}

func TestRulesRun_SeverityFilter(t *testing.T) {
	t.Parallel()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	ctx := testContext(ioStreams)

	opts := &RulesOptions{
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		Severity:       "error",
	}

	err := opts.Run(ctx)
	require.NoError(t, err)

	var rules []pkglint.RuleMeta
	err = json.Unmarshal(outBuf.Bytes(), &rules)
	require.NoError(t, err)
	assert.NotEmpty(t, rules)
	for _, r := range rules {
		assert.Equal(t, "error", r.Severity, "all rules should be error severity")
	}
}

func TestCommandRule(t *testing.T) {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandRule(cliParams, ioStreams, "scafctl/lint")
	require.NotNil(t, cmd)
	assert.Equal(t, "rule <rule-name>", cmd.Use)
	assert.True(t, strings.HasPrefix(cmd.Use, "rule"))
	assert.False(t, cmd.Hidden, "canonical rule command must not be hidden")
	assert.Empty(t, cmd.Deprecated, "canonical rule command must not be deprecated")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	// Embedder-safe help: Long must use the configured binary name, not scafctl.
	assert.Contains(t, cmd.Long, "mycli lint rule")
	assert.NotContains(t, cmd.Long, "scafctl lint rule")
}

func TestCommandRule_RequiresArg(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandRule(cliParams, ioStreams, "scafctl/lint")
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "should fail without rule-name argument")
}

func TestCommandExplainRuleDeprecated(t *testing.T) {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExplainRuleDeprecated(cliParams, ioStreams, "scafctl/lint")
	require.NotNil(t, cmd)
	assert.Equal(t, "explain <rule-name>", cmd.Use)
	assert.True(t, cmd.Hidden, "deprecated explain alias must be hidden")
	assert.NotEmpty(t, cmd.Deprecated)
	assert.Contains(t, cmd.Deprecated, "mycli lint rule", "deprecation should point at canonical command")
	assert.NotNil(t, cmd.RunE)
}

func TestCommandExplainRuleDeprecated_RequiresArg(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExplainRuleDeprecated(cliParams, ioStreams, "scafctl/lint")
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "should fail without rule-name argument")
}

// TestCanonicalAndDeprecated_ShareRunE verifies the canonical 'lint rule'
// command and the deprecated 'lint explain' alias produce identical stdout for
// the same args (shared RunE), and that the deprecation notice is emitted on
// the deprecated command's stderr (cobra writer), not on stdout.
func TestCanonicalAndDeprecated_ShareRunE(t *testing.T) {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "scafctl"

	ioC, outC, errC := terminal.NewTestIOStreams()
	ioD, outD, errD := terminal.NewTestIOStreams()

	canonical := CommandRule(cliParams, ioC, "scafctl/lint")
	deprecated := CommandExplainRuleDeprecated(cliParams, ioD, "scafctl/lint")

	canonical.SetOut(errC)
	canonical.SetErr(errC)
	canonical.SetArgs([]string{"empty-solution", "-o", "json"})
	require.NoError(t, canonical.Execute())

	deprecated.SetOut(errD)
	deprecated.SetErr(errD)
	deprecated.SetArgs([]string{"empty-solution", "-o", "json"})
	require.NoError(t, deprecated.Execute())

	assert.Equal(t, outC.String(), outD.String(), "canonical and deprecated stdout must match")
	assert.Contains(t, outC.String(), "empty-solution")
	// Deprecation notice goes to the cobra writer (errD), not stdout.
	assert.Contains(t, errD.String(), "is deprecated")
	assert.NotContains(t, outD.String(), "is deprecated")
}

// ── ExplainRun output format tests ───────────────────────────────────────────

func TestExplainRun_JSONOutput(t *testing.T) {
	t.Parallel()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := testCliParams()
	ctx := testContext(ioStreams)

	opts := &RuleOptions{
		BinaryName:     "scafctl",
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
	}

	err := opts.Run(ctx, "empty-solution")
	require.NoError(t, err)

	var rule pkglint.RuleMeta
	err = json.Unmarshal(outBuf.Bytes(), &rule)
	require.NoError(t, err, "output should be valid JSON")
	assert.Equal(t, "empty-solution", rule.Rule)
	assert.Equal(t, "error", rule.Severity)
	assert.Equal(t, "structure", rule.Category)
	assert.NotEmpty(t, rule.Description)
	assert.NotEmpty(t, rule.Why)
	assert.NotEmpty(t, rule.Fix)
	assert.NotEmpty(t, rule.Examples)
}

func TestExplainRun_YAMLOutput(t *testing.T) {
	t.Parallel()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := testCliParams()
	ctx := testContext(ioStreams)

	opts := &RuleOptions{
		BinaryName:     "scafctl",
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "yaml"},
	}

	err := opts.Run(ctx, "empty-solution")
	require.NoError(t, err)

	output := outBuf.String()
	assert.Contains(t, output, "rule: empty-solution")
	assert.Contains(t, output, "severity: error")
	assert.Contains(t, output, "category: structure")
}

func TestExplainRun_TableOutput(t *testing.T) {
	t.Parallel()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := testCliParams()
	ctx := testContext(ioStreams)

	opts := &RuleOptions{
		BinaryName:     "scafctl",
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "table"},
	}

	err := opts.Run(ctx, "empty-solution")
	require.NoError(t, err)

	output := outBuf.String()
	assert.Contains(t, output, "empty-solution")
	assert.Contains(t, output, "error")
	assert.Contains(t, output, "structure")
}

func TestExplainRun_ListOutput(t *testing.T) {
	t.Parallel()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := testCliParams()
	ctx := testContext(ioStreams)

	opts := &RuleOptions{
		BinaryName:     "scafctl",
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "list"},
	}

	err := opts.Run(ctx, "empty-solution")
	require.NoError(t, err)

	output := outBuf.String()
	assert.Contains(t, output, "empty-solution")
	assert.Contains(t, output, "error")
}

func TestExplainRun_CSVOutput(t *testing.T) {
	t.Parallel()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := testCliParams()
	ctx := testContext(ioStreams)

	opts := &RuleOptions{
		BinaryName:     "scafctl",
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "csv"},
	}

	err := opts.Run(ctx, "empty-solution")
	require.NoError(t, err)

	output := outBuf.String()
	assert.NotEmpty(t, output, "CSV output should not be empty")
	assert.Contains(t, output, "empty-solution")
}

// TestExplainRun_NonInteractiveOptions_UseSchemaJSON is the regression test
// for issue #672: the non-interactive output path previously wired an
// interactive display schema (WithOutputDisplaySchemaJSON) instead of a
// column-hint schema (WithOutputSchemaJSON) when building outputOpts. This
// calls the exact same nonInteractiveOutputOptions helper that Run() uses
// and asserts directly on the resulting struct fields, so it fails if the
// wrong setter is reintroduced there -- unlike output-content assertions,
// which render identically either way for this schema/data combination
// because explainColumnOrder already pins ColumnOrder explicitly (bypassing
// any ColumnOrder/HiddenColumns that DisplaySchemaJSON would otherwise
// derive).
func TestExplainRun_NonInteractiveOptions_UseSchemaJSON(t *testing.T) {
	t.Parallel()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)

	opts := &RuleOptions{
		BinaryName:     "scafctl",
		IOStreams:      ioStreams,
		CliParams:      testCliParams(),
		KvxOutputFlags: flags.KvxOutputFlags{Output: "table"},
	}

	outputOpts := opts.nonInteractiveOutputOptions(ctx)

	assert.Equal(t, lintExplainSchemaJSON, outputOpts.SchemaJSON,
		"non-interactive path must use SchemaJSON (column hints), not DisplaySchemaJSON")
	assert.Empty(t, outputOpts.DisplaySchemaJSON,
		"non-interactive path must not set DisplaySchemaJSON; that is reserved for the interactive branch")
}

// TestExplainRun_TableOutput_WithExamples is a smoke test covering table
// rendering for a rule with a real, non-trivial Examples value
// (call-not-found). lintExplainSchemaJSON declares "examples" as a JSON
// array while projectRule() flattens RuleMeta.Examples into a single
// joined string; this confirms that shape difference does not break
// non-interactive table rendering or panic. It does not, by itself,
// discriminate between WithOutputSchemaJSON and WithOutputDisplaySchemaJSON
// for this schema/data combination -- see
// TestExplainRun_NonInteractiveOptions_UseSchemaJSON for the assertion that
// actually regresses issue #672.
func TestExplainRun_TableOutput_WithExamples(t *testing.T) {
	t.Parallel()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := testCliParams()
	ctx := testContext(ioStreams)

	opts := &RuleOptions{
		BinaryName:     "scafctl",
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "table"},
	}

	err := opts.Run(ctx, "call-not-found")
	require.NoError(t, err)

	output := outBuf.String()
	assert.Contains(t, output, "call-not-found")
	assert.Contains(t, output, "error")
	assert.Contains(t, output, "call")
	assert.Contains(t, output, "getUser")
}

// TestExplainRun_ListOutput_WithExamples is the list-format counterpart to
// TestExplainRun_TableOutput_WithExamples, covering issue #672 for the
// "list" output format as well as "table".
func TestExplainRun_ListOutput_WithExamples(t *testing.T) {
	t.Parallel()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := testCliParams()
	ctx := testContext(ioStreams)

	opts := &RuleOptions{
		BinaryName:     "scafctl",
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "list"},
	}

	err := opts.Run(ctx, "call-not-found")
	require.NoError(t, err)

	output := outBuf.String()
	assert.Contains(t, output, "call-not-found")
	assert.Contains(t, output, "error")
	assert.Contains(t, output, "getUser")
}

func TestExplainRun_UnknownRule(t *testing.T) {
	t.Parallel()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := testCliParams()
	ctx := testContext(ioStreams)

	opts := &RuleOptions{
		BinaryName:     "scafctl",
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
	}

	err := opts.Run(ctx, "nonexistent-rule")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown rule")
	assert.Contains(t, err.Error(), "nonexistent-rule")
}

func TestExplainRun_EmbedderBinaryName(t *testing.T) {
	t.Parallel()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := testCliParams()
	cliParams.BinaryName = "mycli"
	ctx := testContext(ioStreams)

	opts := &RuleOptions{
		BinaryName:     "mycli",
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
	}

	err := opts.Run(ctx, "nonexistent-rule")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mycli lint rules", "error should reference embedder binary name")
}

func TestExplainRun_DefaultFormat(t *testing.T) {
	t.Parallel()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := testCliParams()
	ctx := testContext(ioStreams)

	opts := &RuleOptions{
		BinaryName:     "scafctl",
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: flags.KvxOutputFlags{}, // default (auto)
	}

	err := opts.Run(ctx, "empty-solution")
	require.NoError(t, err)

	output := outBuf.String()
	assert.Contains(t, output, "empty-solution")
	assert.Contains(t, output, "error")
	assert.Contains(t, output, "structure")
}

// ── projectRule tests ────────────────────────────────────────────────────────

func TestProjectRule_IncludesAllFields(t *testing.T) {
	t.Parallel()
	rule := pkglint.RuleMeta{
		Rule:        "test-rule",
		Severity:    "warning",
		Category:    "naming",
		Description: "A test rule",
		Why:         "Because testing",
		Fix:         "Fix it",
		Examples:    []string{"example1", "example2"},
	}

	m := projectRule(rule)
	assert.Equal(t, "test-rule", m["rule"])
	assert.Equal(t, "warning", m["severity"])
	assert.Equal(t, "naming", m["category"])
	assert.Equal(t, "A test rule", m["description"])
	assert.Equal(t, "Because testing", m["why"])
	assert.Equal(t, "Fix it", m["fix"])
	assert.Equal(t, "example1\n---\nexample2", m["examples"])
}

func TestProjectRule_OmitsEmptyFields(t *testing.T) {
	t.Parallel()
	rule := pkglint.RuleMeta{
		Rule:        "minimal-rule",
		Severity:    "info",
		Category:    "structure",
		Description: "Minimal",
	}

	m := projectRule(rule)
	assert.Equal(t, "minimal-rule", m["rule"])
	assert.Equal(t, "info", m["severity"])
	assert.Equal(t, "structure", m["category"])
	assert.Equal(t, "Minimal", m["description"])
	_, hasWhy := m["why"]
	assert.False(t, hasWhy, "empty why should be omitted")
	_, hasFix := m["fix"]
	assert.False(t, hasFix, "empty fix should be omitted")
	_, hasExamples := m["examples"]
	assert.False(t, hasExamples, "empty examples should be omitted")
}

func TestProjectRule_SingleExample(t *testing.T) {
	t.Parallel()
	rule := pkglint.RuleMeta{
		Rule:        "single-ex",
		Severity:    "error",
		Category:    "structure",
		Description: "Has one example",
		Examples:    []string{"only-one"},
	}

	m := projectRule(rule)
	assert.Equal(t, "only-one", m["examples"])
}

// ── explainColumnHints tests ─────────────────────────────────────────────────

func TestExplainColumnOrder_HasExpectedFields(t *testing.T) {
	t.Parallel()
	expected := []string{"rule", "severity", "category", "description", "why", "fix", "examples"}
	assert.Equal(t, expected, explainColumnOrder)
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
	hints := findingsColumnHints()
	for _, col := range []string{"severity", "ruleName", "location", "message"} {
		assert.Contains(t, hints, col, "column %q must be present", col)
	}
	assert.Equal(t, 8, hints["severity"].MaxWidth)
	assert.Equal(t, maxRuleWidth, hints["ruleName"].MaxWidth)
	assert.Equal(t, maxLocationWidth, hints["location"].MaxWidth)
	assert.True(t, hints["message"].Flex, "message column must be flex")
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
		File:           solPath,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "table"},
		Severity:       "info",
		CliParams:      testCliParams(),
		IOStreams:      ioStreams,
		BinaryName:     "scafctl",
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
		File:           solPath,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "table"},
		Severity:       "info",
		CliParams:      testCliParams(),
		IOStreams:      ioStreams,
		BinaryName:     "scafctl",
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
		File:           solPath,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		Severity:       "info",
		CliParams:      testCliParams(),
		IOStreams:      ioStreams,
		BinaryName:     "scafctl",
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

	ioStreams, out, _ := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)
	opts := &Options{
		File:           solPath,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "quiet"},
		Severity:       "info",
		CliParams:      testCliParams(),
		IOStreams:      ioStreams,
		BinaryName:     "scafctl",
	}
	err := runLint(ctx, opts)
	assert.NoError(t, err)
	assert.Empty(t, out.String(), "quiet must produce no stdout on a clean solution")
}

// TestRenderResult_QuietProducesNoOutput verifies the shared renderer emits
// nothing for the quiet format, regardless of findings -- so all callers
// (lint, validate solution, validate resolver's gate) honor quiet's
// exit-code-only contract consistently.
func TestRenderResult_QuietProducesNoOutput(t *testing.T) {
	t.Parallel()

	ioStreams, out, errOut := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)

	// A clean result (no findings): must not print "No lint issues found."
	clean := &Result{Findings: nil}
	require.NoError(t, RenderResult(ctx, clean, "scafctl lint", ioStreams,
		flags.KvxOutputFlags{Output: "quiet"}, false))
	assert.Empty(t, out.String())
	assert.Empty(t, errOut.String())

	// A result WITH findings: still no output under quiet.
	withFindings := &Result{Findings: []*Finding{{
		Severity: SeverityError,
		RuleName: "schema-violation",
		Location: "spec",
		Message:  "boom",
	}}}
	require.NoError(t, RenderResult(ctx, withFindings, "scafctl lint", ioStreams,
		flags.KvxOutputFlags{Output: "quiet"}, false))
	assert.Empty(t, out.String())
	assert.Empty(t, errOut.String())
}

func TestRunLint_FileNotFound(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	ctx := testContext(ioStreams)
	opts := &Options{
		File:           "/nonexistent/solution.yaml",
		KvxOutputFlags: flags.KvxOutputFlags{Output: "table"},
		Severity:       "info",
		CliParams:      testCliParams(),
		IOStreams:      ioStreams,
		BinaryName:     "scafctl",
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

// ── Display Schema tests ─────────────────────────────────────────────────────

func TestLintSchemaJSON_IsValidJSON(t *testing.T) {
	t.Parallel()
	assert.True(t, json.Valid(lintSchemaJSON), "lint_schema.json must be valid JSON")
}

func TestLintSchemaJSON_ParsesWithDisplay(t *testing.T) {
	t.Parallel()
	hints, ds, err := tui.ParseSchemaWithDisplay(lintSchemaJSON)
	require.NoError(t, err, "lint_schema.json must parse without error")
	assert.NotNil(t, hints, "should produce column hints")
	assert.NotNil(t, ds, "should produce display schema")
}

func TestLintRulesSchemaJSON_IsValidJSON(t *testing.T) {
	t.Parallel()
	assert.True(t, json.Valid(lintRulesSchemaJSON), "lint_rules_schema.json must be valid JSON")
}

func TestLintRulesSchemaJSON_ParsesWithDisplay(t *testing.T) {
	t.Parallel()
	hints, ds, err := tui.ParseSchemaWithDisplay(lintRulesSchemaJSON)
	require.NoError(t, err, "lint_rules_schema.json must parse without error")
	assert.NotNil(t, hints, "should produce column hints")
	assert.NotNil(t, ds, "should produce display schema")
}

func TestLintExplainSchemaJSON_IsValidJSON(t *testing.T) {
	t.Parallel()
	assert.True(t, json.Valid(lintExplainSchemaJSON), "lint_explain_schema.json must be valid JSON")
}

func TestLintExplainSchemaJSON_ParsesWithDisplay(t *testing.T) {
	t.Parallel()
	hints, ds, err := tui.ParseSchemaWithDisplay(lintExplainSchemaJSON)
	require.NoError(t, err, "lint_explain_schema.json must parse without error")
	assert.NotNil(t, hints, "should produce column hints")
	assert.NotNil(t, ds, "should produce display schema")
}
