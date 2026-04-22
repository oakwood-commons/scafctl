// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateURLNotPrivate(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		// Should be blocked
		{name: "IPv4 loopback", url: "http://127.0.0.1/", wantErr: true},
		{name: "IPv4 loopback alt", url: "http://127.1.2.3/", wantErr: true},
		{name: "RFC1918 10.x", url: "http://10.0.0.1/api", wantErr: true},
		{name: "RFC1918 172.16.x", url: "http://172.16.0.1/", wantErr: true},
		{name: "RFC1918 172.31.x", url: "http://172.31.255.255/", wantErr: true},
		{name: "RFC1918 192.168.x", url: "http://192.168.1.1/", wantErr: true},
		{name: "link-local IMDS", url: "http://169.254.169.254/latest/meta-data/", wantErr: true},
		{name: "CGNAT", url: "http://100.64.0.1/", wantErr: true},
		{name: "IPv6 loopback", url: "http://[::1]/", wantErr: true},
		{name: "decimal IP 2130706433", url: "http://2130706433/", wantErr: true},
		{name: "hex IP 0x7f000001", url: "http://0x7f000001/", wantErr: true},
		{name: "octal IP 0177.0.0.1", url: "http://0177.0.0.1/", wantErr: true},
		{name: "pure octal 017700000001", url: "http://017700000001/", wantErr: true},
		{name: "localhost hostname", url: "http://localhost/api", wantErr: true},
		{name: "localhost.localdomain", url: "http://localhost.localdomain/api", wantErr: true},
		{name: "metadata.google.internal", url: "http://metadata.google.internal/computeMetadata/v1/", wantErr: true},

		// Should be allowed
		{name: "public IP", url: "https://8.8.8.8/", wantErr: false},
		{name: "hostname", url: "https://api.example.com/", wantErr: false},
		{name: "public HTTPS", url: "https://graph.microsoft.com/v1.0/me", wantErr: false},
		{name: "invalid URL", url: "://bad", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateURLNotPrivate(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPrivateIPsAllowed(t *testing.T) {
	falseVal := false
	trueVal := true

	t.Run("nil config defaults to denied", func(t *testing.T) {
		assert.False(t, privateIPsAllowed(context.Background()))
	})

	t.Run("config with nil AllowPrivateIPs defaults to denied", func(t *testing.T) {
		ctx := config.WithConfig(context.Background(), &config.Config{})
		assert.False(t, privateIPsAllowed(ctx))
	})

	t.Run("config with AllowPrivateIPs=true", func(t *testing.T) {
		cfg := &config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &trueVal}}
		ctx := config.WithConfig(context.Background(), cfg)
		assert.True(t, privateIPsAllowed(ctx))
	})

	t.Run("config with AllowPrivateIPs=false", func(t *testing.T) {
		cfg := &config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &falseVal}}
		ctx := config.WithConfig(context.Background(), cfg)
		assert.False(t, privateIPsAllowed(ctx))
	})
}

func BenchmarkValidateURLNotPrivate(b *testing.B) {
	urls := []string{
		"https://api.example.com/users",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/internal",
		"https://graph.microsoft.com/v1.0/me",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateURLNotPrivate(urls[i%len(urls)])
	}
}

func TestHTTPProvider_Execute_BlocksPrivateIP(t *testing.T) {
	falseVal := false
	cfg := &config.Config{HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &falseVal}}
	ctx := config.WithConfig(context.Background(), cfg)

	p := NewHTTPProvider()
	_, err := p.Execute(ctx, map[string]any{
		"url":    "http://169.254.169.254/latest/meta-data/",
		"method": "GET",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AllowPrivateIPs")
}
