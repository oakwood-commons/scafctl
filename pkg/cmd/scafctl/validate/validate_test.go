// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandValidate(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidate(cliParams, streams, "scafctl")

	assert.Equal(t, "validate [name[@version]]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.Flags().Lookup("file"))
	assert.NotNil(t, cmd.Flags().Lookup("strict"))
	assert.NotNil(t, cmd.Flags().Lookup("output"))

	// The subcommands must be wired up.
	subs := map[string]bool{}
	for _, sub := range cmd.Commands() {
		subs[sub.Name()] = true
	}
	require.True(t, subs["resolver"], "validate must register the resolver subcommand")
	require.True(t, subs["solution"], "validate must register the solution subcommand")
	require.True(t, subs["schema"], "validate must register the schema subcommand")
}

func TestCommandValidate_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"

	cmd := CommandValidate(cliParams, streams, "mycli")
	assert.Contains(t, cmd.Short, "mycli")

	cmd.SetArgs([]string{"-f", "my-app"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mycli validate my-app")
}

func TestCommandValidate_DefaultsToValidateSolution(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(p, []byte(cleanSolution), 0o600))

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandValidate(cliParams, streams, "scafctl")
	cmd.SetArgs([]string{"-f", p, "-o", "quiet"})
	require.NoError(t, cmd.Execute())

	// Also supports -f - stdin on root validate
	stdinStreams := terminal.NewIOStreams(io.NopCloser(bytes.NewBufferString(cleanSolution)), &bytes.Buffer{}, &bytes.Buffer{}, false)
	cmdStdin := CommandValidate(cliParams, stdinStreams, "scafctl")
	cmdStdin.SetArgs([]string{"-f", "-", "-o", "quiet"})
	require.NoError(t, cmdStdin.Execute())
}
