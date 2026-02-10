// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "04b07795-8ddb-461a-bbee-02f9e1bf7b46", cfg.ClientID)
	assert.Equal(t, "common", cfg.TenantID)
	assert.Equal(t, "https://login.microsoftonline.com", cfg.Authority)
	assert.Contains(t, cfg.DefaultScopes, "openid")
	assert.Contains(t, cfg.DefaultScopes, "profile")
	assert.Contains(t, cfg.DefaultScopes, "offline_access")
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "missing client ID",
			config: &Config{
				TenantID: "common",
			},
			wantErr: true,
		},
		{
			name: "missing tenant ID",
			config: &Config{
				ClientID: "test-client-id",
			},
			wantErr: true,
		},
		{
			name:    "empty config",
			config:  &Config{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_GetAuthority(t *testing.T) {
	tests := []struct {
		name      string
		authority string
		want      string
	}{
		{
			name:      "default authority",
			authority: "",
			want:      "https://login.microsoftonline.com",
		},
		{
			name:      "custom authority",
			authority: "https://login.microsoftonline.us",
			want:      "https://login.microsoftonline.us",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Authority: tt.authority}
			assert.Equal(t, tt.want, cfg.GetAuthority())
		})
	}
}

func TestConfig_GetAuthorityWithTenant(t *testing.T) {
	cfg := DefaultConfig()

	// With default authority
	assert.Equal(t, "https://login.microsoftonline.com/my-tenant", cfg.GetAuthorityWithTenant("my-tenant"))

	// With custom authority
	cfg.Authority = "https://login.microsoftonline.us"
	assert.Equal(t, "https://login.microsoftonline.us/my-tenant", cfg.GetAuthorityWithTenant("my-tenant"))
}
