// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmplfunction

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandGotmplFunction(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandGotmplFunction(cliParams, ioStreams, "scafctl/get")
	require.NotNil(t, cmd)
	assert.Equal(t, "go-template-functions", cmd.Use)
	assert.Contains(t, cmd.Aliases, "gotmpl-funcs")
	assert.Contains(t, cmd.Aliases, "gotmpl")
	assert.Contains(t, cmd.Aliases, "gtf")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandGotmplFunction_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandGotmplFunction(cliParams, ioStreams, "scafctl/get")
	flags := []string{"output", "interactive", "expression", "custom", "sprig"}
	for _, name := range flags {
		t.Run(name, func(t *testing.T) {
			f := cmd.Flags().Lookup(name)
			assert.NotNil(t, f, "flag %q should exist", name)
		})
	}
}

func TestCommandGotmplFunction_Shorthands(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandGotmplFunction(cliParams, ioStreams, "scafctl/get")
	shorthands := map[string]string{
		"o": "output",
		"i": "interactive",
		"e": "expression",
	}
	for short, full := range shorthands {
		f := cmd.Flags().ShorthandLookup(short)
		require.NotNil(t, f, "shorthand -%s should exist", short)
		assert.Equal(t, full, f.Name, "shorthand -%s should map to --%s", short, full)
	}
}

func TestCommandGotmplFunction_NoSubcommands(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandGotmplFunction(cliParams, ioStreams, "scafctl/get")
	assert.Len(t, cmd.Commands(), 0, "gotmplfunction should have no subcommands")
}

func BenchmarkCommandGotmplFunction(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandGotmplFunction(cliParams, ioStreams, "scafctl/get")
	}
}
