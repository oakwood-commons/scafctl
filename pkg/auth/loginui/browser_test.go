// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package loginui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/oakwood-commons/kvx/pkg/core"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunInteractiveTUI_WiresOpenURLWithBrowserPreferenceAndFlushesWarnings(t *testing.T) {
	// Not parallel: mutates package-level runStatusScreen and openBrowser seams.
	origScreen := runStatusScreen
	origOpen := openBrowser
	t.Cleanup(func() {
		runStatusScreen = origScreen
		openBrowser = origOpen
	})

	var (
		capturedURL     string
		capturedBrowser string
		midTUIOutput    string
	)

	var outBuf, errBuf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &outBuf, &errBuf, false)
	w := writer.New(ioStreams, settings.NewCliParams())

	openBrowser = func(ctx context.Context, rawURL, pref string) error {
		var err error
		capturedURL = rawURL
		capturedBrowser, err = auth.ResolveBrowserPreference(ctx, pref)
		if err != nil {
			return err
		}
		// Simulate a browser fallback warning emitted while Bubble Tea owns the terminal.
		auth.WarnIfBrowserUnavailable(ctx, "definitely-missing-browser-for-tui-test")
		return nil
	}

	const wantAuthURL = "https://login.microsoftonline.com/common/oauth2/v2.0/authorize?client_id=abc&state=xyz"

	loginRelease := make(chan struct{})
	handler := &mockHandler{
		name:        "entra",
		displayName: "Microsoft Entra ID",
		loginFunc: func(_ context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			opts.DeviceCodeCallback("", wantAuthURL, "")
			<-loginRelease
			return &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}, nil
		},
	}

	runStatusScreen = func(_ any, cfg tui.Config, _ ...tea.ProgramOption) error {
		require.NotNil(t, cfg.OpenURL, "interactive status TUI must configure cfg.OpenURL")
		require.NoError(t, cfg.OpenURL(wantAuthURL))
		// While runStatusScreen is active, buffered warnings must NOT have hit outBuf yet.
		midTUIOutput = outBuf.String()
		close(loginRelease)
		res := <-cfg.Done
		require.NoError(t, res.Err)
		return nil
	}

	ctx := config.WithConfig(writer.WithWriter(context.Background(), w), &config.Config{
		Auth: config.GlobalAuthConfig{Browser: "msedge"},
	})

	res, err := runInteractiveTUI(ctx, w, "scafctl", handler, auth.LoginOptions{
		Flow:    auth.FlowInteractive,
		Browser: "firefox",
	}, ioStreams)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, wantAuthURL, capturedURL)
	assert.Equal(t, "firefox", capturedBrowser, "--browser option must win over config auth.browser")
	assert.Empty(t, midTUIOutput, "warnings emitted inside OpenURL while TUI is active must be buffered off the alt-screen")
	assert.Contains(t, outBuf.String(), `configured browser "definitely-missing-browser-for-tui-test" could not be launched`,
		"buffered warnings must flush cleanly after TUI exits")
}

func TestRunInteractiveTUI_PreTUIPassThroughAndPlainFallbackFlushOrdering(t *testing.T) {
	origScreen := runStatusScreen
	t.Cleanup(func() { runStatusScreen = origScreen })

	t.Run("pre-TUI warning passes straight through when outcomeChan completes before TUI", func(t *testing.T) {
		var outBuf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &outBuf, &outBuf, false)
		w := writer.New(ioStreams, settings.NewCliParams())

		handler := &mockHandler{
			name:        "entra",
			displayName: "Microsoft Entra ID",
			loginFunc: func(loginCtx context.Context, _ auth.LoginOptions) (*auth.Result, error) {
				auth.WarnIfBrowserUnavailable(loginCtx, "missing-pre-tui-browser")
				return &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}, nil
			},
		}

		res, err := runInteractiveTUI(context.Background(), w, "scafctl", handler, auth.LoginOptions{Flow: auth.FlowInteractive}, ioStreams)
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Contains(t, outBuf.String(), `configured browser "missing-pre-tui-browser" could not be launched`)
	})

	t.Run("TUI startup failure flushes buffered warnings before printing plain sign-in block", func(t *testing.T) {
		var outBuf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &outBuf, &outBuf, false)
		w := writer.New(ioStreams, settings.NewCliParams())

		loginRelease := make(chan struct{})
		var loginCtxRef context.Context
		handler := &mockHandler{
			name:        "entra",
			displayName: "Microsoft Entra ID",
			loginFunc: func(loginCtx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
				loginCtxRef = loginCtx
				opts.DeviceCodeCallback("", "https://login.example.com/authorize", "")
				<-loginRelease
				return &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}, nil
			},
		}

		runStatusScreen = func(_ any, _ tui.Config, _ ...tea.ProgramOption) error {
			// Emit warning while TUI is active, then fail TUI startup to enter plain fallback.
			auth.WarnIfBrowserUnavailable(loginCtxRef, "missing-fallback-browser")
			close(loginRelease)
			return errors.New("tty unavailable")
		}

		res, err := runInteractiveTUI(context.Background(), w, "scafctl", handler, auth.LoginOptions{Flow: auth.FlowInteractive}, ioStreams)
		require.NoError(t, err)
		require.NotNil(t, res)

		out := outBuf.String()
		warnIdx := strings.Index(out, `configured browser "missing-fallback-browser" could not be launched`)
		headingIdx := strings.Index(out, signInHeading)
		require.NotEqual(t, -1, warnIdx, "buffered warning must be present in output: %s", out)
		require.NotEqual(t, -1, headingIdx, "plain sign-in heading must be present in output: %s", out)
		assert.Less(t, warnIdx, headingIdx, "buffered warnings must flush before plain fallback sign-in block")
	})
}

func TestRunStatusTUI_DeviceCodeWiresOpenURLWithBrowserPreference(t *testing.T) {
	origScreen := runStatusScreen
	origOpen := openBrowser
	t.Cleanup(func() {
		runStatusScreen = origScreen
		openBrowser = origOpen
	})

	var (
		capturedURL     string
		capturedBrowser string
	)
	openBrowser = func(ctx context.Context, rawURL, pref string) error {
		var err error
		capturedURL = rawURL
		capturedBrowser, err = auth.ResolveBrowserPreference(ctx, pref)
		return err
	}

	loginRelease := make(chan struct{})
	handler := &mockHandler{
		name:        "entra",
		displayName: "Microsoft Entra ID",
		loginFunc: func(_ context.Context, opts auth.LoginOptions) (*auth.Result, error) {
			opts.DeviceCodeCallback("CODE-999", "https://microsoft.com/devicelogin", "")
			<-loginRelease
			return &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}, nil
		},
	}

	runStatusScreen = func(_ any, cfg tui.Config, _ ...tea.ProgramOption) error {
		require.NotNil(t, cfg.OpenURL, "device-code status TUI must configure cfg.OpenURL")
		require.NoError(t, cfg.OpenURL("https://microsoft.com/devicelogin"))
		close(loginRelease)
		res := <-cfg.Done
		require.NoError(t, res.Err)
		return nil
	}

	w := newTestWriter(t)
	ctx := config.WithConfig(context.Background(), &config.Config{
		Auth: config.GlobalAuthConfig{Browser: "msedge"},
	})

	res, err := runStatusTUI(ctx, w, "scafctl", handler, auth.LoginOptions{Flow: auth.FlowDeviceCode}, w.IOStreams())
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "https://microsoft.com/devicelogin", capturedURL)
	assert.Equal(t, "msedge", capturedBrowser)
}

func TestBuildInteractiveStatusTUI_SnapshotOpenURLActionInvokesCustomOpener(t *testing.T) {
	origOpen := openBrowser
	t.Cleanup(func() { openBrowser = origOpen })

	var (
		openedURL  string
		openedPref string
	)
	openBrowser = func(ctx context.Context, rawURL, pref string) error {
		var err error
		openedURL = rawURL
		openedPref, err = auth.ResolveBrowserPreference(ctx, pref)
		return err
	}

	ctx := config.WithConfig(context.Background(), &config.Config{
		Auth: config.GlobalAuthConfig{Browser: "msedge"},
	})

	const fullURL = "https://login.microsoftonline.com/common/oauth2/v2.0/authorize?client_id=cid&redirect_uri=http%3A%2F%2Flocalhost%3A8400"
	done := make(chan tui.StatusResult, 1)
	cfg, data := buildInteractiveStatusTUI("scafctl", "Microsoft Entra ID", deviceCodeData{verificationURI: fullURL}, done, func(string) error {
		return nil
	}, func(targetURL string) error {
		return openBrowser(ctx, targetURL, "chrome")
	})
	cfg.Width = 100
	cfg.Height = 24
	cfg.NoColor = true
	// F3 triggers the Open URL action even while the inline redirect paste input is focused.
	cfg.StartKeys = []string{"<F3>"}

	root, err := core.LoadObject(data)
	require.NoError(t, err)
	snap := tui.RenderSnapshot(root, cfg)
	assert.Equal(t, fullURL, openedURL)
	assert.Equal(t, "chrome", openedPref)
	assert.Contains(t, snap, "Opened in browser")
}
