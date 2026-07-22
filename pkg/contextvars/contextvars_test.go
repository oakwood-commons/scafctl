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
		assert.Contains(t, []string{LangCEL, LangTemplate}, v.Language, "language must be cel or go-template: %s", v.Name)
		assert.NotEmpty(t, v.Phases, "phases must not be empty: %s", v.Name)
		assert.NotEmpty(t, v.Description, "description must not be empty: %s", v.Name)
	}

	// Sorted by language then name.
	for i := 1; i < len(all); i++ {
		prev, cur := all[i-1], all[i]
		if prev.Language == cur.Language {
			assert.LessOrEqual(t, prev.Name, cur.Name)
		} else {
			assert.Less(t, prev.Language, cur.Language)
		}
	}
}

func TestGet(t *testing.T) {
	v, ok := Get("__self")
	require.True(t, ok)
	assert.Equal(t, celexp.VarSelf, v.Name)
	assert.Equal(t, LangCEL, v.Language)
	assert.ElementsMatch(t, []string{PhaseTransform, PhaseValidate}, v.Phases)

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

	errVar, ok := Get(celexp.VarError)
	require.True(t, ok)
	assert.Equal(t, []string{PhaseError}, errVar.Phases, "__error must be error-context-only")
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
		assert.Equal(t, LangTemplate, v.Language)
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
}

func TestList_ReturnsCopy(t *testing.T) {
	a := List()
	require.NotEmpty(t, a)
	a[0].Name = "mutated"
	b := List()
	assert.NotEqual(t, "mutated", b[0].Name, "List must return a defensive copy")
}
