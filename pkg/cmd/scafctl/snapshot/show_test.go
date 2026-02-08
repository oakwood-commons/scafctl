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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandShow(t *testing.T) {
	cliParams := &settings.Run{}
	ioStreams := terminal.IOStreams{}

	cmd := CommandShow(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "show [snapshot-file]", cmd.Use)
	assert.Equal(t, "Display snapshot contents", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)

	// Verify flags
	formatFlag := cmd.Flags().Lookup("format")
	require.NotNil(t, formatFlag, "format flag should exist")
	assert.Equal(t, "f", formatFlag.Shorthand)
	assert.Equal(t, "summary", formatFlag.DefValue)

	verboseFlag := cmd.Flags().Lookup("verbose")
	require.NotNil(t, verboseFlag, "verbose flag should exist")
	assert.Equal(t, "v", verboseFlag.Shorthand)
}

func TestRunShow_MissingFile(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))
	opts := &ShowOptions{
		SnapshotFile: "/nonexistent/snapshot.json",
		Format:       "summary",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err := runShow(ctx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load snapshot")
}

func TestRunShow_InvalidFormat(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	// Create a test snapshot
	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	err := resolver.SaveSnapshot(snapshot, snapshotFile)
	require.NoError(t, err)

	opts := &ShowOptions{
		SnapshotFile: snapshotFile,
		Format:       "invalid-format",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err = runShow(ctx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestRunShow_Summary(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	// Create a test snapshot
	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	err := resolver.SaveSnapshot(snapshot, snapshotFile)
	require.NoError(t, err)

	opts := &ShowOptions{
		SnapshotFile: snapshotFile,
		Format:       "summary",
		Verbose:      false,
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err = runShow(ctx, opts, ioStreams)

	require.NoError(t, err)
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

	// Create a test snapshot with phases and parameters
	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	snapshot.Phases = []resolver.SnapshotPhase{
		{Phase: 1, Duration: "1s", Resolvers: []string{"test_resolver"}},
	}
	snapshot.Parameters = map[string]any{"env": "test", "region": "us-west-2"}
	err := resolver.SaveSnapshot(snapshot, snapshotFile)
	require.NoError(t, err)

	opts := &ShowOptions{
		SnapshotFile: snapshotFile,
		Format:       "summary",
		Verbose:      true,
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err = runShow(ctx, opts, ioStreams)

	require.NoError(t, err)
	output := stdout.String()
	assert.Contains(t, output, "Phase 1:")
	assert.Contains(t, output, "env:")
	assert.Contains(t, output, "region:")
}

func TestRunShow_JSON(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	// Create a test snapshot
	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	err := resolver.SaveSnapshot(snapshot, snapshotFile)
	require.NoError(t, err)

	opts := &ShowOptions{
		SnapshotFile: snapshotFile,
		Format:       "json",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err = runShow(ctx, opts, ioStreams)

	require.NoError(t, err)

	// Verify output is valid JSON
	var result map[string]any
	err = json.Unmarshal(stdout.Bytes(), &result)
	require.NoError(t, err, "output should be valid JSON")

	// Check for expected fields
	assert.Contains(t, result, "metadata")
	assert.Contains(t, result, "resolvers")
}

func TestRunShow_Resolvers(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	// Create a test snapshot
	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	err := resolver.SaveSnapshot(snapshot, snapshotFile)
	require.NoError(t, err)

	opts := &ShowOptions{
		SnapshotFile: snapshotFile,
		Format:       "resolvers",
		Verbose:      false,
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err = runShow(ctx, opts, ioStreams)

	require.NoError(t, err)
	output := stdout.String()
	assert.Contains(t, output, "Resolvers")
	assert.Contains(t, output, "test_resolver")
	assert.Contains(t, output, "✓") // Success icon
	assert.Contains(t, output, "Status:")
	assert.Contains(t, output, "Phase:")
}

func TestRunShow_ResolversVerbose(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	// Create a test snapshot with detailed resolver info
	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := createTestSnapshot()
	snapshot.Resolvers["test_resolver"].ValueSizeBytes = 1024
	snapshot.Resolvers["test_resolver"].Sensitive = true
	err := resolver.SaveSnapshot(snapshot, snapshotFile)
	require.NoError(t, err)

	opts := &ShowOptions{
		SnapshotFile: snapshotFile,
		Format:       "resolvers",
		Verbose:      true,
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err = runShow(ctx, opts, ioStreams)

	require.NoError(t, err)
	output := stdout.String()
	assert.Contains(t, output, "Value:")
	assert.Contains(t, output, "Value Size:")
	assert.Contains(t, output, "Sensitive:")
}

func TestRunShow_ResolversWithErrors(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	// Create a test snapshot with failed resolver
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
	err := resolver.SaveSnapshot(snapshot, snapshotFile)
	require.NoError(t, err)

	opts := &ShowOptions{
		SnapshotFile: snapshotFile,
		Format:       "resolvers",
		Verbose:      true,
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err = runShow(ctx, opts, ioStreams)

	require.NoError(t, err)
	output := stdout.String()
	assert.Contains(t, output, "✗") // Failed icon
	assert.Contains(t, output, "failed_resolver")
	assert.Contains(t, output, "Error:")
	assert.Contains(t, output, "connection timeout")
	assert.Contains(t, output, "Failed Attempts:")
}

func TestShowSummary_StatusCounting(t *testing.T) {
	snapshot := createTestSnapshot()
	snapshot.Resolvers["success1"] = &resolver.SnapshotResolver{Status: "success", Phase: 1, Duration: "10ms"}
	snapshot.Resolvers["success2"] = &resolver.SnapshotResolver{Status: "success", Phase: 1, Duration: "10ms"}
	snapshot.Resolvers["failed1"] = &resolver.SnapshotResolver{Status: "failed", Phase: 1, Duration: "10ms"}
	snapshot.Resolvers["skipped1"] = &resolver.SnapshotResolver{Status: "skipped", Phase: 1, Duration: "10ms"}

	var stdout bytes.Buffer
	ioStreams := terminal.IOStreams{Out: &stdout}

	err := showSummary(snapshot, &ShowOptions{}, ioStreams)

	require.NoError(t, err)
	output := stdout.String()

	// Check counts
	assert.Contains(t, output, "Success:       3") // 2 + original test_resolver
	assert.Contains(t, output, "Failed:        1")
	assert.Contains(t, output, "Skipped:       1")
}

// Helper function to create a test snapshot
func createTestSnapshot() *resolver.Snapshot {
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
		Phases:     []resolver.SnapshotPhase{},
		Parameters: map[string]any{},
	}
}

func TestShowResolvers_StatusIcons(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		wantIcon   string
		wantInText bool
	}{
		{
			name:       "success status shows checkmark",
			status:     "success",
			wantIcon:   "✓",
			wantInText: true,
		},
		{
			name:       "failed status shows X",
			status:     "failed",
			wantIcon:   "✗",
			wantInText: true,
		},
		{
			name:       "skipped status shows circle",
			status:     "skipped",
			wantIcon:   "○",
			wantInText: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := createTestSnapshot()
			snapshot.Resolvers["test"] = &resolver.SnapshotResolver{
				Status:   tt.status,
				Phase:    1,
				Duration: "1ms",
			}

			var stdout bytes.Buffer
			ioStreams := terminal.IOStreams{Out: &stdout}

			err := showResolvers(snapshot, &ShowOptions{}, ioStreams)
			require.NoError(t, err)

			output := stdout.String()
			if tt.wantInText {
				assert.Contains(t, output, tt.wantIcon)
			} else {
				assert.NotContains(t, output, tt.wantIcon)
			}
		})
	}
}

func TestRunShow_Integration(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := logger.WithLogger(context.Background(), logger.Get(-1))
	tmpDir := t.TempDir()

	// Create a realistic snapshot
	snapshotFile := filepath.Join(tmpDir, "snapshot.json")
	snapshot := &resolver.Snapshot{
		Metadata: resolver.SnapshotMetadata{
			Solution:       "my-app",
			Version:        "2.3.1",
			Timestamp:      time.Now(),
			ScafctlVersion: "0.1.0",
			TotalDuration:  "5s",
			Status:         "success",
		},
		Resolvers: map[string]*resolver.SnapshotResolver{
			"db_password": {
				Status:         "success",
				Value:          "<redacted>",
				Sensitive:      true,
				Phase:          1,
				Duration:       "250ms",
				ProviderCalls:  2,
				ValueSizeBytes: 32,
			},
			"api_url": {
				Status:        "success",
				Value:         "https://api.example.com",
				Phase:         1,
				Duration:      "50ms",
				ProviderCalls: 1,
			},
			"feature_flag": {
				Status:   "failed",
				Phase:    2,
				Duration: "1s",
				Error:    "provider not available",
				FailedAttempts: []resolver.SnapshotFailedAttempt{
					{Provider: "ssm", Error: "parameter not found", Duration: "500ms", Timestamp: "2024-01-01T00:00:00Z"},
				},
			},
		},
		Phases: []resolver.SnapshotPhase{
			{Phase: 1, Duration: "3s", Resolvers: []string{"db_password", "api_url"}},
			{Phase: 2, Duration: "2s", Resolvers: []string{"feature_flag"}},
		},
		Parameters: map[string]any{
			"environment": "production",
			"region":      "us-east-1",
		},
	}
	err := resolver.SaveSnapshot(snapshot, snapshotFile)
	require.NoError(t, err)

	tests := []struct {
		name           string
		format         string
		verbose        bool
		wantSubstrings []string
	}{
		{
			name:    "summary format basic",
			format:  "summary",
			verbose: false,
			wantSubstrings: []string{
				"Snapshot Summary",
				"my-app",
				"2.3.1",
				"Success:       2",
				"Failed:        1",
			},
		},
		{
			name:    "summary format verbose",
			format:  "summary",
			verbose: true,
			wantSubstrings: []string{
				"Phase 1:",
				"Phase 2:",
				"environment:",
				"production",
				"region:",
			},
		},
		{
			name:    "resolvers format",
			format:  "resolvers",
			verbose: false,
			wantSubstrings: []string{
				"db_password",
				"api_url",
				"feature_flag",
				"✓",
				"✗",
			},
		},
		{
			name:    "resolvers format verbose",
			format:  "resolvers",
			verbose: true,
			wantSubstrings: []string{
				"Value:",
				"<redacted>",
				"https://api.example.com",
				"Sensitive:     yes",
				"Failed Attempts:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &ShowOptions{
				SnapshotFile: snapshotFile,
				Format:       tt.format,
				Verbose:      tt.verbose,
			}
			var stdout bytes.Buffer
			ioStreams := terminal.IOStreams{Out: &stdout}

			err := runShow(ctx, opts, ioStreams)
			require.NoError(t, err)

			output := stdout.String()
			for _, want := range tt.wantSubstrings {
				assert.Contains(t, output, want, "output should contain: %s", want)
			}
		})
	}

	// Test JSON format separately
	t.Run("json format", func(t *testing.T) {
		opts := &ShowOptions{
			SnapshotFile: snapshotFile,
			Format:       "json",
		}
		var stdout bytes.Buffer
		ioStreams := terminal.IOStreams{Out: &stdout}

		err := runShow(ctx, opts, ioStreams)
		require.NoError(t, err)

		// Parse and verify JSON structure
		var result map[string]any
		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		metadata := result["metadata"].(map[string]any)
		assert.Equal(t, "my-app", metadata["solution"])
		assert.Equal(t, "2.3.1", metadata["version"])

		resolversMap := result["resolvers"].(map[string]any)
		assert.Len(t, resolversMap, 3)
	})
}

func TestCommandShow_InvalidSnapshotJSON(t *testing.T) {
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))

	// Create temp file with invalid JSON
	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "invalid.json")
	err := os.WriteFile(snapshotFile, []byte("{invalid json content"), 0o600)
	require.NoError(t, err)

	opts := &ShowOptions{
		SnapshotFile: snapshotFile,
		Format:       "summary",
	}
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	}

	err = runShow(ctx, opts, ioStreams)

	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "failed to load snapshot")
}
