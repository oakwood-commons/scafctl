// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refindex

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fixtureYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: refindex-test
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    appName:
      dependsOn:
        - environment
      when:
        expr: _.environment == "dev"
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: _.environment
    greeting:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                tmpl: "{{ ._.appName }}/{{ ._.environment }}"
    aliasRef:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                rslvr: appName
`

func buildFixture(t *testing.T, y string) (*Index, []byte, *sourcepos.LineIndex) {
	t.Helper()
	sol := &solution.Solution{}
	require.NoError(t, sol.UnmarshalFromBytes([]byte(y)))
	idx, err := Build(sol)
	require.NoError(t, err)
	raw := sol.RawContent()
	return idx, raw, sourcepos.NewLineIndex(raw, "")
}

// assertByteExact verifies the reference's Range slices out exactly its name.
func assertByteExact(t *testing.T, raw []byte, li *sourcepos.LineIndex, r Reference) {
	t.Helper()
	start := li.Offset(r.Range.Start)
	end := li.Offset(r.Range.End)
	require.GreaterOrEqual(t, start, 0)
	require.LessOrEqual(t, end, len(raw))
	assert.Equalf(t, r.Symbol.Name, string(raw[start:end]),
		"range %s (origin %s) must slice to %q", r.Range, r.Origin, r.Symbol.Name)
}

func TestBuild_NamesAndDefinitions(t *testing.T) {
	idx, raw, li := buildFixture(t, fixtureYAML)

	assert.Equal(t, []string{"aliasRef", "appName", "environment", "greeting"}, idx.Names())
	assert.Zero(t, idx.Unresolved(), "clean fixture should have no unresolved refs")

	for _, name := range idx.Names() {
		def, ok := idx.Definition(name)
		require.Truef(t, ok, "definition for %q", name)
		assert.True(t, def.IsDef)
		assert.Equal(t, OriginDefinition, def.Origin)
		assert.Equal(t, SymbolResolver, def.Symbol.Kind)
		assertByteExact(t, raw, li, def)
	}
}

func TestBuild_ReferencesByOrigin(t *testing.T) {
	idx, raw, li := buildFixture(t, fixtureYAML)

	// "environment" is referenced four ways: dependsOn, CEL (when), CEL (expr),
	// and template.
	envRefs := idx.References("environment")
	origins := map[Origin]int{}
	for _, r := range envRefs {
		origins[r.Origin]++
		assert.False(t, r.IsDef)
		assertByteExact(t, raw, li, r)
	}
	assert.Equal(t, 1, origins[OriginDependsOn], "dependsOn ref")
	assert.Equal(t, 2, origins[OriginCEL], "when + expr CEL refs")
	assert.Equal(t, 1, origins[OriginTemplate], "template ref")
	assert.Len(t, envRefs, 4)

	// "appName" is referenced by a template (._.appName) and a rslvr.
	appRefs := idx.References("appName")
	appOrigins := map[Origin]int{}
	for _, r := range appRefs {
		appOrigins[r.Origin]++
		assertByteExact(t, raw, li, r)
	}
	assert.Equal(t, 1, appOrigins[OriginTemplate])
	assert.Equal(t, 1, appOrigins[OriginResolverRef])
	assert.Len(t, appRefs, 2)
}

func TestBuild_Occurrences_IncludesDefinition(t *testing.T) {
	idx, raw, li := buildFixture(t, fixtureYAML)

	occ := idx.Occurrences("environment")
	// 1 definition + 4 uses.
	assert.Len(t, occ, 5)

	defCount := 0
	for _, r := range occ {
		if r.IsDef {
			defCount++
		}
		assertByteExact(t, raw, li, r)
	}
	assert.Equal(t, 1, defCount)

	// Occurrences are sorted by position (definition comes first in the file).
	assert.True(t, occ[0].IsDef, "first occurrence is the definition")
}

func TestBuild_AllSortedAndByteExact(t *testing.T) {
	idx, raw, li := buildFixture(t, fixtureYAML)

	all := idx.All()
	require.NotEmpty(t, all)

	prevLine, prevCol := 0, 0
	for _, r := range all {
		// Non-decreasing position order.
		if r.Range.Start.Line == prevLine {
			assert.GreaterOrEqual(t, r.Range.Start.Column, prevCol)
		} else {
			assert.Greater(t, r.Range.Start.Line, prevLine)
		}
		prevLine, prevCol = r.Range.Start.Line, r.Range.Start.Column
		assertByteExact(t, raw, li, r)
	}
}

func TestBuild_ConditionScalarShorthand(t *testing.T) {
	// A condition in scalar shorthand form (when: <expr>) rather than {expr: ...}.
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: shorthand
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    gated:
      when: _.environment == "prod"
      resolve:
        with:
          - provider: parameter
            inputs:
              value: yes
`
	idx, raw, li := buildFixture(t, y)
	refs := idx.References("environment")
	require.Len(t, refs, 1)
	assert.Equal(t, OriginCEL, refs[0].Origin)
	assertByteExact(t, raw, li, refs[0])
}

func TestBuild_SingleQuotedExpr(t *testing.T) {
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: single-quoted
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    user:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: '_.environment'
`
	idx, raw, li := buildFixture(t, y)
	refs := idx.References("environment")
	require.Len(t, refs, 1)
	assert.Equal(t, OriginCEL, refs[0].Origin)
	assertByteExact(t, raw, li, refs[0])
	assert.Zero(t, idx.Unresolved())
}

func TestBuild_BlockTemplateAndBareField(t *testing.T) {
	// A block-literal template: line 1 resolves; the bare-field ref counts as
	// unresolved (context-dependent, not yet positioned); the line-2 explicit
	// ref lands past the block's stripped indentation and is dropped by the
	// byte-exact gate (documented block limitation) -- also counted unresolved.
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: block
spec:
  resolvers:
    appName:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: app
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    banner:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                tmpl: |
                  {{ ._.appName }}
                  {{ ._.environment }}
    other:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                tmpl: "{{ .someDataField }}"
`
	idx, raw, li := buildFixture(t, y)

	// Line-1 explicit ref in the block resolves byte-exact.
	appRefs := idx.References("appName")
	require.Len(t, appRefs, 1)
	assert.Equal(t, OriginTemplate, appRefs[0].Origin)
	assertByteExact(t, raw, li, appRefs[0])

	// The bare {{ .someDataField }} and the line-2 block ref are unresolved.
	assert.GreaterOrEqual(t, idx.Unresolved(), 1, "bare field / block line-2 unresolved")
}

func TestBuild_Nil(t *testing.T) {
	idx, err := Build(nil)
	require.NoError(t, err)
	assert.Empty(t, idx.All())
	assert.Empty(t, idx.Names())
	_, ok := idx.Definition("x")
	assert.False(t, ok)
}

func TestBuild_EmptyReferencesForUnknown(t *testing.T) {
	idx, _, _ := buildFixture(t, fixtureYAML)
	assert.Empty(t, idx.References("does-not-exist"))
	assert.Empty(t, idx.Occurrences("does-not-exist"))
}

func TestBuild_MalformedExpressionsAreSkipped(t *testing.T) {
	// Malformed CEL and template content parse as strings but fail reference
	// extraction; Build must skip them without error or panic.
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: malformed
spec:
  resolvers:
    broken:
      resolve:
        with:
          - provider: parameter
            inputs:
              bad_expr:
                expr: "_.a +"
              bad_tmpl:
                tmpl: "{{ .x "
`
	sol := &solution.Solution{}
	require.NoError(t, sol.UnmarshalFromBytes([]byte(y)))
	idx, err := Build(sol)
	require.NoError(t, err)
	assert.Empty(t, idx.References("a"))
	assert.Empty(t, idx.References("x"))
	_, ok := idx.Definition("broken")
	assert.True(t, ok)
}

func TestSymbolKindString(t *testing.T) {
	assert.Equal(t, "resolver", SymbolResolver.String())
	assert.Equal(t, "unknown", SymbolKind(99).String())
}

func TestOriginString(t *testing.T) {
	cases := map[Origin]string{
		OriginDefinition:  "definition",
		OriginDependsOn:   "dependsOn",
		OriginResolverRef: "rslvr",
		OriginCEL:         "cel",
		OriginTemplate:    "template",
		Origin(99):        "unknown",
	}
	for o, want := range cases {
		assert.Equal(t, want, o.String())
	}
}

func BenchmarkBuild(b *testing.B) {
	sol := &solution.Solution{}
	if err := sol.UnmarshalFromBytes([]byte(fixtureYAML)); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = Build(sol)
	}
}

func TestBuild_NestedLiteralAndDollarRefsAreUnresolvedByName(t *testing.T) {
	// C1: nested inline {rslvr}/{expr} inside a literal map/array, and
	// C2: a $-rooted template reference -- all reference "environment" but are
	// not positioned, so they must be attributed to "environment" as unresolved
	// (blocking a rename of environment) while leaving other names unaffected.
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fail-safe
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    other:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: x
    nestedRslvr:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                settings:
                  env:
                    rslvr: environment
    nestedExpr:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                items:
                  - expr: _.environment
    dollarTmpl:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                tmpl: "{{ $.environment }}"
`
	idx, _, _ := buildFixture(t, y)

	// Every reference above targets "environment" and is unpositioned.
	assert.GreaterOrEqual(t, idx.UnresolvedFor("environment"), 3,
		"nested rslvr + nested expr + $-template must all count against environment")

	// The fail-safe is name-scoped: renaming an unrelated resolver is not blocked.
	assert.Zero(t, idx.UnresolvedFor("other"))
	assert.Zero(t, idx.UnresolvedFor("nestedRslvr"))
}

func TestBuild_CleanFixtureHasNoPerNameUnresolved(t *testing.T) {
	idx, _, _ := buildFixture(t, fixtureYAML)
	for _, name := range idx.Names() {
		assert.Zerof(t, idx.UnresolvedFor(name), "resolver %q should have no unresolved refs", name)
	}
}

func TestBuild_NestedLiteralTemplateRefIsUnresolved(t *testing.T) {
	// A nested {tmpl: ...} inside a literal list references environment via an
	// explicit ._.name; it must be attributed to environment as unresolved.
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: nested-tmpl
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    banner:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                items:
                  - tmpl: "{{ ._.environment }}"
`
	idx, _, _ := buildFixture(t, y)
	assert.GreaterOrEqual(t, idx.UnresolvedFor("environment"), 1)
}

func TestBuild_MalformedNestedExprBlocksAllRenames(t *testing.T) {
	// A malformed nested expression cannot be parsed, so its target name is
	// unknown; it conservatively blocks every rename (unresolvedOther).
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: malformed-nested
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                wrapper:
                  expr: "_.a +"
`
	idx, _, _ := buildFixture(t, y)
	assert.Positive(t, idx.Unresolved())
	// unresolvedOther is included for every name.
	assert.Positive(t, idx.UnresolvedFor("environment"))
	assert.Positive(t, idx.UnresolvedFor("anything-at-all"))
}
