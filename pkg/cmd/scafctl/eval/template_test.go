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

func BenchmarkCommandTemplate(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandTemplate(cliParams, ioStreams, "scafctl/eval")
	}
}
