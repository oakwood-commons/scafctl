// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandSnapshot(t *testing.T) {
	cliParams := &settings.Run{}
	ioStreams := terminal.IOStreams{}

	cmd := CommandSnapshot(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "snapshot", cmd.Use)
	assert.Equal(t, "Manage resolver execution snapshots", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)

	// Verify subcommands are added (save was moved to render solution --snapshot,
	// diff was moved to the top-level `diff snapshot` command)
	subcommands := cmd.Commands()
	assert.Len(t, subcommands, 1, "should have 1 subcommand (show)")

	foundShow := false

	for _, sub := range subcommands {
		if sub.Name() == "show" {
			foundShow = true
		}
	}

	assert.True(t, foundShow, "show subcommand should be present")
}

// TestCommandSnapshot_UnknownSubcommandErrors verifies that an unknown
// subcommand (e.g. the hard-removed 'snapshot diff') errors, while a bare
// invocation shows help and exits 0.
func TestCommandSnapshot_UnknownSubcommandErrors(t *testing.T) {
	cliParams := &settings.Run{}
	ioStreams := terminal.IOStreams{}

	cmd := CommandSnapshot(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"diff", "a", "b"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandSnapshot(cliParams, ioStreams, "scafctl")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}

func TestCommandSnapshot_ExampleContainsBinaryName(t *testing.T) {
	cliParams := &settings.Run{}
	ioStreams := terminal.IOStreams{}
	binaryName := "testcli"

	cmd := CommandSnapshot(cliParams, ioStreams, binaryName)

	assert.Contains(t, cmd.Example, binaryName, "example should contain binary name")
}
