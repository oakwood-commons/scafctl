// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestCommandSnapshot(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}

	cmd := CommandSnapshot(cliParams, ioStreams, "get")

	require.NotNil(t, cmd)
	assert.Equal(t, "snapshot [snapshot-file]", cmd.Use)
	assert.Equal(t, "Display snapshot contents", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)

	// Verify --format is present as a long-only flag.
	formatFlag := cmd.Flags().Lookup("format")
	require.NotNil(t, formatFlag, "format flag should exist")
	assert.Equal(t, "", formatFlag.Shorthand, "format must be long-only")
	assert.Equal(t, "summary", formatFlag.DefValue)

	// -f must NOT be bound (it means --file elsewhere).
	assert.Nil(t, cmd.Flags().ShorthandLookup("f"), "-f shorthand must not be bound")
}

func TestCommandSnapshot_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}

	cmd := CommandSnapshot(cliParams, ioStreams, "get")
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{})
	assert.Error(t, cmd.Execute())

	cmd2 := CommandSnapshot(cliParams, ioStreams, "get")
	cmd2.SilenceErrors = true
	cmd2.SetArgs([]string{"a", "b"})
	assert.Error(t, cmd2.Execute())
}

func TestRunShow_MissingFile(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))
	opts := &ShowOptions{
		SnapshotFile: "/nonexistent/snapshot.json",
		Format:       "summary",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	w := writer.New(ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err := runShow(testCtx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load snapshot")
}

func TestRunShow_InvalidFormat(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	require.NoError(t, resolver.SaveSnapshot(snapshot, snapshotFile))

	opts := &ShowOptions{SnapshotFile: snapshotFile, Format: "invalid-format"}
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	w := writer.New(ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err := runShow(testCtx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestRunShow_Summary(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	require.NoError(t, resolver.SaveSnapshot(snapshot, snapshotFile))

	opts := &ShowOptions{SnapshotFile: snapshotFile, Format: "summary"}
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	w := writer.New(ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	require.NoError(t, runShow(testCtx, opts, ioStreams))
	output := stdout.String()
	assert.Contains(t, output, "Snapshot Summary")
	assert.Contains(t, output, "test-solution")
	assert.Contains(t, output, "1.0.0")
	assert.Contains(t, output, "Resolvers:")
	assert.Contains(t, output, "Success:")
	assert.Contains(t, output, "Failed:")
}

func TestRunShow_SummaryVerbose(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	snapshot.Phases = []resolver.SnapshotPhase{
		{Phase: 1, Duration: "1s", Resolvers: []string{"test_resolver"}},
	}
	snapshot.Parameters = map[string]any{"env": "test", "region": "us-west-2"}
	require.NoError(t, resolver.SaveSnapshot(snapshot, snapshotFile))

	opts := &ShowOptions{SnapshotFile: snapshotFile, Format: "summary", Verbose: true}
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	w := writer.New(ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	require.NoError(t, runShow(testCtx, opts, ioStreams))
	output := stdout.String()
	assert.Contains(t, output, "Phase 1:")
	assert.Contains(t, output, "env:")
	assert.Contains(t, output, "region:")
}

func TestRunShow_JSON(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	require.NoError(t, resolver.SaveSnapshot(snapshot, snapshotFile))

	opts := &ShowOptions{SnapshotFile: snapshotFile, Format: "json"}
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	w := writer.New(ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	require.NoError(t, runShow(testCtx, opts, ioStreams))

	var result map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result), "output should be valid JSON")
	assert.Contains(t, result, "metadata")
	assert.Contains(t, result, "resolvers")
}

func TestRunShow_Resolvers(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	require.NoError(t, resolver.SaveSnapshot(snapshot, snapshotFile))

	opts := &ShowOptions{SnapshotFile: snapshotFile, Format: "resolvers"}
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	w := writer.New(ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	require.NoError(t, runShow(testCtx, opts, ioStreams))
	output := stdout.String()
	assert.Contains(t, output, "Resolvers")
	assert.Contains(t, output, "test_resolver")
	assert.Contains(t, output, "✓")
	assert.Contains(t, output, "Status:")
	assert.Contains(t, output, "Phase:")
}

func TestRunShow_ResolversVerbose(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	snapshot.Resolvers["test_resolver"].ValueSizeBytes = 1024
	snapshot.Resolvers["test_resolver"].Sensitive = true
	require.NoError(t, resolver.SaveSnapshot(snapshot, snapshotFile))

	opts := &ShowOptions{SnapshotFile: snapshotFile, Format: "resolvers", Verbose: true}
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	w := writer.New(ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	require.NoError(t, runShow(testCtx, opts, ioStreams))
	output := stdout.String()
	assert.Contains(t, output, "Value:")
	assert.Contains(t, output, "Value Size:")
	assert.Contains(t, output, "Sensitive:")
}

func TestRunShow_ResolversWithErrors(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	snapshot.Resolvers["failed_resolver"] = &resolver.SnapshotResolver{
		Status:   "failed",
		Phase:    1,
		Duration: "100ms",
		Error:    "provider error: connection timeout",
		FailedAttempts: []resolver.SnapshotFailedAttempt{
			{Provider: "env", Error: "key not found", Duration: "10ms", Timestamp: "2024-01-01T00:00:00Z"},
			{Provider: "ssm", Error: "connection timeout", Duration: "50ms", Timestamp: "2024-01-01T00:00:01Z"},
		},
	}
	require.NoError(t, resolver.SaveSnapshot(snapshot, snapshotFile))

	opts := &ShowOptions{SnapshotFile: snapshotFile, Format: "resolvers", Verbose: true}
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	w := writer.New(ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	require.NoError(t, runShow(testCtx, opts, ioStreams))
	output := stdout.String()
	assert.Contains(t, output, "✗")
	assert.Contains(t, output, "failed_resolver")
	assert.Contains(t, output, "Error:")
	assert.Contains(t, output, "connection timeout")
	assert.Contains(t, output, "Failed Attempts:")
}

func TestRunShow_NilWriterFallback(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	require.NoError(t, resolver.SaveSnapshot(snapshot, snapshotFile))

	opts := &ShowOptions{SnapshotFile: snapshotFile, Format: "summary"}
	var stdout bytes.Buffer
	// No ErrOut, and no writer in context: exercises the nil-Writer fallback.
	ioStreams := &terminal.IOStreams{Out: &stdout}

	require.NoError(t, runShow(ctx, opts, ioStreams))
	assert.Contains(t, stdout.String(), "Snapshot Summary")
}

func TestShowSummary_StatusCounting(t *testing.T) {
	snapshot := createTestSnapshot()
	snapshot.Resolvers["success1"] = &resolver.SnapshotResolver{Status: "success", Phase: 1, Duration: "10ms"}
	snapshot.Resolvers["success2"] = &resolver.SnapshotResolver{Status: "success", Phase: 1, Duration: "10ms"}
	snapshot.Resolvers["failed1"] = &resolver.SnapshotResolver{Status: "failed", Phase: 1, Duration: "10ms"}
	snapshot.Resolvers["skipped1"] = &resolver.SnapshotResolver{Status: "skipped", Phase: 1, Duration: "10ms"}

	var stdout bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout}
	w := writer.New(ioStreams, &settings.Run{})

	require.NoError(t, showSummary(snapshot, &ShowOptions{}, w))
	output := stdout.String()
	assert.Contains(t, output, "Success:       3")
	assert.Contains(t, output, "Failed:        1")
	assert.Contains(t, output, "Skipped:       1")
}

func TestShowResolvers_StatusIcons(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		wantIcon string
	}{
		{name: "success status shows checkmark", status: "success", wantIcon: "✓"},
		{name: "failed status shows X", status: "failed", wantIcon: "✗"},
		{name: "skipped status shows circle", status: "skipped", wantIcon: "○"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := createTestSnapshot()
			snapshot.Resolvers["test"] = &resolver.SnapshotResolver{Status: tt.status, Phase: 1, Duration: "1ms"}

			var stdout bytes.Buffer
			ioStreams := &terminal.IOStreams{Out: &stdout}
			w := writer.New(ioStreams, &settings.Run{})

			require.NoError(t, showResolvers(snapshot, &ShowOptions{}, w))
			assert.Contains(t, stdout.String(), tt.wantIcon)
		})
	}
}

func TestCommandSnapshot_InvalidSnapshotJSON(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "invalid.json")
	require.NoError(t, os.WriteFile(snapshotFile, []byte("{invalid json content"), 0o600))

	opts := &ShowOptions{SnapshotFile: snapshotFile, Format: "summary"}
	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	w := writer.New(ioStreams, &settings.Run{})
	testCtx := writer.WithWriter(ctx, w)

	err := runShow(testCtx, opts, ioStreams)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "failed to load snapshot")
}

// createTestSnapshot builds a minimal snapshot for tests.
func createTestSnapshot() *resolver.Snapshot {
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
		Phases:     []resolver.SnapshotPhase{},
		Parameters: map[string]any{},
	}
}

func BenchmarkCommandSnapshot(b *testing.B) {
	cliParams := &settings.Run{}
	ioStreams := &terminal.IOStreams{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandSnapshot(cliParams, ioStreams, "get")
	}
}
