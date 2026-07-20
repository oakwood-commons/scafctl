// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandAuth(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandAuth(cliParams, ioStreams, "scafctl")

	assert.Equal(t, "auth", cmd.Use)
	assert.Contains(t, cmd.Aliases, "authenticate")
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify subcommands are added
	subCmds := cmd.Commands()
	require.Len(t, subCmds, 11)

	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Use
	}
	assert.Contains(t, cmdNames, "alias")
	assert.Contains(t, cmdNames, "diagnose")
	assert.Contains(t, cmdNames, "list [handler]")
	assert.Contains(t, cmdNames, "login <handler>")
	assert.Contains(t, cmdNames, "logout [handler]")
	assert.Contains(t, cmdNames, "migrate")
	assert.Contains(t, cmdNames, "handlers [name]")
	assert.Contains(t, cmdNames, "profile")
	assert.Contains(t, cmdNames, "status [handler]")
	assert.Contains(t, cmdNames, "token <handler>")
}

// TestCommandAuth_UnknownSubcommandErrors verifies that an unknown subcommand
// errors (non-zero) while a bare invocation shows help and exits 0.
func TestCommandAuth_UnknownSubcommandErrors(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandAuth(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandAuth(cliParams, ioStreams, "scafctl")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}

// TestCommandAlias_UnknownSubcommandErrors verifies the nested 'auth alias'
// group rejects unknown subcommands while bare invocation shows help.
func TestCommandAlias_UnknownSubcommandErrors(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandAlias(cliParams, ioStreams, "scafctl/auth")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandAlias(cliParams, ioStreams, "scafctl/auth")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}

// TestCommandProfile_UnknownSubcommandErrors verifies the nested 'auth profile'
// group rejects unknown subcommands while bare invocation shows help.
func TestCommandProfile_UnknownSubcommandErrors(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandProfile(cliParams, ioStreams, "scafctl/auth")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandProfile(cliParams, ioStreams, "scafctl/auth")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}
