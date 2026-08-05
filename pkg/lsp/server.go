// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"
	"net/url"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	glspserver "github.com/tliron/glsp/server"
)

// Server is a language server for solution files. It keeps a per-document
// parse+index cache for each open document and republishes lint diagnostics on
// open/change/save.
type Server struct {
	binaryName string
	version    string
	registry   *provider.Registry

	docs *DocumentCache
}

// NewServer constructs a language server. binaryName labels diagnostics and the
// server info; version is reported to the client (may be empty). registry is
// used by lint for provider input validation.
func NewServer(binaryName, version string, registry *provider.Registry) *Server {
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}
	return &Server{
		binaryName: binaryName,
		version:    version,
		registry:   registry,
		docs:       NewDocumentCache(),
	}
}

// Run starts the server over stdio and blocks until the client disconnects.
func (s *Server) Run() error {
	srv := glspserver.NewServer(s.Handler(), s.binaryName+"-lsp", false)
	return srv.RunStdio()
}

// Handler builds the glsp protocol handler wired to this server's callbacks.
func (s *Server) Handler() *protocol.Handler {
	handler := &protocol.Handler{}

	handler.Initialize = func(_ *glsp.Context, _ *protocol.InitializeParams) (any, error) {
		capabilities := handler.CreateServerCapabilities()
		// Use full-document sync so each change carries the whole text; the
		// server does not need to apply incremental patches.
		if sync, ok := capabilities.TextDocumentSync.(*protocol.TextDocumentSyncOptions); ok {
			full := protocol.TextDocumentSyncKindFull
			sync.Change = &full
		}
		// Advertise rename with prepare support so clients validate the cursor
		// position before prompting for a new name.
		prepare := true
		capabilities.RenameProvider = &protocol.RenameOptions{PrepareProvider: &prepare}
		result := protocol.InitializeResult{
			Capabilities: capabilities,
			ServerInfo: &protocol.InitializeResultServerInfo{
				Name: s.binaryName + " lsp",
			},
		}
		if s.version != "" {
			result.ServerInfo.Version = &s.version
		}
		return result, nil
	}
	handler.Initialized = func(_ *glsp.Context, _ *protocol.InitializedParams) error { return nil }
	handler.Shutdown = func(_ *glsp.Context) error { return nil }
	handler.SetTrace = func(_ *glsp.Context, _ *protocol.SetTraceParams) error { return nil }
	// Accept and ignore client cancellations. We do not support mid-flight
	// request cancellation (handlers are fast and synchronous), but
	// $/cancelRequest is an optional notification the client sends frequently
	// (e.g. while typing). Registering a no-op handler avoids logging an
	// "unsupported method" error for each one; the spec allows a server that
	// does not cancel to simply complete the request normally.
	handler.CancelRequest = func(_ *glsp.Context, _ *protocol.CancelParams) error { return nil }

	handler.TextDocumentDidOpen = s.didOpen
	handler.TextDocumentDidChange = s.didChange
	handler.TextDocumentDidSave = s.didSave
	handler.TextDocumentDidClose = s.didClose

	handler.TextDocumentDefinition = s.definition
	handler.TextDocumentReferences = s.references
	handler.TextDocumentPrepareRename = s.prepareRename
	handler.TextDocumentRename = s.rename

	return handler
}

func (s *Server) didOpen(ctx *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	s.setDoc(params.TextDocument.URI, params.TextDocument.Version, params.TextDocument.Text)
	s.publish(ctx, params.TextDocument.URI)
	return nil
}

func (s *Server) didChange(ctx *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	updated := false
	for _, change := range params.ContentChanges {
		switch c := change.(type) {
		case protocol.TextDocumentContentChangeEventWhole:
			s.setDoc(params.TextDocument.URI, params.TextDocument.Version, c.Text)
			updated = true
		case protocol.TextDocumentContentChangeEvent:
			// Full sync: a rangeless change carries the whole document. glsp
			// normally maps these to the Whole type; handle the raw form too.
			if c.Range == nil {
				s.setDoc(params.TextDocument.URI, params.TextDocument.Version, c.Text)
				updated = true
			}
		}
	}
	// Only republish when the stored content actually changed. A client that
	// ignores the negotiated full-sync and sends ranged changes would otherwise
	// trigger a redundant publish of diagnostics for unchanged content.
	if updated {
		s.publish(ctx, params.TextDocument.URI)
	}
	return nil
}

func (s *Server) didSave(ctx *glsp.Context, params *protocol.DidSaveTextDocumentParams) error {
	if params.Text != nil {
		// DidSave carries no document version; reuse the version of the cached
		// entry (the save reflects the last synced content).
		var version int32
		if entry, ok := s.getDoc(params.TextDocument.URI); ok {
			version = entry.Version
		}
		s.setDoc(params.TextDocument.URI, version, *params.Text)
	}
	s.publish(ctx, params.TextDocument.URI)
	return nil
}

func (s *Server) didClose(ctx *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	s.deleteDoc(params.TextDocument.URI)
	// Clear diagnostics for the closed document.
	ctx.Notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         params.TextDocument.URI,
		Diagnostics: []protocol.Diagnostic{},
	})
	return nil
}

func (s *Server) definition(_ *glsp.Context, params *protocol.DefinitionParams) (any, error) {
	entry, ok := s.getDoc(params.TextDocument.URI)
	if !ok || entry.Index == nil {
		return nil, nil
	}
	if loc := definitionFromIndex(entry.Index, params.TextDocument.URI, params.Position); loc != nil {
		return *loc, nil
	}
	return nil, nil
}

func (s *Server) references(_ *glsp.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	entry, ok := s.getDoc(params.TextDocument.URI)
	if !ok || entry.Index == nil {
		return nil, nil
	}
	return referencesFromIndex(entry.Index, params.TextDocument.URI, params.Position, params.Context.IncludeDeclaration), nil
}

func (s *Server) prepareRename(_ *glsp.Context, params *protocol.PrepareRenameParams) (any, error) {
	entry, ok := s.getDoc(params.TextDocument.URI)
	if !ok || entry.Index == nil {
		return nil, nil
	}
	if rng := prepareRenameFromIndex(entry.Index, params.Position); rng != nil {
		return *rng, nil
	}
	return nil, nil
}

func (s *Server) rename(_ *glsp.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	entry, ok := s.getDoc(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	if entry.ParseErr != nil {
		return nil, fmt.Errorf("failed to parse solution: %w", entry.ParseErr)
	}
	if entry.Sol == nil || entry.Index == nil {
		return nil, fmt.Errorf("solution is not indexed")
	}
	return renameFromIndex(entry.Sol, entry.Index, params.TextDocument.URI, params.Position, params.NewName)
}

// publish computes and sends diagnostics for the current content of uri. When
// the document parsed cleanly it reuses the cached solution (no re-parse); a
// parse failure falls back to the raw-content path, which reports the parse
// error itself.
func (s *Server) publish(ctx *glsp.Context, uri protocol.DocumentUri) {
	entry, ok := s.getDoc(uri)
	if !ok {
		return
	}
	var diags []protocol.Diagnostic
	if entry.Sol != nil {
		diags = diagnosticsFromSolution(entry.Sol, entry.Raw, uriToPath(uri), s.binaryName, s.registry)
	} else {
		diags = Diagnostics(entry.Raw, uriToPath(uri), s.binaryName, s.registry)
	}
	ctx.Notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	})
}

func (s *Server) setDoc(uri protocol.DocumentUri, version int32, content string) {
	s.docs.Set(uri, version, content)
}

func (s *Server) getDoc(uri protocol.DocumentUri) (*DocEntry, bool) {
	return s.docs.Get(uri)
}

func (s *Server) deleteDoc(uri protocol.DocumentUri) {
	s.docs.Delete(uri)
}

// uriToPath converts a file:// document URI to a filesystem path, falling back
// to the raw string for non-file URIs.
func uriToPath(uri protocol.DocumentUri) string {
	u, err := url.Parse(string(uri))
	if err != nil || u.Scheme != "file" {
		return string(uri)
	}
	return u.Path
}
