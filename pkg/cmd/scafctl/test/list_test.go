// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandList(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandList(cliParams, ioStreams, "scafctl/test")

	require.NotNil(t, cmd)
	assert.Equal(t, "list [name[@version]]", cmd.Use)
	assert.Contains(t, cmd.Aliases, "ls")
	assert.Contains(t, cmd.Aliases, "l")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandList_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandList(cliParams, ioStreams, "scafctl/test")

	tests := []struct {
		name     string
		flagName string
	}{
		{"file", "file"},
		{"tests-path", "tests-path"},
		{"output", "output"},
		{"include-builtins", "include-builtins"},
		{"filter", "filter"},
		{"tag", "tag"},
		{"solution", "solution"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			assert.NotNil(t, f, "flag %q should exist", tt.flagName)
		})
	}
}

func TestCommandList_PreRunE_VersionedRef(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandList(cliParams, ioStreams, "scafctl/test")

	err := cmd.PreRunE(cmd, []string{"my-app@1.2.3"})
	assert.NoError(t, err)
}

func TestCommandList_PreRunE_BareNameRejected(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandList(cliParams, ioStreams, "scafctl/test")

	err := cmd.PreRunE(cmd, []string{"my-app"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bare names are not supported")
}

func TestCommandList_PreRunE_ConflictsWithFileFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandList(cliParams, ioStreams, "scafctl/test")
	require.NoError(t, cmd.Flags().Set("file", "solution.yaml"))

	err := cmd.PreRunE(cmd, []string{"my-app@1.0.0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot use both")
}

func TestCommandList_PreRunE_NoArgs(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandList(cliParams, ioStreams, "scafctl/test")

	err := cmd.PreRunE(cmd, []string{})
	assert.NoError(t, err)
}

func BenchmarkCommandList(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandList(cliParams, ioStreams, "scafctl/test")
	}
}
