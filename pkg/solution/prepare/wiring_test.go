// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
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

func TestBuildPluginFetcherWithConfig_WarningAndStrictChainMode(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	// Plain HTTP server on 127.0.0.1 fails HTTPS handshake immediately as UnreachableError.
	unreachSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(unreachSrv.Close)
	host := strings.TrimPrefix(unreachSrv.URL, "http://")

	appCfg := &config.Config{
		Settings: config.Settings{DisableOfficialCatalog: true},
		Catalogs: []config.CatalogConfig{
			{Name: "broken", Type: config.CatalogTypeOCI, URL: "oci://" + host + "/broken", Insecure: true},
			{Name: "second", Type: config.CatalogTypeOCI, URL: "oci://" + host + "/second", Insecure: true},
		},
	}

	var stdout, stderr bytes.Buffer
	ios := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	w := writer.New(ios, settings.NewCliParams())
	ctx := config.WithConfig(writer.WithWriter(context.Background(), w), appCfg)

	deps := []solution.PluginDependency{
		{Name: "exec", Kind: solution.PluginKindProvider},
		{Name: "git", Kind: solution.PluginKindProvider},
	}

	// Default (best-effort) emits warning to stderr safely across concurrent resolves, leaving stdout untouched.
	fetcher, err := BuildPluginFetcherWithConfig(ctx, PluginFetcherOverrides{
		DisallowLocalBuilds: true,
	})
	require.NoError(t, err)
	_, err = fetcher.ResolvePlugins(ctx, deps)
	require.Error(t, err)
	assert.Empty(t, stdout.String(), "warnings must never write to stdout")
	assert.Contains(t, stderr.String(), `catalog "broken" unavailable, trying next catalog...`)

	// Strict chain mode halts immediately without emitting fallback warnings.
	stderr.Reset()
	strictMode := catalog.ChainModeStrict
	strictFetcher, err := BuildPluginFetcherWithConfig(ctx, PluginFetcherOverrides{
		DisallowLocalBuilds: true,
		ChainMode:           &strictMode,
	})
	require.NoError(t, err)
	_, err = strictFetcher.ResolvePlugins(ctx, deps)
	require.Error(t, err)
	assert.Empty(t, stderr.String(), "strict mode must halt without fallback warnings")

	// OptionsFromContext wires ChainModeStrict when Strict or LockMode=="strict" is set.
	stderr.Reset()
	strictOpts, err := OptionsFromContext(ctx, CLIWiring{LockMode: "strict"})
	require.NoError(t, err)
	strictCfg := applyOptions(t, strictOpts)
	require.NotNil(t, strictCfg.pluginFetcher)
	_, err = strictCfg.pluginFetcher.ResolvePlugins(ctx, deps)
	require.Error(t, err)
	assert.Empty(t, stderr.String(), "OptionsFromContext with LockMode=strict must use ChainModeStrict")

	// registerBundleAuthHandlers fails closed when a locked auth-handler's catalog is unreachable.
	sol := &solution.Solution{}
	sol.Bundle.Plugins = []solution.PluginDependency{
		{Name: "corp-auth", Kind: solution.PluginKindAuthHandler, Version: "^1.0.0"},
	}
	prepCfg := &prepareConfig{
		pluginFetcher: fetcher,
		lockMode:      LockModeConstrained,
		lockFile: &bundler.LockFile{
			Version: bundler.LockFileVersion,
			Plugins: []bundler.LockPlugin{
				{
					Name:              "corp-auth",
					Kind:              string(solution.PluginKindAuthHandler),
					Version:           "1.2.0",
					Constraint:        "^1.0.0",
					ResolvedFrom:      "broken",
					ResolvedCanonical: host + "/broken",
				},
			},
		},
	}
	_, err = registerBundleAuthHandlers(ctx, sol, prepCfg)
	require.Error(t, err, "locked auth-handler must fail closed when its locked catalog is unreachable")
}
