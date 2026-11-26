// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandLogin_GitHubSeedsDefaultScopesFromEmbeddedDefaults(t *testing.T) {
	t.Parallel()

	t.Run("seeds embedded defaultScopes when config has no github section and --scope omitted", func(t *testing.T) {
		t.Parallel()
		ctx, buf := newTestContext(t)

		mock := auth.NewMockHandler("github")
		mock.DisplayNameValue = "GitHub"
		mock.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin, auth.CapHostname}
		mock.SetNotAuthenticated()
		mock.LoginResult = &auth.Result{Claims: &auth.Claims{Username: "octocat"}}

		ctx = withTestHandler(ctx, mock)
		ctx = config.WithConfig(ctx, &config.Config{})

		cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"github"})

		require.NoError(t, cmd.Execute())
		require.Len(t, mock.LoginCalls, 1)
		assert.Equal(t, []string{"repo", "read:org", "read:packages"}, mock.LoginCalls[0].Scopes)
	})

	t.Run("explicit --scope overrides embedded defaults", func(t *testing.T) {
		t.Parallel()
		ctx, buf := newTestContext(t)

		mock := auth.NewMockHandler("github")
		mock.DisplayNameValue = "GitHub"
		mock.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin, auth.CapHostname}
		mock.SetNotAuthenticated()
		mock.LoginResult = &auth.Result{Claims: &auth.Claims{Username: "octocat"}}

		ctx = withTestHandler(ctx, mock)
		ctx = config.WithConfig(ctx, &config.Config{})

		cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"github", "--scope", "public_repo"})

		require.NoError(t, cmd.Execute())
		require.Len(t, mock.LoginCalls, 1)
		assert.Equal(t, []string{"public_repo"}, mock.LoginCalls[0].Scopes)
	})

	t.Run("configured defaultScopes in config inhibits embedded scope seeding", func(t *testing.T) {
		t.Parallel()
		ctx, buf := newTestContext(t)

		mock := auth.NewMockHandler("github")
		mock.DisplayNameValue = "GitHub"
		mock.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin, auth.CapHostname}
		mock.SetNotAuthenticated()
		mock.LoginResult = &auth.Result{Claims: &auth.Claims{Username: "octocat"}}

		ctx = withTestHandler(ctx, mock)
		ctx = config.WithConfig(ctx, &config.Config{
			Auth: config.GlobalAuthConfig{
				GitHub: &config.GitHubAuthConfig{
					DefaultScopes: []string{"read:packages"},
				},
			},
		})

		cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"github"})

		require.NoError(t, cmd.Execute())
		require.Len(t, mock.LoginCalls, 1)
		assert.Empty(t, mock.LoginCalls[0].Scopes)
	})
}

func TestCommandOAuth2_GitHostAndGitUsernameFlags(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	var outBuf, errBuf bytes.Buffer
	streams := &terminal.IOStreams{
		In:     io.NopCloser(bytes.NewReader(nil)),
		Out:    &outBuf,
		ErrOut: &errBuf,
	}
	cliParams := settings.NewCliParams()
	ctx := writer.WithWriter(t.Context(), writer.New(streams, cliParams))
	ctx = settings.IntoContext(ctx, cliParams)

	root := CommandAuth(cliParams, streams, "scafctl")
	root.PersistentFlags().String("config", cfgPath, "")
	root.SetContext(ctx)
	root.SetArgs([]string{
		"oauth2", "add", "git-idp",
		"--token-url", "https://git.example.com/oauth/token",
		"--client-id", "git-cli",
		"--client-secret", "env://GIT_CLIENT_SECRET",
		"--default-flow", "client_credentials",
		"--git-host", "git.example.com",
		"--git-username", "oauth2",
	})
	require.NoError(t, root.Execute())

	outBuf.Reset()
	errBuf.Reset()
	listRoot := CommandAuth(cliParams, streams, "scafctl")
	listRoot.PersistentFlags().String("config", cfgPath, "")
	listRoot.SetContext(ctx)
	listRoot.SetArgs([]string{"oauth2", "list", "-o", "json"})
	require.NoError(t, listRoot.Execute())

	var items []config.SanitizedCustomOAuth2
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &items))
	require.Len(t, items, 1)
	assert.Equal(t, "git-idp", items[0].Name)
	assert.Equal(t, "git.example.com", items[0].GitHost)
	assert.Equal(t, "oauth2", items[0].GitUsername)
}
