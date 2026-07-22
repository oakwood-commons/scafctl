// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package contextvars

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestList_ReturnsAllSortedAndWellFormed(t *testing.T) {
	all := List()
	require.NotEmpty(t, all)
	assert.Equal(t, len(builtinVariables), len(all))

	for _, v := range all {
		assert.NotEmpty(t, v.Name, "name must not be empty")
		assert.NotEmpty(t, v.Languages, "languages must not be empty: %s", v.Name)
		for _, lang := range v.Languages {
			assert.Contains(t, []string{LangCEL, LangTemplate}, lang, "language must be cel or go-template: %s", v.Name)
		}
		assert.NotEmpty(t, v.Phases, "phases must not be empty: %s", v.Name)
		assert.NotEmpty(t, v.Description, "description must not be empty: %s", v.Name)
	}

	// Sorted by primary language then name.
	for i := 1; i < len(all); i++ {
		prev, cur := all[i-1], all[i]
		if primaryLang(prev) == primaryLang(cur) {
			assert.LessOrEqual(t, prev.Name, cur.Name)
		} else {
			assert.Less(t, primaryLang(prev), primaryLang(cur))
		}
	}
}

func TestGet(t *testing.T) {
	v, ok := Get("__self")
	require.True(t, ok)
	assert.Equal(t, celexp.VarSelf, v.Name)
	// __self is available in both CEL and Go templates.
	assert.ElementsMatch(t, []string{LangCEL, LangTemplate}, v.Languages)
	assert.ElementsMatch(t, []string{PhaseTransform, PhaseValidate, PhaseForEach}, v.Phases)

	_, ok = Get("__nonexistent")
	assert.False(t, ok)
}

// TestCanonicalNamesTrackEngine guards against drift: the registry names must
// match the celexp.Var* constants the engine actually injects.
func TestCanonicalNamesTrackEngine(t *testing.T) {
	expected := map[string]struct{}{
		celexp.VarSelf:      {},
		celexp.VarItem:      {},
		celexp.VarIndex:     {},
		celexp.VarPlan:      {},
		celexp.VarExecution: {},
		celexp.VarActions:   {},
		celexp.VarCwd:       {},
		celexp.VarParams:    {},
		celexp.VarError:     {},
	}
	for name := range expected {
		_, ok := Get(name)
		assert.True(t, ok, "engine variable %q must be registered", name)
	}
}

// TestCorrectedScoping locks in the accuracy fixes validated against the engine:
// __cwd is action-only, __params is state-backend-only, __error is error-only.
func TestCorrectedScoping(t *testing.T) {
	cwd, ok := Get(celexp.VarCwd)
	require.True(t, ok)
	assert.Equal(t, []string{PhaseAction}, cwd.Phases, "__cwd must be action-only")
	assert.NotContains(t, cwd.Phases, PhaseResolve, "__cwd must NOT be available in resolvers")

	params, ok := Get(celexp.VarParams)
	require.True(t, ok)
	assert.Equal(t, []string{PhaseStateBackend}, params.Phases, "__params must be state-backend-only")
	assert.Equal(t, []string{LangCEL}, params.Languages, "__params is CEL-only")

	errVar, ok := Get(celexp.VarError)
	require.True(t, ok)
	assert.Equal(t, []string{PhaseError}, errVar.Phases, "__error must be error-context-only")
	assert.Equal(t, []string{LangCEL}, errVar.Languages, "__error is CEL-only")
}

// TestDualLanguageVariables locks in that variables the engine injects into both
// CEL and Go-template data are reported as available in both.
func TestDualLanguageVariables(t *testing.T) {
	for _, name := range []string{"_", celexp.VarSelf, celexp.VarItem, celexp.VarIndex, celexp.VarActions, celexp.VarCwd, celexp.VarExecution} {
		v, ok := Get(name)
		require.True(t, ok, "%s must be registered", name)
		assert.ElementsMatch(t, []string{LangCEL, LangTemplate}, v.Languages, "%s must be available in both CEL and Go templates", name)
	}
}

// TestForEachScoping locks in that resolver data (_) and __self are reported as
// available inside forEach iterations, alongside __item and __index.
func TestForEachScoping(t *testing.T) {
	fe := ByPhase(PhaseForEach)
	names := make(map[string]bool)
	for _, v := range fe {
		names[v.Name] = true
	}
	assert.True(t, names["_"], "_ (resolver data) is in scope during forEach")
	assert.True(t, names[celexp.VarSelf], "__self is bound during forEach iterations")
	assert.True(t, names[celexp.VarItem], "__item is bound during forEach")
	assert.True(t, names[celexp.VarIndex], "__index is bound during forEach")
}

func TestByPhase(t *testing.T) {
	action := ByPhase(PhaseAction)
	require.NotEmpty(t, action)
	names := make(map[string]bool)
	for _, v := range action {
		names[v.Name] = true
		assert.Contains(t, v.Phases, PhaseAction)
	}
	assert.True(t, names[celexp.VarExecution])
	assert.True(t, names[celexp.VarActions])
	assert.True(t, names[celexp.VarCwd])
	// A resolve-only variable must not appear.
	assert.False(t, names[celexp.VarPlan], "__plan is resolve-phase, not action")

	// Unknown phase yields empty.
	assert.Empty(t, ByPhase("no-such-phase"))
}

func TestByPhase_TemplateFile(t *testing.T) {
	tmpl := ByPhase(PhaseTemplateFile)
	require.NotEmpty(t, tmpl)
	for _, v := range tmpl {
		assert.Equal(t, []string{LangTemplate}, v.Languages)
	}
	_, ok := Get(".__fileStem")
	assert.True(t, ok)
}

func TestPhases(t *testing.T) {
	phases := Phases()
	require.NotEmpty(t, phases)
	// Sorted and unique.
	for i := 1; i < len(phases); i++ {
		assert.Less(t, phases[i-1], phases[i])
	}
	assert.Contains(t, phases, PhaseResolve)
	assert.Contains(t, phases, PhaseAction)
	assert.Contains(t, phases, PhaseTemplateFile)
	assert.Contains(t, phases, PhaseForEach)
}

// TestList_ReturnsDeepCopy asserts callers cannot mutate the package registry
// through the returned values -- neither the top-level fields nor the nested
// Languages/Phases slices.
func TestList_ReturnsDeepCopy(t *testing.T) {
	a := List()
	require.NotEmpty(t, a)
	a[0].Name = "mutated"
	// Mutate the nested slices too.
	if len(a[0].Phases) > 0 {
		a[0].Phases[0] = "mutated-phase"
	}
	if len(a[0].Languages) > 0 {
		a[0].Languages[0] = "mutated-lang"
	}

	b := List()
	assert.NotEqual(t, "mutated", b[0].Name, "List must return a defensive copy of fields")
	assert.NotContains(t, b[0].Phases, "mutated-phase", "List must deep-copy Phases")
	assert.NotContains(t, b[0].Languages, "mutated-lang", "List must deep-copy Languages")
}

// TestGet_ReturnsDeepCopy asserts Get's returned value does not alias registry slices.
func TestGet_ReturnsDeepCopy(t *testing.T) {
	v, ok := Get("_")
	require.True(t, ok)
	require.NotEmpty(t, v.Phases)
	v.Phases[0] = "mutated"
	v.Languages[0] = "mutated"

	again, ok := Get("_")
	require.True(t, ok)
	assert.NotContains(t, again.Phases, "mutated", "Get must deep-copy Phases")
	assert.NotContains(t, again.Languages, "mutated", "Get must deep-copy Languages")
}

// TestByPhase_ReturnsDeepCopy asserts ByPhase's returned values do not alias registry slices.
func TestByPhase_ReturnsDeepCopy(t *testing.T) {
	vs := ByPhase(PhaseAction)
	require.NotEmpty(t, vs)
	vs[0].Phases[0] = "mutated"

	again := ByPhase(PhaseAction)
	require.NotEmpty(t, again)
	for _, v := range again {
		assert.NotContains(t, v.Phases, "mutated", "ByPhase must deep-copy Phases")
	}
}
