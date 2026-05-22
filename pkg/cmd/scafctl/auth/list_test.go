// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandList_NoTokens(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.ListCachedTokensResult = nil

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No cached tokens found")
}

func TestCommandList_WithTokens(t *testing.T) {
	ctx, buf := newTestContext(t)

	now := time.Now()
	mock := auth.NewMockHandler("entra")
	mock.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{
			Handler:   "entra",
			TokenKind: "refresh",
			Flow:      auth.FlowDeviceCode,
			ExpiresAt: now.Add(89 * 24 * time.Hour),
			CachedAt:  now.Add(-1 * time.Hour),
			IsExpired: false,
		},
		{
			Handler:   "entra",
			TokenKind: "access",
			Scope:     "https://graph.microsoft.com/.default",
			TokenType: "Bearer",
			Flow:      auth.FlowDeviceCode,
			ExpiresAt: now.Add(55 * time.Minute),
			CachedAt:  now.Add(-5 * time.Minute),
			IsExpired: false,
		},
	}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "entra")
	assert.Contains(t, output, "refresh")
	assert.Contains(t, output, "access")
	assert.Contains(t, output, "device_code")
}

func TestCommandList_FilterByHandler(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{
			Handler:   "entra",
			TokenKind: "refresh",
			ExpiresAt: time.Now().Add(89 * 24 * time.Hour),
			IsExpired: false,
		},
	}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "entra")
}

func TestCommandList_NoHandlers(t *testing.T) {
	ctx, _ := newTestContext(t)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no auth handlers registered")
}

func TestCommandList_TooManyArgs(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "github"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandList_JSONOutput(t *testing.T) {
	ctx, buf := newTestContext(t)

	now := time.Now()
	mock := auth.NewMockHandler("github")
	mock.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{
			Handler:   "github",
			TokenKind: "access",
			TokenType: "Bearer",
			ExpiresAt: now.Add(8 * time.Hour),
			CachedAt:  now.Add(-10 * time.Minute),
			IsExpired: false,
		},
	}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, `"handler"`)
	assert.Contains(t, output, `"github"`)
	assert.Contains(t, output, `"tokenKind"`)
	assert.Contains(t, output, `"access"`)
	assert.Contains(t, output, `"isExpired"`)
	assert.Contains(t, output, `"tokenType"`)
}

func TestCommandList_ExpiredToken(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("gcp")
	mock.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{
			Handler:   "gcp",
			TokenKind: "access",
			TokenType: "Bearer",
			Scope:     "https://www.googleapis.com/auth/cloud-platform",
			ExpiresAt: time.Now().Add(-1 * time.Hour),
			IsExpired: true,
		},
	}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "gcp")
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		name     string
		d        time.Duration
		expected string
	}{
		{"zero", 0, "expired"},
		{"negative", -5 * time.Second, "expired"},
		{"seconds", 45 * time.Second, "45s"},
		{"minutes", 5*time.Minute + 30*time.Second, "5m30s"},
		{"hours", 2*time.Hour + 15*time.Minute, "2h15m"},
		{"days", 3*24*time.Hour + 6*time.Hour, "3d6h"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, humanDuration(tc.d))
		})
	}
}

func TestCommandList_NoEagerHandlers_ShowsOfficialHint(t *testing.T) {
	ctx, buf := newTestContext(t)

	// Add official registry to context (no eager handlers registered)
	officialReg := authofficial.NewRegistry()
	ctx = authofficial.WithRegistry(ctx, officialReg)

	// Also add an empty auth registry so the code path doesn't hit nil
	authReg := auth.NewRegistry()
	ctx = auth.WithRegistry(ctx, authReg)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "No active authentication sessions")
	assert.Contains(t, output, "Official auth handlers")
	assert.Contains(t, output, "entra")
	assert.Contains(t, output, "gcp")
	assert.Contains(t, output, "github")
	assert.Contains(t, output, "auth login <handler>")
	assert.Contains(t, output, "downloads automatically")
}

func TestCommandList_NoOfficialRegistry_StillErrors(t *testing.T) {
	ctx, _ := newTestContext(t)

	// No official registry, no eager handlers — should still error
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no auth handlers registered")
}

func TestCommandList_WithTokens_ShowsRemainingHint(t *testing.T) {
	ctx, buf := newTestContext(t)

	// Set up a handler with tokens
	now := time.Now()
	mock := auth.NewMockHandler("github")
	mock.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{
			Handler:   "github",
			TokenKind: "access",
			Scope:     "repo",
			TokenType: "Bearer",
			Flow:      auth.FlowDeviceCode,
			ExpiresAt: now.Add(1 * time.Hour),
			CachedAt:  now.Add(-5 * time.Minute),
			IsExpired: false,
		},
	}
	ctx = withTestHandler(ctx, mock)

	// Add official registry so hint about remaining handlers can appear
	officialReg := authofficial.NewRegistry()
	ctx = authofficial.WithRegistry(ctx, officialReg)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)
	// Token results are shown (this test verifies no crash with official registry present)
	output := buf.String()
	assert.Contains(t, output, "github")
}

func TestCommandList_ValidArgsFunction(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)

	completions, directive := cmd.ValidArgsFunction(cmd, []string{}, "")
	assert.Contains(t, completions, "entra")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	completions, directive = cmd.ValidArgsFunction(cmd, []string{"entra"}, "")
	assert.Empty(t, completions)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestCommandList_WarningsGoToStderr(t *testing.T) {
	// Verify that when a handler doesn't support token listing,
	// the warning goes to stderr (not stdout) so it doesn't corrupt JSON output.
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)
	ctx = logger.WithLogger(ctx, logger.GetNoopLogger())

	// Register a handler that does NOT implement TokenLister.
	// Use a minimal mock that only implements auth.Handler (not TokenLister).
	reg := auth.NewRegistry()
	minimalMock := &minimalHandler{name: "minimal"}
	require.NoError(t, reg.Register(minimalMock))
	ctx = auth.WithRegistry(ctx, reg)

	cliParams := settings.NewCliParams()
	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"-o", "json"})

	err := cmd.Execute()
	assert.NoError(t, err)

	// Warning should be in stderr, not stdout
	assert.Contains(t, stderr.String(), "does not support token listing")
	assert.NotContains(t, stdout.String(), "does not support token listing")
}

// minimalHandler implements only auth.Handler (not TokenLister or TokenPurger).
type minimalHandler struct {
	name string
}

func (m *minimalHandler) Name() string                    { return m.name }
func (m *minimalHandler) DisplayName() string             { return m.name }
func (m *minimalHandler) SupportedFlows() []auth.Flow     { return nil }
func (m *minimalHandler) Capabilities() []auth.Capability { return nil }
func (m *minimalHandler) Login(_ context.Context, _ auth.LoginOptions) (*auth.Result, error) {
	return nil, nil
}
func (m *minimalHandler) Logout(_ context.Context) error                 { return nil }
func (m *minimalHandler) Status(_ context.Context) (*auth.Status, error) { return &auth.Status{}, nil }
func (m *minimalHandler) GetToken(_ context.Context, _ auth.TokenOptions) (*auth.Token, error) {
	return nil, nil
}

func (m *minimalHandler) InjectAuth(_ context.Context, _ *http.Request, _ auth.TokenOptions) error {
	return nil
}
