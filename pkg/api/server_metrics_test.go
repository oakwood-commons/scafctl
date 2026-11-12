// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/oakwood-commons/scafctl/pkg/metrics"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// statsPool is a PluginPool that reports fixed stats.
type statsPool struct {
	stats plugin.PoolStats
}

func (p *statsPool) EnsureAndAcquire(context.Context, []solution.PluginDependency) (func(), error) {
	return func() {}, nil
}

func (p *statsPool) Stats() plugin.PoolStats { return p.stats }

func (p *statsPool) Shutdown() {}

// poolGaugeValues collects the plugin pool entries gauge by state.
func poolGaugeValues(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	out := map[string]int64{}
	name := settings.CliBinaryName + "_plugin_pool_entries"
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok, "unexpected data type %T", m.Data)
			for _, dp := range gauge.DataPoints {
				v, _ := dp.Attributes.Value(attribute.Key(metrics.AttrPoolState))
				out[v.AsString()] = dp.Value
			}
		}
	}
	return out
}

// TestServer_ReportPluginPoolMetrics swaps the global meter provider, so it
// must not run in parallel, and it is the only test in this package that does
// so: metrics.InitMetrics binds its instruments once per process.
func TestServer_ReportPluginPoolMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	require.NoError(t, metrics.InitMetrics(context.Background()))

	t.Run("reports pool stats until stopped", func(t *testing.T) {
		pool := &statsPool{stats: plugin.PoolStats{Active: 1, Idle: 2, Dead: 3, Total: 6}}
		srv, err := NewServer(WithServerPluginPool(pool))
		require.NoError(t, err)

		stop := srv.reportPluginPoolMetrics()
		assert.Equal(t, map[string]int64{
			metrics.PoolStateActive: 1,
			metrics.PoolStateIdle:   2,
			metrics.PoolStateDead:   3,
		}, poolGaugeValues(t, reader))

		pool.stats = plugin.PoolStats{Idle: 4, Total: 4}
		assert.Equal(t, int64(4), poolGaugeValues(t, reader)[metrics.PoolStateIdle],
			"the gauge must read live stats on each collection")

		stop()
		assert.Empty(t, poolGaugeValues(t, reader), "stopping must clear the gauge source")
	})

	t.Run("no pool is a no-op", func(t *testing.T) {
		srv, err := NewServer()
		require.NoError(t, err)

		stop := srv.reportPluginPoolMetrics()
		require.NotNil(t, stop)
		assert.Empty(t, poolGaugeValues(t, reader))
		assert.NotPanics(t, stop)
	})
}
