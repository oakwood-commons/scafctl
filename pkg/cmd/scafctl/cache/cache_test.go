// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandCache(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandCache(cliParams, ioStreams, "scafctl")
	require.NotNil(t, cmd)
	assert.Equal(t, "cache", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.True(t, cmd.SilenceUsage)
	assert.Nil(t, cmd.RunE, "parent cache command should not have RunE")
	subCmds := cmd.Commands()
	require.Len(t, subCmds, 2, "should have 2 subcommands: clear, info")
	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Name()
	}
	assert.Contains(t, cmdNames, "clear")
	assert.Contains(t, cmdNames, "info")
}

func TestCommandClear(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandClear(cliParams, ioStreams, "scafctl/cache")
	require.NotNil(t, cmd)
	assert.Equal(t, "clear", cmd.Use)
	assert.Contains(t, cmd.Aliases, "clean")
	assert.Contains(t, cmd.Aliases, "rm")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandClear_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandClear(cliParams, ioStreams, "scafctl/cache")
	tests := []struct {
		name     string
		flagName string
		defVal   string
	}{
		{"kind", "kind", ""},
		{"name", "name", ""},
		{"force", "force", "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, f, "flag %q should exist", tt.flagName)
			assert.Equal(t, tt.defVal, f.DefValue, "flag %q default value", tt.flagName)
		})
	}
}

func TestCommandInfo(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInfo(cliParams, ioStreams, "scafctl/cache")
	require.NotNil(t, cmd)
	assert.Equal(t, "info", cmd.Use)
	assert.Contains(t, cmd.Aliases, "status")
	assert.Contains(t, cmd.Aliases, "show")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func BenchmarkCommandCache(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandCache(cliParams, ioStreams, "scafctl")
	}
}
