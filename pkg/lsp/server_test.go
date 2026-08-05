// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const badSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bad
spec:
  resolvers:
    appName:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: _.doesNotExist
`

// captureContext returns a glsp.Context whose Notify records the last published
// method and params.
func captureContext(method *string, params *protocol.PublishDiagnosticsParams) *glsp.Context {
	return &glsp.Context{
		Notify: func(m string, p any) {
			*method = m
			if pd, ok := p.(protocol.PublishDiagnosticsParams); ok {
				*params = pd
			}
		},
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	reg, err := builtin.DefaultRegistry(context.Background())
	require.NoError(t, err)
	return NewServer("scafctl", "test", reg)
}

func TestServer_DidOpenPublishesDiagnostics(t *testing.T) {
	s := newTestServer(t)
	var method string
	var params protocol.PublishDiagnosticsParams
	ctx := captureContext(&method, &params)

	err := s.didOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: "file:///t.yaml", Text: badSolution},
	})
	require.NoError(t, err)

	assert.Equal(t, string(protocol.ServerTextDocumentPublishDiagnostics), method)
	assert.Equal(t, protocol.DocumentUri("file:///t.yaml"), params.URI)
	assert.NotEmpty(t, params.Diagnostics, "undefined resolver should surface a diagnostic")
}

func TestServer_DidChangeRepublishes(t *testing.T) {
	s := newTestServer(t)
	var method string
	var params protocol.PublishDiagnosticsParams
	ctx := captureContext(&method, &params)

	// Open clean, then change to the bad content.
	require.NoError(t, s.didOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: "file:///t.yaml", Text: "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: ok\nspec:\n  resolvers:\n    a:\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              value: x\n"},
	}))

	err := s.didChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: "file:///t.yaml"},
		},
		ContentChanges: []any{protocol.TextDocumentContentChangeEventWhole{Text: badSolution}},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, params.Diagnostics, "changed content should be re-linted")
}

func TestServer_DidChangeIgnoresRangedChangeUnderFullSync(t *testing.T) {
	s := newTestServer(t)

	// Seed a document via didOpen (uses a throwaway context).
	var openMethod string
	var openParams protocol.PublishDiagnosticsParams
	require.NoError(t, s.didOpen(captureContext(&openMethod, &openParams), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: "file:///t.yaml", Text: badSolution},
	}))

	// A client that ignores the negotiated full-sync and sends a ranged
	// (incremental) change: the server cannot apply it, so it must not update
	// stored content nor republish diagnostics.
	var method string
	var params protocol.PublishDiagnosticsParams
	ctx := captureContext(&method, &params)
	err := s.didChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: "file:///t.yaml"},
		},
		ContentChanges: []any{protocol.TextDocumentContentChangeEvent{
			Range: &protocol.Range{}, Text: "ignored",
		}},
	})
	require.NoError(t, err)
	assert.Empty(t, method, "a ranged change under full-sync must not republish diagnostics")
}

func TestServer_DidCloseClearsDiagnostics(t *testing.T) {
	s := newTestServer(t)
	var method string
	var params protocol.PublishDiagnosticsParams
	ctx := captureContext(&method, &params)

	require.NoError(t, s.didOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: "file:///t.yaml", Text: badSolution},
	}))
	require.NotEmpty(t, params.Diagnostics)

	err := s.didClose(ctx, &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///t.yaml"},
	})
	require.NoError(t, err)
	assert.Equal(t, string(protocol.ServerTextDocumentPublishDiagnostics), method)
	assert.Empty(t, params.Diagnostics, "closing a document clears its diagnostics")

	// The document is forgotten.
	_, ok := s.getDoc("file:///t.yaml")
	assert.False(t, ok)
}

func TestServer_InitializeAdvertisesFullSync(t *testing.T) {
	s := newTestServer(t)
	handler := s.Handler()
	res, err := handler.Initialize(&glsp.Context{}, &protocol.InitializeParams{})
	require.NoError(t, err)

	init, ok := res.(protocol.InitializeResult)
	require.True(t, ok)
	require.NotNil(t, init.ServerInfo)
	assert.Equal(t, "scafctl lsp", init.ServerInfo.Name)

	sync, ok := init.Capabilities.TextDocumentSync.(*protocol.TextDocumentSyncOptions)
	require.True(t, ok)
	require.NotNil(t, sync.Change)
	assert.Equal(t, protocol.TextDocumentSyncKindFull, *sync.Change)
}

func TestServer_CancelRequestIsNoOp(t *testing.T) {
	// $/cancelRequest is an optional notification; the server must accept it
	// (returning nil) rather than leaving the handler nil, which would make glsp
	// respond with "method not supported" and log an error for every client
	// cancellation.
	handler := newTestServer(t).Handler()
	require.NotNil(t, handler.CancelRequest, "CancelRequest handler must be registered")
	assert.NoError(t, handler.CancelRequest(&glsp.Context{}, &protocol.CancelParams{}))
}

func TestNewServer_DefaultsBinaryName(t *testing.T) {
	s := NewServer("", "", nil)
	assert.Equal(t, settings.CliBinaryName, s.binaryName)
}

func TestURIToPath(t *testing.T) {
	assert.Equal(t, "/tmp/solution.yaml", uriToPath("file:///tmp/solution.yaml"))
	assert.Equal(t, "not-a-uri", uriToPath("not-a-uri"))
	// url.Parse decodes percent-escapes into u.Path, so the returned path is a
	// real filesystem path (spaces, not %20).
	assert.Equal(t, "/tmp/my dir/solution.yaml", uriToPath("file:///tmp/my%20dir/solution.yaml"))
}

func TestServer_NavigationHandlers(t *testing.T) {
	s := newTestServer(t)
	uri := protocol.DocumentUri("file:///nav.yaml")
	noop := &glsp.Context{Notify: func(string, any) {}}
	require.NoError(t, s.didOpen(noop, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: uri, Text: navFixture},
	}))

	pos := celRefPosition(t, navFixture)
	tdp := protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}

	def, err := s.definition(nil, &protocol.DefinitionParams{TextDocumentPositionParams: tdp})
	require.NoError(t, err)
	require.NotNil(t, def)

	refs, err := s.references(nil, &protocol.ReferenceParams{
		TextDocumentPositionParams: tdp,
		Context:                    protocol.ReferenceContext{IncludeDeclaration: true},
	})
	require.NoError(t, err)
	assert.Len(t, refs, 3)

	prep, err := s.prepareRename(nil, &protocol.PrepareRenameParams{TextDocumentPositionParams: tdp})
	require.NoError(t, err)
	require.NotNil(t, prep)

	we, err := s.rename(nil, &protocol.RenameParams{TextDocumentPositionParams: tdp, NewName: "env"})
	require.NoError(t, err)
	require.NotNil(t, we)
	assert.Len(t, we.Changes[uri], 3)
}

func TestServer_NavigationHandlersOnUnknownDoc(t *testing.T) {
	s := newTestServer(t)
	tdp := protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///missing.yaml"},
		Position:     protocol.Position{Line: 0, Character: 0},
	}
	def, err := s.definition(nil, &protocol.DefinitionParams{TextDocumentPositionParams: tdp})
	require.NoError(t, err)
	assert.Nil(t, def)

	refs, err := s.references(nil, &protocol.ReferenceParams{TextDocumentPositionParams: tdp})
	require.NoError(t, err)
	assert.Nil(t, refs)

	we, err := s.rename(nil, &protocol.RenameParams{TextDocumentPositionParams: tdp, NewName: "x"})
	require.NoError(t, err)
	assert.Nil(t, we)
}

func TestServer_InitializeAdvertisesNavigationCapabilities(t *testing.T) {
	s := newTestServer(t)
	res, err := s.Handler().Initialize(&glsp.Context{}, &protocol.InitializeParams{})
	require.NoError(t, err)
	init := res.(protocol.InitializeResult)

	assert.Equal(t, true, init.Capabilities.DefinitionProvider)
	assert.Equal(t, true, init.Capabilities.ReferencesProvider)
	rename, ok := init.Capabilities.RenameProvider.(*protocol.RenameOptions)
	require.True(t, ok, "rename should advertise prepare support")
	require.NotNil(t, rename.PrepareProvider)
	assert.True(t, *rename.PrepareProvider)
}
