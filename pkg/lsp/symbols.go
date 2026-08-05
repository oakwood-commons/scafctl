// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	glsp "github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// symbolsFeature wires textDocument/documentSymbol so the Outline pane,
// breadcrumbs, and "Go to Symbol in File" list the solution's resolvers,
// actions, calls, and functions. glsp derives the DocumentSymbolProvider
// capability from the wired handler, so no advertise func is needed.
func symbolsFeature() feature {
	return feature{
		name: "symbols",
		wire: func(h *protocol.Handler, s *Server) {
			h.TextDocumentDocumentSymbol = s.documentSymbol
		},
	}
}

// documentSymbol answers textDocument/documentSymbol from the per-document
// cache. It returns nil (no result) when the document is unknown, not indexed,
// or contains no symbols, so an empty or unparseable document yields an empty
// outline rather than an error.
func (s *Server) documentSymbol(_ *glsp.Context, params *protocol.DocumentSymbolParams) (any, error) {
	entry, ok := s.getDoc(params.TextDocument.URI)
	if !ok || entry.Index == nil {
		return nil, nil
	}
	syms := documentSymbolsFromIndex(entry.Index)
	if len(syms) == 0 {
		return nil, nil
	}
	return syms, nil
}

// symbolGroup describes one outline group: the scafctl symbol kind it collects,
// the label shown for the group node, and the protocol.SymbolKind used for its
// leaf symbols. Groups are presented in this fixed order under a single "spec"
// root.
type symbolGroup struct {
	label    string
	kind     refindex.SymbolKind
	leafKind protocol.SymbolKind
}

// symbolGroups is the fixed presentation order of outline groups. Leaf kinds are
// chosen so editors render distinguishable icons: resolvers as fields, calls as
// methods, actions and author functions as functions.
var symbolGroups = []symbolGroup{
	{label: "resolvers", kind: refindex.SymbolResolver, leafKind: protocol.SymbolKindField},
	{label: "actions", kind: refindex.SymbolAction, leafKind: protocol.SymbolKindFunction},
	{label: "calls", kind: refindex.SymbolCall, leafKind: protocol.SymbolKindMethod},
	{label: "functions", kind: refindex.SymbolFunction, leafKind: protocol.SymbolKindFunction},
}

// DocumentSymbols parses solution content and returns its document-symbol
// hierarchy. A parse error yields nil (an empty outline), never a panic.
func DocumentSymbols(content []byte) []protocol.DocumentSymbol {
	_, idx, err := loadIndex(content)
	if err != nil {
		return nil
	}
	return documentSymbolsFromIndex(idx)
}

// documentSymbolsFromIndex builds the document-symbol hierarchy from an
// already-built index, allowing the server to reuse the per-document cache
// instead of re-parsing on every request.
//
// The shape is a single "spec" root whose children are the non-empty groups
// (resolvers, actions, calls, functions), each of which lists its symbols as
// children. Empty groups are omitted; when the solution has no symbols at all,
// the result is nil (an empty outline). Group and root ranges enclose their
// children so cursor-in-symbol reveal works at every level.
func documentSymbolsFromIndex(idx *refindex.Index) []protocol.DocumentSymbol {
	if idx == nil {
		return nil
	}

	groups := make([]protocol.DocumentSymbol, 0, len(symbolGroups))
	for _, g := range symbolGroups {
		names := idx.Names(g.kind)
		leaves := make([]protocol.DocumentSymbol, 0, len(names))
		for _, name := range names {
			def, ok := idx.Definition(g.kind, name)
			if !ok {
				continue
			}
			r := toLSPRange(def.Range)
			leaves = append(leaves, protocol.DocumentSymbol{
				Name:           name,
				Kind:           g.leafKind,
				Range:          r,
				SelectionRange: r,
			})
		}
		if len(leaves) == 0 {
			continue
		}
		// Present leaves in source order so the outline mirrors the file.
		sort.SliceStable(leaves, func(i, j int) bool {
			return lessLSPRange(leaves[i].Range, leaves[j].Range)
		})
		gr := enclosingRange(leaves)
		groups = append(groups, protocol.DocumentSymbol{
			Name:           g.label,
			Kind:           protocol.SymbolKindNamespace,
			Range:          gr,
			SelectionRange: gr,
			Children:       leaves,
		})
	}

	if len(groups) == 0 {
		return nil
	}
	specRange := enclosingRange(groups)
	return []protocol.DocumentSymbol{{
		Name:           "spec",
		Kind:           protocol.SymbolKindNamespace,
		Range:          specRange,
		SelectionRange: specRange,
		Children:       groups,
	}}
}

// enclosingRange returns the smallest range spanning every symbol's range. It
// assumes syms is non-empty (callers guard this).
func enclosingRange(syms []protocol.DocumentSymbol) protocol.Range {
	out := syms[0].Range
	for _, s := range syms[1:] {
		if lessLSPPosition(s.Range.Start, out.Start) {
			out.Start = s.Range.Start
		}
		if lessLSPPosition(out.End, s.Range.End) {
			out.End = s.Range.End
		}
	}
	return out
}

// lessLSPRange orders ranges by start position, then end position.
func lessLSPRange(a, b protocol.Range) bool {
	if a.Start != b.Start {
		return lessLSPPosition(a.Start, b.Start)
	}
	return lessLSPPosition(a.End, b.End)
}

// lessLSPPosition reports whether a precedes b in a document.
func lessLSPPosition(a, b protocol.Position) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Character < b.Character
}
