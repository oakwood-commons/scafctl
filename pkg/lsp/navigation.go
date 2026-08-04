// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/refactor"
	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// loadIndex parses solution content and builds a positioned reference index.
func loadIndex(content []byte) (*solution.Solution, *refindex.Index, error) {
	sol := &solution.Solution{}
	if err := sol.UnmarshalFromBytes(content); err != nil {
		return nil, nil, err
	}
	idx, err := refindex.Build(sol)
	if err != nil {
		return nil, nil, err
	}
	return sol, idx, nil
}

// symbolAt returns the reference occurrence under the given LSP position, if any.
func symbolAt(idx *refindex.Index, pos protocol.Position) (refindex.Reference, bool) {
	for _, r := range idx.All() {
		if rangeContains(r.Range, pos) {
			return r, true
		}
	}
	return refindex.Reference{}, false
}

// Definition returns the definition location for the symbol under pos, or nil if
// there is no renameable symbol there. Parse errors yield no result (nil).
func Definition(content []byte, uri protocol.DocumentUri, pos protocol.Position) *protocol.Location {
	_, idx, err := loadIndex(content)
	if err != nil {
		return nil
	}
	ref, ok := symbolAt(idx, pos)
	if !ok {
		return nil
	}
	def, ok := idx.Definition(ref.Symbol.Kind, ref.Symbol.Name)
	if !ok {
		return nil
	}
	return &protocol.Location{URI: uri, Range: toLSPRange(def.Range)}
}

// References returns all reference locations for the symbol under pos. When
// includeDeclaration is true the definition is included. Returns an empty slice
// when there is no symbol under the cursor.
func References(content []byte, uri protocol.DocumentUri, pos protocol.Position, includeDeclaration bool) []protocol.Location {
	_, idx, err := loadIndex(content)
	if err != nil {
		return []protocol.Location{}
	}
	ref, ok := symbolAt(idx, pos)
	if !ok {
		return []protocol.Location{}
	}

	var refs []refindex.Reference
	if includeDeclaration {
		refs = idx.Occurrences(ref.Symbol.Kind, ref.Symbol.Name)
	} else {
		refs = idx.References(ref.Symbol.Kind, ref.Symbol.Name)
	}

	locs := make([]protocol.Location, 0, len(refs))
	for _, r := range refs {
		locs = append(locs, protocol.Location{URI: uri, Range: toLSPRange(r.Range)})
	}
	return locs
}

// PrepareRename returns the range of the renameable symbol under pos, or nil if
// the cursor is not on a renameable symbol (the client then blocks the rename).
func PrepareRename(content []byte, pos protocol.Position) *protocol.Range {
	_, idx, err := loadIndex(content)
	if err != nil {
		return nil
	}
	ref, ok := symbolAt(idx, pos)
	if !ok {
		return nil
	}
	rng := toLSPRange(ref.Range)
	return &rng
}

// Rename computes a WorkspaceEdit renaming the symbol under pos to newName. It
// returns an error (surfaced to the client) when the cursor is not on a symbol,
// or when the rename is rejected (invalid name, collision, or an unlocatable
// reference that would make the rename partial).
func Rename(content []byte, uri protocol.DocumentUri, pos protocol.Position, newName string) (*protocol.WorkspaceEdit, error) {
	sol, idx, err := loadIndex(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse solution: %w", err)
	}
	ref, ok := symbolAt(idx, pos)
	if !ok {
		return nil, fmt.Errorf("no renameable symbol at the cursor position")
	}

	result, err := refactor.RenameSymbol(sol, ref.Symbol.Kind, ref.Symbol.Name, newName)
	if err != nil {
		return nil, err
	}

	edits := make([]protocol.TextEdit, 0, len(result.Edits))
	for _, e := range result.Edits {
		edits = append(edits, protocol.TextEdit{Range: toLSPRange(e.Range), NewText: e.NewText})
	}
	return &protocol.WorkspaceEdit{
		Changes: map[protocol.DocumentUri][]protocol.TextEdit{uri: edits},
	}, nil
}

// rangeContains reports whether the 0-based LSP position falls within the
// 1-based source range. References occupy a single line, so containment is
// checked on that line with an inclusive end (the cursor may sit just after the
// last character).
func rangeContains(r sourcepos.Range, pos protocol.Position) bool {
	startLine := lspLine(r.Start.Line)
	if pos.Line != startLine {
		return false
	}
	return pos.Character >= lspChar(r.Start.Column) && pos.Character <= lspChar(r.End.Column)
}

func toLSPPosition(p sourcepos.Position) protocol.Position {
	return protocol.Position{Line: lspLine(p.Line), Character: lspChar(p.Column)}
}

func toLSPRange(r sourcepos.Range) protocol.Range {
	return protocol.Range{Start: toLSPPosition(r.Start), End: toLSPPosition(r.End)}
}

// lspLine/lspChar convert 1-based source line/column to 0-based LSP coordinates,
// clamping at zero.
func lspLine(line int) uint32 {
	if line <= 1 {
		return 0
	}
	return uint32(line - 1) //nolint:gosec // line-1 is a small positive int after the <=1 guard
}

func lspChar(col int) uint32 {
	if col <= 1 {
		return 0
	}
	return uint32(col - 1) //nolint:gosec // col-1 is a small positive int after the <=1 guard
}
