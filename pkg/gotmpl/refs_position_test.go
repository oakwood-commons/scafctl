// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"testing"
	"text/template/parse"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPositionedReferences(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []PositionedRef
	}{
		{
			name: "bare field",
			tmpl: `{{ .appName }}`,
			want: []PositionedRef{{Name: "appName", Offset: 4, Len: 7, Kind: RefKindField}},
		},
		{
			name: "explicit resolver dot form",
			tmpl: `{{ ._.resolverRef }}`,
			want: []PositionedRef{{Name: "resolverRef", Offset: 6, Len: 11, Kind: RefKindExplicitResolver}},
		},
		{
			name: "explicit resolver underscore form skips leading underscore",
			tmpl: `{{ ._name }}`,
			want: []PositionedRef{{Name: "name", Offset: 5, Len: 4, Kind: RefKindExplicitResolver}},
		},
		{
			name: "special variable is skipped",
			tmpl: `{{ .__self }}`,
			want: nil,
		},
		{
			name: "bare resolver context is skipped",
			tmpl: `{{ ._ }}`,
			want: nil,
		},
		{
			name: "root segment of a chained field",
			tmpl: `{{ .config.host }}`,
			want: []PositionedRef{{Name: "config", Offset: 4, Len: 6, Kind: RefKindField}},
		},
		{
			name: "two refs across the template in source order",
			tmpl: `prefix {{ .a.b }} mid {{ .c }}`,
			want: []PositionedRef{
				{Name: "a", Offset: 11, Len: 1, Kind: RefKindField},
				{Name: "c", Offset: 26, Len: 1, Kind: RefKindField},
			},
		},
		{
			name: "nested pipe argument",
			tmpl: `{{ printf "%s" .x }}`,
			want: []PositionedRef{{Name: "x", Offset: 16, Len: 1, Kind: RefKindField}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetGoTemplatePositionedReferences(tt.tmpl, "", "")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			// Byte-exact invariant: every reported range slices out exactly the name.
			for _, r := range got {
				require.LessOrEqual(t, r.End(), len(tt.tmpl))
				assert.Equalf(t, r.Name, tt.tmpl[r.Offset:r.End()],
					"range [%d:%d] must slice to %q", r.Offset, r.End(), r.Name)
			}
		})
	}
}

func TestGetPositionedReferences_Scoped(t *testing.T) {
	// Inside {{ range }} the dot is rebound: .items is unscoped, .inner is scoped.
	tmpl := `{{ range .items }}{{ .inner }}{{ end }}`
	got, err := GetGoTemplatePositionedReferences(tmpl, "", "")
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "items", got[0].Name)
	assert.False(t, got[0].Scoped)
	assert.Equal(t, "inner", got[1].Name)
	assert.True(t, got[1].Scoped)

	for _, r := range got {
		assert.Equal(t, r.Name, tmpl[r.Offset:r.End()])
	}
}

func TestGetPositionedReferences_MultibytePrefix(t *testing.T) {
	// A 2-byte rune before the reference must shift the BYTE offset accordingly.
	tmpl := `{{ "é" }}{{ .afterMultibyte }}`
	got, err := GetGoTemplatePositionedReferences(tmpl, "", "")
	require.NoError(t, err)
	require.Len(t, got, 1)

	r := got[0]
	assert.Equal(t, "afterMultibyte", r.Name)
	assert.Equal(t, "afterMultibyte", tmpl[r.Offset:r.End()])
}

func TestGetPositionedReferences_CustomDelims(t *testing.T) {
	got, err := GetGoTemplatePositionedReferences(`[[ ._.env ]]`, "[[", "]]")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, PositionedRef{Name: "env", Offset: 6, Len: 3, Kind: RefKindExplicitResolver}, got[0])
	assert.Equal(t, "env", `[[ ._.env ]]`[got[0].Offset:got[0].End()])
}

func TestGetPositionedReferences_Errors(t *testing.T) {
	_, err := GetGoTemplatePositionedReferences("", "", "")
	assert.Error(t, err, "empty content")

	_, err = GetGoTemplatePositionedReferences(`{{ .x `, "", "")
	assert.Error(t, err, "unterminated action")
}

func TestPositionedRef_End(t *testing.T) {
	assert.Equal(t, 11, PositionedRef{Offset: 4, Len: 7}.End())
}

// TestGetPositionedReferences_ControlFlow exercises if/else, range/else,
// with/else, and template nodes so every branch of walkPositioned is covered.
func TestGetPositionedReferences_ControlFlow(t *testing.T) {
	type wantRef struct {
		name   string
		scoped bool
	}
	tests := []struct {
		name string
		tmpl string
		want []wantRef
	}{
		{
			name: "if/else",
			tmpl: `{{ if .cond }}{{ .a }}{{ else }}{{ .b }}{{ end }}`,
			want: []wantRef{{"cond", false}, {"a", false}, {"b", false}},
		},
		{
			name: "range with else",
			tmpl: `{{ range .items }}{{ .x }}{{ else }}{{ .empty }}{{ end }}`,
			want: []wantRef{{"items", false}, {"x", true}, {"empty", false}},
		},
		{
			name: "with and else",
			tmpl: `{{ with .ctx }}{{ .inner }}{{ else }}{{ .fallback }}{{ end }}`,
			want: []wantRef{{"ctx", false}, {"inner", true}, {"fallback", false}},
		},
		{
			name: "template node pipe",
			tmpl: `{{ template "sub" .data }}`,
			want: []wantRef{{"data", false}},
		},
		{
			name: "parenthesized sub-pipe argument",
			tmpl: `{{ len (.items) }}`,
			want: []wantRef{{"items", false}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetGoTemplatePositionedReferences(tt.tmpl, "", "")
			require.NoError(t, err)
			require.Len(t, got, len(tt.want))
			for i, w := range tt.want {
				assert.Equalf(t, w.name, got[i].Name, "ref %d name", i)
				assert.Equalf(t, w.scoped, got[i].Scoped, "ref %d scoped", i)
				assert.Equal(t, got[i].Name, tt.tmpl[got[i].Offset:got[i].End()])
			}
		})
	}
}

// TestFieldStartOffset_Fallback white-box tests the defensive fallback: when the
// Pos-based arithmetic does not validate against src, the field text is located
// by scanning.
func TestFieldStartOffset_Fallback(t *testing.T) {
	// ".a" lives at index 3, but a deliberately wrong Pos makes the arithmetic
	// out of range so the fallback scan must recover it.
	fn := &parse.FieldNode{NodeType: parse.NodeField, Pos: 100, Ident: []string{"a"}}
	start, ok := fieldStartOffset("xx .a yy", fn)
	require.True(t, ok)
	assert.Equal(t, 3, start)

	// When the field text is absent, it reports failure.
	_, ok = fieldStartOffset("nothing here", fn)
	assert.False(t, ok)
}

func TestIndexNearest(t *testing.T) {
	tests := []struct {
		name string
		src  string
		sub  string
		pos  int
		want int
	}{
		{"single", "abc._.x def", "._.x", 3, 3},
		{"nearest of two", "._.x ._.x", "._.x", 6, 5},
		{"not found", "abc", "._.x", 0, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, indexNearest(tt.src, tt.sub, tt.pos))
		})
	}
}

func BenchmarkGetPositionedReferences(b *testing.B) {
	tmpl := `{{ ._.a }}-{{ .b.c }}{{ range .items }}{{ .inner }}{{ end }}{{ printf "%s" ._.d }}`
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = GetGoTemplatePositionedReferences(tmpl, "", "")
	}
}
