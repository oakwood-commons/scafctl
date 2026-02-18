// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandMCP(t *testing.T) {
	t.Run("creates command with serve subcommand", func(t *testing.T) {
		cliParams := &settings.Run{}
		ioStreams := &terminal.IOStreams{}
		cmd := CommandMCP(cliParams, ioStreams, "scafctl")

		assert.Equal(t, "mcp", cmd.Use)
		assert.NotEmpty(t, cmd.Short)
		assert.NotEmpty(t, cmd.Long)
		assert.True(t, cmd.SilenceUsage)

		// Verify serve subcommand is registered
		subCmds := cmd.Commands()
		require.NotEmpty(t, subCmds)

		var serveFound bool
		for _, sub := range subCmds {
			if sub.Use == "serve" {
				serveFound = true
				break
			}
		}
		assert.True(t, serveFound, "expected 'serve' subcommand")
	})
}

func TestCommandServe(t *testing.T) {
	t.Run("creates command with correct flags", func(t *testing.T) {
		cliParams := &settings.Run{}
		ioStreams := &terminal.IOStreams{}
		cmd := CommandServe(cliParams, ioStreams, "scafctl/mcp")

		assert.Equal(t, "serve", cmd.Use)
		assert.NotEmpty(t, cmd.Short)
		assert.NotEmpty(t, cmd.Long)
		assert.NotEmpty(t, cmd.Example)
		assert.True(t, cmd.SilenceUsage)

		// Verify flags exist
		transportFlag := cmd.Flags().Lookup("transport")
		require.NotNil(t, transportFlag)
		assert.Equal(t, "stdio", transportFlag.DefValue)

		logFileFlag := cmd.Flags().Lookup("log-file")
		require.NotNil(t, logFileFlag)
		assert.Equal(t, "", logFileFlag.DefValue)

		infoFlag := cmd.Flags().Lookup("info")
		require.NotNil(t, infoFlag)
		assert.Equal(t, "false", infoFlag.DefValue)
	})

	t.Run("has RunE set", func(t *testing.T) {
		cliParams := &settings.Run{}
		ioStreams := &terminal.IOStreams{}
		cmd := CommandServe(cliParams, ioStreams, "scafctl/mcp")

		assert.NotNil(t, cmd.RunE, "expected RunE to be set")
	})
}

func TestServeOptions(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		opts := &ServeOptions{
			CliParams: &settings.Run{},
			IOStreams: &terminal.IOStreams{},
		}
		assert.Empty(t, opts.Transport)
		assert.Empty(t, opts.LogFile)
		assert.False(t, opts.Info)
	})
}
