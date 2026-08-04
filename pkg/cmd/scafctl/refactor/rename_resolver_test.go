// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cmdFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cmd-test # comment stays
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
`

func testCliParams() *settings.Run {
	p := settings.NewCliParams()
	p.ExitOnError = false
	return p
}

func testContext(t *testing.T) (context.Context, *bytesBuffers) {
	t.Helper()
	ioStreams, out, errOut := terminal.NewTestIOStreams()
	ctx := logger.WithLogger(context.Background(), logger.GetNoopLogger())
	ctx = writer.WithWriter(ctx, writer.New(ioStreams, testCliParams()))
	return ctx, &bytesBuffers{out: out, errOut: errOut}
}

type bytesBuffers struct {
	out    interface{ String() string }
	errOut interface{ String() string }
}

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestRunRenameResolver_HappyPath(t *testing.T) {
	path := writeFixture(t, cmdFixture)
	ctx, bufs := testContext(t)

	err := runRenameResolver(ctx, &renameResolverOptions{File: path, CliParams: testCliParams()}, "environment", "env")
	require.NoError(t, err)

	got, readErr := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, readErr)
	assert.NotContains(t, string(got), "environment")
	assert.Contains(t, string(got), "env:")
	assert.Contains(t, string(got), "# comment stays")
	assert.Contains(t, bufs.out.String(), "Renamed resolver")
}

func TestRunRenameResolver_DryRunLeavesFileUnchanged(t *testing.T) {
	path := writeFixture(t, cmdFixture)
	ctx, bufs := testContext(t)

	err := runRenameResolver(ctx, &renameResolverOptions{File: path, DryRun: true, CliParams: testCliParams()}, "environment", "env")
	require.NoError(t, err)

	got, _ := os.ReadFile(path) //nolint:gosec // test-controlled path
	assert.Equal(t, cmdFixture, string(got), "dry-run must not modify the file")
	assert.Contains(t, bufs.out.String(), "Would rename resolver")
	assert.Contains(t, bufs.out.String(), "environment -> env")
}

func TestRunRenameResolver_Errors(t *testing.T) {
	tests := []struct {
		name     string
		old      string
		newName  string
		wantCode int
		wantMsg  string
	}{
		{"collision", "environment", "appName", exitcode.ValidationFailed, "already exists"},
		{"invalid name", "environment", "1bad", exitcode.ValidationFailed, "not a valid resolver name"},
		{"undefined", "missing", "x", exitcode.ValidationFailed, "is not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFixture(t, cmdFixture)
			ctx, bufs := testContext(t)

			err := runRenameResolver(ctx, &renameResolverOptions{File: path, CliParams: testCliParams()}, tt.old, tt.newName)
			require.Error(t, err)
			assert.Equal(t, tt.wantCode, exitcode.GetCode(err))
			assert.Contains(t, bufs.errOut.String(), tt.wantMsg)

			// The file must be untouched on any error.
			got, _ := os.ReadFile(path) //nolint:gosec // test-controlled path
			assert.Equal(t, cmdFixture, string(got))
		})
	}
}

func TestRunRenameResolver_FileNotFound(t *testing.T) {
	ctx, _ := testContext(t)
	err := runRenameResolver(ctx, &renameResolverOptions{File: "/no/such/solution.yaml", CliParams: testCliParams()}, "a", "b")
	require.Error(t, err)
	assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
}

func TestCommandRefactor_Tree(t *testing.T) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	root := CommandRefactor(testCliParams(), ioStreams, "scafctl")
	assert.Equal(t, "refactor", root.Name())

	rename, _, err := root.Find([]string{"rename"})
	require.NoError(t, err)
	assert.Equal(t, "rename", rename.Name())

	resolver, _, err := root.Find([]string{"rename", "resolver"})
	require.NoError(t, err)
	assert.Equal(t, "resolver", resolver.Name())
	require.NotNil(t, resolver.Flags().Lookup("file"))
	require.NotNil(t, resolver.Flags().Lookup("dry-run"))
	// Exactly two positional args (old, new).
	assert.Error(t, resolver.Args(resolver, []string{"only-one"}))
	assert.NoError(t, resolver.Args(resolver, []string{"a", "b"}))
}

func TestCommandRefactor_EmbedderBinaryName(t *testing.T) {
	// An embedder with a non-default binary name must see it in generated help
	// examples, not a hardcoded "scafctl".
	p := settings.NewCliParams()
	p.ExitOnError = false
	p.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()

	root := CommandRefactor(p, ioStreams, "mycli")
	resolver, _, err := root.Find([]string{"rename", "resolver"})
	require.NoError(t, err)
	assert.Contains(t, resolver.Example, "mycli refactor rename resolver")
	assert.NotContains(t, resolver.Example, "scafctl refactor rename resolver")
}
