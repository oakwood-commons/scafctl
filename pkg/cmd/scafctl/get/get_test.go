// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package get

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandGet(t *testing.T) {
	t.Parallel()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandGet(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "get", cmd.Use)
	assert.Contains(t, cmd.Aliases, "g")
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)

	// The group wires up its subcommands.
	names := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}
	for _, want := range []string{"provider", "solution", "examples", "cel-functions", "go-template-functions", "commands"} {
		assert.Contains(t, names, want, "get should wire up the %q subcommand", want)
	}
}

// TestCommandGet_UnknownSubcommandErrors verifies that an unknown subcommand
// errors (non-zero) while a bare invocation shows help and exits 0.
func TestCommandGet_UnknownSubcommandErrors(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandGet(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandGet(cliParams, ioStreams, "scafctl")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}
