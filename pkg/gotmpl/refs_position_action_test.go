// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPositionedActionReferences(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []PositionedRef
	}{
		{
			name: "action reference with trailing path",
			tmpl: `{{ .__actions.build.results }}`,
			want: []PositionedRef{{Name: "build", Offset: 14, Len: 5, Kind: RefKindExplicitAction}},
		},
		{
			name: "bare __actions root is not a reference",
			tmpl: `{{ .__actions }}`,
			want: nil,
		},
		{
			name: "plain field is not an action reference",
			tmpl: `{{ .build }}`,
			want: nil,
		},
		{
			name: "explicit resolver ref is not an action reference",
			tmpl: `{{ ._.build }}`,
			want: nil,
		},
		{
			name: "two action refs in source order",
			tmpl: `{{ .__actions.a.x }} mid {{ .__actions.b }}`,
			want: []PositionedRef{
				{Name: "a", Offset: 14, Len: 1, Kind: RefKindExplicitAction},
				{Name: "b", Offset: 39, Len: 1, Kind: RefKindExplicitAction},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetGoTemplatePositionedActionReferences(tt.tmpl, "", "")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			// Byte-exact invariant: every range slices out exactly the name.
			for _, r := range got {
				require.LessOrEqual(t, r.End(), len(tt.tmpl))
				assert.Equalf(t, r.Name, tt.tmpl[r.Offset:r.End()],
					"range [%d:%d] must slice to %q", r.Offset, r.End(), r.Name)
			}
		})
	}
}

func TestGetPositionedActionReferences_Scoped(t *testing.T) {
	// Inside {{ with }} the dot is rebound, so the action ref is scoped.
	tmpl := `{{ with .ctx }}{{ .__actions.deploy }}{{ end }}`
	got, err := GetGoTemplatePositionedActionReferences(tmpl, "", "")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "deploy", got[0].Name)
	assert.True(t, got[0].Scoped)
	assert.Equal(t, "deploy", tmpl[got[0].Offset:got[0].End()])
}

func TestGetPositionedActionReferences_CustomDelims(t *testing.T) {
	got, err := GetGoTemplatePositionedActionReferences(`[[ .__actions.x ]]`, "[[", "]]")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, PositionedRef{Name: "x", Offset: 14, Len: 1, Kind: RefKindExplicitAction}, got[0])
}

func TestGetPositionedActionReferences_Errors(t *testing.T) {
	_, err := GetGoTemplatePositionedActionReferences("", "", "")
	assert.Error(t, err, "empty content")

	_, err = GetGoTemplatePositionedActionReferences(`{{ .__actions.x `, "", "")
	assert.Error(t, err, "unterminated action")
}

func BenchmarkGetPositionedActionReferences(b *testing.B) {
	tmpl := `{{ .__actions.a.results }}-{{ .b }}{{ with .ctx }}{{ .__actions.c }}{{ end }}{{ printf "%s" .__actions.d }}`
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = GetGoTemplatePositionedActionReferences(tmpl, "", "")
	}
}
