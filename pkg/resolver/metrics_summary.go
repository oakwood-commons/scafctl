// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
)

// MetricsSummary contains aggregated metrics for display
type MetricsSummary struct {
	TotalResolvers   int                    `json:"totalResolvers" yaml:"totalResolvers" doc:"Total number of resolvers executed" maximum:"10000" example:"20"`
	SuccessCount     int                    `json:"successCount" yaml:"successCount" doc:"Number of successful resolvers" maximum:"10000" example:"18"`
	FailedCount      int                    `json:"failedCount" yaml:"failedCount" doc:"Number of failed resolvers" maximum:"10000" example:"1"`
	SkippedCount     int                    `json:"skippedCount" yaml:"skippedCount" doc:"Number of skipped resolvers" maximum:"10000" example:"1"`
	TotalDuration    time.Duration          `json:"totalDuration" yaml:"totalDuration" doc:"Total execution time"`
	PhaseCount       int                    `json:"phaseCount" yaml:"phaseCount" doc:"Number of execution phases" maximum:"100" example:"3"`
	SlowestResolvers []ResolverMetric       `json:"slowestResolvers,omitempty" yaml:"slowestResolvers,omitempty" doc:"Top resolvers by execution time" maxItems:"20"`
	LargestValues    []ResolverMetric       `json:"largestValues,omitempty" yaml:"largestValues,omitempty" doc:"Top resolvers by value size" maxItems:"20"`
	FailedAttempts   []FailedAttemptSummary `json:"failedAttempts,omitempty" yaml:"failedAttempts,omitempty" doc:"Resolvers with failed provider attempts" maxItems:"100"`
	Failures         []FailureSummary       `json:"failures,omitempty" yaml:"failures,omitempty" doc:"Failed resolver details" maxItems:"100"`
}

// ResolverMetric contains metrics for a single resolver
// nolint:revive // ResolverMetric name is intentional for clarity in metrics context
type ResolverMetric struct {
	Name     string        `json:"name" yaml:"name" doc:"Resolver name" maxLength:"256" example:"api-data"`
	Duration time.Duration `json:"duration,omitempty" yaml:"duration,omitempty" doc:"Execution duration"`
	Size     int64         `json:"size,omitempty" yaml:"size,omitempty" doc:"Value size in bytes" maximum:"1073741824" example:"1024"`
	Phase    int           `json:"phase" yaml:"phase" doc:"Execution phase number" maximum:"100" example:"1"`
}

// FailedAttemptSummary summarizes failed attempts for a resolver
type FailedAttemptSummary struct {
	ResolverName string          `json:"resolverName" yaml:"resolverName" doc:"Resolver name" maxLength:"256" example:"api-data"`
	AttemptCount int             `json:"attemptCount" yaml:"attemptCount" doc:"Number of failed attempts" maximum:"100" example:"2"`
	Attempts     []AttemptDetail `json:"attempts" yaml:"attempts" doc:"Details of each failed attempt" maxItems:"100"`
}

// AttemptDetail contains details of a failed attempt
type AttemptDetail struct {
	Provider string `json:"provider" yaml:"provider" doc:"Provider name" maxLength:"128" example:"http"`
	Error    string `json:"error" yaml:"error" doc:"Error message" maxLength:"4096" example:"connection refused"`
}

// FailureSummary contains information about a failed resolver
type FailureSummary struct {
	Name  string `json:"name" yaml:"name" doc:"Resolver name" maxLength:"256" example:"api-data"`
	Phase int    `json:"phase" yaml:"phase" doc:"Phase where failure occurred" maximum:"100" example:"1"`
	Error string `json:"error" yaml:"error" doc:"Error message" maxLength:"4096" example:"timeout"`
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
	fmt.Fprintf(&sb, "Total: %d | Success: %d | Failed: %d | Skipped: %d\n",
		summary.TotalResolvers, summary.SuccessCount, summary.FailedCount, summary.SkippedCount)
	fmt.Fprintf(&sb, "Duration: %v | Phases: %d\n\n",
		summary.TotalDuration.Round(time.Millisecond), summary.PhaseCount)

	// Slowest resolvers
	if len(summary.SlowestResolvers) > 0 {
		sb.WriteString("Slowest Resolvers:\n")
		for i, r := range summary.SlowestResolvers {
			fmt.Fprintf(&sb, "  %d. %-20s %6s  (phase %d)\n",
				i+1, r.Name, format.Duration(r.Duration), r.Phase)
		}
		sb.WriteString("\n")
	}

	// Largest values
	if len(summary.LargestValues) > 0 {
		sb.WriteString("Largest Values:\n")
		for i, r := range summary.LargestValues {
			fmt.Fprintf(&sb, "  %d. %-20s %s\n",
				i+1, r.Name, format.Bytes(r.Size))
		}
		sb.WriteString("\n")
	}

	// Failed attempts (onError: continue)
	if len(summary.FailedAttempts) > 0 {
		sb.WriteString("Failed Attempts (onError: continue):\n")
		for _, fa := range summary.FailedAttempts {
			fmt.Fprintf(&sb, "  %s: %d attempts before success\n",
				fa.ResolverName, fa.AttemptCount)
			for _, attempt := range fa.Attempts {
				fmt.Fprintf(&sb, "    ✗ %s (%s)\n",
					attempt.Provider, attempt.Error)
			}
		}
		sb.WriteString("\n")
	}

	// Failures
	if len(summary.Failures) > 0 {
		sb.WriteString("Failures:\n")
		for _, f := range summary.Failures {
			fmt.Fprintf(&sb, "  ✗ %s (phase %d)\n",
				f.Name, f.Phase)
			fmt.Fprintf(&sb, "    Error: %s\n", f.Error)
		}
	}

	return sb.String()
}
