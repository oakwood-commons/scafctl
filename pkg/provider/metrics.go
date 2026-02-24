// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/metrics"
)

// ExecutionMetrics tracks execution statistics for a provider.
type ExecutionMetrics struct {
	ExecutionCount  uint64 `json:"executionCount" yaml:"executionCount" doc:"Total number of executions"`
	SuccessCount    uint64 `json:"successCount" yaml:"successCount" doc:"Number of successful executions"`
	FailureCount    uint64 `json:"failureCount" yaml:"failureCount" doc:"Number of failed executions"`
	TotalDurationNs uint64 `json:"totalDurationNs" yaml:"totalDurationNs" doc:"Total execution duration in nanoseconds"`
	LastExecutionNs uint64 `json:"lastExecutionNs" yaml:"lastExecutionNs" doc:"Timestamp of last execution in nanoseconds since epoch"`
}

// AverageDuration returns the average execution duration.
func (m *ExecutionMetrics) AverageDuration() time.Duration {
	count := atomic.LoadUint64(&m.ExecutionCount)
	if count == 0 {
		return 0
	}
	totalNs := atomic.LoadUint64(&m.TotalDurationNs)
	//nolint:gosec // Integer overflow is not a concern for duration averaging
	return time.Duration(totalNs / count)
}

// SuccessRate returns the success rate as a percentage (0-100).
func (m *ExecutionMetrics) SuccessRate() float64 {
	count := atomic.LoadUint64(&m.ExecutionCount)
	if count == 0 {
		return 0
	}
	success := atomic.LoadUint64(&m.SuccessCount)
	return float64(success) / float64(count) * 100
}

// Metrics provides global provider metrics collection.
// It is safe for concurrent use.
type Metrics struct {
	enabled   bool
	mu        sync.RWMutex
	providers sync.Map // map[string]*ExecutionMetrics
}

// GlobalMetrics is the default metrics collector.
// Metrics collection is disabled by default; call Enable() to turn it on.
var GlobalMetrics = &Metrics{enabled: false}

// Enable turns on metrics collection.
func (m *Metrics) Enable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = true
}

// Disable turns off metrics collection.
func (m *Metrics) Disable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = false
}

// IsEnabled returns whether metrics collection is enabled.
func (m *Metrics) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// Record records an execution result for a provider.
// When metrics collection is enabled, results are written to both the in-memory
// store and the global OTel MeterProvider.
func (m *Metrics) Record(ctx context.Context, providerName string, duration time.Duration, success bool) {
	if !m.IsEnabled() {
		return
	}

	// Record to OTel.
	metrics.RecordProviderExecution(ctx, providerName, duration.Seconds(), success)

	pm, _ := m.providers.LoadOrStore(providerName, &ExecutionMetrics{})
	metrics, ok := pm.(*ExecutionMetrics)
	if !ok {
		return // Should not happen, but be safe
	}

	atomic.AddUint64(&metrics.ExecutionCount, 1)
	//nolint:gosec // Duration nanoseconds will not overflow uint64 for reasonable durations
	atomic.AddUint64(&metrics.TotalDurationNs, uint64(duration.Nanoseconds()))
	//nolint:gosec // Unix timestamp will not overflow uint64
	atomic.StoreUint64(&metrics.LastExecutionNs, uint64(time.Now().UnixNano()))

	if success {
		atomic.AddUint64(&metrics.SuccessCount, 1)
	} else {
		atomic.AddUint64(&metrics.FailureCount, 1)
	}
}

// GetMetrics returns metrics for a specific provider.
// Returns nil if no metrics have been recorded for the provider.
func (m *Metrics) GetMetrics(providerName string) *ExecutionMetrics {
	if pm, ok := m.providers.Load(providerName); ok {
		if metrics, ok := pm.(*ExecutionMetrics); ok {
			return metrics
		}
	}
	return nil
}

// GetAllMetrics returns a map of all provider metrics.
// The returned map is a copy and safe to modify.
func (m *Metrics) GetAllMetrics() map[string]*ExecutionMetrics {
	result := make(map[string]*ExecutionMetrics)
	m.providers.Range(func(key, value any) bool {
		name, ok := key.(string)
		if !ok {
			return true
		}
		metrics, ok := value.(*ExecutionMetrics)
		if !ok {
			return true
		}
		// Return a copy to avoid race conditions
		result[name] = &ExecutionMetrics{
			ExecutionCount:  atomic.LoadUint64(&metrics.ExecutionCount),
			SuccessCount:    atomic.LoadUint64(&metrics.SuccessCount),
			FailureCount:    atomic.LoadUint64(&metrics.FailureCount),
			TotalDurationNs: atomic.LoadUint64(&metrics.TotalDurationNs),
			LastExecutionNs: atomic.LoadUint64(&metrics.LastExecutionNs),
		}
		return true
	})
	return result
}

// Reset clears all collected metrics.
func (m *Metrics) Reset() {
	m.providers.Range(func(key, _ any) bool {
		m.providers.Delete(key)
		return true
	})
}
