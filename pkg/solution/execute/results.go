// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package execute

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/resolver"
)

// ---------------------------------------------------------------------------
// Graph rendering
// ---------------------------------------------------------------------------

// GraphRenderer defines the interface for types that can render as ASCII, DOT, and Mermaid.
type GraphRenderer interface {
	RenderASCII(w io.Writer) error
	RenderDOT(w io.Writer) error
	RenderMermaid(w io.Writer) error
}

// RenderGraph renders a graph in the specified format using the GraphRenderer interface.
func RenderGraph(w io.Writer, graph GraphRenderer, data any, format string) error {
	switch format {
	case "ascii":
		return graph.RenderASCII(w)
	case "dot":
		return graph.RenderDOT(w)
	case "mermaid":
		return graph.RenderMermaid(w)
	case "json":
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(data)
	default:
		return fmt.Errorf("unsupported graph format: %s (supported: ascii, dot, mermaid, json)", format)
	}
}

// ---------------------------------------------------------------------------
// Resolver helpers
// ---------------------------------------------------------------------------

// ResolverProviderName extracts the primary provider name from a resolver.
func ResolverProviderName(r *resolver.Resolver) string {
	if r.Resolve != nil && len(r.Resolve.With) > 0 {
		return r.Resolve.With[0].Provider
	}
	return "unknown"
}

// FilterResolversWithDependencies returns the specified resolvers and all their dependencies.
// When targetNames is empty, all resolvers are returned.
// Uses resolver.ExtractDependencies to detect dependencies from CEL expressions,
// Go templates, explicit rslvr: references, and provider-specific extraction.
func FilterResolversWithDependencies(resolvers []*resolver.Resolver, targetNames []string, lookup resolver.DescriptorLookup) []*resolver.Resolver {
	if len(targetNames) == 0 {
		return resolvers
	}

	// Build a map of resolvers by name
	resolverMap := make(map[string]*resolver.Resolver)
	for _, r := range resolvers {
		resolverMap[r.Name] = r
	}

	// Collect all dependencies recursively
	needed := make(map[string]bool)
	var collectDeps func(name string)
	collectDeps = func(name string) {
		if needed[name] {
			return
		}
		needed[name] = true

		r, exists := resolverMap[name]
		if !exists {
			return
		}

		deps := resolver.ExtractDependencies(r, lookup)
		for _, dep := range deps {
			collectDeps(dep)
		}
	}

	for _, name := range targetNames {
		if _, exists := resolverMap[name]; exists {
			collectDeps(name)
		}
	}

	// Filter resolvers to only those needed
	var result []*resolver.Resolver
	for _, r := range resolvers {
		if needed[r.Name] {
			result = append(result, r)
		}
	}

	return result
}

// ---------------------------------------------------------------------------
// Execution metadata builders
// ---------------------------------------------------------------------------

// BuildExecutionData constructs structured execution metadata from resolver results.
// The returned map is extensible — new top-level sections can be added without
// breaking consumers (e.g. "diagrams", "timeline", "warnings").
func BuildExecutionData(
	resolverCtx *resolver.Context,
	resolvers []*resolver.Resolver,
	totalElapsed time.Duration,
) map[string]any {
	allResults := resolverCtx.GetAllResults()

	// Build per-resolver execution metadata
	resolverMeta := make(map[string]any, len(resolvers))
	for _, r := range resolvers {
		deps := resolver.ExtractDependencies(r, nil)
		entry := map[string]any{
			"provider":     ResolverProviderName(r),
			"dependencies": deps,
		}

		if result, ok := allResults[r.Name]; ok {
			entry["phase"] = result.Phase
			entry["duration"] = result.TotalDuration.Round(time.Millisecond).String()
			entry["status"] = string(result.Status)
			entry["providerCallCount"] = result.ProviderCallCount
			entry["valueSizeBytes"] = result.ValueSizeBytes
			entry["dependencyCount"] = result.DependencyCount

			// Per-phase breakdown (resolve, transform, validate)
			if len(result.PhaseMetrics) > 0 {
				metrics := make([]map[string]any, 0, len(result.PhaseMetrics))
				for _, pm := range result.PhaseMetrics {
					metrics = append(metrics, map[string]any{
						"phase":    pm.Phase,
						"duration": pm.Duration.Round(time.Millisecond).String(),
					})
				}
				entry["phaseMetrics"] = metrics
			}

			// Include failed attempts for debugging
			if len(result.FailedAttempts) > 0 {
				attempts := make([]map[string]any, 0, len(result.FailedAttempts))
				for _, fa := range result.FailedAttempts {
					attempt := map[string]any{
						"provider":   fa.Provider,
						"phase":      fa.Phase,
						"duration":   fa.Duration.Round(time.Millisecond).String(),
						"sourceStep": fa.SourceStep,
					}
					if fa.Error != "" {
						attempt["error"] = fa.Error
					}
					if fa.OnError != "" {
						attempt["onError"] = fa.OnError
					}
					attempts = append(attempts, attempt)
				}
				entry["failedAttempts"] = attempts
			}
		} else {
			entry["phase"] = 0
			entry["duration"] = "0s"
			entry["status"] = "unknown"
		}

		resolverMeta[r.Name] = entry
	}

	// Build summary
	phaseCount := 0
	for _, result := range allResults {
		if result.Phase > phaseCount {
			phaseCount = result.Phase
		}
	}

	summary := map[string]any{
		"totalDuration": totalElapsed.Round(time.Millisecond).String(),
		"resolverCount": len(resolvers),
		"phaseCount":    phaseCount,
	}

	return map[string]any{
		"resolvers": resolverMeta,
		"summary":   summary,
	}
}

// BuildProviderSummary aggregates per-provider usage statistics from resolver execution.
func BuildProviderSummary(
	resolverCtx *resolver.Context,
	resolvers []*resolver.Resolver,
) map[string]any {
	allResults := resolverCtx.GetAllResults()

	type providerStats struct {
		count         int
		totalDuration time.Duration
		callCount     int
		successCount  int
		failedCount   int
	}

	stats := make(map[string]*providerStats)

	for _, r := range resolvers {
		provName := ResolverProviderName(r)
		ps, ok := stats[provName]
		if !ok {
			ps = &providerStats{}
			stats[provName] = ps
		}
		ps.count++

		if result, ok := allResults[r.Name]; ok {
			ps.totalDuration += result.TotalDuration
			ps.callCount += result.ProviderCallCount
			if result.Status == resolver.ExecutionStatusSuccess {
				ps.successCount++
			} else {
				ps.failedCount++
			}
		}
	}

	summary := make(map[string]any, len(stats))
	for name, ps := range stats {
		entry := map[string]any{
			"resolverCount": ps.count,
			"totalDuration": ps.totalDuration.Round(time.Millisecond).String(),
			"callCount":     ps.callCount,
			"successCount":  ps.successCount,
			"failedCount":   ps.failedCount,
		}
		if ps.count > 0 {
			entry["avgDuration"] = (ps.totalDuration / time.Duration(ps.count)).Round(time.Millisecond).String()
		}
		summary[name] = entry
	}

	return summary
}

// CalculateValueSize estimates the size of a value in bytes by JSON-marshalling it.
func CalculateValueSize(value any) int64 {
	data, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return int64(len(data))
}
