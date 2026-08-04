// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package sourcepos

import (
	"fmt"
	"sort"
	"unicode/utf8"
)

// Range is a half-open span [Start, End) between two positions in a source
// document. It describes the exact extent of a token -- a resolver reference,
// a field name, an expression -- so tools can highlight it, navigate to it, or
// rewrite it (e.g. a rename that replaces just the identifier bytes).
//
// End is exclusive: for the identifier "appName" starting at column 5, Start is
// column 5 and End is column 12, so End.Column - Start.Column == len("appName").
type Range struct {
	// Start is the inclusive start position of the span.
	Start Position `json:"start" yaml:"start" doc:"Inclusive start position of the span"`

	// End is the exclusive end position of the span.
	End Position `json:"end" yaml:"end" doc:"Exclusive end position of the span"`
}

// String returns a human-readable representation of the range.
func (r Range) String() string {
	return fmt.Sprintf("%s-%s", r.Start.String(), r.End.String())
}

// IsZero reports whether the range has no meaningful location.
func (r Range) IsZero() bool {
	return r.Start.IsZero() && r.End.IsZero()
}

// LineIndex provides fast, bidirectional conversion between byte offsets and
// 1-based line/column positions in a source document.
//
// Columns are counted in RUNES (Unicode code points), not bytes, matching YAML
// and editor conventions: a multibyte character such as "e-acute" advances the
// column by one, not by its UTF-8 byte length. This is what lets positioned
// reference extraction stay accurate when an expression contains non-ASCII text
// before an identifier.
//
// A LineIndex holds a reference to the source bytes it was built from; callers
// must not mutate that slice while the index is in use.
type LineIndex struct {
	src       []byte
	file      string
	lineStart []int // lineStart[i] = byte offset of the first byte of line i+1
}

// NewLineIndex builds a LineIndex over src. The file argument is recorded in
// every Position the index produces, which matters for multi-file (compose)
// scenarios. Lines are split on "\n"; a trailing "\n" yields a final empty
// line, consistent with editor behavior.
func NewLineIndex(src []byte, file string) *LineIndex {
	// Line 1 always begins at offset 0. Each "\n" starts a new line at the
	// byte immediately after it.
	lineStart := make([]int, 1, 1+len(src)/32)
	lineStart[0] = 0
	for i, b := range src {
		if b == '\n' {
			lineStart = append(lineStart, i+1)
		}
	}
	return &LineIndex{src: src, file: file, lineStart: lineStart}
}

// Len returns the number of lines in the indexed document.
func (li *LineIndex) Len() int {
	if li == nil {
		return 0
	}
	return len(li.lineStart)
}

// Position converts a byte offset to a 1-based line/column Position. The offset
// is clamped to [0, len(src)], so out-of-range offsets return the nearest valid
// boundary position rather than panicking.
func (li *LineIndex) Position(byteOffset int) Position {
	if li == nil {
		return Position{}
	}
	if byteOffset < 0 {
		byteOffset = 0
	}
	if byteOffset > len(li.src) {
		byteOffset = len(li.src)
	}

	// Find the line whose start is the greatest offset <= byteOffset.
	// sort.Search returns the first index whose start is > byteOffset.
	idx := sort.Search(len(li.lineStart), func(k int) bool {
		return li.lineStart[k] > byteOffset
	}) - 1
	if idx < 0 {
		idx = 0
	}

	lineByteStart := li.lineStart[idx]
	// Column is the number of runes between the line start and the offset,
	// plus one (columns are 1-based).
	column := utf8.RuneCount(li.src[lineByteStart:byteOffset]) + 1

	return Position{Line: idx + 1, Column: column, File: li.file}
}

// Offset converts a 1-based line/column Position back to a byte offset. It is
// the inverse of Position for in-range inputs. Out-of-range lines clamp to the
// document bounds; a column past the end of its line clamps to the line end.
func (li *LineIndex) Offset(pos Position) int {
	if li == nil || pos.Line < 1 {
		return 0
	}
	if pos.Line > len(li.lineStart) {
		return len(li.src)
	}

	lineByteStart := li.lineStart[pos.Line-1]
	lineByteEnd := len(li.src)
	if pos.Line < len(li.lineStart) {
		// Exclude the trailing "\n" (which lives at lineStart[pos.Line]-1) so an
		// over-long column clamps to the end of the visible content, not onto
		// the newline of the next line.
		lineByteEnd = li.lineStart[pos.Line] - 1
	}

	// Advance (Column-1) runes from the line start, without crossing into the
	// next line.
	off := lineByteStart
	for c := 1; c < pos.Column && off < lineByteEnd; c++ {
		_, size := utf8.DecodeRune(li.src[off:lineByteEnd])
		if size == 0 {
			break
		}
		off += size
	}
	return off
}

// Range builds a Range from a half-open byte span [startByte, endByte).
func (li *LineIndex) Range(startByte, endByte int) Range {
	return Range{
		Start: li.Position(startByte),
		End:   li.Position(endByte),
	}
}
