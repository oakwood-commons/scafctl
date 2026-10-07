// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandTemplate(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTemplate(cliParams, ioStreams, "scafctl/eval")

	require.NotNil(t, cmd)
	assert.Equal(t, "template", cmd.Use)
	assert.Contains(t, cmd.Aliases, "tmpl")
	assert.Contains(t, cmd.Aliases, "t")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandTemplate_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTemplate(cliParams, ioStreams, "scafctl/eval")

	tests := []struct {
		name     string
		flagName string
		defVal   string
	}{
		{"template flag", "template", ""},
		{"template-file flag", "template-file", ""},
		{"var flag", "var", "[]"},
		{"data flag", "data", ""},
		{"file flag", "file", ""},
		{"show-refs flag", "show-refs", "false"},
		{"output flag", "output", "auto"},
		{"missing-key flag", "missing-key", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, f, "flag %q should exist", tt.flagName)
			assert.Equal(t, tt.defVal, f.DefValue, "flag %q default value", tt.flagName)
		})
	}
}

func TestCommandTemplate_MutuallyExclusiveFlags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTemplate(cliParams, ioStreams, "scafctl/eval")
	cmd.SetArgs([]string{"--template", "{{ .foo }}", "--template-file", "test.tmpl"})

	err := cmd.Execute()
	assert.Error(t, err, "should fail when both --template and --template-file are provided")
}

func TestCommandTemplate_Shorthands(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTemplate(cliParams, ioStreams, "scafctl/eval")

	shorthands := map[string]string{
		"t": "template",
		"v": "var",
		"o": "output",
	}

	for short, full := range shorthands {
		f := cmd.Flags().ShorthandLookup(short)
		require.NotNil(t, f, "shorthand -%s should exist", short)
		assert.Equal(t, full, f.Name, "shorthand -%s should map to --%s", short, full)
	}
}

func TestCommandTemplate_HidesFullKvxFlags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTemplate(cliParams, ioStreams, "scafctl/eval")

	assert.Nil(t, cmd.Flags().Lookup("interactive"), "--interactive should not be registered")
	assert.Nil(t, cmd.Flags().Lookup("expression"), "--expression should not be registered")
	assert.Nil(t, cmd.Flags().Lookup("where"), "--where should not be registered")
	assert.Nil(t, cmd.Flags().ShorthandLookup("i"), "-i shorthand should not be registered")
	assert.Nil(t, cmd.Flags().ShorthandLookup("e"), "-e shorthand should not be registered")
	assert.Nil(t, cmd.Flags().ShorthandLookup("w"), "-w shorthand should not be registered")
}

func TestCommandTemplate_OutputFormats(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		wantErr   bool
		wantOut   string
		wantNoOut bool
	}{
		{name: "yaml", format: "yaml", wantOut: "output"},
		{name: "json", format: "json", wantOut: "output"},
		{name: "text", format: "text", wantOut: "hello"},
		{name: "quiet", format: "quiet", wantNoOut: true},
		{name: "excluded table", format: "table", wantErr: true},
		{name: "excluded csv", format: "csv", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cliParams := settings.NewCliParams()
			ioStreams, out, _ := terminal.NewTestIOStreams()

			cmd := CommandTemplate(cliParams, ioStreams, "scafctl/eval")
			cmd.SetArgs([]string{"-t", "hello {{ .name }}", "-v", "name=world", "-o", tt.format})

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

func BenchmarkCommandTemplate(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandTemplate(cliParams, ioStreams, "scafctl/eval")
	}
}
