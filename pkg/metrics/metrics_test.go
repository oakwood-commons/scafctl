// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── InitMetrics tests ─────────────────────────────────────────────────────────

// TestInitMetrics tests that InitMetrics populates all metric instruments.
// Not parallel: mutates package-level metric globals.
func TestInitMetrics(t *testing.T) {
	err := InitMetrics(context.Background())
	require.NoError(t, err)

	assert.NotNil(t, HTTPClientDuration)
	assert.NotNil(t, HTTPClientRequestsTotal)
	assert.NotNil(t, HTTPClientErrorsTotal)
	assert.NotNil(t, HTTPClientRetriesTotal)
	assert.NotNil(t, HTTPClientCacheHits)
	assert.NotNil(t, HTTPClientCacheMisses)
	assert.NotNil(t, HTTPClientRequestSize)
	assert.NotNil(t, HTTPClientResponseSize)
	assert.NotNil(t, GetSolutionTimeHistogram)
	assert.NotNil(t, ProviderExecutionDuration)
	assert.NotNil(t, ProviderExecutionTotal)
}

// Not parallel: calls InitMetrics which mutates package-level globals.
func TestInitMetrics_Idempotent(t *testing.T) {
	err1 := InitMetrics(context.Background())
	err2 := InitMetrics(context.Background())
	require.NoError(t, err1)
	require.NoError(t, err2)
}

// ── SetCacheSizeBytes tests ───────────────────────────────────────────────────

func TestSetCacheSizeBytes(t *testing.T) {
	SetCacheSizeBytes(42)
	assert.Equal(t, int64(42), cacheSizeBytesVal.Load())

	SetCacheSizeBytes(0)
	assert.Equal(t, int64(0), cacheSizeBytesVal.Load())

	SetCacheSizeBytes(1024 * 1024)
	assert.Equal(t, int64(1024*1024), cacheSizeBytesVal.Load())
}

// ── SetCircuitBreakerState tests ──────────────────────────────────────────────

func TestSetCircuitBreakerState(t *testing.T) {
	SetCircuitBreakerState("host-a", 0) // closed
	SetCircuitBreakerState("host-b", 1) // open
	SetCircuitBreakerState("host-c", 2) // half-open

	v, ok := circuitBreakerStates.Load("host-a")
	require.True(t, ok)
	assert.Equal(t, float64(0), v)

	v, ok = circuitBreakerStates.Load("host-b")
	require.True(t, ok)
	assert.Equal(t, float64(1), v)

	v, ok = circuitBreakerStates.Load("host-c")
	require.True(t, ok)
	assert.Equal(t, float64(2), v)
}

func TestSetCircuitBreakerState_Overwrite(t *testing.T) {
	SetCircuitBreakerState("host-x", 0)
	SetCircuitBreakerState("host-x", 1)

	v, ok := circuitBreakerStates.Load("host-x")
	require.True(t, ok)
	assert.Equal(t, float64(1), v)
}

// ── IncConcurrentRequests / DecConcurrentRequests tests ───────────────────────

// This test mutates package globals; do NOT add t.Parallel().
func TestIncDecConcurrentRequests_NilSafe(t *testing.T) {
	saved := httpClientConcurrentRequests
	httpClientConcurrentRequests = nil
	defer func() { httpClientConcurrentRequests = saved }()

	assert.NotPanics(t, func() {
		IncConcurrentRequests(context.Background())
	})
	assert.NotPanics(t, func() {
		DecConcurrentRequests(context.Background())
	})
}

// Not parallel: calls InitMetrics which mutates package-level globals.
func TestIncDecConcurrentRequests(t *testing.T) {
	_ = InitMetrics(context.Background())

	assert.NotPanics(t, func() {
		IncConcurrentRequests(context.Background())
		DecConcurrentRequests(context.Background())
	})
}

// ── Handler tests ─────────────────────────────────────────────────────────────

func TestHandler_ReturnsHTTPHandler(t *testing.T) {
	h := Handler()
	require.NotNil(t, h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// ── RecordProviderExecution tests ─────────────────────────────────────────────

// Not parallel: calls InitMetrics which mutates package-level globals.
func TestRecordProviderExecution_Success(t *testing.T) {
	_ = InitMetrics(context.Background())
	assert.NotPanics(t, func() {
		RecordProviderExecution(context.Background(), "static", 0.5, true)
	})
}

// Not parallel: calls InitMetrics which mutates package-level globals.
func TestRecordProviderExecution_Failure(t *testing.T) {
	_ = InitMetrics(context.Background())
	assert.NotPanics(t, func() {
		RecordProviderExecution(context.Background(), "http", 1.2, false)
	})
}

// This test mutates package globals; do NOT add t.Parallel().
func TestRecordProviderExecution_NilInstruments(t *testing.T) {
	savedDur := ProviderExecutionDuration
	savedTotal := ProviderExecutionTotal
	ProviderExecutionDuration = nil
	ProviderExecutionTotal = nil
	defer func() {
		ProviderExecutionDuration = savedDur
		ProviderExecutionTotal = savedTotal
	}()

	assert.NotPanics(t, func() {
		RecordProviderExecution(context.Background(), "test", 0.1, true)
	})
}

// ── Plugin Resolution metrics tests ──────────────────────────────────────────

// Not parallel: calls InitMetrics which mutates package-level globals.
func TestRecordPluginResolution_CacheHit(t *testing.T) {
	_ = InitMetrics(context.Background())
	assert.NotPanics(t, func() {
		RecordPluginResolution(context.Background(), "hcl", "cache", 0.02, true)
	})
}

// Not parallel: calls InitMetrics which mutates package-level globals.
func TestRecordPluginResolution_RegistryFetch(t *testing.T) {
	_ = InitMetrics(context.Background())
	assert.NotPanics(t, func() {
		RecordPluginResolution(context.Background(), "hcl", "registry", 5.1, true)
	})
}

// Not parallel: calls InitMetrics which mutates package-level globals.
func TestRecordPluginResolution_Failure(t *testing.T) {
	_ = InitMetrics(context.Background())
	assert.NotPanics(t, func() {
		RecordPluginResolution(context.Background(), "unknown-plugin", "registry", 2.0, false)
	})
}

// This test mutates package globals; do NOT add t.Parallel().
func TestRecordPluginResolution_NilInstruments(t *testing.T) {
	savedDur := PluginResolutionDuration
	savedTotal := PluginResolutionTotal
	PluginResolutionDuration = nil
	PluginResolutionTotal = nil
	defer func() {
		PluginResolutionDuration = savedDur
		PluginResolutionTotal = savedTotal
	}()

	assert.NotPanics(t, func() {
		RecordPluginResolution(context.Background(), "test", "cache", 0.01, true)
	})
}

// ── Constants tests ───────────────────────────────────────────────────────────

func TestConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "status_code", AttrStatusCode)
	assert.Equal(t, "url", AttrURL)
	assert.Equal(t, "path", AttrPath)
	assert.Equal(t, "method", AttrMethod)
	assert.Equal(t, "error_type", AttrErrorType)
	assert.Equal(t, "host", LabelHost)
	assert.Equal(t, "path_template", LabelPathTemplate)
	assert.Equal(t, "provider_name", AttrProviderName)
	assert.Equal(t, "status", AttrStatus)
	assert.Equal(t, "plugin_name", AttrPluginName)
	assert.Equal(t, "source", AttrPluginSource)
}

// ── Benchmark tests ───────────────────────────────────────────────────────────

func BenchmarkRecordProviderExecution(b *testing.B) {
	_ = InitMetrics(context.Background())
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		RecordProviderExecution(ctx, "bench-provider", 0.5, true)
	}
}

func BenchmarkRecordPluginResolution(b *testing.B) {
	_ = InitMetrics(context.Background())
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		RecordPluginResolution(ctx, "bench-plugin", "cache", 0.02, true)
	}
}

func BenchmarkSetCacheSizeBytes(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var i int64
	for b.Loop() {
		SetCacheSizeBytes(i)
		i++
	}
}

func BenchmarkSetCircuitBreakerState(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var i int
	for b.Loop() {
		SetCircuitBreakerState("bench-host", float64(i%3))
		i++
	}
}
