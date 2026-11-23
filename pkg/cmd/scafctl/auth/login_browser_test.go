// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandLogin_BrowserPreferenceAndValidation(t *testing.T) {
	t.Parallel()

	t.Run("--browser flag overrides auth.browser config", func(t *testing.T) {
		t.Parallel()
		ctx, buf := newTestContext(t)
		mock := auth.NewMockHandler("entra")
		mock.DisplayNameValue = "Microsoft Entra ID"
		mock.FlowsValue = []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode}
		mock.SetNotAuthenticated()
		mock.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}
		ctx = withTestHandler(ctx, mock)
		ctx = config.WithConfig(ctx, &config.Config{
			Auth: config.GlobalAuthConfig{Browser: "chrome"},
		})

		cliParams := settings.NewCliParams()
		ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
		cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra", "--browser", "msedge"})

		require.NoError(t, cmd.Execute())
		require.Len(t, mock.LoginCalls, 1)
		assert.Equal(t, "msedge", mock.LoginCalls[0].Browser)
	})

	t.Run("auth.browser from config used when --browser omitted", func(t *testing.T) {
		t.Parallel()
		ctx, buf := newTestContext(t)
		mock := auth.NewMockHandler("github")
		mock.DisplayNameValue = "GitHub"
		mock.FlowsValue = []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode}
		mock.SetNotAuthenticated()
		mock.LoginResult = &auth.Result{Claims: &auth.Claims{Username: "octocat"}}
		ctx = withTestHandler(ctx, mock)
		ctx = config.WithConfig(ctx, &config.Config{
			Auth: config.GlobalAuthConfig{Browser: "firefox"},
		})

		cliParams := settings.NewCliParams()
		ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
		cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"github", "--flow", "interactive"})

		require.NoError(t, cmd.Execute())
		require.Len(t, mock.LoginCalls, 1)
		assert.Equal(t, "firefox", mock.LoginCalls[0].Browser)
	})

	t.Run("empty when neither --browser nor auth.browser is set", func(t *testing.T) {
		t.Parallel()
		ctx, buf := newTestContext(t)
		mock := auth.NewMockHandler("entra")
		mock.DisplayNameValue = "Microsoft Entra ID"
		mock.FlowsValue = []auth.Flow{auth.FlowInteractive}
		mock.SetNotAuthenticated()
		mock.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}
		ctx = withTestHandler(ctx, mock)

		cliParams := settings.NewCliParams()
		ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
		cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra"})

		require.NoError(t, cmd.Execute())
		require.Len(t, mock.LoginCalls, 1)
		assert.Empty(t, mock.LoginCalls[0].Browser)
	})

	t.Run("invalid --browser fails up front before handler lookup", func(t *testing.T) {
		t.Parallel()
		for _, bad := range []string{"", "   ", "--headless", "-p", `"--flag"`, "msedge\n"} {
			ctx, buf := newTestContext(t)
			cliParams := settings.NewCliParams()
			ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
			cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
			cmd.SetContext(ctx)
			cmd.SetArgs([]string{"nonexistent-handler", "--browser", bad})

			err := cmd.Execute()
			require.Error(t, err, "expected invalid input error for %q", bad)
			assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		}
	})

	t.Run("invalid auth.browser in config fails with ConfigError unless overridden by valid --browser", func(t *testing.T) {
		t.Parallel()
		ctx, buf := newTestContext(t)
		mock := auth.NewMockHandler("entra")
		mock.DisplayNameValue = "Microsoft Entra ID"
		mock.FlowsValue = []auth.Flow{auth.FlowInteractive}
		mock.SetNotAuthenticated()
		mock.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}
		ctx = withTestHandler(ctx, mock)
		ctx = config.WithConfig(ctx, &config.Config{
			Auth: config.GlobalAuthConfig{Browser: "--bad-config-browser"},
		})

		cliParams := settings.NewCliParams()
		ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
		cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra"})

		err := cmd.Execute()
		require.Error(t, err)
		assert.Equal(t, exitcode.ConfigError, exitcode.GetCode(err))

		// Explicit valid --browser overrides bad config value cleanly.
		cmd2 := CommandLogin(cliParams, ioStreams, "scafctl/auth")
		cmd2.SetContext(ctx)
		cmd2.SetArgs([]string{"entra", "--browser", "default"})
		require.NoError(t, cmd2.Execute())
		require.Len(t, mock.LoginCalls, 1)
		assert.Equal(t, "default", mock.LoginCalls[0].Browser)
	})

	t.Run("embedder binary name in --browser help example", func(t *testing.T) {
		t.Parallel()
		p := settings.NewCliParams()
		p.BinaryName = "mycli"
		cmd := CommandLogin(p, terminal.NewIOStreams(nil, nil, nil, false), "mycli/auth")
		assert.Contains(t, cmd.Long, "mycli auth login entra --browser msedge")
		assert.NotContains(t, cmd.Long, settings.CliBinaryName)
	})
}
