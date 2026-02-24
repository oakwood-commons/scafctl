// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
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
	cmd.SetArgs([]string{})

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
