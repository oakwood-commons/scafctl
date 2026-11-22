// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

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
	ctx := context.Background()
	inputs := map[string]any{
		"url":    srv.URL + "/api/data",
		"method": "GET",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
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
