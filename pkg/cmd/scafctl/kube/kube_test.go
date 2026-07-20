// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/terminal"
)

func TestCommandKube(t *testing.T) {
	t.Parallel()

	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)
	cmd := CommandKube(embedderParams(), ioStreams, "mycli")

	require.NotNil(t, cmd)
	assert.Equal(t, "kube", cmd.Name())
	assert.Contains(t, cmd.Aliases, "k8s")

	sub := make(map[string]bool)
	for _, c := range cmd.Commands() {
		sub[c.Name()] = true
	}
	assert.True(t, sub["login"], "kube must expose the login subcommand")
	assert.True(t, sub["logout"], "kube must expose the logout subcommand")
	assert.True(t, sub["list"], "kube must expose the list subcommand")
	assert.True(t, sub["status"], "kube must expose the status subcommand")
}

func TestCommandKube_LongHelpUsesBinaryName(t *testing.T) {
	t.Parallel()

	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)
	cmd := CommandKube(embedderParams(), ioStreams, "mycli")

	// The heredoc references "scafctl"; it must be rewritten to the embedder's
	// binary name so help text is never hardcoded.
	assert.Contains(t, cmd.Long, "mycli kube login")
	assert.NotContains(t, cmd.Long, "scafctl kube login")
}

// TestCommandKube_UnknownSubcommandErrors verifies that an unknown subcommand
// errors (non-zero) while a bare invocation shows help and exits 0.
func TestCommandKube_UnknownSubcommandErrors(t *testing.T) {
	t.Parallel()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandKube(embedderParams(), ioStreams, "mycli")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandKube(embedderParams(), ioStreams, "mycli")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}
