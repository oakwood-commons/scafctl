// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandBundle(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandBundle(cliParams, ioStreams, "")

	require.NotNil(t, cmd)
	assert.Equal(t, "bundle", cmd.Use)
	assert.Equal(t, []string{"bun"}, cmd.Aliases)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)
	// The parent has a RunE (shows help when bare) plus Args: NoArgs so that
	// unknown subcommands (e.g. the hard-removed 'bundle diff') error instead
	// of falling back to help/exit-0. A non-runnable parent would return
	// flag.ErrHelp before args are validated, swallowing the error.
	assert.NotNil(t, cmd.RunE, "parent bundle command needs a RunE so NoArgs is enforced on unknown subcommands")
	assert.NotNil(t, cmd.Args, "parent bundle command must reject unknown subcommands via Args")
}

// TestCommandBundle_UnknownSubcommandErrors verifies that an unknown
// subcommand errors (non-zero) while a bare invocation shows help and exits 0.
func TestCommandBundle_UnknownSubcommandErrors(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	// Unknown subcommand must error.
	cmd := CommandBundle(cliParams, ioStreams, "")
	cmd.SetArgs([]string{"diff", "a", "b"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	// Bare invocation shows help and does not error.
	cmd2 := CommandBundle(cliParams, ioStreams, "")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}

func TestCommandBundle_Subcommands(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandBundle(cliParams, ioStreams, "")

	subCmds := cmd.Commands()
	require.Len(t, subCmds, 1, "should have 1 subcommand: verify")

	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Name()
	}
	assert.Contains(t, cmdNames, "verify")
}

func TestCommandVerify(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandVerify(cliParams, ioStreams, "")

	require.NotNil(t, cmd)
	assert.Equal(t, "verify <artifact-ref>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandVerify_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandVerify(cliParams, ioStreams, "")

	paramsFlag := cmd.Flags().Lookup("params")
	require.NotNil(t, paramsFlag, "params flag should exist")
	assert.Equal(t, "", paramsFlag.DefValue)

	paramsFileFlag := cmd.Flags().Lookup("params-file")
	require.NotNil(t, paramsFileFlag, "params-file flag should exist")
	assert.Equal(t, "", paramsFileFlag.DefValue)

	strictFlag := cmd.Flags().Lookup("strict")
	require.NotNil(t, strictFlag, "strict flag should exist")
	assert.Equal(t, "false", strictFlag.DefValue)
}

func TestCommandVerify_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	// No args should fail
	cmd := CommandVerify(cliParams, ioStreams, "")
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)

	// Two args should fail
	cmd2 := CommandVerify(cliParams, ioStreams, "")
	cmd2.SilenceErrors = true
	cmd2.SetArgs([]string{"ref1", "ref2"})
	err = cmd2.Execute()
	assert.Error(t, err)
}

func TestHasPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		s        string
		prefix   string
		expected bool
	}{
		{name: "matching prefix", s: "glob:pattern", prefix: "glob:", expected: true},
		{name: "plugin prefix", s: "plugin:myplugin", prefix: "plugin:", expected: true},
		{name: "no match", s: "static/path", prefix: "glob:", expected: false},
		{name: "empty string", s: "", prefix: "glob:", expected: false},
		{name: "empty prefix", s: "anything", prefix: "", expected: true},
		{name: "both empty", s: "", prefix: "", expected: true},
		{name: "exact match", s: "glob:", prefix: "glob:", expected: true},
		{name: "prefix longer than string", s: "gl", prefix: "glob:", expected: false},
		{name: "pattern prefix", s: "pattern matched", prefix: "pattern ", expected: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, hasPrefix(tc.s, tc.prefix))
		})
	}
}

// Benchmarks

func BenchmarkCommandBundle(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandBundle(cliParams, ioStreams, "")
	}
}

func BenchmarkCommandVerify(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandVerify(cliParams, ioStreams, "")
	}
}
