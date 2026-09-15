// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
)

// ctxWithAllowedCIDRs returns a context carrying an application config that
// exempts the given ranges from private-address blocking.
func ctxWithAllowedCIDRs(cidrs ...string) context.Context {
	return config.WithConfig(context.Background(), &config.Config{
		HTTPClient: config.HTTPClientConfig{AllowedPrivateCIDRs: cidrs},
	})
}

// A solution must not be able to reach the cloud metadata endpoint, and no
// configuration may re-enable it -- not the blanket allowPrivateIPs switch, and
// not an allowlist entry that covers the address.
func TestHTTPProvider_Execute_MetadataAddressIsNeverReachable(t *testing.T) {
	t.Parallel()

	allow := true
	cases := map[string]*config.Config{
		"default deny": {},
		"allowPrivateIPs=true": {
			HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allow},
		},
		"allowlist covering the metadata range": {
			HTTPClient: config.HTTPClientConfig{
				AllowedPrivateCIDRs: []string{"169.254.0.0/16"},
			},
		},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := config.WithConfig(context.Background(), cfg)

			p := NewHTTPProvider()
			_, err := p.Execute(ctx, map[string]any{
				"url":    "http://169.254.169.254/latest/meta-data/",
				"method": "GET",
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, httpc.ErrBlockedByPolicy)
		})
	}
}

// Without an explicit exception a solution cannot reach a private address.
func TestHTTPProvider_Execute_BlocksPrivateIPByDefault(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewHTTPProvider()
	_, err := p.Execute(context.Background(), map[string]any{
		"url":    srv.URL,
		"method": "GET",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, httpc.ErrBlockedByPolicy)
}

// Naming the range is what makes an internal endpoint reachable. This is the
// counterpart to the test above: it proves the deny is configuration-driven and
// not a blanket refusal, so a failure here means the allowlist does not work.
func TestHTTPProvider_Execute_AllowsConfiguredRange(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	p := NewHTTPProvider()
	out, err := p.Execute(ctxWithAllowedCIDRs("127.0.0.0/8", "::1/128"), map[string]any{
		"url":    srv.URL,
		"method": "GET",
	})
	require.NoError(t, err)
	require.NotNil(t, out)
}

func TestValidateNextURLHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		original string
		next     string
		wantErr  bool
	}{
		{name: "same host", original: "https://api.example.com/a", next: "https://api.example.com/b"},
		{name: "relative next", original: "https://api.example.com/a", next: "/page/2"},
		{name: "case-insensitive host", original: "https://API.example.com/a", next: "https://api.example.com/b"},
		{name: "cross host rejected", original: "https://api.example.com/a", next: "http://169.254.169.254/", wantErr: true},
		{name: "invalid original", original: "://bad", next: "https://api.example.com/", wantErr: true},
		{name: "invalid next", original: "https://api.example.com/", next: "://bad", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateNextURLHost(tt.original, tt.next)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func BenchmarkPolicyFromContext(b *testing.B) {
	ctx := ctxWithAllowedCIDRs("10.0.0.0/8", "192.168.0.0/16")
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = httpc.PolicyFromContext(ctx)
	}
}
