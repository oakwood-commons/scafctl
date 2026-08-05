// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCliParams() *settings.Run {
	p := settings.NewCliParams()
	p.ExitOnError = false
	return p
}

func TestCommandLsp_Construction(t *testing.T) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLsp(testCliParams(), ioStreams, "scafctl")
	assert.Equal(t, "lsp", cmd.Name())
	require.NotNil(t, cmd.RunE)
	assert.NoError(t, cmd.Args(cmd, []string{}))
	assert.Error(t, cmd.Args(cmd, []string{"unexpected"}), "lsp takes no positional args")
}

func TestCommandLsp_StdioFlag(t *testing.T) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLsp(testCliParams(), ioStreams, "scafctl")

	// The --stdio flag must be registered (LSP clients pass it by convention) so
	// the server is not rejected with "unknown flag".
	flag := cmd.Flags().Lookup("stdio")
	require.NotNil(t, flag, "--stdio flag must be registered")
	assert.Equal(t, "false", flag.DefValue)

	// Parsing --stdio must succeed without error.
	require.NoError(t, cmd.ParseFlags([]string{"--stdio"}))
	v, err := cmd.Flags().GetBool("stdio")
	require.NoError(t, err)
	assert.True(t, v)
}

func TestCommandLsp_EmbedderBinaryName(t *testing.T) {
	p := settings.NewCliParams()
	p.ExitOnError = false
	p.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLsp(p, ioStreams, "mycli")
	assert.Contains(t, cmd.Long, "mycli language server")
	assert.NotContains(t, cmd.Long, "scafctl language server")
}
