// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateVerificationURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		trusted []string
		wantErr string
	}{
		// Valid cases
		{
			name:    "valid HTTPS github.com with empty trusted list",
			uri:     "https://github.com/login/device",
			trusted: nil,
		},
		{
			name:    "valid HTTPS with trusted match",
			uri:     "https://github.com/login/device",
			trusted: []string{"github.com"},
		},
		{
			name:    "valid HTTPS subdomain match",
			uri:     "https://login.microsoftonline.com/common/oauth2/deviceauth",
			trusted: []string{"microsoftonline.com"},
		},
		{
			name:    "valid HTTPS exact domain match in list",
			uri:     "https://accounts.google.com/o/oauth2/device/code",
			trusted: []string{"github.com", "accounts.google.com", "login.microsoftonline.com"},
		},
		{
			name:    "valid HTTPS with explicit port 443",
			uri:     "https://github.com:443/login/device",
			trusted: []string{"github.com"},
		},

		// Scheme failures
		{
			name:    "HTTP rejected",
			uri:     "http://github.com/login/device",
			trusted: nil,
			wantErr: "must use HTTPS",
		},
		{
			name:    "FTP rejected",
			uri:     "ftp://github.com/login/device",
			trusted: nil,
			wantErr: "must use HTTPS",
		},
		{
			name:    "empty scheme rejected",
			uri:     "://github.com/login/device",
			trusted: nil,
			wantErr: "invalid verification URI",
		},

		// Private/loopback failures
		{
			name:    "localhost rejected",
			uri:     "https://localhost/auth",
			trusted: nil,
			wantErr: "must not target private network",
		},
		{
			name:    "127.0.0.1 rejected",
			uri:     "https://127.0.0.1/auth",
			trusted: nil,
			wantErr: "must not target private network",
		},
		{
			name:    "IPv6 loopback rejected",
			uri:     "https://[::1]/auth",
			trusted: nil,
			wantErr: "must not target private network",
		},
		{
			name:    "private 10.x rejected",
			uri:     "https://10.0.0.1/auth",
			trusted: nil,
			wantErr: "must not target private network",
		},
		{
			name:    "private 192.168.x rejected",
			uri:     "https://192.168.1.1/auth",
			trusted: nil,
			wantErr: "must not target private network",
		},
		{
			name:    "link-local rejected",
			uri:     "https://169.254.169.254/auth",
			trusted: nil,
			wantErr: "must not target private network",
		},

		// Port failures
		{
			name:    "non-standard port rejected",
			uri:     "https://github.com:8443/login/device",
			trusted: nil,
			wantErr: "non-standard port",
		},
		{
			name:    "port 80 rejected",
			uri:     "https://github.com:80/login/device",
			trusted: nil,
			wantErr: "non-standard port",
		},

		// Trusted domain failures
		{
			name:    "domain not in trusted list",
			uri:     "https://evil.example.com/login/device",
			trusted: []string{"github.com", "microsoftonline.com"},
			wantErr: "not in trusted domains",
		},
		{
			name:    "partial match is not a subdomain match",
			uri:     "https://notgithub.com/login/device",
			trusted: []string{"github.com"},
			wantErr: "not in trusted domains",
		},
		{
			name:    "trusted domain as suffix without dot separator rejected",
			uri:     "https://evilgithub.com/login/device",
			trusted: []string{"github.com"},
			wantErr: "not in trusted domains",
		},

		// Edge cases
		{
			name:    "empty URI",
			uri:     "",
			trusted: nil,
			wantErr: "must use HTTPS",
		},
		{
			name:    "empty hostname",
			uri:     "https:///path",
			trusted: nil,
			wantErr: "empty hostname",
		},

		// Case-insensitive hostname matching
		{
			name:    "mixed-case hostname matches lowercase trusted domain",
			uri:     "https://GitHub.Com/login/device",
			trusted: []string{"github.com"},
		},
		{
			name:    "uppercase hostname matches lowercase trusted domain",
			uri:     "https://LOGIN.MICROSOFTONLINE.COM/common/oauth2/deviceauth",
			trusted: []string{"microsoftonline.com"},
		},
		{
			name:    "lowercase hostname matches mixed-case trusted domain",
			uri:     "https://github.com/login/device",
			trusted: []string{"GitHub.COM"},
		},
		{
			name:    "trailing dot in hostname normalized",
			uri:     "https://github.com./login/device",
			trusted: []string{"github.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVerificationURI(tt.uri, tt.trusted)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestIsPrivateOrLoopback(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.0.1", true},
		{"169.254.1.1", true},
		{"8.8.8.8", false},
		{"github.com", false},
		{"login.microsoftonline.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			assert.Equal(t, tt.want, isPrivateOrLoopback(tt.host))
		})
	}
}
