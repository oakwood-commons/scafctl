// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// MetricsSummary contains aggregated metrics for display
type MetricsSummary struct {
	TotalResolvers   int           `json:"totalResolvers" doc:"Total number of resolvers executed"`
	SuccessCount     int           `json:"successCount" doc:"Number of successful resolvers"`
	FailedCount      int           `json:"failedCount" doc:"Number of failed resolvers"`
	SkippedCount     int           `json:"skippedCount" doc:"Number of skipped resolvers"`
	TotalDuration    time.Duration `json:"totalDuration" doc:"Total execution time"`
	PhaseCount       int           `json:"phaseCount" doc:"Number of execution phases"`
	SlowestResolvers []ResolverMetric
	LargestValues    []ResolverMetric
	FailedAttempts   []FailedAttemptSummary
	Failures         []FailureSummary
}

// ResolverMetric contains metrics for a single resolver
// nolint:revive // ResolverMetric name is intentional for clarity in metrics context
type ResolverMetric struct {
	Name     string        `json:"name" doc:"Resolver name"`
	Duration time.Duration `json:"duration,omitempty" doc:"Execution duration"`
	Size     int64         `json:"size,omitempty" doc:"Value size in bytes"`
	Phase    int           `json:"phase" doc:"Execution phase number"`
}

// FailedAttemptSummary summarizes failed attempts for a resolver
type FailedAttemptSummary struct {
	ResolverName string          `json:"resolverName" doc:"Resolver name"`
	AttemptCount int             `json:"attemptCount" doc:"Number of failed attempts"`
	Attempts     []AttemptDetail `json:"attempts" doc:"Details of each failed attempt"`
}

// AttemptDetail contains details of a failed attempt
type AttemptDetail struct {
	Provider string `json:"provider" doc:"Provider name"`
	Error    string `json:"error" doc:"Error message"`
}

// FailureSummary contains information about a failed resolver
type FailureSummary struct {
	Name  string `json:"name" doc:"Resolver name"`
	Phase int    `json:"phase" doc:"Phase where failure occurred"`
	Error string `json:"error" doc:"Error message"`
}

// BuildMetricsSummary creates a metrics summary from execution results
func BuildMetricsSummary(results map[string]*ExecutionResult, phaseCount int) *MetricsSummary {
	summary := &MetricsSummary{
		TotalResolvers: len(results),
		PhaseCount:     phaseCount,
	}

	var totalDuration time.Duration
	var slowest []ResolverMetric
	var largest []ResolverMetric
	var failedAttempts []FailedAttemptSummary
	var failures []FailureSummary

	for name, result := range results {
		// Count by status
		switch result.Status {
		case ExecutionStatusSuccess:
			summary.SuccessCount++
		case ExecutionStatusFailed:
			summary.FailedCount++
		case ExecutionStatusSkipped:
			summary.SkippedCount++
		}

		// Track total duration (use max to get overall execution time)
		if result.TotalDuration > totalDuration {
			totalDuration = result.TotalDuration
		}

		// Collect slowest resolvers
		if result.Status != ExecutionStatusSkipped {
			slowest = append(slowest, ResolverMetric{
				Name:     name,
				Duration: result.TotalDuration,
				Phase:    result.Phase,
			})
		}

		// Collect largest values
		if result.ValueSizeBytes > 0 {
			largest = append(largest, ResolverMetric{
				Name:  name,
				Size:  result.ValueSizeBytes,
				Phase: result.Phase,
			})
		}

		// Collect failed attempts (onError: continue)
		if len(result.FailedAttempts) > 0 {
			fas := FailedAttemptSummary{
				ResolverName: name,
				AttemptCount: len(result.FailedAttempts),
			}
			for _, attempt := range result.FailedAttempts {
				fas.Attempts = append(fas.Attempts, AttemptDetail{
					Provider: attempt.Provider,
					Error:    attempt.Error,
				})
			}
			failedAttempts = append(failedAttempts, fas)
		}

		// Collect failures
		if result.Status == ExecutionStatusFailed && result.Error != nil {
			failures = append(failures, FailureSummary{
				Name:  name,
				Phase: result.Phase,
				Error: result.Error.Error(),
			})
		}
	}

	summary.TotalDuration = totalDuration

	// Sort slowest by duration (descending) and take top 5
	sort.Slice(slowest, func(i, j int) bool {
		return slowest[i].Duration > slowest[j].Duration
	})
	if len(slowest) > 5 {
		slowest = slowest[:5]
	}
	summary.SlowestResolvers = slowest

	// Sort largest by size (descending) and take top 5
	sort.Slice(largest, func(i, j int) bool {
		return largest[i].Size > largest[j].Size
	})
	if len(largest) > 5 {
		largest = largest[:5]
	}
	summary.LargestValues = largest

	summary.FailedAttempts = failedAttempts
	summary.Failures = failures

	return summary
}

// FormatMetricsSummary formats the metrics summary for human-readable display
func FormatMetricsSummary(summary *MetricsSummary) string {
	var sb strings.Builder

	// Header
	sb.WriteString("Resolver Execution Summary\n")
	sb.WriteString("==========================\n")

	// Overview
	sb.WriteString(fmt.Sprintf("Total: %d | Success: %d | Failed: %d | Skipped: %d\n",
		summary.TotalResolvers, summary.SuccessCount, summary.FailedCount, summary.SkippedCount))
	sb.WriteString(fmt.Sprintf("Duration: %v | Phases: %d\n\n",
		summary.TotalDuration.Round(time.Millisecond), summary.PhaseCount))

	// Slowest resolvers
	if len(summary.SlowestResolvers) > 0 {
		sb.WriteString("Slowest Resolvers:\n")
		for i, r := range summary.SlowestResolvers {
			sb.WriteString(fmt.Sprintf("  %d. %-20s %6s  (phase %d)\n",
				i+1, r.Name, formatDuration(r.Duration), r.Phase))
		}
		sb.WriteString("\n")
	}

	// Largest values
	if len(summary.LargestValues) > 0 {
		sb.WriteString("Largest Values:\n")
		for i, r := range summary.LargestValues {
			sb.WriteString(fmt.Sprintf("  %d. %-20s %s\n",
				i+1, r.Name, formatBytes(r.Size)))
		}
		sb.WriteString("\n")
	}

	// Failed attempts (onError: continue)
	if len(summary.FailedAttempts) > 0 {
		sb.WriteString("Failed Attempts (onError: continue):\n")
		for _, fa := range summary.FailedAttempts {
			sb.WriteString(fmt.Sprintf("  %s: %d attempts before success\n",
				fa.ResolverName, fa.AttemptCount))
			for _, attempt := range fa.Attempts {
				sb.WriteString(fmt.Sprintf("    ✗ %s (%s)\n",
					attempt.Provider, attempt.Error))
			}
		}
		sb.WriteString("\n")
	}

	// Failures
	if len(summary.Failures) > 0 {
		sb.WriteString("Failures:\n")
		for _, f := range summary.Failures {
			sb.WriteString(fmt.Sprintf("  ✗ %s (phase %d)\n",
				f.Name, f.Phase))
			sb.WriteString(fmt.Sprintf("    Error: %s\n", f.Error))
		}
	}

	return sb.String()
}

// formatDuration formats duration in human-readable format
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// formatBytes formats bytes in human-readable format
func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
