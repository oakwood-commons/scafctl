// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package vendor

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandVendor(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandVendor(cliParams, ioStreams, "scafctl")
	require.NotNil(t, cmd)
	assert.Equal(t, "vendor", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.True(t, cmd.SilenceUsage)
	assert.Nil(t, cmd.RunE, "parent vendor command should not have RunE")
	subCmds := cmd.Commands()
	require.Len(t, subCmds, 1, "should have 1 subcommand: update")
	assert.Equal(t, "update", subCmds[0].Name())
}

func TestCommandUpdate(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandUpdate(cliParams, ioStreams, "scafctl/vendor")
	require.NotNil(t, cmd)
	assert.Equal(t, "update [solution-path]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandUpdate_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandUpdate(cliParams, ioStreams, "scafctl/vendor")
	tests := []struct {
		name     string
		flagName string
		defVal   string
	}{
		{"dependency", "dependency", "[]"},
		{"dry-run", "dry-run", "false"},
		{"lock-only", "lock-only", "false"},
		{"pre-release", "pre-release", "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, f, "flag %q should exist", tt.flagName)
			assert.Equal(t, tt.defVal, f.DefValue, "flag %q default value", tt.flagName)
		})
	}
}

func BenchmarkCommandVendor(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandVendor(cliParams, ioStreams, "scafctl")
	}
}
