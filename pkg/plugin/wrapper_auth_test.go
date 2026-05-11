// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"testing"

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
