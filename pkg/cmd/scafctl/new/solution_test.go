// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package newcmd

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandNew(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandNew(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "new", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)

	subCmds := cmd.Commands()
	require.Len(t, subCmds, 1, "should have 1 subcommand: solution")

	names := make([]string, len(subCmds))
	for i, c := range subCmds {
		names[i] = c.Name()
	}
	assert.Contains(t, names, "solution")
}

func TestCommandNew_NoRunE(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandNew(cliParams, ioStreams, "scafctl")
	assert.Nil(t, cmd.RunE, "parent new command should not have RunE")
}

func TestCommandSolution(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")

	require.NotNil(t, cmd)
	assert.Equal(t, "solution", cmd.Use)
	assert.Contains(t, cmd.Aliases, "sol")
	assert.Contains(t, cmd.Aliases, "s")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandSolution_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")

	tests := []struct {
		name     string
		flagName string
		defVal   string
	}{
		{"name flag", "name", ""},
		{"description flag", "description", ""},
		{"version flag", "version", "1.0.0"},
		{"features flag", "features", ""},
		{"providers flag", "providers", ""},
		{"output flag", "output", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, f, "flag %q should exist", tt.flagName)
			assert.Equal(t, tt.defVal, f.DefValue, "flag %q default value", tt.flagName)
		})
	}
}

func TestCommandSolution_RequiredFlags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	assert.Error(t, err, "should fail without required --name and --description flags")
}

func TestCommandSolution_Shorthands(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")

	shorthands := map[string]string{
		"n": "name",
		"o": "output",
	}

	for short, full := range shorthands {
		f := cmd.Flags().ShorthandLookup(short)
		require.NotNil(t, f, "shorthand -%s should exist", short)
		assert.Equal(t, full, f.Name, "shorthand -%s should map to --%s", short, full)
	}
}

func BenchmarkCommandNew(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandNew(cliParams, ioStreams, "scafctl")
	}
}

func BenchmarkCommandSolution(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandSolution(cliParams, ioStreams, "scafctl/new")
	}
}
