// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	upstream "github.com/oakwood-commons/httpc"
	"github.com/stretchr/testify/assert"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/metrics"
)

func TestOTelMetrics_ImplementsInterface(t *testing.T) {
	var _ upstream.Metrics = OTelMetrics{}
	var _ upstream.Metrics = &OTelMetrics{}
}

// TestOTelMetrics_NoPanic verifies that all methods can be called without
// panicking even when OTel instruments are not initialised (nil globals).
func TestOTelMetrics_NoPanic(t *testing.T) {
	m := OTelMetrics{}
	ctx := context.Background()

	assert.NotPanics(t, func() {
		m.RecordRequestDuration(ctx, "GET", "example.com", "/api", 200, 100*time.Millisecond)
	})
	assert.NotPanics(t, func() {
		m.IncrementRequestsTotal(ctx, "POST", "example.com", "/api", 201)
	})
	assert.NotPanics(t, func() {
		m.IncrementErrorsTotal(ctx, "GET", "example.com", "/api", "timeout")
	})
	assert.NotPanics(t, func() {
		m.IncrementRetries(ctx, "GET", "example.com", "/api")
	})
	assert.NotPanics(t, func() {
		m.IncrementCacheHits(ctx)
	})
	assert.NotPanics(t, func() {
		m.IncrementCacheMisses(ctx)
	})
	assert.NotPanics(t, func() {
		m.SetCacheSizeBytes(1024)
	})
	assert.NotPanics(t, func() {
		m.SetCircuitBreakerState("example.com", 0)
	})
	assert.NotPanics(t, func() {
		m.IncrementConcurrentRequests(ctx)
	})
	assert.NotPanics(t, func() {
		m.DecrementConcurrentRequests(ctx)
	})
	assert.NotPanics(t, func() {
		m.RecordRequestSize(ctx, "POST", "example.com", "/api", 256)
	})
	assert.NotPanics(t, func() {
		m.RecordResponseSize(ctx, "GET", "example.com", "/api", 512)
	})
}

// TestOTelMetrics_WithInitializedInstruments exercises the non-nil code paths
// by initializing OTel instruments first.
func TestOTelMetrics_WithInitializedInstruments(t *testing.T) {
	// InitMetrics populates the package-level metric globals via OTel noop provider.
	err := metrics.InitMetrics(context.Background())
	assert.NoError(t, err)

	m := OTelMetrics{}
	ctx := context.Background()

	// Exercise every method with non-nil instruments. None should panic.
	assert.NotPanics(t, func() {
		m.RecordRequestDuration(ctx, "GET", "host.example", "/path", 200, 50*time.Millisecond)
	})
	assert.NotPanics(t, func() {
		m.IncrementRequestsTotal(ctx, "POST", "host.example", "/path", 201)
	})
	assert.NotPanics(t, func() {
		m.IncrementErrorsTotal(ctx, "GET", "host.example", "/path", "connection_refused")
	})
	assert.NotPanics(t, func() {
		m.IncrementRetries(ctx, "PUT", "host.example", "/path")
	})
	assert.NotPanics(t, func() {
		m.IncrementCacheHits(ctx)
	})
	assert.NotPanics(t, func() {
		m.IncrementCacheMisses(ctx)
	})
	assert.NotPanics(t, func() {
		m.SetCacheSizeBytes(2048)
	})
	assert.NotPanics(t, func() {
		m.SetCircuitBreakerState("host.example", 1)
	})
	assert.NotPanics(t, func() {
		m.IncrementConcurrentRequests(ctx)
	})
	assert.NotPanics(t, func() {
		m.DecrementConcurrentRequests(ctx)
	})
	assert.NotPanics(t, func() {
		m.RecordRequestSize(ctx, "POST", "host.example", "/path", 512)
	})
	assert.NotPanics(t, func() {
		m.RecordResponseSize(ctx, "GET", "host.example", "/path", 1024)
	})
}

func BenchmarkOTelMetrics_RecordRequestDuration(b *testing.B) {
	_ = metrics.InitMetrics(context.Background())
	m := OTelMetrics{}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.RecordRequestDuration(ctx, "GET", "bench.example", "/api/v1/resource", 200, 50*time.Millisecond)
	}
}

func BenchmarkNewClientFromAppConfig(b *testing.B) {
	cfg := &config.HTTPClientConfig{
		Timeout:     "5s",
		RetryMax:    3,
		EnableCache: boolPtr(false),
	}
	logger := logr.Discard()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = NewClientFromAppConfig(cfg, logger)
	}
}
