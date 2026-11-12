// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/oakwood-commons/scafctl/pkg/metrics"
	"github.com/oakwood-commons/scafctl/pkg/plugin/identity"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

func TestPoolStats_MetricsSnapshot(t *testing.T) {
	t.Parallel()
	got := PoolStats{Active: 1, Idle: 2, Dead: 3, Total: 6, Evicted: 9}.MetricsSnapshot()
	assert.Equal(t, metrics.PluginPoolStats{Active: 1, Idle: 2, Dead: 3}, got)
}

func TestEvictReason(t *testing.T) {
	t.Parallel()
	assert.Equal(t, metrics.EvictReasonDead, evictReason(true))
	assert.Equal(t, metrics.EvictReasonIdle, evictReason(false))
}

// evictionCounts collects the plugin pool evictions counter by reason.
func evictionCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	out := map[string]int64{}
	name := settings.CliBinaryName + "_plugin_pool_evictions_total"
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "unexpected data type %T", m.Data)
			for _, dp := range sum.DataPoints {
				v, _ := dp.Attributes.Value(attribute.Key(metrics.AttrEvictReason))
				out[v.AsString()] = dp.Value
			}
		}
	}
	return out
}

// TestPoolEvict_RecordsEvictionMetric verifies both pool implementations
// count evictions by reason. It swaps the global meter provider, so it must
// not run in parallel, and it is the only test in this package that does so:
// metrics.InitMetrics binds its instruments once per process.
func TestPoolEvict_RecordsEvictionMetric(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	require.NoError(t, metrics.InitMetrics(context.Background()))

	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	ready := make(chan struct{})
	close(ready)

	tests := []struct {
		name   string
		reason string
		evict  func(t *testing.T)
	}{
		{
			name:   "pool idle entry",
			reason: metrics.EvictReasonIdle,
			evict: func(t *testing.T) {
				p := NewPool(context.Background(), nil, provider.NewRegistry(), logr.Discard(),
					WithIdleTimeout(time.Minute), withClock(func() time.Time { return now }))
				t.Cleanup(p.Shutdown)
				p.mu.Lock()
				p.entries["idle"] = &poolEntry{
					state:    entryReady,
					client:   &fakePluginClient{},
					dep:      solution.PluginDependency{Name: "idle", Kind: solution.PluginKindProvider},
					ready:    ready,
					lastUsed: now.Add(-2 * time.Minute),
				}
				p.mu.Unlock()
				p.evict()
			},
		},
		{
			name:   "pool dead entry",
			reason: metrics.EvictReasonDead,
			evict: func(t *testing.T) {
				p := NewPool(context.Background(), nil, provider.NewRegistry(), logr.Discard(),
					WithIdleTimeout(time.Minute))
				t.Cleanup(p.Shutdown)
				p.mu.Lock()
				p.entries["dead"] = &poolEntry{
					state: entryDead,
					dep:   solution.PluginDependency{Name: "dead", Kind: solution.PluginKindProvider},
					ready: ready,
				}
				p.mu.Unlock()
				p.evict()
			},
		},
		{
			name:   "version pool idle entry",
			reason: metrics.EvictReasonIdle,
			evict: func(t *testing.T) {
				vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{},
					WithVersionPoolIdleTimeout(time.Minute), clockAt(now))
				t.Cleanup(vp.Shutdown)
				seedEntry(vp, identity.NewPluginIdentity("plugin", "official"), "1.0.0",
					providerDep("plugin", "1.0.0"),
					withLastUsed(now.Add(-2*time.Minute)), withClient(&fakePluginClient{}))
				vp.evict()
			},
		},
		{
			name:   "version pool dead entry",
			reason: metrics.EvictReasonDead,
			evict: func(t *testing.T) {
				vp := NewVersionPool(context.Background(), nil, &fakeProviderRegistry{},
					WithVersionPoolIdleTimeout(time.Minute), clockAt(now))
				t.Cleanup(vp.Shutdown)
				seedEntry(vp, identity.NewPluginIdentity("plugin", "official"), "1.0.0",
					providerDep("plugin", "1.0.0"),
					withState(entryDead), withLastUsed(now), withClient(&fakePluginClient{}))
				vp.evict()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := evictionCounts(t, reader)
			tt.evict(t)
			after := evictionCounts(t, reader)

			assert.Equal(t, before[tt.reason]+1, after[tt.reason], "eviction must be counted under %q", tt.reason)
			for reason, n := range after {
				if reason != tt.reason {
					assert.Equal(t, before[reason], n, "no eviction expected under %q", reason)
				}
			}
		})
	}
}
