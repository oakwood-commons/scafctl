// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"encoding/json"
	"testing"

	"github.com/oakwood-commons/kvx/pkg/tui"
	cachelib "github.com/oakwood-commons/scafctl/pkg/cache"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCommandCache(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandCache(cliParams, ioStreams, "scafctl")
	require.NotNil(t, cmd)
	assert.Equal(t, "cache", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.RunE, "parent cache command needs a help-only RunE so NoArgs rejects unknown subcommands")
	subCmds := cmd.Commands()
	require.Len(t, subCmds, 2, "should have 2 subcommands: clear, info")
	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Name()
	}
	assert.Contains(t, cmdNames, "clear")
	assert.Contains(t, cmdNames, "info")
}

func TestCommandClear(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandClear(cliParams, ioStreams, "scafctl/cache")
	require.NotNil(t, cmd)
	assert.Equal(t, "clear", cmd.Use)
	assert.Contains(t, cmd.Aliases, "clean")
	assert.Contains(t, cmd.Aliases, "rm")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandClear_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandClear(cliParams, ioStreams, "scafctl/cache")
	tests := []struct {
		name     string
		flagName string
		defVal   string
	}{
		{"kind", "kind", ""},
		{"name", "name", ""},
		{"force", "force", "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, f, "flag %q should exist", tt.flagName)
			assert.Equal(t, tt.defVal, f.DefValue, "flag %q default value", tt.flagName)
		})
	}
}

func TestCommandInfo(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInfo(cliParams, ioStreams, "scafctl/cache")
	require.NotNil(t, cmd)
	assert.Equal(t, "info", cmd.Use)
	assert.Contains(t, cmd.Aliases, "status")
	assert.Contains(t, cmd.Aliases, "show")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func BenchmarkCommandCache(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandCache(cliParams, ioStreams, "scafctl")
	}
}

// TestCommandCache_UnknownSubcommandErrors verifies that an unknown
// subcommand errors (non-zero) while a bare invocation shows help and exits 0.
func TestCommandCache_UnknownSubcommandErrors(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCache(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandCache(cliParams, ioStreams, "scafctl")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}

// ── cache info output format tests ───────────────────────────────────────────

func TestCommandInfo_JSON(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, out, _ := terminal.NewTestIOStreams()
	cmd := CommandInfo(cliParams, ioStreams, "scafctl/cache")
	cmd.SetArgs([]string{"-o", "json"})

	require.NoError(t, cmd.Execute())

	var output cachelib.InfoOutput
	require.NoError(t, json.Unmarshal(out.Bytes(), &output), "output must be valid JSON InfoOutput")
	assert.Len(t, output.Caches, 3, "should have 3 cache entries")
	for _, item := range output.Caches {
		assert.NotEmpty(t, item.Name, "each cache must have a name")
		assert.NotEmpty(t, item.Path, "each cache must have a path")
		assert.NotEmpty(t, item.Description, "each cache must have a description")
	}
	assert.NotEmpty(t, output.TotalHuman, "totalHuman should be set")
}

func TestCommandInfo_YAML(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, out, _ := terminal.NewTestIOStreams()
	cmd := CommandInfo(cliParams, ioStreams, "scafctl/cache")
	cmd.SetArgs([]string{"-o", "yaml"})

	require.NoError(t, cmd.Execute())

	var output cachelib.InfoOutput
	require.NoError(t, yaml.Unmarshal(out.Bytes(), &output), "output must be valid YAML InfoOutput")
	assert.Len(t, output.Caches, 3, "should have 3 cache entries")
	assert.NotEmpty(t, output.TotalHuman, "totalHuman should be set")
}

func TestCommandInfo_Quiet(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, out, _ := terminal.NewTestIOStreams()
	cmd := CommandInfo(cliParams, ioStreams, "scafctl/cache")
	cmd.SetArgs([]string{"-o", "quiet"})

	require.NoError(t, cmd.Execute())
	assert.Empty(t, out.String(), "quiet mode should produce no stdout output")
}

func TestCommandInfo_DefaultFormat(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, out, _ := terminal.NewTestIOStreams()
	cmd := CommandInfo(cliParams, ioStreams, "scafctl/cache")
	// No -o flag: default auto format (non-TTY falls back to text)
	cmd.SetArgs([]string{})

	require.NoError(t, cmd.Execute())
	output := out.String()
	assert.Contains(t, output, "HTTP Cache", "default output should contain cache names")
	assert.Contains(t, output, "Build Cache")
	assert.Contains(t, output, "Artifact Cache")
}

func TestCommandInfo_TotalsSummaryOnStderr(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, _, errBuf := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	cmd := CommandInfo(cliParams, ioStreams, "scafctl/cache")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	require.NoError(t, cmd.Execute())
	stderr := errBuf.String()
	assert.Contains(t, stderr, "Total:", "stderr should contain totals summary")
	assert.Contains(t, stderr, "files)", "stderr should contain file count")
}

func TestCommandInfo_NoTotalsForStructuredFormats(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"json", "yaml", "csv"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			cliParams := settings.NewCliParams()
			ioStreams, _, errBuf := terminal.NewTestIOStreams()
			w := writer.New(ioStreams, cliParams)
			ctx := writer.WithWriter(t.Context(), w)

			cmd := CommandInfo(cliParams, ioStreams, "scafctl/cache")
			cmd.SetContext(ctx)
			cmd.SetArgs([]string{"-o", format})

			require.NoError(t, cmd.Execute())
			assert.NotContains(t, errBuf.String(), "Total:", "structured format %q should not print totals on stderr", format)
		})
	}
}

func TestCommandInfo_CSV(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, out, _ := terminal.NewTestIOStreams()
	cmd := CommandInfo(cliParams, ioStreams, "scafctl/cache")
	cmd.SetArgs([]string{"-o", "csv"})

	require.NoError(t, cmd.Execute())
	output := out.String()
	assert.NotEmpty(t, output, "CSV output should not be empty")
	assert.Contains(t, output, "HTTP Cache", "CSV output should contain cache data")
	assert.Contains(t, output, "Build Cache", "CSV output should contain all cache entries")
	assert.Contains(t, output, "Artifact Cache", "CSV output should contain all cache entries")
}

func TestCommandInfo_EmbedderBinaryName(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, out, _ := terminal.NewTestIOStreams()
	cmd := CommandInfo(cliParams, ioStreams, "mycli/cache")
	cmd.SetArgs([]string{"-o", "json"})

	require.NoError(t, cmd.Execute())

	var output cachelib.InfoOutput
	require.NoError(t, json.Unmarshal(out.Bytes(), &output))
	assert.Len(t, output.Caches, 3)
}

// ── Display schema tests ─────────────────────────────────────────────────────

func TestCacheInfoDisplaySchema_IsValidJSON(t *testing.T) {
	t.Parallel()
	assert.True(t, json.Valid(cacheInfoSchemaJSON), "cache_info_schema.json must be valid JSON")
}

func TestCacheInfoDisplaySchema_ParsesWithDisplay(t *testing.T) {
	t.Parallel()
	hints, ds, err := tui.ParseSchemaWithDisplay(cacheInfoSchemaJSON)
	require.NoError(t, err, "cache_info_schema.json must parse without error")
	assert.NotNil(t, hints, "should produce column hints")
	assert.NotNil(t, ds, "should produce display schema")
	assert.Equal(t, "name", ds.List.TitleField, "list titleField should be name")
}
