// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package diff

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandDiffBundle(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandDiffBundle(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "bundle <ref-a> <ref-b>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.RunE)
	assert.Contains(t, cmd.Long, "scafctl diff bundle")
}

func TestCommandDiffBundle_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandDiffBundle(cliParams, ioStreams, "scafctl")

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

func TestCommandDiffBundle_RequiresExactlyTwoArgs(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	// No args should fail
	cmd := CommandDiffBundle(cliParams, ioStreams, "scafctl")
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)

	// One arg should fail
	cmd2 := CommandDiffBundle(cliParams, ioStreams, "scafctl")
	cmd2.SilenceErrors = true
	cmd2.SetArgs([]string{"ref1"})
	err = cmd2.Execute()
	assert.Error(t, err)

	// Three args should fail
	cmd3 := CommandDiffBundle(cliParams, ioStreams, "scafctl")
	cmd3.SilenceErrors = true
	cmd3.SetArgs([]string{"ref1", "ref2", "ref3"})
	err = cmd3.Execute()
	assert.Error(t, err)
}

func BenchmarkCommandDiffBundle(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandDiffBundle(cliParams, ioStreams, "scafctl")
	}
}
