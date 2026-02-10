// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMetricsSummary(t *testing.T) {
	tests := []struct {
		name       string
		results    map[string]*ExecutionResult
		phaseCount int
		want       *MetricsSummary
	}{
		{
			name:       "empty results",
			results:    map[string]*ExecutionResult{},
			phaseCount: 0,
			want: &MetricsSummary{
				TotalResolvers:   0,
				SuccessCount:     0,
				FailedCount:      0,
				SkippedCount:     0,
				TotalDuration:    0,
				PhaseCount:       0,
				SlowestResolvers: nil,
				LargestValues:    nil,
				FailedAttempts:   nil,
				Failures:         nil,
			},
		},
		{
			name: "single successful resolver",
			results: map[string]*ExecutionResult{
				"test": {
					Value:          "value1",
					Status:         ExecutionStatusSuccess,
					Phase:          1,
					TotalDuration:  100 * time.Millisecond,
					ValueSizeBytes: 1024,
				},
			},
			phaseCount: 1,
			want: &MetricsSummary{
				TotalResolvers: 1,
				SuccessCount:   1,
				FailedCount:    0,
				SkippedCount:   0,
				TotalDuration:  100 * time.Millisecond,
				PhaseCount:     1,
				SlowestResolvers: []ResolverMetric{
					{Name: "test", Duration: 100 * time.Millisecond, Phase: 1},
				},
				LargestValues: []ResolverMetric{
					{Name: "test", Size: 1024, Phase: 1},
				},
				FailedAttempts: nil,
				Failures:       nil,
			},
		},
		{
			name: "mixed statuses",
			results: map[string]*ExecutionResult{
				"success1": {
					Value:          "value1",
					Status:         ExecutionStatusSuccess,
					Phase:          1,
					TotalDuration:  100 * time.Millisecond,
					ValueSizeBytes: 1024,
				},
				"success2": {
					Value:          "value2",
					Status:         ExecutionStatusSuccess,
					Phase:          2,
					TotalDuration:  200 * time.Millisecond,
					ValueSizeBytes: 2048,
				},
				"failed1": {
					Value:         nil,
					Status:        ExecutionStatusFailed,
					Phase:         2,
					TotalDuration: 50 * time.Millisecond,
					Error:         errors.New("test error"),
				},
				"skipped1": {
					Value:         nil,
					Status:        ExecutionStatusSkipped,
					Phase:         1,
					TotalDuration: 0,
				},
			},
			phaseCount: 2,
			want: &MetricsSummary{
				TotalResolvers: 4,
				SuccessCount:   2,
				FailedCount:    1,
				SkippedCount:   1,
				TotalDuration:  200 * time.Millisecond,
				PhaseCount:     2,
				SlowestResolvers: []ResolverMetric{
					{Name: "success2", Duration: 200 * time.Millisecond, Phase: 2},
					{Name: "success1", Duration: 100 * time.Millisecond, Phase: 1},
					{Name: "failed1", Duration: 50 * time.Millisecond, Phase: 2},
				},
				LargestValues: []ResolverMetric{
					{Name: "success2", Size: 2048, Phase: 2},
					{Name: "success1", Size: 1024, Phase: 1},
				},
				FailedAttempts: nil,
				Failures: []FailureSummary{
					{Name: "failed1", Phase: 2, Error: "test error"},
				},
			},
		},
		{
			name: "resolver with failed attempts",
			results: map[string]*ExecutionResult{
				"withRetries": {
					Value:          "final value",
					Status:         ExecutionStatusSuccess,
					Phase:          1,
					TotalDuration:  500 * time.Millisecond,
					ValueSizeBytes: 512,
					FailedAttempts: []ProviderAttempt{
						{
							Provider: "http-primary",
							Error:    "timeout",
							Phase:    "resolve",
						},
						{
							Provider: "http-backup",
							Error:    "connection refused",
							Phase:    "resolve",
						},
					},
				},
			},
			phaseCount: 1,
			want: &MetricsSummary{
				TotalResolvers: 1,
				SuccessCount:   1,
				FailedCount:    0,
				SkippedCount:   0,
				TotalDuration:  500 * time.Millisecond,
				PhaseCount:     1,
				SlowestResolvers: []ResolverMetric{
					{Name: "withRetries", Duration: 500 * time.Millisecond, Phase: 1},
				},
				LargestValues: []ResolverMetric{
					{Name: "withRetries", Size: 512, Phase: 1},
				},
				FailedAttempts: []FailedAttemptSummary{
					{
						ResolverName: "withRetries",
						AttemptCount: 2,
						Attempts: []AttemptDetail{
							{Provider: "http-primary", Error: "timeout"},
							{Provider: "http-backup", Error: "connection refused"},
						},
					},
				},
				Failures: nil,
			},
		},
		{
			name: "more than 5 resolvers for top lists",
			results: map[string]*ExecutionResult{
				"r1": {Status: ExecutionStatusSuccess, Phase: 1, TotalDuration: 100 * time.Millisecond, ValueSizeBytes: 1000},
				"r2": {Status: ExecutionStatusSuccess, Phase: 1, TotalDuration: 200 * time.Millisecond, ValueSizeBytes: 2000},
				"r3": {Status: ExecutionStatusSuccess, Phase: 1, TotalDuration: 300 * time.Millisecond, ValueSizeBytes: 3000},
				"r4": {Status: ExecutionStatusSuccess, Phase: 1, TotalDuration: 400 * time.Millisecond, ValueSizeBytes: 4000},
				"r5": {Status: ExecutionStatusSuccess, Phase: 1, TotalDuration: 500 * time.Millisecond, ValueSizeBytes: 5000},
				"r6": {Status: ExecutionStatusSuccess, Phase: 1, TotalDuration: 600 * time.Millisecond, ValueSizeBytes: 6000},
				"r7": {Status: ExecutionStatusSuccess, Phase: 1, TotalDuration: 700 * time.Millisecond, ValueSizeBytes: 7000},
			},
			phaseCount: 1,
			want: &MetricsSummary{
				TotalResolvers: 7,
				SuccessCount:   7,
				FailedCount:    0,
				SkippedCount:   0,
				TotalDuration:  700 * time.Millisecond,
				PhaseCount:     1,
				SlowestResolvers: []ResolverMetric{
					{Name: "r7", Duration: 700 * time.Millisecond, Phase: 1},
					{Name: "r6", Duration: 600 * time.Millisecond, Phase: 1},
					{Name: "r5", Duration: 500 * time.Millisecond, Phase: 1},
					{Name: "r4", Duration: 400 * time.Millisecond, Phase: 1},
					{Name: "r3", Duration: 300 * time.Millisecond, Phase: 1},
				},
				LargestValues: []ResolverMetric{
					{Name: "r7", Size: 7000, Phase: 1},
					{Name: "r6", Size: 6000, Phase: 1},
					{Name: "r5", Size: 5000, Phase: 1},
					{Name: "r4", Size: 4000, Phase: 1},
					{Name: "r3", Size: 3000, Phase: 1},
				},
				FailedAttempts: nil,
				Failures:       nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildMetricsSummary(tt.results, tt.phaseCount)

			assert.Equal(t, tt.want.TotalResolvers, got.TotalResolvers)
			assert.Equal(t, tt.want.SuccessCount, got.SuccessCount)
			assert.Equal(t, tt.want.FailedCount, got.FailedCount)
			assert.Equal(t, tt.want.SkippedCount, got.SkippedCount)
			assert.Equal(t, tt.want.TotalDuration, got.TotalDuration)
			assert.Equal(t, tt.want.PhaseCount, got.PhaseCount)

			// Check slowest resolvers (order matters)
			require.Equal(t, len(tt.want.SlowestResolvers), len(got.SlowestResolvers))
			for i := range tt.want.SlowestResolvers {
				assert.Equal(t, tt.want.SlowestResolvers[i].Name, got.SlowestResolvers[i].Name)
				assert.Equal(t, tt.want.SlowestResolvers[i].Duration, got.SlowestResolvers[i].Duration)
				assert.Equal(t, tt.want.SlowestResolvers[i].Phase, got.SlowestResolvers[i].Phase)
			}

			// Check largest values (order matters)
			require.Equal(t, len(tt.want.LargestValues), len(got.LargestValues))
			for i := range tt.want.LargestValues {
				assert.Equal(t, tt.want.LargestValues[i].Name, got.LargestValues[i].Name)
				assert.Equal(t, tt.want.LargestValues[i].Size, got.LargestValues[i].Size)
				assert.Equal(t, tt.want.LargestValues[i].Phase, got.LargestValues[i].Phase)
			}

			// Check failed attempts
			assert.Equal(t, len(tt.want.FailedAttempts), len(got.FailedAttempts))

			// Check failures
			assert.Equal(t, len(tt.want.Failures), len(got.Failures))
		})
	}
}

func TestFormatMetricsSummary(t *testing.T) {
	tests := []struct {
		name     string
		summary  *MetricsSummary
		contains []string
	}{
		{
			name: "basic summary",
			summary: &MetricsSummary{
				TotalResolvers: 10,
				SuccessCount:   8,
				FailedCount:    1,
				SkippedCount:   1,
				TotalDuration:  1234 * time.Millisecond,
				PhaseCount:     3,
			},
			contains: []string{
				"Resolver Execution Summary",
				"Total: 10",
				"Success: 8",
				"Failed: 1",
				"Skipped: 1",
				"Duration:",
				"Phases: 3",
			},
		},
		{
			name: "with slowest resolvers",
			summary: &MetricsSummary{
				TotalResolvers: 3,
				SuccessCount:   3,
				TotalDuration:  1000 * time.Millisecond,
				PhaseCount:     2,
				SlowestResolvers: []ResolverMetric{
					{Name: "slow1", Duration: 500 * time.Millisecond, Phase: 1},
					{Name: "slow2", Duration: 300 * time.Millisecond, Phase: 2},
				},
			},
			contains: []string{
				"Slowest Resolvers:",
				"1. slow1",
				"500ms",
				"phase 1",
				"2. slow2",
				"300ms",
				"phase 2",
			},
		},
		{
			name: "with largest values",
			summary: &MetricsSummary{
				TotalResolvers: 2,
				SuccessCount:   2,
				TotalDuration:  100 * time.Millisecond,
				PhaseCount:     1,
				LargestValues: []ResolverMetric{
					{Name: "large1", Size: 2097152, Phase: 1}, // 2 MB
					{Name: "large2", Size: 10240, Phase: 1},   // 10 KB
				},
			},
			contains: []string{
				"Largest Values:",
				"1. large1",
				"2.00 MB",
				"2. large2",
				"10.00 KB",
			},
		},
		{
			name: "with failed attempts",
			summary: &MetricsSummary{
				TotalResolvers: 1,
				SuccessCount:   1,
				TotalDuration:  500 * time.Millisecond,
				PhaseCount:     1,
				FailedAttempts: []FailedAttemptSummary{
					{
						ResolverName: "apiUrl",
						AttemptCount: 2,
						Attempts: []AttemptDetail{
							{Provider: "primary-api", Error: "timeout"},
							{Provider: "backup-api", Error: "connection refused"},
						},
					},
				},
			},
			contains: []string{
				"Failed Attempts (onError: continue):",
				"apiUrl: 2 attempts before success",
				"✗ primary-api (timeout)",
				"✗ backup-api (connection refused)",
			},
		},
		{
			name: "with failures",
			summary: &MetricsSummary{
				TotalResolvers: 2,
				SuccessCount:   1,
				FailedCount:    1,
				TotalDuration:  200 * time.Millisecond,
				PhaseCount:     2,
				Failures: []FailureSummary{
					{Name: "dbConnection", Phase: 2, Error: "connection timeout after 30s"},
				},
			},
			contains: []string{
				"Failures:",
				"✗ dbConnection (phase 2)",
				"Error: connection timeout after 30s",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := FormatMetricsSummary(tt.summary)

			for _, substr := range tt.contains {
				assert.Contains(t, output, substr, "output should contain: %s", substr)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		want     string
	}{
		{500 * time.Nanosecond, "0µs"},
		{500 * time.Microsecond, "500µs"},
		{1 * time.Millisecond, "1ms"},
		{100 * time.Millisecond, "100ms"},
		{1 * time.Second, "1.00s"},
		{1234 * time.Millisecond, "1.23s"},
		{65 * time.Second, "65.00s"},
	}

	for _, tt := range tests {
		t.Run(tt.duration.String(), func(t *testing.T) {
			got := formatDuration(tt.duration)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1023, "1023 B"},
		{1024, "1.00 KB"},
		{10240, "10.00 KB"},
		{1048576, "1.00 MB"},
		{2097152, "2.00 MB"},
		{1073741824, "1.00 GB"},
		{2147483648, "2.00 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatBytes(tt.bytes)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatMetricsSummary_CompleteExample(t *testing.T) {
	// Create a comprehensive summary
	summary := &MetricsSummary{
		TotalResolvers: 15,
		SuccessCount:   13,
		FailedCount:    1,
		SkippedCount:   1,
		TotalDuration:  1234 * time.Millisecond,
		PhaseCount:     3,
		SlowestResolvers: []ResolverMetric{
			{Name: "externalApiData", Duration: 850 * time.Millisecond, Phase: 2},
			{Name: "complexTransform", Duration: 320 * time.Millisecond, Phase: 3},
			{Name: "validation", Duration: 180 * time.Millisecond, Phase: 3},
		},
		LargestValues: []ResolverMetric{
			{Name: "fullConfig", Size: 2097152, Phase: 2}, // 2 MB
			{Name: "apiResponse", Size: 856064, Phase: 2}, // ~856 KB
			{Name: "processedData", Size: 234567, Phase: 3},
		},
		FailedAttempts: []FailedAttemptSummary{
			{
				ResolverName: "apiUrl",
				AttemptCount: 2,
				Attempts: []AttemptDetail{
					{Provider: "primary-api", Error: "timeout"},
					{Provider: "backup-api", Error: "connection refused"},
				},
			},
		},
		Failures: []FailureSummary{
			{Name: "databaseConnection", Phase: 2, Error: "connection timeout after 30s"},
		},
	}

	output := FormatMetricsSummary(summary)

	// Verify structure
	assert.True(t, strings.HasPrefix(output, "Resolver Execution Summary\n==========================\n"))

	// Verify all sections are present
	assert.Contains(t, output, "Total: 15 | Success: 13 | Failed: 1 | Skipped: 1")
	assert.Contains(t, output, "Slowest Resolvers:")
	assert.Contains(t, output, "Largest Values:")
	assert.Contains(t, output, "Failed Attempts (onError: continue):")
	assert.Contains(t, output, "Failures:")

	// Print for manual verification
	t.Logf("\n%s", output)
}
