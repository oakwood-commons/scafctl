// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package packagecmd

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
)

func TestCommandPackage_Structure(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}

	cmd := CommandPackage(cliParams, ioStreams, "")

	assert.Equal(t, "package", cmd.Use)
	assert.Contains(t, cmd.Aliases, "build", "build must remain a backward-compatible alias")
	assert.Contains(t, cmd.Aliases, "b")
	assert.Contains(t, cmd.Short, "Package artifacts")

	sub := make(map[string]bool)
	for _, c := range cmd.Commands() {
		sub[c.Name()] = true
	}
	assert.True(t, sub["solution"], "package group should have a solution subcommand")
	assert.True(t, sub["plugin"], "package group should have a plugin subcommand")
}

// TestCommandPackage_HelpIsEmbedderSafe verifies the group help renders the
// configured binary name instead of a hardcoded "scafctl", so external CLIs
// embedding scafctl show correct catalog paths in help output.
func TestCommandPackage_HelpIsEmbedderSafe(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{BinaryName: "mycli"}

	cmd := CommandPackage(cliParams, ioStreams, "")

	assert.Contains(t, cmd.Long, "mycli", "help should use the configured binary name")
	assert.NotContains(t, cmd.Long, settings.CliBinaryName, "help must not hardcode the default binary name for embedders")
}
