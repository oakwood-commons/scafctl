// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
)

// registerSnapshotTools registers snapshot inspection and diff tools.
func (s *Server) registerSnapshotTools() {
	showSnapshotTool := mcp.NewTool("show_snapshot",
		mcp.WithDescription("Load and display a resolver execution snapshot. Shows solution metadata, execution timing, status (success/failure), parameter values, and per-resolver results (value, status, duration, provider). Use this to inspect past execution results for debugging."),
		mcp.WithTitleAnnotation("Show Snapshot"),
		mcp.WithToolIcons(toolIcons["snapshot"]),
		mcp.WithDeferLoading(true),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to the snapshot JSON file"),
		),
		mcp.WithString("format",
			mcp.Description("Output detail level: 'summary' (default), 'resolvers' (include per-resolver data), 'full' (everything including raw values)"),
		),
	)
	s.mcpServer.AddTool(showSnapshotTool, s.handleShowSnapshot)

	diffSnapshotsTool := mcp.NewTool("diff_snapshots",
		mcp.WithDescription("Compare two resolver execution snapshots and show differences. Identifies resolvers with changed values, status changes (success→failure), additions, and removals. Useful for detecting regressions between runs or understanding the impact of solution changes."),
		mcp.WithTitleAnnotation("Diff Snapshots"),
		mcp.WithToolIcons(toolIcons["snapshot"]),
		mcp.WithDeferLoading(true),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("before",
			mcp.Required(),
			mcp.Description("Path to the baseline (before) snapshot file"),
		),
		mcp.WithString("after",
			mcp.Required(),
			mcp.Description("Path to the comparison (after) snapshot file"),
		),
		mcp.WithBoolean("ignore_unchanged",
			mcp.Description("Omit unchanged resolvers from the response. Default: true"),
		),
	)
	s.mcpServer.AddTool(diffSnapshotsTool, s.handleDiffSnapshots)
}

// handleShowSnapshot loads a snapshot file and returns structured data.
func (s *Server) handleShowSnapshot(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path"),
			WithSuggestion("Provide the path to a resolver execution snapshot file"),
		), nil
	}
	format := request.GetString("format", "summary")

	snapshot, err := resolver.LoadSnapshot(path)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to load snapshot %q: %v", path, err),
			WithField("path"),
			WithSuggestion("Check that the file exists and is a valid snapshot JSON file"),
		), nil
	}

	// Build resolver counts
	counts := map[string]int{
		"total":   len(snapshot.Resolvers),
		"success": 0,
		"failed":  0,
		"skipped": 0,
	}
	for _, r := range snapshot.Resolvers {
		if r == nil {
			continue
		}
		switch r.Status {
		case string(resolver.ExecutionStatusSuccess):
			counts["success"]++
		case string(resolver.ExecutionStatusFailed):
			counts["failed"]++
		case string(resolver.ExecutionStatusSkipped):
			counts["skipped"]++
		}
	}

	result := map[string]any{
		"solution":       snapshot.Metadata.Solution,
		"version":        snapshot.Metadata.Version,
		"timestamp":      snapshot.Metadata.Timestamp,
		"scafctlVersion": snapshot.Metadata.ScafctlVersion,
		"duration":       snapshot.Metadata.TotalDuration,
		"status":         snapshot.Metadata.Status,
		"resolverCount":  counts,
		"phases":         len(snapshot.Phases),
		"parameters":     snapshot.Parameters,
	}

	if format == "resolvers" || format == "full" {
		resolvers := make([]map[string]any, 0, len(snapshot.Resolvers))
		for name, r := range snapshot.Resolvers {
			if r == nil {
				continue
			}
			entry := map[string]any{
				"name":     name,
				"status":   r.Status,
				"duration": r.Duration,
				"phase":    r.Phase,
			}
			if format == "full" {
				entry["value"] = r.Value
				entry["error"] = r.Error
				entry["sensitive"] = r.Sensitive
				entry["providerCalls"] = r.ProviderCalls
				entry["valueSizeBytes"] = r.ValueSizeBytes
				if len(r.FailedAttempts) > 0 {
					entry["failedAttempts"] = r.FailedAttempts
				}
			}
			resolvers = append(resolvers, entry)
		}
		result["resolvers"] = resolvers
	}

	return mcp.NewToolResultJSON(result)
}

// handleDiffSnapshots compares two snapshots and returns differences.
func (s *Server) handleDiffSnapshots(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	beforePath, err := request.RequireString("before")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("before"),
			WithSuggestion("Provide the path to the 'before' snapshot file"),
		), nil
	}
	afterPath, err := request.RequireString("after")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("after"),
			WithSuggestion("Provide the path to the 'after' snapshot file"),
		), nil
	}

	ignoreUnchanged := request.GetBool("ignore_unchanged", true)

	beforeSnap, err := resolver.LoadSnapshot(beforePath)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to load before snapshot %q: %v", beforePath, err),
			WithField("before"),
			WithSuggestion("Check that the file exists and is a valid snapshot JSON file"),
		), nil
	}
	afterSnap, err := resolver.LoadSnapshot(afterPath)
	if err != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("failed to load after snapshot %q: %v", afterPath, err),
			WithField("after"),
			WithSuggestion("Check that the file exists and is a valid snapshot JSON file"),
		), nil
	}

	diff := resolver.DiffSnapshotsWithOptions(beforeSnap, afterSnap, &resolver.DiffOptions{
		IgnoreUnchanged: ignoreUnchanged,
	})

	// Build structured response matching the design doc schema
	before := map[string]any{
		"solution":  diff.Before.Solution,
		"timestamp": diff.Before.Timestamp,
		"status":    diff.Before.Status,
	}
	after := map[string]any{
		"solution":  diff.After.Solution,
		"timestamp": diff.After.Timestamp,
		"status":    diff.After.Status,
	}

	added := make([]map[string]any, 0)
	removed := make([]map[string]any, 0)
	changed := make([]map[string]any, 0)
	statusChanges := make([]map[string]any, 0)

	for name, rd := range diff.Resolvers {
		if rd == nil {
			continue
		}
		switch rd.Type {
		case resolver.DiffTypeAdded:
			entry := map[string]any{"name": name}
			if rd.After != nil {
				entry["value"] = rd.After.Value
			}
			added = append(added, entry)
		case resolver.DiffTypeRemoved:
			entry := map[string]any{"name": name}
			if rd.Before != nil {
				entry["value"] = rd.Before.Value
			}
			removed = append(removed, entry)
		case resolver.DiffTypeUnchanged:
			// Unchanged resolvers are not included in the diff output
			continue
		case resolver.DiffTypeModified:
			// Separate status changes from value changes
			hasStatusChange := false
			fields := make([]map[string]any, 0)
			for _, fc := range rd.Changes {
				if fc.Field == "status" {
					hasStatusChange = true
					statusChanges = append(statusChanges, map[string]any{
						"name":   name,
						"before": fc.Before,
						"after":  fc.After,
					})
				} else {
					fields = append(fields, map[string]any{
						"field":  fc.Field,
						"before": fc.Before,
						"after":  fc.After,
					})
				}
			}
			if len(fields) > 0 || !hasStatusChange {
				entry := map[string]any{
					"name":   name,
					"fields": fields,
				}
				changed = append(changed, entry)
			}
		}
	}

	summary := map[string]any{
		"added":         diff.Summary.Added,
		"removed":       diff.Summary.Removed,
		"changed":       diff.Summary.Modified,
		"statusChanges": len(statusChanges),
		"unchanged":     diff.Summary.Unchanged,
	}

	result := map[string]any{
		"before": before,
		"after":  after,
		"changes": map[string]any{
			"added":         added,
			"removed":       removed,
			"changed":       changed,
			"statusChanges": statusChanges,
		},
		"summary": summary,
	}

	return mcp.NewToolResultJSON(result)
}
