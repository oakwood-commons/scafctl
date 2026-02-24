// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandAuth(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandAuth(cliParams, ioStreams, "scafctl")

	assert.Equal(t, "auth", cmd.Use)
	assert.Contains(t, cmd.Aliases, "authenticate")
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify subcommands are added
	subCmds := cmd.Commands()
	require.Len(t, subCmds, 6)

	cmdNames := make([]string, len(subCmds))
	for i, c := range subCmds {
		cmdNames[i] = c.Use
	}
	assert.Contains(t, cmdNames, "diagnose")
	assert.Contains(t, cmdNames, "list [handler]")
	assert.Contains(t, cmdNames, "login <handler>")
	assert.Contains(t, cmdNames, "logout [handler]")
	assert.Contains(t, cmdNames, "status [handler]")
	assert.Contains(t, cmdNames, "token <handler>")
}
