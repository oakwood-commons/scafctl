package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandDiff(t *testing.T) {
	cliParams := &settings.Run{}
	ioStreams := terminal.IOStreams{}

	cmd := CommandDiff(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "diff [before-snapshot] [after-snapshot]", cmd.Use)
	assert.Equal(t, "Compare two snapshots", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)

	// Verify flags
	formatFlag := cmd.Flags().Lookup("format")
	require.NotNil(t, formatFlag, "format flag should exist")
	assert.Equal(t, "f", formatFlag.Shorthand)
	assert.Equal(t, "human", formatFlag.DefValue)

	ignoreUnchangedFlag := cmd.Flags().Lookup("ignore-unchanged")
	require.NotNil(t, ignoreUnchangedFlag, "ignore-unchanged flag should exist")

	ignoreFieldsFlag := cmd.Flags().Lookup("ignore-fields")
	require.NotNil(t, ignoreFieldsFlag, "ignore-fields flag should exist")

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag, "output flag should exist")
	assert.Equal(t, "o", outputFlag.Shorthand)
}

func TestRunDiff_MissingBeforeFile(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	afterFile := filepath.Join(tmpDir, "after.json")
	snapshot := createTestSnapshotForDiff()
	err := resolver.SaveSnapshot(snapshot, afterFile)
	require.NoError(t, err)

	opts := &DiffOptions{
		BeforeFile: "/nonexistent/before.json",
		AfterFile:  afterFile,
		Format:     "human",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err = runDiff(ctx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load before snapshot")
}

func TestRunDiff_MissingAfterFile(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	beforeFile := filepath.Join(tmpDir, "before.json")
	snapshot := createTestSnapshotForDiff()
	err := resolver.SaveSnapshot(snapshot, beforeFile)
	require.NoError(t, err)

	opts := &DiffOptions{
		BeforeFile: beforeFile,
		AfterFile:  "/nonexistent/after.json",
		Format:     "human",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err = runDiff(ctx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load after snapshot")
}

func TestRunDiff_InvalidFormat(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	beforeFile, afterFile := createTestSnapshotPair(t, tmpDir)

	opts := &DiffOptions{
		BeforeFile: beforeFile,
		AfterFile:  afterFile,
		Format:     "invalid-format",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err := runDiff(ctx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestRunDiff_HumanFormat(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	beforeFile, afterFile := createTestSnapshotPair(t, tmpDir)

	opts := &DiffOptions{
		BeforeFile: beforeFile,
		AfterFile:  afterFile,
		Format:     "human",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err := runDiff(ctx, opts, ioStreams)

	require.NoError(t, err)
	output := stdout.String()
	assert.Contains(t, output, "Snapshot Comparison")
	assert.Contains(t, output, "Summary")
}

func TestRunDiff_JSONFormat(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	beforeFile, afterFile := createTestSnapshotPair(t, tmpDir)

	opts := &DiffOptions{
		BeforeFile: beforeFile,
		AfterFile:  afterFile,
		Format:     "json",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err := runDiff(ctx, opts, ioStreams)

	require.NoError(t, err)

	// Verify output is valid JSON
	var result map[string]any
	err = json.Unmarshal(stdout.Bytes(), &result)
	require.NoError(t, err, "output should be valid JSON")

	// Check for expected fields
	assert.Contains(t, result, "summary")
	assert.Contains(t, result, "resolvers")
}

func TestRunDiff_UnifiedFormat(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	beforeFile, afterFile := createTestSnapshotPair(t, tmpDir)

	opts := &DiffOptions{
		BeforeFile: beforeFile,
		AfterFile:  afterFile,
		Format:     "unified",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err := runDiff(ctx, opts, ioStreams)

	require.NoError(t, err)
	output := stdout.String()
	assert.Contains(t, output, "---")
	assert.Contains(t, output, "+++")
}

func TestRunDiff_OutputToFile(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	beforeFile, afterFile := createTestSnapshotPair(t, tmpDir)
	outputFile := filepath.Join(tmpDir, "diff.txt")

	opts := &DiffOptions{
		BeforeFile: beforeFile,
		AfterFile:  afterFile,
		Format:     "human",
		Output:     outputFile,
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err := runDiff(ctx, opts, ioStreams)

	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(outputFile)
	assert.NoError(t, err, "output file should be created")

	// Verify content was written
	content, err := os.ReadFile(outputFile)
	require.NoError(t, err)
	assert.NotEmpty(t, content)
	assert.Contains(t, string(content), "Snapshot Comparison")

	// Verify summary was written to stderr
	stderrOutput := stderr.String()
	assert.Contains(t, stderrOutput, "Diff saved to")
	assert.Contains(t, stderrOutput, "Total:")
	assert.Contains(t, stderrOutput, "Added:")
}

// Helper functions
func createTestSnapshotForDiff() *resolver.Snapshot {
	return &resolver.Snapshot{
		Metadata: resolver.SnapshotMetadata{
			Solution:       "test-solution",
			Version:        "1.0.0",
			Timestamp:      time.Now(),
			ScafctlVersion: "dev",
			TotalDuration:  "1s",
			Status:         "success",
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
			Solution:       "test-solution",
			Version:        "1.0.0",
			Timestamp:      time.Now().Add(-time.Hour),
			ScafctlVersion: "dev",
			TotalDuration:  "1s",
			Status:         "success",
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
			Solution:       "test-solution",
			Version:        "1.0.0",
			Timestamp:      time.Now(),
			ScafctlVersion: "dev",
			TotalDuration:  "1s",
			Status:         "success",
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
