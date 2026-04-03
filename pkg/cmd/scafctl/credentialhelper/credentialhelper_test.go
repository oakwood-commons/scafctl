// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package credentialhelper

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/credentialhelper"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandCredentialHelper_Construction(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandCredentialHelper(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "credential-helper", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

func TestCommandCredentialHelper_Subcommands(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandCredentialHelper(cliParams, ioStreams, "scafctl")

	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}

	assert.True(t, names["get"], "should have 'get' subcommand")
	assert.True(t, names["store"], "should have 'store' subcommand")
	assert.True(t, names["erase"], "should have 'erase' subcommand")
	assert.True(t, names["list"], "should have 'list' subcommand")
	assert.True(t, names["install"], "should have 'install' subcommand")
	assert.True(t, names["uninstall"], "should have 'uninstall' subcommand")
}

func TestWriteError(t *testing.T) {
	var buf bytes.Buffer
	err := writeError(&buf, "something went wrong")

	// writeError returns an error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "something went wrong")

	// writes a JSON error response to the writer
	var resp credentialhelper.ErrorResponse
	require.NoError(t, json.NewDecoder(&buf).Decode(&resp))
	assert.Equal(t, "something went wrong", resp.Message)
}

func TestCommandGet_Structure(t *testing.T) {
	cmd := commandGet()
	require.NotNil(t, cmd)
	assert.Equal(t, "get", cmd.Use)
	assert.True(t, cmd.SilenceUsage)
	assert.True(t, cmd.SilenceErrors)
}

func TestCommandStore_Structure(t *testing.T) {
	cmd := commandStore()
	require.NotNil(t, cmd)
	assert.Equal(t, "store", cmd.Use)
	assert.True(t, cmd.SilenceUsage)
	assert.True(t, cmd.SilenceErrors)
}

func TestCommandErase_Structure(t *testing.T) {
	cmd := commandErase()
	require.NotNil(t, cmd)
	assert.Equal(t, "erase", cmd.Use)
	assert.True(t, cmd.SilenceUsage)
	assert.True(t, cmd.SilenceErrors)
}

func TestCommandList_Structure(t *testing.T) {
	cmd := commandList()
	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)
	assert.True(t, cmd.SilenceUsage)
	assert.True(t, cmd.SilenceErrors)
}

func BenchmarkCommandCredentialHelper(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for b.Loop() {
		_ = CommandCredentialHelper(cliParams, ioStreams, "scafctl")
	}
}
