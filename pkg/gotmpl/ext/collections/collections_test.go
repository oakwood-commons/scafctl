// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package collections

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWhere(t *testing.T) {
	items := []any{
		map[string]any{"name": "alice", "status": "active"},
		map[string]any{"name": "bob", "status": "inactive"},
		map[string]any{"name": "carol", "status": "active"},
	}

	t.Run("filter by string value", func(t *testing.T) {
		got, err := Where("status", "active", items)
		require.NoError(t, err)
		assert.Equal(t, []any{
			map[string]any{"name": "alice", "status": "active"},
			map[string]any{"name": "carol", "status": "active"},
		}, got)
	})

	t.Run("no matches", func(t *testing.T) {
		got, err := Where("status", "deleted", items)
		require.NoError(t, err)
		assert.Equal(t, []any{}, got)
	})

	t.Run("filter by boolean", func(t *testing.T) {
		list := []any{
			map[string]any{"name": "a", "enabled": true},
			map[string]any{"name": "b", "enabled": false},
		}
		got, err := Where("enabled", true, list)
		require.NoError(t, err)
		assert.Equal(t, []any{
			map[string]any{"name": "a", "enabled": true},
		}, got)
	})

	t.Run("nil list", func(t *testing.T) {
		got, err := Where("key", "val", nil)
		require.NoError(t, err)
		assert.Equal(t, []any{}, got)
	})

	t.Run("empty list", func(t *testing.T) {
		got, err := Where("key", "val", []any{})
		require.NoError(t, err)
		assert.Equal(t, []any{}, got)
	})

	t.Run("missing key in some entries", func(t *testing.T) {
		list := []any{
			map[string]any{"name": "alice", "status": "active"},
			map[string]any{"name": "bob"},
		}
		got, err := Where("status", "active", list)
		require.NoError(t, err)
		assert.Equal(t, []any{
			map[string]any{"name": "alice", "status": "active"},
		}, got)
	})

	t.Run("non-map entries skipped", func(t *testing.T) {
		list := []any{
			map[string]any{"name": "alice", "status": "active"},
			"not a map",
			42,
		}
		got, err := Where("status", "active", list)
		require.NoError(t, err)
		assert.Equal(t, []any{
			map[string]any{"name": "alice", "status": "active"},
		}, got)
	})

	t.Run("non-list input", func(t *testing.T) {
		_, err := Where("key", "val", "not a list")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected a list/array")
	})

	t.Run("typed slice input", func(t *testing.T) {
		list := []map[string]any{
			{"k": "v", "x": "1"},
			{"k": "other", "x": "2"},
		}
		got, err := Where("k", "v", list)
		require.NoError(t, err)
		assert.Equal(t, []any{
			map[string]any{"k": "v", "x": "1"},
		}, got)
	})
}

func TestSelectField(t *testing.T) {
	items := []any{
		map[string]any{"name": "alice", "age": 30},
		map[string]any{"name": "bob", "age": 25},
		map[string]any{"name": "carol", "age": 35},
	}

	t.Run("extract string field", func(t *testing.T) {
		got, err := SelectField("name", items)
		require.NoError(t, err)
		assert.Equal(t, []any{"alice", "bob", "carol"}, got)
	})

	t.Run("extract number field", func(t *testing.T) {
		got, err := SelectField("age", items)
		require.NoError(t, err)
		assert.Equal(t, []any{30, 25, 35}, got)
	})

	t.Run("missing key produces nil", func(t *testing.T) {
		got, err := SelectField("email", items)
		require.NoError(t, err)
		assert.Equal(t, []any{nil, nil, nil}, got)
	})

	t.Run("nil list", func(t *testing.T) {
		got, err := SelectField("name", nil)
		require.NoError(t, err)
		assert.Equal(t, []any{}, got)
	})

	t.Run("empty list", func(t *testing.T) {
		got, err := SelectField("name", []any{})
		require.NoError(t, err)
		assert.Equal(t, []any{}, got)
	})

	t.Run("non-map entries skipped", func(t *testing.T) {
		list := []any{
			map[string]any{"name": "alice"},
			"not a map",
		}
		got, err := SelectField("name", list)
		require.NoError(t, err)
		assert.Equal(t, []any{"alice"}, got)
	})

	t.Run("non-list input", func(t *testing.T) {
		_, err := SelectField("name", 42)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected a list/array")
	})

	t.Run("typed slice input", func(t *testing.T) {
		list := []map[string]any{
			{"name": "alice"},
			{"name": "bob"},
		}
		got, err := SelectField("name", list)
		require.NoError(t, err)
		assert.Equal(t, []any{"alice", "bob"}, got)
	})
}

func TestWhereFunc(t *testing.T) {
	f := WhereFunc()
	assert.Equal(t, "where", f.Name)
	assert.True(t, f.Custom)
	assert.NotEmpty(t, f.Description)
	assert.NotEmpty(t, f.Examples)
	assert.Contains(t, f.Func, "where")
}

func TestSelectFunc(t *testing.T) {
	f := SelectFunc()
	assert.Equal(t, "selectField", f.Name)
	assert.True(t, f.Custom)
	assert.NotEmpty(t, f.Description)
	assert.NotEmpty(t, f.Examples)
	assert.Contains(t, f.Func, "selectField")
}
