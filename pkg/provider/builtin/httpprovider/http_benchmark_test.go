// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func BenchmarkSerializeBody(b *testing.B) {
	body := map[string]any{
		"query":  "graphQuery",
		"count":  3,
		"nested": map[string]any{"a": 1, "b": []any{"x", "y"}},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _, _ = serializeBody(body)
	}
}

func BenchmarkHTTPProvider_Execute_DryRun(b *testing.B) {
	p := NewHTTPProvider()
	b.Cleanup(func() { _ = p.Close() })

	ctx := provider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"url":    "https://example.com/api/data",
		"method": "GET",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}

func BenchmarkHTTPProvider_Execute_GET(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status": "ok", "value": 42}`)) //nolint:errcheck
	}))
	b.Cleanup(srv.Close)

	p := NewHTTPProvider()
	b.Cleanup(func() { _ = p.Close() })

	ctx := config.WithConfig(context.Background(), &config.Config{
		HTTPClient: config.HTTPClientConfig{
			AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
		},
	})
	inputs := map[string]any{
		"url":    srv.URL + "/api/data",
		"method": "GET",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := p.Execute(ctx, inputs); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHTTPProvider_Execute_ConnectionReuse(b *testing.B) {
	var newConns atomic.Int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns.Add(1)
		}
	}
	srv.Start()
	b.Cleanup(srv.Close)

	p := NewHTTPProvider()
	b.Cleanup(func() { _ = p.Close() })

	ctx := config.WithConfig(context.Background(), &config.Config{
		HTTPClient: config.HTTPClientConfig{
			AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
		},
	})
	inputs := map[string]any{
		"url":    srv.URL + "/api/data",
		"method": "GET",
	}

	// Warm the pooled client and TCP connection before measuring steady-state reuse.
	if _, err := p.Execute(ctx, inputs); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := p.Execute(ctx, inputs); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	if got := newConns.Load(); got != 1 {
		b.Fatalf("expected 1 TCP connection across warm-up and benchmark loop, got %d", got)
	}
}

func BenchmarkDeriveOriginHostname(b *testing.B) {
	rawURL := "https://api.cluster-a.example.com:6443/apis/apps/v1/namespaces/default/deployments?limit=50"

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = deriveOriginHostname(rawURL)
	}
}

func BenchmarkStripCrossOriginAuthOnRedirect(b *testing.B) {
	origURL, _ := url.Parse("https://api.cluster-a.example.com:6443/v1/items")
	destURL, _ := url.Parse("https://api.cluster-b.example.com:6443/v1/items")
	via := []*http.Request{{URL: origURL}}
	hook := stripCrossOriginAuthOnRedirect(nil)
	req := &http.Request{
		URL:    destURL,
		Header: http.Header{"Authorization": []string{"Bearer token"}},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		req.Header.Set("Authorization", "Bearer token")
		_ = hook(req, via)
	}
}
