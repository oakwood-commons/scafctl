// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedTestState(t *testing.T, path string) {
	t.Helper()
	sd := state.NewData()
	sd.Values["env"] = &state.Entry{Value: "prod", Type: "string", UpdatedAt: time.Now().UTC()}
	sd.Values["count"] = &state.Entry{Value: float64(42), Type: "int", UpdatedAt: time.Now().UTC()}
	require.NoError(t, state.SaveToFile(path, sd))
}

func newStateRequest(name string, args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	return req
}

func TestHandleStateList(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("with entries", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		seedTestState(t, path)

		result, err := srv.handleStateList(context.Background(), newStateRequest("state_list", map[string]any{
			"path": path,
		}))
		require.NoError(t, err)
		assert.False(t, result.IsError)

		var output map[string]any
		text := result.Content[0].(mcp.TextContent).Text
		require.NoError(t, json.Unmarshal([]byte(text), &output))

		assert.Equal(t, float64(2), output["count"])
		entries := output["entries"].([]any)
		assert.Len(t, entries, 2)
	})

	t.Run("empty state", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.json")

		result, err := srv.handleStateList(context.Background(), newStateRequest("state_list", map[string]any{
			"path": path,
		}))
		require.NoError(t, err)
		assert.False(t, result.IsError)

		var output map[string]any
		text := result.Content[0].(mcp.TextContent).Text
		require.NoError(t, json.Unmarshal([]byte(text), &output))

		assert.Equal(t, float64(0), output["count"])
	})

	t.Run("missing path", func(t *testing.T) {
		result, err := srv.handleStateList(context.Background(), newStateRequest("state_list", map[string]any{}))
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

func TestHandleStateGet(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("existing key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		seedTestState(t, path)

		result, err := srv.handleStateGet(context.Background(), newStateRequest("state_get", map[string]any{
			"path": path,
			"key":  "env",
		}))
		require.NoError(t, err)
		assert.False(t, result.IsError)

		var output map[string]any
		text := result.Content[0].(mcp.TextContent).Text
		require.NoError(t, json.Unmarshal([]byte(text), &output))

		assert.Equal(t, "env", output["key"])
		entry := output["entry"].(map[string]any)
		assert.Equal(t, "prod", entry["value"])
	})

	t.Run("missing key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		seedTestState(t, path)

		result, err := srv.handleStateGet(context.Background(), newStateRequest("state_get", map[string]any{
			"path": path,
			"key":  "nonexistent",
		}))
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("missing path", func(t *testing.T) {
		result, err := srv.handleStateGet(context.Background(), newStateRequest("state_get", map[string]any{
			"key": "env",
		}))
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("missing key param", func(t *testing.T) {
		result, err := srv.handleStateGet(context.Background(), newStateRequest("state_get", map[string]any{
			"path": "/tmp/foo.json",
		}))
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

func TestHandleStateDelete(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("delete single key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		seedTestState(t, path)

		result, err := srv.handleStateDelete(context.Background(), newStateRequest("state_delete", map[string]any{
			"path": path,
			"key":  "env",
		}))
		require.NoError(t, err)
		assert.False(t, result.IsError)

		// Verify key was deleted
		sd, loadErr := state.LoadFromFile(path)
		require.NoError(t, loadErr)
		assert.NotContains(t, sd.Values, "env")
		assert.Contains(t, sd.Values, "count")
	})

	t.Run("delete nonexistent key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		seedTestState(t, path)

		result, err := srv.handleStateDelete(context.Background(), newStateRequest("state_delete", map[string]any{
			"path": path,
			"key":  "nope",
		}))
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("clear all", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		seedTestState(t, path)

		result, err := srv.handleStateDelete(context.Background(), newStateRequest("state_delete", map[string]any{
			"path": path,
		}))
		require.NoError(t, err)
		assert.False(t, result.IsError)

		var output map[string]any
		text := result.Content[0].(mcp.TextContent).Text
		require.NoError(t, json.Unmarshal([]byte(text), &output))
		assert.Contains(t, output["message"], "cleared 2 entries")

		// Verify all entries gone
		sd, loadErr := state.LoadFromFile(path)
		require.NoError(t, loadErr)
		assert.Empty(t, sd.Values)
	})

	t.Run("missing path", func(t *testing.T) {
		result, err := srv.handleStateDelete(context.Background(), newStateRequest("state_delete", map[string]any{}))
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}
