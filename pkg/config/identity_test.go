// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIEntraIdentityConfig_Validate_ValidSecret(t *testing.T) {
	t.Parallel()
	cfg := &APIEntraIdentityConfig{
		TenantID: "00000000-0000-0000-0000-000000000000",
		ClientID: "11111111-1111-1111-1111-111111111111",
		Credential: ServerCredentialConfig{
			Type:         "secret",
			ClientSecret: "env://SCAFCTL_API_ENTRA_CLIENT_SECRET",
		},
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestAPIEntraIdentityConfig_Validate_ValidWIF(t *testing.T) {
	t.Parallel()
	cfg := &APIEntraIdentityConfig{
		TenantID: "00000000-0000-0000-0000-000000000000",
		ClientID: "11111111-1111-1111-1111-111111111111",
		Credential: ServerCredentialConfig{
			Type:         "wif",
			WIFTokenPath: "/var/run/secrets/azure/tokens/federated-token",
		},
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestAPIEntraIdentityConfig_Validate_MissingTenantID(t *testing.T) {
	t.Parallel()
	cfg := &APIEntraIdentityConfig{
		ClientID: "11111111-1111-1111-1111-111111111111",
		Credential: ServerCredentialConfig{
			Type:         "secret",
			ClientSecret: "env://SECRET",
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenantId is required")
}

func TestAPIEntraIdentityConfig_Validate_MissingClientID(t *testing.T) {
	t.Parallel()
	cfg := &APIEntraIdentityConfig{
		TenantID: "00000000-0000-0000-0000-000000000000",
		Credential: ServerCredentialConfig{
			Type:         "secret",
			ClientSecret: "env://SECRET",
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clientId is required")
}

func TestAPIEntraIdentityConfig_Validate_MissingCredentialType(t *testing.T) {
	t.Parallel()
	cfg := &APIEntraIdentityConfig{
		TenantID: "00000000-0000-0000-0000-000000000000",
		ClientID: "11111111-1111-1111-1111-111111111111",
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credential:")
	assert.Contains(t, err.Error(), "type is required")
}

func TestAPIEntraIdentityConfig_Validate_InvalidCredentialType(t *testing.T) {
	t.Parallel()
	cfg := &APIEntraIdentityConfig{
		TenantID: "00000000-0000-0000-0000-000000000000",
		ClientID: "11111111-1111-1111-1111-111111111111",
		Credential: ServerCredentialConfig{
			Type: "certificate",
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credential:")
	assert.Contains(t, err.Error(), "invalid value")
	assert.Contains(t, err.Error(), "certificate")
}

func TestAPIEntraIdentityConfig_Validate_SecretMissingRef(t *testing.T) {
	t.Parallel()
	cfg := &APIEntraIdentityConfig{
		TenantID: "00000000-0000-0000-0000-000000000000",
		ClientID: "11111111-1111-1111-1111-111111111111",
		Credential: ServerCredentialConfig{
			Type: "secret",
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clientSecret is required")
}

func TestAPIEntraIdentityConfig_Validate_WIFMissingPath(t *testing.T) {
	t.Parallel()
	cfg := &APIEntraIdentityConfig{
		TenantID: "00000000-0000-0000-0000-000000000000",
		ClientID: "11111111-1111-1111-1111-111111111111",
		Credential: ServerCredentialConfig{
			Type: "wif",
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wifTokenPath is required")
}

func TestAPIIdentityConfig_Validate_NilEntra(t *testing.T) {
	t.Parallel()
	cfg := &APIIdentityConfig{}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestAPIIdentityConfig_Validate_DelegatesToEntra(t *testing.T) {
	t.Parallel()
	cfg := &APIIdentityConfig{
		Entra: &APIEntraIdentityConfig{
			Credential: ServerCredentialConfig{Type: "secret"},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entra:")
	assert.Contains(t, err.Error(), "tenantId is required")
}

func TestDelegationFlowsConfig_IsFlowPermitted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		config   *DelegationFlowsConfig
		flow     string
		expected bool
	}{
		{
			name:     "nil allows OBO",
			config:   nil,
			flow:     DelegationFlowOBO,
			expected: true,
		},
		{
			name:     "nil denies client_credentials",
			config:   nil,
			flow:     DelegationFlowClientCredentials,
			expected: false,
		},
		{
			name:     "empty flows denies OBO",
			config:   &DelegationFlowsConfig{},
			flow:     DelegationFlowOBO,
			expected: false,
		},
		{
			name:     "empty flows denies client_credentials",
			config:   &DelegationFlowsConfig{},
			flow:     DelegationFlowClientCredentials,
			expected: false,
		},
		{
			name:     "explicit OBO permits OBO",
			config:   &DelegationFlowsConfig{Flows: []string{DelegationFlowOBO}},
			flow:     DelegationFlowOBO,
			expected: true,
		},
		{
			name:     "explicit OBO denies client_credentials",
			config:   &DelegationFlowsConfig{Flows: []string{DelegationFlowOBO}},
			flow:     DelegationFlowClientCredentials,
			expected: false,
		},
		{
			name:     "both flows permits OBO",
			config:   &DelegationFlowsConfig{Flows: []string{DelegationFlowOBO, DelegationFlowClientCredentials}},
			flow:     DelegationFlowOBO,
			expected: true,
		},
		{
			name:     "both flows permits client_credentials",
			config:   &DelegationFlowsConfig{Flows: []string{DelegationFlowOBO, DelegationFlowClientCredentials}},
			flow:     DelegationFlowClientCredentials,
			expected: true,
		},
		{
			name:     "unknown flow denied",
			config:   &DelegationFlowsConfig{Flows: []string{DelegationFlowOBO}},
			flow:     "unknown",
			expected: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.config.IsFlowPermitted(tc.flow)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestDelegationFlowsConfig_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		config  *DelegationFlowsConfig
		wantErr string
	}{
		{
			name:   "nil is valid",
			config: nil,
		},
		{
			name:   "empty flows is valid",
			config: &DelegationFlowsConfig{},
		},
		{
			name:   "valid OBO",
			config: &DelegationFlowsConfig{Flows: []string{DelegationFlowOBO}},
		},
		{
			name:   "valid client_credentials",
			config: &DelegationFlowsConfig{Flows: []string{DelegationFlowClientCredentials}},
		},
		{
			name:   "valid both",
			config: &DelegationFlowsConfig{Flows: []string{DelegationFlowOBO, DelegationFlowClientCredentials}},
		},
		{
			name:    "invalid flow",
			config:  &DelegationFlowsConfig{Flows: []string{"password"}},
			wantErr: "invalid flow",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.config.Validate()
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAPIEntraIdentityConfig_Validate_AllowedFlowsInvalid(t *testing.T) {
	t.Parallel()
	cfg := &APIEntraIdentityConfig{
		TenantID: "00000000-0000-0000-0000-000000000000",
		ClientID: "11111111-1111-1111-1111-111111111111",
		Credential: ServerCredentialConfig{
			Type:         "secret",
			ClientSecret: "env://SECRET",
		},
		AllowedFlows: &DelegationFlowsConfig{Flows: []string{"invalid"}},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "allowedFlows:")
	assert.Contains(t, err.Error(), "invalid flow")
}

func TestAPIEntraIdentityConfig_Validate_AllowedFlowsValid(t *testing.T) {
	t.Parallel()
	cfg := &APIEntraIdentityConfig{
		TenantID: "00000000-0000-0000-0000-000000000000",
		ClientID: "11111111-1111-1111-1111-111111111111",
		Credential: ServerCredentialConfig{
			Type:         "secret",
			ClientSecret: "env://SECRET",
		},
		AllowedFlows: &DelegationFlowsConfig{Flows: []string{DelegationFlowOBO, DelegationFlowClientCredentials}},
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}
