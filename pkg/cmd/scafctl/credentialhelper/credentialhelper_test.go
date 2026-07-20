// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package credentialhelper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestFormatGetError(t *testing.T) {
	t.Run("reauth error yields actionable hint with binary name", func(t *testing.T) {
		ctx := settings.IntoContext(context.Background(), &settings.Run{BinaryName: "mybin"})
		err := &credentialhelper.ReauthRequiredError{
			Handler:   "github",
			ServerURL: "https://ghcr.io",
		}
		msg := formatGetError(ctx, err)
		assert.Contains(t, msg, "mybin auth login github")
		assert.Contains(t, msg, "https://ghcr.io")
		assert.Contains(t, msg, "credential-helper subprocess")
	})

	t.Run("reauth error wrapped in chain is detected", func(t *testing.T) {
		ctx := context.Background() // no settings -> default binary name
		base := &credentialhelper.ReauthRequiredError{Handler: "entra", ServerURL: "acr.azurecr.io"}
		wrapped := fmt.Errorf("get failed: %w", base)
		msg := formatGetError(ctx, wrapped)
		assert.Contains(t, msg, settings.CliBinaryName+" auth login entra")
	})

	t.Run("plain error returns its message verbatim", func(t *testing.T) {
		msg := formatGetError(context.Background(), errors.New("credentials not found"))
		assert.Equal(t, "credentials not found", msg)
	})
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

// TestCommandCredentialHelper_UnknownSubcommandErrors verifies that an unknown
// subcommand errors (non-zero) while a bare invocation shows help and exits 0.
func TestCommandCredentialHelper_UnknownSubcommandErrors(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandCredentialHelper(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandCredentialHelper(cliParams, ioStreams, "scafctl")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}
