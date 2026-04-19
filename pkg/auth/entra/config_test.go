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

func TestQualifyScope(t *testing.T) {
	tests := []struct {
		name  string
		scope string
		want  string
	}{
		{
			name:  "fully qualified graph scope",
			scope: "https://graph.microsoft.com/.default",
			want:  "https://graph.microsoft.com/.default",
		},
		{
			name:  "fully qualified custom API scope",
			scope: "api://my-api/.default",
			want:  "api://my-api/.default",
		},
		{
			name:  "bare permission name",
			scope: "User.Read",
			want:  "https://graph.microsoft.com/User.Read",
		},
		{
			name:  "bare group read all",
			scope: "Group.Read.All",
			want:  "https://graph.microsoft.com/Group.Read.All",
		},
		{
			name:  "openid is well-known",
			scope: "openid",
			want:  "openid",
		},
		{
			name:  "profile is well-known",
			scope: "profile",
			want:  "profile",
		},
		{
			name:  "offline_access is well-known",
			scope: "offline_access",
			want:  "offline_access",
		},
		{
			name:  "email is well-known",
			scope: "email",
			want:  "email",
		},
		{
			name:  "scope with slash is already qualified",
			scope: "https://management.azure.com/.default",
			want:  "https://management.azure.com/.default",
		},
		{
			name:  "space-delimited multi-scope with bare permissions",
			scope: "openid profile User.Read offline_access",
			want:  "openid profile https://graph.microsoft.com/User.Read offline_access",
		},
		{
			name:  "space-delimited all well-known",
			scope: "openid profile email",
			want:  "openid profile email",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, QualifyScope(tt.scope))
		})
	}
}

func BenchmarkQualifyScope(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		QualifyScope("User.Read")
	}
}
