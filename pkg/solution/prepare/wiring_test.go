// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
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

func TestLoadAdjacentLockFile(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when no lock file exists", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solutionPath := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.WriteFile(solutionPath, []byte("kind: Solution\n"), 0o600))

		lf, err := loadAdjacentLockFile(solutionPath)
		require.NoError(t, err)
		assert.Nil(t, lf)
	})

	t.Run("returns an error for a malformed lock file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solutionPath := filepath.Join(dir, "solution.yaml")
		lockPath := filepath.Join(dir, bundler.DefaultLockFileName)
		require.NoError(t, os.WriteFile(lockPath, []byte("::: not valid yaml :::"), 0o600))

		lf, err := loadAdjacentLockFile(solutionPath)
		require.Error(t, err)
		assert.Nil(t, lf)
	})

	t.Run("loads an adjacent lock file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solutionPath := filepath.Join(dir, "solution.yaml")
		lockPath := filepath.Join(dir, bundler.DefaultLockFileName)
		require.NoError(t, os.WriteFile(lockPath, []byte("version: 1\nplugins: []\n"), 0o600))

		lf, err := loadAdjacentLockFile(solutionPath)
		require.NoError(t, err)
		require.NotNil(t, lf)
	})
}

func TestOptionsFromContext_Defaults(t *testing.T) {
	t.Parallel()

	opts, err := OptionsFromContext(context.Background(), CLIWiring{})
	require.NoError(t, err)

	cfg := applyOptions(t, opts)
	assert.Nil(t, cfg.getter)
	assert.Nil(t, cfg.registry)
	assert.Nil(t, cfg.stdin)
	assert.False(t, cfg.noCache)
	assert.False(t, cfg.strict)
	assert.Nil(t, cfg.pluginCfg)
	assert.Equal(t, LockMode(0), cfg.lockMode, "no --lock-mode leaves the mode unset for source-based default")
}

func TestOptionsFromContext_ScalarFields(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	var stdin bytes.Buffer
	var metrics bytes.Buffer
	pluginCfg := &plugin.ProviderConfig{Quiet: true, NoColor: true, BinaryName: "mycli"}

	opts, err := OptionsFromContext(context.Background(), CLIWiring{
		Registry:      reg,
		Stdin:         &stdin,
		NoCache:       true,
		Strict:        true,
		DiscoveryMode: settings.DiscoveryModeAction,
		MetricsOut:    &metrics,
		PluginConfig:  pluginCfg,
	})
	require.NoError(t, err)

	cfg := applyOptions(t, opts)
	assert.Same(t, reg, cfg.registry)
	assert.Same(t, &stdin, cfg.stdin)
	assert.True(t, cfg.noCache)
	assert.True(t, cfg.strict)
	assert.Equal(t, settings.DiscoveryModeAction, cfg.discoveryMode)
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

func TestOptionsFromContext_AdjacentLockFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	solutionPath := filepath.Join(dir, "solution.yaml")
	lockPath := filepath.Join(dir, bundler.DefaultLockFileName)
	require.NoError(t, os.WriteFile(lockPath, []byte("version: 1\nplugins: []\n"), 0o600))

	opts, err := OptionsFromContext(context.Background(), CLIWiring{File: solutionPath})
	require.NoError(t, err)
	cfg := applyOptions(t, opts)
	assert.NotNil(t, cfg.lockFile, "an adjacent lock file should be wired via WithLockFile")

	t.Run("stdin does not load a lock file", func(t *testing.T) {
		t.Parallel()
		stdinOpts, err := OptionsFromContext(context.Background(), CLIWiring{File: "-"})
		require.NoError(t, err)
		assert.Nil(t, applyOptions(t, stdinOpts).lockFile)
	})
}
