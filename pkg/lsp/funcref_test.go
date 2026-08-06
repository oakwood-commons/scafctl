// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"testing"

	celexpext "github.com/oakwood-commons/scafctl/pkg/celexp/ext"
	gotmplext "github.com/oakwood-commons/scafctl/pkg/gotmpl/ext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupFunc_CEL(t *testing.T) {
	fi, ok := LookupFunc("arrays.groupBy")
	require.True(t, ok, "arrays.groupBy should be a known CEL function")
	assert.True(t, fi.CEL)
	assert.Equal(t, "arrays.groupBy", fi.Name)
	assert.NotEmpty(t, fi.Signature, "CEL functions carry a signature")
	assert.NotEmpty(t, fi.Description)
}

func TestLookupFunc_Template(t *testing.T) {
	fi, ok := LookupFunc("upper")
	require.True(t, ok, "upper should be a known template function")
	assert.False(t, fi.CEL)
	assert.Equal(t, "upper", fi.Name)
	assert.Empty(t, fi.Signature, "template functions have no CEL signature")
	assert.NotEmpty(t, fi.Description)
}

func TestLookupFunc_Unknown(t *testing.T) {
	_, ok := LookupFunc("definitelyNotARealFunction")
	assert.False(t, ok)

	_, ok = LookupFunc("")
	assert.False(t, ok)
}

func TestAllFuncs(t *testing.T) {
	all := AllFuncs()
	require.NotEmpty(t, all)

	var cel, tmpl int
	seen := map[string]bool{}
	for _, f := range all {
		assert.NotEmpty(t, f.Name)
		assert.False(t, seen[f.Name], "duplicate name %q in AllFuncs", f.Name)
		seen[f.Name] = true
		if f.CEL {
			cel++
		} else {
			tmpl++
		}
	}
	assert.Positive(t, cel, "should include CEL functions")
	assert.Positive(t, tmpl, "should include template functions")

	// The returned slice is a copy; mutating it must not affect the index.
	all[0].Name = "mutated"
	fresh := AllFuncs()
	assert.NotEqual(t, "mutated", fresh[0].Name)
}

func TestLookupFunc_ExamplesPopulated(t *testing.T) {
	// arrays.groupBy ships examples; verify they are surfaced.
	fi, ok := LookupFunc("arrays.groupBy")
	require.True(t, ok)
	assert.NotEmpty(t, fi.Examples)
}

func TestLookupFunc_ExamplesAreACopy(t *testing.T) {
	// The returned Examples slice must be a copy: mutating an element (or
	// appending into spare capacity) must not corrupt the shared index that
	// subsequent hover/completion/signature-help calls read.
	fi, ok := LookupFunc("arrays.groupBy")
	require.True(t, ok)
	require.NotEmpty(t, fi.Examples)
	orig := fi.Examples[0]
	fi.Examples[0] = "mutated"

	fresh, ok := LookupFunc("arrays.groupBy")
	require.True(t, ok)
	require.NotEmpty(t, fresh.Examples)
	assert.Equal(t, orig, fresh.Examples[0], "mutating a returned example must not affect the index")
}

func TestAllFuncs_ExamplesAreACopy(t *testing.T) {
	// AllFuncs promises a copy the caller may retain or mutate; that guarantee
	// must extend to each entry's Examples slice, not just the outer slice.
	all := AllFuncs()
	idx := -1
	for i := range all {
		if len(all[i].Examples) > 0 {
			idx = i
			break
		}
	}
	require.GreaterOrEqual(t, idx, 0, "expected at least one function with examples")
	orig := all[idx].Examples[0]
	name := all[idx].Name
	all[idx].Examples[0] = "mutated"

	fresh, ok := LookupFunc(name)
	require.True(t, ok)
	require.NotEmpty(t, fresh.Examples)
	assert.Equal(t, orig, fresh.Examples[0], "mutating an AllFuncs example must not affect the index")
}

func TestLookupFunc_CELFirstPrecedence(t *testing.T) {
	// A name registered in both the CEL and template registries must resolve to
	// its CEL definition (CEL is the primary expression language). This locks the
	// contract that #775/#777 rely on. Find an overlapping name dynamically so
	// the test stays valid as the registries evolve.
	cel := map[string]bool{}
	for _, f := range celexpext.All() {
		cel[f.Name] = true
		for _, sub := range f.GetSubNames() {
			cel[sub] = true
		}
	}
	overlap := ""
	for _, f := range gotmplext.All() {
		if cel[f.Name] {
			overlap = f.Name
			break
		}
	}
	if overlap == "" {
		t.Skip("no name is registered in both the CEL and template registries")
	}
	fi, ok := LookupFunc(overlap)
	require.True(t, ok)
	assert.True(t, fi.CEL, "overlapping name %q must resolve to the CEL definition", overlap)
}
