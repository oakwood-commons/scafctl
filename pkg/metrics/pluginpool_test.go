// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const testPoolBinaryName = "mycli"

// newPluginPoolReader binds the plugin pool instruments to a meter backed by a
// manual reader and restores the package globals when the test ends. Tests
// using it must not run in parallel because the instruments are globals.
func newPluginPoolReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	prevEvictions := pluginPoolEvictionsTotal
	t.Cleanup(func() {
		pluginPoolEvictionsTotal = prevEvictions
		SetPluginPoolStatsFunc(nil)
		_ = mp.Shutdown(context.Background())
	})

	require.NoError(t, registerPluginPoolInstruments(mp.Meter("test"), testPoolBinaryName))
	return reader
}

// findMetric collects from reader and returns the metric with the given name.
func findMetric(t *testing.T, reader *sdkmetric.ManualReader, name string) (metricdata.Metrics, bool) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m, true
			}
		}
	}
	return metricdata.Metrics{}, false
}

// pointsByAttr maps each data point's value of attrKey to the point's value.
func pointsByAttr(t *testing.T, points []metricdata.DataPoint[int64], attrKey string) map[string]int64 {
	t.Helper()
	out := make(map[string]int64, len(points))
	for _, dp := range points {
		v, ok := dp.Attributes.Value(attribute.Key(attrKey))
		require.True(t, ok, "data point missing %q attribute", attrKey)
		out[v.AsString()] = dp.Value
	}
	return out
}

func TestPluginPoolEntriesGauge(t *testing.T) {
	reader := newPluginPoolReader(t)
	SetPluginPoolStatsFunc(func() PluginPoolStats {
		return PluginPoolStats{Active: 2, Idle: 3, Dead: 1}
	})

	m, ok := findMetric(t, reader, testPoolBinaryName+"_plugin_pool_entries")
	require.True(t, ok, "gauge not reported")
	gauge, ok := m.Data.(metricdata.Gauge[int64])
	require.True(t, ok, "unexpected data type %T", m.Data)

	assert.Equal(t, map[string]int64{
		PoolStateActive: 2,
		PoolStateIdle:   3,
		PoolStateDead:   1,
	}, pointsByAttr(t, gauge.DataPoints, AttrPoolState))
}

func TestPluginPoolEntriesGauge_NoSource(t *testing.T) {
	tests := []struct {
		name  string
		setup func()
	}{
		{name: "never set", setup: func() {}},
		{name: "cleared with nil", setup: func() {
			SetPluginPoolStatsFunc(func() PluginPoolStats { return PluginPoolStats{Active: 1} })
			SetPluginPoolStatsFunc(nil)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := newPluginPoolReader(t)
			tt.setup()

			m, ok := findMetric(t, reader, testPoolBinaryName+"_plugin_pool_entries")
			if !ok {
				return // no observations: the SDK omits the metric entirely
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok, "unexpected data type %T", m.Data)
			assert.Empty(t, gauge.DataPoints)
		})
	}
}

func TestRecordPluginPoolEviction(t *testing.T) {
	reader := newPluginPoolReader(t)
	ctx := context.Background()

	RecordPluginPoolEviction(ctx, EvictReasonIdle)
	RecordPluginPoolEviction(ctx, EvictReasonIdle)
	RecordPluginPoolEviction(ctx, EvictReasonDead)

	m, ok := findMetric(t, reader, testPoolBinaryName+"_plugin_pool_evictions_total")
	require.True(t, ok, "counter not reported")
	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "unexpected data type %T", m.Data)
	assert.True(t, sum.IsMonotonic)

	assert.Equal(t, map[string]int64{
		EvictReasonIdle: 2,
		EvictReasonDead: 1,
	}, pointsByAttr(t, sum.DataPoints, AttrEvictReason))
}

func TestRecordPluginPoolEviction_NilInstrument(t *testing.T) {
	prev := pluginPoolEvictionsTotal
	pluginPoolEvictionsTotal = nil
	t.Cleanup(func() { pluginPoolEvictionsTotal = prev })

	assert.NotPanics(t, func() {
		RecordPluginPoolEviction(context.Background(), EvictReasonIdle)
	})
}

func BenchmarkRecordPluginPoolEviction(b *testing.B) {
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
	prev := pluginPoolEvictionsTotal
	b.Cleanup(func() {
		pluginPoolEvictionsTotal = prev
		_ = mp.Shutdown(context.Background())
	})
	require.NoError(b, registerPluginPoolInstruments(mp.Meter("bench"), testPoolBinaryName))

	ctx := context.Background()
	for b.Loop() {
		RecordPluginPoolEviction(ctx, EvictReasonIdle)
	}
}
