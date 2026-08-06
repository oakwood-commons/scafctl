// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/refactor"
	glsp "github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Server-side executeCommand command identifiers. These are the commands the
// server declares in its ExecuteCommandProvider and dispatches in
// s.executeCommand -- each applies a concrete WorkspaceEdit. They are distinct
// from the CLIENT-side prompt commands referenced by code actions
// (cmdPromptExtractToCall / cmdPromptAddResolver in codeaction_generate.go),
// which collect user input first and THEN invoke one of these.
const (
	// cmdApplyExtractCall extracts a resolve/transform/validate step into a
	// reusable spec.calls definition. Arguments: [uri, blockPath, callName].
	cmdApplyExtractCall = "scafctl.applyExtractCall"
	// cmdApplyAddResolver inserts a stub resolver under spec.resolvers.
	// Arguments: [uri, resolverName, provider].
	cmdApplyAddResolver = "scafctl.applyAddResolver"
)

// commandFeature wires workspace/executeCommand and advertises the concrete
// server commands the client may invoke. glsp derives ExecuteCommandProvider!=nil
// from the wired handler, but the client needs the explicit Commands list to know
// which commands exist, so this feature computes it.
func commandFeature() feature {
	return feature{
		name: "command",
		wire: func(h *protocol.Handler, s *Server) {
			h.WorkspaceExecuteCommand = s.executeCommand
		},
		advertise: func(c *protocol.ServerCapabilities) {
			c.ExecuteCommandProvider = &protocol.ExecuteCommandOptions{
				Commands: []string{cmdApplyExtractCall, cmdApplyAddResolver},
			}
		},
	}
}

// executeCommand is the single entry point for workspace/executeCommand. It
// dispatches on params.Command to the concrete apply-edit handlers, each of which
// builds a WorkspaceEdit from the cached document and applies it via a
// server->client workspace/applyEdit request. An unknown command is an error
// (the client asked for something the server never advertised).
func (s *Server) executeCommand(ctx *glsp.Context, params *protocol.ExecuteCommandParams) (any, error) {
	switch params.Command {
	case cmdApplyExtractCall:
		return s.applyExtractCall(ctx, params.Arguments)
	case cmdApplyAddResolver:
		return s.applyAddResolver(ctx, params.Arguments)
	default:
		return nil, fmt.Errorf("executeCommand: unknown command %q", params.Command)
	}
}

// applyExtractCall runs refactor.ExtractCall for the [uri, blockPath, callName]
// arguments and applies the resulting edits. All logic lives in the refactor
// engine; this handler only marshals arguments, converts edits, and applies them.
func (s *Server) applyExtractCall(ctx *glsp.Context, args []any) (any, error) {
	uri, blockPath, callName, err := stringArgs3(args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cmdApplyExtractCall, err)
	}
	entry, ok := s.getDoc(protocol.DocumentUri(uri))
	if !ok || entry.Sol == nil {
		return nil, fmt.Errorf("%s: document %q is not open or did not parse", cmdApplyExtractCall, uri)
	}
	result, err := refactor.ExtractCall(entry.Sol, blockPath, callName)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cmdApplyExtractCall, err)
	}
	edit := workspaceEditFor(protocol.DocumentUri(uri), result.Edits)
	if err := s.applyWorkspaceEdit(ctx, edit, "Extract to call"); err != nil {
		return nil, fmt.Errorf("%s: %w", cmdApplyExtractCall, err)
	}
	return nil, nil
}

// applyAddResolver inserts a stub resolver named args[1] using provider args[2]
// under spec.resolvers, and applies the resulting edit.
func (s *Server) applyAddResolver(ctx *glsp.Context, args []any) (any, error) {
	uri, name, provider, err := stringArgs3(args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cmdApplyAddResolver, err)
	}
	// Validate the name server-side before splicing it as a YAML key. The
	// first-party extension validates too, but executeCommand is a public
	// protocol surface any LSP client can call, and an invalid name (whitespace,
	// a colon, a YAML indicator) would corrupt the document -- mirror the
	// name guard refactor.ExtractCall applies to a call name.
	if !resolverNameRe.MatchString(name) {
		return nil, fmt.Errorf("%s: %q is not a valid resolver name (must match %s)", cmdApplyAddResolver, name, refactor.ResolverNamePattern)
	}
	entry, ok := s.getDoc(protocol.DocumentUri(uri))
	if !ok || entry.Raw == nil {
		return nil, fmt.Errorf("%s: document %q is not open", cmdApplyAddResolver, uri)
	}
	stub := resolverStub(name, provider)
	insert, err := refactor.InsertMappingEntry(entry.Raw, "spec.resolvers", stub)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cmdApplyAddResolver, err)
	}
	edit := workspaceEditFor(protocol.DocumentUri(uri), []refactor.TextEdit{insert})
	if err := s.applyWorkspaceEdit(ctx, edit, "Add resolver"); err != nil {
		return nil, fmt.Errorf("%s: %w", cmdApplyAddResolver, err)
	}
	return nil, nil
}

// applyWorkspaceEdit sends a server->client workspace/applyEdit request and
// reports an error when the edit was not applied. It is best-effort about the
// client's response: a client that never answers (or a test harness with no Call
// hook) leaves the edit unconfirmed, which is treated as success rather than a
// hard failure, because the request frame was still emitted.
func (s *Server) applyWorkspaceEdit(ctx *glsp.Context, edit *protocol.WorkspaceEdit, label string) error {
	if edit == nil {
		return fmt.Errorf("apply workspace edit: nil edit")
	}
	// In unit tests (and any transport without a client call channel) Call is
	// nil. There is nothing to send, so treat it as a no-op success; the caller's
	// edit was still constructed and can be asserted on directly.
	if ctx == nil || ctx.Call == nil {
		return nil
	}
	var resp protocol.ApplyWorkspaceEditResponse
	ctx.Call(protocol.ServerWorkspaceApplyEdit, protocol.ApplyWorkspaceEditParams{
		Label: &label,
		Edit:  *edit,
	}, &resp)
	if !resp.Applied {
		return fmt.Errorf("apply workspace edit: client did not apply %q", label)
	}
	return nil
}

// workspaceEditFor converts a slice of refactor.TextEdits into a single-document
// WorkspaceEdit for uri, reusing toLSPRange for coordinate conversion.
func workspaceEditFor(uri protocol.DocumentUri, edits []refactor.TextEdit) *protocol.WorkspaceEdit {
	lspEdits := make([]protocol.TextEdit, 0, len(edits))
	for _, e := range edits {
		lspEdits = append(lspEdits, protocol.TextEdit{Range: toLSPRange(e.Range), NewText: e.NewText})
	}
	return &protocol.WorkspaceEdit{
		Changes: map[protocol.DocumentUri][]protocol.TextEdit{uri: lspEdits},
	}
}

// stringArgs3 asserts that args is exactly three JSON-decoded string arguments
// and returns them. executeCommand arguments arrive as []any of decoded JSON, so
// each element must be a string; a wrong count or type is a client error.
func stringArgs3(args []any) (a, b, c string, err error) {
	if len(args) != 3 {
		return "", "", "", fmt.Errorf("expected 3 string arguments, got %d", len(args))
	}
	a, ok := args[0].(string)
	if !ok {
		return "", "", "", fmt.Errorf("argument 0 must be a string, got %T", args[0])
	}
	b, ok = args[1].(string)
	if !ok {
		return "", "", "", fmt.Errorf("argument 1 must be a string, got %T", args[1])
	}
	c, ok = args[2].(string)
	if !ok {
		return "", "", "", fmt.Errorf("argument 2 must be a string, got %T", args[2])
	}
	return a, b, c, nil
}
