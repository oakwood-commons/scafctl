// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

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
