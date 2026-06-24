// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandValidate(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidate(cliParams, streams, "scafctl")

	assert.Equal(t, "validate", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	// The resolver subcommand must be wired up.
	var resolverCmd bool
	for _, sub := range cmd.Commands() {
		if sub.Name() == "resolver" {
			resolverCmd = true
		}
	}
	require.True(t, resolverCmd, "validate must register the resolver subcommand")
}

func TestCommandValidate_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"

	cmd := CommandValidate(cliParams, streams, "mycli")
	assert.Contains(t, cmd.Short, "mycli")
}
