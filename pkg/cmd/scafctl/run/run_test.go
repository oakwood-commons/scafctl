// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommandRun_UnknownSubcommandErrors verifies that an unknown subcommand
// errors (non-zero) while a bare invocation shows help and exits 0.
func TestCommandRun_UnknownSubcommandErrors(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandRun(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
	assert.Contains(t, err.Error(), `"scafctl run solution bogus-xyz"`)
	assert.Contains(t, err.Error(), `"scafctl run action bogus-xyz"`)

	cmd2 := CommandRun(cliParams, ioStreams, "scafctl")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}

func TestCommandRun_EmbedderBinaryNameHint(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandRun(cliParams, ioStreams, "mycli")
	cmd.SetArgs([]string{"deploy-app"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"mycli run solution deploy-app"`)
	assert.Contains(t, err.Error(), `"mycli run action deploy-app"`)
	assert.NotContains(t, err.Error(), "scafctl")

	cmdFile := CommandRun(cliParams, ioStreams, "mycli")
	cmdFile.SetArgs([]string{"./solution.yaml"})
	cmdFile.SilenceErrors = true
	cmdFile.SilenceUsage = true
	errFile := cmdFile.Execute()
	require.Error(t, errFile)
	assert.Contains(t, errFile.Error(), `"mycli run solution -f ./solution.yaml"`)
	assert.Contains(t, errFile.Error(), `"mycli run action -f ./solution.yaml"`)
}
