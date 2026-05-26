// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectAuthHandlerSettings(t *testing.T) {
	tests := []struct {
		name        string
		handlerName string
		appCfg      *config.Config
		wantKey     string
		wantField   string
		wantValue   string
	}{
		{
			name:        "github config forwarded",
			handlerName: "github",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					GitHub: &config.GitHubAuthConfig{
						ClientID:      "test-client-id",
						Hostname:      "github.example.com",
						DefaultScopes: []string{"read:org"},
					},
				},
			},
			wantKey:   "github",
			wantField: "clientId",
			wantValue: "test-client-id",
		},
		{
			name:        "entra config forwarded",
			handlerName: "entra",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{
						ClientID: "entra-client-id",
						TenantID: "tenant-123",
					},
				},
			},
			wantKey:   "entra",
			wantField: "clientId",
			wantValue: "entra-client-id",
		},
		{
			name:        "gcp config forwarded",
			handlerName: "gcp",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					GCP: &config.GCPAuthConfig{
						ClientID: "gcp-client-id",
						Project:  "my-project",
					},
				},
			},
			wantKey:   "gcp",
			wantField: "clientId",
			wantValue: "gcp-client-id",
		},
		{
			name:        "nil github config skipped",
			handlerName: "github",
			appCfg:      &config.Config{},
		},
		{
			name:        "unknown handler skipped",
			handlerName: "unknown",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					GitHub: &config.GitHubAuthConfig{ClientID: "test"},
				},
			},
		},
		{
			name:        "nil app config skipped",
			handlerName: "github",
			appCfg:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.appCfg != nil {
				ctx = config.WithConfig(ctx, tt.appCfg)
			}

			cfg := &ProviderConfig{}
			injectAuthHandlerSettings(ctx, tt.handlerName, cfg)

			if tt.wantKey == "" {
				assert.Nil(t, cfg.Settings)
				return
			}

			require.NotNil(t, cfg.Settings)
			raw, ok := cfg.Settings[tt.wantKey]
			require.True(t, ok, "expected settings key %q", tt.wantKey)

			var m map[string]any
			require.NoError(t, json.Unmarshal(raw, &m))
			assert.Equal(t, tt.wantValue, m[tt.wantField])
		})
	}
}

func TestInjectAuthHandlerSettings_ProfileMerge(t *testing.T) {
	tests := []struct {
		name        string
		handlerName string
		profile     string
		appCfg      *config.Config
		wantField   string
		wantValue   string
	}{
		{
			name:        "entra profile overrides clientId",
			handlerName: "entra",
			profile:     "machine",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{
						ClientID: "top-level-client",
						TenantID: "shared-tenant",
						Profiles: map[string]*config.EntraProfileConfig{
							"machine": {ClientID: "machine-client"},
						},
					},
				},
			},
			wantField: "clientId",
			wantValue: "machine-client",
		},
		{
			name:        "entra profile inherits tenantId",
			handlerName: "entra",
			profile:     "machine",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{
						ClientID: "top-level-client",
						TenantID: "shared-tenant",
						Profiles: map[string]*config.EntraProfileConfig{
							"machine": {ClientID: "machine-client"},
						},
					},
				},
			},
			wantField: "tenantId",
			wantValue: "shared-tenant",
		},
		{
			name:        "entra profile includes federatedTokenFile",
			handlerName: "entra",
			profile:     "wif",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{
						Profiles: map[string]*config.EntraProfileConfig{
							"wif": {
								ClientID:           "wif-client",
								FederatedTokenFile: "/var/run/secrets/token",
							},
						},
					},
				},
			},
			wantField: "federatedTokenFile",
			wantValue: "/var/run/secrets/token",
		},
		{
			name:        "gcp profile includes serviceAccountKeyFile",
			handlerName: "gcp",
			profile:     "deploy",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					GCP: &config.GCPAuthConfig{
						Profiles: map[string]*config.GCPProfileConfig{
							"deploy": {ServiceAccountKeyFile: "/path/to/sa.json"},
						},
					},
				},
			},
			wantField: "serviceAccountKeyFile",
			wantValue: "/path/to/sa.json",
		},
		{
			name:        "no profile sends top-level config",
			handlerName: "entra",
			profile:     "",
			appCfg: &config.Config{
				Auth: config.GlobalAuthConfig{
					Entra: &config.EntraAuthConfig{
						ClientID: "top-level-client",
						Profiles: map[string]*config.EntraProfileConfig{
							"work": {ClientID: "work-client"},
						},
					},
				},
			},
			wantField: "clientId",
			wantValue: "top-level-client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := config.WithConfig(context.Background(), tt.appCfg)
			if tt.profile != "" {
				ctx = auth.WithProfile(ctx, tt.profile)
			}

			cfg := &ProviderConfig{}
			injectAuthHandlerSettings(ctx, tt.handlerName, cfg)

			require.NotNil(t, cfg.Settings)
			raw, ok := cfg.Settings[tt.handlerName]
			require.True(t, ok)

			var m map[string]any
			require.NoError(t, json.Unmarshal(raw, &m))
			assert.Equal(t, tt.wantValue, m[tt.wantField])

			// Verify profiles and activeProfile are not forwarded to the plugin.
			assert.Nil(t, m["profiles"])
			assert.Empty(t, m["activeProfile"])
		})
	}
}

func TestPropagateStartupLatency(t *testing.T) {
	tests := []struct {
		name           string
		registerNames  []string
		propagateNames []string
		wantLatency    map[string]time.Duration
	}{
		{
			name:           "sets latency on all registered wrappers",
			registerNames:  []string{"handler-a", "handler-b"},
			propagateNames: []string{"handler-a", "handler-b"},
			wantLatency:    map[string]time.Duration{"handler-a": 42 * time.Millisecond, "handler-b": 42 * time.Millisecond},
		},
		{
			name:           "only propagates to specified names",
			registerNames:  []string{"handler-a", "handler-b"},
			propagateNames: []string{"handler-a"},
			wantLatency:    map[string]time.Duration{"handler-a": 42 * time.Millisecond, "handler-b": 0},
		},
		{
			name:           "unknown names are skipped",
			registerNames:  []string{"handler-a"},
			propagateNames: []string{"handler-a", "nonexistent"},
			wantLatency:    map[string]time.Duration{"handler-a": 42 * time.Millisecond},
		},
		{
			name:           "empty names slice is no-op",
			registerNames:  []string{"handler-a"},
			propagateNames: []string{},
			wantLatency:    map[string]time.Duration{"handler-a": 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reg := auth.NewRegistry()
			client := &AuthHandlerClient{startupDuration: 42 * time.Millisecond}

			for _, name := range tt.registerNames {
				w := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: name})
				require.NoError(t, reg.Register(w))
			}

			propagateStartupLatency(ctx, reg, client, tt.propagateNames)

			for name, want := range tt.wantLatency {
				h, err := reg.Get(name)
				require.NoError(t, err)
				w, ok := h.(*AuthHandlerWrapper)
				require.True(t, ok)
				assert.Equal(t, want, w.startupLatency, "latency mismatch for %s", name)
			}
		})
	}
}

func TestConfigureAndRegisterAuthHandlers_ReturnsRegisteredNames(t *testing.T) {
	ctx := context.Background()
	reg := auth.NewRegistry()
	client := &AuthHandlerClient{startupDuration: 10 * time.Millisecond}

	handlers := []AuthHandlerInfo{
		{Name: "handler-a", DisplayName: "Handler A"},
		{Name: "handler-b", DisplayName: "Handler B"},
	}

	registered := configureAndRegisterAuthHandlers(ctx, reg, client, handlers, nil)
	assert.Equal(t, []string{"handler-a", "handler-b"}, registered)
	assert.True(t, reg.Has("handler-a"))
	assert.True(t, reg.Has("handler-b"))
}

func TestConfigureAndRegisterAuthHandlers_SkipsDuplicates(t *testing.T) {
	ctx := context.Background()
	reg := auth.NewRegistry()
	client := &AuthHandlerClient{startupDuration: 10 * time.Millisecond}

	// Pre-register handler-a.
	existing := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "handler-a"})
	require.NoError(t, reg.Register(existing))

	handlers := []AuthHandlerInfo{
		{Name: "handler-a", DisplayName: "Handler A"},
		{Name: "handler-b", DisplayName: "Handler B"},
	}

	registered := configureAndRegisterAuthHandlers(ctx, reg, client, handlers, nil)
	assert.Equal(t, []string{"handler-b"}, registered)
}

func TestAuthHandlerWrapper_ApplyOverrides_Empty(t *testing.T) {
	t.Parallel()
	mock := &MockAuthHandlerPlugin{}
	client := &AuthHandlerClient{plugin: mock}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "entra"})

	err := wrapper.ApplyOverrides(context.Background(), map[string]string{})
	require.NoError(t, err)
	// No call to ConfigureAuthHandler when overrides are empty.
	assert.Nil(t, mock.lastConfig)
}

func TestAuthHandlerWrapper_ApplyOverrides_MergesOntoBase(t *testing.T) {
	t.Parallel()
	mock := &MockAuthHandlerPlugin{}
	client := &AuthHandlerClient{plugin: mock}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "entra"})

	overrides := map[string]string{
		"clientId": "my-client",
		"tenantId": "my-tenant",
	}
	err := wrapper.ApplyOverrides(context.Background(), overrides)
	require.NoError(t, err)
	require.NotNil(t, mock.lastConfig)

	// The settings should contain the handler name key with the merged JSON.
	raw, ok := mock.lastConfig.Settings["entra"]
	require.True(t, ok)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &settings))
	assert.Equal(t, "my-client", settings["clientId"])
	assert.Equal(t, "my-tenant", settings["tenantId"])
}

func TestAuthHandlerWrapper_ApplyOverrides_SkipsEmptyValues(t *testing.T) {
	t.Parallel()
	mock := &MockAuthHandlerPlugin{}
	client := &AuthHandlerClient{plugin: mock}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "gcp"})

	// Empty values are filtered out before marshaling — they should not
	// appear in the settings sent to the plugin.
	overrides := map[string]string{
		"projectId": "proj-123",
		"region":    "",
	}
	err := wrapper.ApplyOverrides(context.Background(), overrides)
	require.NoError(t, err)
	require.NotNil(t, mock.lastConfig)

	raw := mock.lastConfig.Settings["gcp"]
	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &settings))
	assert.Equal(t, "proj-123", settings["projectId"])
	// Empty string is filtered out — key should not be present.
	assert.NotContains(t, settings, "region")
}

func TestAuthHandlerWrapper_ApplyOverrides_PreservesHostConfig(t *testing.T) {
	t.Parallel()
	mock := &MockAuthHandlerPlugin{}
	client := &AuthHandlerClient{plugin: mock}
	wrapper := NewAuthHandlerWrapper(client, AuthHandlerInfo{Name: "entra"})
	wrapper.hostCfg = hostConfig{
		Quiet:      true,
		NoColor:    true,
		BinaryName: "mycli",
	}

	overrides := map[string]string{"clientId": "abc"}
	err := wrapper.ApplyOverrides(context.Background(), overrides)
	require.NoError(t, err)
	require.NotNil(t, mock.lastConfig)

	// Host-level fields must be preserved from initial config.
	assert.True(t, mock.lastConfig.Quiet, "Quiet should be preserved")
	assert.True(t, mock.lastConfig.NoColor, "NoColor should be preserved")
	assert.Equal(t, "mycli", mock.lastConfig.BinaryName, "BinaryName should be preserved")
}
