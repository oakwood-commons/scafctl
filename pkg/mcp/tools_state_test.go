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

func TestHandleStateSet(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	t.Run("set new key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		seedTestState(t, path)

		result, err := srv.handleStateSet(context.Background(), newStateRequest("state_set", map[string]any{
			"path":  path,
			"key":   "region",
			"value": "us-east-1",
		}))
		require.NoError(t, err)
		assert.False(t, result.IsError)

		sd, loadErr := state.LoadFromFile(path)
		require.NoError(t, loadErr)
		require.Contains(t, sd.Values, "region")
		assert.Equal(t, "us-east-1", sd.Values["region"].Value)
		assert.Equal(t, "string", sd.Values["region"].Type)
	})

	t.Run("update existing key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		seedTestState(t, path)

		result, err := srv.handleStateSet(context.Background(), newStateRequest("state_set", map[string]any{
			"path":  path,
			"key":   "env",
			"value": "staging",
		}))
		require.NoError(t, err)
		assert.False(t, result.IsError)

		sd, loadErr := state.LoadFromFile(path)
		require.NoError(t, loadErr)
		assert.Equal(t, "staging", sd.Values["env"].Value)
	})

	t.Run("immutable key blocked", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		sd := state.NewData()
		sd.Values["locked"] = &state.Entry{Value: "v1", Type: "string", Immutable: true, UpdatedAt: time.Now().UTC()}
		require.NoError(t, state.SaveToFile(path, sd))

		result, err := srv.handleStateSet(context.Background(), newStateRequest("state_set", map[string]any{
			"path":  path,
			"key":   "locked",
			"value": "v2",
		}))
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("type coercion int", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		seedTestState(t, path)

		result, err := srv.handleStateSet(context.Background(), newStateRequest("state_set", map[string]any{
			"path":  path,
			"key":   "port",
			"value": "8080",
			"type":  "int",
		}))
		require.NoError(t, err)
		assert.False(t, result.IsError)

		sd, loadErr := state.LoadFromFile(path)
		require.NoError(t, loadErr)
		// JSON roundtrip converts int64 to float64
		assert.Equal(t, float64(8080), sd.Values["port"].Value)
		assert.Equal(t, "int", sd.Values["port"].Type)
	})

	t.Run("type coercion bool", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		seedTestState(t, path)

		result, err := srv.handleStateSet(context.Background(), newStateRequest("state_set", map[string]any{
			"path":  path,
			"key":   "debug",
			"value": "true",
			"type":  "bool",
		}))
		require.NoError(t, err)
		assert.False(t, result.IsError)

		sd, loadErr := state.LoadFromFile(path)
		require.NoError(t, loadErr)
		assert.Equal(t, true, sd.Values["debug"].Value)
	})

	t.Run("missing path", func(t *testing.T) {
		result, err := srv.handleStateSet(context.Background(), newStateRequest("state_set", map[string]any{
			"key":   "foo",
			"value": "bar",
		}))
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})

	t.Run("missing key", func(t *testing.T) {
		result, err := srv.handleStateSet(context.Background(), newStateRequest("state_set", map[string]any{
			"path":  "/tmp/foo.json",
			"value": "bar",
		}))
		require.NoError(t, err)
		assert.True(t, result.IsError)
	})
}

func TestCoerceStateValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		typ  string
		want any
	}{
		{"string default", "hello", "string", "hello"},
		{"int valid", "42", "int", int64(42)},
		{"int invalid falls back", "abc", "int", "abc"},
		{"float valid", "3.14", "float", 3.14},
		{"float invalid falls back", "xyz", "float", "xyz"},
		{"bool true", "true", "bool", true},
		{"bool false", "false", "bool", false},
		{"bool invalid falls back", "maybe", "bool", "maybe"},
		{"unknown type returns string", "val", "unknown", "val"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, coerceStateValue(tc.raw, tc.typ))
		})
	}
}
