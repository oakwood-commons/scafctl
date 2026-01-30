package resolver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffSnapshots_IdenticalSnapshots(t *testing.T) {
	snapshot := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"resolver1": {
				Value:          "value1",
				Status:         "completed",
				Phase:          1,
				Duration:       "100ms",
				ProviderCalls:  1,
				ValueSizeBytes: 100,
			},
			"resolver2": {
				Value:          "value2",
				Status:         "completed",
				Phase:          1,
				Duration:       "200ms",
				ProviderCalls:  2,
				ValueSizeBytes: 200,
			},
		},
	}

	diff := DiffSnapshots(snapshot, snapshot)

	assert.NotNil(t, diff)
	assert.Equal(t, 2, diff.Summary.TotalResolvers)
	assert.Equal(t, 0, diff.Summary.Added)
	assert.Equal(t, 0, diff.Summary.Removed)
	assert.Equal(t, 0, diff.Summary.Modified)
	assert.Equal(t, 2, diff.Summary.Unchanged)

	for _, rd := range diff.Resolvers {
		assert.Equal(t, DiffTypeUnchanged, rd.Type)
		assert.Empty(t, rd.Changes)
	}
}

func TestDiffSnapshots_AddedResolvers(t *testing.T) {
	before := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"resolver1": {
				Value:  "value1",
				Status: "completed",
				Phase:  1,
			},
		},
	}

	after := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"resolver1": {
				Value:  "value1",
				Status: "completed",
				Phase:  1,
			},
			"resolver2": {
				Value:  "value2",
				Status: "completed",
				Phase:  1,
			},
			"resolver3": {
				Value:  "value3",
				Status: "completed",
				Phase:  2,
			},
		},
	}

	diff := DiffSnapshots(before, after)

	assert.NotNil(t, diff)
	assert.Equal(t, 3, diff.Summary.TotalResolvers)
	assert.Equal(t, 2, diff.Summary.Added)
	assert.Equal(t, 0, diff.Summary.Removed)
	assert.Equal(t, 0, diff.Summary.Modified)
	assert.Equal(t, 1, diff.Summary.Unchanged)

	assert.Equal(t, DiffTypeAdded, diff.Resolvers["resolver2"].Type)
	assert.Equal(t, DiffTypeAdded, diff.Resolvers["resolver3"].Type)
	assert.Equal(t, DiffTypeUnchanged, diff.Resolvers["resolver1"].Type)

	assert.NotNil(t, diff.Resolvers["resolver2"].After)
	assert.Nil(t, diff.Resolvers["resolver2"].Before)
}

func TestDiffSnapshots_RemovedResolvers(t *testing.T) {
	before := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"resolver1": {
				Value:  "value1",
				Status: "completed",
				Phase:  1,
			},
			"resolver2": {
				Value:  "value2",
				Status: "completed",
				Phase:  1,
			},
			"resolver3": {
				Value:  "value3",
				Status: "failed",
				Phase:  2,
				Error:  "some error",
			},
		},
	}

	after := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"resolver1": {
				Value:  "value1",
				Status: "completed",
				Phase:  1,
			},
		},
	}

	diff := DiffSnapshots(before, after)

	assert.NotNil(t, diff)
	assert.Equal(t, 3, diff.Summary.TotalResolvers)
	assert.Equal(t, 0, diff.Summary.Added)
	assert.Equal(t, 2, diff.Summary.Removed)
	assert.Equal(t, 0, diff.Summary.Modified)
	assert.Equal(t, 1, diff.Summary.Unchanged)

	assert.Equal(t, DiffTypeRemoved, diff.Resolvers["resolver2"].Type)
	assert.Equal(t, DiffTypeRemoved, diff.Resolvers["resolver3"].Type)
	assert.Equal(t, DiffTypeUnchanged, diff.Resolvers["resolver1"].Type)

	assert.NotNil(t, diff.Resolvers["resolver2"].Before)
	assert.Nil(t, diff.Resolvers["resolver2"].After)
}

func TestDiffSnapshots_ModifiedResolvers(t *testing.T) {
	before := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"resolver1": {
				Value:          "old-value",
				Status:         "completed",
				Phase:          1,
				Duration:       "100ms",
				ProviderCalls:  1,
				ValueSizeBytes: 100,
			},
			"resolver2": {
				Value:  "value2",
				Status: "failed",
				Phase:  1,
				Error:  "old error",
				FailedAttempts: []SnapshotFailedAttempt{
					{Provider: "provider1", Error: "error1"},
				},
			},
		},
	}

	after := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"resolver1": {
				Value:          "new-value",
				Status:         "completed",
				Phase:          2,
				Duration:       "200ms",
				ProviderCalls:  3,
				ValueSizeBytes: 200,
			},
			"resolver2": {
				Value:  "value2",
				Status: "completed",
				Phase:  2,
				Error:  "",
			},
		},
	}

	diff := DiffSnapshots(before, after)

	assert.NotNil(t, diff)
	assert.Equal(t, 2, diff.Summary.TotalResolvers)
	assert.Equal(t, 0, diff.Summary.Added)
	assert.Equal(t, 0, diff.Summary.Removed)
	assert.Equal(t, 2, diff.Summary.Modified)
	assert.Equal(t, 0, diff.Summary.Unchanged)

	// Check resolver1 changes
	rd1 := diff.Resolvers["resolver1"]
	assert.Equal(t, DiffTypeModified, rd1.Type)
	assert.NotEmpty(t, rd1.Changes)

	// Verify specific field changes for resolver1
	changeMap := make(map[string]FieldChange)
	for _, change := range rd1.Changes {
		changeMap[change.Field] = change
	}

	assert.Contains(t, changeMap, "value")
	assert.Equal(t, "old-value", changeMap["value"].Before)
	assert.Equal(t, "new-value", changeMap["value"].After)

	assert.Contains(t, changeMap, "phase")
	assert.Equal(t, 1, changeMap["phase"].Before)
	assert.Equal(t, 2, changeMap["phase"].After)

	assert.Contains(t, changeMap, "duration")
	assert.Contains(t, changeMap, "providerCalls")
	assert.Contains(t, changeMap, "valueSizeBytes")

	// Check resolver2 changes (status and error)
	rd2 := diff.Resolvers["resolver2"]
	assert.Equal(t, DiffTypeModified, rd2.Type)

	changeMap2 := make(map[string]FieldChange)
	for _, change := range rd2.Changes {
		changeMap2[change.Field] = change
	}

	assert.Contains(t, changeMap2, "status")
	assert.Equal(t, "failed", changeMap2["status"].Before)
	assert.Equal(t, "completed", changeMap2["status"].After)

	assert.Contains(t, changeMap2, "error")
	assert.Equal(t, "old error", changeMap2["error"].Before)
	assert.Equal(t, "", changeMap2["error"].After)

	assert.Contains(t, changeMap2, "failedAttempts")
}

func TestDiffSnapshots_MixedChanges(t *testing.T) {
	before := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"unchanged": {
				Value:  "value",
				Status: "completed",
				Phase:  1,
			},
			"modified": {
				Value:  "old",
				Status: "completed",
				Phase:  1,
			},
			"removed": {
				Value:  "gone",
				Status: "completed",
				Phase:  1,
			},
		},
	}

	after := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "2.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"unchanged": {
				Value:  "value",
				Status: "completed",
				Phase:  1,
			},
			"modified": {
				Value:  "new",
				Status: "completed",
				Phase:  1,
			},
			"added": {
				Value:  "fresh",
				Status: "completed",
				Phase:  2,
			},
		},
	}

	diff := DiffSnapshots(before, after)

	assert.NotNil(t, diff)
	assert.Equal(t, 4, diff.Summary.TotalResolvers)
	assert.Equal(t, 1, diff.Summary.Added)
	assert.Equal(t, 1, diff.Summary.Removed)
	assert.Equal(t, 1, diff.Summary.Modified)
	assert.Equal(t, 1, diff.Summary.Unchanged)

	assert.Equal(t, DiffTypeUnchanged, diff.Resolvers["unchanged"].Type)
	assert.Equal(t, DiffTypeModified, diff.Resolvers["modified"].Type)
	assert.Equal(t, DiffTypeRemoved, diff.Resolvers["removed"].Type)
	assert.Equal(t, DiffTypeAdded, diff.Resolvers["added"].Type)

	// Verify metadata difference
	assert.Equal(t, "1.0.0", diff.Before.Version)
	assert.Equal(t, "2.0.0", diff.After.Version)
}

func TestFormatDiffHuman(t *testing.T) {
	before := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"modified": {
				Value:  "old",
				Status: "completed",
				Phase:  1,
			},
			"removed": {
				Value:  "gone",
				Status: "completed",
				Phase:  1,
			},
		},
	}

	after := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "2.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"added": {
				Value:  "fresh",
				Status: "completed",
				Phase:  2,
			},
			"modified": {
				Value:  "new",
				Status: "completed",
				Phase:  2,
			},
		},
	}

	diff := DiffSnapshots(before, after)
	output := FormatDiffHuman(diff)

	// Check that output contains expected sections
	assert.Contains(t, output, "Snapshot Comparison")
	assert.Contains(t, output, "Summary")
	assert.Contains(t, output, "Added Resolvers")
	assert.Contains(t, output, "Removed Resolvers")
	assert.Contains(t, output, "Modified Resolvers")

	// Check metadata
	assert.Contains(t, output, "Before: test-solution (v1.0.0)")
	assert.Contains(t, output, "After:  test-solution (v2.0.0)")

	// Check summary counts
	assert.Contains(t, output, "Total: 3")
	assert.Contains(t, output, "Added: 1")
	assert.Contains(t, output, "Removed: 1")
	assert.Contains(t, output, "Modified: 1")

	// Check resolver names
	assert.Contains(t, output, "added")
	assert.Contains(t, output, "removed")
	assert.Contains(t, output, "modified")
}

func TestFormatDiffJSON(t *testing.T) {
	before := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"resolver1": {
				Value:  "value1",
				Status: "completed",
				Phase:  1,
			},
		},
	}

	after := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"resolver1": {
				Value:  "value2",
				Status: "completed",
				Phase:  1,
			},
		},
	}

	diff := DiffSnapshots(before, after)
	output, err := FormatDiffJSON(diff)

	require.NoError(t, err)
	assert.Contains(t, output, `"before"`)
	assert.Contains(t, output, `"after"`)
	assert.Contains(t, output, `"resolvers"`)
	assert.Contains(t, output, `"summary"`)
	assert.Contains(t, output, `"modified"`)
	assert.Contains(t, output, `"resolver1"`)
}

func TestFormatDiffUnified(t *testing.T) {
	before := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"modified": {
				Value:  "old",
				Status: "completed",
				Phase:  1,
			},
		},
	}

	after := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "2.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"modified": {
				Value:  "new",
				Status: "completed",
				Phase:  2,
			},
		},
	}

	diff := DiffSnapshots(before, after)
	output := FormatDiffUnified(diff)

	// Check unified diff format
	assert.Contains(t, output, "--- test-solution (v1.0.0)")
	assert.Contains(t, output, "+++ test-solution (v2.0.0)")
	assert.Contains(t, output, "@@ Resolver: modified (modified) @@")
	assert.Contains(t, output, "-  value: old")
	assert.Contains(t, output, "+  value: new")
	assert.Contains(t, output, "-  phase: 1")
	assert.Contains(t, output, "+  phase: 2")
}

func TestDiffSnapshotsWithOptions_IgnoreUnchanged(t *testing.T) {
	before := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"unchanged": {
				Value:  "value",
				Status: "completed",
				Phase:  1,
			},
			"modified": {
				Value:  "old",
				Status: "completed",
				Phase:  1,
			},
		},
	}

	after := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"unchanged": {
				Value:  "value",
				Status: "completed",
				Phase:  1,
			},
			"modified": {
				Value:  "new",
				Status: "completed",
				Phase:  1,
			},
		},
	}

	opts := &DiffOptions{
		IgnoreUnchanged: true,
	}

	diff := DiffSnapshotsWithOptions(before, after, opts)

	assert.Equal(t, 1, len(diff.Resolvers))
	assert.Contains(t, diff.Resolvers, "modified")
	assert.NotContains(t, diff.Resolvers, "unchanged")
}

func TestDiffSnapshotsWithOptions_IgnoreFields(t *testing.T) {
	before := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"resolver1": {
				Value:          "value",
				Status:         "completed",
				Phase:          1,
				Duration:       "100ms",
				ProviderCalls:  1,
				ValueSizeBytes: 100,
			},
		},
	}

	after := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"resolver1": {
				Value:          "value",
				Status:         "completed",
				Phase:          1,
				Duration:       "200ms", // Changed
				ProviderCalls:  3,       // Changed
				ValueSizeBytes: 200,     // Changed
			},
		},
	}

	// Ignore timing-related fields
	opts := &DiffOptions{
		IgnoreFields: []string{"duration", "providerCalls", "valueSizeBytes"},
	}

	diff := DiffSnapshotsWithOptions(before, after, opts)

	// Should be marked as unchanged since we ignored the only changed fields
	assert.Equal(t, DiffTypeUnchanged, diff.Resolvers["resolver1"].Type)
	assert.Equal(t, 0, diff.Summary.Modified)
	assert.Equal(t, 1, diff.Summary.Unchanged)
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{
			name:     "nil value",
			value:    nil,
			expected: "<nil>",
		},
		{
			name:     "simple string",
			value:    "hello",
			expected: "hello",
		},
		{
			name:     "string with spaces",
			value:    "hello world",
			expected: `"hello world"`,
		},
		{
			name:     "string with newlines",
			value:    "line1\nline2",
			expected: `"line1\nline2"`,
		},
		{
			name:     "integer",
			value:    42,
			expected: "42",
		},
		{
			name:     "boolean",
			value:    true,
			expected: "true",
		},
		{
			name:     "slice",
			value:    []string{"a", "b", "c"},
			expected: `["a","b","c"]`,
		},
		{
			name:     "map",
			value:    map[string]int{"count": 5},
			expected: `{"count":5}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatValue(tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCompareResolvers(t *testing.T) {
	tests := []struct {
		name           string
		before         *SnapshotResolver
		after          *SnapshotResolver
		expectedFields []string
	}{
		{
			name: "all fields changed",
			before: &SnapshotResolver{
				Value:          "old",
				Status:         "failed",
				Phase:          1,
				Duration:       "100ms",
				ProviderCalls:  1,
				ValueSizeBytes: 100,
				Error:          "error1",
				FailedAttempts: []SnapshotFailedAttempt{{Provider: "p1"}},
			},
			after: &SnapshotResolver{
				Value:          "new",
				Status:         "completed",
				Phase:          2,
				Duration:       "200ms",
				ProviderCalls:  2,
				ValueSizeBytes: 200,
				Error:          "",
				FailedAttempts: []SnapshotFailedAttempt{},
			},
			expectedFields: []string{"value", "status", "phase", "duration", "providerCalls", "valueSizeBytes", "error", "failedAttempts"},
		},
		{
			name: "only value changed",
			before: &SnapshotResolver{
				Value:  "old",
				Status: "completed",
				Phase:  1,
			},
			after: &SnapshotResolver{
				Value:  "new",
				Status: "completed",
				Phase:  1,
			},
			expectedFields: []string{"value"},
		},
		{
			name: "no changes",
			before: &SnapshotResolver{
				Value:  "same",
				Status: "completed",
				Phase:  1,
			},
			after: &SnapshotResolver{
				Value:  "same",
				Status: "completed",
				Phase:  1,
			},
			expectedFields: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := compareResolvers(tt.before, tt.after)
			assert.Equal(t, len(tt.expectedFields), len(changes))

			changedFields := make([]string, 0, len(changes))
			for _, change := range changes {
				changedFields = append(changedFields, change.Field)
			}

			for _, expectedField := range tt.expectedFields {
				assert.Contains(t, changedFields, expectedField)
			}
		})
	}
}

func TestDiffSnapshots_EmptySnapshots(t *testing.T) {
	before := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{},
	}

	after := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{},
	}

	diff := DiffSnapshots(before, after)

	assert.NotNil(t, diff)
	assert.Equal(t, 0, diff.Summary.TotalResolvers)
	assert.Equal(t, 0, diff.Summary.Added)
	assert.Equal(t, 0, diff.Summary.Removed)
	assert.Equal(t, 0, diff.Summary.Modified)
	assert.Equal(t, 0, diff.Summary.Unchanged)
	assert.Empty(t, diff.Resolvers)
}

func TestDiffSnapshots_ComplexValues(t *testing.T) {
	before := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"map_resolver": {
				Value: map[string]any{
					"key1": "value1",
					"key2": 42,
				},
				Status: "completed",
				Phase:  1,
			},
			"slice_resolver": {
				Value:  []string{"a", "b", "c"},
				Status: "completed",
				Phase:  1,
			},
		},
	}

	after := &Snapshot{
		Metadata: SnapshotMetadata{
			Timestamp: time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
			Solution:  "test-solution",
			Version:   "1.0.0",
		},
		Resolvers: map[string]*SnapshotResolver{
			"map_resolver": {
				Value: map[string]any{
					"key1": "value1",
					"key2": 43, // Changed
				},
				Status: "completed",
				Phase:  1,
			},
			"slice_resolver": {
				Value:  []string{"a", "b", "d"}, // Changed
				Status: "completed",
				Phase:  1,
			},
		},
	}

	diff := DiffSnapshots(before, after)

	assert.Equal(t, 2, diff.Summary.Modified)
	assert.Equal(t, 0, diff.Summary.Unchanged)

	// Both resolvers should be marked as modified
	assert.Equal(t, DiffTypeModified, diff.Resolvers["map_resolver"].Type)
	assert.Equal(t, DiffTypeModified, diff.Resolvers["slice_resolver"].Type)

	// Verify value changes are captured
	assert.NotEmpty(t, diff.Resolvers["map_resolver"].Changes)
	assert.NotEmpty(t, diff.Resolvers["slice_resolver"].Changes)
}
