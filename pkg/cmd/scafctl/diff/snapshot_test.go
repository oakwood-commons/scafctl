// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package diff

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/diffreport"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandDiffSnapshot(t *testing.T) {
	cliParams := &settings.Run{}
	ioStreams := terminal.IOStreams{}

	cmd := CommandDiffSnapshot(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "snapshot [before-snapshot] [after-snapshot]", cmd.Use)
	assert.Equal(t, "Compare two snapshots", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)
	assert.Contains(t, cmd.Example, "scafctl diff snapshot")

	// Standard kvx output flag with the default report format.
	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag, "output flag should exist")
	assert.Equal(t, "o", outputFlag.Shorthand)
	assert.Equal(t, "auto", outputFlag.DefValue)

	// -f must NOT be bound (reserved for file semantics across the CLI).
	assert.Nil(t, cmd.Flags().ShorthandLookup("f"), "-f must not be bound on diff snapshot")
	assert.Nil(t, cmd.Flags().Lookup("format"), "legacy --format flag should be removed")

	ignoreUnchangedFlag := cmd.Flags().Lookup("ignore-unchanged")
	require.NotNil(t, ignoreUnchangedFlag, "ignore-unchanged flag should exist")

	ignoreFieldsFlag := cmd.Flags().Lookup("ignore-fields")
	require.NotNil(t, ignoreFieldsFlag, "ignore-fields flag should exist")
}

func TestRunSnapshotDiff_MissingBeforeFile(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	afterFile := filepath.Join(tmpDir, "after.json")
	snapshot := createTestSnapshotForDiff()
	err := resolver.SaveSnapshot(snapshot, afterFile)
	require.NoError(t, err)

	opts := &SnapshotDiffOptions{
		BeforeFile: "/nonexistent/before.json",
		AfterFile:  afterFile,
	}
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	testCtx := writer.WithWriter(ctx, writer.New(ioStreams, &settings.Run{}))

	err = runSnapshotDiff(testCtx, opts, ioStreams, "scafctl")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load before snapshot")
}

func TestRunSnapshotDiff_MissingAfterFile(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	beforeFile := filepath.Join(tmpDir, "before.json")
	snapshot := createTestSnapshotForDiff()
	err := resolver.SaveSnapshot(snapshot, beforeFile)
	require.NoError(t, err)

	opts := &SnapshotDiffOptions{
		BeforeFile: beforeFile,
		AfterFile:  "/nonexistent/after.json",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	testCtx := writer.WithWriter(ctx, writer.New(ioStreams, &settings.Run{}))

	err = runSnapshotDiff(testCtx, opts, ioStreams, "scafctl")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load after snapshot")
}

func TestRunSnapshotDiff_DefaultReport(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	beforeFile, afterFile := createTestSnapshotPair(t, tmpDir)

	opts := &SnapshotDiffOptions{
		BeforeFile: beforeFile,
		AfterFile:  afterFile,
	}
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	testCtx := writer.WithWriter(ctx, writer.New(ioStreams, &settings.Run{NoColor: true}))

	err := runSnapshotDiff(testCtx, opts, ioStreams, "scafctl")

	require.NoError(t, err)
	output := stdout.String()
	assert.Contains(t, output, "Diff: test-solution@1.0.0 -> test-solution@1.0.0")
	assert.Contains(t, output, "test_resolver")
	assert.Contains(t, output, "old-value")
	assert.Contains(t, output, "new-value")
	assert.Contains(t, output, "Summary:")
}

func TestRunSnapshotDiff_JSONFormat(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	beforeFile, afterFile := createTestSnapshotPair(t, tmpDir)

	opts := &SnapshotDiffOptions{
		BeforeFile: beforeFile,
		AfterFile:  afterFile,
	}
	opts.Output = "json"
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	testCtx := writer.WithWriter(ctx, writer.New(ioStreams, &settings.Run{}))

	err := runSnapshotDiff(testCtx, opts, ioStreams, "scafctl")

	require.NoError(t, err)

	// Verify output is valid JSON with the native snapshot-diff shape preserved.
	var result map[string]any
	err = json.Unmarshal(stdout.Bytes(), &result)
	require.NoError(t, err, "output should be valid JSON")

	assert.Contains(t, result, "summary")
	assert.Contains(t, result, "resolvers")
}

func TestReportFromSnapshotDiff(t *testing.T) {
	diff := &resolver.SnapshotDiff{
		Before: &resolver.SnapshotMetadata{Solution: "sol", Version: "1.0.0"},
		After:  &resolver.SnapshotMetadata{Solution: "sol", Version: "2.0.0"},
		Resolvers: map[string]*resolver.ResolverDiff{
			"addr": {Type: resolver.DiffTypeAdded, After: &resolver.SnapshotResolver{Value: "fresh"}},
			"rmr":  {Type: resolver.DiffTypeRemoved, Before: &resolver.SnapshotResolver{Value: "gone"}},
			"modr": {
				Type: resolver.DiffTypeModified,
				Changes: []resolver.FieldChange{
					{Field: "value", Before: "old", After: "new"},
				},
			},
			"modempty": {Type: resolver.DiffTypeModified},
			"unch":     {Type: resolver.DiffTypeUnchanged},
		},
	}

	r := reportFromSnapshotDiff(diff)

	assert.Equal(t, "snapshot", r.Kind)
	assert.Equal(t, "sol@1.0.0", r.LeftRef)
	assert.Equal(t, "sol@2.0.0", r.RightRef)

	byPath := map[string]diffreport.Entry{}
	for _, e := range r.Entries {
		byPath[e.Path] = e
	}

	// Modified resolver expands to one entry per field change, grouped by name.
	modEntry, ok := byPath["value"]
	require.True(t, ok)
	assert.Equal(t, "modr", modEntry.Group)
	assert.Equal(t, diffreport.ChangeModified, modEntry.Kind)
	assert.Equal(t, "old", modEntry.Before)
	assert.Equal(t, "new", modEntry.After)

	// A modified resolver with no field changes still yields one modified entry.
	assert.Equal(t, diffreport.ChangeModified, byPath["modempty"].Kind)

	// Unchanged resolvers are recorded when not ignored.
	assert.Equal(t, diffreport.ChangeUnchanged, byPath["unch"].Kind)
}

func TestSnapshotRef(t *testing.T) {
	assert.Equal(t, "", snapshotRef(nil))
	assert.Equal(t, "sol@1.0.0", snapshotRef(&resolver.SnapshotMetadata{Solution: "sol", Version: "1.0.0"}))
	assert.Equal(t, "sol", snapshotRef(&resolver.SnapshotMetadata{Solution: "sol"}))
}

func TestResolverValue(t *testing.T) {
	assert.Nil(t, resolverValue(nil))
	assert.Equal(t, "v", resolverValue(&resolver.SnapshotResolver{Value: "v"}))
}

func TestCommandDiffSnapshot_Execute(t *testing.T) {
	tmpDir := t.TempDir()
	beforeFile, afterFile := createTestSnapshotPair(t, tmpDir)

	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	cmd := CommandDiffSnapshot(&settings.Run{NoColor: true}, ioStreams, "scafctl")
	cmd.SetArgs([]string{beforeFile, afterFile})
	cmd.SetContext(logger.WithLogger(context.Background(), logger.Get(-1)))

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Diff: test-solution@1.0.0 -> test-solution@1.0.0")
}

// Helper functions
func createTestSnapshotForDiff() *resolver.Snapshot {
	return &resolver.Snapshot{
		Metadata: resolver.SnapshotMetadata{
			Solution:      "test-solution",
			Version:       "1.0.0",
			Timestamp:     time.Now(),
			Runtime:       resolver.SnapshotRuntime{Engine: resolver.SnapshotRuntimeComponent{Name: "scafctl", Version: "dev"}, CLI: resolver.SnapshotRuntimeComponent{Name: "scafctl", Version: "dev"}},
			TotalDuration: "1s",
			Status:        "success",
		},
		Resolvers: map[string]*resolver.SnapshotResolver{
			"test_resolver": {
				Status:        "success",
				Value:         "test-value",
				Phase:         1,
				Duration:      "100ms",
				ProviderCalls: 1,
			},
		},
	}
}

func createTestSnapshotPair(t *testing.T, dir string) (beforeFile, afterFile string) {
	t.Helper()

	beforeFile = filepath.Join(dir, "before.json")
	before := &resolver.Snapshot{
		Metadata: resolver.SnapshotMetadata{
			Solution:      "test-solution",
			Version:       "1.0.0",
			Timestamp:     time.Now().Add(-time.Hour),
			Runtime:       resolver.SnapshotRuntime{Engine: resolver.SnapshotRuntimeComponent{Name: "scafctl", Version: "dev"}, CLI: resolver.SnapshotRuntimeComponent{Name: "scafctl", Version: "dev"}},
			TotalDuration: "1s",
			Status:        "success",
		},
		Resolvers: map[string]*resolver.SnapshotResolver{
			"test_resolver": {
				Status:        "success",
				Value:         "old-value",
				Phase:         1,
				Duration:      "100ms",
				ProviderCalls: 1,
			},
		},
	}
	err := resolver.SaveSnapshot(before, beforeFile)
	require.NoError(t, err)

	afterFile = filepath.Join(dir, "after.json")
	after := &resolver.Snapshot{
		Metadata: resolver.SnapshotMetadata{
			Solution:      "test-solution",
			Version:       "1.0.0",
			Timestamp:     time.Now(),
			Runtime:       resolver.SnapshotRuntime{Engine: resolver.SnapshotRuntimeComponent{Name: "scafctl", Version: "dev"}, CLI: resolver.SnapshotRuntimeComponent{Name: "scafctl", Version: "dev"}},
			TotalDuration: "1s",
			Status:        "success",
		},
		Resolvers: map[string]*resolver.SnapshotResolver{
			"test_resolver": {
				Status:        "success",
				Value:         "new-value",
				Phase:         1,
				Duration:      "100ms",
				ProviderCalls: 1,
			},
		},
	}
	err = resolver.SaveSnapshot(after, afterFile)
	require.NoError(t, err)

	return beforeFile, afterFile
}
