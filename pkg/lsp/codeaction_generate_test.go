// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/refactor"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// missingRefDoc references an undefined resolver (_.doesNotExist) so lint emits
// an unknown-resolver-reference finding.
const missingRefDoc = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: gen
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

// codeActionParamsAt builds a code-action request over a line range with no
// incoming diagnostics and no kind filter.
func codeActionParamsAt(uri protocol.DocumentUri, startLine, endLine uint32) *protocol.CodeActionParams {
	return &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range: protocol.Range{
			Start: protocol.Position{Line: startLine, Character: 0},
			End:   protocol.Position{Line: endLine, Character: 40},
		},
		Context: protocol.CodeActionContext{},
	}
}

func TestGenerative_CreateMissingResolver(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///gen.yaml"
	openDoc(t, s, uri, missingRefDoc)
	entry, _ := s.getDoc(uri)

	// The _.doesNotExist reference is on line 11 (0-based).
	params := codeActionParamsAt(uri, 11, 11)
	actions := s.generativeCodeActions(entry, params)

	var create *protocol.CodeAction
	for i := range actions {
		if actions[i].Title == `Create missing resolver "doesNotExist"` {
			create = &actions[i]
		}
	}
	require.NotNil(t, create, "a create-missing-resolver action must be offered")
	require.NotNil(t, create.Kind)
	assert.Equal(t, protocol.CodeActionKindQuickFix, *create.Kind)
	require.NotNil(t, create.Edit, "create-missing-resolver is a direct edit, not a command")
	assert.Nil(t, create.Command)

	edits := create.Edit.Changes[uri]
	require.Len(t, edits, 1)
	assert.Contains(t, edits[0].NewText, "doesNotExist:")
	assert.Contains(t, edits[0].NewText, "provider: static")

	// The action's edit is the same insertion refactor.InsertMappingEntry
	// produces; applying it yields a solution that parses, validates, and
	// resolves the dangling reference.
	insert, err := refactor.InsertMappingEntry(entry.Raw, "spec.resolvers", resolverStub("doesNotExist", "static"))
	require.NoError(t, err)
	out, err := refactor.Apply(entry.Raw, []refactor.TextEdit{insert})
	require.NoError(t, err)
	sol := &solution.Solution{}
	require.NoError(t, sol.UnmarshalFromBytes(out), "applied edit must re-parse")
	require.NoError(t, sol.ValidateSpec())
	require.NotNil(t, sol.Spec.Resolvers["doesNotExist"])
}

func TestGenerative_ExtractToCall(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///gen.yaml"
	openDoc(t, s, uri, extractDoc)
	entry, _ := s.getDoc(uri)

	// The provider step spans lines 9-11 (0-based); target inside it.
	params := codeActionParamsAt(uri, 9, 11)
	actions := s.generativeCodeActions(entry, params)

	var extract *protocol.CodeAction
	for i := range actions {
		if actions[i].Title == "Extract to call..." {
			extract = &actions[i]
		}
	}
	require.NotNil(t, extract, "extract-to-call must be offered inside a provider step")
	require.NotNil(t, extract.Kind)
	assert.Equal(t, protocol.CodeActionKindRefactorExtract, *extract.Kind)
	assert.Nil(t, extract.Edit, "extract-to-call is a command action, no direct edit")
	require.NotNil(t, extract.Command)
	assert.Equal(t, cmdPromptExtractToCall, extract.Command.Command)
	require.Len(t, extract.Command.Arguments, 2)
	assert.Equal(t, string(uri), extract.Command.Arguments[0])
	assert.Equal(t, "spec.resolvers.environment.resolve.with[0]", extract.Command.Arguments[1])
}

func TestGenerative_ExtractToCall_NotOfferedOutsideStep(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///gen.yaml"
	openDoc(t, s, uri, extractDoc)
	entry, _ := s.getDoc(uri)

	// Line 0 (apiVersion) is not inside any step.
	params := codeActionParamsAt(uri, 0, 0)
	actions := s.generativeCodeActions(entry, params)
	for _, a := range actions {
		assert.NotEqual(t, "Extract to call...", a.Title, "no extract action outside a step")
	}
}

// callStepDoc uses a call: step, which is NOT an extractable provider step.
const callStepDoc = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: gen
spec:
  calls:
    fetch:
      provider: message
      inputs:
        message: hi
  resolvers:
    r1:
      resolve:
        with:
          - call: fetch
`

func TestGenerative_ExtractToCall_NotOfferedForCallStep(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///gen.yaml"
	openDoc(t, s, uri, callStepDoc)
	entry, _ := s.getDoc(uri)

	// The call: step is on line 14 (0-based).
	params := codeActionParamsAt(uri, 14, 14)
	actions := s.generativeCodeActions(entry, params)
	for _, a := range actions {
		assert.NotEqual(t, "Extract to call...", a.Title, "a call step is not extractable")
	}
}

func TestGenerative_AddResolver(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///gen.yaml"
	openDoc(t, s, uri, extractDoc)
	entry, _ := s.getDoc(uri)

	params := codeActionParamsAt(uri, 0, 0)
	actions := s.generativeCodeActions(entry, params)

	var add *protocol.CodeAction
	for i := range actions {
		if actions[i].Title == "Add resolver..." {
			add = &actions[i]
		}
	}
	require.NotNil(t, add, "add-resolver must always be offered for a parsed doc")
	require.NotNil(t, add.Kind)
	assert.Equal(t, protocol.CodeActionKindSource, *add.Kind)
	assert.Nil(t, add.Edit)
	require.NotNil(t, add.Command)
	assert.Equal(t, cmdPromptAddResolver, add.Command.Command)
	require.Len(t, add.Command.Arguments, 1)
	assert.Equal(t, string(uri), add.Command.Arguments[0])
}

func TestResolverStub(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantProv string
	}{
		{"n1", "static", "provider: static"},
		{"n2", "parameter", "provider: parameter"},
		{"n3", "", "provider: static"}, // empty defaults to static
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			stub := resolverStub(tt.name, tt.provider)
			assert.Contains(t, stub, tt.name+":")
			assert.Contains(t, stub, tt.wantProv)
			assert.Contains(t, stub, `value: ""`)

			// The stub inserts into a solution cleanly under spec.resolvers.
			base := "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: t\nspec:\n  resolvers:\n    seed:\n      resolve:\n        with:\n          - provider: static\n            inputs:\n              value: x\n"
			edit, err := refactor.InsertMappingEntry([]byte(base), "spec.resolvers", stub)
			require.NoError(t, err)
			out, err := refactor.Apply([]byte(base), []refactor.TextEdit{edit})
			require.NoError(t, err)
			sol := &solution.Solution{}
			require.NoError(t, sol.UnmarshalFromBytes(out))
			require.NoError(t, sol.ValidateSpec())
			require.NotNil(t, sol.Spec.Resolvers[tt.name])
		})
	}
}

func TestMissingResolverName(t *testing.T) {
	assert.Equal(t, "foo", missingResolverName(`reference to undefined resolver "foo"`))
	assert.Equal(t, "my-res_2", missingResolverName(`reference to undefined resolver "my-res_2"`))
	assert.Equal(t, "", missingResolverName("some other message"))
	// A bracket-access reference like _["q\"x"] yields a %q-escaped message; the
	// regex would truncate the name to a backslash-terminated fragment, which is
	// not a valid resolver identifier and must be rejected (no garbage stub).
	assert.Equal(t, "", missingResolverName(`reference to undefined resolver "q\"x"`))
	assert.Equal(t, "", missingResolverName(`reference to undefined resolver "has space"`))
}

func TestGenerative_NilSolutionNoResolverActions(t *testing.T) {
	s := newTestServer(t)
	// A DocEntry with no parsed solution: create-missing-resolver and add-resolver
	// require entry.Sol, so neither is offered.
	entry := &DocEntry{URI: "file:///x.yaml", Raw: []byte("not: valid: yaml: :")}
	params := codeActionParamsAt("file:///x.yaml", 0, 0)
	actions := s.generativeCodeActions(entry, params)
	for _, a := range actions {
		assert.NotEqual(t, "Add resolver...", a.Title)
	}
}
