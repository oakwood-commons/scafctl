// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refindex

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// actionFixtureYAML references action "build" via dependsOn, CEL
// (__actions.build) and template (.__actions.build). "make build" is unrelated
// literal text. "deploy" carries an alias that must NOT be treated as a
// reference to the action it names. A finally action references a regular one.
const actionFixtureYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: refindex-action-test
spec:
  resolvers: {}
  workflow:
    actions:
      build:
        alias: b
        provider: shell
        inputs:
          command: make build
      deploy:
        dependsOn:
          - build
        provider: shell
        when:
          expr: __actions.build.results.exitCode == 0
        inputs:
          command:
            tmpl: 'deploy {{ .__actions.build.results.stdout }}'
    finally:
      cleanup:
        provider: shell
        inputs:
          note:
            expr: __actions.deploy.results.exitCode
`

func TestBuild_ActionNamesAndDefinitions(t *testing.T) {
	idx, raw, li := buildFixture(t, actionFixtureYAML)

	assert.Equal(t, []string{"build", "cleanup", "deploy"}, idx.Names(SymbolAction))
	assert.Zero(t, idx.Unresolved(), "clean action fixture should have no unresolved refs")

	for _, name := range idx.Names(SymbolAction) {
		def, ok := idx.Definition(SymbolAction, name)
		require.Truef(t, ok, "definition for action %q", name)
		assert.True(t, def.IsDef)
		assert.Equal(t, OriginDefinition, def.Origin)
		assert.Equal(t, SymbolAction, def.Symbol.Kind)
		assertByteExact(t, raw, li, def)
	}
}

func TestBuild_ActionReferencesByOrigin(t *testing.T) {
	idx, raw, li := buildFixture(t, actionFixtureYAML)

	// "build" is referenced three ways: dependsOn, CEL (when expr), template.
	buildRefs := idx.References(SymbolAction, "build")
	origins := map[Origin]int{}
	for _, r := range buildRefs {
		origins[r.Origin]++
		assert.False(t, r.IsDef)
		assert.Equal(t, SymbolAction, r.Symbol.Kind)
		assertByteExact(t, raw, li, r)
	}
	assert.Equal(t, 1, origins[OriginDependsOn], "dependsOn ref")
	assert.Equal(t, 1, origins[OriginCEL], "when expr CEL ref")
	assert.Equal(t, 1, origins[OriginTemplate], "template ref")
	assert.Len(t, buildRefs, 3)

	// "deploy" is referenced once, from the finally action's CEL expression.
	deployRefs := idx.References(SymbolAction, "deploy")
	assert.Len(t, deployRefs, 1)
	assert.Equal(t, OriginCEL, deployRefs[0].Origin)
	assertByteExact(t, raw, li, deployRefs[0])
}

func TestBuild_ActionAliasIsNotAReference(t *testing.T) {
	idx := mustBuild(t, actionFixtureYAML)

	// The alias "b" on action "build" is an independent name, not a reference to
	// any action -- renaming an action must not touch it.
	assert.Empty(t, idx.References(SymbolAction, "b"), "alias must not be a reference")
	_, defined := idx.Definition(SymbolAction, "b")
	assert.False(t, defined, "alias must not be an action definition")
	assert.NotContains(t, idx.Names(SymbolAction), "b")
}

func TestBuild_ActionOccurrencesIncludeDefinition(t *testing.T) {
	idx, raw, li := buildFixture(t, actionFixtureYAML)

	occ := idx.Occurrences(SymbolAction, "build")
	// 1 definition + 3 uses.
	assert.Len(t, occ, 4)

	defCount := 0
	for _, r := range occ {
		if r.IsDef {
			defCount++
		}
		assertByteExact(t, raw, li, r)
	}
	assert.Equal(t, 1, defCount)
	assert.True(t, occ[0].IsDef, "first occurrence is the definition")
}

func TestBuild_ActionAndResolverKindsAreIsolated(t *testing.T) {
	idx := mustBuild(t, actionFixtureYAML)

	// This fixture defines actions but no resolvers -- action names must not
	// leak into the resolver kind and vice versa.
	assert.Empty(t, idx.Names(SymbolResolver))
	assert.Empty(t, idx.References(SymbolResolver, "build"))
	_, defined := idx.Definition(SymbolResolver, "build")
	assert.False(t, defined)
}

// mustBuild builds an index from YAML, failing the test on error.
func mustBuild(t *testing.T, y string) *Index {
	t.Helper()
	idx, _, _ := buildFixture(t, y)
	return idx
}

// TestMarkCELNamesUnresolved_ActionFailSafe white-box tests the defensive branch
// taken when a CEL scalar cannot be positioned at all: every action name in the
// expression must be attributed to the unresolved set so the rename fails safe.
func TestMarkCELNamesUnresolved_ActionFailSafe(t *testing.T) {
	b := &builder{idx: &Index{byKey: map[symbolKey][]Reference{}, unresolvedByKey: map[symbolKey]int{}}}
	expr := celexp.Expression(`__actions.build.results + __actions.deploy.status`)
	b.markCELNamesUnresolved(&expr, "__actions.", SymbolAction)

	assert.Positive(t, b.idx.UnresolvedFor(SymbolAction, "build"))
	assert.Positive(t, b.idx.UnresolvedFor(SymbolAction, "deploy"))
}

func TestBuild_UnpositionableActionCELRefIsUnresolved(t *testing.T) {
	// An action CEL reference nested inside a literal array has no positionable
	// scalar node, so it must be recorded as unresolved (attributed to that
	// action name) rather than silently dropped -- exercising the
	// markCELNamesUnresolved fail-safe for the action kind. This blocks a rename
	// of that action, which is the safe behavior.
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: unpositionable-action
spec:
  resolvers: {}
  workflow:
    actions:
      build:
        provider: message
        inputs:
          message: building
      deploy:
        provider: message
        inputs:
          value:
            items:
              - expr: __actions.build.results
`
	idx := mustBuild(t, y)
	assert.Positive(t, idx.Unresolved())
	assert.Positive(t, idx.UnresolvedFor(SymbolAction, "build"),
		"a nested (unpositionable) action CEL ref must count against that action")
	// Name-scoped: an unrelated action is not blocked.
	assert.Zero(t, idx.UnresolvedFor(SymbolAction, "deploy"))
}
