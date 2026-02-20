// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.ClientID)
	assert.Empty(t, cfg.ClientSecret)
	assert.Empty(t, cfg.ImpersonateServiceAccount)
	assert.Empty(t, cfg.Project)
	assert.Contains(t, cfg.DefaultScopes, "openid")
	assert.Contains(t, cfg.DefaultScopes, "email")
	assert.Contains(t, cfg.DefaultScopes, "profile")
	assert.Contains(t, cfg.DefaultScopes, "https://www.googleapis.com/auth/cloud-platform")
}

func TestDefaultADCClientCredentials(t *testing.T) {
	// Verify the well-known Google ADC client credentials are set correctly.
	// These are public values embedded in gcloud's source code.
	assert.NotEmpty(t, DefaultADCClientID)
	assert.Contains(t, DefaultADCClientID, ".apps.googleusercontent.com")
	assert.NotEmpty(t, DefaultADCClientSecret)
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "empty config is valid",
			config:  Config{},
			wantErr: false,
		},
		{
			name: "valid impersonation email",
			config: Config{
				ImpersonateServiceAccount: "my-sa@project.iam.gserviceaccount.com",
			},
			wantErr: false,
		},
		{
			name: "short impersonation email",
			config: Config{
				ImpersonateServiceAccount: "ab",
			},
			wantErr: true,
		},
		{
			name: "full config is valid",
			config: Config{
				ClientID:                  "test-client-id",
				ClientSecret:              "test-secret",
				DefaultScopes:             []string{"openid"},
				ImpersonateServiceAccount: "my-sa@project.iam.gserviceaccount.com",
				Project:                   "my-project",
			},
			wantErr: false,
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
