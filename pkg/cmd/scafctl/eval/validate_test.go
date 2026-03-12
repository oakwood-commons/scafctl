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

func TestCommandValidate(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandValidate(cliParams, ioStreams, "scafctl/eval")

	require.NotNil(t, cmd)
	assert.Equal(t, "validate", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandValidate_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandValidate(cliParams, ioStreams, "scafctl/eval")

	tests := []struct {
		name     string
		flagName string
		defVal   string
	}{
		{"expression flag", "expression", ""},
		{"type flag", "type", ""},
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

func TestCommandValidate_RequiredFlags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandValidate(cliParams, ioStreams, "scafctl/eval")
	cmd.SetArgs([]string{}) // no required flags

	err := cmd.Execute()
	assert.Error(t, err, "should fail without required --expression and --type flags")
}

func BenchmarkCommandValidate(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandValidate(cliParams, ioStreams, "scafctl/eval")
	}
}
