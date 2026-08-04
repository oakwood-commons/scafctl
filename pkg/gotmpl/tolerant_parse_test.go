// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTemplatesTolerant(t *testing.T) {
	base := map[string]any{"printf": true}

	tests := []struct {
		name    string
		tmpl    string
		wantErr bool
	}{
		{"no unknown functions", `{{ .x }} {{ printf "%s" .y }}`, false},
		{"single author function", `{{ greet .x }}`, false},
		{"multiple author functions", `{{ greet .x }}{{ shout .y }}`, false},
		{"author function piped", `{{ .x | greet }}`, false},
		{"author function nested", `{{ printf "%s" (greet .x) }}`, false},
		{"genuine syntax error still fails", `{{ .x `, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trees, err := parseTemplatesTolerant("t", tt.tmpl, "", "", base)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, trees)
		})
	}

	// The caller's func map must not be mutated by tolerant parsing.
	_, err := parseTemplatesTolerant("t", `{{ greet .x }}`, "", "", base)
	require.NoError(t, err)
	_, leaked := base["greet"]
	assert.False(t, leaked, "tolerant parse must not mutate the caller's funcs map")
}

// TestGetGoTemplateReferences_TolerateAuthorFunctions is the regression test for
// the core fix: reference extraction from a template that invokes an unknown
// (author-defined) function must succeed and still return the data references,
// rather than failing to parse and silently dropping them.
func TestGetGoTemplateReferences_TolerateAuthorFunctions(t *testing.T) {
	refs, err := GetGoTemplateReferences(`{{ greet ._.env }} and {{ loud ._.appName }}`, "", "")
	require.NoError(t, err)

	paths := make(map[string]bool)
	for _, r := range refs {
		paths[r.Path] = true
	}
	assert.True(t, paths["._.env"], "resolver ref ._.env must survive the author-function call")
	assert.True(t, paths["._.appName"], "resolver ref ._.appName must survive the author-function call")

	// A genuine syntax error is still reported.
	_, err = GetGoTemplateReferences(`{{ .x `, "", "")
	require.Error(t, err)
}

// TestPositionedExtractors_TolerateAuthorFunctions confirms the positioned
// reference extractors also tolerate an unknown author function even when no
// declaredFuncs are supplied (refindex passes them, but the extractors must not
// depend on it to avoid dropping references).
func TestPositionedExtractors_TolerateAuthorFunctions(t *testing.T) {
	refs, err := GetGoTemplatePositionedReferences(`{{ greet ._.env }}`, "", "", nil)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "env", refs[0].Name)

	aRefs, err := GetGoTemplatePositionedActionReferences(`{{ greet .__actions.build }}`, "", "", nil)
	require.NoError(t, err)
	require.Len(t, aRefs, 1)
	assert.Equal(t, "build", aRefs[0].Name)
}
