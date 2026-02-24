// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTestSnapshot writes a resolver.Snapshot to a JSON file and returns the path.
func writeTestSnapshot(t *testing.T, dir, name string, snap *resolver.Snapshot) string {
	t.Helper()
	path := filepath.Join(dir, name)
	data, err := json.MarshalIndent(snap, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func TestHandleShowSnapshot(t *testing.T) {
	baseSnap := &resolver.Snapshot{
		Metadata: resolver.SnapshotMetadata{
			Solution:       "my-solution",
			Version:        "1.0.0",
			Timestamp:      time.Date(2026, 2, 20, 15, 30, 0, 0, time.UTC),
			ScafctlVersion: "0.15.0",
			TotalDuration:  "2.3s",
			Status:         "success",
		},
		Parameters: map[string]any{"env": "prod", "region": "us-east1"},
		Resolvers: map[string]*resolver.SnapshotResolver{
			"config": {
				Value:         map[string]any{"host": "api.example.com"},
				Status:        "success",
				Phase:         1,
				Duration:      "150ms",
				ProviderCalls: 1,
			},
			"deploy": {
				Value:         "ok",
				Status:        "success",
				Phase:         2,
				Duration:      "1.2s",
				ProviderCalls: 1,
			},
			"validate": {
				Status:        "failed",
				Phase:         2,
				Duration:      "50ms",
				ProviderCalls: 1,
				Error:         "validation failed: invalid region",
			},
		},
		Phases: []resolver.SnapshotPhase{
			{Phase: 1, Duration: "200ms", Resolvers: []string{"config"}},
			{Phase: 2, Duration: "1.5s", Resolvers: []string{"deploy", "validate"}},
		},
	}

	t.Run("summary format", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		tmpDir := t.TempDir()
		snapPath := writeTestSnapshot(t, tmpDir, "snap.json", baseSnap)

		request := mcp.CallToolRequest{}
		request.Params.Name = "show_snapshot"
		request.Params.Arguments = map[string]any{
			"path": snapPath,
		}

		result, err := srv.handleShowSnapshot(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		assert.Equal(t, "my-solution", data["solution"])
		assert.Equal(t, "1.0.0", data["version"])
		assert.Equal(t, "success", data["status"])
		assert.Equal(t, "2.3s", data["duration"])
		assert.Equal(t, float64(2), data["phases"])

		counts := data["resolverCount"].(map[string]any)
		assert.Equal(t, float64(3), counts["total"])
		assert.Equal(t, float64(2), counts["success"])
		assert.Equal(t, float64(1), counts["failed"])

		// summary format should NOT include resolvers list
		_, hasResolvers := data["resolvers"]
		assert.False(t, hasResolvers)
	})

	t.Run("resolvers format", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		tmpDir := t.TempDir()
		snapPath := writeTestSnapshot(t, tmpDir, "snap.json", baseSnap)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"path":   snapPath,
			"format": "resolvers",
		}

		result, err := srv.handleShowSnapshot(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		resolvers, ok := data["resolvers"].([]any)
		require.True(t, ok)
		assert.Len(t, resolvers, 3)

		// resolvers format should include name, status, duration, phase but NOT value
		for _, r := range resolvers {
			entry := r.(map[string]any)
			assert.Contains(t, entry, "name")
			assert.Contains(t, entry, "status")
			assert.Contains(t, entry, "duration")
			assert.Contains(t, entry, "phase")
			_, hasValue := entry["value"]
			assert.False(t, hasValue, "resolvers format should not include value")
		}
	})

	t.Run("full format includes values", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		tmpDir := t.TempDir()
		snapPath := writeTestSnapshot(t, tmpDir, "snap.json", baseSnap)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"path":   snapPath,
			"format": "full",
		}

		result, err := srv.handleShowSnapshot(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		resolvers := data["resolvers"].([]any)
		assert.Len(t, resolvers, 3)

		// full format should include value and error fields
		foundConfig := false
		foundValidate := false
		for _, r := range resolvers {
			entry := r.(map[string]any)
			name := entry["name"].(string)
			if name == "config" {
				foundConfig = true
				assert.NotNil(t, entry["value"])
			}
			if name == "validate" {
				foundValidate = true
				assert.Equal(t, "validation failed: invalid region", entry["error"])
			}
		}
		assert.True(t, foundConfig, "should have config resolver")
		assert.True(t, foundValidate, "should have validate resolver")
	})

	t.Run("missing path returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleShowSnapshot(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"path": "/nonexistent/snapshot.json",
		}

		result, err := srv.handleShowSnapshot(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

func TestHandleDiffSnapshots(t *testing.T) {
	beforeSnap := &resolver.Snapshot{
		Metadata: resolver.SnapshotMetadata{
			Solution:  "my-solution",
			Timestamp: time.Date(2026, 2, 20, 15, 0, 0, 0, time.UTC),
			Status:    "success",
		},
		Resolvers: map[string]*resolver.SnapshotResolver{
			"config": {
				Value:    map[string]any{"host": "old.example.com"},
				Status:   "success",
				Phase:    1,
				Duration: "100ms",
			},
			"api-call": {
				Value:    "ok",
				Status:   "success",
				Phase:    2,
				Duration: "500ms",
			},
			"old-resolver": {
				Value:    "removed",
				Status:   "success",
				Phase:    1,
				Duration: "50ms",
			},
		},
		Phases: []resolver.SnapshotPhase{
			{Phase: 1, Duration: "150ms", Resolvers: []string{"config", "old-resolver"}},
			{Phase: 2, Duration: "500ms", Resolvers: []string{"api-call"}},
		},
	}

	afterSnap := &resolver.Snapshot{
		Metadata: resolver.SnapshotMetadata{
			Solution:  "my-solution",
			Timestamp: time.Date(2026, 2, 20, 16, 0, 0, 0, time.UTC),
			Status:    "success",
		},
		Resolvers: map[string]*resolver.SnapshotResolver{
			"config": {
				Value:    map[string]any{"host": "new.example.com"},
				Status:   "success",
				Phase:    1,
				Duration: "120ms",
			},
			"api-call": {
				Value:    "",
				Status:   "failed",
				Phase:    2,
				Duration: "2s",
				Error:    "connection refused",
			},
			"new-resolver": {
				Value:    "added",
				Status:   "success",
				Phase:    1,
				Duration: "30ms",
			},
		},
		Phases: []resolver.SnapshotPhase{
			{Phase: 1, Duration: "160ms", Resolvers: []string{"config", "new-resolver"}},
			{Phase: 2, Duration: "2s", Resolvers: []string{"api-call"}},
		},
	}

	t.Run("detects changes between snapshots", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		tmpDir := t.TempDir()
		beforePath := writeTestSnapshot(t, tmpDir, "before.json", beforeSnap)
		afterPath := writeTestSnapshot(t, tmpDir, "after.json", afterSnap)

		request := mcp.CallToolRequest{}
		request.Params.Name = "diff_snapshots"
		request.Params.Arguments = map[string]any{
			"before": beforePath,
			"after":  afterPath,
		}

		result, err := srv.handleDiffSnapshots(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		// Check before/after metadata
		before := data["before"].(map[string]any)
		assert.Equal(t, "my-solution", before["solution"])

		after := data["after"].(map[string]any)
		assert.Equal(t, "my-solution", after["solution"])

		// Check summary
		summary := data["summary"].(map[string]any)
		assert.Equal(t, float64(1), summary["added"])
		assert.Equal(t, float64(1), summary["removed"])

		// Check changes structure exists
		changes := data["changes"].(map[string]any)
		assert.Contains(t, changes, "added")
		assert.Contains(t, changes, "removed")
		assert.Contains(t, changes, "changed")
		assert.Contains(t, changes, "statusChanges")
	})

	t.Run("ignore_unchanged defaults to true", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		tmpDir := t.TempDir()
		beforePath := writeTestSnapshot(t, tmpDir, "before.json", beforeSnap)
		afterPath := writeTestSnapshot(t, tmpDir, "after.json", afterSnap)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"before":           beforePath,
			"after":            afterPath,
			"ignore_unchanged": true,
		}

		result, err := srv.handleDiffSnapshots(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
	})

	t.Run("missing before returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"after": "/some/path",
		}

		result, err := srv.handleDiffSnapshots(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("missing after returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"before": "/some/path",
		}

		result, err := srv.handleDiffSnapshots(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("nonexistent before file returns error", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		tmpDir := t.TempDir()
		afterPath := writeTestSnapshot(t, tmpDir, "after.json", afterSnap)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]any{
			"before": "/nonexistent.json",
			"after":  afterPath,
		}

		result, err := srv.handleDiffSnapshots(context.Background(), request)
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

func TestHandleAnalyzeExecutionPrompt(t *testing.T) {
	t.Run("with snapshot only", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.GetPromptRequest{}
		request.Params.Name = "analyze_execution"
		request.Params.Arguments = map[string]string{
			"snapshot_path": "/tmp/snapshot.json",
			"problem":       "resolvers timing out",
		}

		result, err := srv.handleAnalyzeExecutionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Contains(t, result.Description, "Analyze execution")
		require.Len(t, result.Messages, 1)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "/tmp/snapshot.json")
		assert.Contains(t, text, "resolvers timing out")
		assert.Contains(t, text, "show_snapshot")
		assert.Contains(t, text, "No previous snapshot provided")
	})

	t.Run("with previous snapshot for comparison", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.GetPromptRequest{}
		request.Params.Name = "analyze_execution"
		request.Params.Arguments = map[string]string{
			"snapshot_path":     "/tmp/bad-snapshot.json",
			"previous_snapshot": "/tmp/good-snapshot.json",
		}

		result, err := srv.handleAnalyzeExecutionPrompt(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "diff_snapshots")
		assert.Contains(t, text, "/tmp/good-snapshot.json")
		assert.Contains(t, text, "/tmp/bad-snapshot.json")
		assert.NotContains(t, text, "No previous snapshot provided")
	})

	t.Run("default problem description", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.GetPromptRequest{}
		request.Params.Arguments = map[string]string{
			"snapshot_path": "/tmp/snapshot.json",
		}

		result, err := srv.handleAnalyzeExecutionPrompt(context.Background(), request)
		require.NoError(t, err)

		text := result.Messages[0].Content.(mcp.TextContent).Text
		assert.Contains(t, text, "did not produce the expected results")
	})
}
