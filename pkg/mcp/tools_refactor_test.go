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

func TestHandleFindResolverReferences(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)
	path := writeRefactorFixture(t, refactorFixture)

	_, data := callTool(t, "find_resolver_references",
		map[string]any{"file": path, "resolver": "environment"},
		srv.handleFindResolverReferences)

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
		srv.handleFindResolverReferences)

	assert.Equal(t, false, data["defined"])
	refs, _ := data["references"].([]any)
	assert.Empty(t, refs)
}

func TestHandleRenameResolver(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	path := writeRefactorFixture(t, refactorFixture)

	_, data := callTool(t, "rename_resolver",
		map[string]any{"file": path, "old_name": "environment", "new_name": "env"},
		srv.handleRenameResolver)

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
		srv.handleRenameResolver)
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
		srv.handleRenameResolver)
	assert.True(t, result.IsError)
	assert.Contains(t, extractText(t, result), "could not be located")
}

func TestHandleRenameResolver_FileNotFound(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	result, _ := callTool(t, "rename_resolver",
		map[string]any{"file": "/no/such/solution.yaml", "old_name": "a", "new_name": "b"},
		srv.handleRenameResolver)
	assert.True(t, result.IsError)
	assert.Contains(t, extractText(t, result), "loading solution")
}

func TestHandleRefactor_MissingArgs(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))

	r1, _ := callTool(t, "find_resolver_references", map[string]any{"file": "x.yaml"}, srv.handleFindResolverReferences)
	assert.True(t, r1.IsError, "missing 'resolver' should error")

	r2, _ := callTool(t, "rename_resolver", map[string]any{"file": "x.yaml", "old_name": "a"}, srv.handleRenameResolver)
	assert.True(t, r2.IsError, "missing 'new_name' should error")
}

func TestHandleFindResolverReferences_CwdRelativePath(t *testing.T) {
	srv, _ := NewServer(WithServerVersion("test"))
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "solution.yaml"), []byte(refactorFixture), 0o600))

	// Relative file resolved against cwd.
	_, data := callTool(t, "find_resolver_references",
		map[string]any{"file": "solution.yaml", "resolver": "environment", "cwd": dir},
		srv.handleFindResolverReferences)
	assert.Equal(t, true, data["defined"])
}
