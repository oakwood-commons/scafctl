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

func TestCommandInit(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandInit(cliParams, ioStreams, "scafctl/test")

	require.NotNil(t, cmd)
	assert.Equal(t, "init [name[@version]]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandInit_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandInit(cliParams, ioStreams, "scafctl/test")

	f := cmd.Flags().Lookup("file")
	assert.NotNil(t, f, "flag 'file' should exist")
}

func TestCommandInit_PreRunE_VersionedRef(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandInit(cliParams, ioStreams, "scafctl/test")

	cmd.SetArgs([]string{"my-app@1.2.3"})
	// PreRunE runs as part of Execute but RunE will fail without a real solution.
	// We test PreRunE directly by checking it doesn't error on valid input.
	err := cmd.PreRunE(cmd, []string{"my-app@1.2.3"})
	assert.NoError(t, err)
}

func TestCommandInit_PreRunE_BareNameRejected(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandInit(cliParams, ioStreams, "scafctl/test")

	err := cmd.PreRunE(cmd, []string{"my-app"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bare names are not supported")
}

func TestCommandInit_PreRunE_ConflictsWithFileFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandInit(cliParams, ioStreams, "scafctl/test")
	require.NoError(t, cmd.Flags().Set("file", "solution.yaml"))

	err := cmd.PreRunE(cmd, []string{"my-app@1.0.0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot use both")
}

func TestCommandInit_PreRunE_NoArgs(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandInit(cliParams, ioStreams, "scafctl/test")

	err := cmd.PreRunE(cmd, []string{})
	assert.NoError(t, err)
}

func BenchmarkCommandInit(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandInit(cliParams, ioStreams, "scafctl/test")
	}
}
