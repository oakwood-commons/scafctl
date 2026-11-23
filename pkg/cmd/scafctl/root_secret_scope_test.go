// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthHandlerSecretScopeWiring fails if any auth-handler registration path
// (fetch fallback resolver, cached-official lazy wrapper, or AuthPluginDirs)
// stops scoping host secret access to the handler's own namespace.
func TestAuthHandlerSecretScopeWiring(t *testing.T) {
	t.Parallel()
	deps := &plugin.HostServiceDeps{
		PromptBroker: &plugin.AuthPromptBroker{},
	}
	base := []plugin.ClientOption{plugin.WithHostDeps(deps)}

	// Fallback resolver path.
	got := plugin.ResolvedHostDeps(scopedAuthClientOpts(base, "entra")...)
	require.NotNil(t, got)
	assert.Equal(t, "scafctl.auth.entra.", got.AllowedSecretPrefix)

	// Cached-official lazy wrapper path.
	cfg := cachedAuthHandlerConfig("github", "/bin/fake", nil, base, nil)
	assert.Equal(t, "github", cfg.Name)
	got = plugin.ResolvedHostDeps(cfg.ClientOpts...)
	require.NotNil(t, got)
	assert.Equal(t, "scafctl.auth.github.", got.AllowedSecretPrefix)

	assert.Empty(t, deps.AllowedSecretPrefix, "shared base deps must not be mutated")
	assert.Len(t, base, 1, "base options must not be appended to in place")
}

func TestHasValidAuthSecretScope(t *testing.T) {
	t.Parallel()

	for _, valid := range []string{"a", "entra", "github", "test-auth", "corp-sso-2"} {
		assert.True(t, hasValidAuthSecretScope(valid), "expected valid scope for %q", valid)
	}
	for _, invalid := range []string{"", "entra.evil", "Entra", "entra_", "-entra", "entra-", "../secret", "entra evil"} {
		assert.False(t, hasValidAuthSecretScope(invalid), "expected invalid scope for %q", invalid)
	}
}

func TestRegisterDirAuthHandlerPlugins_ScopesPerHandlerAndRejectsInvalidNames(t *testing.T) {
	pluginBin := buildTestHelperBinary(t, "../../plugin/testdata/authhandler", "scafctl-plugin-auth-test")
	pluginDir := filepath.Dir(pluginBin)

	sharedDeps := &plugin.HostServiceDeps{
		PromptBroker: &plugin.AuthPromptBroker{},
	}
	baseOpts := []plugin.ClientOption{plugin.WithHostDeps(sharedDeps)}

	t.Run("valid discovered handler receives isolated secret scope and prompt broker", func(t *testing.T) {
		reg := auth.NewRegistry()
		cfgCalls := 0
		var capturedOnly string
		var capturedDeps *plugin.HostServiceDeps

		clients, err := registerDirAuthHandlerPluginsWith(
			context.Background(),
			reg,
			[]string{pluginDir},
			func() *plugin.ProviderConfig {
				cfgCalls++
				return &plugin.ProviderConfig{BinaryName: "mycli"}
			},
			baseOpts,
			func(ctx context.Context, registry *auth.Registry, results []plugin.FetchResult, cfg *plugin.ProviderConfig, only string, clientOpts ...plugin.ClientOption) ([]*plugin.AuthHandlerClient, error) {
				capturedOnly = only
				capturedDeps = plugin.ResolvedHostDeps(clientOpts...)
				return plugin.RegisterFetchedAuthHandlerPluginsNamed(ctx, registry, results, cfg, only, clientOpts...)
			},
		)
		defer plugin.KillAllAuthHandlers(clients)

		require.NoError(t, err)
		assert.Equal(t, 1, cfgCalls)
		assert.Equal(t, "test-auth", capturedOnly)
		require.NotNil(t, capturedDeps)
		assert.Equal(t, "scafctl.auth.test-auth.", capturedDeps.AllowedSecretPrefix)
		assert.NotSame(t, sharedDeps.PromptBroker, capturedDeps.PromptBroker)
		assert.Empty(t, sharedDeps.AllowedSecretPrefix, "shared deps must remain unscoped")
		assert.True(t, reg.Has("test-auth"))
	})

	t.Run("invalid handler name is rejected before exposing host dependencies", func(t *testing.T) {
		t.Setenv("SCAFCTL_TEST_AUTH_HANDLER_NAME", "entra.evil")
		reg := auth.NewRegistry()
		cfgCalls := 0
		registerCalled := false

		clients, err := registerDirAuthHandlerPluginsWith(
			context.Background(),
			reg,
			[]string{pluginDir},
			func() *plugin.ProviderConfig {
				cfgCalls++
				return &plugin.ProviderConfig{BinaryName: "mycli"}
			},
			baseOpts,
			func(_ context.Context, _ *auth.Registry, _ []plugin.FetchResult, _ *plugin.ProviderConfig, _ string, _ ...plugin.ClientOption) ([]*plugin.AuthHandlerClient, error) {
				registerCalled = true
				return nil, nil
			},
		)
		defer plugin.KillAllAuthHandlers(clients)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "entra.evil")
		assert.False(t, registerCalled, "must not start a HostService-enabled client for an invalid handler name")
		assert.Equal(t, 0, cfgCalls)
		assert.Empty(t, clients)
	})
}
