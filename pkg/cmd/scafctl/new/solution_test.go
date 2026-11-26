// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package newcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandNew(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandNew(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "new", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)

	subCmds := cmd.Commands()
	require.Len(t, subCmds, 1, "should have 1 subcommand: solution")

	names := make([]string, len(subCmds))
	for i, c := range subCmds {
		names[i] = c.Name()
	}
	assert.Contains(t, names, "solution")
}

func TestCommandNew_HasHelpOnlyRunE(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandNew(cliParams, ioStreams, "scafctl")
	assert.NotNil(t, cmd.RunE, "parent new command needs a help-only RunE so NoArgs rejects unknown subcommands")
	assert.NotNil(t, cmd.Args, "parent new command must reject unknown subcommands via Args")
}

func TestCommandSolution(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")

	require.NotNil(t, cmd)
	assert.Equal(t, "solution [name]", cmd.Use)
	assert.Equal(t, []string{"sol", "s", "solutions"}, cmd.Aliases)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandSolution_EmbedderBinaryName(t *testing.T) {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandSolution(cliParams, ioStreams, "mycli/new")

	assert.Contains(t, cmd.Long, "mycli new solution")
	assert.NotContains(t, cmd.Long, "scafctl new solution")
}

func TestCommandSolution_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")

	tests := []struct {
		name     string
		flagName string
		defVal   string
	}{
		{"name flag", "name", ""},
		{"description flag", "description", ""},
		{"version flag", "version", ""},
		{"features flag", "features", ""},
		{"providers flag", "providers", ""},
		{"output-file flag", "output-file", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, f, "flag %q should exist", tt.flagName)
			assert.Equal(t, tt.defVal, f.DefValue, "flag %q default value", tt.flagName)
		})
	}

	assert.Nil(t, cmd.Flags().Lookup("output"), "--output must not be registered on new solution")
}

func TestCommandSolution_RequiredFlags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	assert.Error(t, err, "should fail without required --description flag")
}

func TestCommandSolution_Shorthands(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")

	shorthands := map[string]string{
		"n": "name",
		"d": "description",
	}

	for short, full := range shorthands {
		f := cmd.Flags().ShorthandLookup(short)
		require.NotNil(t, f, "shorthand -%s should exist", short)
		assert.Equal(t, full, f.Name, "shorthand -%s should map to --%s", short, full)
	}

	assert.Nil(t, cmd.Flags().ShorthandLookup("o"), "-o shorthand must not exist on new solution")
}

func TestCommandSolution_Execution(t *testing.T) {
	t.Parallel()

	t.Run("positional name and -d shorthand", func(t *testing.T) {
		t.Parallel()
		cliParams := settings.NewCliParams()
		ioStreams, out, _ := terminal.NewTestIOStreams()

		cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")
		cmd.SetArgs([]string{"my-deploy", "-d", "Deploy to Kubernetes"})

		require.NoError(t, cmd.Execute())
		assert.Contains(t, out.String(), "name: my-deploy")
		assert.Contains(t, out.String(), "Deploy to Kubernetes")
		assert.Equal(t, filepath.Join("scafctl/new", "solution"), cliParams.EntryPointSettings.Path)
	})

	t.Run("flag -n and -d shorthand", func(t *testing.T) {
		t.Parallel()
		cliParams := settings.NewCliParams()
		ioStreams, out, _ := terminal.NewTestIOStreams()

		cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")
		cmd.SetArgs([]string{"-n", "my-deploy", "-d", "Deploy to Kubernetes"})

		require.NoError(t, cmd.Execute())
		assert.Contains(t, out.String(), "name: my-deploy")
	})

	t.Run("matching positional and --name succeeds", func(t *testing.T) {
		t.Parallel()
		cliParams := settings.NewCliParams()
		ioStreams, out, _ := terminal.NewTestIOStreams()

		cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")
		cmd.SetArgs([]string{"my-deploy", "-n", "my-deploy", "-d", "Deploy"})

		require.NoError(t, cmd.Execute())
		assert.Contains(t, out.String(), "name: my-deploy")
	})

	t.Run("missing both positional name and --name returns InvalidInput", func(t *testing.T) {
		t.Parallel()
		cliParams := settings.NewCliParams()
		ioStreams, _, errBuf := terminal.NewTestIOStreams()

		cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")
		cmd.SetArgs([]string{"-d", "Deploy"})

		err := cmd.Execute()
		require.Error(t, err)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.Contains(t, errBuf.String(), "solution name is required")
	})

	t.Run("conflicting positional name and --name returns InvalidInput", func(t *testing.T) {
		t.Parallel()
		cliParams := settings.NewCliParams()
		ioStreams, _, errBuf := terminal.NewTestIOStreams()

		cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")
		cmd.SetArgs([]string{"pos-app", "-n", "flag-app", "-d", "Deploy"})

		err := cmd.Execute()
		require.Error(t, err)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.Contains(t, errBuf.String(), "conflicting solution names")
	})

	t.Run("writes to --output-file", func(t *testing.T) {
		t.Parallel()
		cliParams := settings.NewCliParams()
		ioStreams, out, _ := terminal.NewTestIOStreams()
		outFile := filepath.Join(t.TempDir(), "solution.yaml")

		cmd := CommandSolution(cliParams, ioStreams, "scafctl/new")
		cmd.SetArgs([]string{"file-app", "-d", "Written to disk", "--output-file", outFile})

		require.NoError(t, cmd.Execute())
		assert.Contains(t, out.String(), "Solution written to")
		data, err := os.ReadFile(outFile)
		require.NoError(t, err)
		assert.Contains(t, string(data), "name: file-app")
	})

	t.Run("solutions alias resolves to solution subcommand", func(t *testing.T) {
		t.Parallel()
		cliParams := settings.NewCliParams()
		ioStreams, out, _ := terminal.NewTestIOStreams()

		parent := CommandNew(cliParams, ioStreams, "scafctl")
		parent.SetArgs([]string{"solutions", "alias-app", "-d", "Via plural alias"})

		require.NoError(t, parent.Execute())
		assert.Contains(t, out.String(), "name: alias-app")
	})
}

func BenchmarkCommandNew(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandNew(cliParams, ioStreams, "scafctl")
	}
}

func BenchmarkCommandSolution(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandSolution(cliParams, ioStreams, "scafctl/new")
	}
}

// TestCommandNew_UnknownSubcommandErrors verifies that an unknown subcommand
// errors (non-zero) while a bare invocation shows help and exits 0.
func TestCommandNew_UnknownSubcommandErrors(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandNew(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandNew(cliParams, ioStreams, "scafctl")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}
