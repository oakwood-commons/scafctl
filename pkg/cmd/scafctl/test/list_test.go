// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunList_AutoDiscover(t *testing.T) {
	t.Parallel()

	ctx, _ := discoveryDir(t, true)
	streams, _, _ := terminal.NewTestIOStreams()

	err := runList(ctx, &ListOptions{IOStreams: streams, CliParams: settings.NewCliParams()})
	require.NoError(t, err)
}

func TestRunList_AutoDiscover_NotFound(t *testing.T) {
	t.Parallel()

	ctx, _ := discoveryDir(t, false)
	streams, _, errOut := terminal.NewTestIOStreams()

	err := runList(ctx, &ListOptions{IOStreams: streams, CliParams: settings.NewCliParams()})
	require.ErrorIs(t, err, load.ErrNoSolutionFound)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, errOut.String(), "no solution file found")
}

func TestRunList_FetchesCatalogReference(t *testing.T) {
	t.Parallel()

	streams, out, _ := terminal.NewTestIOStreams()
	fl := &fakeLoader{ms: &load.MaterializedSolution{Solution: parsedSolution(t)}}

	err := runList(t.Context(), &ListOptions{
		IOStreams:       streams,
		CliParams:       settings.NewCliParams(),
		File:            "discovered@1.0.0",
		IncludeBuiltins: true,
		selector:        newFakeSelector(fl),
		KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
	})
	require.NoError(t, err)
	assert.Equal(t, source.KindBareName, fl.got.Kind)
	assert.Contains(t, out.String(), "renders")
}

func TestCommandList(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandList(cliParams, ioStreams, "scafctl/test")

	require.NotNil(t, cmd)
	assert.Equal(t, "list [name[@version]]", cmd.Use)
	assert.Contains(t, cmd.Aliases, "ls")
	assert.Contains(t, cmd.Aliases, "l")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandList_EmbedderBinaryName(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandList(cliParams, ioStreams, "mycli/test")

	assert.Contains(t, cmd.Long, "mycli test list my-solution@1.0.0")
	assert.NotContains(t, cmd.Long, "scafctl")
}

func TestCommandList_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandList(cliParams, ioStreams, "scafctl/test")

	tests := []struct {
		name     string
		flagName string
	}{
		{"file", "file"},
		{"tests-path", "tests-path"},
		{"output", "output"},
		{"include-builtins", "include-builtins"},
		{"filter", "filter"},
		{"tag", "tag"},
		{"solution", "solution"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			assert.NotNil(t, f, "flag %q should exist", tt.flagName)
		})
	}
}

func TestCommandList_PreRunE_VersionedRef(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandList(cliParams, ioStreams, "scafctl/test")

	err := cmd.PreRunE(cmd, []string{"my-app@1.2.3"})
	assert.NoError(t, err)
}

func TestCommandList_PreRunE_BareNameRejected(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandList(cliParams, ioStreams, "scafctl/test")

	err := cmd.PreRunE(cmd, []string{"my-app"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bare names are not supported")
}

func TestCommandList_PreRunE_ConflictsWithFileFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandList(cliParams, ioStreams, "scafctl/test")
	require.NoError(t, cmd.Flags().Set("file", "solution.yaml"))

	err := cmd.PreRunE(cmd, []string{"my-app@1.0.0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot use both")
}

func TestCommandList_PreRunE_NoArgs(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandList(cliParams, ioStreams, "scafctl/test")

	err := cmd.PreRunE(cmd, []string{})
	assert.NoError(t, err)
}

func BenchmarkCommandList(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandList(cliParams, ioStreams, "scafctl/test")
	}
}
