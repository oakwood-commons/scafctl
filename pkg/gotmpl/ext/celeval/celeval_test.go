// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celeval

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCel(t *testing.T) {
	t.Run("simple string expression", func(t *testing.T) {
		got, err := Cel("'hello world'", nil)
		require.NoError(t, err)
		assert.Equal(t, "hello world", got)
	})

	t.Run("arithmetic expression", func(t *testing.T) {
		got, err := Cel("2 + 3", nil)
		require.NoError(t, err)
		assert.Equal(t, int64(5), got)
	})

	t.Run("boolean expression", func(t *testing.T) {
		got, err := Cel("true && false", nil)
		require.NoError(t, err)
		assert.Equal(t, false, got)
	})

	t.Run("access root data via underscore", func(t *testing.T) {
		data := map[string]any{"name": "world"}
		got, err := Cel("'hello ' + _.name", data)
		require.NoError(t, err)
		assert.Equal(t, "hello world", got)
	})

	t.Run("conditional expression", func(t *testing.T) {
		data := map[string]any{"count": int64(15)}
		got, err := Cel("_.count > 10 ? 'many' : 'few'", data)
		require.NoError(t, err)
		assert.Equal(t, "many", got)
	})

	t.Run("list filtering", func(t *testing.T) {
		data := map[string]any{
			"items": []any{
				map[string]any{"name": "a", "active": true},
				map[string]any{"name": "b", "active": false},
				map[string]any{"name": "c", "active": true},
			},
		}
		got, err := Cel("_.items.filter(x, x.active)", data)
		require.NoError(t, err)
		result, ok := got.([]any)
		require.True(t, ok, "expected []any, got %T", got)
		assert.Len(t, result, 2)
	})

	t.Run("empty expression returns error", func(t *testing.T) {
		_, err := Cel("", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expression cannot be empty")
	})

	t.Run("invalid expression returns error", func(t *testing.T) {
		_, err := Cel("invalid syntax here!", nil)
		require.Error(t, err)
	})

	t.Run("nil data", func(t *testing.T) {
		got, err := Cel("42", nil)
		require.NoError(t, err)
		assert.Equal(t, int64(42), got)
	})

	t.Run("nested map access", func(t *testing.T) {
		data := map[string]any{
			"config": map[string]any{
				"server": map[string]any{
					"port": int64(8080),
				},
			},
		}
		got, err := Cel("_.config.server.port", data)
		require.NoError(t, err)
		assert.Equal(t, int64(8080), got)
	})
}

func TestCelFunc(t *testing.T) {
	f := CelFunc()
	assert.Equal(t, "cel", f.Name)
	assert.True(t, f.Custom)
	assert.NotEmpty(t, f.Description)
	assert.NotEmpty(t, f.Examples)
	assert.Contains(t, f.Func, "cel")
}
