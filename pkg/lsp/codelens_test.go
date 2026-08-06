// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const codeLensFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: c
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    appName:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: _.environment
  workflow:
    actions:
      deploy:
        provider: debug
        dependsOn:
          - appName
`

const codeLensURI = protocol.DocumentUri("file:///cl.yaml")

func lensesFor(t *testing.T, content string) (*Server, []protocol.CodeLens) {
	t.Helper()
	s := newTestServer(t)
	s.setDoc(codeLensURI, 1, content)
	lenses, err := s.codeLens(nil, &protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: codeLensURI},
	})
	require.NoError(t, err)
	return s, lenses
}

// lensTitles returns the resolved command titles (resolving any deferred lens).
func lensTitles(t *testing.T, s *Server, lenses []protocol.CodeLens) []string {
	t.Helper()
	out := make([]string, 0, len(lenses))
	for i := range lenses {
		l := lenses[i]
		if l.Command == nil {
			resolved, err := s.codeLensResolve(nil, &l)
			require.NoError(t, err)
			require.NotNil(t, resolved.Command)
			out = append(out, resolved.Command.Title)
			continue
		}
		out = append(out, l.Command.Title)
	}
	return out
}

func TestCodeLens_PlacementPerSymbol(t *testing.T) {
	s, lenses := lensesFor(t, codeLensFixture)
	// 3 symbols (environment, appName, deploy) x 3 lenses each.
	assert.Len(t, lenses, 9)

	titles := lensTitles(t, s, lenses)
	// Each symbol contributes one references lens, a Run, and a Preview output.
	assert.Equal(t, 3, countTitle(titles, "Run"))
	assert.Equal(t, 3, countTitle(titles, "Preview output"))
	// Reference-count titles vary; there should be 3 of them.
	refCount := 0
	for _, tt := range titles {
		if tt == "1 reference" || tt == "0 references" {
			refCount++
		}
	}
	assert.Equal(t, 3, refCount)
}

func TestCodeLens_ReferenceCountResolves(t *testing.T) {
	s, lenses := lensesFor(t, codeLensFixture)
	// environment is referenced once (appName's _.environment).
	title, cmd := resolveRefLensFor(t, s, lenses, "environment")
	assert.Equal(t, "1 reference", title)
	assert.Equal(t, cmdShowReferences, cmd.Command)
	// showReferences args: [uri, position, locations].
	require.Len(t, cmd.Arguments, 3)
	assert.Equal(t, codeLensURI, cmd.Arguments[0])
	locs, ok := cmd.Arguments[2].([]protocol.Location)
	require.True(t, ok)
	assert.Len(t, locs, 1)

	// appName has no references.
	title, _ = resolveRefLensFor(t, s, lenses, "appName")
	assert.Equal(t, "0 references", title)
}

func TestCodeLens_RunAndPreviewCommands(t *testing.T) {
	_, lenses := lensesFor(t, codeLensFixture)

	run := commandLensFor(t, lenses, "environment", cmdRun)
	assert.Equal(t, "Run", run.Title)
	assert.Equal(t, []any{codeLensURI, []string{"run", "resolver", "environment"}}, run.Arguments)

	// A resolver's preview runs the resolver (side-effect free), no --dry-run.
	prev := commandLensFor(t, lenses, "environment", cmdPreview)
	assert.Equal(t, []any{codeLensURI, []string{"run", "resolver", "environment"}}, prev.Arguments)

	// An action's preview adds --dry-run.
	actionPrev := commandLensFor(t, lenses, "deploy", cmdPreview)
	assert.Equal(t, []any{codeLensURI, []string{"run", "action", "deploy", "--dry-run"}}, actionPrev.Arguments)

	actionRun := commandLensFor(t, lenses, "deploy", cmdRun)
	assert.Equal(t, []any{codeLensURI, []string{"run", "action", "deploy"}}, actionRun.Arguments)
}

func TestCodeLens_UnknownDocument(t *testing.T) {
	s := newTestServer(t)
	lenses, err := s.codeLens(nil, &protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///missing.yaml"},
	})
	require.NoError(t, err)
	assert.Nil(t, lenses)
}

func TestCodeLens_ParseErrorNoLenses(t *testing.T) {
	s := newTestServer(t)
	s.setDoc(codeLensURI, 1, "spec: {resolvers:\n  broken")
	lenses, err := s.codeLens(nil, &protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: codeLensURI},
	})
	require.NoError(t, err)
	assert.Empty(t, lenses)
}

func TestCodeLensResolve_AlreadyResolvedUnchanged(t *testing.T) {
	s := newTestServer(t)
	cmd := &protocol.Command{Title: "Run", Command: cmdRun}
	lens := &protocol.CodeLens{Command: cmd}
	got, err := s.codeLensResolve(nil, lens)
	require.NoError(t, err)
	assert.Same(t, cmd, got.Command, "a lens with a command is returned unchanged")
}

func TestCodeLensResolve_BadDataUnchanged(t *testing.T) {
	s := newTestServer(t)
	lens := &protocol.CodeLens{Data: "not-a-lensData"}
	got, err := s.codeLensResolve(nil, lens)
	require.NoError(t, err)
	assert.Nil(t, got.Command)

	// nil lens is safe.
	assert.NotPanics(t, func() { _, _ = s.codeLensResolve(nil, nil) })
}

func TestCodeLens_FeatureRegisteredAndAdvertised(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()
	assert.NotNil(t, h.TextDocumentCodeLens)
	assert.NotNil(t, h.CodeLensResolve)

	res, err := h.Initialize(nil, &protocol.InitializeParams{})
	require.NoError(t, err)
	init := res.(protocol.InitializeResult)
	require.NotNil(t, init.Capabilities.CodeLensProvider)
	require.NotNil(t, init.Capabilities.CodeLensProvider.ResolveProvider)
	assert.True(t, *init.Capabilities.CodeLensProvider.ResolveProvider)
}

func TestDecodeLensData(t *testing.T) {
	orig := lensData{URI: "file:///x.yaml", Kind: "resolver", Name: "a"}

	// In-process struct.
	got, ok := decodeLensData(orig)
	require.True(t, ok)
	assert.Equal(t, orig, got)

	// JSON-decoded map (client round-trip).
	got, ok = decodeLensData(map[string]any{"uri": "file:///x.yaml", "kind": "action", "name": "b"})
	require.True(t, ok)
	assert.Equal(t, lensData{URI: "file:///x.yaml", Kind: "action", Name: "b"}, got)

	// Missing fields / nil.
	_, ok = decodeLensData(map[string]any{"uri": "x"})
	assert.False(t, ok)
	_, ok = decodeLensData(nil)
	assert.False(t, ok)
}

func TestCodeLensHelpers(t *testing.T) {
	assert.Equal(t, []string{"run", "resolver", "r"}, runArgs("resolver", "r"))
	assert.Equal(t, []string{"run", "action", "a"}, runArgs("action", "a"))
	assert.Equal(t, []string{"run", "resolver", "r"}, previewArgs("resolver", "r"))
	assert.Equal(t, []string{"run", "action", "a", "--dry-run"}, previewArgs("action", "a"))

	assert.Equal(t, "1 reference", referenceTitle(1))
	assert.Equal(t, "0 references", referenceTitle(0))
	assert.Equal(t, "3 references", referenceTitle(3))

	k, ok := symbolKindFor("resolver")
	assert.True(t, ok)
	assert.Equal(t, refindex.SymbolResolver, k)
	k, ok = symbolKindFor("action")
	assert.True(t, ok)
	assert.Equal(t, refindex.SymbolAction, k)
	_, ok = symbolKindFor("nope")
	assert.False(t, ok)
}

// --- helpers ---

func countTitle(titles []string, want string) int {
	n := 0
	for _, t := range titles {
		if t == want {
			n++
		}
	}
	return n
}

// resolveRefLensFor finds the deferred references lens whose data targets name,
// resolves it, and returns its title and command.
func resolveRefLensFor(t *testing.T, s *Server, lenses []protocol.CodeLens, name string) (string, *protocol.Command) {
	t.Helper()
	for i := range lenses {
		l := lenses[i]
		if l.Command != nil {
			continue
		}
		d, ok := decodeLensData(l.Data)
		if !ok || d.Name != name {
			continue
		}
		resolved, err := s.codeLensResolve(nil, &l)
		require.NoError(t, err)
		require.NotNil(t, resolved.Command)
		return resolved.Command.Title, resolved.Command
	}
	t.Fatalf("no references lens for %q", name)
	return "", nil
}

// commandLensFor finds a lens whose command id matches and whose args target name.
func commandLensFor(t *testing.T, lenses []protocol.CodeLens, name, cmdID string) *protocol.Command {
	t.Helper()
	for i := range lenses {
		l := lenses[i]
		if l.Command == nil || l.Command.Command != cmdID {
			continue
		}
		if args, ok := l.Command.Arguments[1].([]string); ok && len(args) >= 3 && args[2] == name {
			return l.Command
		}
	}
	t.Fatalf("no %s lens for %q", cmdID, name)
	return nil
}
