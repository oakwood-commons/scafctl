// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"bytes"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandRender_UnknownSubcommandErrors(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	// Unknown subcommand must error.
	cmd := CommandRender(cliParams, ioStreams, "")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestCommandRender_BareShowsHelpExitZero(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandRender(cliParams, ioStreams, "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})
	cmd.SilenceErrors = true

	err := cmd.Execute()

	require.NoError(t, err, "bare invocation should not error")
	assert.Contains(t, out.String(), "Usage:", "bare invocation should print help")
}
