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

func TestCommandSnapshot_ExampleContainsBinaryName(t *testing.T) {
	cliParams := &settings.Run{}
	ioStreams := terminal.IOStreams{}
	binaryName := "testcli"

	cmd := CommandSnapshot(cliParams, ioStreams, binaryName)

	assert.Contains(t, cmd.Example, binaryName, "example should contain binary name")
}
