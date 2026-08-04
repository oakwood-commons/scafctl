// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetGoTemplatePositionedFunctionCalls(t *testing.T) {
	declared := []string{"greet", "loud"}
	tests := []struct {
		name string
		tmpl string
		want []PositionedRef
	}{
		{
			name: "author function at command head",
			tmpl: `{{ greet .x }}`,
			want: []PositionedRef{{Name: "greet", Offset: 3, Len: 5, Kind: RefKindFunctionCall}},
		},
		{
			name: "builtin and author in source order",
			tmpl: `{{ greet .x }} {{ printf "%s" .y }}`,
			want: []PositionedRef{
				{Name: "greet", Offset: 3, Len: 5, Kind: RefKindFunctionCall},
				{Name: "printf", Offset: 18, Len: 6, Kind: RefKindFunctionCall},
			},
		},
		{
			name: "piped function is a command head",
			tmpl: `{{ .x | greet }}`,
			want: []PositionedRef{{Name: "greet", Offset: 8, Len: 5, Kind: RefKindFunctionCall}},
		},
		{
			name: "nested parenthesized call",
			tmpl: `{{ loud (greet .x) }}`,
			want: []PositionedRef{
				{Name: "loud", Offset: 3, Len: 4, Kind: RefKindFunctionCall},
				{Name: "greet", Offset: 9, Len: 5, Kind: RefKindFunctionCall},
			},
		},
		{
			name: "no function calls",
			tmpl: `{{ .x }}`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetGoTemplatePositionedFunctionCalls(tt.tmpl, "", "", declared)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			for _, r := range got {
				require.LessOrEqual(t, r.End(), len(tt.tmpl))
				assert.Equalf(t, r.Name, tt.tmpl[r.Offset:r.End()],
					"range [%d:%d] must slice to %q", r.Offset, r.End(), r.Name)
			}
		})
	}
}

func TestGetGoTemplatePositionedFunctionCalls_ControlFlow(t *testing.T) {
	// Function calls inside if/range/with/template pipes are all found.
	tmpl := `{{ if greet .c }}{{ loud .a }}{{ else }}{{ greet .b }}{{ end }}`
	got, err := GetGoTemplatePositionedFunctionCalls(tmpl, "", "", []string{"greet", "loud"})
	require.NoError(t, err)
	names := make([]string, len(got))
	for i, r := range got {
		names[i] = r.Name
		assert.Equal(t, r.Name, tmpl[r.Offset:r.End()])
	}
	assert.Equal(t, []string{"greet", "loud", "greet"}, names)
}

func TestGetGoTemplatePositionedFunctionCalls_CustomDelims(t *testing.T) {
	got, err := GetGoTemplatePositionedFunctionCalls(`[[ greet .x ]]`, "[[", "]]", []string{"greet"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, PositionedRef{Name: "greet", Offset: 3, Len: 5, Kind: RefKindFunctionCall}, got[0])
}

func TestGetGoTemplatePositionedFunctionCalls_Errors(t *testing.T) {
	_, err := GetGoTemplatePositionedFunctionCalls("", "", "", nil)
	assert.Error(t, err, "empty content")

	// An undeclared author function is an "unknown function" parse error.
	_, err = GetGoTemplatePositionedFunctionCalls(`{{ greet .x }}`, "", "", nil)
	assert.Error(t, err, "undeclared function")
}

func BenchmarkGetGoTemplatePositionedFunctionCalls(b *testing.B) {
	tmpl := `{{ greet .x }}-{{ printf "%s" .y }}{{ if loud .c }}{{ greet (loud .z) }}{{ end }}`
	declared := []string{"greet", "loud"}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = GetGoTemplatePositionedFunctionCalls(tmpl, "", "", declared)
	}
}
