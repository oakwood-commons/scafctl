// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// applyOptions applies the given options to a fresh prepareConfig for
// white-box inspection.
func applyOptions(t *testing.T, opts []Option) *prepareConfig {
	t.Helper()
	cfg := &prepareConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

func TestParseLockMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    LockMode
		wantErr bool
	}{
		{name: "strict", input: "strict", want: LockModeStrict},
		{name: "constrained", input: "constrained", want: LockModeConstrained},
		{name: "bestEffort", input: "bestEffort", want: LockModeBestEffort},
		{name: "empty is invalid", input: "", wantErr: true},
		{name: "unknown is invalid", input: "loose", wantErr: true},
		{name: "wrong case is invalid", input: "Strict", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLockMode(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid --lock-mode")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOptionsFromContext_Defaults(t *testing.T) {
	t.Parallel()

	opts, err := OptionsFromContext(context.Background(), CLIWiring{})
	require.NoError(t, err)

	cfg := applyOptions(t, opts)
	assert.Nil(t, cfg.registry)
	assert.False(t, cfg.strict)
	assert.Nil(t, cfg.pluginCfg)
	assert.Equal(t, LockMode(0), cfg.lockMode, "no --lock-mode leaves the mode unset for source-based default")
}

func TestOptionsFromContext_ScalarFields(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	var metrics bytes.Buffer
	pluginCfg := &plugin.ProviderConfig{Quiet: true, NoColor: true, BinaryName: "mycli"}

	opts, err := OptionsFromContext(context.Background(), CLIWiring{
		Registry:     reg,
		Strict:       true,
		MetricsOut:   &metrics,
		PluginConfig: pluginCfg,
	})
	require.NoError(t, err)

	cfg := applyOptions(t, opts)
	assert.Same(t, reg, cfg.registry)
	assert.True(t, cfg.strict)
	assert.True(t, cfg.showMetrics)
	assert.Same(t, &metrics, cfg.metricsOut)
	assert.Same(t, pluginCfg, cfg.pluginCfg)
}

func TestOptionsFromContext_LockMode(t *testing.T) {
	t.Parallel()

	t.Run("valid lock mode is applied", func(t *testing.T) {
		t.Parallel()
		opts, err := OptionsFromContext(context.Background(), CLIWiring{LockMode: "strict"})
		require.NoError(t, err)
		cfg := applyOptions(t, opts)
		assert.Equal(t, LockModeStrict, cfg.lockMode)
	})

	t.Run("invalid lock mode returns an error", func(t *testing.T) {
		t.Parallel()
		_, err := OptionsFromContext(context.Background(), CLIWiring{LockMode: "bogus"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid --lock-mode")
	})
}

func TestOptionsFromContext_GRPCMaxMessageSize(t *testing.T) {
	t.Parallel()

	t.Run("no config leaves client options empty", func(t *testing.T) {
		t.Parallel()
		opts, err := OptionsFromContext(context.Background(), CLIWiring{})
		require.NoError(t, err)
		cfg := applyOptions(t, opts)
		assert.Empty(t, cfg.clientOpts)
	})

	t.Run("config gRPC size adds a client option", func(t *testing.T) {
		t.Parallel()
		cfgIn := &config.Config{}
		cfgIn.Plugins.GRPCMaxMessageSize = 32 * 1024 * 1024
		ctx := config.WithConfig(context.Background(), cfgIn)

		opts, err := OptionsFromContext(ctx, CLIWiring{})
		require.NoError(t, err)
		cfg := applyOptions(t, opts)
		assert.NotEmpty(t, cfg.clientOpts, "gRPC max message size should add a client option")
	})
}

func TestOptionsFromContext_DebugLogging(t *testing.T) {
	t.Parallel()

	optsOff, err := OptionsFromContext(context.Background(), CLIWiring{DebugLogging: false})
	require.NoError(t, err)
	assert.Empty(t, applyOptions(t, optsOff).clientOpts)

	optsOn, err := OptionsFromContext(context.Background(), CLIWiring{DebugLogging: true})
	require.NoError(t, err)
	assert.NotEmpty(t, applyOptions(t, optsOn).clientOpts)
}
