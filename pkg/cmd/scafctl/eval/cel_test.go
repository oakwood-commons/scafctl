// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"os"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestCommandCEL(t *testing.T) {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCEL(cliParams, ioStreams, "mycli/eval")

	require.NotNil(t, cmd)
	assert.Equal(t, "cel", cmd.Use)
	assert.Contains(t, cmd.Aliases, "c")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
	assert.Contains(t, cmd.Long, "mycli eval cel")
	assert.NotContains(t, cmd.Long, "scafctl eval cel")
}

func TestCommandCEL_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCEL(cliParams, ioStreams, "scafctl/eval")

	tests := []struct {
		name     string
		flagName string
		defVal   string
	}{
		{"expression flag", "expression", ""},
		{"var flag", "var", "[]"},
		{"data flag", "data", ""},
		{"file flag", "file", ""},
		{"output flag", "output", "auto"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, f, "flag %q should exist", tt.flagName)
			assert.Equal(t, tt.defVal, f.DefValue, "flag %q default value", tt.flagName)
		})
	}
}

func TestCommandCEL_ExpressionRequired(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCEL(cliParams, ioStreams, "scafctl/eval")
	cmd.SetArgs([]string{}) // no --expression

	err := cmd.Execute()
	assert.Error(t, err, "should fail without required --expression flag")
}

func TestCommandCEL_OutputShorthand(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCEL(cliParams, ioStreams, "scafctl/eval")

	shorthands := map[string]string{
		"v": "var",
		"d": "data",
		"f": "file",
		"o": "output",
	}
	for short, full := range shorthands {
		f := cmd.Flags().ShorthandLookup(short)
		require.NotNil(t, f, "shorthand -%s should exist", short)
		assert.Equal(t, full, f.Name, "shorthand -%s should map to --%s", short, full)
	}
}

func TestCommandCEL_DataAndFileShorthandExecution(t *testing.T) {
	t.Parallel()

	t.Run("-d inline JSON data", func(t *testing.T) {
		t.Parallel()
		cliParams := settings.NewCliParams()
		ioStreams, out, _ := terminal.NewTestIOStreams()

		cmd := CommandCEL(cliParams, ioStreams, "scafctl/eval")
		cmd.SetArgs([]string{"--expression", "int(_.count) + 1", "-d", `{"count": 4}`})

		require.NoError(t, cmd.Execute())
		assert.Equal(t, "5\n", out.String())
	})

	t.Run("-f data file", func(t *testing.T) {
		t.Parallel()
		dataPath := t.TempDir() + "/ctx.json"
		require.NoError(t, writeTestFile(dataPath, `{"name": "demo"}`))

		cliParams := settings.NewCliParams()
		ioStreams, out, _ := terminal.NewTestIOStreams()

		cmd := CommandCEL(cliParams, ioStreams, "scafctl/eval")
		cmd.SetArgs([]string{"--expression", "_.name", "-f", dataPath})

		require.NoError(t, cmd.Execute())
		assert.Equal(t, "demo\n", out.String())
	})
}

func TestCommandCEL_HidesFullKvxFlags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCEL(cliParams, ioStreams, "scafctl/eval")

	// The data-only subset must not expose the interactive/where/expression
	// filter flags; eval cel returns a single object.
	assert.Nil(t, cmd.Flags().Lookup("interactive"), "--interactive should not be registered")
	assert.Nil(t, cmd.Flags().Lookup("where"), "--where should not be registered")
	assert.Nil(t, cmd.Flags().ShorthandLookup("i"), "-i shorthand should not be registered")
	assert.Nil(t, cmd.Flags().ShorthandLookup("e"), "-e shorthand should not be registered")
	assert.Nil(t, cmd.Flags().ShorthandLookup("w"), "-w shorthand should not be registered")
}

func TestCommandCEL_OutputFormats(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		wantErr   bool
		wantOut   string
		wantNoOut bool
	}{
		{name: "yaml", format: "yaml", wantOut: "result"},
		{name: "json", format: "json", wantOut: "result"},
		{name: "text", format: "text", wantOut: "3"},
		{name: "quiet", format: "quiet", wantNoOut: true},
		{name: "excluded table", format: "table", wantErr: true},
		{name: "excluded csv", format: "csv", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cliParams := settings.NewCliParams()
			ioStreams, out, _ := terminal.NewTestIOStreams()

			cmd := CommandCEL(cliParams, ioStreams, "scafctl/eval")
			cmd.SetArgs([]string{"--expression", "1 + 2", "-o", tt.format})

			err := cmd.Execute()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid output format")
				return
			}
			require.NoError(t, err)
			if tt.wantNoOut {
				assert.Empty(t, out.String())
				return
			}
			assert.Contains(t, out.String(), tt.wantOut)
		})
	}
}

func BenchmarkCommandCEL(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandCEL(cliParams, ioStreams, "scafctl/eval")
	}
}
