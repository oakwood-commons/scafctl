// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandLogin_BrowserFlagAndConfigPriority(t *testing.T) {
	t.Parallel()

	t.Run("explicit --browser overrides auth.browser in config", func(t *testing.T) {
		t.Parallel()
		ctx, buf := newTestContext(t)
		ctx, mock := withMockHandler(t, ctx)
		mock.GetTokenResult = &auth.Token{AccessToken: "tok"}
		ctx = config.WithConfig(ctx, &config.Config{
			Auth: config.GlobalAuthConfig{Browser: "chrome"},
		})

		path := filepath.Join(t.TempDir(), "config")
		ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
		cmd := CommandLogin(embedderParams(), ioStreams, "mycli")

		err := runCmd(t, cmd, ctx, "prod",
			"--handler", "oidc",
			"--server", "https://api.example.com:6443",
			"--kubeconfig", path,
			"--browser", "msedge",
		)
		require.NoError(t, err)
		require.Len(t, mock.LoginCalls, 1)
		assert.Equal(t, "msedge", mock.LoginCalls[0].Browser)
	})

	t.Run("auth.browser from config used when --browser omitted", func(t *testing.T) {
		t.Parallel()
		ctx, buf := newTestContext(t)
		ctx, mock := withMockHandler(t, ctx)
		mock.GetTokenResult = &auth.Token{AccessToken: "tok"}
		ctx = config.WithConfig(ctx, &config.Config{
			Auth: config.GlobalAuthConfig{Browser: "firefox"},
		})

		path := filepath.Join(t.TempDir(), "config")
		ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
		cmd := CommandLogin(embedderParams(), ioStreams, "mycli")

		err := runCmd(t, cmd, ctx, "prod",
			"--handler", "oidc",
			"--server", "https://api.example.com:6443",
			"--kubeconfig", path,
		)
		require.NoError(t, err)
		require.Len(t, mock.LoginCalls, 1)
		assert.Equal(t, "firefox", mock.LoginCalls[0].Browser)
	})

	t.Run("invalid --browser fails fast before login", func(t *testing.T) {
		t.Parallel()
		for _, bad := range []string{"", "   ", "--headless", "msedge\n"} {
			ctx, _ := newTestContext(t)
			ctx, mock := withMockHandler(t, ctx)
			cmd := CommandLogin(embedderParams(), terminal.NewIOStreams(nil, nil, nil, false), "mycli")
			err := runCmd(t, cmd, ctx, "prod", "--handler", "oidc", "--server", "https://api.example.com:6443", "--browser", bad)
			require.Error(t, err)
			assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
			assert.Empty(t, mock.LoginCalls)
		}
	})

	t.Run("structured -o json keeps stdout valid JSON when warnings are emitted", func(t *testing.T) {
		t.Parallel()
		var outBuf, errBuf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &outBuf, &errBuf, false)
		w := writer.New(ioStreams, embedderParams())
		ctx := logger.WithLogger(writer.WithWriter(context.Background(), w), logger.GetNoopLogger())

		reg := auth.NewRegistry()
		warnMock := &warningStubHandler{name: "oidc"}
		require.NoError(t, reg.Register(warnMock))
		ctx = auth.WithRegistry(ctx, reg)

		path := filepath.Join(t.TempDir(), "config")
		cmd := CommandLogin(embedderParams(), ioStreams, "mycli")
		err := runCmd(t, cmd, ctx, "prod",
			"--handler", "oidc",
			"--server", "https://api.example.com:6443",
			"--kubeconfig", path,
			"--browser", "nonexistent-browser-json-test",
			"--output", "json",
		)
		require.NoError(t, err)

		var rows []map[string]any
		require.NoError(t, json.Unmarshal(outBuf.Bytes(), &rows), "stdout must remain pure JSON: %s", outBuf.String())
		require.Len(t, rows, 1)
		assert.Contains(t, errBuf.String(), `configured browser "nonexistent-browser-json-test" could not be launched`)
	})

	t.Run("embedder binary name replaces scafctl in help examples", func(t *testing.T) {
		t.Parallel()
		cmd := CommandLogin(embedderParams(), terminal.NewIOStreams(nil, nil, nil, false), "mycli")
		assert.Contains(t, cmd.Long, "mycli kube login prod --browser msedge")
		assert.NotContains(t, cmd.Long, settings.CliBinaryName)

		fallbackCmd := CommandLogin(nil, terminal.NewIOStreams(nil, nil, nil, false), "")
		assert.Contains(t, fallbackCmd.Long, settings.CliBinaryName+" kube login prod --browser msedge")
	})

	t.Run("invalid auth.browser in config fails with ConfigError unless overridden by valid --browser", func(t *testing.T) {
		t.Parallel()
		ctx, buf := newTestContext(t)
		ctx, mock := withMockHandler(t, ctx)
		mock.GetTokenResult = &auth.Token{AccessToken: "tok"}
		ctx = config.WithConfig(ctx, &config.Config{
			Auth: config.GlobalAuthConfig{Browser: "--bad-config-browser"},
		})

		path := filepath.Join(t.TempDir(), "config")
		ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
		cmd := CommandLogin(embedderParams(), ioStreams, "mycli")
		err := runCmd(t, cmd, ctx, "prod", "--handler", "oidc", "--server", "https://api.example.com:6443", "--kubeconfig", path)
		require.Error(t, err)
		assert.Equal(t, exitcode.ConfigError, exitcode.GetCode(err))
		assert.Empty(t, mock.LoginCalls)

		cmd2 := CommandLogin(embedderParams(), ioStreams, "mycli")
		require.NoError(t, runCmd(t, cmd2, ctx, "prod", "--handler", "oidc", "--server", "https://api.example.com:6443", "--kubeconfig", path, "--browser", "default"))
		require.Len(t, mock.LoginCalls, 1)
		assert.Equal(t, "default", mock.LoginCalls[0].Browser)
	})
}

type warningStubHandler struct {
	*auth.MockHandler
	name string
}

func (h *warningStubHandler) Name() string        { return h.name }
func (h *warningStubHandler) DisplayName() string { return h.name }

func (h *warningStubHandler) Login(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	auth.WarnIfBrowserUnavailable(ctx, opts.Browser)
	return &auth.Result{}, nil
}

func (h *warningStubHandler) GetToken(_ context.Context, _ auth.TokenOptions) (*auth.Token, error) {
	return &auth.Token{AccessToken: "tok"}, nil
}

func (h *warningStubHandler) Logout(_ context.Context) error { return nil }

func (h *warningStubHandler) Status(_ context.Context) (*auth.Status, error) {
	return &auth.Status{}, nil
}

func (h *warningStubHandler) SupportedFlows() []auth.Flow     { return []auth.Flow{auth.FlowInteractive} }
func (h *warningStubHandler) Capabilities() []auth.Capability { return nil }
