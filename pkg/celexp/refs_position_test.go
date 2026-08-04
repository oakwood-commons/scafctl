// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnderscoreVariableRefs(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		expr string
		want []VariableRef
	}{
		{
			name: "single ref",
			expr: `_.appName`,
			want: []VariableRef{{Name: "appName", Offset: 2, Len: 7}},
		},
		{
			name: "two refs in source order",
			expr: `_.a + _.b`,
			want: []VariableRef{
				{Name: "a", Offset: 2, Len: 1},
				{Name: "b", Offset: 8, Len: 1},
			},
		},
		{
			name: "duplicate name -> two distinct occurrences",
			expr: `_.a + _.a`,
			want: []VariableRef{
				{Name: "a", Offset: 2, Len: 1},
				{Name: "a", Offset: 8, Len: 1},
			},
		},
		{
			name: "optional access flagged",
			expr: `_.?maybe.orValue("")`,
			want: []VariableRef{{Name: "maybe", Offset: 3, Len: 5, Optional: true}},
		},
		{
			name: "bracket access",
			expr: `_["bracketed"]`,
			want: []VariableRef{{Name: "bracketed", Offset: 3, Len: 9}},
		},
		{
			name: "string literal is NOT matched",
			expr: `_.a + "a"`,
			want: []VariableRef{{Name: "a", Offset: 2, Len: 1}},
		},
		{
			name: "short name does not match inside longer identifier",
			expr: `_.app + _.appName`,
			want: []VariableRef{
				{Name: "app", Offset: 2, Len: 3},
				{Name: "appName", Offset: 10, Len: 7},
			},
		},
		{
			name: "no underscore refs",
			expr: `1 + 2 + foo.bar`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, err := Expression(tt.expr).UnderscoreVariableRefs(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, refs)

			// Byte-exact invariant: every reported range slices out exactly the name.
			raw := []byte(tt.expr)
			for _, r := range refs {
				require.LessOrEqual(t, r.End(), len(raw))
				assert.Equalf(t, r.Name, string(raw[r.Offset:r.End()]),
					"range [%d:%d] must slice to %q", r.Offset, r.End(), r.Name)
			}
		})
	}
}

func TestUnderscoreVariableRefs_MultibytePrefix(t *testing.T) {
	ctx := context.Background()
	// A 2-byte rune before the reference must shift the BYTE offset by 2 even
	// though CEL's internal offset counts it as one rune.
	expr := `"é" + _.afterMultibyte`
	refs, err := Expression(expr).UnderscoreVariableRefs(ctx)
	require.NoError(t, err)
	require.Len(t, refs, 1)

	r := refs[0]
	assert.Equal(t, "afterMultibyte", r.Name)
	// Byte-exact: the offset accounts for the 2-byte 'é'.
	assert.Equal(t, "afterMultibyte", string([]byte(expr)[r.Offset:r.End()]))
}

func TestUnderscoreVariableRefs_HardAndOptionalSameName(t *testing.T) {
	ctx := context.Background()
	// Both occurrences are reported (per-occurrence semantics), each with its
	// own optionality -- unlike GetUnderscoreVariablesByOptionality which
	// collapses to a name set where hard dominates.
	refs, err := Expression(`_.a + _.?a.orValue("")`).UnderscoreVariableRefs(ctx)
	require.NoError(t, err)
	require.Len(t, refs, 2)

	assert.Equal(t, "a", refs[0].Name)
	assert.False(t, refs[0].Optional)
	assert.Equal(t, "a", refs[1].Name)
	assert.True(t, refs[1].Optional)
}

func TestUnderscoreVariableRefs_ParseError(t *testing.T) {
	ctx := context.Background()
	_, err := Expression(`_.a +`).UnderscoreVariableRefs(ctx)
	assert.Error(t, err)
}

func TestVariableRef_End(t *testing.T) {
	assert.Equal(t, 9, VariableRef{Offset: 2, Len: 7}.End())
}

func TestIsIdentRune(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{'_', true},
		{'a', true},
		{'Z', true},
		{'5', true},
		{'é', true}, // non-ASCII treated as ident rune (forms a boundary)
		{'.', false},
		{' ', false},
		{'"', false},
		{'[', false},
	}
	for _, tt := range tests {
		t.Run(string(tt.r), func(t *testing.T) {
			assert.Equal(t, tt.want, isIdentRune(tt.r))
		})
	}
}

func TestFindIdentNear(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		target string
		anchor int
		want   int
	}{
		{"basic", "_.abc", "abc", 1, 2},
		{"nearest to anchor picks second", "a + a", "a", 4, 4},
		{"nearest to anchor picks first", "a + a", "a", 0, 0},
		{"no match", "xyz", "abc", 0, -1},
		{"boundary rejects substring", "appName", "app", 0, -1},
		{"empty target", "abc", "", 0, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findIdentNear([]rune(tt.src), []rune(tt.target), tt.anchor)
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkUnderscoreVariableRefs(b *testing.B) {
	ctx := context.Background()
	expr := Expression(`_.a + _.b.field + _.?c.orValue("") + _["d"] + map.merge(_.e, _.f)`)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = expr.UnderscoreVariableRefs(ctx)
	}
}

func TestUnderscoreVariableRefs_UnpositionableIsReportedNotDropped(t *testing.T) {
	// A bracket key whose decoded value differs from the source (escaped quote)
	// cannot be located byte-exact. It must be reported with Offset -1 (so a
	// rename can fail safe) rather than silently dropped.
	ctx := context.Background()
	refs, err := Expression(`_["a\"b"]`).UnderscoreVariableRefs(ctx)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, `a"b`, refs[0].Name)
	assert.Equal(t, -1, refs[0].Offset, "unpositionable occurrence must be reported with Offset -1")
}

func TestPrefixedVariableRefs_ActionsPrefix(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		expr string
		want []VariableRef
	}{
		{
			name: "single action ref with trailing path",
			expr: `__actions.build.results.exitCode`,
			want: []VariableRef{{Name: "build", Offset: 10, Len: 5}},
		},
		{
			name: "two action refs in source order",
			expr: `__actions.a + __actions.b`,
			want: []VariableRef{
				{Name: "a", Offset: 10, Len: 1},
				{Name: "b", Offset: 24, Len: 1},
			},
		},
		{
			name: "resolver refs are ignored under the actions prefix",
			expr: `_.env + __actions.deploy`,
			want: []VariableRef{{Name: "deploy", Offset: 18, Len: 6}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, err := Expression(tt.expr).PrefixedVariableRefs(ctx, "__actions.")
			require.NoError(t, err)
			assert.Equal(t, tt.want, refs)

			// Byte-exact invariant: each positioned range slices out its name.
			for _, r := range refs {
				if r.Offset < 0 {
					continue
				}
				assert.Equal(t, r.Name, tt.expr[r.Offset:r.Offset+r.Len])
			}
		})
	}
}

func TestPrefixedVariableRefs_ParseError(t *testing.T) {
	_, err := Expression(`__actions.a +`).PrefixedVariableRefs(context.Background(), "__actions.")
	require.Error(t, err)
}
