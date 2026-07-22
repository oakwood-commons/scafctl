// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cel

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/celfunction"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandCel(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCel(cliParams, ioStreams, "scafctl/get")
	require.NotNil(t, cmd)
	assert.Equal(t, "cel", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	names := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "functions", "get cel should wire up the 'functions' child")
}

// TestCommandCel_BareShowsHelp verifies a bare 'get cel' shows help (exit 0)
// and an unknown subcommand errors.
func TestCommandCel_BareShowsHelp(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCel(cliParams, ioStreams, "scafctl/get")
	cmd.SetArgs([]string{})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	assert.NoError(t, cmd.Execute())
}

func TestCommandCel_UnknownSubcommandErrors(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCel(cliParams, ioStreams, "scafctl/get")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

// TestEmbedderBinaryName verifies the deprecated cel-functions leaf reachable
// through 'get cel' honors a non-default (embedder) binary name in its
// user-facing deprecation notice rather than hardcoding "scafctl".
func TestEmbedderBinaryName(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := celfunction.CommandCelFunctionDeprecated(cliParams, ioStreams, "mycli/get")
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Deprecated, "mycli")
	assert.NotContains(t, cmd.Deprecated, "scafctl")
}
