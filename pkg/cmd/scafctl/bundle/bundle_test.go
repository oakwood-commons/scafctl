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
	assert.Nil(t, cmd.RunE, "parent bundle command should not have RunE, it is a group command")
}

func TestCommandBundle_Subcommands(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandBundle(cliParams, ioStreams, "")

	subCmds := cmd.Commands()
	require.Len(t, subCmds, 3, "should have 3 subcommands: verify, diff, extract")

	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Name()
	}
	assert.Contains(t, cmdNames, "verify")
	assert.Contains(t, cmdNames, "diff")
	assert.Contains(t, cmdNames, "extract")
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

func TestCommandDiff(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandDiff(cliParams, ioStreams, "")

	require.NotNil(t, cmd)
	assert.Equal(t, "diff <ref-a> <ref-b>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandDiff_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandDiff(cliParams, ioStreams, "")

	filesOnlyFlag := cmd.Flags().Lookup("files-only")
	require.NotNil(t, filesOnlyFlag, "files-only flag should exist")
	assert.Equal(t, "false", filesOnlyFlag.DefValue)

	solutionOnlyFlag := cmd.Flags().Lookup("solution-only")
	require.NotNil(t, solutionOnlyFlag, "solution-only flag should exist")
	assert.Equal(t, "false", solutionOnlyFlag.DefValue)

	ignoreFlag := cmd.Flags().Lookup("ignore")
	require.NotNil(t, ignoreFlag, "ignore flag should exist")
	assert.Equal(t, "[]", ignoreFlag.DefValue)

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag, "output flag should exist")

	interactiveFlag := cmd.Flags().Lookup("interactive")
	require.NotNil(t, interactiveFlag, "interactive flag should exist")

	expressionFlag := cmd.Flags().Lookup("expression")
	require.NotNil(t, expressionFlag, "expression flag should exist")
}

func TestCommandDiff_RequiresExactlyTwoArgs(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	// No args should fail
	cmd := CommandDiff(cliParams, ioStreams, "")
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)

	// One arg should fail
	cmd2 := CommandDiff(cliParams, ioStreams, "")
	cmd2.SilenceErrors = true
	cmd2.SetArgs([]string{"ref1"})
	err = cmd2.Execute()
	assert.Error(t, err)

	// Three args should fail
	cmd3 := CommandDiff(cliParams, ioStreams, "")
	cmd3.SilenceErrors = true
	cmd3.SetArgs([]string{"ref1", "ref2", "ref3"})
	err = cmd3.Execute()
	assert.Error(t, err)
}

func TestCommandExtract(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandExtract(cliParams, ioStreams, "")

	require.NotNil(t, cmd)
	assert.Equal(t, "extract <artifact-ref>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandExtract_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandExtract(cliParams, ioStreams, "")

	outputDirFlag := cmd.Flags().Lookup("output-dir")
	require.NotNil(t, outputDirFlag, "output-dir flag should exist")
	assert.Equal(t, ".", outputDirFlag.DefValue)

	resolverFlag := cmd.Flags().Lookup("resolver")
	require.NotNil(t, resolverFlag, "resolver flag should exist")
	assert.Equal(t, "[]", resolverFlag.DefValue)

	actionFlag := cmd.Flags().Lookup("action")
	require.NotNil(t, actionFlag, "action flag should exist")
	assert.Equal(t, "[]", actionFlag.DefValue)

	includeFlag := cmd.Flags().Lookup("include")
	require.NotNil(t, includeFlag, "include flag should exist")
	assert.Equal(t, "[]", includeFlag.DefValue)

	listOnlyFlag := cmd.Flags().Lookup("list-only")
	require.NotNil(t, listOnlyFlag, "list-only flag should exist")
	assert.Equal(t, "false", listOnlyFlag.DefValue)

	flattenFlag := cmd.Flags().Lookup("flatten")
	require.NotNil(t, flattenFlag, "flatten flag should exist")
	assert.Equal(t, "false", flattenFlag.DefValue)
}

func TestCommandExtract_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	// No args should fail
	cmd := CommandExtract(cliParams, ioStreams, "")
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)

	// Two args should fail
	cmd2 := CommandExtract(cliParams, ioStreams, "")
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

func BenchmarkCommandDiff(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandDiff(cliParams, ioStreams, "")
	}
}

func BenchmarkCommandExtract(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandExtract(cliParams, ioStreams, "")
	}
}
