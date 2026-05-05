// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package serve

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandServe(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandServe(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "serve", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)
}

func TestCommandServe_HasOpenAPISubcommand(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandServe(cliParams, ioStreams, "scafctl")

	subCmds := cmd.Commands()
	require.Len(t, subCmds, 1, "should have 1 subcommand: openapi")
	assert.Equal(t, "openapi", subCmds[0].Name())
}

func TestCommandServe_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandServe(cliParams, ioStreams, "scafctl")

	flags := []string{"host", "port", "tls-cert", "tls-key", "enable-tls", "api-version"}
	for _, flagName := range flags {
		f := cmd.Flags().Lookup(flagName)
		assert.NotNilf(t, f, "expected flag %q to be registered", flagName)
	}
}

func TestCommandOpenAPI(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandOpenAPI(cliParams, ioStreams)

	require.NotNil(t, cmd)
	assert.Equal(t, "openapi", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.True(t, cmd.SilenceUsage)
}

func TestCommandOpenAPI_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandOpenAPI(cliParams, ioStreams)

	formatFlag := cmd.Flags().Lookup("format")
	require.NotNil(t, formatFlag)
	assert.Equal(t, "json", formatFlag.DefValue)

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
}

func BenchmarkCommandServe(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	for b.Loop() {
		CommandServe(cliParams, ioStreams, "scafctl")
	}
}

func TestPreloadOfficialProviders_SkipsRegistered(t *testing.T) {
	ctx := context.Background()
	reg, err := builtin.DefaultRegistry(ctx)
	require.NoError(t, err)

	// Create an official registry containing "http" which is already a builtin.
	// preloadOfficialProviders should skip it since it's already registered.
	officialReg := official.NewRegistryFrom([]official.Provider{
		{Name: "http", CatalogRef: "http", DefaultVersion: "latest"},
	})

	// No plugin fetcher needed -- all providers are already registered
	clients, preloadErr := preloadOfficialProviders(ctx, reg, officialReg, nil)
	assert.NoError(t, preloadErr)
	assert.Empty(t, clients)
}

func TestPreloadOfficialProviders_NilFetcher(t *testing.T) {
	ctx := context.Background()
	reg := provider.NewRegistry()
	officialReg := official.NewRegistry()

	// Without a fetcher, should return nil (not panic)
	clients, preloadErr := preloadOfficialProviders(ctx, reg, officialReg, nil)
	assert.Nil(t, clients)
	assert.Nil(t, preloadErr)
}
