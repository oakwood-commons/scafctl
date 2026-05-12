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
