// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os"
	"path/filepath"
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
	assert.Equal(t, "test [reference]", cmd.Use)
	assert.Contains(t, cmd.Aliases, "t")
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.RunE, "parent test command should have RunE (defaults to functional)")

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

func TestCommandTest_DefaultsToFunctional(t *testing.T) {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTest(cliParams, ioStreams, "mycli")
	assert.NotNil(t, cmd.RunE, "parent test command should have RunE that defaults to functional")
	assert.Contains(t, cmd.Long, "without a subcommand")
}

func TestCommandTest_RunE_NoSolutionFile(t *testing.T) {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTest(cliParams, ioStreams, "mycli")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no solution path provided")

	// Verify EntryPointSettings.Path was set via filepath.Join
	assert.Equal(t, filepath.Join("mycli", "test"), cliParams.EntryPointSettings.Path)
	assert.Equal(t, "mycli", cliParams.BinaryName)
}

func TestCommandTest_RegistersFunctionalFlagsAndValidatesFileFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, errBuf := terminal.NewTestIOStreams()

	cmd := CommandTest(cliParams, ioStreams, "mycli")
	for _, name := range []string{"file", "tests-path", "filter", "tag", "solution", "dry-run", "sequential", "concurrency", "fail-fast", "update-snapshots", "watch"} {
		require.NotNilf(t, cmd.Flags().Lookup(name), "root test command must register --%s", name)
	}
	assert.Empty(t, cmd.Flags().Lookup("solution").Shorthand, "--solution glob filter must not claim -s")

	// Rejects catalog ref in -f with positional hint using embedder name
	cmd.SetArgs([]string{"-f", "my-app"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error()+errBuf.String(), "mycli test my-app")

	// Runs functional dry-run via -f on root test
	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	solYAML := "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: root-test\n  version: 1.0.0\nspec:\n  testing:\n    cases:\n      smoke:\n        command: [render, solution]\n        assertions:\n          - expression: __exitCode == 0\n"
	require.NoError(t, os.WriteFile(solFile, []byte(solYAML), 0o600))

	streams2, _, _ := terminal.NewTestIOStreams()
	cmd2 := CommandTest(cliParams, streams2, "mycli")
	cmd2.SetArgs([]string{"-f", solFile, "--dry-run", "-o", "quiet"})
	require.NoError(t, cmd2.Execute())
}

func BenchmarkCommandTest(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandTest(cliParams, ioStreams, "scafctl")
	}
}
