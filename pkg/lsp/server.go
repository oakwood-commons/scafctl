// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"net/url"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	glspserver "github.com/tliron/glsp/server"
)

// Server is a language server for solution files. It keeps an in-memory copy of
// each open document and republishes lint diagnostics on open/change/save.
type Server struct {
	binaryName string
	version    string
	registry   *provider.Registry

	mu   sync.RWMutex
	docs map[protocol.DocumentUri]string
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
		docs:       make(map[protocol.DocumentUri]string),
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

	handler.TextDocumentDidOpen = s.didOpen
	handler.TextDocumentDidChange = s.didChange
	handler.TextDocumentDidSave = s.didSave
	handler.TextDocumentDidClose = s.didClose

	return handler
}

func (s *Server) didOpen(ctx *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	s.setDoc(params.TextDocument.URI, params.TextDocument.Text)
	s.publish(ctx, params.TextDocument.URI)
	return nil
}

func (s *Server) didChange(ctx *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	for _, change := range params.ContentChanges {
		switch c := change.(type) {
		case protocol.TextDocumentContentChangeEventWhole:
			s.setDoc(params.TextDocument.URI, c.Text)
		case protocol.TextDocumentContentChangeEvent:
			// Full sync: a rangeless change carries the whole document. glsp
			// normally maps these to the Whole type; handle the raw form too.
			if c.Range == nil {
				s.setDoc(params.TextDocument.URI, c.Text)
			}
		}
	}
	s.publish(ctx, params.TextDocument.URI)
	return nil
}

func (s *Server) didSave(ctx *glsp.Context, params *protocol.DidSaveTextDocumentParams) error {
	if params.Text != nil {
		s.setDoc(params.TextDocument.URI, *params.Text)
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

// publish computes and sends diagnostics for the current content of uri.
func (s *Server) publish(ctx *glsp.Context, uri protocol.DocumentUri) {
	content, ok := s.getDoc(uri)
	if !ok {
		return
	}
	diags := Diagnostics([]byte(content), uriToPath(uri), s.binaryName, s.registry)
	ctx.Notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	})
}

func (s *Server) setDoc(uri protocol.DocumentUri, content string) {
	s.mu.Lock()
	s.docs[uri] = content
	s.mu.Unlock()
}

func (s *Server) getDoc(uri protocol.DocumentUri) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.docs[uri]
	return c, ok
}

func (s *Server) deleteDoc(uri protocol.DocumentUri) {
	s.mu.Lock()
	delete(s.docs, uri)
	s.mu.Unlock()
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
