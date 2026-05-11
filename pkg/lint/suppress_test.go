// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDirectives_IgnoreSingle(t *testing.T) {
	t.Parallel()

	yaml := []byte("# scafctl-lint-ignore: exec-command-injection\ncommand:\n  tmpl: 'echo hello'\n")
	ss := ParseDirectives(yaml, "solution.yaml")

	require.Len(t, ss.directives, 1)
	assert.Equal(t, DirectiveIgnore, ss.directives[0].Type)
	assert.Equal(t, []string{"exec-command-injection"}, ss.directives[0].Rules)
	assert.Equal(t, 1, ss.directives[0].Line)
	assert.Equal(t, "solution.yaml", ss.directives[0].File)
	assert.False(t, ss.directives[0].Inline)
}

func TestParseDirectives_IgnoreMultipleRules(t *testing.T) {
	t.Parallel()

	yaml := []byte("# scafctl-lint-ignore: rule-a, rule-b\nfoo: bar\n")
	ss := ParseDirectives(yaml, "test.yaml")

	require.Len(t, ss.directives, 1)
	assert.Equal(t, DirectiveIgnore, ss.directives[0].Type)
	assert.Equal(t, []string{"rule-a", "rule-b"}, ss.directives[0].Rules)
}

func TestParseDirectives_IgnoreAllRules(t *testing.T) {
	t.Parallel()

	yaml := []byte("# scafctl-lint-ignore\nfoo: bar\n")
	ss := ParseDirectives(yaml, "test.yaml")

	require.Len(t, ss.directives, 1)
	assert.Equal(t, DirectiveIgnore, ss.directives[0].Type)
	assert.Nil(t, ss.directives[0].Rules, "no rules means all rules")
}

func TestParseDirectives_DisableEnable(t *testing.T) {
	t.Parallel()

	yaml := []byte("# scafctl-lint-disable: exec-command-injection\nstep1: a\nstep2: b\n# scafctl-lint-enable: exec-command-injection\n")
	ss := ParseDirectives(yaml, "test.yaml")

	require.Len(t, ss.directives, 2)
	assert.Equal(t, DirectiveDisable, ss.directives[0].Type)
	assert.Equal(t, 1, ss.directives[0].Line)
	assert.Equal(t, DirectiveEnable, ss.directives[1].Type)
	assert.Equal(t, 4, ss.directives[1].Line)
}

func TestParseDirectives_DisableFile(t *testing.T) {
	t.Parallel()

	yaml := []byte("# scafctl-lint-disable-file: exec-command-injection\napiVersion: scafctl.io/v1\n")
	ss := ParseDirectives(yaml, "test.yaml")

	require.Len(t, ss.directives, 1)
	assert.Equal(t, DirectiveDisableFile, ss.directives[0].Type)
	assert.Equal(t, []string{"exec-command-injection"}, ss.directives[0].Rules)
}

func TestParseDirectives_NonDirectiveComments(t *testing.T) {
	t.Parallel()

	yaml := []byte("# This is a normal comment\n# Another comment\nfoo: bar\n")
	ss := ParseDirectives(yaml, "test.yaml")

	assert.Empty(t, ss.directives)
}

func TestParseDirectives_Empty(t *testing.T) {
	t.Parallel()

	ss := ParseDirectives(nil, "test.yaml")
	assert.Empty(t, ss.directives)
}

func TestParseDirectives_MultipleDirectives(t *testing.T) {
	t.Parallel()

	yaml := []byte("# scafctl-lint-disable-file: rule-a\n# scafctl-lint-ignore: rule-b\nfoo: bar\n# scafctl-lint-disable: rule-c\nbar: baz\n# scafctl-lint-enable: rule-c\n")
	ss := ParseDirectives(yaml, "test.yaml")

	require.Len(t, ss.directives, 4)
	assert.Equal(t, DirectiveDisableFile, ss.directives[0].Type)
	assert.Equal(t, DirectiveIgnore, ss.directives[1].Type)
	assert.Equal(t, DirectiveDisable, ss.directives[2].Type)
	assert.Equal(t, DirectiveEnable, ss.directives[3].Type)
}

func TestParseDirectives_BlockScalarIgnored(t *testing.T) {
	t.Parallel()

	yaml := []byte("script: |\n  # scafctl-lint-disable-file: rule-a\n  echo hello\nfoo: bar\n")
	ss := ParseDirectives(yaml, "test.yaml")

	assert.Empty(t, ss.directives, "directives inside block scalars must be ignored")
}

func TestParseDirectives_BlockScalarFolded(t *testing.T) {
	t.Parallel()

	yaml := []byte("script: >\n  # scafctl-lint-ignore: rule-a\n  echo hello\nfoo: bar\n")
	ss := ParseDirectives(yaml, "test.yaml")

	assert.Empty(t, ss.directives, "directives inside folded block scalars must be ignored")
}

func TestParseDirectives_BlockScalarChomping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		literal string
	}{
		{"literal strip", "script: |-\n  # scafctl-lint-ignore: rule-a\n  echo\nfoo: bar\n"},
		{"literal keep", "script: |+\n  # scafctl-lint-ignore: rule-a\n  echo\nfoo: bar\n"},
		{"folded strip", "script: >-\n  # scafctl-lint-ignore: rule-a\n  echo\nfoo: bar\n"},
		{"folded keep", "script: >+\n  # scafctl-lint-ignore: rule-a\n  echo\nfoo: bar\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ss := ParseDirectives([]byte(tt.literal), "test.yaml")
			assert.Empty(t, ss.directives, "directives inside block scalars with chomping must be ignored")
		})
	}
}

func TestParseDirectives_AfterBlockScalar(t *testing.T) {
	t.Parallel()

	yaml := []byte("script: |\n  echo hello\n# scafctl-lint-ignore: rule-a\nfoo: bar\n")
	ss := ParseDirectives(yaml, "test.yaml")

	require.Len(t, ss.directives, 1, "directive after block scalar should be parsed")
	assert.Equal(t, DirectiveIgnore, ss.directives[0].Type)
	assert.Equal(t, 3, ss.directives[0].Line)
}

func TestParseDirectives_QuotedHashNotInlineComment(t *testing.T) {
	t.Parallel()

	yaml := []byte("foo: 'literal # scafctl-lint-ignore: rule-a'\nbar: baz\n")
	ss := ParseDirectives(yaml, "test.yaml")

	assert.Empty(t, ss.directives, "# inside single-quoted scalar must not be parsed")
}

func TestParseDirectives_DoubleQuotedHashNotInlineComment(t *testing.T) {
	t.Parallel()

	yaml := []byte("foo: \"literal # scafctl-lint-ignore: rule-a\"\nbar: baz\n")
	ss := ParseDirectives(yaml, "test.yaml")

	assert.Empty(t, ss.directives, "# inside double-quoted scalar must not be parsed")
}

func TestParseDirectives_RejectsSpaceThenJunk(t *testing.T) {
	t.Parallel()

	yaml := []byte("# scafctl-lint-ignore TODO\n# scafctl-lint-disable something\nfoo: bar\n")
	ss := ParseDirectives(yaml, "test.yaml")

	assert.Empty(t, ss.directives, "space-then-junk after prefix must be rejected")
}

func TestFilter_IgnoreSuppressesNextLine(t *testing.T) {
	t.Parallel()

	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveIgnore, Rules: []string{"my-rule"}, Line: 5, File: "test.yaml", Inline: false},
		},
		used: make([]bool, 1),
	}

	findings := []*Finding{
		{RuleName: "my-rule", Line: 6, SourceFile: "test.yaml"},
		{RuleName: "other-rule", Line: 6, SourceFile: "test.yaml"},
		{RuleName: "my-rule", Line: 10, SourceFile: "test.yaml"},
	}

	result := ss.Filter(findings)

	require.Len(t, result, 2)
	assert.Equal(t, "other-rule", result[0].RuleName)
	assert.Equal(t, "my-rule", result[1].RuleName)
	assert.Equal(t, 10, result[1].Line)
	assert.True(t, ss.used[0])
}

func TestFilter_IgnoreSuppressesSameLine(t *testing.T) {
	t.Parallel()

	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveIgnore, Rules: []string{"my-rule"}, Line: 5, File: "test.yaml", Inline: false},
		},
		used: make([]bool, 1),
	}

	findings := []*Finding{
		{RuleName: "my-rule", Line: 5, SourceFile: "test.yaml"},
	}

	result := ss.Filter(findings)
	assert.Empty(t, result)
}

func TestFilter_InlineIgnoreOnlyCurrentLine(t *testing.T) {
	t.Parallel()

	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveIgnore, Rules: []string{"my-rule"}, Line: 5, File: "test.yaml", Inline: true},
		},
		used: make([]bool, 1),
	}

	findings := []*Finding{
		{RuleName: "my-rule", Line: 5, SourceFile: "test.yaml"},
		{RuleName: "my-rule", Line: 6, SourceFile: "test.yaml"},
	}

	result := ss.Filter(findings)

	require.Len(t, result, 1, "inline ignore must not suppress next line")
	assert.Equal(t, 6, result[0].Line)
}

func TestFilter_DisableEnableBlock(t *testing.T) {
	t.Parallel()

	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveDisable, Rules: []string{"my-rule"}, Line: 3, File: "test.yaml"},
			{Type: DirectiveEnable, Rules: []string{"my-rule"}, Line: 8, File: "test.yaml"},
		},
		used: make([]bool, 2),
	}

	findings := []*Finding{
		{RuleName: "my-rule", Line: 2, SourceFile: "test.yaml"},  // before block
		{RuleName: "my-rule", Line: 5, SourceFile: "test.yaml"},  // inside block
		{RuleName: "my-rule", Line: 10, SourceFile: "test.yaml"}, // after block
	}

	result := ss.Filter(findings)

	require.Len(t, result, 2)
	assert.Equal(t, 2, result[0].Line)
	assert.Equal(t, 10, result[1].Line)
}

func TestFilter_DisableEnableExactMatchRequired(t *testing.T) {
	t.Parallel()

	// disable: a,b should NOT be ended by enable: a (partial match).
	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveDisable, Rules: []string{"rule-a", "rule-b"}, Line: 3, File: "test.yaml"},
			{Type: DirectiveEnable, Rules: []string{"rule-a"}, Line: 8, File: "test.yaml"},
		},
		used: make([]bool, 2),
	}

	findings := []*Finding{
		{RuleName: "rule-b", Line: 10, SourceFile: "test.yaml"},
	}

	result := ss.Filter(findings)
	assert.Empty(t, result, "disable: a,b without matching enable should suppress to EOF")
}

func TestFilter_DisableWithoutEnable(t *testing.T) {
	t.Parallel()

	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveDisable, Rules: []string{"my-rule"}, Line: 3, File: "test.yaml"},
		},
		used: make([]bool, 1),
	}

	findings := []*Finding{
		{RuleName: "my-rule", Line: 5, SourceFile: "test.yaml"},
		{RuleName: "my-rule", Line: 100, SourceFile: "test.yaml"},
	}

	result := ss.Filter(findings)
	assert.Empty(t, result, "unmatched disable should suppress to EOF")
}

func TestFilter_DisableFile(t *testing.T) {
	t.Parallel()

	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveDisableFile, Rules: []string{"my-rule"}, Line: 1, File: "test.yaml"},
		},
		used: make([]bool, 1),
	}

	findings := []*Finding{
		{RuleName: "my-rule", Line: 50, SourceFile: "test.yaml"},
		{RuleName: "other-rule", Line: 50, SourceFile: "test.yaml"},
	}

	result := ss.Filter(findings)

	require.Len(t, result, 1)
	assert.Equal(t, "other-rule", result[0].RuleName)
}

func TestFilter_AllRules(t *testing.T) {
	t.Parallel()

	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveIgnore, Rules: nil, Line: 5, File: "test.yaml"},
		},
		used: make([]bool, 1),
	}

	findings := []*Finding{
		{RuleName: "any-rule", Line: 6, SourceFile: "test.yaml"},
		{RuleName: "another-rule", Line: 6, SourceFile: "test.yaml"},
	}

	result := ss.Filter(findings)
	assert.Empty(t, result, "nil rules should suppress all rules")
}

func TestFilter_NoDirectives(t *testing.T) {
	t.Parallel()

	ss := &SuppressionSet{
		directives: nil,
		used:       nil,
	}

	findings := []*Finding{
		{RuleName: "my-rule", Line: 1},
	}

	result := ss.Filter(findings)
	require.Len(t, result, 1)
	assert.Equal(t, "my-rule", result[0].RuleName)
}

func TestFilter_DifferentFile(t *testing.T) {
	t.Parallel()

	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveIgnore, Rules: []string{"my-rule"}, Line: 5, File: "a.yaml"},
		},
		used: make([]bool, 1),
	}

	findings := []*Finding{
		{RuleName: "my-rule", Line: 6, SourceFile: "b.yaml"},
	}

	result := ss.Filter(findings)
	require.Len(t, result, 1, "directive for a.yaml should not suppress b.yaml findings")
}

func TestUnusedFindings_UnusedDirective(t *testing.T) {
	t.Parallel()

	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveIgnore, Rules: []string{"nonexistent-rule"}, Line: 5, File: "test.yaml"},
		},
		used: []bool{false},
	}

	unused := ss.UnusedFindings()

	require.Len(t, unused, 1)
	assert.Equal(t, SeverityInfo, unused[0].Severity)
	assert.Equal(t, "unused-suppression", unused[0].RuleName)
	assert.Equal(t, 5, unused[0].Line)
	assert.Contains(t, unused[0].Message, "nonexistent-rule")
}

func TestUnusedFindings_UsedDirective(t *testing.T) {
	t.Parallel()

	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveIgnore, Rules: []string{"my-rule"}, Line: 5, File: "test.yaml"},
		},
		used: []bool{true},
	}

	unused := ss.UnusedFindings()
	assert.Empty(t, unused)
}

func TestUnusedFindings_SkipsUsedEnableDirectives(t *testing.T) {
	t.Parallel()

	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveDisable, Rules: []string{"my-rule"}, Line: 3, File: "test.yaml"},
			{Type: DirectiveEnable, Rules: []string{"my-rule"}, Line: 8, File: "test.yaml"},
		},
		used: []bool{false, true}, // enable is used (paired with disable)
	}

	unused := ss.UnusedFindings()

	// Only the disable directive should produce an unused finding.
	require.Len(t, unused, 1)
	assert.Equal(t, 3, unused[0].Line)
	assert.Contains(t, unused[0].Message, "did not match")
}

func TestUnusedFindings_UnmatchedEnable(t *testing.T) {
	t.Parallel()

	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveEnable, Rules: []string{"my-rule"}, Line: 10, File: "test.yaml"},
		},
		used: []bool{false},
	}

	unused := ss.UnusedFindings()

	require.Len(t, unused, 1)
	assert.Equal(t, "unused-suppression", unused[0].RuleName)
	assert.Equal(t, 10, unused[0].Line)
	assert.Contains(t, unused[0].Message, "no matching disable")
}

func TestParseRuleList_EmptyColon(t *testing.T) {
	t.Parallel()

	rules := parseRuleList(": ")
	assert.Nil(t, rules)
}

func TestParseRuleList_WhitespaceHandling(t *testing.T) {
	t.Parallel()

	rules := parseRuleList(":  rule-a ,  rule-b , rule-c ")
	assert.Equal(t, []string{"rule-a", "rule-b", "rule-c"}, rules)
}

func TestRulesMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		a, b  []string
		match bool
	}{
		{"both empty (all rules)", nil, nil, true},
		{"a empty b not", nil, []string{"x"}, false},
		{"a not empty b empty", []string{"x"}, nil, false},
		{"exact match", []string{"x", "y"}, []string{"y", "x"}, true},
		{"different length", []string{"x"}, []string{"x", "y"}, false},
		{"no match", []string{"x"}, []string{"y"}, false},
		{"same length different rules", []string{"a", "b"}, []string{"a", "c"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.match, rulesMatch(tt.a, tt.b))
		})
	}
}

func TestRulesOverlap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		a, b    []string
		overlap bool
	}{
		{"both empty (all rules)", nil, nil, true},
		{"a empty", nil, []string{"x"}, true},
		{"b empty", []string{"x"}, nil, true},
		{"matching", []string{"x", "y"}, []string{"y", "z"}, true},
		{"no overlap", []string{"x"}, []string{"y"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.overlap, rulesOverlap(tt.a, tt.b))
		})
	}
}

func TestFindInlineComment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want int
	}{
		{"simple inline", "foo: bar # comment", 9},
		{"no comment", "foo: bar", -1},
		{"single-quoted hash", "foo: 'bar # baz'", -1},
		{"double-quoted hash", `foo: "bar # baz"`, -1},
		{"hash after quote close", "foo: 'bar' # comment", 11},
		{"no space before hash", "foo: bar#comment", -1},
		{"escaped double quote", `foo: "bar \" # baz"`, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, findInlineComment(tt.line))
		})
	}
}

func TestKnownRules_UnusedSuppression(t *testing.T) {
	t.Parallel()

	rule, ok := GetRule("unused-suppression")
	require.True(t, ok, "unused-suppression rule should be in KnownRules")
	assert.Equal(t, "info", rule.Severity)
	assert.Equal(t, "suppression", rule.Category)
}

func TestParseDirectives_RejectsFalsePrefixMatch(t *testing.T) {
	t.Parallel()

	yaml := []byte("# scafctl-lint-disablefoo\n# scafctl-lint-ignorex: rule-a\n# scafctl-lint-enableXYZ\n")
	ss := ParseDirectives(yaml, "test.yaml")

	assert.Empty(t, ss.directives, "false prefix matches must not be parsed as directives")
}

func TestParseDirectives_InlineComment(t *testing.T) {
	t.Parallel()

	yaml := []byte("foo: bar  # scafctl-lint-ignore: my-rule\nbaz: qux\n")
	ss := ParseDirectives(yaml, "test.yaml")

	require.Len(t, ss.directives, 1)
	assert.Equal(t, DirectiveIgnore, ss.directives[0].Type)
	assert.Equal(t, []string{"my-rule"}, ss.directives[0].Rules)
	assert.Equal(t, 1, ss.directives[0].Line, "inline directive should be on the same line")
	assert.True(t, ss.directives[0].Inline)
}

func TestFilter_InlineIgnoreSuppressesSameLine(t *testing.T) {
	t.Parallel()

	// Inline directive on line 1 should suppress a finding on line 1 only.
	ss := &SuppressionSet{
		directives: []Directive{
			{Type: DirectiveIgnore, Rules: []string{"my-rule"}, Line: 1, File: "test.yaml", Inline: true},
		},
		used: make([]bool, 1),
	}

	findings := []*Finding{
		{RuleName: "my-rule", Line: 1, SourceFile: "test.yaml"},
	}

	result := ss.Filter(findings)
	assert.Empty(t, result, "inline ignore should suppress finding on same line")
}

func TestDirectiveMatchesFile_EmptyDirectiveFile(t *testing.T) {
	t.Parallel()

	d := &Directive{File: ""}
	f := &Finding{SourceFile: "something.yaml"}

	assert.True(t, directiveMatchesFile(d, f), "empty directive file should match any finding")
}

func TestDirectiveMatchesFile_BothEmpty(t *testing.T) {
	t.Parallel()

	d := &Directive{File: ""}
	f := &Finding{SourceFile: ""}

	assert.True(t, directiveMatchesFile(d, f), "both empty should match")
}

func TestParseDirectives_LongLine(t *testing.T) {
	t.Parallel()

	// Ensure lines up to scannerMaxLine are handled correctly.
	long := strings.Repeat("x", 100_000)
	yaml := []byte("key: " + long + "\n# scafctl-lint-ignore: rule-a\nfoo: bar\n")
	ss := ParseDirectives(yaml, "test.yaml")

	require.Len(t, ss.directives, 1, "directive after long line should be parsed")
	assert.Equal(t, DirectiveIgnore, ss.directives[0].Type)
}

func BenchmarkParseDirectives(b *testing.B) {
	yaml := []byte("# scafctl-lint-disable-file: rule-a\napiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: test\n# scafctl-lint-ignore: rule-b\nspec:\n  resolvers:\n    greeting:\n      resolve:\n        with:\n          - provider: static\n            inputs:\n              value: hello\n# scafctl-lint-disable: rule-c\n  workflow:\n    actions:\n      step1:\n        provider: exec\n# scafctl-lint-enable: rule-c\n")

	b.ResetTimer()
	for b.Loop() {
		ParseDirectives(yaml, "bench.yaml")
	}
}

func BenchmarkFilter(b *testing.B) {
	findings := make([]*Finding, 20)
	for i := range findings {
		findings[i] = &Finding{
			RuleName:   "my-rule",
			Line:       i*5 + 1,
			SourceFile: "test.yaml",
		}
	}

	b.ResetTimer()
	for b.Loop() {
		ss := &SuppressionSet{
			directives: []Directive{
				{Type: DirectiveIgnore, Rules: []string{"my-rule"}, Line: 5, File: "test.yaml"},
				{Type: DirectiveDisable, Rules: []string{"my-rule"}, Line: 20, File: "test.yaml"},
				{Type: DirectiveEnable, Rules: []string{"my-rule"}, Line: 40, File: "test.yaml"},
				{Type: DirectiveDisableFile, Rules: []string{"other-rule"}, Line: 1, File: "test.yaml"},
			},
			used: make([]bool, 4),
		}
		ss.Filter(findings)
	}
}
