// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// feature is a self-contained LSP capability registered explicitly in
// NewServer. It has two responsibilities:
//
//   - wire attaches the feature's glsp handler callbacks to the shared
//     *protocol.Handler (e.g. handler.TextDocumentHover = s.hover).
//   - advertise sets any server capability that needs a computed value beyond
//     what glsp's CreateServerCapabilities already derives from the wired
//     handlers (e.g. rename's PrepareProvider, a completion provider's trigger
//     characters, the document-sync kind). It may be nil when the wired handler
//     alone is enough for glsp to advertise the capability.
//
// Registration is explicit (a slice built in NewServer), NOT init()-based:
// implicit package-level registration hides ordering and control flow. This
// table exists only so two developers adding features rarely edit the same line
// -- each feature contributes one alphabetized entry plus its own constructor in
// the feature's file, instead of both editing the Handler()/Initialize wiring.
type feature struct {
	// name identifies the feature for tests and debugging; it does not affect
	// wiring.
	name string
	// wire attaches the feature's handler callbacks. It must be non-nil.
	wire func(h *protocol.Handler, s *Server)
	// advertise sets computed capability values. It may be nil.
	advertise func(c *protocol.ServerCapabilities)
}

// defaultFeatures returns the built-in feature set in a deterministic order.
// Adding a new feature means adding ONE alphabetized entry here plus a
// feature-returning constructor in the feature's own file -- no edits to
// Handler() or the Initialize closure.
func defaultFeatures() []feature {
	return []feature{
		codeActionFeature(),
		documentSyncFeature(),
		navigationFeature(),
		renameFeature(),
		symbolsFeature(),
	}
}

// wireFeatures attaches every feature's handler callbacks to handler.
func (s *Server) wireFeatures(handler *protocol.Handler) {
	for _, f := range s.features {
		f.wire(handler, s)
	}
}

// advertiseFeatures applies every feature's computed capability values to caps.
func (s *Server) advertiseFeatures(caps *protocol.ServerCapabilities) {
	for _, f := range s.features {
		if f.advertise != nil {
			f.advertise(caps)
		}
	}
}

// documentSyncFeature wires the text-document lifecycle notifications (open,
// change, save, close) that keep the per-document cache current and republish
// lint diagnostics, and advertises full-document sync.
func documentSyncFeature() feature {
	return feature{
		name: "documentSync",
		wire: func(h *protocol.Handler, s *Server) {
			h.TextDocumentDidOpen = s.didOpen
			h.TextDocumentDidChange = s.didChange
			h.TextDocumentDidSave = s.didSave
			h.TextDocumentDidClose = s.didClose
		},
		advertise: func(c *protocol.ServerCapabilities) {
			// Use full-document sync so each change carries the whole text; the
			// server does not need to apply incremental patches.
			if sync, ok := c.TextDocumentSync.(*protocol.TextDocumentSyncOptions); ok {
				full := protocol.TextDocumentSyncKindFull
				sync.Change = &full
			}
		},
	}
}

// navigationFeature wires go-to-definition and find-references. glsp derives the
// DefinitionProvider/ReferencesProvider capabilities from the wired handlers, so
// no advertise func is needed.
func navigationFeature() feature {
	return feature{
		name: "navigation",
		wire: func(h *protocol.Handler, s *Server) {
			h.TextDocumentDefinition = s.definition
			h.TextDocumentReferences = s.references
		},
	}
}

// renameFeature wires prepare-rename and rename, and advertises rename with
// prepare support so clients validate the cursor position before prompting for a
// new name (a computed capability, not a plain flag).
func renameFeature() feature {
	return feature{
		name: "rename",
		wire: func(h *protocol.Handler, s *Server) {
			h.TextDocumentPrepareRename = s.prepareRename
			h.TextDocumentRename = s.rename
		},
		advertise: func(c *protocol.ServerCapabilities) {
			prepare := true
			c.RenameProvider = &protocol.RenameOptions{PrepareProvider: &prepare}
		},
	}
}
