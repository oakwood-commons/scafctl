// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuthType_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		authType AuthType
		want     bool
	}{
		{name: "auto/empty", authType: AuthTypeAuto, want: true},
		{name: "oauth", authType: AuthTypeOAuth, want: true},
		{name: "oidc", authType: AuthTypeOIDC, want: true},
		{name: "unknown", authType: AuthType("saml"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.authType.Valid())
		})
	}
}

func TestClusterInfo_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		info    ClusterInfo
		wantErr error
	}{
		{
			name: "valid minimal",
			info: ClusterInfo{Name: "prod"},
		},
		{
			name: "valid full",
			info: ClusterInfo{
				Name:            "prod",
				APIServerURL:    "https://api.example.com:6443",
				ConsoleURL:      "https://console.example.com",
				AuthType:        AuthTypeOIDC,
				OIDCAudience:    "client-id",
				InsecureSkipTLS: true,
			},
		},
		{
			name:    "empty name",
			info:    ClusterInfo{Name: ""},
			wantErr: ErrEmptyClusterName,
		},
		{
			name:    "whitespace name",
			info:    ClusterInfo{Name: "   "},
			wantErr: ErrEmptyClusterName,
		},
		{
			name:    "invalid auth type",
			info:    ClusterInfo{Name: "prod", AuthType: AuthType("saml")},
			wantErr: ErrInvalidAuthType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.info.Validate()
			if tt.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
