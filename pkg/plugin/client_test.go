// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	hplugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/settings"
)

func TestWithDebugLogging_SetsClientOption(t *testing.T) {
	var o clientOptions
	WithDebugLogging()(&o)
	assert.True(t, o.debugLog)
}

func TestWithStartTimeout_SetsClientOption(t *testing.T) {
	var o clientOptions
	WithStartTimeout(5 * time.Second)(&o)
	assert.Equal(t, 5*time.Second, o.startTimeout)
}

func TestWithGRPCMaxMessageSize_SetsClientOption(t *testing.T) {
	var o clientOptions
	WithGRPCMaxMessageSize(128 * 1024 * 1024)(&o)
	assert.Equal(t, 128*1024*1024, o.grpcMaxMessageSize)
}

func TestWithGRPCMaxMessageSize_ZeroValueIsInitialState(t *testing.T) {
	var o clientOptions
	// An unmodified clientOptions must start at zero; connectPlugin applies
	// the default via resolveGRPCMaxMessageSize when the value is zero.
	assert.Equal(t, 0, o.grpcMaxMessageSize)
}

func TestResolveGRPCMaxMessageSize(t *testing.T) {
	defaultSize := settings.DefaultGRPCMaxMessageSize

	tests := []struct {
		name  string
		input int
		want  int
	}{
		{name: "zero uses default", input: 0, want: defaultSize},
		{name: "negative uses default", input: -1, want: defaultSize},
		{name: "below minimum clamps to minimum", input: settings.MinGRPCMaxMessageSize - 1, want: settings.MinGRPCMaxMessageSize},
		{name: "exactly minimum is accepted", input: settings.MinGRPCMaxMessageSize, want: settings.MinGRPCMaxMessageSize},
		{name: "large value is accepted", input: 128 * 1024 * 1024, want: 128 * 1024 * 1024},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveGRPCMaxMessageSize(tc.input))
		})
	}
}

func TestPluginLogger_DefaultsToNullWhenNil(t *testing.T) {
	l := pluginLogger(nil)
	require.NotNil(t, l)
	// Null logger should be usable and not emit output by default.
	l.Debug("suppressed")

	custom := hclog.New(&hclog.LoggerOptions{Name: "custom", Level: hclog.Debug})
	assert.Same(t, custom, pluginLogger(custom))
}

func TestBuildPluginClient_UsesSanitizedCmdAndDebugLogger(t *testing.T) {
	providerPlugin := &MockProviderPlugin{}
	captured := pluginConfig{}

	typed, client, err := buildPluginClient(
		"ignored-plugin-path",
		[]ClientOption{WithSanitizedEnv(), WithDebugLogging()},
		func(_ string, cfg pluginConfig) (any, *hplugin.Client, error) {
			captured = cfg
			return providerPlugin, &hplugin.Client{}, nil
		},
		func(_ clientOptions, logger hclog.Logger, cmdFn func(string) *exec.Cmd) pluginConfig {
			return pluginConfig{logger: logger, cmdFn: cmdFn}
		},
		func(raw any) (ProviderPlugin, error) {
			p, ok := raw.(ProviderPlugin)
			if !ok {
				return nil, assert.AnError
			}
			return p, nil
		},
	)
	require.NoError(t, err)
	require.NotNil(t, typed)
	require.NotNil(t, client)
	require.NotNil(t, captured.logger)

	cmd := captured.cmdFn("/tmp/fake-plugin")
	require.NotNil(t, cmd)
	require.NotNil(t, cmd.Env)
}

func TestBuildPluginClient_DefaultCmdAndNilLoggerWhenNoFlags(t *testing.T) {
	providerPlugin := &MockProviderPlugin{}
	captured := pluginConfig{}

	typed, client, err := buildPluginClient(
		"ignored-plugin-path",
		nil,
		func(_ string, cfg pluginConfig) (any, *hplugin.Client, error) {
			captured = cfg
			return providerPlugin, &hplugin.Client{}, nil
		},
		func(_ clientOptions, logger hclog.Logger, cmdFn func(string) *exec.Cmd) pluginConfig {
			return pluginConfig{logger: logger, cmdFn: cmdFn}
		},
		func(raw any) (ProviderPlugin, error) {
			p, ok := raw.(ProviderPlugin)
			if !ok {
				return nil, assert.AnError
			}
			return p, nil
		},
	)
	require.NoError(t, err)
	require.NotNil(t, typed)
	require.NotNil(t, client)
	assert.Nil(t, captured.logger)

	cmd := captured.cmdFn("/tmp/fake-plugin")
	require.NotNil(t, cmd)
	assert.Nil(t, cmd.Env)
}

func TestBuildPluginClient_ConnectError(t *testing.T) {
	_, _, err := buildPluginClient(
		"ignored-plugin-path",
		nil,
		func(_ string, _ pluginConfig) (any, *hplugin.Client, error) {
			return nil, nil, assert.AnError
		},
		func(_ clientOptions, logger hclog.Logger, cmdFn func(string) *exec.Cmd) pluginConfig {
			return pluginConfig{logger: logger, cmdFn: cmdFn}
		},
		func(raw any) (ProviderPlugin, error) {
			p, ok := raw.(ProviderPlugin)
			if !ok {
				return nil, assert.AnError
			}
			return p, nil
		},
	)
	require.Error(t, err)
}

func TestConnectPlugin_MissingBinaryReturnsWrappedError(t *testing.T) {
	cfg := pluginConfig{
		handshake:  HandshakeConfig,
		pluginName: PluginName,
		grpcPlugin: &GRPCPlugin{},
	}

	_, _, err := connectPlugin(filepath.Join(t.TempDir(), "missing-plugin"), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to plugin")
}

func TestNewClientWithConnector_Success(t *testing.T) {
	providerPlugin := &MockProviderPlugin{}

	client, err := newClientWithConnector(
		"/tmp/provider-plugin",
		func(_ string, _ pluginConfig) (any, *hplugin.Client, error) {
			return providerPlugin, &hplugin.Client{}, nil
		},
	)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, "provider-plugin", client.Name())
	assert.Equal(t, "/tmp/provider-plugin", client.Path())
}

func TestNewClient_MissingBinary(t *testing.T) {
	_, err := NewClient(filepath.Join(t.TempDir(), "missing-provider-plugin"))
	require.Error(t, err)
}

func TestNewAuthHandlerClientWithConnector_Success(t *testing.T) {
	authPlugin := &MockAuthHandlerPlugin{}

	client, err := newAuthHandlerClientWithConnector(
		"/tmp/auth-plugin",
		func(_ string, _ pluginConfig) (any, *hplugin.Client, error) {
			return authPlugin, &hplugin.Client{}, nil
		},
	)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, "auth-plugin", client.Name())
	assert.Equal(t, "/tmp/auth-plugin", client.Path())
}

func TestNewAuthHandlerClient_MissingBinary(t *testing.T) {
	_, err := NewAuthHandlerClient(filepath.Join(t.TempDir(), "missing-auth-plugin"))
	require.Error(t, err)
}
