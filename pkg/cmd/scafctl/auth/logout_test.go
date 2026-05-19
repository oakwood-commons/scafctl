// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandLogout_UnknownHandler(t *testing.T) {
	ctx, _ := newTestContext(t)
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandLogout(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"unknown"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth handler")
}

func TestCommandLogout_MissingHandler(t *testing.T) {
	ctx, _ := newTestContext(t)
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandLogout(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg(s)")
}

func TestCommandLogout_NotAuthenticated(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.SetNotAuthenticated()

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogout(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify logout was NOT called (not authenticated)
	assert.Equal(t, 0, mock.LogoutCalls)

	// Verify output
	output := buf.String()
	assert.Contains(t, output, "Not currently authenticated")
}

func TestCommandLogout_Success(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.SetAuthenticated(&auth.Claims{
		Email: "test@example.com",
	})

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogout(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify logout was called
	assert.Equal(t, 1, mock.LogoutCalls)

	// Verify output
	output := buf.String()
	assert.Contains(t, output, "Successfully logged out")
}

func TestCommandLogout_Failure(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.SetAuthenticated(&auth.Claims{
		Email: "test@example.com",
	})
	mock.LogoutErr = errors.New("failed to clear credentials")

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogout(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one or more logout operations failed")
	// The specific underlying error is printed to output, not wrapped in the returned error
	assert.Contains(t, buf.String(), "failed to clear credentials")
}

func TestCommandLogout_AllNoHandlers_ShowsOfficialAvailable(t *testing.T) {
	ctx, buf := newTestContext(t)

	// No auth registry / no test handler, but official registry is present.
	officialReg := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "github", CatalogRef: "github"},
		{Name: "entra", CatalogRef: "entra"},
	})
	ctx = authofficial.WithRegistry(ctx, officialReg)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogout(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--all"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "No active authentication sessions")
	assert.Contains(t, output, "github")
	assert.Contains(t, output, "entra")
}

func TestCommandLogout_ValidArgsFunction(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := auth.NewMockHandler("github")
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandLogout(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)

	// ValidArgsFunction with no args should return handler names.
	completions, directive := cmd.ValidArgsFunction(cmd, []string{}, "")
	assert.Contains(t, completions, "github")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	// With one arg already, should return no completions.
	completions, directive = cmd.ValidArgsFunction(cmd, []string{"github"}, "")
	assert.Empty(t, completions)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}
