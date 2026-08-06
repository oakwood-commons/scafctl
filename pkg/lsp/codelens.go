// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"encoding/json"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Client command ids emitted in code-lens commands. "N references" uses the
// editor's built-in show-references command (locations computed server-side);
// Run and Preview are handled by the VS Code extension, which spawns the CLI
// with the server-provided argument list. Keeping the exact CLI arguments on the
// server side (not the extension) keeps that logic testable in Go.
const (
	cmdShowReferences = "editor.action.showReferences"
	cmdRun            = "scafctl.run"
	cmdPreview        = "scafctl.preview"
)

// lensData is carried on an unresolved "N references" code lens so codeLens/resolve
// can recompute the reference set for the target symbol without re-scanning.
type lensData struct {
	URI  protocol.DocumentUri `json:"uri"`
	Kind string               `json:"kind"` // "resolver" | "action"
	Name string               `json:"name"`
}

// codeLensFeature registers textDocument/codeLens (+ codeLens/resolve) and
// advertises the provider with resolve support so the expensive "N references"
// title/locations are deferred to resolve.
func codeLensFeature() feature {
	return feature{
		name: "codeLens",
		wire: func(h *protocol.Handler, s *Server) {
			h.TextDocumentCodeLens = s.codeLens
			h.CodeLensResolve = s.codeLensResolve
		},
		advertise: func(c *protocol.ServerCapabilities) {
			resolve := true
			c.CodeLensProvider = &protocol.CodeLensOptions{ResolveProvider: &resolve}
		},
	}
}

// codeLens returns the lenses for a document: above each resolver and action
// definition, a "N references" lens (title deferred to resolve), a "Run" lens,
// and a "Preview output" lens. It returns nothing when the document has no
// built index (parse error / unknown document).
func (s *Server) codeLens(_ *glsp.Context, params *protocol.CodeLensParams) ([]protocol.CodeLens, error) {
	entry, ok := s.getDoc(params.TextDocument.URI)
	if !ok || entry.Index == nil {
		return nil, nil
	}
	return codeLenses(entry.Index, params.TextDocument.URI), nil
}

// codeLenses builds the lenses for every resolver and action definition in idx.
func codeLenses(idx *refindex.Index, uri protocol.DocumentUri) []protocol.CodeLens {
	var lenses []protocol.CodeLens
	for _, sk := range []struct {
		kind refindex.SymbolKind
		name string
	}{
		{refindex.SymbolResolver, "resolver"},
		{refindex.SymbolAction, "action"},
	} {
		for _, name := range idx.Names(sk.kind) {
			def, ok := idx.Definition(sk.kind, name)
			if !ok {
				continue
			}
			rng := toLSPRange(def.Range)
			lenses = append(lenses,
				// Unresolved reference-count lens (title + locations filled by resolve).
				protocol.CodeLens{
					Range: rng,
					Data:  lensData{URI: uri, Kind: sk.name, Name: name},
				},
				// Run lens.
				protocol.CodeLens{
					Range: rng,
					Command: &protocol.Command{
						Title:     "Run",
						Command:   cmdRun,
						Arguments: []any{uri, runArgs(sk.name, name)},
					},
				},
				// Preview-output lens.
				protocol.CodeLens{
					Range: rng,
					Command: &protocol.Command{
						Title:     "Preview output",
						Command:   cmdPreview,
						Arguments: []any{uri, previewArgs(sk.name, name)},
					},
				},
			)
		}
	}
	return lenses
}

// codeLensResolve fills the "N references" lens: it recomputes the reference set
// for the lens's target symbol and wires the built-in show-references command.
// A lens that already has a command (Run/Preview) is returned unchanged.
func (s *Server) codeLensResolve(_ *glsp.Context, lens *protocol.CodeLens) (*protocol.CodeLens, error) {
	if lens == nil {
		return lens, nil
	}
	if lens.Command != nil {
		return lens, nil // already resolved
	}
	data, ok := decodeLensData(lens.Data)
	if !ok {
		return lens, nil
	}
	entry, ok := s.getDoc(data.URI)
	if !ok || entry.Index == nil {
		return lens, nil
	}
	kind, ok := symbolKindFor(data.Kind)
	if !ok {
		return lens, nil
	}

	refs := entry.Index.References(kind, data.Name)
	locations := make([]protocol.Location, 0, len(refs))
	for _, r := range refs {
		locations = append(locations, protocol.Location{URI: data.URI, Range: toLSPRange(r.Range)})
	}

	lens.Command = &protocol.Command{
		Title:   referenceTitle(len(refs)),
		Command: cmdShowReferences,
		// VS Code's showReferences expects (uri, position, locations).
		Arguments: []any{data.URI, lens.Range.Start, locations},
	}
	return lens, nil
}

// runArgs returns the CLI arguments (excluding the -f file flag, which the
// extension appends) to run a resolver/action.
func runArgs(kind, name string) []string {
	return []string{"run", kind, name}
}

// previewArgs returns the CLI arguments to preview a resolver/action's output.
// Resolvers are side-effect-free, so running one is already a preview; actions
// add --dry-run so no side effects occur.
func previewArgs(kind, name string) []string {
	args := []string{"run", kind, name}
	if kind == "action" {
		args = append(args, "--dry-run")
	}
	return args
}

// referenceTitle renders the reference-count lens title with correct pluralization.
func referenceTitle(n int) string {
	if n == 1 {
		return "1 reference"
	}
	return fmt.Sprintf("%d references", n)
}

// symbolKindFor maps a lensData kind string to a refindex.SymbolKind.
func symbolKindFor(kind string) (refindex.SymbolKind, bool) {
	switch kind {
	case "resolver":
		return refindex.SymbolResolver, true
	case "action":
		return refindex.SymbolAction, true
	default:
		return 0, false
	}
}

// decodeLensData recovers a lensData from a code lens's Data field, which may be
// the original struct (in-process) or a JSON-decoded map (round-tripped through
// the client). A JSON round-trip handles both.
func decodeLensData(v any) (lensData, bool) {
	if v == nil {
		return lensData{}, false
	}
	if d, ok := v.(lensData); ok {
		return d, true
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return lensData{}, false
	}
	var d lensData
	if err := json.Unmarshal(raw, &d); err != nil || d.Name == "" || d.Kind == "" {
		return lensData{}, false
	}
	return d, true
}
