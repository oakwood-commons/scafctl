// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	glsp "github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// extractDoc has a single provider step extractable into a call.
const extractDoc = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cmd
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
`

// captureApplyEditContext returns a glsp.Context whose Call records the
// ApplyWorkspaceEditParams and answers Applied=true, plus a pointer to the
// captured params.
func captureApplyEditContext(t *testing.T) (*glsp.Context, *protocol.ApplyWorkspaceEditParams) {
	t.Helper()
	captured := &protocol.ApplyWorkspaceEditParams{}
	ctx := &glsp.Context{
		Call: func(method string, params, result any) {
			assert.Equal(t, string(protocol.ServerWorkspaceApplyEdit), method)
			p, ok := params.(protocol.ApplyWorkspaceEditParams)
			require.True(t, ok, "params must be ApplyWorkspaceEditParams")
			*captured = p
			if resp, ok := result.(*protocol.ApplyWorkspaceEditResponse); ok {
				resp.Applied = true
			}
		},
	}
	return ctx, captured
}

func TestExecuteCommand_ApplyExtractCall(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///cmd.yaml"
	openDoc(t, s, uri, extractDoc)

	ctx, captured := captureApplyEditContext(t)
	res, err := s.executeCommand(ctx, &protocol.ExecuteCommandParams{
		Command:   cmdApplyExtractCall,
		Arguments: []any{uri, "spec.resolvers.environment.resolve.with[0]", "getEnv"},
	})
	require.NoError(t, err)
	assert.Nil(t, res)

	require.NotNil(t, captured.Label)
	assert.Equal(t, "Extract to call", *captured.Label)
	edits := captured.Edit.Changes[uri]
	require.NotEmpty(t, edits)

	// The step rewrite to a call reference and the calls insertion are present.
	var joined string
	for _, e := range edits {
		joined += e.NewText + "\n"
	}
	assert.Contains(t, joined, "- call: getEnv")
	assert.Contains(t, joined, "getEnv:")
	assert.Contains(t, joined, "provider: parameter")
}

func TestExecuteCommand_ApplyAddResolver(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///cmd.yaml"
	openDoc(t, s, uri, extractDoc)

	ctx, captured := captureApplyEditContext(t)
	res, err := s.executeCommand(ctx, &protocol.ExecuteCommandParams{
		Command:   cmdApplyAddResolver,
		Arguments: []any{uri, "newRes", "static"},
	})
	require.NoError(t, err)
	assert.Nil(t, res)

	require.NotNil(t, captured.Label)
	assert.Equal(t, "Add resolver", *captured.Label)
	edits := captured.Edit.Changes[uri]
	require.Len(t, edits, 1)
	assert.Contains(t, edits[0].NewText, "newRes:")
	assert.Contains(t, edits[0].NewText, "provider: static")
	// The insertion is zero-width.
	assert.Equal(t, edits[0].Range.Start, edits[0].Range.End)
}

func TestExecuteCommand_AddResolverRejectsInvalidName(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///cmd.yaml"
	openDoc(t, s, uri, extractDoc)

	// executeCommand is a public protocol surface: an invalid resolver name (which
	// the first-party extension would reject client-side) must be refused
	// server-side rather than spliced into the document as a corrupt YAML key.
	ctx, captured := captureApplyEditContext(t)
	for _, bad := range []string{"has space", "a:b", `"lead`, "1num", ""} {
		_, err := s.executeCommand(ctx, &protocol.ExecuteCommandParams{
			Command:   cmdApplyAddResolver,
			Arguments: []any{uri, bad, "static"},
		})
		require.Error(t, err, "name %q must be rejected", bad)
		assert.Contains(t, err.Error(), "not a valid resolver name")
	}
	// No edit was ever applied for a rejected name.
	assert.Nil(t, captured.Edit.Changes)
}

func TestExecuteCommand_UnknownCommand(t *testing.T) {
	s := newTestServer(t)
	_, err := s.executeCommand(&glsp.Context{}, &protocol.ExecuteCommandParams{
		Command: "scafctl.nope",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestExecuteCommand_BadArguments(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///cmd.yaml"
	openDoc(t, s, uri, extractDoc)

	tests := []struct {
		name    string
		command string
		args    []any
	}{
		{"extract wrong count", cmdApplyExtractCall, []any{uri, "path"}},
		{"extract wrong type", cmdApplyExtractCall, []any{uri, 42, "getEnv"}},
		{"add resolver wrong count", cmdApplyAddResolver, []any{uri}},
		{"add resolver wrong type", cmdApplyAddResolver, []any{uri, "n", true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.executeCommand(&glsp.Context{}, &protocol.ExecuteCommandParams{
				Command:   tt.command,
				Arguments: tt.args,
			})
			require.Error(t, err)
		})
	}
}

func TestExecuteCommand_UnknownDocument(t *testing.T) {
	s := newTestServer(t)
	_, err := s.executeCommand(&glsp.Context{}, &protocol.ExecuteCommandParams{
		Command:   cmdApplyAddResolver,
		Arguments: []any{"file:///missing.yaml", "r", "static"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not open")
}

func TestApplyWorkspaceEdit_NilCallIsNoop(t *testing.T) {
	s := newTestServer(t)
	edit := workspaceEditFor("file:///x.yaml", nil)
	// A context with no Call channel (unit tests) is a no-op success.
	require.NoError(t, s.applyWorkspaceEdit(&glsp.Context{}, edit, "label"))
}

func TestApplyWorkspaceEdit_NotAppliedIsError(t *testing.T) {
	s := newTestServer(t)
	ctx := &glsp.Context{
		Call: func(_ string, _, result any) {
			if resp, ok := result.(*protocol.ApplyWorkspaceEditResponse); ok {
				resp.Applied = false
			}
		},
	}
	edit := workspaceEditFor("file:///x.yaml", nil)
	err := s.applyWorkspaceEdit(ctx, edit, "label")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not apply")
}

func TestCommandFeature_AdvertisesCommands(t *testing.T) {
	f := commandFeature()
	require.NotNil(t, f.advertise)

	var caps protocol.ServerCapabilities
	f.advertise(&caps)

	opts := caps.ExecuteCommandProvider
	require.NotNil(t, opts, "ExecuteCommandProvider must be set")
	assert.Equal(t, []string{cmdApplyExtractCall, cmdApplyAddResolver}, opts.Commands)
}

func TestCommandFeature_WiresHandler(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()
	assert.NotNil(t, h.WorkspaceExecuteCommand, "executeCommand handler must be wired")
}
