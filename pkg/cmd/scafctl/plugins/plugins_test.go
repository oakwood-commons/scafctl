// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandPlugins(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPlugins(cliParams, ioStreams, "scafctl")
	require.NotNil(t, cmd)
	assert.Equal(t, "plugins", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.True(t, cmd.SilenceUsage)
	assert.Nil(t, cmd.RunE, "parent plugins command should not have RunE")
	subCmds := cmd.Commands()
	require.Len(t, subCmds, 2, "should have 2 subcommands: install, list")
	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Name()
	}
	assert.Contains(t, cmdNames, "install")
	assert.Contains(t, cmdNames, "list")
}

func TestCommandInstall(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInstall(cliParams, ioStreams, "scafctl/plugins")
	require.NotNil(t, cmd)
	assert.Equal(t, "install", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandInstall_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInstall(cliParams, ioStreams, "scafctl/plugins")
	tests := []struct {
		name     string
		flagName string
		defVal   string
	}{
		{"file", "file", ""},
		{"platform", "platform", ""},
		{"cache-dir", "cache-dir", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, f, "flag %q should exist", tt.flagName)
			assert.Equal(t, tt.defVal, f.DefValue, "flag %q default value", tt.flagName)
		})
	}
}

func TestCommandPluginsList(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/plugins")
	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)
	assert.Contains(t, cmd.Aliases, "ls")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	f := cmd.Flags().Lookup("cache-dir")
	require.NotNil(t, f, "--cache-dir flag should exist")
}

func BenchmarkCommandPlugins(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandPlugins(cliParams, ioStreams, "scafctl")
	}
}
