// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmplfunction

import (
	"bytes"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandFunctions(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandFunctions(cliParams, ioStreams, "scafctl/get/template")
	require.NotNil(t, cmd)
	assert.Equal(t, "functions", cmd.Use)
	assert.Empty(t, cmd.Aliases)
	assert.False(t, cmd.Hidden)
	assert.Empty(t, cmd.Deprecated)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandGotmplFunctionDeprecated(t *testing.T) {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "scafctl"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandGotmplFunctionDeprecated(cliParams, ioStreams, "scafctl/get")
	require.NotNil(t, cmd)
	assert.Equal(t, "go-template-functions", cmd.Use)
	assert.Contains(t, cmd.Aliases, "gotmpl-funcs")
	assert.Contains(t, cmd.Aliases, "gotmpl")
	assert.Contains(t, cmd.Aliases, "gtf")
	assert.True(t, cmd.Hidden)
	assert.NotEmpty(t, cmd.Deprecated)
	assert.Contains(t, cmd.Deprecated, "get template functions")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandFunctions_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandFunctions(cliParams, ioStreams, "scafctl/get/template")
	flags := []string{"output", "interactive", "expression", "custom", "sprig"}
	for _, name := range flags {
		t.Run(name, func(t *testing.T) {
			f := cmd.Flags().Lookup(name)
			assert.NotNil(t, f, "flag %q should exist", name)
		})
	}
}

func TestCommandFunctions_Shorthands(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandFunctions(cliParams, ioStreams, "scafctl/get/template")
	shorthands := map[string]string{
		"o": "output",
		"i": "interactive",
		"e": "expression",
	}
	for short, full := range shorthands {
		f := cmd.Flags().ShorthandLookup(short)
		require.NotNil(t, f, "shorthand -%s should exist", short)
		assert.Equal(t, full, f.Name, "shorthand -%s should map to --%s", short, full)
	}
}

func TestCommandFunctions_NoSubcommands(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandFunctions(cliParams, ioStreams, "scafctl/get/template")
	assert.Len(t, cmd.Commands(), 0, "functions should have no subcommands")
}

func BenchmarkCommandFunctions(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandFunctions(cliParams, ioStreams, "scafctl/get/template")
	}
}

func TestCommandFunctions_SearchFlag(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandFunctions(cliParams, ioStreams, "scafctl/get/template")

	f := cmd.Flags().Lookup("search")
	assert.NotNil(t, f, "search flag should exist")
	assert.Equal(t, "s", f.Shorthand)
}

func TestFilterBySearch(t *testing.T) {
	t.Parallel()
	funcs := gotmpl.ExtFunctionList{
		{Name: "toHcl", Description: "Converts a value to HCL format"},
		{Name: "toYaml", Description: "Converts a value to YAML format"},
		{Name: "upper", Description: "Converts string to uppercase"},
	}

	tests := []struct {
		name    string
		query   string
		wantLen int
	}{
		{"match by name", "toHcl", 1},
		{"match by description", "YAML", 1},
		{"match multiple", "Converts", 3},
		{"case insensitive", "TOHCL", 1},
		{"no match", "nonexistent", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := filterBySearch(funcs, tt.query)
			assert.Len(t, result, tt.wantLen)
		})
	}
}

// TestCanonicalAndDeprecated_ShareRunE verifies the canonical child and the
// deprecated leaf produce identical output for the same args, since both share
// the newCommand builder / RunE.
func TestCanonicalAndDeprecated_ShareRunE(t *testing.T) {
	t.Parallel()
	cliParams := &settings.Run{BinaryName: "scafctl"}

	var outC, errC, outD, errD bytes.Buffer
	ioC := terminal.NewIOStreams(nil, &outC, &errC, false)
	ioD := terminal.NewIOStreams(nil, &outD, &errD, false)

	canonical := CommandFunctions(cliParams, ioC, "scafctl/get/template")
	deprecated := CommandGotmplFunctionDeprecated(cliParams, ioD, "scafctl/get")

	canonical.SetOut(&errC)
	canonical.SetErr(&errC)
	canonical.SetArgs([]string{"-o", "json"})
	require.NoError(t, canonical.Execute())

	deprecated.SetOut(&errD)
	deprecated.SetErr(&errD)
	deprecated.SetArgs([]string{"-o", "json"})
	require.NoError(t, deprecated.Execute())

	// The rendered function list is written through IOStreams.Out (outC/outD),
	// which is independent of cobra's own writer. Cobra emits its deprecation
	// notice through the command's writer (pointed at errC/errD here), so the
	// two streams stay cleanly separated: the deprecated leaf's stdout must be
	// byte-identical to the canonical child's stdout (shared RunE), with no
	// stripping required.
	assert.Equal(t, outC.String(), outD.String())
	assert.Contains(t, outC.String(), "\"name\"")
	// The deprecation notice must be emitted on the deprecated leaf's cobra
	// writer (errD), not mixed into the function-list output on stdout.
	assert.Contains(t, errD.String(), `is deprecated`)
	assert.NotContains(t, outD.String(), `is deprecated`)
}
