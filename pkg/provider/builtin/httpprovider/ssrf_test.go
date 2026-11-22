// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
)

// ctxWithAllowedCIDRs returns a context carrying an application config that
// exempts the given ranges from private-address blocking.
func ctxWithAllowedCIDRs(cidrs ...string) context.Context {
	return config.WithConfig(context.Background(), &config.Config{
		HTTPClient: config.HTTPClientConfig{AllowedPrivateCIDRs: config.PrivateCIDRList(cidrs...)},
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
				AllowedPrivateCIDRs: config.PrivateCIDRList("169.254.0.0/16"),
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
		{name: "same host and custom port", original: "https://api.example.com:6443/a", next: "https://api.example.com:6443/b"},
		{name: "https implicit and explicit default port 443 match", original: "https://api.example.com/a", next: "https://api.example.com:443/b"},
		{name: "https explicit 443 to implicit matches", original: "https://api.example.com:443/a", next: "https://api.example.com/b"},
		{name: "http implicit and explicit default port 80 match", original: "http://api.example.com/a", next: "http://api.example.com:80/b"},
		{name: "ipv6 implicit and explicit default port 443 match", original: "https://[::1]/a", next: "https://[::1]:443/b"},
		{name: "relative next", original: "https://api.example.com/a", next: "/page/2"},
		{name: "case-insensitive scheme and host", original: "HTTPS://API.example.com:6443/a", next: "https://api.example.com:6443/b"},
		{name: "cross host rejected", original: "https://api.example.com/a", next: "http://169.254.169.254/", wantErr: true},
		{name: "subdomain change rejected", original: "https://api.example.com/a", next: "https://other.api.example.com/b", wantErr: true},
		{name: "port change rejected", original: "https://api.example.com:6443/a", next: "https://api.example.com:9000/b", wantErr: true},
		{name: "https to http downgrade rejected", original: "https://api.example.com/a", next: "http://api.example.com/b", wantErr: true},
		{name: "http to https scheme change rejected", original: "http://api.example.com/a", next: "https://api.example.com/b", wantErr: true},
		{name: "userinfo in next URL rejected", original: "https://api.example.com/a", next: "https://attacker:secret@api.example.com/b", wantErr: true},
		{name: "network-path reference rejected", original: "https://api.example.com/a", next: "//evil.example.com/b", wantErr: true},
		{name: "non-http scheme without host rejected", original: "https://api.example.com/a", next: "file:///etc/passwd", wantErr: true},
		{name: "out-of-range port rejected", original: "https://api.example.com/a", next: "https://api.example.com:70000/b", wantErr: true},
		{name: "missing original scheme rejected", original: "/relative", next: "/page/2", wantErr: true},
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

func TestResolveNextURL(t *testing.T) {
	t.Parallel()
	resolved, err := resolveNextURL("https://api.example.com:6443/v1/items?page=1", "/v1/items?page=2")
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com:6443/v1/items?page=2", resolved)

	abs, err := resolveNextURL("https://api.example.com:6443/v1/items?page=1", "https://api.example.com:6443/v1/items?page=2")
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com:6443/v1/items?page=2", abs)
}

func TestHTTPProvider_Pagination_BlocksCrossPortNextLinkWithAuth(t *testing.T) {
	t.Parallel()
	secondServerHit := false
	secondSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondServerHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer secondSrv.Close()

	firstSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Link", fmt.Sprintf(`<%s/items?page=2>; rel="next"`, secondSrv.URL))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":1}]`))
	}))
	defer firstSrv.Close()

	mockHandler := auth.NewMockHandler("github")
	mockHandler.SetToken(&auth.Token{
		AccessToken: "secret-pagination-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mockHandler))
	ctx := testContextWithCLIAuth(t, reg)

	p := NewHTTPProvider()
	_, err := p.Execute(ctx, map[string]any{
		"url":          firstSrv.URL + "/items?page=1",
		"method":       "GET",
		"authProvider": "github",
		"pagination": map[string]any{
			"strategy":    "linkHeader",
			"maxPages":    3,
			"collectPath": "body",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cross-origin pagination is not permitted")
	assert.False(t, secondServerHit, "pagination must abort before sending Authorization to another port")

	// Also verify cursor (nextURLPath) and custom (nextURL) strategies reject cross-origin/scheme changes.
	cursorSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"items":[{"id":1}],"next":%q}`, secondSrv.URL+"/items?cursor=2")
	}))
	defer cursorSrv.Close()

	_, err = p.Execute(ctx, map[string]any{
		"url":          cursorSrv.URL + "/items",
		"method":       "GET",
		"authProvider": "github",
		"pagination": map[string]any{
			"strategy":    "cursor",
			"maxPages":    3,
			"nextURLPath": "body.next",
			"collectPath": "body.items",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cross-origin pagination is not permitted")
	assert.False(t, secondServerHit)

	_, err = p.Execute(ctx, map[string]any{
		"url":          cursorSrv.URL + "/items",
		"method":       "GET",
		"authProvider": "github",
		"pagination": map[string]any{
			"strategy":    "custom",
			"maxPages":    3,
			"nextURL":     "body.next",
			"collectPath": "body.items",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cross-origin pagination is not permitted")
	assert.False(t, secondServerHit)
}

func BenchmarkPolicyFromContext(b *testing.B) {
	ctx := ctxWithAllowedCIDRs("10.0.0.0/8", "192.168.0.0/16")
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = httpc.PolicyFromContext(ctx)
	}
}
