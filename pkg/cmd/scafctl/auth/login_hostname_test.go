// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"context"
	"testing"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
)

// hostnameLoginFixture builds a not-authenticated mock handler with CapHostname
// and a context seeded with the given hostname config for that handler.
func hostnameLoginFixture(t *testing.T, hn *config.HostnameConfig) (context.Context, *auth.MockHandler) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_STATE_HOME", tmp)
	xdg.Reload()

	ctx, _ := newTestContext(t)

	const handlerName = "openshift"
	mock := auth.NewMockHandler(handlerName)
	mock.CapabilitiesValue = []auth.Capability{auth.CapHostname}
	mock.SetNotAuthenticated()
	mock.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}
	ctx = withTestHandler(ctx, mock)
	ctx = configureAuthAndTokenRegistry(t, ctx, mock)

	cfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Handlers: map[string]config.HandlerConfig{
				handlerName: {Hostname: hn},
			},
		},
	}
	ctx = config.WithConfig(ctx, cfg)
	return ctx, mock
}

// TestCommandLogin_HostnameAliasResolved verifies that a static alias is
// resolved to a concrete endpoint URL and forwarded to the handler.
func TestCommandLogin_HostnameAliasResolved(t *testing.T) {
	ctx, mock := hostnameLoginFixture(t, &config.HostnameConfig{
		Aliases: map[string]string{"prod": "https://api.prod.example.com:6443"},
	})

	buf := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandLogin(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"openshift", "--hostname", "prod"})

	require.NoError(t, cmd.Execute())
	require.Len(t, mock.LoginCalls, 1)
	assert.Equal(t, "https://api.prod.example.com:6443", mock.LoginCalls[0].Hostname)
}

// TestCommandLogin_HostnameSelectorNotFound verifies that an unknown selector
// fails with an invalid-input error and never calls the handler.
func TestCommandLogin_HostnameSelectorNotFound(t *testing.T) {
	ctx, mock := hostnameLoginFixture(t, &config.HostnameConfig{
		Aliases: map[string]string{"prod": "https://api.prod.example.com:6443"},
	})

	buf := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandLogin(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"openshift", "--hostname", "staging"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "selector not found")
	assert.Empty(t, mock.LoginCalls, "handler must not be called when selector resolution fails")
}

// TestCommandLogin_HostnameConcreteURLPassthrough verifies that an already
// concrete URL is forwarded unchanged even when aliases exist.
func TestCommandLogin_HostnameConcreteURLPassthrough(t *testing.T) {
	ctx, mock := hostnameLoginFixture(t, &config.HostnameConfig{
		Aliases: map[string]string{"prod": "https://api.prod.example.com:6443"},
	})

	buf := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandLogin(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"openshift", "--hostname", "https://api.custom.example.com:6443"})

	require.NoError(t, cmd.Execute())
	require.Len(t, mock.LoginCalls, 1)
	assert.Equal(t, "https://api.custom.example.com:6443", mock.LoginCalls[0].Hostname)
}

// TestCommandLogin_HostnameResolvedForEmbedder verifies the hook works under a
// non-default embedder binary name.
func TestCommandLogin_HostnameResolvedForEmbedder(t *testing.T) {
	ctx, mock := hostnameLoginFixture(t, &config.HostnameConfig{
		Aliases: map[string]string{"prod": "https://api.prod.example.com:6443"},
	})

	params := settings.NewCliParams()
	params.BinaryName = "mycli"

	buf := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandLogin(params, ioStreams, "mycli/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"openshift", "--hostname", "prod"})

	require.NoError(t, cmd.Execute())
	require.Len(t, mock.LoginCalls, 1)
	assert.Equal(t, "https://api.prod.example.com:6443", mock.LoginCalls[0].Hostname)
}
