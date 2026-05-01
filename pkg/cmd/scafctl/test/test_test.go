// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandTest(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTest(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "test [reference]", cmd.Use)
	assert.Contains(t, cmd.Aliases, "t")
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.RunE, "parent test command should have RunE (defaults to functional)")

	subCmds := cmd.Commands()
	require.Len(t, subCmds, 3, "should have 3 subcommands: functional, init, list")

	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Name()
	}
	assert.Contains(t, cmdNames, "functional")
	assert.Contains(t, cmdNames, "init")
	assert.Contains(t, cmdNames, "list")
}

func TestCommandTest_DefaultsToFunctional(t *testing.T) {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTest(cliParams, ioStreams, "mycli")
	assert.NotNil(t, cmd.RunE, "parent test command should have RunE that defaults to functional")
	assert.Contains(t, cmd.Long, "without a subcommand")
}

func TestCommandTest_RunE_NoSolutionFile(t *testing.T) {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTest(cliParams, ioStreams, "mycli")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no solution path provided")

	// Verify EntryPointSettings.Path was set via filepath.Join
	assert.Equal(t, filepath.Join("mycli", "test"), cliParams.EntryPointSettings.Path)
	assert.Equal(t, "mycli", cliParams.BinaryName)
}

func BenchmarkCommandTest(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandTest(cliParams, ioStreams, "scafctl")
	}
}
