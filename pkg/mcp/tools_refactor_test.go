// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const refactorFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: mcp-refactor
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    appName:
      dependsOn:
        - environment
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: _.environment
    greeting:
      resolve:
        with:
          - provider: go-template
            inputs:
              template:
                tmpl: "{{ ._.appName }}-{{ ._.environment }}"
`

func writeRefactorFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func callTool(t *testing.T, name string, args map[string]any, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	request := mcp.CallToolRequest{}
	request.Params.Name = name
	request.Params.Arguments = args
	result, err := handler(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, result)
	if result.IsError {
		return result, nil
	}
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(extractText(t, result)), &data))
	return result, data
}

// findResolverRefs and renameResolver bind the generic kind-aware handlers to
// the resolver kind for the resolver-focused tests below.
func findResolverRefs(srv *Server) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return srv.handleFindReferences(ctx, request, refindex.SymbolResolver, "resolver")
	}
}

func renameResolver(srv *Server) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return srv.handleRename(ctx, request, refindex.SymbolResolver, "resolver")
	}
}

func TestHandleFindResolverReferences(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)
	path := writeRefactorFixture(t, refactorFixture)

	_, data := callTool(t, "find_resolver_references",
		map[string]any{"file": path, "resolver": "environment"},
		findResolverRefs(srv))

	assert.Equal(t, true, data["defined"])
	assert.NotNil(t, data["definition"])
	assert.EqualValues(t, 0, data["unresolved"])

	refs, ok := data["references"].([]any)
	require.True(t, ok)
	// dependsOn + CEL + template = 3 uses.
	assert.Len(t, refs, 3)

	origins := map[string]bool{}
	for _, r := range refs {
		origins[r.(map[string]any)["origin"].(string)] = true
	}
	assert.True(t, origins["dependsOn"])
	assert.True(t, origins["cel"])
	assert.True(t, origins["template"])
}

func TestHandleFindResolverReferences_Undefined(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	path := writeRefactorFixture(t, refactorFixture)

	_, data := callTool(t, "find_resolver_references",
		map[string]any{"file": path, "resolver": "nope"},
		findResolverRefs(srv))

	assert.Equal(t, false, data["defined"])
	refs, _ := data["references"].([]any)
	assert.Empty(t, refs)
}

func TestHandleRenameResolver(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	path := writeRefactorFixture(t, refactorFixture)

	_, data := callTool(t, "rename_resolver",
		map[string]any{"file": path, "old_name": "environment", "new_name": "env"},
		renameResolver(srv))

	assert.Equal(t, "environment", data["oldName"])
	assert.Equal(t, "env", data["newName"])

	occ, ok := data["occurrences"].([]any)
	require.True(t, ok)
	// definition + dependsOn + CEL + template = 4.
	assert.Len(t, occ, 4)

	content, ok := data["content"].(string)
	require.True(t, ok)
	assert.NotContains(t, content, "environment")
	assert.Contains(t, content, "env:")
	assert.Contains(t, content, "expr: _.env")
}

func TestHandleRenameResolver_InvalidName(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	path := writeRefactorFixture(t, refactorFixture)

	result, _ := callTool(t, "rename_resolver",
		map[string]any{"file": path, "old_name": "environment", "new_name": "1bad"},
		renameResolver(srv))
	assert.True(t, result.IsError)
	assert.Contains(t, extractText(t, result), "not a valid resolver name")

	// A refused rename on valid arguments is a semantic validation error, not a
	// bad-request-shape INVALID_INPUT -- lock down the code so clients can
	// distinguish the two.
	var te struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractText(t, result)), &te))
	assert.Equal(t, ErrCodeValidationError, te.Code)
}

func TestLoadSolutionRaw_SetsPath(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	path := writeRefactorFixture(t, refactorFixture)

	// The loaded solution must carry its path so SourceMap/Range positions built
	// during parsing get a non-empty file (empty otherwise).
	sol, err := srv.loadSolutionRaw(path, "")
	require.NoError(t, err)
	assert.Equal(t, path, sol.GetPath())

	// A cwd-relative file resolves to the joined path.
	dir := filepath.Dir(path)
	solRel, err := srv.loadSolutionRaw("solution.yaml", dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "solution.yaml"), solRel.GetPath())
}

func TestRenameResolver_IdempotentAnnotation(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	// rename_resolver is read-only with no side effects (it only returns
	// rewritten content), so repeated calls with the same inputs are safe --
	// it must advertise the idempotent hint so clients may cache/retry.
	var found bool
	for _, st := range srv.mcpServer.ListTools() {
		if st.Tool.Name != "rename_resolver" {
			continue
		}
		found = true
		require.NotNil(t, st.Tool.Annotations.IdempotentHint)
		assert.True(t, *st.Tool.Annotations.IdempotentHint)
	}
	assert.True(t, found, "rename_resolver tool must be registered")
}

func TestHandleRenameResolver_RefusesWhenUnresolved(t *testing.T) {
	// A $-rooted template reference to environment is unpositionable, so the
	// rename must refuse.
	fixture := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: unresolved
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    greet:
      resolve:
        with:
          - provider: go-template
            inputs:
              template:
                tmpl: "{{ $.environment }}"
`
	srv, _ := NewServer(WithServerVersion("test"))
	path := writeRefactorFixture(t, fixture)

	result, _ := callTool(t, "rename_resolver",
		map[string]any{"file": path, "old_name": "environment", "new_name": "env"},
		renameResolver(srv))
	assert.True(t, result.IsError)
	assert.Contains(t, extractText(t, result), "could not be located")
}

func TestHandleRenameResolver_FileNotFound(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	result, _ := callTool(t, "rename_resolver",
		map[string]any{"file": "/no/such/solution.yaml", "old_name": "a", "new_name": "b"},
		renameResolver(srv))
	assert.True(t, result.IsError)
	assert.Contains(t, extractText(t, result), "loading solution")
}

func TestHandleRefactor_MissingArgs(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))

	r1, _ := callTool(t, "find_resolver_references", map[string]any{"file": "x.yaml"}, findResolverRefs(srv))
	assert.True(t, r1.IsError, "missing 'resolver' should error")

	r2, _ := callTool(t, "rename_resolver", map[string]any{"file": "x.yaml", "old_name": "a"}, renameResolver(srv))
	assert.True(t, r2.IsError, "missing 'new_name' should error")
}

func TestHandleFindResolverReferences_CwdRelativePath(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "solution.yaml"), []byte(refactorFixture), 0o600))

	// Relative file resolved against cwd.
	_, data := callTool(t, "find_resolver_references",
		map[string]any{"file": "solution.yaml", "resolver": "environment", "cwd": dir},
		findResolverRefs(srv))
	assert.Equal(t, true, data["defined"])
}

// actionRefactorFixture references action "build" via dependsOn, CEL, and
// template forms; "make build" is unrelated literal text left untouched.
const actionRefactorFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: mcp-refactor-action
spec:
  resolvers: {}
  workflow:
    actions:
      build:
        provider: shell
        inputs:
          command: make build
      deploy:
        dependsOn:
          - build
        provider: shell
        when:
          expr: __actions.build.results.exitCode == 0
        inputs:
          command:
            tmpl: 'deploy {{ .__actions.build.results.stdout }}'
`

func findActionRefs(srv *Server) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return srv.handleFindReferences(ctx, request, refindex.SymbolAction, "action")
	}
}

func renameAction(srv *Server) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return srv.handleRename(ctx, request, refindex.SymbolAction, "action")
	}
}

func TestHandleFindActionReferences(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)
	path := writeRefactorFixture(t, actionRefactorFixture)

	_, data := callTool(t, "find_action_references",
		map[string]any{"file": path, "action": "build"},
		findActionRefs(srv))

	assert.Equal(t, true, data["defined"])
	assert.NotNil(t, data["definition"])
	assert.EqualValues(t, 0, data["unresolved"])

	refs, ok := data["references"].([]any)
	require.True(t, ok)
	// dependsOn + CEL + template = 3 uses.
	assert.Len(t, refs, 3)

	origins := map[string]bool{}
	for _, r := range refs {
		origins[r.(map[string]any)["origin"].(string)] = true
	}
	assert.True(t, origins["dependsOn"])
	assert.True(t, origins["cel"])
	assert.True(t, origins["template"])
}

func TestHandleRenameAction(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	path := writeRefactorFixture(t, actionRefactorFixture)

	_, data := callTool(t, "rename_action",
		map[string]any{"file": path, "old_name": "build", "new_name": "compile"},
		renameAction(srv))

	assert.Equal(t, "build", data["oldName"])
	assert.Equal(t, "compile", data["newName"])

	occ, ok := data["occurrences"].([]any)
	require.True(t, ok)
	// definition + dependsOn + CEL + template = 4.
	assert.Len(t, occ, 4)

	content, ok := data["content"].(string)
	require.True(t, ok)
	assert.Contains(t, content, "compile:")
	assert.Contains(t, content, "__actions.compile.results.exitCode")
	assert.NotContains(t, content, "__actions.build")
	// Unrelated literal text is preserved.
	assert.Contains(t, content, "make build")
}

func TestHandleRenameAction_InvalidName(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	path := writeRefactorFixture(t, actionRefactorFixture)

	result, _ := callTool(t, "rename_action",
		map[string]any{"file": path, "old_name": "build", "new_name": "1bad"},
		renameAction(srv))
	assert.True(t, result.IsError)

	var te struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractText(t, result)), &te))
	assert.Equal(t, ErrCodeValidationError, te.Code)
}

// callRefactorFixture references call "fetch" from a resolve step and an action.
const callRefactorFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: mcp-refactor-call
spec:
  calls:
    fetch:
      provider: message
      inputs:
        message: fetching
  resolvers:
    r1:
      resolve:
        with:
          - call: fetch
  workflow:
    actions:
      a1:
        call: fetch
`

// functionRefactorFixture invokes author function "greet" from a function body
// and a resolver template (explicit resolver ref, no sprig helpers).
const functionRefactorFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: mcp-refactor-function
spec:
  functions:
    greet:
      params:
        - name: who
      template: "hello {{ .args.who }}"
    loud:
      params:
        - name: msg
      template: "{{ greet .args.msg }}!"
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    msg:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "{{ greet ._.env }}"
`

func findCallRefs(srv *Server) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return srv.handleFindReferences(ctx, request, refindex.SymbolCall, "call")
	}
}

func renameCall(srv *Server) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return srv.handleRename(ctx, request, refindex.SymbolCall, "call")
	}
}

func findFunctionRefs(srv *Server) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return srv.handleFindReferences(ctx, request, refindex.SymbolFunction, "function")
	}
}

func renameFunction(srv *Server) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return srv.handleRename(ctx, request, refindex.SymbolFunction, "function")
	}
}

func TestHandleFindCallReferences(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	path := writeRefactorFixture(t, callRefactorFixture)

	_, data := callTool(t, "find_call_references",
		map[string]any{"file": path, "call": "fetch"}, findCallRefs(srv))

	assert.Equal(t, true, data["defined"])
	refs, ok := data["references"].([]any)
	require.True(t, ok)
	// resolve.with + action = 2 uses.
	assert.Len(t, refs, 2)
	for _, r := range refs {
		assert.Equal(t, "call", r.(map[string]any)["origin"])
	}
}

func TestHandleRenameCall(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	path := writeRefactorFixture(t, callRefactorFixture)

	_, data := callTool(t, "rename_call",
		map[string]any{"file": path, "old_name": "fetch", "new_name": "download"}, renameCall(srv))

	assert.Equal(t, "download", data["newName"])
	occ, ok := data["occurrences"].([]any)
	require.True(t, ok)
	// definition + 2 uses = 3.
	assert.Len(t, occ, 3)
	content, _ := data["content"].(string)
	assert.Contains(t, content, "download:")
	assert.Contains(t, content, "- call: download")
	// Unrelated literal preserved.
	assert.Contains(t, content, "message: fetching")
}

func TestHandleFindFunctionReferences(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	path := writeRefactorFixture(t, functionRefactorFixture)

	_, data := callTool(t, "find_function_references",
		map[string]any{"file": path, "function": "greet"}, findFunctionRefs(srv))

	assert.Equal(t, true, data["defined"])
	refs, ok := data["references"].([]any)
	require.True(t, ok)
	// loud's body + msg template = 2 invocations.
	assert.Len(t, refs, 2)
	for _, r := range refs {
		assert.Equal(t, "function", r.(map[string]any)["origin"])
	}
}

func TestHandleRenameFunction(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	path := writeRefactorFixture(t, functionRefactorFixture)

	_, data := callTool(t, "rename_function",
		map[string]any{"file": path, "old_name": "greet", "new_name": "welcome"}, renameFunction(srv))

	assert.Equal(t, "welcome", data["newName"])
	occ, ok := data["occurrences"].([]any)
	require.True(t, ok)
	// definition + 2 invocations = 3.
	assert.Len(t, occ, 3)
	content, _ := data["content"].(string)
	assert.Contains(t, content, "welcome:")
	assert.Contains(t, content, "{{ welcome .args.msg }}")
	assert.Contains(t, content, "{{ welcome ._.env }}")
}

func TestHandleRenameFunction_InvalidName(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	path := writeRefactorFixture(t, functionRefactorFixture)

	result, _ := callTool(t, "rename_function",
		map[string]any{"file": path, "old_name": "greet", "new_name": "1bad"}, renameFunction(srv))
	assert.True(t, result.IsError)
	var te struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractText(t, result)), &te))
	assert.Equal(t, ErrCodeValidationError, te.Code)
}
