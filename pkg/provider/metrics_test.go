// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetrics_EnableDisable(t *testing.T) {
	m := &Metrics{enabled: false}

	assert.False(t, m.IsEnabled())

	m.Enable()
	assert.True(t, m.IsEnabled())

	m.Disable()
	assert.False(t, m.IsEnabled())
}

func TestMetrics_Record(t *testing.T) {
	m := &Metrics{enabled: true}

	// Record a successful execution
	m.Record("test-provider", 100*time.Millisecond, true)

	metrics := m.GetMetrics("test-provider")
	require.NotNil(t, metrics)

	assert.Equal(t, uint64(1), metrics.ExecutionCount)
	assert.Equal(t, uint64(1), metrics.SuccessCount)
	assert.Equal(t, uint64(0), metrics.FailureCount)
	assert.Equal(t, uint64(100*time.Millisecond), metrics.TotalDurationNs)
	assert.NotZero(t, metrics.LastExecutionNs)
}

func TestMetrics_Record_Disabled(t *testing.T) {
	m := &Metrics{enabled: false}

	// Recording when disabled should be a no-op
	m.Record("test-provider", 100*time.Millisecond, true)

	metrics := m.GetMetrics("test-provider")
	assert.Nil(t, metrics)
}

func TestMetrics_Record_Failure(t *testing.T) {
	m := &Metrics{enabled: true}

	m.Record("test-provider", 50*time.Millisecond, false)

	metrics := m.GetMetrics("test-provider")
	require.NotNil(t, metrics)

	assert.Equal(t, uint64(1), metrics.ExecutionCount)
	assert.Equal(t, uint64(0), metrics.SuccessCount)
	assert.Equal(t, uint64(1), metrics.FailureCount)
}

func TestMetrics_Record_Multiple(t *testing.T) {
	m := &Metrics{enabled: true}

	m.Record("test-provider", 100*time.Millisecond, true)
	m.Record("test-provider", 200*time.Millisecond, true)
	m.Record("test-provider", 50*time.Millisecond, false)

	metrics := m.GetMetrics("test-provider")
	require.NotNil(t, metrics)

	assert.Equal(t, uint64(3), metrics.ExecutionCount)
	assert.Equal(t, uint64(2), metrics.SuccessCount)
	assert.Equal(t, uint64(1), metrics.FailureCount)
	assert.Equal(t, uint64(350*time.Millisecond), metrics.TotalDurationNs)
}

func TestExecutionMetrics_AverageDuration(t *testing.T) {
	m := &Metrics{enabled: true}

	m.Record("test-provider", 100*time.Millisecond, true)
	m.Record("test-provider", 200*time.Millisecond, true)
	m.Record("test-provider", 300*time.Millisecond, true)

	metrics := m.GetMetrics("test-provider")
	require.NotNil(t, metrics)

	avg := metrics.AverageDuration()
	assert.Equal(t, 200*time.Millisecond, avg)
}

func TestExecutionMetrics_AverageDuration_Empty(t *testing.T) {
	metrics := &ExecutionMetrics{}
	assert.Equal(t, time.Duration(0), metrics.AverageDuration())
}

func TestExecutionMetrics_SuccessRate(t *testing.T) {
	m := &Metrics{enabled: true}

	m.Record("test-provider", 100*time.Millisecond, true)
	m.Record("test-provider", 100*time.Millisecond, true)
	m.Record("test-provider", 100*time.Millisecond, false)
	m.Record("test-provider", 100*time.Millisecond, false)

	metrics := m.GetMetrics("test-provider")
	require.NotNil(t, metrics)

	rate := metrics.SuccessRate()
	assert.Equal(t, 50.0, rate)
}

func TestExecutionMetrics_SuccessRate_Empty(t *testing.T) {
	metrics := &ExecutionMetrics{}
	assert.Equal(t, 0.0, metrics.SuccessRate())
}

func TestMetrics_GetAllMetrics(t *testing.T) {
	m := &Metrics{enabled: true}

	m.Record("provider-a", 100*time.Millisecond, true)
	m.Record("provider-b", 200*time.Millisecond, false)

	all := m.GetAllMetrics()

	assert.Len(t, all, 2)
	assert.Contains(t, all, "provider-a")
	assert.Contains(t, all, "provider-b")

	assert.Equal(t, uint64(1), all["provider-a"].SuccessCount)
	assert.Equal(t, uint64(1), all["provider-b"].FailureCount)
}

func TestMetrics_Reset(t *testing.T) {
	m := &Metrics{enabled: true}

	m.Record("provider-a", 100*time.Millisecond, true)
	m.Record("provider-b", 200*time.Millisecond, true)

	assert.Len(t, m.GetAllMetrics(), 2)

	m.Reset()

	assert.Len(t, m.GetAllMetrics(), 0)
	assert.Nil(t, m.GetMetrics("provider-a"))
	assert.Nil(t, m.GetMetrics("provider-b"))
}

func TestMetrics_ConcurrentAccess(t *testing.T) {
	m := &Metrics{enabled: true}

	var wg sync.WaitGroup
	numGoroutines := 100
	numRecordsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numRecordsPerGoroutine; j++ {
				m.Record("concurrent-provider", time.Millisecond, true)
			}
		}()
	}

	wg.Wait()

	metrics := m.GetMetrics("concurrent-provider")
	require.NotNil(t, metrics)

	expectedCount := uint64(numGoroutines * numRecordsPerGoroutine)
	assert.Equal(t, expectedCount, metrics.ExecutionCount)
	assert.Equal(t, expectedCount, metrics.SuccessCount)
}

func TestMetrics_GetMetrics_NonExistent(t *testing.T) {
	m := &Metrics{enabled: true}
	assert.Nil(t, m.GetMetrics("non-existent-provider"))
}
