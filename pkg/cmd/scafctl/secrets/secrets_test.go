// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandSecrets(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandSecrets(cliParams, ioStreams, "scafctl")
	require.NotNil(t, cmd)
	assert.Equal(t, "secrets", cmd.Use)
	assert.Contains(t, cmd.Aliases, "secret")
	assert.NotEmpty(t, cmd.Short)
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.RunE, "parent secrets command needs a help-only RunE so NoArgs rejects unknown subcommands")
	subCmds := cmd.Commands()
	require.Len(t, subCmds, 8, "should have 8 subcommands")
	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Name()
	}
	assert.Contains(t, cmdNames, "list")
	assert.Contains(t, cmdNames, "get")
	assert.Contains(t, cmdNames, "set")
	assert.Contains(t, cmdNames, "delete")
	assert.Contains(t, cmdNames, "exists")
	assert.Contains(t, cmdNames, "export")
	assert.Contains(t, cmdNames, "import")
	assert.Contains(t, cmdNames, "rotate")
}

func TestCommandDelete(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "delete <name>", cmd.Use)
	assert.Contains(t, cmd.Aliases, "rm")
	assert.Contains(t, cmd.Aliases, "remove")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	f := cmd.Flags().Lookup("force")
	require.NotNil(t, f, "--force flag should exist")
	af := cmd.Flags().Lookup("all")
	require.NotNil(t, af, "--all flag should exist")
}

func TestCommandDelete_RequiresArg(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/secrets")
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "should fail without name argument")
}

func TestCommandExists(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExists(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "exists <name>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	af := cmd.Flags().Lookup("all")
	require.NotNil(t, af, "--all flag should exist")
}

func TestCommandExport(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExport(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "export", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandGet(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandGet(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "get <name>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandImport(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandImport(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "import <file>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandList(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandRotate(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandRotate(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "rotate", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandSet(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandSet(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "set <name> [value]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func BenchmarkCommandSecrets(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandSecrets(cliParams, ioStreams, "scafctl")
	}
}

// TestCommandSecrets_UnknownSubcommandErrors verifies that an unknown
// subcommand errors (non-zero) while a bare invocation shows help and exits 0.
func TestCommandSecrets_UnknownSubcommandErrors(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandSecrets(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandSecrets(cliParams, ioStreams, "scafctl")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}
