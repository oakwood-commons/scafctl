// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refindex

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callFixtureYAML references call "fetch" from a resolve step, a transform step,
// a validate step, and a workflow action -- the four sites where a call: appears.
const callFixtureYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: refindex-call-test
spec:
  calls:
    fetch:
      provider: message
      inputs:
        message: fetching
  resolvers:
    r1:
      resolve:
        with:
          - call: fetch
      transform:
        with:
          - call: fetch
      validate:
        with:
          - call: fetch
  workflow:
    actions:
      a1:
        call: fetch
`

func TestBuild_CallNamesAndDefinitions(t *testing.T) {
	idx, raw, li := buildFixture(t, callFixtureYAML)

	assert.Equal(t, []string{"fetch"}, idx.Names(SymbolCall))
	assert.Zero(t, idx.Unresolved(), "clean call fixture should have no unresolved refs")

	def, ok := idx.Definition(SymbolCall, "fetch")
	require.True(t, ok)
	assert.True(t, def.IsDef)
	assert.Equal(t, SymbolCall, def.Symbol.Kind)
	assert.Equal(t, OriginDefinition, def.Origin)
	assertByteExact(t, raw, li, def)
}

func TestBuild_CallReferencesAllSites(t *testing.T) {
	idx, raw, li := buildFixture(t, callFixtureYAML)

	refs := idx.References(SymbolCall, "fetch")
	// resolve.with + transform.with + validate.with + action = 4 call: refs.
	assert.Len(t, refs, 4)
	for _, r := range refs {
		assert.Equal(t, OriginCallRef, r.Origin)
		assert.False(t, r.IsDef)
		assertByteExact(t, raw, li, r)
	}
	// definition + 4 uses.
	assert.Len(t, idx.Occurrences(SymbolCall, "fetch"), 5)
}

func TestBuild_CallKindIsolation(t *testing.T) {
	// A resolver and a call sharing the name "fetch" must not collide.
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: call-iso
spec:
  calls:
    fetch:
      provider: message
      inputs:
        message: hi
  resolvers:
    fetch:
      resolve:
        with:
          - call: fetch
`
	idx := mustBuild(t, y)
	// The call "fetch" has a definition + 1 call: ref.
	assert.Len(t, idx.Occurrences(SymbolCall, "fetch"), 2)
	// The resolver "fetch" has only its definition (the call: is a call ref, not
	// a resolver ref).
	assert.Len(t, idx.Occurrences(SymbolResolver, "fetch"), 1)
	assert.Zero(t, idx.Unresolved())
}

// functionFixtureYAML invokes author functions "greet" and "loud" from a
// resolver template, an action template, and inside another function's body.
// Explicit resolver refs (._.env) keep the fixture free of conservatively
// unresolvable bare fields; only author functions are invoked (no sprig helpers,
// which are registered via import side-effects the test binary lacks).
const functionFixtureYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: refindex-func-test
spec:
  functions:
    greet:
      params:
        - name: who
      template: "hello {{ .args.who }}"
    loud:
      params:
        - name: msg
      template: "{{ greet .args.msg }}!"
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    r1:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "{{ greet ._.env }} and {{ loud ._.env }}"
  workflow:
    actions:
      a1:
        provider: message
        inputs:
          message:
            tmpl: "{{ greet ._.env }}"
`

func TestBuild_FunctionNamesAndDefinitions(t *testing.T) {
	idx, raw, li := buildFixture(t, functionFixtureYAML)

	assert.Equal(t, []string{"greet", "loud"}, idx.Names(SymbolFunction))
	assert.Zero(t, idx.Unresolved(), "clean function fixture should have no unresolved refs")

	for _, name := range idx.Names(SymbolFunction) {
		def, ok := idx.Definition(SymbolFunction, name)
		require.Truef(t, ok, "definition for function %q", name)
		assert.True(t, def.IsDef)
		assert.Equal(t, SymbolFunction, def.Symbol.Kind)
		assertByteExact(t, raw, li, def)
	}
}

func TestBuild_FunctionReferencesAcrossTemplatesAndBodies(t *testing.T) {
	idx, raw, li := buildFixture(t, functionFixtureYAML)

	// "greet" is invoked in: loud's body, r1's template, and a1's template = 3.
	greetRefs := idx.References(SymbolFunction, "greet")
	assert.Len(t, greetRefs, 3)
	for _, r := range greetRefs {
		assert.Equal(t, OriginFunctionCall, r.Origin)
		assertByteExact(t, raw, li, r)
	}

	// "loud" is invoked once (r1's template).
	loudRefs := idx.References(SymbolFunction, "loud")
	assert.Len(t, loudRefs, 1)
	assertByteExact(t, raw, li, loudRefs[0])
}

func TestBuild_FunctionBuiltinsAreNotReferences(t *testing.T) {
	// A template invokes the built-in printf and the author function greet;
	// only greet is a renameable symbol.
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: func-builtin
spec:
  functions:
    greet:
      params:
        - name: who
      template: "hi {{ .args.who }}"
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    msg:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: '{{ printf "%s" (greet ._.env) }}'
`
	idx := mustBuild(t, y)
	assert.Equal(t, []string{"greet"}, idx.Names(SymbolFunction))
	assert.NotContains(t, idx.Names(SymbolFunction), "printf")
	_, ok := idx.Definition(SymbolFunction, "printf")
	assert.False(t, ok)
	// greet: definition + 1 invocation (nested in the printf call).
	assert.Len(t, idx.Occurrences(SymbolFunction, "greet"), 2)
	assert.Zero(t, idx.Unresolved())
}

// TestBuild_UnpositionableFunctionRefIsUnresolved documents the byte-exact
// fail-safe: a function invocation whose position cannot be located (here,
// skewed by preceding escaped quotes in a double-quoted YAML scalar) is recorded
// as unresolved for that function, so a rename of it refuses rather than
// corrupting the file.
func TestBuild_UnpositionableFunctionRefIsUnresolved(t *testing.T) {
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: func-escape
spec:
  functions:
    greet:
      params:
        - name: who
      template: "hi {{ .args.who }}"
  resolvers:
    msg:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "say \"x\" {{ greet .msg }}"
`
	idx := mustBuild(t, y)
	assert.Positive(t, idx.UnresolvedFor(SymbolFunction, "greet"),
		"an unpositionable function invocation must count against greet (fail-safe)")
}

// TestBuild_FunctionInTemplateDoesNotBreakResolverRefs is a regression test:
// before author-function names were threaded into the template extractors, a
// template invoking an author function failed to parse and marked every
// reference unresolved. Now resolver/action refs in such templates resolve.
func TestBuild_FunctionInTemplateDoesNotBreakResolverRefs(t *testing.T) {
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: func-and-resolver
spec:
  functions:
    greet:
      params:
        - name: who
      template: "hi {{ .args.who }}"
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    msg:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "{{ greet ._.env }}"
`
	idx := mustBuild(t, y)
	assert.Zero(t, idx.Unresolved(), "author function must not poison resolver ref extraction")
	// The explicit resolver ref ._.env inside the function-invoking template is
	// located, so env has definition + 1 template use.
	assert.Len(t, idx.Occurrences(SymbolResolver, "env"), 2)
	// greet: definition + 1 invocation.
	assert.Len(t, idx.Occurrences(SymbolFunction, "greet"), 2)
}
