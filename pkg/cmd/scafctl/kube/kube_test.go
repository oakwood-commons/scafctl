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
