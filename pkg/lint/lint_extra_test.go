// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- registryAdapter ----

func TestRegistryAdapter_GetAndHas(t *testing.T) {
	reg := provider.NewRegistry()
	fp := newFakeProvider("myprovider", nil)
	require.NoError(t, reg.Register(fp))

	adapter := &registryAdapter{registry: reg}

	p, ok := adapter.Get("myprovider")
	assert.True(t, ok)
	assert.NotNil(t, p)

	_, ok = adapter.Get("nonexistent")
	assert.False(t, ok)

	assert.True(t, adapter.Has("myprovider"))
	assert.False(t, adapter.Has("nonexistent"))
}

// ---- FilterBySeverity ----

func makeResultWithFindings() *Result {
	r := &Result{File: "test.yaml"}
	r.addFinding(SeverityError, "cat", "loc", "an error", "", "err-rule")
	r.addFinding(SeverityWarning, "cat", "loc", "a warning", "", "warn-rule")
	r.addFinding(SeverityInfo, "cat", "loc", "an info", "", "info-rule")
	return r
}

func TestFilterBySeverity_Error(t *testing.T) {
	result := makeResultWithFindings()
	filtered := FilterBySeverity(result, "error")
	assert.Len(t, filtered.Findings, 1)
	assert.Equal(t, SeverityError, filtered.Findings[0].Severity)
	assert.Equal(t, 1, filtered.ErrorCount)
	assert.Equal(t, 0, filtered.WarnCount)
	assert.Equal(t, 0, filtered.InfoCount)
}

func TestFilterBySeverity_Warning(t *testing.T) {
	result := makeResultWithFindings()
	filtered := FilterBySeverity(result, "warning")
	assert.Len(t, filtered.Findings, 2)
	assert.Equal(t, 1, filtered.ErrorCount)
	assert.Equal(t, 1, filtered.WarnCount)
}

func TestFilterBySeverity_Info(t *testing.T) {
	result := makeResultWithFindings()
	filtered := FilterBySeverity(result, "info")
	assert.Len(t, filtered.Findings, 3)
}

func TestFilterBySeverity_Unknown(t *testing.T) {
	// Unknown min severity defaults to info (minLevel = 1)
	result := makeResultWithFindings()
	filtered := FilterBySeverity(result, "unknown")
	assert.Len(t, filtered.Findings, 3)
}

func TestFilterBySeverity_PreservesFile(t *testing.T) {
	result := &Result{File: "mysolution.yaml"}
	filtered := FilterBySeverity(result, "error")
	assert.Equal(t, "mysolution.yaml", filtered.File)
}

// ---- validateCELSyntax ----

func TestValidateCELSyntax_Valid(t *testing.T) {
	assert.NoError(t, validateCELSyntax("1 + 1"))
	assert.NoError(t, validateCELSyntax("_.myResolver"))
	assert.NoError(t, validateCELSyntax("'hello' + ' world'"))
}

func TestValidateCELSyntax_Invalid(t *testing.T) {
	err := validateCELSyntax("1 +++ invalid %%%")
	assert.Error(t, err)
}

// ---- validateTemplateSyntax ----

func TestValidateTemplateSyntax_Valid(t *testing.T) {
	assert.NoError(t, validateTemplateSyntax("Hello, {{ .Name }}!"))
	assert.NoError(t, validateTemplateSyntax("plain text"))
	assert.NoError(t, validateTemplateSyntax("{{ if .Cond }}yes{{ end }}"))
}

func TestValidateTemplateSyntax_Invalid(t *testing.T) {
	err := validateTemplateSyntax("{{ .Name")
	assert.Error(t, err)
}

// ---- isCoveredByBundleInclude ----

func TestIsCoveredByBundleInclude(t *testing.T) {
	includes := []string{"templates/**/*.yaml", "config/*.json"}

	assert.True(t, isCoveredByBundleInclude("templates/sub/file.yaml", includes))
	assert.True(t, isCoveredByBundleInclude("config/app.json", includes))
	assert.False(t, isCoveredByBundleInclude("other/file.txt", includes))
	assert.False(t, isCoveredByBundleInclude("templates/file.txt", includes))
}

func TestIsCoveredByBundleInclude_Empty(t *testing.T) {
	assert.False(t, isCoveredByBundleInclude("anything.yaml", nil))
}

// ---- testFileReachable ----

func TestTestFileReachable_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "test-*.yaml")
	require.NoError(t, err)
	f.Close()

	base := filepath.Base(f.Name())
	assert.True(t, testFileReachable(dir, base))
}

func TestTestFileReachable_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, testFileReachable(dir, "nonexistent.yaml"))
}

func TestTestFileReachable_GlobPattern(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "case-*.yaml")
	require.NoError(t, err)
	f.Close()

	assert.True(t, testFileReachable(dir, "case-*.yaml"))
	assert.False(t, testFileReachable(dir, "nope-*.yaml"))
}

func TestTestFileReachable_PathTraversal(t *testing.T) {
	// Path traversal and absolute paths should return true (don't double-flag)
	assert.True(t, testFileReachable("/some/dir", "../other/file.yaml"))
	assert.True(t, testFileReachable("/some/dir", "/absolute/path.yaml"))
}

func TestTestFileReachable_RootSolutionDir(t *testing.T) {
	// When solutionDir is "/" the old HasPrefix check produced "//" which no
	// normal path starts with, causing false negatives. The Rel-based check
	// should correctly handle this edge case.
	dir := t.TempDir()
	// dir is an absolute path like /tmp/TestXxx12345; relative to "/" it is
	// the same path with the leading slash stripped.
	relDir := dir[1:] // strip leading "/"

	// The temp dir exists on disk, so it should be reachable from "/"
	assert.True(t, testFileReachable("/", relDir))

	// A nonexistent entry inside / should be false (file missing).
	assert.False(t, testFileReachable("/", "this_path_does_not_exist_scafctl_xyz"))
}

// ---- lintAction ----

func doLintAction(act *action.Action) *Result {
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("known-provider", nil))
	result := &Result{}
	lintAction(act, "workflow.actions.myaction", map[string]bool{"step1": true}, result, reg)
	return result
}

func hasRuleName(findings []*Finding, ruleName string) bool {
	for _, f := range findings {
		if f.RuleName == ruleName {
			return true
		}
	}
	return false
}

func TestLintAction_MissingDescription(t *testing.T) {
	assert.True(t, hasRuleName(doLintAction(&action.Action{Provider: "known-provider"}).Findings, "missing-description"))
}

func TestLintAction_UnknownProvider(t *testing.T) {
	result := doLintAction(&action.Action{Description: "do it", Provider: "unknown"})
	assert.True(t, hasRuleName(result.Findings, "missing-provider"))
}

func TestLintAction_InvalidDependency(t *testing.T) {
	result := doLintAction(&action.Action{
		Description: "do it",
		Provider:    "known-provider",
		DependsOn:   []string{"nonexistent-step"},
	})
	assert.True(t, hasRuleName(result.Findings, "invalid-dependency"))
}

func TestLintAction_LongTimeout(t *testing.T) {
	result := doLintAction(&action.Action{
		Description: "slow",
		Provider:    "known-provider",
		Timeout:     &duration.Duration{Duration: 15 * time.Minute},
	})
	assert.True(t, hasRuleName(result.Findings, "long-timeout"))
}

func TestLintAction_ShortTimeoutNoFinding(t *testing.T) {
	result := doLintAction(&action.Action{
		Description: "fast",
		Provider:    "known-provider",
		Timeout:     &duration.Duration{Duration: 30 * time.Second},
	})
	assert.False(t, hasRuleName(result.Findings, "long-timeout"))
}

// ---- lintResultSchema ----

func TestLintResultSchema_ValidTyped(t *testing.T) {
	schema := &jsonschema.Schema{Type: "object"}
	result := &Result{}
	lintResultSchema(schema, "workflow.actions.foo.resultSchema", result)
	assert.Empty(t, result.Findings)
}

func TestLintResultSchema_NoType(t *testing.T) {
	schema := &jsonschema.Schema{}
	result := &Result{}
	lintResultSchema(schema, "workflow.actions.foo.resultSchema", result)
	assert.True(t, hasRuleName(result.Findings, "permissive-result-schema"))
}

func TestLintResultSchema_UndefinedRequiredProperty(t *testing.T) {
	schema := &jsonschema.Schema{
		Type:     "object",
		Required: []string{"name"},
		Properties: map[string]*jsonschema.Schema{
			"other": {Type: "string"},
		},
	}
	result := &Result{}
	lintResultSchema(schema, "test.resultSchema", result)
	assert.True(t, hasRuleName(result.Findings, "undefined-required-property"))
}

func TestLintResultSchema_NestedProperties(t *testing.T) {
	schema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"child": {Type: "string"},
		},
	}
	result := &Result{}
	lintResultSchema(schema, "test.resultSchema", result)
	assert.Empty(t, result.Findings)
}

func TestLintResultSchema_WithItems(t *testing.T) {
	schema := &jsonschema.Schema{
		Type:  "array",
		Items: &jsonschema.Schema{Type: "string"},
	}
	result := &Result{}
	lintResultSchema(schema, "test.resultSchema", result)
	assert.Empty(t, result.Findings)
}

func TestLintWorkflow_Nil(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Workflow = nil
	result := &Result{}
	reg := provider.NewRegistry()
	lintWorkflow(sol, result, reg)
	assert.Empty(t, result.Findings)
}

func TestLintWorkflow_EmptyWorkflow(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Workflow = &action.Workflow{}
	result := &Result{}
	reg := provider.NewRegistry()
	lintWorkflow(sol, result, reg)
	assert.NotEmpty(t, result.Findings)
	assert.Equal(t, "empty-workflow", result.Findings[0].RuleName)
}

func TestLintWorkflow_FinallyWithNoActions(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Workflow = &action.Workflow{
		Finally: map[string]*action.Action{
			"cleanup": {Provider: "noop"},
		},
	}
	result := &Result{}
	reg := provider.NewRegistry()
	lintWorkflow(sol, result, reg)

	rules := make([]string, 0, len(result.Findings))
	for _, f := range result.Findings {
		rules = append(rules, f.RuleName)
	}
	assert.Contains(t, rules, "unused-finally")
}

func TestLintWorkflow_WithActions(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"step1": {Provider: "noop"},
		},
	}
	result := &Result{}
	reg := provider.NewRegistry()
	lintWorkflow(sol, result, reg)
	// No empty-workflow finding expected
	for _, f := range result.Findings {
		assert.NotEqual(t, "empty-workflow", f.RuleName)
	}
}

func TestLintTests_HasTesting_Empty(t *testing.T) {
	sol := &solution.Solution{}
	result := &Result{}
	// No testing defined — should return early
	lintTests(sol, "/tmp/test.yaml", result)
	assert.Empty(t, result.Findings)
}

func TestLintTests_InvalidTestName(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Testing = &soltesting.TestSuite{
		Cases: map[string]*soltesting.TestCase{
			"!invalid-name!": {},
		},
	}
	result := &Result{}
	lintTests(sol, "/tmp/test.yaml", result)
	rules := make([]string, 0, len(result.Findings))
	for _, f := range result.Findings {
		rules = append(rules, f.RuleName)
	}
	assert.Contains(t, rules, "invalid-test-name")
}

func TestLintTests_InvalidTemplateName(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Testing = &soltesting.TestSuite{
		Cases: map[string]*soltesting.TestCase{
			"_bad template": {},
		},
	}
	result := &Result{}
	lintTests(sol, "/tmp/test.yaml", result)
	rules := make([]string, 0, len(result.Findings))
	for _, f := range result.Findings {
		rules = append(rules, f.RuleName)
	}
	assert.Contains(t, rules, "invalid-test-name")
}

func TestLintTests_UnusedTemplate(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Testing = &soltesting.TestSuite{
		Cases: map[string]*soltesting.TestCase{
			"_myTemplate": {},
			"testA":       {},
		},
	}
	result := &Result{}
	lintTests(sol, "/tmp/test.yaml", result)
	rules := make([]string, 0, len(result.Findings))
	for _, f := range result.Findings {
		rules = append(rules, f.RuleName)
	}
	assert.Contains(t, rules, "unused-template")
}

func TestLintTests_UsedTemplate_NoFinding(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Testing = &soltesting.TestSuite{
		Cases: map[string]*soltesting.TestCase{
			"_myTemplate": {},
			"testA":       {Extends: []string{"_myTemplate"}},
		},
	}
	result := &Result{}
	lintTests(sol, "/tmp/test.yaml", result)
	for _, f := range result.Findings {
		assert.NotEqual(t, "unused-template", f.RuleName)
	}
}

func TestAddFinding_WithSourceMap(t *testing.T) {
	sm := sourcepos.NewSourceMap()
	sm.Set("spec.resolvers.foo", sourcepos.Position{Line: 10, Column: 5, File: "sol.yaml"})

	result := &Result{sourceMap: sm}
	result.addFinding(SeverityError, "structure", "resolvers.foo", "msg", "suggestion", "test-rule")

	require.Len(t, result.Findings, 1)
	f := result.Findings[0]
	assert.Equal(t, 10, f.Line)
	assert.Equal(t, 5, f.Column)
	assert.Equal(t, "sol.yaml", f.SourceFile)
}

func TestAddFinding_WithSourceMap_RawLocation(t *testing.T) {
	sm := sourcepos.NewSourceMap()
	sm.Set("workflow.actions.build", sourcepos.Position{Line: 42, Column: 3, File: ""})

	result := &Result{sourceMap: sm}
	result.addFinding(SeverityError, "structure", "workflow.actions.build", "msg", "", "test-rule")

	require.Len(t, result.Findings, 1)
	assert.Equal(t, 42, result.Findings[0].Line)
}

func TestAddFinding_NoSourceMap(t *testing.T) {
	result := &Result{}
	result.addFinding(SeverityWarning, "usage", "resolvers.foo", "msg", "", "test-rule")
	require.Len(t, result.Findings, 1)
	assert.Equal(t, 0, result.Findings[0].Line)
}
