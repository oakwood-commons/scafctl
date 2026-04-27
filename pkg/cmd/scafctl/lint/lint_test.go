// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandLint(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLint(cliParams, ioStreams, "scafctl")
	require.NotNil(t, cmd)
	assert.Equal(t, "lint [name[@version]]", cmd.Use)
	assert.Contains(t, cmd.Aliases, "l")
	assert.Contains(t, cmd.Aliases, "check")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE, "lint command should have RunE")
	subCmds := cmd.Commands()
	require.Len(t, subCmds, 2, "should have 2 subcommands: rules, explain")
	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Name()
	}
	assert.Contains(t, cmdNames, "rules")
	assert.Contains(t, cmdNames, "explain")
}

func TestCommandLint_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLint(cliParams, ioStreams, "scafctl")
	tests := []struct {
		name     string
		flagName string
		defVal   string
	}{
		{"file", "file", ""},
		{"output", "output", "table"},
		{"expression", "expression", ""},
		{"severity", "severity", "info"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, f, "flag %q should exist", tt.flagName)
			assert.Equal(t, tt.defVal, f.DefValue, "flag %q default value", tt.flagName)
		})
	}
}

func TestCommandRules(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandRules(cliParams, ioStreams, "scafctl/lint")
	require.NotNil(t, cmd)
	assert.Equal(t, "rules", cmd.Use)
	assert.Contains(t, cmd.Aliases, "r")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	sf := cmd.Flags().Lookup("severity")
	require.NotNil(t, sf, "--severity flag should exist")
	cf := cmd.Flags().Lookup("category")
	require.NotNil(t, cf, "--category flag should exist")
}

func TestCommandExplainRule(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExplainRule(cliParams, ioStreams, "scafctl/lint")
	require.NotNil(t, cmd)
	assert.Equal(t, "explain <rule-name>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandExplainRule_RequiresArg(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExplainRule(cliParams, ioStreams, "scafctl/lint")
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "should fail without rule-name argument")
}

func BenchmarkCommandLint(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandLint(cliParams, ioStreams, "scafctl")
	}
}
