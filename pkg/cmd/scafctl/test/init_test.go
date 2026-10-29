// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const discoverySolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: discovered
  version: 1.0.0
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: static
            inputs:
              value: dev
`

// discoveryDir returns a temp dir, with solution.yaml written when withSolution
// is set, and a context whose working directory (--cwd) is that dir.
func discoveryDir(t *testing.T, withSolution bool) (context.Context, string) {
	t.Helper()
	dir := t.TempDir()
	if withSolution {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "solution.yaml"), []byte(discoverySolutionYAML), 0o600))
	}
	return provider.WithWorkingDirectory(t.Context(), dir), dir
}

func TestRunInit_AutoDiscover(t *testing.T) {
	t.Parallel()

	ctx, dir := discoveryDir(t, true)
	streams, out, _ := terminal.NewTestIOStreams()

	err := runInit(ctx, &InitOptions{IOStreams: streams, CliParams: settings.NewCliParams()})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "# Generated test scaffold for "+filepath.Join(dir, "solution.yaml"))
}

func TestRunInit_AutoDiscover_NotFound(t *testing.T) {
	t.Parallel()

	ctx, _ := discoveryDir(t, false)
	streams, _, errOut := terminal.NewTestIOStreams()

	err := runInit(ctx, &InitOptions{IOStreams: streams, CliParams: settings.NewCliParams()})
	require.ErrorIs(t, err, load.ErrNoSolutionFound)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, errOut.String(), "no solution file found")
}

func TestRunInit_FetchesCatalogReference(t *testing.T) {
	t.Parallel()

	streams, out, _ := terminal.NewTestIOStreams()
	fl := &fakeLoader{ms: &load.MaterializedSolution{Solution: parsedSolution(t), Bundle: bundleTar(t)}}

	err := runInit(t.Context(), &InitOptions{
		IOStreams: streams,
		CliParams: settings.NewCliParams(),
		File:      "discovered@1.0.0",
		selector:  newFakeSelector(fl),
	})
	require.NoError(t, err)
	assert.Equal(t, source.KindBareName, fl.got.Kind)
	assert.Contains(t, out.String(), "# Generated test scaffold for discovered@1.0.0", "the header names the ref, not the temp copy")
}

func TestRunInit_FetchError(t *testing.T) {
	t.Parallel()

	streams, _, errOut := terminal.NewTestIOStreams()

	err := runInit(t.Context(), &InitOptions{
		IOStreams: streams,
		CliParams: settings.NewCliParams(),
		File:      "discovered@1.0.0",
		selector:  newFakeSelector(&fakeLoader{err: errors.New("not in catalog")}),
	})
	require.ErrorIs(t, err, load.ErrFetch)
	assert.Equal(t, exitcode.CatalogError, exitcode.GetCode(err))
	assert.Contains(t, errOut.String(), "not in catalog")
}

func TestCommandInit(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandInit(cliParams, ioStreams, "scafctl/test")

	require.NotNil(t, cmd)
	assert.Equal(t, "init [name[@version]]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandInit_EmbedderBinaryName(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandInit(cliParams, ioStreams, "mycli/test")

	assert.Contains(t, cmd.Long, "mycli test init my-solution@1.0.0")
	assert.NotContains(t, cmd.Long, "scafctl")
}

func TestCommandInit_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandInit(cliParams, ioStreams, "scafctl/test")

	f := cmd.Flags().Lookup("file")
	assert.NotNil(t, f, "flag 'file' should exist")
}

func TestCommandInit_PreRunE_VersionedRef(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandInit(cliParams, ioStreams, "scafctl/test")

	cmd.SetArgs([]string{"my-app@1.2.3"})
	// PreRunE runs as part of Execute but RunE will fail without a real solution.
	// We test PreRunE directly by checking it doesn't error on valid input.
	err := cmd.PreRunE(cmd, []string{"my-app@1.2.3"})
	assert.NoError(t, err)
}

func TestCommandInit_PreRunE_BareNameRejected(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandInit(cliParams, ioStreams, "scafctl/test")

	err := cmd.PreRunE(cmd, []string{"my-app"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bare names are not supported")
}

func TestCommandInit_PreRunE_ConflictsWithFileFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandInit(cliParams, ioStreams, "scafctl/test")
	require.NoError(t, cmd.Flags().Set("file", "solution.yaml"))

	err := cmd.PreRunE(cmd, []string{"my-app@1.0.0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot use both")
}

func TestCommandInit_PreRunE_NoArgs(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandInit(cliParams, ioStreams, "scafctl/test")

	err := cmd.PreRunE(cmd, []string{})
	assert.NoError(t, err)
}

func BenchmarkCommandInit(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandInit(cliParams, ioStreams, "scafctl/test")
	}
}
