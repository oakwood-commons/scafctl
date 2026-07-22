// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/gotmplfunction"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandTemplate(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTemplate(cliParams, ioStreams, "scafctl/get")
	require.NotNil(t, cmd)
	assert.Equal(t, "template", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	names := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "functions", "get template should wire up the 'functions' child")
}

// TestCommandTemplate_BareShowsHelp verifies a bare 'get template' shows help
// (exit 0) and an unknown subcommand errors.
func TestCommandTemplate_BareShowsHelp(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTemplate(cliParams, ioStreams, "scafctl/get")
	cmd.SetArgs([]string{})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	assert.NoError(t, cmd.Execute())
}

func TestCommandTemplate_UnknownSubcommandErrors(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandTemplate(cliParams, ioStreams, "scafctl/get")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

// TestEmbedderBinaryName verifies the deprecated go-template-functions leaf
// reachable through 'get template' honors a non-default (embedder) binary name
// in its user-facing deprecation notice rather than hardcoding "scafctl".
func TestEmbedderBinaryName(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := gotmplfunction.CommandGotmplFunctionDeprecated(cliParams, ioStreams, "mycli/get")
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Deprecated, "mycli")
	assert.NotContains(t, cmd.Deprecated, "scafctl")

	// The canonical child's Long/Examples must also honor the embedder binary
	// name for command-prefix tokens, while product-name prose stays literal.
	canonical := gotmplfunction.CommandFunctions(cliParams, ioStreams, "mycli/get/template")
	require.NotNil(t, canonical)
	assert.Contains(t, canonical.Long, "mycli get template functions")
	assert.NotContains(t, canonical.Long, "scafctl get template functions")
	// Product-name prose must NOT be rewritten to the binary name.
	assert.Contains(t, canonical.Long, "scafctl-specific")
}
