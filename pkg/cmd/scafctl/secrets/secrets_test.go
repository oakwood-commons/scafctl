// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/adrg/xdg"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandSecrets(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandSecrets(cliParams, ioStreams, "scafctl")
	require.NotNil(t, cmd)
	assert.Equal(t, "secrets", cmd.Use)
	assert.Contains(t, cmd.Aliases, "secret")
	assert.NotEmpty(t, cmd.Short)
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.RunE, "parent secrets command needs a help-only RunE so NoArgs rejects unknown subcommands")
	subCmds := cmd.Commands()
	require.Len(t, subCmds, 8, "should have 8 subcommands")
	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Name()
	}
	assert.Contains(t, cmdNames, "list")
	assert.Contains(t, cmdNames, "get")
	assert.Contains(t, cmdNames, "set")
	assert.Contains(t, cmdNames, "delete")
	assert.Contains(t, cmdNames, "exists")
	assert.Contains(t, cmdNames, "export")
	assert.Contains(t, cmdNames, "import")
	assert.Contains(t, cmdNames, "rotate")
}

func TestCommandDelete(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "delete <name>", cmd.Use)
	assert.Contains(t, cmd.Aliases, "rm")
	assert.Contains(t, cmd.Aliases, "remove")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	f := cmd.Flags().Lookup("force")
	require.NotNil(t, f, "--force flag should exist")
	af := cmd.Flags().Lookup("all")
	require.NotNil(t, af, "--all flag should exist")
}

func TestCommandDelete_RequiresArg(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/secrets")
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "should fail without name argument")
}

func TestCommandExists(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExists(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "exists <name>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	af := cmd.Flags().Lookup("all")
	require.NotNil(t, af, "--all flag should exist")
}

func TestCommandExport(t *testing.T) {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandExport(cliParams, ioStreams, "mycli/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "export", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	assert.Contains(t, cmd.Long, "mycli secrets export --output-file")

	outputFileFlag := cmd.Flags().Lookup("output-file")
	require.NotNil(t, outputFileFlag, "--output-file flag should exist")
	assert.Empty(t, outputFileFlag.Shorthand, "--output-file must not claim -o shorthand")

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag, "--output format flag should exist")
	assert.Equal(t, "yaml", outputFlag.DefValue)
	assert.Equal(t, "o", outputFlag.Shorthand)

	assert.Nil(t, cmd.Flags().Lookup("format"), "--format should be replaced by -o/--output")
	assert.Nil(t, cmd.Flags().ShorthandLookup("f"), "-f shorthand should not exist on secrets export")

	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `required flag(s) "output-file" not set`)
}

func TestRunExport_ExecutionAndValidation(t *testing.T) {
	// Uses XDG_DATA_HOME + a Base64-encoded 32-byte SCAFCTL_SECRET_KEY for the
	// environment keyring fallback; cannot run in parallel due to Setenv + xdg.Reload.
	tmpDir := t.TempDir()
	t.Cleanup(func() { xdg.Reload() })
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("SCAFCTL_SECRET_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	xdg.Reload()

	cliParams := settings.NewCliParams()
	ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	// Seed a user secret via CommandSet -f <file>.
	secretIn := filepath.Join(tmpDir, "token.txt")
	require.NoError(t, os.WriteFile(secretIn, []byte("s3cr3t-value"), 0o600))
	setCmd := CommandSet(cliParams, ioStreams, "scafctl/secrets")
	setCmd.SetContext(ctx)
	setCmd.SetArgs([]string{"api-token", "-f", secretIn})
	require.NoError(t, setCmd.Execute())

	// Seed default-namespace and custom-embedder internal secrets directly in the store.
	store, err := newStoreFromContext(ctx)
	require.NoError(t, err)
	require.NoError(t, store.Set(ctx, "scafctl.auth.token", []byte("default-internal-tok")))
	require.NoError(t, store.Set(ctx, "mycli.auth.token", []byte("embedder-internal-tok")))

	t.Run("empty output-file returns InvalidInput", func(t *testing.T) {
		err := runExport(ctx, cliParams, exportRunOptions{
			outputFile: "",
			format:     "yaml",
		})
		require.Error(t, err)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.Contains(t, err.Error(), "--output-file")
	})

	t.Run("invalid output format returns InvalidInput before confirmation", func(t *testing.T) {
		calledConfirm := false
		err := runExport(ctx, cliParams, exportRunOptions{
			outputFile: filepath.Join(tmpDir, "bad.txt"),
			format:     "table",
			confirmPlaintext: func(context.Context, bool) (bool, error) {
				calledConfirm = true
				return true, nil
			},
		})
		require.Error(t, err)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.Contains(t, err.Error(), "unsupported output format")
		assert.False(t, calledConfirm, "invalid -o format must fail before prompting")
	})

	t.Run("exports JSON and YAML files with 0600 permissions and excludes embedder internal secrets", func(t *testing.T) {
		embedderParams := settings.NewCliParams()
		embedderParams.BinaryName = "mycli"

		for _, format := range []string{"json", "yaml"} {
			outBuf.Reset()
			errBuf.Reset()
			outPath := filepath.Join(tmpDir, "exported."+format)

			err := runExport(ctx, embedderParams, exportRunOptions{
				outputFile: outPath,
				format:     format,
				confirmPlaintext: func(context.Context, bool) (bool, error) {
					return true, nil
				},
			})
			require.NoError(t, err)

			if runtime.GOOS != "windows" {
				info, statErr := os.Stat(outPath)
				require.NoError(t, statErr)
				assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			}

			data, readErr := os.ReadFile(outPath)
			require.NoError(t, readErr)
			assert.Contains(t, string(data), "api-token")
			assert.Contains(t, string(data), "s3cr3t-value")
			assert.NotContains(t, string(data), "mycli.auth.token")
			assert.NotContains(t, string(data), "embedder-internal-tok")
			assert.NotContains(t, string(data), "scafctl.auth.token")
			assert.NotContains(t, string(data), "default-internal-tok")
			if format == "json" {
				assert.Contains(t, string(data), `"secrets"`)
			} else {
				assert.Contains(t, string(data), "secrets:")
			}
		}

		// With --all, both embedder and default internal secrets are included.
		allPath := filepath.Join(tmpDir, "exported-all.json")
		err := runExport(ctx, embedderParams, exportRunOptions{
			outputFile: allPath,
			format:     "json",
			all:        true,
			confirmPlaintext: func(context.Context, bool) (bool, error) {
				return true, nil
			},
		})
		require.NoError(t, err)
		allData, readErr := os.ReadFile(allPath)
		require.NoError(t, readErr)
		assert.Contains(t, string(allData), "api-token")
		assert.Contains(t, string(allData), "mycli.auth.token")
		assert.Contains(t, string(allData), "scafctl.auth.token")
	})
}

func TestCommandGet(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandGet(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "get <name>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandImport(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandImport(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "import <file>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandList(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandRotate(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandRotate(cliParams, ioStreams, "scafctl/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "rotate", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandSet(t *testing.T) {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandSet(cliParams, ioStreams, "mycli/secrets")
	require.NotNil(t, cmd)
	assert.Equal(t, "set <name> [value]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	assert.Contains(t, cmd.Long, "mycli secrets set name -f /path/to/file")
	assert.NotContains(t, cmd.Long, "scafctl secrets set")

	fileFlag := cmd.Flags().ShorthandLookup("f")
	require.NotNil(t, fileFlag, "-f shorthand should exist on secrets set")
	assert.Equal(t, "file", fileFlag.Name)
}

func BenchmarkCommandSecrets(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandSecrets(cliParams, ioStreams, "scafctl")
	}
}

// TestCommandSecrets_UnknownSubcommandErrors verifies that an unknown
// subcommand errors (non-zero) while a bare invocation shows help and exits 0.
func TestCommandSecrets_UnknownSubcommandErrors(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandSecrets(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandSecrets(cliParams, ioStreams, "scafctl")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}
