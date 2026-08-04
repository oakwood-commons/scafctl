// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package sourcepos

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLineIndex_Position(t *testing.T) {
	// "line1\nline2\ncafé x\n"
	//  offsets: l=0..4 \n=5 | line2=6..10 \n=11 | café x=12.. (é is 2 bytes)
	src := []byte("line1\nline2\ncafé x\n")

	tests := []struct {
		name       string
		byteOffset int
		wantLine   int
		wantCol    int
	}{
		{"start of file", 0, 1, 1},
		{"mid line1", 3, 1, 4},
		{"end of line1 (the newline)", 5, 1, 6},
		{"start of line2", 6, 2, 1},
		{"start of line3", 12, 3, 1},
		// Column must count runes: 'c','a','f','é' -> the space after é is col 5,
		// not col 6, even though é is 2 bytes. Byte offset of the space is 17.
		{"after multibyte rune", 17, 3, 5},
		{"the x after multibyte", 18, 3, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			li := NewLineIndex(src, "f.yaml")
			pos := li.Position(tt.byteOffset)
			assert.Equal(t, tt.wantLine, pos.Line, "line")
			assert.Equal(t, tt.wantCol, pos.Column, "column")
			assert.Equal(t, "f.yaml", pos.File, "file")
		})
	}
}

func TestLineIndex_Position_Clamping(t *testing.T) {
	src := []byte("ab\ncd")
	li := NewLineIndex(src, "")

	// Negative clamps to start.
	neg := li.Position(-5)
	assert.Equal(t, 1, neg.Line)
	assert.Equal(t, 1, neg.Column)

	// Beyond end clamps to the final position (offset len(src) == 5 -> line 2, col 3).
	over := li.Position(9999)
	assert.Equal(t, 2, over.Line)
	assert.Equal(t, 3, over.Column)
}

func TestLineIndex_Position_EmptySource(t *testing.T) {
	li := NewLineIndex([]byte(""), "empty.yaml")
	assert.Equal(t, 1, li.Len())
	pos := li.Position(0)
	assert.Equal(t, 1, pos.Line)
	assert.Equal(t, 1, pos.Column)
	assert.Equal(t, "empty.yaml", pos.File)
}

func TestLineIndex_Offset_RoundTrip(t *testing.T) {
	src := []byte("apiVersion: v1\nspec:\n  expr: _.café\n")
	li := NewLineIndex(src, "")

	// For every byte offset that begins a rune, Position -> Offset must be
	// stable (idempotent round-trip).
	for off := 0; off <= len(src); off++ {
		// Skip offsets that land in the middle of a multibyte rune.
		if off < len(src) && !utf8RuneStart(src[off]) {
			continue
		}
		pos := li.Position(off)
		got := li.Offset(pos)
		assert.Equalf(t, off, got, "round-trip failed at byte offset %d (pos %s)", off, pos)
	}
}

func TestLineIndex_Offset_Clamping(t *testing.T) {
	src := []byte("abc\ndef")
	li := NewLineIndex(src, "")

	// Line below 1 -> offset 0.
	assert.Equal(t, 0, li.Offset(Position{Line: 0, Column: 5}))
	// Line beyond the document -> end of source.
	assert.Equal(t, len(src), li.Offset(Position{Line: 99, Column: 1}))
	// Column beyond the end of its line clamps to the line end (before the '\n').
	assert.Equal(t, 3, li.Offset(Position{Line: 1, Column: 99}))
}

func TestLineIndex_Range(t *testing.T) {
	// The identifier "appName" sits on line 2. End is exclusive.
	src := []byte("spec:\n  expr: _.appName\n")
	li := NewLineIndex(src, "sol.yaml")

	// Byte span of "appName": find it.
	start := indexOf(src, "appName")
	end := start + len("appName")

	r := li.Range(start, end)
	assert.Equal(t, 2, r.Start.Line)
	assert.Equal(t, 2, r.End.Line)
	// The sliced bytes must equal the identifier -- the invariant rename relies on.
	assert.Equal(t, "appName", string(src[li.Offset(r.Start):li.Offset(r.End)]))
	assert.False(t, r.IsZero())
}

func TestLineIndex_NilSafety(t *testing.T) {
	var li *LineIndex
	assert.Equal(t, 0, li.Len())
	assert.True(t, li.Position(0).IsZero())
	assert.Equal(t, 0, li.Offset(Position{Line: 1, Column: 1}))
}

func TestLineIndex_CRLF(t *testing.T) {
	// Documented behavior: lines split on "\n"; a "\r" before it counts as a
	// rune in the column. This locks the behavior so it is a conscious choice.
	src := []byte("ab\r\ncd")
	li := NewLineIndex(src, "")
	// 'c' is at byte offset 4 (a,b,\r,\n,c) -> line 2, col 1.
	assert.Equal(t, Position{Line: 2, Column: 1}, li.Position(4))
	// The "\r" is at offset 2 -> still line 1, col 3.
	assert.Equal(t, Position{Line: 1, Column: 3}, li.Position(2))
}

func TestRange_String(t *testing.T) {
	r := Range{
		Start: Position{Line: 2, Column: 11, File: "s.yaml"},
		End:   Position{Line: 2, Column: 18, File: "s.yaml"},
	}
	assert.Equal(t, "s.yaml:2:11-s.yaml:2:18", r.String())
}

func TestRange_IsZero(t *testing.T) {
	assert.True(t, Range{}.IsZero())
	assert.False(t, Range{Start: Position{Line: 1, Column: 1}}.IsZero())
}

// --- helpers ---

func utf8RuneStart(b byte) bool {
	// Continuation bytes are 0b10xxxxxx.
	return b&0xC0 != 0x80
}

func indexOf(src []byte, sub string) int {
	s := string(src)
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func BenchmarkLineIndex_Position(b *testing.B) {
	src := []byte("apiVersion: v1\nkind: Solution\nspec:\n  resolvers:\n    appName:\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              value: _.environment\n")
	li := NewLineIndex(src, "bench.yaml")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = li.Position(len(src) - 5)
	}
}

func BenchmarkNewLineIndex(b *testing.B) {
	src := []byte("apiVersion: v1\nkind: Solution\nspec:\n  resolvers:\n    appName:\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              value: _.environment\n")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = NewLineIndex(src, "bench.yaml")
	}
}
