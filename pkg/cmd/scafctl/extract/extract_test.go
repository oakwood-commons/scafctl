// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package extract

import (
	"bytes"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandExtract_Subcommands(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}

	cmd := CommandExtract(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "extract", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)

	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names[subBundle], "extract should have bundle subcommand")
}

func TestCommandExtract_BareShowsHelp(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}

	cmd := CommandExtract(cliParams, ioStreams, "scafctl")
	require.NotNil(t, cmd.RunE, "bare extract should have RunE that shows help")

	var out, errBuf bytes.Buffer
	cmd.SetArgs([]string{})
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceErrors = true
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Usage:", "bare extract must render help")
}

func TestCommandExtract_UnknownSubcommandErrors(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}

	cmd := CommandExtract(cliParams, ioStreams, "scafctl")
	var out, errBuf bytes.Buffer
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestCommandExtract_ExampleUsesBinaryName(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}

	cmd := CommandExtract(cliParams, ioStreams, "mycli")
	assert.Contains(t, cmd.Example, "mycli extract bundle")
}

func BenchmarkCommandExtract(b *testing.B) {
	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandExtract(cliParams, ioStreams, "scafctl")
	}
}
