// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package diff

import (
	"bytes"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandDiff_Subcommands(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}

	cmd := CommandDiff(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "diff", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)

	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names[subSolution], "diff should have solution subcommand")
	assert.True(t, names[subBundle], "diff should have bundle subcommand")
	assert.True(t, names[subSnapshot], "diff should have snapshot subcommand")
}

func TestCommandDiff_BareShowsHelp(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}

	cmd := CommandDiff(cliParams, ioStreams, "scafctl")
	require.NotNil(t, cmd.RunE, "bare diff should have RunE that shows help")

	// Bare invocation renders help and exits 0.
	var out, errBuf bytes.Buffer
	cmd.SetArgs([]string{})
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceErrors = true
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Usage:", "bare diff must render help")
}

// TestCommandDiff_UnknownSubcommandErrors verifies that an unknown subcommand
// errors (non-zero) with an "unknown command" message instead of falling back
// to help/exit-0.
func TestCommandDiff_UnknownSubcommandErrors(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}

	cmd := CommandDiff(cliParams, ioStreams, "scafctl")
	var out, errBuf bytes.Buffer
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestCommandDiff_ExampleUsesBinaryName(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}

	cmd := CommandDiff(cliParams, ioStreams, "mycli")
	assert.Contains(t, cmd.Example, "mycli diff solution")
	assert.Contains(t, cmd.Example, "mycli diff bundle")
	assert.Contains(t, cmd.Example, "mycli diff snapshot")
}
