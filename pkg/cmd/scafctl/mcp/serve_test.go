// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
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

func TestBuildMCPPluginPool(t *testing.T) {
	t.Run("nil config enables official registry", func(t *testing.T) {
		reg := provider.NewRegistry()
		lgr := logr.Discard()
		pool, ctx := buildMCPPluginPool(context.Background(), nil, reg, &lgr)
		defer pool.Shutdown()

		// Official registry should be injected into context
		officialReg := official.RegistryFromContext(ctx)
		assert.NotNil(t, officialReg, "official registry should be in context")
	})

	t.Run("DisableOfficialProviders skips official registry", func(t *testing.T) {
		reg := provider.NewRegistry()
		lgr := logr.Discard()
		cfg := &config.Config{
			Settings: config.Settings{DisableOfficialProviders: true},
		}
		pool, ctx := buildMCPPluginPool(context.Background(), cfg, reg, &lgr)
		defer pool.Shutdown()

		// Official registry should NOT be in context
		officialReg := official.RegistryFromContext(ctx)
		assert.Nil(t, officialReg, "official registry should not be in context when disabled")
	})

	t.Run("enabled config creates official registry", func(t *testing.T) {
		reg := provider.NewRegistry()
		lgr := logr.Discard()
		cfg := &config.Config{
			Settings: config.Settings{DisableOfficialProviders: false},
		}
		pool, ctx := buildMCPPluginPool(context.Background(), cfg, reg, &lgr)
		defer pool.Shutdown()

		officialReg := official.RegistryFromContext(ctx)
		assert.NotNil(t, officialReg, "official registry should be in context")
	})

	t.Run("pool does not sanitize env for MCP interactive sessions", func(t *testing.T) {
		reg := provider.NewRegistry()
		lgr := logr.Discard()
		pool, _ := buildMCPPluginPool(context.Background(), nil, reg, &lgr)
		defer pool.Shutdown()

		// MCP pools should not sanitize env so host credentials are available
		assert.False(t, pool.SanitizeEnv(), "MCP pool should not sanitize env")
	})
}
