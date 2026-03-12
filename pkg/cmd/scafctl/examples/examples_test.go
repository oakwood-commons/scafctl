// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandExamples(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExamples(cliParams, ioStreams, "scafctl")
	require.NotNil(t, cmd)
	assert.Equal(t, "examples", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.True(t, cmd.SilenceUsage)
	assert.Nil(t, cmd.RunE, "parent examples command should not have RunE")
	subCmds := cmd.Commands()
	require.Len(t, subCmds, 2, "should have 2 subcommands: list, get")
	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Name()
	}
	assert.Contains(t, cmdNames, "list")
	assert.Contains(t, cmdNames, "get")
}

func TestCommandExamplesList(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/examples")
	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)
	assert.Contains(t, cmd.Aliases, "ls")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	f := cmd.Flags().Lookup("category")
	require.NotNil(t, f, "--category flag should exist")
}

func TestCommandExamplesGet(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandGet(cliParams, ioStreams, "scafctl/examples")
	require.NotNil(t, cmd)
	assert.Equal(t, "get <example-path>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	f := cmd.Flags().Lookup("output")
	require.NotNil(t, f, "--output flag should exist")
	assert.Equal(t, "", f.DefValue)
}

func TestCommandExamplesGet_RequiresArg(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandGet(cliParams, ioStreams, "scafctl/examples")
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "should fail without example-path argument")
}

func BenchmarkCommandExamples(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandExamples(cliParams, ioStreams, "scafctl")
	}
}
