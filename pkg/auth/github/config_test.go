// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, DefaultClientID, cfg.ClientID)
	assert.Equal(t, DefaultHostname, cfg.Hostname)
	assert.Equal(t, []string{"gist", "read:org", "repo", "workflow"}, cfg.DefaultScopes)
	assert.Equal(t, 5*time.Second, cfg.MinPollInterval)
	assert.Equal(t, 5*time.Second, cfg.SlowDownIncrement)
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "missing client ID",
			config: &Config{
				Hostname: "github.com",
			},
			wantErr: true,
			errMsg:  "client ID is required",
		},
		{
			name: "missing hostname",
			config: &Config{
				ClientID: "test-id",
			},
			wantErr: true,
			errMsg:  "hostname is required",
		},
		{
			name: "custom values are valid",
			config: &Config{
				ClientID:      "custom-id",
				Hostname:      "github.example.com",
				DefaultScopes: []string{"repo"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfig_GetOAuthBaseURL(t *testing.T) {
	cfg := &Config{Hostname: "github.com"}
	assert.Equal(t, "https://github.com", cfg.GetOAuthBaseURL())

	cfg = &Config{Hostname: "github.example.com"}
	assert.Equal(t, "https://github.example.com", cfg.GetOAuthBaseURL())
}

func TestConfig_GetAPIBaseURL(t *testing.T) {
	cfg := &Config{Hostname: "github.com"}
	assert.Equal(t, "https://api.github.com", cfg.GetAPIBaseURL())

	cfg = &Config{Hostname: "github.example.com"}
	assert.Equal(t, "https://github.example.com/api/v3", cfg.GetAPIBaseURL())
}
