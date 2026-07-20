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

	// Verify flags: format is exposed via -o/--output (house convention);
	// -f is never used for format (it means file elsewhere in the CLI).
	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag, "output flag should exist")
	assert.Equal(t, "o", outputFlag.Shorthand)
	assert.Equal(t, "human", outputFlag.DefValue)

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
		Format:     "human",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}
	w := writer.New(&ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err = runSnapshotDiff(testCtx, opts, ioStreams)

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
		Format:     "human",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}
	w := writer.New(&ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err = runSnapshotDiff(testCtx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load after snapshot")
}

func TestRunSnapshotDiff_InvalidFormat(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	beforeFile, afterFile := createTestSnapshotPair(t, tmpDir)

	opts := &SnapshotDiffOptions{
		BeforeFile: beforeFile,
		AfterFile:  afterFile,
		Format:     "invalid-format",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}
	w := writer.New(&ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err := runSnapshotDiff(testCtx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestRunSnapshotDiff_HumanFormat(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	beforeFile, afterFile := createTestSnapshotPair(t, tmpDir)

	opts := &SnapshotDiffOptions{
		BeforeFile: beforeFile,
		AfterFile:  afterFile,
		Format:     "human",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}
	w := writer.New(&ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err := runSnapshotDiff(testCtx, opts, ioStreams)

	require.NoError(t, err)
	output := stdout.String()
	assert.Contains(t, output, "Snapshot Comparison")
	assert.Contains(t, output, "Summary")
}

func TestRunSnapshotDiff_JSONFormat(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	beforeFile, afterFile := createTestSnapshotPair(t, tmpDir)

	opts := &SnapshotDiffOptions{
		BeforeFile: beforeFile,
		AfterFile:  afterFile,
		Format:     "json",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}
	w := writer.New(&ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err := runSnapshotDiff(testCtx, opts, ioStreams)

	require.NoError(t, err)

	// Verify output is valid JSON
	var result map[string]any
	err = json.Unmarshal(stdout.Bytes(), &result)
	require.NoError(t, err, "output should be valid JSON")

	// Check for expected fields
	assert.Contains(t, result, "summary")
	assert.Contains(t, result, "resolvers")
}

func TestRunSnapshotDiff_UnifiedFormat(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	beforeFile, afterFile := createTestSnapshotPair(t, tmpDir)

	opts := &SnapshotDiffOptions{
		BeforeFile: beforeFile,
		AfterFile:  afterFile,
		Format:     "unified",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}
	w := writer.New(&ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err := runSnapshotDiff(testCtx, opts, ioStreams)

	require.NoError(t, err)
	output := stdout.String()
	assert.Contains(t, output, "---")
	assert.Contains(t, output, "+++")
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
