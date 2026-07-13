// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/state"
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
	// Optional access syntax must parse -- the env enables cel.OptionalTypes()
	// to match the reference collector and runtime evaluation.
	assert.NoError(t, validateCELSyntax(`_.?optionalDep.orValue("")`))
	assert.NoError(t, validateCELSyntax(`_[?"optionalDep"].orValue("")`))
}

func TestValidateCELSyntax_Invalid(t *testing.T) {
	err := validateCELSyntax("1 +++ invalid %%%")
	assert.Error(t, err)
}

// ---- lintOrValueOnConcrete (orvalue-on-non-optional) ----

func collectOrValueFindings(expr string) *Result {
	result := &Result{}
	ast, err := parseCELForLint(expr)
	if err != nil {
		return result
	}
	lintOrValueOnConcrete(ast, "test.location", result)
	return result
}

func TestLintOrValueOnConcrete_FlagsConcreteReceivers(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want string // substring expected in the message
	}{
		{"plain ident", `__self.orValue([])`, "__self"},
		{"field access", `_.field.orValue("")`, "_.field"},
		{"nested field access", `_.config.host.orValue("")`, "_.config.host"},
		{"literal receiver", `"x".orValue("")`, "<literal>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := collectOrValueFindings(tc.expr)
			require.Len(t, result.Findings, 1)
			f := result.Findings[0]
			assert.Equal(t, SeverityError, f.Severity)
			assert.Equal(t, "orvalue-on-non-optional", f.RuleName)
			assert.Equal(t, "expression", f.Category)
			assert.Contains(t, f.Message, tc.want)
		})
	}
}

func TestLintOrValueOnConcrete_AllowsOptionalReceivers(t *testing.T) {
	cases := []string{
		`_.?field.orValue("")`,
		`_[?"field"].orValue("")`,
		`_.?config.host.orValue("")`,
		`optional.of("x").orValue("")`,
		`optional.ofNonZeroValue(_.field).orValue("")`,
		// Result of another call is not provably concrete.
		`_.field.split(",").orValue([])`,
	}
	for _, expr := range cases {
		t.Run(expr, func(t *testing.T) {
			result := collectOrValueFindings(expr)
			assert.Empty(t, result.Findings, "expected no findings for %q", expr)
		})
	}
}

func TestLintOrValueOnConcrete_NoOrValueCall(t *testing.T) {
	result := collectOrValueFindings(`_.field + "x"`)
	assert.Empty(t, result.Findings)
}

func TestLintOrValueOnConcrete_FlagsNestedOccurrence(t *testing.T) {
	// orValue on a concrete receiver buried inside a larger expression.
	result := collectOrValueFindings(`[1, 2, _.field.orValue("")]`)
	require.Len(t, result.Findings, 1)
	assert.Contains(t, result.Findings[0].Message, "_.field")
}

func TestLintOrValueOnConcrete_DeduplicatesSameReceiver(t *testing.T) {
	result := collectOrValueFindings(`_.field.orValue("") + _.field.orValue("x")`)
	assert.Len(t, result.Findings, 1)
}

func TestLintOrValueOnConcrete_IgnoresSyntaxErrors(t *testing.T) {
	// Invalid syntax must not panic and must produce no findings (callers
	// run validateCELSyntax first and skip this check on parse failure).
	result := collectOrValueFindings(`1 +++ %%%`)
	assert.Empty(t, result.Findings)
}

func TestOrValueSuggestion(t *testing.T) {
	cases := []struct {
		name        string
		receiver    string
		wantExample string // substring expected in the suggestion
	}{
		{"underscore-rooted field", "_.field", "'_.?field'"},
		{"underscore-rooted nested", "_.config.host", "'_.?config.host'"},
		{"self receiver", "__self", "'_.?name'"},
		{"parent receiver", "__parent", "'_.?name'"},
		{"literal receiver", "<literal>", "'_.?name'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := orValueSuggestion(tc.receiver)
			assert.Contains(t, got, tc.wantExample)
			// Never emit an invalid example that splices a non-"_."-rooted
			// receiver in after "_.?".
			assert.NotContains(t, got, "_.?__")
			assert.NotContains(t, got, "_.?<literal>")
		})
	}
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
	// When solutionDir is the filesystem root, the Rel-based check should still
	// allow reachable paths under that root.
	dir := t.TempDir()
	root := string(filepath.Separator)
	if volume := filepath.VolumeName(dir); volume != "" {
		root = volume + string(filepath.Separator)
	}
	relDir, err := filepath.Rel(root, dir)
	require.NoError(t, err)

	// The temp dir exists on disk, so it should be reachable from the root.
	assert.True(t, testFileReachable(root, relDir))

	// A nonexistent entry inside the root should be false (file missing).
	assert.False(t, testFileReachable(root, "this_path_does_not_exist_scafctl_xyz"))
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

func TestLintResolvers_ValidateBlockNotFlaggedUnused(t *testing.T) {
	// A resolver with a validate block should not be flagged as unused,
	// even if it is not referenced by any other resolver or action.
	reg := provider.NewRegistry()
	fp := newFakeProvider("static", nil)
	require.NoError(t, reg.Register(fp))
	vp := newFakeProvider("validation", nil)
	require.NoError(t, reg.Register(vp))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"versionValidated": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static"},
				},
			},
			Validate: &resolver.ValidatePhase{
				With: []resolver.ProviderValidation{
					{Provider: "validation"},
				},
			},
		},
	}

	referencedResolvers := collectReferencedResolvers(sol)
	result := &Result{}
	lintResolvers(sol, result, reg, referencedResolvers)

	for _, f := range result.Findings {
		assert.NotEqual(t, "unused-resolver", f.RuleName,
			"resolver with validate block should not be flagged as unused")
	}
}

func TestLintResolvers_NoValidateBlockFlaggedUnused(t *testing.T) {
	// A resolver without a validate block that is not referenced should be flagged.
	reg := provider.NewRegistry()
	fp := newFakeProvider("static", nil)
	require.NoError(t, reg.Register(fp))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"unreferenced": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static"},
				},
			},
		},
	}

	referencedResolvers := collectReferencedResolvers(sol)
	result := &Result{}
	lintResolvers(sol, result, reg, referencedResolvers)

	rules := make([]string, 0, len(result.Findings))
	for _, f := range result.Findings {
		rules = append(rules, f.RuleName)
	}
	assert.Contains(t, rules, "unused-resolver")
}

func TestLintResolvers_CELExpressionInputNotFlaggedUnused(t *testing.T) {
	// Resolvers referenced only via _.name in a CEL provider's expression
	// input (a string literal) should NOT be flagged as unused.
	reg := provider.NewRegistry()
	celProv := newFakeProvider("cel", nil)
	execProv := newFakeProvider("exec", nil)
	require.NoError(t, reg.Register(celProv))
	require.NoError(t, reg.Register(execProv))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"winResult": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "exec"}},
			},
		},
		"unixResult": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "exec"}},
			},
		},
		"result": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{
					Provider: "cel",
					Inputs: map[string]*spec.ValueRef{
						"expression": {
							Literal: `has(_.winResult) ? _.winResult : _.unixResult`,
						},
					},
				}},
			},
		},
	}

	referencedResolvers := collectReferencedResolvers(sol)
	result := &Result{}
	lintResolvers(sol, result, reg, referencedResolvers)

	for _, f := range result.Findings {
		if f.RuleName == "unused-resolver" {
			assert.NotContains(t, f.Message, "winResult",
				"winResult referenced in CEL expression should not be flagged unused")
			assert.NotContains(t, f.Message, "unixResult",
				"unixResult referenced in CEL expression should not be flagged unused")
		}
	}
}

func TestLintResolvers_OptionalAccessNotFlaggedUnused(t *testing.T) {
	// A resolver referenced only via optional access (_.?name) must NOT be
	// flagged as unused -- the AST-based collector recognizes optional select.
	optExpr := celexp.Expression(`_.?optionalDep.orValue("")`)

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("static", nil)))
	require.NoError(t, reg.Register(newFakeProvider("cel", nil)))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"optionalDep": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
		"consumer": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{
					Provider: "cel",
					Inputs: map[string]*spec.ValueRef{
						"expression": {Expr: &optExpr},
					},
				}},
			},
		},
	}

	referencedResolvers := collectReferencedResolvers(sol)
	assert.True(t, referencedResolvers["optionalDep"],
		"optional access _.?optionalDep should mark optionalDep as referenced")

	result := &Result{}
	lintResolvers(sol, result, reg, referencedResolvers)
	for _, f := range result.Findings {
		if f.RuleName == "unused-resolver" {
			assert.NotContains(t, f.Message, "optionalDep",
				"optionalDep referenced via optional access should not be flagged unused")
		}
	}
}

func TestLintResolvers_DependsOnCountedAsUsage(t *testing.T) {
	// A resolver referenced only via another resolver's dependsOn must NOT be
	// flagged as unused.
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("static", nil)))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"base": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
		"dependent": {
			DependsOn: []string{"base"},
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
	}

	referencedResolvers := collectReferencedResolvers(sol)
	assert.True(t, referencedResolvers["base"],
		"dependsOn membership should mark base as referenced")

	result := &Result{}
	lintResolvers(sol, result, reg, referencedResolvers)
	for _, f := range result.Findings {
		if f.RuleName == "unused-resolver" {
			assert.NotContains(t, f.Message, "base",
				"base referenced via dependsOn should not be flagged unused")
		}
	}
}

func TestLintResolvers_StateReferenceCountedAsUsage(t *testing.T) {
	// Resolvers referenced only from the state block (saveOverrides rslvr and a
	// backend input expression) must NOT be flagged as unused.
	branch := "featureBranch"
	pathExpr := celexp.Expression("_.statePath")

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"featureBranch": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
		"statePath": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
	}
	sol.State = &state.Config{
		Enabled: &spec.ValueRef{Literal: true},
		Backend: state.Backend{
			Provider:      "github",
			Inputs:        map[string]*spec.ValueRef{"path": {Expr: &pathExpr}},
			SaveOverrides: map[string]*spec.ValueRef{"branch": {Resolver: &branch}},
		},
	}

	referencedResolvers := collectReferencedResolvers(sol)
	assert.True(t, referencedResolvers["featureBranch"],
		"saveOverrides rslvr reference should mark featureBranch as referenced")
	assert.True(t, referencedResolvers["statePath"],
		"backend input expr reference should mark statePath as referenced")
}

func TestLintResolvers_HyphenatedName(t *testing.T) {
	reg := provider.NewRegistry()
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"my-resolver": {
			Description: "has hyphens",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static"},
				},
			},
		},
		"my_resolver": {
			Description: "uses underscores",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static"},
				},
			},
		},
	}

	referencedResolvers := map[string]bool{"my-resolver": true, "my_resolver": true}
	result := &Result{}
	lintResolvers(sol, result, reg, referencedResolvers)

	var hyphenFindings []*Finding
	for _, f := range result.Findings {
		if f.RuleName == "hyphenated-name" {
			hyphenFindings = append(hyphenFindings, f)
		}
	}

	require.Len(t, hyphenFindings, 1)
	assert.Contains(t, hyphenFindings[0].Message, "my-resolver")
	assert.Contains(t, hyphenFindings[0].Message, "myResolver")
	assert.Equal(t, SeverityWarning, hyphenFindings[0].Severity)
}

// ---- missing-fallback-source ----

func TestLintResolvers_MissingFallbackSource_AllConditional(t *testing.T) {
	expr1 := celexp.Expression("_.authenticated == true")
	expr2 := celexp.Expression("_.fallbackEnabled == true")

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("http", nil)))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"myResolver": {
			Description: "all sources conditional",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "http", When: &resolver.Condition{Expr: &expr1}},
					{Provider: "http", When: &resolver.Condition{Expr: &expr2}},
				},
			},
		},
	}

	referencedResolvers := map[string]bool{"myResolver": true}
	result := &Result{}
	lintResolvers(sol, result, reg, referencedResolvers)

	var findings []*Finding
	for _, f := range result.Findings {
		if f.RuleName == "missing-fallback-source" {
			findings = append(findings, f)
		}
	}

	require.Len(t, findings, 1)
	assert.Equal(t, SeverityWarning, findings[0].Severity)
	assert.Equal(t, "resolvers.myResolver.resolve", findings[0].Location)
	assert.Contains(t, findings[0].Message, "no unconditional fallback")
}

func TestLintResolvers_MissingFallbackSource_HasUnconditionalSource(t *testing.T) {
	expr1 := celexp.Expression("_.authenticated == true")

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("http", nil)))
	require.NoError(t, reg.Register(newFakeProvider("static", nil)))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"myResolver": {
			Description: "has unconditional fallback",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "http", When: &resolver.Condition{Expr: &expr1}},
					{Provider: "static"},
				},
			},
		},
	}

	referencedResolvers := map[string]bool{"myResolver": true}
	result := &Result{}
	lintResolvers(sol, result, reg, referencedResolvers)

	for _, f := range result.Findings {
		assert.NotEqual(t, "missing-fallback-source", f.RuleName,
			"should not warn when an unconditional source exists")
	}
}

func TestLintResolvers_MissingFallbackSource_WhenTrue(t *testing.T) {
	exprCond := celexp.Expression("_.environment == 'prod'")
	exprTrue := celexp.Expression("true")

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("http", nil)))
	require.NoError(t, reg.Register(newFakeProvider("static", nil)))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"myResolver": {
			Description: "has when: true fallback",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "http", When: &resolver.Condition{Expr: &exprCond}},
					{Provider: "static", When: &resolver.Condition{Expr: &exprTrue}},
				},
			},
		},
	}

	referencedResolvers := map[string]bool{"myResolver": true}
	result := &Result{}
	lintResolvers(sol, result, reg, referencedResolvers)

	for _, f := range result.Findings {
		assert.NotEqual(t, "missing-fallback-source", f.RuleName,
			"should not warn when a source has when: true (always passes)")
	}
}

func TestLintResolvers_MissingFallbackSource_WhenTrueWithWhitespace(t *testing.T) {
	exprCond := celexp.Expression("_.environment == 'prod'")
	exprTrue := celexp.Expression("  true  ")

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("http", nil)))
	require.NoError(t, reg.Register(newFakeProvider("static", nil)))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"myResolver": {
			Description: "has when: '  true  ' with whitespace",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "http", When: &resolver.Condition{Expr: &exprCond}},
					{Provider: "static", When: &resolver.Condition{Expr: &exprTrue}},
				},
			},
		},
	}

	referencedResolvers := map[string]bool{"myResolver": true}
	result := &Result{}
	lintResolvers(sol, result, reg, referencedResolvers)

	for _, f := range result.Findings {
		assert.NotEqual(t, "missing-fallback-source", f.RuleName,
			"should not warn when a source has when: '  true  ' (whitespace-padded true)")
	}
}

func TestLintResolvers_MissingFallbackSource_NoResolvePhase(t *testing.T) {
	reg := provider.NewRegistry()

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"myResolver": {
			Description: "no resolve phase",
		},
	}

	referencedResolvers := map[string]bool{"myResolver": true}
	result := &Result{}
	lintResolvers(sol, result, reg, referencedResolvers)

	for _, f := range result.Findings {
		assert.NotEqual(t, "missing-fallback-source", f.RuleName,
			"should not warn when there is no resolve phase")
	}
}

// ---- parameter-missing-default ----

func filterByRule(findings []*Finding) []*Finding {
	var out []*Finding
	for _, f := range findings {
		if f.RuleName == "parameter-missing-default" {
			out = append(out, f)
		}
	}
	return out
}

func TestLintResolvers_ParameterMissingDefault_Fires(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("parameter", nil)))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"environment": {
			Description: "param with no default and no fallback",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*spec.ValueRef{
						"key": {Literal: "environment"},
					}},
				},
			},
		},
	}

	result := &Result{}
	lintResolvers(sol, result, reg, map[string]bool{"environment": true})

	findings := filterByRule(result.Findings)
	require.Len(t, findings, 1)
	assert.Equal(t, SeverityWarning, findings[0].Severity)
	assert.Equal(t, "resolvers.environment.resolve", findings[0].Location)
	assert.Contains(t, findings[0].Message, "no 'default'")
}

func TestLintResolvers_ParameterMissingDefault_HasDefault(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("parameter", nil)))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"environment": {
			Description: "param with default",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*spec.ValueRef{
						"key":     {Literal: "environment"},
						"default": {Literal: "development"},
					}},
				},
			},
		},
	}

	result := &Result{}
	lintResolvers(sol, result, reg, map[string]bool{"environment": true})

	assert.Empty(t, filterByRule(result.Findings),
		"should not warn when the parameter source declares a default")
}

func TestLintResolvers_ParameterMissingDefault_HasUnconditionalFallback(t *testing.T) {
	exprCond := celexp.Expression("_.useParam == true")

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("parameter", nil)))
	require.NoError(t, reg.Register(newFakeProvider("static", nil)))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"environment": {
			Description: "param with static fallback",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "parameter", When: &resolver.Condition{Expr: &exprCond}, Inputs: map[string]*spec.ValueRef{
						"key": {Literal: "environment"},
					}},
					{Provider: "static", Inputs: map[string]*spec.ValueRef{
						"value": {Literal: "development"},
					}},
				},
			},
		},
	}

	result := &Result{}
	lintResolvers(sol, result, reg, map[string]bool{"environment": true})

	assert.Empty(t, filterByRule(result.Findings),
		"should not warn when an unconditional non-parameter fallback exists")
}

func TestLintResolvers_ParameterMissingDefault_AllConditional(t *testing.T) {
	exprCond := celexp.Expression("_.useParam == true")

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("parameter", nil)))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"environment": {
			Description: "conditional param only",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "parameter", When: &resolver.Condition{Expr: &exprCond}, Inputs: map[string]*spec.ValueRef{
						"key": {Literal: "environment"},
					}},
				},
			},
		},
	}

	result := &Result{}
	lintResolvers(sol, result, reg, map[string]bool{"environment": true})

	// The all-conditional case is covered by missing-fallback-source; the
	// parameter-missing-default rule only targets unconditional parameter
	// sources to avoid duplicate findings.
	assert.Empty(t, filterByRule(result.Findings),
		"should not warn for a conditional parameter source (handled by missing-fallback-source)")
}

// ---- parameter-numeric-matches ----

func filterByNumericMatches(findings []*Finding) []*Finding {
	var out []*Finding
	for _, f := range findings {
		if f.RuleName == "parameter-numeric-matches" {
			out = append(out, f)
		}
	}
	return out
}

func numericMatchesResolver(defaultRef, typeRef *spec.ValueRef) *resolver.Resolver {
	inputs := map[string]*spec.ValueRef{"key": {Literal: "version"}}
	if defaultRef != nil {
		inputs["default"] = defaultRef
	}
	if typeRef != nil {
		inputs["type"] = typeRef
	}
	matchExpr := celexp.Expression(`__self.matches("^[0-9]+$")`)
	return &resolver.Resolver{
		Resolve: &resolver.ResolvePhase{
			With: []resolver.ProviderSource{
				{Provider: "parameter", Inputs: inputs},
			},
		},
		Validate: &resolver.ValidatePhase{
			With: []resolver.ProviderValidation{
				{Provider: "validation", Inputs: map[string]*spec.ValueRef{
					"expression": {Expr: &matchExpr},
				}},
			},
		},
	}
}

func TestLintParameterNumericMatches_Fires(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"version": numericMatchesResolver(&spec.ValueRef{Literal: 1}, nil),
	}

	result := &Result{}
	lintParameterNumericMatches(sol, result)

	findings := filterByNumericMatches(result.Findings)
	require.Len(t, findings, 1)
	assert.Equal(t, SeverityWarning, findings[0].Severity)
	assert.Equal(t, "resolvers.version", findings[0].Location)
	assert.Contains(t, findings[0].Message, "matches()")
}

func TestLintParameterNumericMatches_NumericStringDefault(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"version": numericMatchesResolver(&spec.ValueRef{Literal: "8080"}, nil),
	}

	result := &Result{}
	lintParameterNumericMatches(sol, result)

	assert.Len(t, filterByNumericMatches(result.Findings), 1,
		"numeric-looking string default should also trigger the rule")
}

func TestLintParameterNumericMatches_ExplicitStringType(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"version": numericMatchesResolver(
			&spec.ValueRef{Literal: 1},
			&spec.ValueRef{Literal: "string"},
		),
	}

	result := &Result{}
	lintParameterNumericMatches(sol, result)

	assert.Empty(t, filterByNumericMatches(result.Findings),
		"explicit type should suppress the warning")
}

func TestLintParameterNumericMatches_AutoTypeStillFires(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"version": numericMatchesResolver(
			&spec.ValueRef{Literal: 1},
			&spec.ValueRef{Literal: "auto"},
		),
	}

	result := &Result{}
	lintParameterNumericMatches(sol, result)

	assert.Len(t, filterByNumericMatches(result.Findings), 1,
		"type: auto is the inference default and should not suppress the warning")
}

func TestLintParameterNumericMatches_ResolverTypeString(t *testing.T) {
	res := numericMatchesResolver(&spec.ValueRef{Literal: 1}, nil)
	res.Type = resolver.Type("string")

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{"version": res}

	result := &Result{}
	lintParameterNumericMatches(sol, result)

	assert.Empty(t, filterByNumericMatches(result.Findings),
		"resolver-level type: string coerces the value and should suppress the warning")
}

func TestLintParameterNumericMatches_NoMatches(t *testing.T) {
	res := numericMatchesResolver(&spec.ValueRef{Literal: 1}, nil)
	res.Validate = nil

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{"version": res}

	result := &Result{}
	lintParameterNumericMatches(sol, result)

	assert.Empty(t, filterByNumericMatches(result.Findings),
		"no matches() call means no footgun to warn about")
}

func TestLintParameterNumericMatches_NonNumericDefault(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"version": numericMatchesResolver(&spec.ValueRef{Literal: "production"}, nil),
	}

	result := &Result{}
	lintParameterNumericMatches(sol, result)

	assert.Empty(t, filterByNumericMatches(result.Findings),
		"non-numeric default is not coerced to an integer")
}

func TestLintParameterNumericMatches_MatchesInTransform(t *testing.T) {
	matchExpr := celexp.Expression(`__self.matches("^[0-9]+$") ? "ok" : "bad"`)
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"version": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*spec.ValueRef{
						"key":     {Literal: "version"},
						"default": {Literal: 1},
					}},
				},
			},
			Transform: &resolver.TransformPhase{
				With: []resolver.ProviderTransform{
					{Provider: "cel", Inputs: map[string]*spec.ValueRef{
						"expression": {Expr: &matchExpr},
					}},
				},
			},
		},
	}

	result := &Result{}
	lintParameterNumericMatches(sol, result)

	assert.Len(t, filterByNumericMatches(result.Findings), 1,
		"matches() in a transform expression should trigger the rule")
}

func TestLintParameterNumericMatches_LiteralExpressionString(t *testing.T) {
	// The validation provider takes its CEL as a plain string literal, not an
	// expr: ValueRef. The rule must still detect matches() in that form.
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"version": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "parameter", Inputs: map[string]*spec.ValueRef{
						"key":     {Literal: "version"},
						"default": {Literal: 1},
					}},
				},
			},
			Validate: &resolver.ValidatePhase{
				With: []resolver.ProviderValidation{
					{Provider: "validation", Inputs: map[string]*spec.ValueRef{
						"expression": {Literal: `__self.matches("^[0-9]+$")`},
					}},
				},
			},
		},
	}

	result := &Result{}
	lintParameterNumericMatches(sol, result)

	assert.Len(t, filterByNumericMatches(result.Findings), 1,
		"matches() in a literal expression string should trigger the rule")
}

// ---- hyphensToCamelCase ----

func TestHyphensToCamelCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"my-resolver", "myResolver"},
		{"my-resolver-name", "myResolverName"},
		{"simple", "simple"},
		{"a-b-c", "aBC"},
		{"already-camelCase", "alreadyCamelCase"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, hyphensToCamelCase(tt.input))
		})
	}
}

// ---- resolver-cycle ----

func TestLintResolverCycles_NoCycle(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("static", nil)))

	rslvrA := "resolverA"
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"resolverA": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{
						"value": {Literal: "hello"},
					}},
				},
			},
		},
		"resolverB": {
			DependsOn: []string{rslvrA},
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{
						"value": {Resolver: &rslvrA},
					}},
				},
			},
		},
	}

	result := &Result{}
	lintResolverCycles(sol, result, reg)
	for _, f := range result.Findings {
		assert.NotEqual(t, "resolver-cycle", f.RuleName, "no cycle should be detected")
	}
}

func TestLintResolverCycles_SimpleCycle(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("static", nil)))

	rslvrA := "resolverA"
	rslvrB := "resolverB"
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"resolverA": {
			DependsOn: []string{rslvrB},
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{
						"value": {Resolver: &rslvrB},
					}},
				},
			},
		},
		"resolverB": {
			DependsOn: []string{rslvrA},
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{
						"value": {Resolver: &rslvrA},
					}},
				},
			},
		},
	}

	result := &Result{}
	lintResolverCycles(sol, result, reg)

	var cycleFindings []*Finding
	for _, f := range result.Findings {
		if f.RuleName == "resolver-cycle" {
			cycleFindings = append(cycleFindings, f)
		}
	}

	require.Len(t, cycleFindings, 1)
	assert.Equal(t, SeverityError, cycleFindings[0].Severity)
	assert.Contains(t, cycleFindings[0].Message, "circular dependency")
}

func TestLintResolverCycles_GenericSuggestion(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("static", nil)))
	require.NoError(t, reg.Register(newFakeProviderWithCapabilities("validation", []provider.Capability{provider.CapabilityValidation})))

	rslvrA := "resolverA"
	rslvrB := "resolverB"
	expr := celexp.Expression("_.resolverA == true")
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"resolverA": {
			DependsOn: []string{rslvrB},
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{
						"value": {Resolver: &rslvrB},
					}},
				},
			},
		},
		"resolverB": {
			DependsOn: []string{rslvrA},
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{
						"value": {Literal: "default"},
					}},
				},
			},
			Validate: &resolver.ValidatePhase{
				With: []resolver.ProviderValidation{
					{
						Provider: "validation",
						Inputs: map[string]*spec.ValueRef{
							"expression": {Expr: &expr},
						},
					},
				},
			},
		},
	}

	result := &Result{}
	lintResolverCycles(sol, result, reg)

	var cycleFindings []*Finding
	for _, f := range result.Findings {
		if f.RuleName == "resolver-cycle" {
			cycleFindings = append(cycleFindings, f)
		}
	}

	require.Len(t, cycleFindings, 1)
	assert.Contains(t, cycleFindings[0].Suggestion, "Break the cycle",
		"a genuine resolve cycle should give the generic break-the-cycle suggestion")
	assert.NotContains(t, cycleFindings[0].Suggestion, "validate block",
		"validation references no longer form cycles, so the validate-specific suggestion is retired")
}

func TestLintDeferredValidation(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(newFakeProvider("static", nil)))
	require.NoError(t, reg.Register(newFakeProviderWithCapabilities("validation", []provider.Capability{provider.CapabilityValidation})))

	selfExpr := celexp.Expression("__self != ''")
	crossExpr := celexp.Expression("_.region != _.backupRegion")

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"backupRegion": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "us-west1"}}},
				},
			},
		},
		"selfOnly": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "x"}}},
				},
			},
			Validate: &resolver.ValidatePhase{
				With: []resolver.ProviderValidation{
					{Provider: "validation", Inputs: map[string]*spec.ValueRef{"expression": {Expr: &selfExpr}}},
				},
			},
		},
		"region": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "us-east1"}}},
				},
			},
			Validate: &resolver.ValidatePhase{
				With: []resolver.ProviderValidation{
					{Provider: "validation", Inputs: map[string]*spec.ValueRef{"expression": {Expr: &crossExpr}}},
				},
			},
		},
	}

	result := &Result{}
	lintDeferredValidation(sol, result, reg)

	var advisories []*Finding
	for _, f := range result.Findings {
		if f.RuleName == "deferred-validation-not-fail-fast" {
			advisories = append(advisories, f)
		}
	}

	// Only the cross-resolver rule ("region") is deferred; the self-only rule is not.
	require.Len(t, advisories, 1)
	assert.Equal(t, SeverityInfo, advisories[0].Severity)
	assert.Equal(t, "resolvers.region.validate", advisories[0].Location)
}

// ---- collectTemplateFileResolverRefs ----

func TestCollectTemplateFileResolverRefs(t *testing.T) {
	// Create a temp directory with a template file.
	tmpDir := t.TempDir()
	tplDir := filepath.Join(tmpDir, "templates")
	require.NoError(t, os.MkdirAll(tplDir, 0o755))

	tplContent := `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .appName }}
data:
  region: {{ .region }}
  cluster: {{ .clusterName }}
`
	require.NoError(t, os.WriteFile(filepath.Join(tplDir, "config.tpl"), []byte(tplContent), 0o644))

	sol := &solution.Solution{}
	sol.Bundle = solution.Bundle{
		Include: []string{"templates/**/*.tpl"},
	}

	refs := collectTemplateFileResolverRefs(sol, tmpDir)

	assert.True(t, refs["appName"], "should find appName reference")
	assert.True(t, refs["region"], "should find region reference")
	assert.True(t, refs["clusterName"], "should find clusterName reference")
}

func TestCollectTemplateFileResolverRefs_DirectoryProvider(t *testing.T) {
	// Create a temp directory with a template file.
	tmpDir := t.TempDir()
	tplDir := filepath.Join(tmpDir, "mytemplates")
	require.NoError(t, os.MkdirAll(tplDir, 0o755))

	tplContent := `Hello {{ .userName }}`
	require.NoError(t, os.WriteFile(filepath.Join(tplDir, "greeting.tpl"), []byte(tplContent), 0o644))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"templateSource": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "directory",
						Inputs: map[string]*spec.ValueRef{
							"path": {Literal: "mytemplates"},
						},
					},
				},
			},
		},
	}

	refs := collectTemplateFileResolverRefs(sol, tmpDir)
	assert.True(t, refs["userName"], "should find userName reference from directory provider path")
}

func TestCollectTemplateFileResolverRefs_NonTplExtensions(t *testing.T) {
	// Terraform templates (.tf/.tfvars) and Kubernetes manifests (.yaml) are
	// rendered through the go-template provider but do not use a .tpl extension.
	// Discovery is role-based, so their resolver references must still be found.
	tmpDir := t.TempDir()
	tplDir := filepath.Join(tmpDir, "terraform")
	require.NoError(t, os.MkdirAll(tplDir, 0o755))

	tfContent := `resource "azuread_application" "app" {
  display_name = "{{ .appName }}"
{{ if .spnInPlatformAppsGroup }}  owners = [data.azuread_service_principal.spn.object_id]{{ end }}
}`
	require.NoError(t, os.WriteFile(filepath.Join(tplDir, "app.tf"), []byte(tfContent), 0o644))

	tfvarsContent := `region = "{{ .region }}"`
	require.NoError(t, os.WriteFile(filepath.Join(tplDir, "vars.tfvars"), []byte(tfvarsContent), 0o644))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"templateSource": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "directory",
						Inputs: map[string]*spec.ValueRef{
							"path": {Literal: "terraform"},
						},
					},
				},
			},
		},
	}

	refs := collectTemplateFileResolverRefs(sol, tmpDir)
	assert.True(t, refs["appName"], "should find appName reference in a .tf template")
	assert.True(t, refs["spnInPlatformAppsGroup"], "should find resolver referenced only in a .tf template")
	assert.True(t, refs["region"], "should find region reference in a .tfvars template")
}

func TestCollectTemplateFileResolverRefs_BundleNonTplExtension(t *testing.T) {
	// A bundle.include'd .yaml template (no .tpl extension) must still be scanned.
	tmpDir := t.TempDir()
	tplDir := filepath.Join(tmpDir, "manifests")
	require.NoError(t, os.MkdirAll(tplDir, 0o755))

	content := "metadata:\n  name: {{ .appName }}\n"
	require.NoError(t, os.WriteFile(filepath.Join(tplDir, "deployment.yaml"), []byte(content), 0o644))

	sol := &solution.Solution{}
	sol.Bundle = solution.Bundle{
		Include: []string{"manifests/**/*.yaml"},
	}

	refs := collectTemplateFileResolverRefs(sol, tmpDir)
	assert.True(t, refs["appName"], "should find appName reference in a bundle-included .yaml template")
}

func TestCollectTemplateFileResolverRefs_SkipsNonTemplateFiles(t *testing.T) {
	// Binary/non-template files reached via a directory provider must be skipped
	// gracefully (no panic, no spurious references) rather than crashing the scan.
	tmpDir := t.TempDir()
	tplDir := filepath.Join(tmpDir, "assets")
	require.NoError(t, os.MkdirAll(tplDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(tplDir, "logo.png"), []byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0x01}, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tplDir, "valid.tpl"), []byte(`{{ .appName }}`), 0o644))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"templateSource": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "directory",
						Inputs: map[string]*spec.ValueRef{
							"path": {Literal: "assets"},
						},
					},
				},
			},
		},
	}

	refs := collectTemplateFileResolverRefs(sol, tmpDir)
	assert.True(t, refs["appName"], "should still find references in valid templates")
}

// ---- missing-template-dependency ----

func TestLintTemplateFileDependencies_MissingDep(t *testing.T) {
	tmpDir := t.TempDir()
	tplDir := filepath.Join(tmpDir, "templates")
	require.NoError(t, os.MkdirAll(tplDir, 0o755))

	tplContent := `name: {{ .appName }}
env: {{ .environment }}
`
	require.NoError(t, os.WriteFile(filepath.Join(tplDir, "config.tpl"), []byte(tplContent), 0o644))

	sourceResolver := "templateSource"
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"appName": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{
						"value": {Literal: "myapp"},
					}},
				},
			},
		},
		"environment": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{
						"value": {Literal: "prod"},
					}},
				},
			},
		},
		"templateSource": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "directory",
						Inputs: map[string]*spec.ValueRef{
							"path":           {Literal: "templates"},
							"operation":      {Literal: "list"},
							"includeContent": {Literal: true},
						},
					},
				},
			},
		},
		"rendered": {
			// Only depends on templateSource, missing appName and environment.
			DependsOn: []string{"templateSource"},
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "go-template",
						Inputs: map[string]*spec.ValueRef{
							"operation": {Literal: "render-tree"},
							"entries":   {Resolver: &sourceResolver},
						},
					},
				},
			},
		},
	}

	result := &Result{}
	lintTemplateFileDependencies(sol, tmpDir, result, nil)

	var depFindings []*Finding
	for _, f := range result.Findings {
		if f.RuleName == "missing-template-dependency" {
			depFindings = append(depFindings, f)
		}
	}

	assert.GreaterOrEqual(t, len(depFindings), 1, "should find at least one missing dependency")

	// Check that at least one of the missing refs is reported.
	messages := make([]string, 0, len(depFindings))
	for _, f := range depFindings {
		messages = append(messages, f.Message)
	}
	allMessages := ""
	for _, m := range messages {
		allMessages += m + " "
	}
	assert.True(t,
		(assert.ObjectsAreEqual(true, contains(allMessages, "appName")) ||
			assert.ObjectsAreEqual(true, contains(allMessages, "environment"))),
		"should mention appName or environment as missing dependency")
}

func TestLintTemplateFileDependencies_AllDepsPresent(t *testing.T) {
	tmpDir := t.TempDir()
	tplDir := filepath.Join(tmpDir, "templates")
	require.NoError(t, os.MkdirAll(tplDir, 0o755))

	tplContent := `name: {{ .appName }}`
	require.NoError(t, os.WriteFile(filepath.Join(tplDir, "config.tpl"), []byte(tplContent), 0o644))

	sourceResolver := "templateSource"
	appNameResolver := "appName"
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"appName": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{
						"value": {Literal: "myapp"},
					}},
				},
			},
		},
		"templateSource": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "directory",
						Inputs: map[string]*spec.ValueRef{
							"path": {Literal: "templates"},
						},
					},
				},
			},
		},
		"rendered": {
			DependsOn: []string{"templateSource", "appName"},
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "go-template",
						Inputs: map[string]*spec.ValueRef{
							"operation": {Literal: "render-tree"},
							"entries":   {Resolver: &sourceResolver},
							"data":      {Resolver: &appNameResolver},
						},
					},
				},
			},
		},
	}

	result := &Result{}
	lintTemplateFileDependencies(sol, tmpDir, result, nil)

	for _, f := range result.Findings {
		assert.NotEqual(t, "missing-template-dependency", f.RuleName,
			"should not report missing dependency when all deps are present")
	}
}

func TestLintTemplateFileDependencies_CustomDelimiters(t *testing.T) {
	tmpDir := t.TempDir()
	tplDir := filepath.Join(tmpDir, "templates")
	require.NoError(t, os.MkdirAll(tplDir, 0o755))

	// Template uses custom delimiters; default '{{' parsing would miss the ref.
	tplContent := `name: <% .appName %>`
	require.NoError(t, os.WriteFile(filepath.Join(tplDir, "config.tpl"), []byte(tplContent), 0o644))

	sourceResolver := "templateSource"
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"appName": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{
						"value": {Literal: "myapp"},
					}},
				},
			},
		},
		"templateSource": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "directory",
						Inputs: map[string]*spec.ValueRef{
							"path": {Literal: "templates"},
						},
					},
				},
			},
		},
		"rendered": {
			// Missing appName dependency.
			DependsOn: []string{"templateSource"},
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "go-template",
						Inputs: map[string]*spec.ValueRef{
							"operation":  {Literal: "render-tree"},
							"entries":    {Resolver: &sourceResolver},
							"leftDelim":  {Literal: "<%"},
							"rightDelim": {Literal: "%>"},
						},
					},
				},
			},
		},
	}

	result := &Result{}
	lintTemplateFileDependencies(sol, tmpDir, result, nil)

	var found bool
	for _, f := range result.Findings {
		if f.RuleName == "missing-template-dependency" && contains(f.Message, "appName") {
			found = true
		}
	}
	assert.True(t, found, "should detect appName ref parsed with custom delimiters")
}

func TestLintTemplateFileDependencies_LiteralDataKey(t *testing.T) {
	tmpDir := t.TempDir()
	tplDir := filepath.Join(tmpDir, "templates")
	require.NoError(t, os.MkdirAll(tplDir, 0o755))

	// Template references 'config', which is satisfied by a literal data map
	// key, not the same-named resolver.
	tplContent := `host: {{ .config }}`
	require.NoError(t, os.WriteFile(filepath.Join(tplDir, "config.tpl"), []byte(tplContent), 0o644))

	sourceResolver := "templateSource"
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"config": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "static", Inputs: map[string]*spec.ValueRef{
						"value": {Literal: "unrelated"},
					}},
				},
			},
		},
		"templateSource": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "directory",
						Inputs: map[string]*spec.ValueRef{
							"path": {Literal: "templates"},
						},
					},
				},
			},
		},
		"rendered": {
			// Does not depend on the 'config' resolver, but provides 'config'
			// as a literal data key, so no warning should be raised.
			DependsOn: []string{"templateSource"},
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "go-template",
						Inputs: map[string]*spec.ValueRef{
							"operation": {Literal: "render-tree"},
							"entries":   {Resolver: &sourceResolver},
							"data": {Literal: map[string]any{
								"config": "local-value",
							}},
						},
					},
				},
			},
		},
	}

	result := &Result{}
	lintTemplateFileDependencies(sol, tmpDir, result, nil)

	for _, f := range result.Findings {
		assert.NotEqual(t, "missing-template-dependency", f.RuleName,
			"should not warn for refs satisfied by a literal data key")
	}
}

// ---- findResolverCycles ----

func TestFindResolverCycles_NoCycles(t *testing.T) {
	deps := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {},
	}
	cycles := findResolverCycles(deps)
	assert.Empty(t, cycles)
}

func TestFindResolverCycles_SimpleCycle(t *testing.T) {
	deps := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}
	cycles := findResolverCycles(deps)
	assert.Len(t, cycles, 1)
}

func TestFindResolverCycles_SelfCycle(t *testing.T) {
	deps := map[string][]string{
		"a": {"a"},
	}
	cycles := findResolverCycles(deps)
	assert.Len(t, cycles, 1)
}

func TestFindResolverCycles_MultipleCycles(t *testing.T) {
	deps := map[string][]string{
		"a": {"b"},
		"b": {"a"},
		"c": {"d"},
		"d": {"c"},
	}
	cycles := findResolverCycles(deps)
	assert.Len(t, cycles, 2)
}

func TestFindResolverCycles_DistinctCyclesSameSmallestNode(t *testing.T) {
	// a->b->a and a->c->a share their smallest node "a" but are distinct cycles.
	// Both must be reported (previously de-duplicated to one).
	deps := map[string][]string{
		"a": {"b", "c"},
		"b": {"a"},
		"c": {"a"},
	}
	cycles := findResolverCycles(deps)
	assert.Len(t, cycles, 2)
}

func TestCanonicalCycleKey_RotationInvariant(t *testing.T) {
	// The same directed cycle discovered from different start nodes yields the
	// same key.
	assert.Equal(t,
		canonicalCycleKey([]string{"a", "b", "c", "a"}),
		canonicalCycleKey([]string{"b", "c", "a", "b"}),
	)
	assert.Equal(t,
		canonicalCycleKey([]string{"a", "b", "c", "a"}),
		canonicalCycleKey([]string{"c", "a", "b", "c"}),
	)
}

func TestCanonicalCycleKey_DistinctCycles(t *testing.T) {
	// Distinct cycles sharing the smallest node produce different keys.
	assert.NotEqual(t,
		canonicalCycleKey([]string{"a", "b", "a"}),
		canonicalCycleKey([]string{"a", "c", "a"}),
	)
}

func TestCanonicalCycleKey_Empty(t *testing.T) {
	assert.Empty(t, canonicalCycleKey(nil))
	assert.Empty(t, canonicalCycleKey([]string{}))
}

// ---- collectReachableDependencies ----

func TestCollectReachableDependencies_DependsOnTransitive(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"a": {Name: "a", DependsOn: []string{"b"}},
		"b": {Name: "b", DependsOn: []string{"c"}},
		"c": {Name: "c"},
	}
	reachable := collectReachableDependencies("a", sol, nil)
	assert.True(t, reachable["b"])
	assert.True(t, reachable["c"])
}

func TestCollectReachableDependencies_InferredFromExpr(t *testing.T) {
	// "main" references "dep" only via a CEL expression input. The dependency
	// must be discovered (previously missed, causing false-positive warnings).
	expr := celexp.Expression("_.dep")
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"main": {
			Name: "main",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{
					Provider: "cel",
					Inputs: map[string]*spec.ValueRef{
						"expression": {Expr: &expr},
					},
				}},
			},
		},
		"dep": {Name: "dep"},
	}
	reachable := collectReachableDependencies("main", sol, nil)
	assert.True(t, reachable["dep"], "dependency inferred from expr should be reachable")
}

func TestCollectReachableDependencies_InferredFromResolverRef(t *testing.T) {
	depName := "dep"
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"main": {
			Name: "main",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{
					Provider: "static",
					Inputs: map[string]*spec.ValueRef{
						"value": {Resolver: &depName},
					},
				}},
			},
		},
		"dep": {Name: "dep"},
	}
	reachable := collectReachableDependencies("main", sol, nil)
	assert.True(t, reachable["dep"])
}

// ---- findEntriesSourceResolver ----

func TestFindEntriesSourceResolver_ResolverRef(t *testing.T) {
	name := "entriesSource"
	step := resolver.ProviderSource{
		Inputs: map[string]*spec.ValueRef{
			"entries": {Resolver: &name},
		},
	}
	assert.Equal(t, "entriesSource", findEntriesSourceResolver(step, nil))
}

func TestFindEntriesSourceResolver_DotNotation(t *testing.T) {
	expr := celexp.Expression("_.myResolver.entries")
	step := resolver.ProviderSource{
		Inputs: map[string]*spec.ValueRef{
			"entries": {Expr: &expr},
		},
	}
	assert.Equal(t, "myResolver", findEntriesSourceResolver(step, nil))
}

func TestFindEntriesSourceResolver_BracketNotationDoubleQuote(t *testing.T) {
	expr := celexp.Expression(`_["my-resolver"].entries`)
	step := resolver.ProviderSource{
		Inputs: map[string]*spec.ValueRef{
			"entries": {Expr: &expr},
		},
	}
	assert.Equal(t, "my-resolver", findEntriesSourceResolver(step, nil))
}

func TestFindEntriesSourceResolver_BracketNotationSingleQuote(t *testing.T) {
	expr := celexp.Expression(`_['my-resolver'].entries`)
	step := resolver.ProviderSource{
		Inputs: map[string]*spec.ValueRef{
			"entries": {Expr: &expr},
		},
	}
	assert.Equal(t, "my-resolver", findEntriesSourceResolver(step, nil))
}

func TestFindEntriesSourceResolver_NoEntries(t *testing.T) {
	step := resolver.ProviderSource{Inputs: map[string]*spec.ValueRef{}}
	assert.Empty(t, findEntriesSourceResolver(step, nil))
}

// ---- discoverTemplateFilesFromResolver ----

func TestDiscoverTemplateFilesFromResolver_HonorsRecursive(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "top.tpl"), []byte("x"), 0o600))
	sub := filepath.Join(dir, "nested")
	require.NoError(t, os.MkdirAll(sub, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "deep.tpl"), []byte("y"), 0o600))

	recursiveFalse := &spec.ValueRef{Literal: false}
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"tree": {
			Name: "tree",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{
					Provider: "directory",
					Inputs: map[string]*spec.ValueRef{
						"path":      {Literal: dir},
						"recursive": recursiveFalse,
					},
				}},
			},
		},
	}

	files := discoverTemplateFilesFromResolver(sol, "tree", "")
	assert.Len(t, files, 1, "non-recursive walk should only return top-level files")
	assert.Equal(t, filepath.Join(dir, "top.tpl"), files[0])
}

func TestDiscoverTemplateFilesFromResolver_NonRecursiveByDefault(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "top.tpl"), []byte("x"), 0o600))
	sub := filepath.Join(dir, "nested")
	require.NoError(t, os.MkdirAll(sub, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "deep.tpl"), []byte("y"), 0o600))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"tree": {
			Name: "tree",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{
					Provider: "directory",
					Inputs: map[string]*spec.ValueRef{
						"path": {Literal: dir},
					},
				}},
			},
		},
	}

	files := discoverTemplateFilesFromResolver(sol, "tree", "")
	assert.Len(t, files, 1, "default walk should be non-recursive, matching the directory provider")
	assert.Equal(t, filepath.Join(dir, "top.tpl"), files[0])
}

func TestDiscoverTemplateFilesFromResolver_HonorsRecursiveTrue(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "top.tpl"), []byte("x"), 0o600))
	sub := filepath.Join(dir, "nested")
	require.NoError(t, os.MkdirAll(sub, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "deep.tpl"), []byte("y"), 0o600))

	recursiveTrue := &spec.ValueRef{Literal: true}
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"tree": {
			Name: "tree",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{
					Provider: "directory",
					Inputs: map[string]*spec.ValueRef{
						"path":      {Literal: dir},
						"recursive": recursiveTrue,
					},
				}},
			},
		},
	}

	files := discoverTemplateFilesFromResolver(sol, "tree", "")
	assert.Len(t, files, 2, "recursive: true should descend into subdirectories")
}

func TestDiscoverTemplateFilesFromResolver_HonorsFilterGlob(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "keep.tpl"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skip.tpl"), []byte("y"), 0o600))

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"tree": {
			Name: "tree",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{
					Provider: "directory",
					Inputs: map[string]*spec.ValueRef{
						"path":       {Literal: dir},
						"filterGlob": {Literal: "keep.*"},
					},
				}},
			},
		},
	}

	files := discoverTemplateFilesFromResolver(sol, "tree", "")
	assert.Len(t, files, 1)
	assert.Equal(t, filepath.Join(dir, "keep.tpl"), files[0])
}

// contains is a helper for substring matching in test assertions.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// newFakeProviderWithCapabilities creates a test provider with specific capabilities.
func newFakeProviderWithCapabilities(name string, caps []provider.Capability) *fakeProvider {
	outputSchemas := make(map[provider.Capability]*jsonschema.Schema)
	for _, cap := range caps {
		switch cap {
		case provider.CapabilityValidation:
			outputSchemas[cap] = &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"valid":  {Type: "boolean"},
					"errors": {Type: "array"},
				},
			}
		default:
			outputSchemas[cap] = &jsonschema.Schema{Type: "object"}
		}
	}
	return &fakeProvider{
		desc: &provider.Descriptor{
			Name:          name,
			APIVersion:    "v1",
			Version:       semver.MustParse("1.0.0"),
			Schema:        &jsonschema.Schema{Type: "object"},
			OutputSchemas: outputSchemas,
			Description:   "Test provider",
			Capabilities:  caps,
		},
	}
}
