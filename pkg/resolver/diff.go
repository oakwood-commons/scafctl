// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// DiffType represents the type of difference found
type DiffType string

const (
	DiffTypeAdded     DiffType = "added"     // Resolver exists in after but not before
	DiffTypeRemoved   DiffType = "removed"   // Resolver exists in before but not after
	DiffTypeModified  DiffType = "modified"  // Resolver exists in both but values differ
	DiffTypeUnchanged DiffType = "unchanged" // Resolver exists in both and is identical
)

// SnapshotDiff represents the differences between two snapshots
type SnapshotDiff struct {
	Before    *SnapshotMetadata        `json:"before" yaml:"before" doc:"Metadata of the before snapshot"`
	After     *SnapshotMetadata        `json:"after" yaml:"after" doc:"Metadata of the after snapshot"`
	Resolvers map[string]*ResolverDiff `json:"resolvers" yaml:"resolvers" doc:"Differences in resolvers"`
	Summary   *DiffSummary             `json:"summary" yaml:"summary" doc:"Summary of changes"`
}

// ResolverDiff represents the difference for a single resolver
//
//nolint:revive // ResolverDiff name is intentional for clarity in resolver package
type ResolverDiff struct {
	Type    DiffType          `json:"type" yaml:"type" doc:"Type of difference (added, removed, modified, unchanged)" maxLength:"16" example:"modified"`
	Before  *SnapshotResolver `json:"before,omitempty" yaml:"before,omitempty" doc:"Value before change"`
	After   *SnapshotResolver `json:"after,omitempty" yaml:"after,omitempty" doc:"Value after change"`
	Changes []FieldChange     `json:"changes,omitempty" yaml:"changes,omitempty" doc:"List of field changes" maxItems:"50"`
}

// FieldChange represents a change in a specific field
type FieldChange struct {
	Field  string `json:"field" yaml:"field" doc:"Field name that changed" maxLength:"128" example:"value"`
	Before any    `json:"before" yaml:"before" doc:"Value before change"`
	After  any    `json:"after" yaml:"after" doc:"Value after change"`
}

// DiffSummary provides a summary of all changes
type DiffSummary struct {
	TotalResolvers int `json:"totalResolvers" yaml:"totalResolvers" doc:"Total number of resolvers compared" maximum:"10000" example:"20"`
	Added          int `json:"added" yaml:"added" doc:"Number of resolvers added" maximum:"10000" example:"2"`
	Removed        int `json:"removed" yaml:"removed" doc:"Number of resolvers removed" maximum:"10000" example:"1"`
	Modified       int `json:"modified" yaml:"modified" doc:"Number of resolvers modified" maximum:"10000" example:"3"`
	Unchanged      int `json:"unchanged" yaml:"unchanged" doc:"Number of resolvers unchanged" maximum:"10000" example:"14"`
}

// DiffSnapshots compares two snapshots and returns their differences
func DiffSnapshots(before, after *Snapshot) *SnapshotDiff {
	diff := &SnapshotDiff{
		Before:    &before.Metadata,
		After:     &after.Metadata,
		Resolvers: make(map[string]*ResolverDiff),
		Summary:   &DiffSummary{},
	}

	// Get all unique resolver names
	allNames := make(map[string]bool)
	for name := range before.Resolvers {
		allNames[name] = true
	}
	for name := range after.Resolvers {
		allNames[name] = true
	}

	// Compare each resolver
	for name := range allNames {
		beforeRes, existsBefore := before.Resolvers[name]
		afterRes, existsAfter := after.Resolvers[name]

		var resolverDiff *ResolverDiff

		switch {
		case !existsBefore && existsAfter:
			// Resolver added
			resolverDiff = &ResolverDiff{
				Type:  DiffTypeAdded,
				After: afterRes,
			}
			diff.Summary.Added++
		case existsBefore && !existsAfter:
			// Resolver removed
			resolverDiff = &ResolverDiff{
				Type:   DiffTypeRemoved,
				Before: beforeRes,
			}
			diff.Summary.Removed++
		default:
			// Resolver exists in both - check for changes
			changes := compareResolvers(beforeRes, afterRes)
			if len(changes) > 0 {
				resolverDiff = &ResolverDiff{
					Type:    DiffTypeModified,
					Before:  beforeRes,
					After:   afterRes,
					Changes: changes,
				}
				diff.Summary.Modified++
			} else {
				resolverDiff = &ResolverDiff{
					Type:   DiffTypeUnchanged,
					Before: beforeRes,
					After:  afterRes,
				}
				diff.Summary.Unchanged++
			}
		}

		diff.Resolvers[name] = resolverDiff
	}

	diff.Summary.TotalResolvers = len(allNames)

	return diff
}

// compareResolvers compares two resolver snapshots and returns list of changes
func compareResolvers(before, after *SnapshotResolver) []FieldChange {
	var changes []FieldChange

	// Compare value (use deep equal for complex types)
	if !reflect.DeepEqual(before.Value, after.Value) {
		changes = append(changes, FieldChange{
			Field:  "value",
			Before: before.Value,
			After:  after.Value,
		})
	}

	// Compare status
	if before.Status != after.Status {
		changes = append(changes, FieldChange{
			Field:  "status",
			Before: before.Status,
			After:  after.Status,
		})
	}

	// Compare phase
	if before.Phase != after.Phase {
		changes = append(changes, FieldChange{
			Field:  "phase",
			Before: before.Phase,
			After:  after.Phase,
		})
	}

	// Compare duration
	if before.Duration != after.Duration {
		changes = append(changes, FieldChange{
			Field:  "duration",
			Before: before.Duration,
			After:  after.Duration,
		})
	}

	// Compare provider calls
	if before.ProviderCalls != after.ProviderCalls {
		changes = append(changes, FieldChange{
			Field:  "providerCalls",
			Before: before.ProviderCalls,
			After:  after.ProviderCalls,
		})
	}

	// Compare value size
	if before.ValueSizeBytes != after.ValueSizeBytes {
		changes = append(changes, FieldChange{
			Field:  "valueSizeBytes",
			Before: before.ValueSizeBytes,
			After:  after.ValueSizeBytes,
		})
	}

	// Compare error
	if before.Error != after.Error {
		changes = append(changes, FieldChange{
			Field:  "error",
			Before: before.Error,
			After:  after.Error,
		})
	}

	// Compare failed attempts count
	if len(before.FailedAttempts) != len(after.FailedAttempts) {
		changes = append(changes, FieldChange{
			Field:  "failedAttempts",
			Before: len(before.FailedAttempts),
			After:  len(after.FailedAttempts),
		})
	}

	return changes
}

// FormatDiffHuman formats the diff in human-readable format
func FormatDiffHuman(diff *SnapshotDiff) string {
	var sb strings.Builder

	// Header
	sb.WriteString("Snapshot Comparison\n")
	sb.WriteString("===================\n\n")

	// Metadata comparison
	fmt.Fprintf(&sb, "Before: %s (v%s) at %s\n",
		diff.Before.Solution,
		diff.Before.Version,
		diff.Before.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&sb, "After:  %s (v%s) at %s\n\n",
		diff.After.Solution,
		diff.After.Version,
		diff.After.Timestamp.Format("2006-01-02 15:04:05"))

	// Summary
	sb.WriteString("Summary\n")
	sb.WriteString("-------\n")
	fmt.Fprintf(&sb, "Total: %d | Added: %d | Removed: %d | Modified: %d | Unchanged: %d\n\n",
		diff.Summary.TotalResolvers,
		diff.Summary.Added,
		diff.Summary.Removed,
		diff.Summary.Modified,
		diff.Summary.Unchanged)

	// Get sorted resolver names
	names := make([]string, 0, len(diff.Resolvers))
	for name := range diff.Resolvers {
		names = append(names, name)
	}
	sort.Strings(names)

	// Added resolvers
	if diff.Summary.Added > 0 {
		sb.WriteString("Added Resolvers\n")
		sb.WriteString("---------------\n")
		for _, name := range names {
			rd := diff.Resolvers[name]
			if rd.Type == DiffTypeAdded {
				fmt.Fprintf(&sb, "+ %s\n", name)
				fmt.Fprintf(&sb, "    Value:  %v\n", formatValue(rd.After.Value))
				fmt.Fprintf(&sb, "    Status: %s\n", rd.After.Status)
				fmt.Fprintf(&sb, "    Phase:  %d\n", rd.After.Phase)
			}
		}
		sb.WriteString("\n")
	}

	// Removed resolvers
	if diff.Summary.Removed > 0 {
		sb.WriteString("Removed Resolvers\n")
		sb.WriteString("-----------------\n")
		for _, name := range names {
			rd := diff.Resolvers[name]
			if rd.Type == DiffTypeRemoved {
				fmt.Fprintf(&sb, "- %s\n", name)
				fmt.Fprintf(&sb, "    Value:  %v\n", formatValue(rd.Before.Value))
				fmt.Fprintf(&sb, "    Status: %s\n", rd.Before.Status)
				fmt.Fprintf(&sb, "    Phase:  %d\n", rd.Before.Phase)
			}
		}
		sb.WriteString("\n")
	}

	// Modified resolvers
	if diff.Summary.Modified > 0 {
		sb.WriteString("Modified Resolvers\n")
		sb.WriteString("------------------\n")
		for _, name := range names {
			rd := diff.Resolvers[name]
			if rd.Type == DiffTypeModified {
				fmt.Fprintf(&sb, "~ %s\n", name)
				for _, change := range rd.Changes {
					fmt.Fprintf(&sb, "    %s:\n", change.Field)
					fmt.Fprintf(&sb, "      - %v\n", formatValue(change.Before))
					fmt.Fprintf(&sb, "      + %v\n", formatValue(change.After))
				}
			}
		}
	}

	return sb.String()
}

// FormatDiffJSON formats the diff as JSON
func FormatDiffJSON(diff *SnapshotDiff) (string, error) {
	data, err := json.MarshalIndent(diff, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal diff to JSON: %w", err)
	}
	return string(data), nil
}

// FormatDiffUnified formats the diff in unified diff format (similar to git diff)
func FormatDiffUnified(diff *SnapshotDiff) string {
	var sb strings.Builder

	// Header
	fmt.Fprintf(&sb, "--- %s (v%s) %s\n",
		diff.Before.Solution,
		diff.Before.Version,
		diff.Before.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&sb, "+++ %s (v%s) %s\n",
		diff.After.Solution,
		diff.After.Version,
		diff.After.Timestamp.Format("2006-01-02 15:04:05"))
	sb.WriteString("\n")

	// Get sorted resolver names
	names := make([]string, 0, len(diff.Resolvers))
	for name := range diff.Resolvers {
		names = append(names, name)
	}
	sort.Strings(names)

	// Output changes
	for _, name := range names {
		rd := diff.Resolvers[name]

		switch rd.Type {
		case DiffTypeAdded:
			fmt.Fprintf(&sb, "@@ Resolver: %s (added) @@\n", name)
			fmt.Fprintf(&sb, "+  status: %s\n", rd.After.Status)
			fmt.Fprintf(&sb, "+  value: %v\n", formatValue(rd.After.Value))
			fmt.Fprintf(&sb, "+  phase: %d\n", rd.After.Phase)
			fmt.Fprintf(&sb, "+  duration: %s\n\n", rd.After.Duration)

		case DiffTypeRemoved:
			fmt.Fprintf(&sb, "@@ Resolver: %s (removed) @@\n", name)
			fmt.Fprintf(&sb, "-  status: %s\n", rd.Before.Status)
			fmt.Fprintf(&sb, "-  value: %v\n", formatValue(rd.Before.Value))
			fmt.Fprintf(&sb, "-  phase: %d\n", rd.Before.Phase)
			fmt.Fprintf(&sb, "-  duration: %s\n\n", rd.Before.Duration)

		case DiffTypeModified:
			fmt.Fprintf(&sb, "@@ Resolver: %s (modified) @@\n", name)
			for _, change := range rd.Changes {
				fmt.Fprintf(&sb, "-  %s: %v\n", change.Field, formatValue(change.Before))
				fmt.Fprintf(&sb, "+  %s: %v\n", change.Field, formatValue(change.After))
			}
			sb.WriteString("\n")

		case DiffTypeUnchanged:
			// Do nothing for unchanged resolvers in unified diff format
		}
	}

	return sb.String()
}

// formatValue formats a value for display, handling complex types
func formatValue(v any) string {
	if v == nil {
		return "<nil>"
	}

	// Handle strings specially to avoid quotes in simple cases
	if str, ok := v.(string); ok {
		// Check if it's a simple value or needs quotes
		if strings.Contains(str, " ") || strings.Contains(str, "\n") {
			return fmt.Sprintf("%q", str)
		}
		return str
	}

	// For complex types, use JSON representation
	if reflect.TypeOf(v).Kind() == reflect.Map || reflect.TypeOf(v).Kind() == reflect.Slice {
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(data)
	}

	return fmt.Sprintf("%v", v)
}

// DiffOptions provides options for diff comparison
type DiffOptions struct {
	IgnoreUnchanged bool     `json:"ignoreUnchanged" yaml:"ignoreUnchanged" doc:"Whether to omit unchanged resolvers from output"`
	IgnoreFields    []string `json:"ignoreFields,omitempty" yaml:"ignoreFields,omitempty" doc:"Fields to ignore in comparison (e.g., duration, providerCalls)" maxItems:"20"`
	ResolverFilter  string   `json:"resolverFilter,omitempty" yaml:"resolverFilter,omitempty" doc:"Only compare resolvers matching this pattern" maxLength:"256" example:"api-*"`
}

// DiffSnapshotsWithOptions compares two snapshots with custom options
func DiffSnapshotsWithOptions(before, after *Snapshot, opts *DiffOptions) *SnapshotDiff {
	diff := DiffSnapshots(before, after)

	if opts == nil {
		return diff
	}

	// Apply filters
	if opts.IgnoreUnchanged {
		for name, rd := range diff.Resolvers {
			if rd.Type == DiffTypeUnchanged {
				delete(diff.Resolvers, name)
			}
		}
	}

	// Filter fields if specified
	if len(opts.IgnoreFields) > 0 {
		ignoreMap := make(map[string]bool)
		for _, field := range opts.IgnoreFields {
			ignoreMap[field] = true
		}

		for _, rd := range diff.Resolvers {
			if rd.Type == DiffTypeModified {
				var filteredChanges []FieldChange
				for _, change := range rd.Changes {
					if !ignoreMap[change.Field] {
						filteredChanges = append(filteredChanges, change)
					}
				}
				rd.Changes = filteredChanges

				// If no changes remain after filtering, mark as unchanged
				if len(filteredChanges) == 0 {
					rd.Type = DiffTypeUnchanged
					diff.Summary.Modified--
					diff.Summary.Unchanged++
				}
			}
		}
	}

	return diff
}
