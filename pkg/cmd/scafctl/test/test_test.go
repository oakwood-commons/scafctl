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

func TestCommandTest(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTest(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "test", cmd.Use)
	assert.Contains(t, cmd.Aliases, "t")
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)

	subCmds := cmd.Commands()
	require.Len(t, subCmds, 3, "should have 3 subcommands: functional, init, list")

	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Name()
	}
	assert.Contains(t, cmdNames, "functional")
	assert.Contains(t, cmdNames, "init")
	assert.Contains(t, cmdNames, "list")
}

func TestCommandTest_NoRunE(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTest(cliParams, ioStreams, "scafctl")
	assert.Nil(t, cmd.RunE, "parent test command should not have RunE")
}

func BenchmarkCommandTest(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandTest(cliParams, ioStreams, "scafctl")
	}
}
