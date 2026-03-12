// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandCEL(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCEL(cliParams, ioStreams, "scafctl/eval")

	require.NotNil(t, cmd)
	assert.Equal(t, "cel", cmd.Use)
	assert.Contains(t, cmd.Aliases, "c")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandCEL_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCEL(cliParams, ioStreams, "scafctl/eval")

	tests := []struct {
		name     string
		flagName string
		defVal   string
	}{
		{"expression flag", "expression", ""},
		{"var flag", "var", "[]"},
		{"data flag", "data", ""},
		{"file flag", "file", ""},
		{"output flag", "output", "auto"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, f, "flag %q should exist", tt.flagName)
			assert.Equal(t, tt.defVal, f.DefValue, "flag %q default value", tt.flagName)
		})
	}
}

func TestCommandCEL_ExpressionRequired(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCEL(cliParams, ioStreams, "scafctl/eval")
	cmd.SetArgs([]string{}) // no --expression

	err := cmd.Execute()
	assert.Error(t, err, "should fail without required --expression flag")
}

func TestCommandCEL_OutputShorthand(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCEL(cliParams, ioStreams, "scafctl/eval")

	f := cmd.Flags().ShorthandLookup("o")
	require.NotNil(t, f, "output flag should have -o shorthand")
	assert.Equal(t, "output", f.Name)
}

func BenchmarkCommandCEL(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandCEL(cliParams, ioStreams, "scafctl/eval")
	}
}
