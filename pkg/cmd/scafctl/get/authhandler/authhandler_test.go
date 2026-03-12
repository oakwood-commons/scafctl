// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authhandler

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandAuthHandler(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandAuthHandler(cliParams, ioStreams, "scafctl/get")
	require.NotNil(t, cmd)
	assert.Equal(t, "authhandler [name]", cmd.Use)
	assert.Contains(t, cmd.Aliases, "authhandlers")
	assert.Contains(t, cmd.Aliases, "ah")
	assert.Contains(t, cmd.Aliases, "auth-handler")
	assert.Contains(t, cmd.Aliases, "auth-handlers")
	assert.Contains(t, cmd.Aliases, "handlers")
	assert.Contains(t, cmd.Aliases, "handler")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandAuthHandler_NoSubcommands(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandAuthHandler(cliParams, ioStreams, "scafctl/get")
	assert.Len(t, cmd.Commands(), 0, "authhandler should have no subcommands")
}

func BenchmarkCommandAuthHandler(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandAuthHandler(cliParams, ioStreams, "scafctl/get")
	}
}
